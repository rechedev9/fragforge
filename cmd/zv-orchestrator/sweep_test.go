package main

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/obs"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

// seedJob inserts and returns a job in the given status.
func seedJob(t *testing.T, repo interruptSweeperRepo, status job.Status) job.Job {
	t.Helper()
	j := &job.Job{
		Status:        job.StatusQueued,
		DemoPath:      "demos/" + status.String() + ".dem",
		DemoSHA256:    "sha-" + status.String(),
		TargetSteamID: "76561198000000000",
		Rules:         rules.Default(),
	}
	if err := repo.Create(context.Background(), j); err != nil {
		t.Fatalf("Create(%s): %v", status, err)
	}
	if status != job.StatusQueued {
		if err := repo.UpdateStatus(context.Background(), j.ID, status, ""); err != nil {
			t.Fatalf("UpdateStatus(%s): %v", status, err)
		}
		j.Status = status
	}
	return *j
}

// interruptSweeperRepo is the repo surface the sweep test drives: enough to seed
// jobs and read them back. Both the memory and sqlite repos satisfy it.
type interruptSweeperRepo interface {
	interruptSweeper
	Create(context.Context, *job.Job) error
	Get(context.Context, uuid.UUID) (job.Job, error)
}

type streamInterruptSweeperRepo interface {
	streamInterruptSweeper
	Create(context.Context, *streamclips.Job) error
	Get(context.Context, uuid.UUID) (streamclips.Job, error)
}

func seedStreamJob(t *testing.T, repo streamInterruptSweeperRepo, status streamclips.Status) streamclips.Job {
	t.Helper()
	j := &streamclips.Job{
		Status:       status,
		SourcePath:   "streams/" + string(status) + ".mp4",
		SourceSHA256: "sha-" + string(status),
		Probe:        streamclips.SourceProbe{Width: 1920, Height: 1080},
	}
	if err := repo.Create(context.Background(), j); err != nil {
		t.Fatalf("Create(%s): %v", status, err)
	}
	return *j
}

func putSweepFixture(t *testing.T, store storage.Storage, key string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal(%s): %v", key, err)
	}
	if err := store.Put(key, bytes.NewReader(b)); err != nil {
		t.Fatalf("Put(%s): %v", key, err)
	}
}

func readSweepFixture(t *testing.T, store storage.Storage, key string, dst any) {
	t.Helper()
	rc, err := store.Open(key)
	if err != nil {
		t.Fatalf("Open(%s): %v", key, err)
	}
	defer rc.Close()
	if err := json.NewDecoder(rc).Decode(dst); err != nil {
		t.Fatalf("Decode(%s): %v", key, err)
	}
}

func TestSweepInterruptedJobsFailsOnlyTransientStates(t *testing.T) {
	repos := map[string]interruptSweeperRepo{
		"memory": newMemoryJobRepository(),
		"sqlite": newTestSQLiteRepo(t),
	}

	transient := []job.Status{
		job.StatusQueued,
		job.StatusScanning,
		job.StatusParsing,
		job.StatusRecording,
		job.StatusComposing,
	}
	// Stable and terminal statuses that a restart must never touch.
	untouched := []job.Status{
		job.StatusScanned,
		job.StatusParsed,
		job.StatusRecorded,
		job.StatusComposed,
		job.StatusDone,
		job.StatusFailed,
	}

	for name, repo := range repos {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			transientIDs := map[job.Status]job.Job{}
			for _, s := range transient {
				transientIDs[s] = seedJob(t, repo, s)
			}
			untouchedIDs := map[job.Status]job.Job{}
			for _, s := range untouched {
				untouchedIDs[s] = seedJob(t, repo, s)
			}

			rec, err := obs.New(t.TempDir())
			if err != nil {
				t.Fatalf("obs.New: %v", err)
			}

			swept, err := sweepInterruptedJobs(ctx, repo, rec)
			if err != nil {
				t.Fatalf("sweepInterruptedJobs: %v", err)
			}
			if swept != len(transient) {
				t.Errorf("swept = %d, want %d", swept, len(transient))
			}

			for s, seeded := range transientIDs {
				got, err := repo.Get(ctx, seeded.ID)
				if err != nil {
					t.Fatalf("Get(%s): %v", s, err)
				}
				if got.Status != job.StatusFailed {
					t.Errorf("status after sweep for %s = %s, want failed", s, got.Status)
				}
				if !strings.Contains(got.FailureReason, "interrupted") {
					t.Errorf("failure reason for %s = %q, want interrupted reason", s, got.FailureReason)
				}
				if s != job.StatusQueued && !strings.Contains(got.FailureReason, "mid-"+s.String()) {
					t.Errorf("failure reason for %s = %q, want interrupted mid-%s", s, got.FailureReason, s)
				}
			}

			for s, seeded := range untouchedIDs {
				got, err := repo.Get(ctx, seeded.ID)
				if err != nil {
					t.Fatalf("Get(%s): %v", s, err)
				}
				if got.Status != s {
					t.Errorf("status for %s changed to %s, want untouched", s, got.Status)
				}
			}

			// Each swept failure is recorded once through obs (class=interrupted).
			var interruptErrors int64
			for _, m := range rec.Snapshot() {
				if m.Name == "fragforge_errors_total" && m.Labels["class"] == interruptedClass {
					interruptErrors += m.Value
				}
			}
			if interruptErrors != int64(len(transient)) {
				t.Errorf("obs interrupted errors = %d, want %d", interruptErrors, len(transient))
			}
		})
	}
}

func TestSweepInterruptedDemoRenderStatesFailsActiveStatesAcrossParentStatuses(t *testing.T) {
	for _, repoName := range []string{"memory", "sqlite"} {
		t.Run(repoName, func(t *testing.T) {
			var repo interruptSweeperRepo
			if repoName == "memory" {
				repo = newMemoryJobRepository()
			} else {
				repo = newTestSQLiteRepo(t)
			}
			store, err := storage.NewLocal(t.TempDir())
			if err != nil {
				t.Fatalf("NewLocal: %v", err)
			}
			loadouts := renderplan.LoadoutCatalog()
			if len(loadouts) == 0 {
				t.Fatal("LoadoutCatalog is empty")
			}
			loadout := loadouts[0]
			createdAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
			cases := []struct {
				parentStatus job.Status
				stateStatus  string
				wantFailed   bool
			}{
				{parentStatus: job.StatusDone, stateStatus: renderplan.RenderVariantStatusQueued, wantFailed: true},
				{parentStatus: job.StatusRecorded, stateStatus: renderplan.RenderVariantStatusRendering, wantFailed: true},
				{parentStatus: job.StatusComposed, stateStatus: renderplan.RenderVariantStatusReady},
				{parentStatus: job.StatusFailed, stateStatus: renderplan.RenderVariantStatusFailed},
			}

			type fixture struct {
				key        string
				before     renderplan.RenderVariantState
				wantFailed bool
			}
			fixtures := make([]fixture, 0, len(cases))
			for i, tc := range cases {
				j := seedJob(t, repo, tc.parentStatus)
				state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
					JobID:    j.ID,
					Loadout:  loadout,
					Status:   tc.stateStatus,
					Warnings: []string{"keep warning"},
					Now:      createdAt.Add(time.Duration(i) * time.Minute),
				})
				if err != nil {
					t.Fatalf("NewRenderVariantStateForLoadout: %v", err)
				}
				if tc.stateStatus == renderplan.RenderVariantStatusFailed {
					state.Error = "existing failure"
				}
				key, err := renderplan.RenderVariantStateKey(j.ID, loadout.Variant)
				if err != nil {
					t.Fatalf("RenderVariantStateKey: %v", err)
				}
				putSweepFixture(t, store, key, state)
				fixtures = append(fixtures, fixture{key: key, before: state, wantFailed: tc.wantFailed})
			}

			swept, err := sweepInterruptedDemoRenderStates(context.Background(), repo, store)
			if err != nil {
				t.Fatalf("sweepInterruptedDemoRenderStates: %v", err)
			}
			if got, want := swept, 2; got != want {
				t.Fatalf("swept = %d, want %d", got, want)
			}
			for _, f := range fixtures {
				var got renderplan.RenderVariantState
				readSweepFixture(t, store, f.key, &got)
				want := f.before
				if f.wantFailed {
					if !got.UpdatedAt.After(f.before.UpdatedAt) {
						t.Errorf("UpdatedAt for %s = %s, want after %s", f.key, got.UpdatedAt, f.before.UpdatedAt)
					}
					want.Status = renderplan.RenderVariantStatusFailed
					want.Error = interruptedDemoRenderReason
					want.UpdatedAt = got.UpdatedAt
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("state for %s = %+v, want %+v", f.key, got, want)
				}
			}
		})
	}
}

func TestSweepInterruptedGenerateRunsUsesActiveRunMarker(t *testing.T) {
	for _, repoName := range []string{"memory", "sqlite"} {
		t.Run(repoName, func(t *testing.T) {
			var repo interruptSweeperRepo
			if repoName == "memory" {
				repo = newMemoryJobRepository()
			} else {
				repo = newTestSQLiteRepo(t)
			}
			store, err := storage.NewLocal(t.TempDir())
			if err != nil {
				t.Fatalf("NewLocal: %v", err)
			}
			loadouts := renderplan.LoadoutCatalog()
			if len(loadouts) == 0 {
				t.Fatal("LoadoutCatalog is empty")
			}
			activeIntent := renderplan.GenerateIntent{
				Variant:     loadouts[0].Variant,
				Edit:        renderplan.DefaultEditRequest(),
				ActiveRunID: uuid.New(),
				AcceptedAt:  time.Now().UTC(),
			}
			staleIntent := activeIntent
			staleIntent.ActiveRunID = uuid.Nil

			activeParsed := seedJob(t, repo, job.StatusParsed)
			activeWithReadyState := seedJob(t, repo, job.StatusRecorded)
			activeWithMalformedState := seedJob(t, repo, job.StatusParsed)
			activeInvalidIntent := seedJob(t, repo, job.StatusRecorded)
			staleDisplayIntent := seedJob(t, repo, job.StatusRecorded)
			withoutIntent := seedJob(t, repo, job.StatusRecorded)
			doneWithActiveIntent := seedJob(t, repo, job.StatusDone)
			for _, j := range []job.Job{activeParsed, activeWithReadyState, activeWithMalformedState, doneWithActiveIntent} {
				putSweepFixture(t, store, artifacts.GenerateIntentKey(j.ID), activeIntent)
			}
			invalidIntent := activeIntent
			invalidIntent.Variant = "retired-preset"
			putSweepFixture(t, store, artifacts.GenerateIntentKey(activeInvalidIntent.ID), invalidIntent)
			putSweepFixture(t, store, artifacts.GenerateIntentKey(staleDisplayIntent.ID), staleIntent)

			state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
				JobID:   activeWithReadyState.ID,
				Loadout: loadouts[0],
				Status:  renderplan.RenderVariantStatusReady,
			})
			if err != nil {
				t.Fatalf("NewRenderVariantStateForLoadout: %v", err)
			}
			stateKey, err := renderplan.RenderVariantStateKey(activeWithReadyState.ID, activeIntent.Variant)
			if err != nil {
				t.Fatalf("RenderVariantStateKey: %v", err)
			}
			putSweepFixture(t, store, stateKey, state)
			malformedKey, err := renderplan.RenderVariantStateKey(activeWithMalformedState.ID, activeIntent.Variant)
			if err != nil {
				t.Fatalf("RenderVariantStateKey: %v", err)
			}
			if err := store.Put(malformedKey, strings.NewReader("{not-json")); err != nil {
				t.Fatalf("Put malformed render state: %v", err)
			}

			swept, err := sweepInterruptedGenerateRuns(context.Background(), repo, store, nil)
			if err != nil {
				t.Fatalf("sweepInterruptedGenerateRuns: %v", err)
			}
			if got, want := swept, 4; got != want {
				t.Fatalf("swept = %d, want %d", got, want)
			}
			for _, j := range []job.Job{activeParsed, activeWithReadyState, activeWithMalformedState, activeInvalidIntent} {
				got, err := repo.Get(context.Background(), j.ID)
				if err != nil {
					t.Fatalf("Get(%s): %v", j.ID, err)
				}
				if got.Status != job.StatusFailed || got.FailureReason != interruptedGenerateReason {
					t.Errorf("active run %s = status %s reason %q, want failed/%q", j.ID, got.Status, got.FailureReason, interruptedGenerateReason)
				}
			}
			for _, j := range []job.Job{staleDisplayIntent, withoutIntent, doneWithActiveIntent} {
				got, err := repo.Get(context.Background(), j.ID)
				if err != nil {
					t.Fatalf("Get(%s): %v", j.ID, err)
				}
				if got.Status != j.Status {
					t.Errorf("preserved job %s status = %s, want %s", j.ID, got.Status, j.Status)
				}
			}
		})
	}
}

func TestSweepInterruptedStreamJobsFailsOnlyAcquiringAndRendering(t *testing.T) {
	for _, repoName := range []string{"memory", "sqlite"} {
		t.Run(repoName, func(t *testing.T) {
			var repo streamInterruptSweeperRepo
			if repoName == "memory" {
				repo = newMemoryStreamJobRepository()
			} else {
				repo = newTestSQLiteStreamRepo(t)
			}
			statuses := []streamclips.Status{
				streamclips.StatusAcquiring,
				streamclips.StatusUploaded,
				streamclips.StatusReady,
				streamclips.StatusRendering,
				streamclips.StatusRendered,
				streamclips.StatusFailed,
			}
			seeded := make([]streamclips.Job, 0, len(statuses))
			for _, status := range statuses {
				seeded = append(seeded, seedStreamJob(t, repo, status))
			}

			swept, err := sweepInterruptedStreamJobs(context.Background(), repo, nil)
			if err != nil {
				t.Fatalf("sweepInterruptedStreamJobs: %v", err)
			}
			if got, want := swept, 2; got != want {
				t.Fatalf("swept = %d, want %d", got, want)
			}
			for _, before := range seeded {
				got, err := repo.Get(context.Background(), before.ID)
				if err != nil {
					t.Fatalf("Get(%s): %v", before.ID, err)
				}
				switch before.Status {
				case streamclips.StatusAcquiring:
					if got.Status != streamclips.StatusFailed || got.FailureReason != interruptedStreamAcquire {
						t.Errorf("acquiring job = status %s reason %q, want failed/%q", got.Status, got.FailureReason, interruptedStreamAcquire)
					}
				case streamclips.StatusRendering:
					if got.Status != streamclips.StatusFailed || got.FailureReason != interruptedStreamRender {
						t.Errorf("rendering job = status %s reason %q, want failed/%q", got.Status, got.FailureReason, interruptedStreamRender)
					}
				default:
					if got.Status != before.Status || got.FailureReason != before.FailureReason {
						t.Errorf("job %s changed from status %s reason %q to status %s reason %q", before.ID, before.Status, before.FailureReason, got.Status, got.FailureReason)
					}
				}
			}
		})
	}
}

func TestSweepInterruptedStreamJobsIsNotCappedAtHTTPListLimit(t *testing.T) {
	const jobCount = 105
	for _, repoName := range []string{"memory", "sqlite"} {
		t.Run(repoName, func(t *testing.T) {
			var repo streamInterruptSweeperRepo
			if repoName == "memory" {
				repo = newMemoryStreamJobRepository()
			} else {
				repo = newTestSQLiteStreamRepo(t)
			}
			for range jobCount {
				seedStreamJob(t, repo, streamclips.StatusAcquiring)
			}
			ready := seedStreamJob(t, repo, streamclips.StatusReady)

			swept, err := sweepInterruptedStreamJobs(context.Background(), repo, nil)
			if err != nil {
				t.Fatalf("sweepInterruptedStreamJobs: %v", err)
			}
			if got, want := swept, jobCount; got != want {
				t.Fatalf("swept = %d, want %d", got, want)
			}
			failed, err := repo.ListByStatus(context.Background(), streamclips.StatusFailed)
			if err != nil {
				t.Fatalf("ListByStatus(failed): %v", err)
			}
			if got, want := len(failed), jobCount; got != want {
				t.Errorf("failed jobs = %d, want %d", got, want)
			}
			gotReady, err := repo.Get(context.Background(), ready.ID)
			if err != nil {
				t.Fatalf("Get(ready): %v", err)
			}
			if gotReady.Status != streamclips.StatusReady {
				t.Errorf("ready job status = %s, want ready", gotReady.Status)
			}
		})
	}
}

func TestSweepInterruptedStreamRenderStatesPreservesArtifactData(t *testing.T) {
	for _, repoName := range []string{"memory", "sqlite"} {
		t.Run(repoName, func(t *testing.T) {
			var repo streamInterruptSweeperRepo
			if repoName == "memory" {
				repo = newMemoryStreamJobRepository()
			} else {
				repo = newTestSQLiteStreamRepo(t)
			}
			store, err := storage.NewLocal(t.TempDir())
			if err != nil {
				t.Fatalf("NewLocal: %v", err)
			}
			variants := streamclips.VariantNames()
			if len(variants) == 0 {
				t.Fatal("VariantNames is empty")
			}
			variant := variants[0]
			updatedAt := time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC)
			cases := []struct {
				parentStatus streamclips.Status
				stateStatus  streamclips.Status
				wantFailed   bool
			}{
				{parentStatus: streamclips.StatusReady, stateStatus: streamclips.StatusRendering, wantFailed: true},
				{parentStatus: streamclips.StatusRendered, stateStatus: streamclips.StatusRendering, wantFailed: true},
				{parentStatus: streamclips.StatusRendering, stateStatus: streamclips.StatusReady},
				{parentStatus: streamclips.StatusFailed, stateStatus: streamclips.StatusRendered},
				{parentStatus: streamclips.StatusUploaded, stateStatus: streamclips.StatusFailed},
			}
			type fixture struct {
				key        string
				before     streamclips.RenderState
				wantFailed bool
			}
			fixtures := make([]fixture, 0, len(cases))
			for i, tc := range cases {
				j := seedStreamJob(t, repo, tc.parentStatus)
				state, err := streamclips.NewRenderState(
					j.ID,
					variant,
					tc.stateStatus,
					[]string{"keep warning"},
					"",
					[]streamclips.VideoEntry{{ClipID: "clip-1", Key: "keep/video.mp4"}},
				)
				if err != nil {
					t.Fatalf("NewRenderState: %v", err)
				}
				state.UpdatedAt = updatedAt.Add(time.Duration(i) * time.Minute)
				if tc.stateStatus == streamclips.StatusFailed {
					state.Error = "existing failure"
				}
				key, err := streamclips.RenderStateKey(j.ID, variant)
				if err != nil {
					t.Fatalf("RenderStateKey: %v", err)
				}
				putSweepFixture(t, store, key, state)
				fixtures = append(fixtures, fixture{key: key, before: state, wantFailed: tc.wantFailed})
			}

			swept, err := sweepInterruptedStreamRenderStates(context.Background(), repo, store)
			if err != nil {
				t.Fatalf("sweepInterruptedStreamRenderStates: %v", err)
			}
			if got, want := swept, 2; got != want {
				t.Fatalf("swept = %d, want %d", got, want)
			}
			for _, f := range fixtures {
				var got streamclips.RenderState
				readSweepFixture(t, store, f.key, &got)
				want := f.before
				if f.wantFailed {
					if !got.UpdatedAt.After(f.before.UpdatedAt) {
						t.Errorf("UpdatedAt for %s = %s, want after %s", f.key, got.UpdatedAt, f.before.UpdatedAt)
					}
					want.Status = streamclips.StatusFailed
					want.Error = interruptedStreamRender
					want.UpdatedAt = got.UpdatedAt
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("state for %s = %+v, want %+v", f.key, got, want)
				}
			}
		})
	}
}
