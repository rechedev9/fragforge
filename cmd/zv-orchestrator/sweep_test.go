package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

type failingDemoUpdateRepo struct {
	interruptSweeperRepo
	failIDs map[uuid.UUID]bool
}

func (r failingDemoUpdateRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status job.Status, reason string) error {
	if r.failIDs[id] {
		return errors.New("injected demo update failure")
	}
	return r.interruptSweeperRepo.UpdateStatus(ctx, id, status, reason)
}

type failingStreamUpdateRepo struct {
	streamInterruptSweeperRepo
	failIDs map[uuid.UUID]bool
}

func (r failingStreamUpdateRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status streamclips.Status, reason string) error {
	if r.failIDs[id] {
		return errors.New("injected stream update failure")
	}
	return r.streamInterruptSweeperRepo.UpdateStatus(ctx, id, status, reason)
}

type failingPutStorage struct {
	storage.Storage
	failKeys map[string]bool
}

func (s failingPutStorage) Put(key string, r io.Reader) error {
	if s.failKeys[key] {
		return errors.New("injected storage write failure")
	}
	return s.Storage.Put(key, r)
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

func putPublishedStreamRenderArtifacts(t *testing.T, store storage.Storage, state streamclips.RenderState) {
	t.Helper()
	result, err := streamclips.NewRenderResult(state.JobID, state.Variant, state.Videos, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	putSweepFixture(t, store, state.ResultKey, result)
	if err := store.Put(state.GalleryKey, strings.NewReader("<html>gallery</html>")); err != nil {
		t.Fatal(err)
	}
	for _, video := range state.Videos {
		if err := store.Put(video.Key, strings.NewReader("video")); err != nil {
			t.Fatal(err)
		}
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

func interruptedObsCount(rec *obs.Recorder, stage string) int64 {
	var count int64
	for _, metric := range rec.Snapshot() {
		if metric.Name == "fragforge_errors_total" && metric.Labels["stage"] == stage && metric.Labels["class"] == interruptedClass {
			count += metric.Value
		}
	}
	return count
}

func TestSweepInterruptedJobsFailsOnlyNonresumableStates(t *testing.T) {
	repos := map[string]interruptSweeperRepo{
		"memory": newMemoryJobRepository(),
		"sqlite": newTestSQLiteRepo(t),
	}

	nonresumable := []job.Status{
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

			nonresumableIDs := map[job.Status]job.Job{}
			for _, s := range nonresumable {
				nonresumableIDs[s] = seedJob(t, repo, s)
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
			if swept != len(nonresumable) {
				t.Errorf("swept = %d, want %d", swept, len(nonresumable))
			}

			for s, seeded := range nonresumableIDs {
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
			if interruptErrors != int64(len(nonresumable)) {
				t.Errorf("obs interrupted errors = %d, want %d", interruptErrors, len(nonresumable))
			}
		})
	}
}

func TestSweepInterruptedJobsAggregatesRecordFailures(t *testing.T) {
	base := newMemoryJobRepository()
	firstFailure := seedJob(t, base, job.StatusQueued)
	success := seedJob(t, base, job.StatusQueued)
	secondFailure := seedJob(t, base, job.StatusParsing)
	repo := failingDemoUpdateRepo{
		interruptSweeperRepo: base,
		failIDs: map[uuid.UUID]bool{
			firstFailure.ID:  true,
			secondFailure.ID: true,
		},
	}

	swept, err := sweepInterruptedJobs(context.Background(), repo, nil)
	if err == nil {
		t.Fatal("sweepInterruptedJobs error = nil, want aggregated update failures")
	}
	for _, id := range []uuid.UUID{firstFailure.ID, secondFailure.ID} {
		if !strings.Contains(err.Error(), id.String()) {
			t.Errorf("error %q does not include failed job %s", err, id)
		}
	}
	if got, want := swept, 1; got != want {
		t.Fatalf("swept = %d, want %d", got, want)
	}
	got, err := base.Get(context.Background(), success.ID)
	if err != nil {
		t.Fatalf("Get(success): %v", err)
	}
	if got.Status != job.StatusFailed {
		t.Errorf("successful record status = %s, want failed", got.Status)
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

			swept, err := sweepInterruptedDemoRenderStates(context.Background(), repo, store, nil)
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

func TestSweepInterruptedDemoRenderStatesRepairsCorruptDocumentsAndContinues(t *testing.T) {
	repo := newMemoryJobRepository()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	loadouts := renderplan.LoadoutCatalog()
	if len(loadouts) == 0 {
		t.Fatal("LoadoutCatalog is empty")
	}
	loadout := loadouts[0]
	repairedJob := seedJob(t, repo, job.StatusDone)
	unwritableJob := seedJob(t, repo, job.StatusRecorded)
	repairedKey, err := renderplan.RenderVariantStateKey(repairedJob.ID, loadout.Variant)
	if err != nil {
		t.Fatalf("RenderVariantStateKey(repaired): %v", err)
	}
	unwritableKey, err := renderplan.RenderVariantStateKey(unwritableJob.ID, loadout.Variant)
	if err != nil {
		t.Fatalf("RenderVariantStateKey(unwritable): %v", err)
	}
	for _, key := range []string{repairedKey, unwritableKey} {
		if err := store.Put(key, strings.NewReader(`{"status":"queued"`)); err != nil {
			t.Fatalf("Put corrupt %s: %v", key, err)
		}
	}
	rec, err := obs.New(t.TempDir())
	if err != nil {
		t.Fatalf("obs.New: %v", err)
	}

	swept, err := sweepInterruptedDemoRenderStates(
		context.Background(),
		repo,
		failingPutStorage{Storage: store, failKeys: map[string]bool{unwritableKey: true}},
		rec,
	)
	if err == nil {
		t.Fatal("sweepInterruptedDemoRenderStates error = nil, want unwritable record error")
	}
	for _, want := range []string{"decode demo render state", "write failed demo render state", unwritableKey} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not include %q", err, want)
		}
	}
	if got, want := swept, 1; got != want {
		t.Fatalf("swept = %d, want %d", got, want)
	}

	var repaired renderplan.RenderVariantState
	readSweepFixture(t, store, repairedKey, &repaired)
	if repaired.JobID != repairedJob.ID || repaired.Variant != loadout.Variant || repaired.Status != renderplan.RenderVariantStatusFailed {
		t.Errorf("repaired state = %+v, want job=%s variant=%s status=failed", repaired, repairedJob.ID, loadout.Variant)
	}
	if repaired.SchemaVersion == "" || repaired.CreatedAt.IsZero() || repaired.Error != interruptedDemoRenderReason {
		t.Errorf("repaired state metadata = %+v, want minimal complete failed state", repaired)
	}
	if got, want := interruptedObsCount(rec, obs.StageRender), int64(1); got != want {
		t.Errorf("render interrupted obs = %d, want %d", got, want)
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
			malformedIntent := seedJob(t, repo, job.StatusParsed)
			failedWithActiveIntent := seedJob(t, repo, job.StatusFailed)
			staleDisplayIntent := seedJob(t, repo, job.StatusRecorded)
			withoutIntent := seedJob(t, repo, job.StatusRecorded)
			doneWithActiveIntent := seedJob(t, repo, job.StatusDone)
			for _, j := range []job.Job{activeParsed, activeWithReadyState, activeWithMalformedState, failedWithActiveIntent, doneWithActiveIntent} {
				putSweepFixture(t, store, artifacts.GenerateIntentKey(j.ID), activeIntent)
			}
			invalidIntent := activeIntent
			invalidIntent.Variant = "retired-preset"
			putSweepFixture(t, store, artifacts.GenerateIntentKey(activeInvalidIntent.ID), invalidIntent)
			if err := store.Put(artifacts.GenerateIntentKey(malformedIntent.ID), strings.NewReader(`{"active_run_id":`)); err != nil {
				t.Fatalf("Put malformed generate intent: %v", err)
			}
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
			if got, want := swept, 7; got != want {
				t.Fatalf("swept = %d, want %d", got, want)
			}
			for _, j := range []job.Job{activeParsed, activeWithReadyState, activeWithMalformedState, activeInvalidIntent, malformedIntent} {
				got, err := repo.Get(context.Background(), j.ID)
				if err != nil {
					t.Fatalf("Get(%s): %v", j.ID, err)
				}
				if got.Status != job.StatusFailed || got.FailureReason != interruptedGenerateReason {
					t.Errorf("active run %s = status %s reason %q, want failed/%q", j.ID, got.Status, got.FailureReason, interruptedGenerateReason)
				}
			}
			for _, j := range []job.Job{staleDisplayIntent, withoutIntent, failedWithActiveIntent, doneWithActiveIntent} {
				got, err := repo.Get(context.Background(), j.ID)
				if err != nil {
					t.Fatalf("Get(%s): %v", j.ID, err)
				}
				if got.Status != j.Status {
					t.Errorf("preserved job %s status = %s, want %s", j.ID, got.Status, j.Status)
				}
			}
			for _, j := range []job.Job{
				activeParsed,
				activeWithReadyState,
				activeWithMalformedState,
				activeInvalidIntent,
				malformedIntent,
				failedWithActiveIntent,
				doneWithActiveIntent,
			} {
				var repaired renderplan.GenerateIntent
				readSweepFixture(t, store, artifacts.GenerateIntentKey(j.ID), &repaired)
				if repaired.ActiveRunID != uuid.Nil {
					t.Errorf("repaired intent %s ActiveRunID = %s, want nil", j.ID, repaired.ActiveRunID)
				}
				if err := repaired.Validate(); err != nil {
					t.Errorf("repaired intent %s is invalid: %v", j.ID, err)
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

func TestSweepInterruptedStreamJobsAggregatesRecordFailures(t *testing.T) {
	base := newMemoryStreamJobRepository()
	firstFailure := seedStreamJob(t, base, streamclips.StatusAcquiring)
	success := seedStreamJob(t, base, streamclips.StatusAcquiring)
	secondFailure := seedStreamJob(t, base, streamclips.StatusRendering)
	repo := failingStreamUpdateRepo{
		streamInterruptSweeperRepo: base,
		failIDs: map[uuid.UUID]bool{
			firstFailure.ID:  true,
			secondFailure.ID: true,
		},
	}

	swept, err := sweepInterruptedStreamJobs(context.Background(), repo, nil)
	if err == nil {
		t.Fatal("sweepInterruptedStreamJobs error = nil, want aggregated update failures")
	}
	for _, id := range []uuid.UUID{firstFailure.ID, secondFailure.ID} {
		if !strings.Contains(err.Error(), id.String()) {
			t.Errorf("error %q does not include failed stream job %s", err, id)
		}
	}
	if got, want := swept, 1; got != want {
		t.Fatalf("swept = %d, want %d", got, want)
	}
	got, err := base.Get(context.Background(), success.ID)
	if err != nil {
		t.Fatalf("Get(success): %v", err)
	}
	if got.Status != streamclips.StatusFailed {
		t.Errorf("successful record status = %s, want failed", got.Status)
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
				videoKey, err := streamclips.RenderVideoKey(j.ID, variant, "clip-1")
				if err != nil {
					t.Fatalf("RenderVideoKey: %v", err)
				}
				state, err := streamclips.NewRenderState(
					j.ID,
					variant,
					tc.stateStatus,
					[]string{"keep warning"},
					"",
					[]streamclips.VideoEntry{{ClipID: "clip-1", Key: videoKey}},
				)
				if err != nil {
					t.Fatalf("NewRenderState: %v", err)
				}
				state.UpdatedAt = updatedAt.Add(time.Duration(i) * time.Minute)
				if tc.stateStatus == streamclips.StatusFailed {
					state.Error = "existing failure"
				}
				if state.HasPublishedRender() {
					putPublishedStreamRenderArtifacts(t, store, state)
				}
				key, err := streamclips.RenderStateKey(j.ID, variant)
				if err != nil {
					t.Fatalf("RenderStateKey: %v", err)
				}
				putSweepFixture(t, store, key, state)
				fixtures = append(fixtures, fixture{key: key, before: state, wantFailed: tc.wantFailed})
			}

			swept, err := sweepInterruptedStreamRenderStates(context.Background(), repo, store, nil)
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

func TestSweepInterruptedStreamRenderStatesPromotesCompletedRevision(t *testing.T) {
	repo := newMemoryStreamJobRepository()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	j := seedStreamJob(t, repo, streamclips.StatusRendering)
	variant := streamclips.DefaultVariant().Name
	revisionID := uuid.New()
	prefix, err := streamclips.RenderRevisionPrefix(j.ID, variant, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	resultKey, _ := streamclips.RenderRevisionResultKey(j.ID, variant, revisionID)
	galleryKey, _ := streamclips.RenderRevisionGalleryKey(j.ID, variant, revisionID)
	videoKey, _ := streamclips.RenderRevisionVideoKey(j.ID, variant, revisionID, "clip-1")
	state, err := streamclips.NewRenderState(j.ID, variant, streamclips.StatusRendered, nil, "", []streamclips.VideoEntry{{ClipID: "clip-1", Key: videoKey}})
	if err != nil {
		t.Fatal(err)
	}
	state.ArtifactDir = prefix
	state.ResultKey = resultKey
	state.GalleryKey = galleryKey
	putPublishedStreamRenderArtifacts(t, store, state)
	stateKey, _ := streamclips.RenderStateKey(j.ID, variant)
	putSweepFixture(t, store, stateKey, state)

	swept, err := sweepInterruptedStreamRenderStates(context.Background(), repo, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if swept != 1 {
		t.Fatalf("swept = %d, want parent promotion", swept)
	}
	got, err := repo.Get(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != streamclips.StatusRendered {
		t.Fatalf("parent status = %s, want rendered", got.Status)
	}
	var persisted streamclips.RenderState
	readSweepFixture(t, store, stateKey, &persisted)
	if !reflect.DeepEqual(persisted, state) {
		t.Fatalf("revision state changed: got %+v want %+v", persisted, state)
	}
}

func TestSweepInterruptedStreamRenderStatesDoesNotPromoteMissingPublishedArtifacts(t *testing.T) {
	repo := newMemoryStreamJobRepository()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	j := seedStreamJob(t, repo, streamclips.StatusRendering)
	variant := streamclips.DefaultVariant().Name
	videoKey, err := streamclips.RenderVideoKey(j.ID, variant, "clip-1")
	if err != nil {
		t.Fatal(err)
	}
	state, err := streamclips.NewRenderState(
		j.ID, variant, streamclips.StatusRendered, nil, "",
		[]streamclips.VideoEntry{{ClipID: "clip-1", Key: videoKey}},
	)
	if err != nil {
		t.Fatal(err)
	}
	stateKey, err := streamclips.RenderStateKey(j.ID, variant)
	if err != nil {
		t.Fatal(err)
	}
	putSweepFixture(t, store, stateKey, state)

	if _, err := sweepInterruptedStreamRenderStates(context.Background(), repo, store, nil); err != nil {
		t.Fatal(err)
	}
	parent, err := repo.Get(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status == streamclips.StatusRendered {
		t.Fatal("parent was promoted from a state whose published artifacts are missing")
	}
	var persisted streamclips.RenderState
	readSweepFixture(t, store, stateKey, &persisted)
	if persisted.Status != streamclips.StatusFailed || persisted.Published {
		t.Fatalf("persisted state = %+v, want failed without published pointer", persisted)
	}
}

func TestSweepInterruptedStreamRenderStatesRestoresPublishedRevisionAfterInterruptedRerender(t *testing.T) {
	repo := newMemoryStreamJobRepository()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	j := seedStreamJob(t, repo, streamclips.StatusRendering)
	variant := streamclips.DefaultVariant().Name
	revisionID := uuid.New()
	prefix, err := streamclips.RenderRevisionPrefix(j.ID, variant, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	resultKey, err := streamclips.RenderRevisionResultKey(j.ID, variant, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	galleryKey, err := streamclips.RenderRevisionGalleryKey(j.ID, variant, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	videoKey, err := streamclips.RenderRevisionVideoKey(j.ID, variant, revisionID, "clip-1")
	if err != nil {
		t.Fatal(err)
	}
	videos := []streamclips.VideoEntry{{ClipID: "clip-1", Key: videoKey}}
	result, err := streamclips.NewRenderResult(j.ID, variant, videos, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	putSweepFixture(t, store, resultKey, result)
	if err := store.Put(galleryKey, strings.NewReader("published gallery")); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(videoKey, strings.NewReader("published video")); err != nil {
		t.Fatal(err)
	}
	state, err := streamclips.NewRenderState(
		j.ID,
		variant,
		streamclips.StatusRendered,
		nil,
		"",
		videos,
	)
	if err != nil {
		t.Fatal(err)
	}
	state.AttemptID = uuid.New()
	state.Status = streamclips.StatusRendering
	state.ArtifactDir = prefix
	state.ResultKey = resultKey
	state.GalleryKey = galleryKey
	stateKey, err := streamclips.RenderStateKey(j.ID, variant)
	if err != nil {
		t.Fatal(err)
	}
	putPublishedStreamRenderArtifacts(t, store, state)
	putSweepFixture(t, store, stateKey, state)

	swept, err := sweepInterruptedStreamRenderStates(context.Background(), repo, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if swept != 2 {
		t.Fatalf("swept = %d, want interrupted attempt plus parent restoration", swept)
	}
	parent, err := repo.Get(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != streamclips.StatusRendered || parent.FailureReason != "" {
		t.Fatalf("parent = status %s reason %q, want rendered without failure", parent.Status, parent.FailureReason)
	}
	var persisted streamclips.RenderState
	readSweepFixture(t, store, stateKey, &persisted)
	if persisted.Status != streamclips.StatusFailed || persisted.Error != interruptedStreamRender {
		t.Fatalf("attempt = status %s error %q, want failed interruption", persisted.Status, persisted.Error)
	}
	if !persisted.HasPublishedRender() ||
		persisted.ArtifactDir != prefix ||
		persisted.ResultKey != resultKey ||
		persisted.GalleryKey != galleryKey ||
		len(persisted.Videos) != 1 || persisted.Videos[0].Key != videoKey {
		t.Fatalf("published revision was not preserved: %+v", persisted)
	}

	// Simulate a process stop after the worker persisted Failed+Published but
	// before it restored the parent lease. Startup must finish that repair too.
	if err := repo.UpdateStatus(context.Background(), j.ID, streamclips.StatusRendering, ""); err != nil {
		t.Fatal(err)
	}
	swept, err = sweepInterruptedStreamRenderStates(context.Background(), repo, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	if swept != 1 {
		t.Fatalf("failed+published swept = %d, want parent restoration only", swept)
	}
	parent, err = repo.Get(context.Background(), j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != streamclips.StatusRendered {
		t.Fatalf("failed+published parent status = %s, want rendered", parent.Status)
	}
}

func TestSweepInterruptedStreamRenderStatesRepairsCorruptDocumentsAndRecordsEachStateOnce(t *testing.T) {
	repo := newMemoryStreamJobRepository()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	variants := streamclips.VariantNames()
	if len(variants) == 0 {
		t.Fatal("VariantNames is empty")
	}
	variant := variants[0]
	readyParent := seedStreamJob(t, repo, streamclips.StatusReady)
	renderingParent := seedStreamJob(t, repo, streamclips.StatusRendering)
	for _, j := range []streamclips.Job{readyParent, renderingParent} {
		key, err := streamclips.RenderStateKey(j.ID, variant)
		if err != nil {
			t.Fatalf("RenderStateKey(%s): %v", j.ID, err)
		}
		if err := store.Put(key, strings.NewReader(`{"status":"rendering"`)); err != nil {
			t.Fatalf("Put corrupt %s: %v", key, err)
		}
	}
	rec, err := obs.New(t.TempDir())
	if err != nil {
		t.Fatalf("obs.New: %v", err)
	}

	stateResult, err := reconcileInterruptedStreamRenderStates(context.Background(), repo, store, rec)
	if err != nil {
		t.Fatalf("reconcileInterruptedStreamRenderStates: %v", err)
	}
	if got, want := stateResult.Reconciled, 2; got != want {
		t.Fatalf("swept stream states = %d, want %d", got, want)
	}
	if got, want := interruptedObsCount(rec, obs.StageRender), int64(2); got != want {
		t.Errorf("render interrupted obs after state sweep = %d, want %d", got, want)
	}
	for _, j := range []streamclips.Job{readyParent, renderingParent} {
		key, err := streamclips.RenderStateKey(j.ID, variant)
		if err != nil {
			t.Fatalf("RenderStateKey(%s): %v", j.ID, err)
		}
		var state streamclips.RenderState
		readSweepFixture(t, store, key, &state)
		if state.JobID != j.ID || state.Variant != variant || state.Status != streamclips.StatusFailed || state.Error != interruptedStreamRender {
			t.Errorf("repaired stream state = %+v, want job=%s variant=%s status=failed", state, j.ID, variant)
		}
		if state.ResultKey == "" || state.GalleryKey == "" || state.ArtifactDir == "" {
			t.Errorf("repaired stream state lacks minimal artifact refs: %+v", state)
		}
	}

	jobSwept, err := sweepInterruptedStreamJobsAfterRenderStates(context.Background(), repo, rec, stateResult)
	if err != nil {
		t.Fatalf("sweepInterruptedStreamJobs: %v", err)
	}
	if got, want := jobSwept, 1; got != want {
		t.Fatalf("swept stream jobs = %d, want %d", got, want)
	}
	if got, want := interruptedObsCount(rec, obs.StageRender), int64(2); got != want {
		t.Errorf("render interrupted obs after job sweep = %d, want %d", got, want)
	}
}
