package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/tasks"
)

// ctxAwareRepo mimics *job.Repository backed by pgxpool: an UpdateStatus call
// made with an already-cancelled context fails without mutating state, exactly
// as pgxpool.Exec does. It lets these tests prove the failure-path status write
// survives a handler context cancelled by an Asynq deadline or shutdown.
type ctxAwareRepo struct {
	*fakeRepo
	cancel   context.CancelFunc
	cancelOn job.Status
}

func (r *ctxAwareRepo) UpdateStatus(ctx context.Context, id uuid.UUID, s job.Status, reason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.fakeRepo.UpdateStatus(ctx, id, s, reason); err != nil {
		return err
	}
	if r.cancel != nil && s == r.cancelOn {
		r.cancel()
	}
	return nil
}

func TestRecordWorkerMarksFailedWhenHandlerContextCanceled(t *testing.T) {
	base := newFakeRepo()
	id := uuid.New()
	plan := minimalKillPlan()
	base.jobs[id] = &job.Job{
		ID:       id,
		Status:   job.StatusParsed,
		DemoPath: "demos/test.dem",
		Rules:    rules.Default(),
		KillPlan: &plan,
	}
	store := newFakeStorage()
	_ = store.Put("demos/test.dem", bytes.NewReader([]byte("demo")))

	ctx, cancel := context.WithCancel(context.Background())
	repo := &ctxAwareRepo{fakeRepo: base}
	runner := &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		cancel() // Asynq deadline fires while the recorder subprocess runs
		return nil, errors.New("recorder canceled")
	}}
	w := NewRecordWorker(repo, store, RecordWorkerConfig{
		WorkDir:      t.TempDir(),
		RecorderPath: "zv-recorder",
		HLAEPath:     "HLAE.exe",
		CS2Path:      "cs2.exe",
	})
	w.runner = runner

	if err := w.HandleRecordDemo(ctx, recordTask(t, id)); err == nil {
		t.Fatal("HandleRecordDemo error = nil, want non-nil")
	}
	if got := base.jobs[id].Status; got != job.StatusFailed {
		t.Fatalf("Status = %s, want failed (failure write must survive a cancelled handler context)", got)
	}
}

func TestComposeWorkerMarksFailedWhenHandlerContextCanceled(t *testing.T) {
	base := newFakeRepo()
	id := uuid.New()
	base.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default()}
	store := newFakeStorage()
	putJSON(t, store, recording.ResultArtifactKey(id), recordingResultWithSegment("", "C:/stale/seg-001.mp4"))
	_ = store.Put(mustSegmentClipKey(t, id, "seg-001"), bytes.NewReader([]byte("clip")))

	ctx, cancel := context.WithCancel(context.Background())
	repo := &ctxAwareRepo{fakeRepo: base}
	runner := &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		cancel() // Asynq deadline fires while the composer subprocess runs
		return nil, errors.New("composer canceled")
	}}
	w := NewComposeWorker(repo, store, ComposeWorkerConfig{
		WorkDir:      t.TempDir(),
		ComposerPath: "zv-composer",
	})
	w.runner = runner

	if err := w.HandleComposeFinal(ctx, composeTask(t, id)); err == nil {
		t.Fatal("HandleComposeFinal error = nil, want non-nil")
	}
	if got := base.jobs[id].Status; got != job.StatusFailed {
		t.Fatalf("Status = %s, want failed (failure write must survive a cancelled handler context)", got)
	}
}

func TestParserWorkerMarksFailedWhenHandlerContextCanceled(t *testing.T) {
	base := newFakeRepo()
	id := uuid.New()
	base.jobs[id] = &job.Job{
		ID:            id,
		Status:        job.StatusQueued,
		DemoPath:      "demos/test.dem",
		TargetSteamID: "76561197960265729",
		Rules:         rules.Default(),
	}
	store := newFakeStorage()
	_ = store.Put("demos/test.dem", bytes.NewReader([]byte("not a real demo")))

	// Simulate the Asynq deadline firing right after the job is marked parsing,
	// so the subsequent failure-path write hits an already-cancelled context.
	ctx, cancel := context.WithCancel(context.Background())
	repo := &ctxAwareRepo{fakeRepo: base, cancel: cancel, cancelOn: job.StatusParsing}
	w := NewParserWorker(repo, store)

	payload, _ := json.Marshal(tasks.ParseDemoPayload{JobID: id})
	if err := w.HandleParseDemo(ctx, asynq.NewTask(tasks.TypeParseDemo, payload)); err == nil {
		t.Fatal("HandleParseDemo error = nil, want non-nil")
	}
	if got := base.jobs[id].Status; got != job.StatusFailed {
		t.Fatalf("Status = %s, want failed (failure write must survive a cancelled handler context)", got)
	}
}
