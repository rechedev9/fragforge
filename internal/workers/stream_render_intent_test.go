package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

type failRevisionResultStorage struct {
	*fakeStorage
	err error
}

func (s *failRevisionResultStorage) Put(key string, r io.Reader) error {
	if strings.Contains(key, "/revisions/") && strings.HasSuffix(key, "/render-result.json") {
		return s.err
	}
	return s.fakeStorage.Put(key, r)
}

func (s *failRevisionResultStorage) DeleteTree(prefix string) error {
	for key := range s.files {
		if key == prefix || strings.HasPrefix(key, prefix+"/") {
			delete(s.files, key)
		}
	}
	return nil
}

type lockedStreamRenderRepo struct {
	mu  sync.Mutex
	job streamclips.Job
}

func (r *lockedStreamRenderRepo) Get(context.Context, uuid.UUID) (streamclips.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.job, nil
}

func (r *lockedStreamRenderRepo) UpdateStatus(_ context.Context, _ uuid.UUID, status streamclips.Status, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.job.Status = status
	r.job.FailureReason = reason
	return nil
}

func (r *lockedStreamRenderRepo) setPlan(t *testing.T, plan streamclips.EditPlan) {
	t.Helper()
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.job.EditPlan = b
	r.mu.Unlock()
}

func (r *lockedStreamRenderRepo) status() streamclips.Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.job.Status
}

func TestBoundStreamRenderRejectsPlanChangedBeforeWorkerStarts(t *testing.T) {
	store := newFakeStorage()
	id, planA := newReadyStreamJobWithCaptions(t, store, false)
	planA.UpdatedAt = planA.UpdatedAt.UTC()
	planB := planA
	planB.Clips = append([]streamclips.ClipRange(nil), planA.Clips...)
	planB.Clips[0].Title = "changed-before-start"
	planJSON, err := json.Marshal(planB)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id),
		Probe: streamclips.SourceProbe{DurationSeconds: 2}, EditPlan: planJSON,
	})
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg", RequireAppliedKillfeedAnalysis: true,
	})
	w.runner = &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("superseded plan reached FFmpeg")
		return nil, nil
	}}
	task := newBoundStreamRenderTaskForTest(t, id, planA)
	seedBoundStreamRenderAttemptForTest(t, w, id, planA.Variant, task)
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("superseded render error = %v, want recoverable nil", err)
	}
	if got := repo.jobs[id].Status; got != streamclips.StatusReady {
		t.Fatalf("parent status = %s, want ready", got)
	}
	assertNoPublishedStreamRender(t, store, id, planA.Variant, planA.Clips[0].ID)
}

func TestSupersededQueuedRenderCannotOverwriteNewerRenderedState(t *testing.T) {
	store := newFakeStorage()
	id, planA := newReadyStreamJobWithCaptions(t, store, false)
	planB := planA
	planB.Clips = append([]streamclips.ClipRange(nil), planA.Clips...)
	planB.Clips[0].Title = "newer-render"
	planJSON, err := json.Marshal(planB)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusRendered, SourcePath: streamclips.SourceKey(id),
		Probe: streamclips.SourceProbe{DurationSeconds: 2}, EditPlan: planJSON,
	})
	state, err := streamclips.NewRenderState(id, planB.Variant, streamclips.StatusRendered, []string{"newer"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	stateKey, _ := streamclips.RenderStateKey(id, planB.Variant)
	putJSON(t, store, stateKey, state)
	before := append([]byte(nil), store.files[stateKey]...)
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{WorkDir: t.TempDir(), FFmpegPath: "ffmpeg", RequireAppliedKillfeedAnalysis: true})
	w.runner = &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("superseded queued render reached FFmpeg")
		return nil, nil
	}}
	if err := w.HandleRenderStreamClip(context.Background(), newBoundStreamRenderTaskForTest(t, id, planA)); err != nil {
		t.Fatalf("superseded render error = %v, want recoverable nil", err)
	}
	if got := store.files[stateKey]; !bytes.Equal(got, before) {
		t.Fatalf("newer rendered state was overwritten:\ngot  %s\nwant %s", got, before)
	}
	if got := repo.jobs[id].Status; got != streamclips.StatusRendered {
		t.Fatalf("parent status = %s, want rendered", got)
	}
}

func TestBoundStreamRenderRejectsTaskVariantDifferentFromPlan(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, false)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id),
		Probe: streamclips.SourceProbe{DurationSeconds: 2}, EditPlan: planJSON,
	})
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg", RequireAppliedKillfeedAnalysis: true,
	})
	w.runner = &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("mismatched render variant reached FFmpeg")
		return nil, nil
	}}
	fingerprint, err := streamclips.EditPlanFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasks.NewBoundRenderStreamClipTask(
		id,
		streamclips.VariantStreamerLandscape16x9,
		tasks.StreamRenderIntent{AttemptID: uuid.New(), EditPlanFingerprint: fingerprint},
	)
	if err != nil {
		t.Fatal(err)
	}
	seedBoundStreamRenderAttemptForTest(t, w, id, streamclips.VariantStreamerLandscape16x9, task)
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("mismatched variant error = %v, want recoverable nil", err)
	}
	if got := repo.jobs[id].Status; got != streamclips.StatusReady {
		t.Fatalf("parent status = %s, want ready", got)
	}
	assertNoPublishedStreamRender(
		t, store, id, streamclips.VariantStreamerLandscape16x9, plan.Clips[0].ID,
	)
}

func TestBoundStreamRenderRevalidatesAfterFFmpegBeforePublishing(t *testing.T) {
	store := newFakeStorage()
	id, planA := newReadyStreamJobWithCaptions(t, store, false)
	planJSON, err := json.Marshal(planA)
	if err != nil {
		t.Fatal(err)
	}
	repo := &lockedStreamRenderRepo{job: streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id),
		Probe: streamclips.SourceProbe{DurationSeconds: 2}, EditPlan: planJSON,
	}}
	started := make(chan struct{})
	release := make(chan struct{})
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg", RequireAppliedKillfeedAnalysis: true,
	})
	w.runner = &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		close(started)
		<-release
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("temporary-render"), 0o600)
	}}
	task := newBoundStreamRenderTaskForTest(t, id, planA)
	seedBoundStreamRenderAttemptForTest(t, w, id, planA.Variant, task)
	done := make(chan error, 1)
	go func() {
		done <- w.HandleRenderStreamClip(context.Background(), task)
	}()
	<-started
	planB := planA
	planB.Clips = append([]streamclips.ClipRange(nil), planA.Clips...)
	planB.Clips[0].Title = "changed-during-render"
	repo.setPlan(t, planB)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("superseded render error = %v, want recoverable nil", err)
	}
	if got := repo.status(); got != streamclips.StatusReady {
		t.Fatalf("parent status = %s, want restored ready", got)
	}
	assertNoPublishedStreamRender(t, store, id, planA.Variant, planA.Clips[0].ID)
}

func TestStreamRenderPartialRevisionUploadNeverCommitsCanonicalPointer(t *testing.T) {
	base := newFakeStorage()
	store := &failRevisionResultStorage{fakeStorage: base, err: errors.New("result storage unavailable")}
	id, plan := newReadyStreamJobWithCaptions(t, base, false)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id),
		Probe: streamclips.SourceProbe{DurationSeconds: 2}, EditPlan: planJSON,
	})
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{WorkDir: t.TempDir(), FFmpegPath: "ffmpeg"})
	w.runner = &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("new-video"), 0o600)
	}}
	task, err := tasks.NewRenderStreamClipTask(id, plan.Variant)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); !errors.Is(err, store.err) {
		t.Fatalf("render error = %v, want %v", err, store.err)
	}
	assertNoPublishedStreamRender(t, base, id, plan.Variant, plan.Clips[0].ID)

	stateKey, err := streamclips.RenderStateKey(id, plan.Variant)
	if err != nil {
		t.Fatal(err)
	}
	var state streamclips.RenderState
	if err := json.Unmarshal(base.files[stateKey], &state); err != nil {
		t.Fatal(err)
	}
	if state.Status != streamclips.StatusFailed || len(state.Videos) != 0 {
		t.Fatalf("failed render state = %+v, want no committed revision", state)
	}
	for key := range base.files {
		if strings.Contains(key, "/revisions/") && strings.Contains(key, "/videos/") {
			t.Fatalf("uncommitted revision artifact was not cleaned up: %s", key)
		}
	}
}

func TestExactKillfeedArtifactFailureIsRecoverable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		artifact func(*testing.T) []byte
	}{
		{name: "missing"},
		{name: "invalid png", artifact: func(*testing.T) []byte { return []byte("not-a-png") }},
		{name: "truncated png data", artifact: func(t *testing.T) []byte {
			return truncatePNGAfterValidConfigForTest(t, solidPNGForTest(t, 0xff, 0, 0))
		}},
		{name: "wrong png width", artifact: func(t *testing.T) []byte {
			return solidSizedPNGForTest(t, 8, streamclips.KillfeedNoticeHeight, 0xff, 0, 0)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStorage()
			id, plan := newReadyStreamJobWithCaptions(t, store, false)
			plan.KillfeedCrop = &streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
			event := durableKillfeedEvent(exactKillfeedEvent())
			plan.Clips[0].KillfeedSeconds = []float64{event.CueSeconds}
			plan.Clips[0].KillfeedKills = [][]streamclips.KillfeedKill{{}}
			const sourceSHA = "recoverable-exact-artifact-source"
			analysis := appliedKillfeedStateForEvents(t, store, id, sourceSHA, &plan, []streamclips.KillfeedAnalysisEvent{event})
			if tc.artifact != nil {
				key, err := streamclips.KillfeedEventRowKey(id, analysis.GenerationID, plan.Clips[0].ID, event.EventID, 0)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.Put(key, bytes.NewReader(tc.artifact(t))); err != nil {
					t.Fatal(err)
				}
			}
			planJSON, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			repo := newFakeStreamRepo(streamclips.Job{
				ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id),
				SourceSHA256: sourceSHA, Probe: streamclips.SourceProbe{DurationSeconds: 2}, EditPlan: planJSON,
			})
			w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
				WorkDir: t.TempDir(), FFmpegPath: "ffmpeg", RequireAppliedKillfeedAnalysis: true,
			})
			w.runner = &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
				t.Fatal("invalid exact artifact reached FFmpeg")
				return nil, nil
			}}
			planFingerprint, err := streamclips.EditPlanFingerprint(plan)
			if err != nil {
				t.Fatal(err)
			}
			task, err := tasks.NewBoundRenderStreamClipTask(id, plan.Variant, tasks.StreamRenderIntent{
				AttemptID:           uuid.New(),
				EditPlanFingerprint: planFingerprint,
				KillfeedGeneration:  analysis.GenerationID,
				KillfeedFingerprint: analysis.Fingerprint,
			})
			if err != nil {
				t.Fatal(err)
			}
			seedBoundStreamRenderAttemptForTest(t, w, id, plan.Variant, task)
			if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
				t.Fatalf("recoverable artifact render error = %v", err)
			}
			if got := repo.jobs[id].Status; got != streamclips.StatusReady {
				t.Fatalf("parent status = %s, want ready for reanalysis", got)
			}
			assertNoPublishedStreamRender(t, store, id, plan.Variant, plan.Clips[0].ID)
		})
	}
}

func newBoundStreamRenderTaskForTest(t *testing.T, id uuid.UUID, plan streamclips.EditPlan) *asynq.Task {
	t.Helper()
	fingerprint, err := streamclips.EditPlanFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasks.NewBoundRenderStreamClipTask(id, plan.Variant, tasks.StreamRenderIntent{
		AttemptID:           uuid.New(),
		EditPlanFingerprint: fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func seedBoundStreamRenderAttemptForTest(
	t *testing.T,
	w *StreamRenderWorker,
	id uuid.UUID,
	variant string,
	task *asynq.Task,
) {
	t.Helper()
	intent, ok, err := tasks.StreamRenderIntentFromTask(task)
	if err != nil || !ok {
		t.Fatalf("StreamRenderIntentFromTask = (%#v, %v, %v), want bound intent", intent, ok, err)
	}
	state, err := streamclips.NewRenderState(id, variant, streamclips.StatusRendering, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	state.AttemptID = intent.AttemptID
	if err := w.writeStreamRenderState(state); err != nil {
		t.Fatal(err)
	}
}

func assertNoPublishedStreamRender(t *testing.T, store *fakeStorage, id uuid.UUID, variant, clipID string) {
	t.Helper()
	videoKey, err := streamclips.RenderVideoKey(id, variant, clipID)
	if err != nil {
		t.Fatal(err)
	}
	resultKey, err := streamclips.RenderResultKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	galleryKey, err := streamclips.RenderGalleryKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{videoKey, resultKey, galleryKey} {
		if _, exists := store.files[key]; exists {
			t.Fatalf("superseded render published canonical artifact %s", key)
		}
	}
}
