package main

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/obs"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

type startupFailingJobRepository struct {
	orchestratorJobRepository
	failID uuid.UUID
}

type startupFailingOpenStorage struct {
	storage.Storage
	failKey string
}

func (s startupFailingOpenStorage) Open(key string) (io.ReadCloser, error) {
	if key == s.failKey {
		return nil, errors.New("injected render state read failure")
	}
	return s.Storage.Open(key)
}

func (r startupFailingJobRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status job.Status, reason string) error {
	if id == r.failID {
		return errors.New("injected startup update failure")
	}
	return r.orchestratorJobRepository.UpdateStatus(ctx, id, status, reason)
}

func TestReconcileInterruptedWorkPromotesCompletedStreamAfterSQLiteRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	databasePath := filepath.Join(dir, "jobs.db")
	storagePath := filepath.Join(dir, "artifacts")

	before, err := newSQLiteJobRepository(databasePath)
	if err != nil {
		t.Fatalf("newSQLiteJobRepository before restart: %v", err)
	}
	streamBefore, err := newSQLiteStreamJobRepository(before.db)
	if err != nil {
		_ = before.Close()
		t.Fatalf("newSQLiteStreamJobRepository before restart: %v", err)
	}
	storeBefore, err := storage.NewLocal(storagePath)
	if err != nil {
		_ = before.Close()
		t.Fatalf("storage.NewLocal before restart: %v", err)
	}

	j := createStartupStreamJob(t, streamBefore, streamclips.StatusRendering)
	variant := streamclips.DefaultVariant().Name
	wantState, err := streamclips.NewRenderState(
		j.ID,
		variant,
		streamclips.StatusRendered,
		[]string{"preserve completed warning"},
		"",
		[]streamclips.VideoEntry{{ClipID: "clip-1", Key: "completed/video.mp4"}},
	)
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	stateKey, err := streamclips.RenderStateKey(j.ID, variant)
	if err != nil {
		t.Fatalf("RenderStateKey: %v", err)
	}
	putRestartJSON(t, storeBefore, stateKey, wantState)
	if err := before.Close(); err != nil {
		t.Fatalf("close SQLite before restart: %v", err)
	}

	after, err := newSQLiteJobRepository(databasePath)
	if err != nil {
		t.Fatalf("newSQLiteJobRepository after restart: %v", err)
	}
	t.Cleanup(func() { _ = after.Close() })
	streamAfter, err := newSQLiteStreamJobRepository(after.db)
	if err != nil {
		t.Fatalf("newSQLiteStreamJobRepository after restart: %v", err)
	}
	storeAfter, err := storage.NewLocal(storagePath)
	if err != nil {
		t.Fatalf("storage.NewLocal after restart: %v", err)
	}
	rec, err := obs.New(t.TempDir())
	if err != nil {
		t.Fatalf("obs.New: %v", err)
	}

	if _, err := reconcileInterruptedWork(ctx, after, streamAfter, storeAfter, rec); err != nil {
		t.Fatalf("reconcileInterruptedWork: %v", err)
	}
	gotJob, err := streamAfter.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get completed stream parent: %v", err)
	}
	if gotJob.Status != streamclips.StatusRendered || gotJob.FailureReason != "" {
		t.Fatalf("completed stream parent = status %q reason %q, want rendered without failure", gotJob.Status, gotJob.FailureReason)
	}
	var gotState streamclips.RenderState
	readRestartJSON(t, storeAfter, stateKey, &gotState)
	if !reflect.DeepEqual(gotState, wantState) {
		t.Fatalf("completed render state changed after restart: got %+v, want %+v", gotState, wantState)
	}
	if got := startupInterruptedObsCount(rec, obs.StageRender); got != 0 {
		t.Fatalf("completed stream interrupted render events = %d, want 0", got)
	}
}

func TestReconcileInterruptedWorkRecordsOneEventForActiveStreamStateAndParent(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryJobRepository()
	streamRepo := newMemoryStreamJobRepository()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewLocal: %v", err)
	}
	j := createStartupStreamJob(t, streamRepo, streamclips.StatusRendering)
	variant := streamclips.DefaultVariant().Name
	state, err := streamclips.NewRenderState(j.ID, variant, streamclips.StatusRendering, nil, "", nil)
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	stateKey, err := streamclips.RenderStateKey(j.ID, variant)
	if err != nil {
		t.Fatalf("RenderStateKey: %v", err)
	}
	putRestartJSON(t, store, stateKey, state)
	rec, err := obs.New(t.TempDir())
	if err != nil {
		t.Fatalf("obs.New: %v", err)
	}

	result, err := reconcileInterruptedWork(ctx, repo, streamRepo, store, rec)
	if err != nil {
		t.Fatalf("reconcileInterruptedWork: %v", err)
	}
	if result.StreamRenderStates != 1 || result.StreamJobs != 1 {
		t.Fatalf("reconciled stream counts = states %d jobs %d, want 1/1", result.StreamRenderStates, result.StreamJobs)
	}
	if got := startupInterruptedObsCount(rec, obs.StageRender); got != 1 {
		t.Fatalf("active stream interrupted render events = %d, want 1", got)
	}
	gotJob, err := streamRepo.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get active stream parent: %v", err)
	}
	if gotJob.Status != streamclips.StatusFailed || gotJob.FailureReason != interruptedStreamRender {
		t.Fatalf("active stream parent = status %q reason %q, want failed/%q", gotJob.Status, gotJob.FailureReason, interruptedStreamRender)
	}
	var gotState streamclips.RenderState
	readRestartJSON(t, store, stateKey, &gotState)
	if gotState.Status != streamclips.StatusFailed || gotState.Error != interruptedStreamRender {
		t.Fatalf("active stream state = status %q error %q, want failed/%q", gotState.Status, gotState.Error, interruptedStreamRender)
	}
}

func TestReconcileInterruptedWorkPreservesRenderingParentsWhenStateAuditFails(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryJobRepository()
	streamRepo := newMemoryStreamJobRepository()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewLocal: %v", err)
	}
	rendering := createStartupStreamJob(t, streamRepo, streamclips.StatusRendering)
	acquiring := createStartupStreamJob(t, streamRepo, streamclips.StatusAcquiring)
	variant := streamclips.DefaultVariant().Name
	state, err := streamclips.NewRenderState(
		rendering.ID,
		variant,
		streamclips.StatusRendered,
		nil,
		"",
		[]streamclips.VideoEntry{{ClipID: "clip-1", Key: "completed/video.mp4"}},
	)
	if err != nil {
		t.Fatalf("NewRenderState: %v", err)
	}
	stateKey, err := streamclips.RenderStateKey(rendering.ID, variant)
	if err != nil {
		t.Fatalf("RenderStateKey: %v", err)
	}
	putRestartJSON(t, store, stateKey, state)

	result, err := reconcileInterruptedWork(
		ctx,
		repo,
		streamRepo,
		startupFailingOpenStorage{Storage: store, failKey: stateKey},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "injected render state read failure") {
		t.Fatalf("reconcileInterruptedWork error = %v, want render state audit failure", err)
	}
	if result.StreamJobs != 1 {
		t.Fatalf("reconciled stream jobs = %d, want only acquiring job", result.StreamJobs)
	}

	gotRendering, err := streamRepo.Get(ctx, rendering.ID)
	if err != nil {
		t.Fatalf("Get rendering parent: %v", err)
	}
	if gotRendering.Status != streamclips.StatusRendering || gotRendering.FailureReason != "" {
		t.Fatalf("rendering parent after incomplete audit = status %q reason %q, want unchanged rendering", gotRendering.Status, gotRendering.FailureReason)
	}
	gotAcquiring, err := streamRepo.Get(ctx, acquiring.ID)
	if err != nil {
		t.Fatalf("Get acquiring parent: %v", err)
	}
	if gotAcquiring.Status != streamclips.StatusFailed || gotAcquiring.FailureReason != interruptedStreamAcquire {
		t.Fatalf("acquiring parent after incomplete audit = status %q reason %q, want failed/%q", gotAcquiring.Status, gotAcquiring.FailureReason, interruptedStreamAcquire)
	}
}

func TestReconcileInterruptedWorkContinuesAfterCategoryFailure(t *testing.T) {
	ctx := context.Background()
	base := newMemoryJobRepository()
	failedDemo := seedJob(t, base, job.StatusQueued)
	repairedDemo := seedJob(t, base, job.StatusQueued)
	repo := startupFailingJobRepository{
		orchestratorJobRepository: base,
		failID:                    failedDemo.ID,
	}
	streamRepo := newMemoryStreamJobRepository()
	repairedStream := createStartupStreamJob(t, streamRepo, streamclips.StatusAcquiring)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewLocal: %v", err)
	}

	result, err := reconcileInterruptedWork(ctx, repo, streamRepo, store, nil)
	if err == nil {
		t.Fatal("reconcileInterruptedWork error = nil, want contextual startup failure")
	}
	for _, want := range []string{"demo jobs", failedDemo.ID.String(), "injected startup update failure"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("reconcileInterruptedWork error %q does not contain %q", err, want)
		}
	}
	if result.DemoJobs != 1 || result.StreamJobs != 1 {
		t.Fatalf("reconciled counts = demo %d stream %d, want 1/1", result.DemoJobs, result.StreamJobs)
	}

	gotDemo, err := base.Get(ctx, repairedDemo.ID)
	if err != nil {
		t.Fatalf("Get repaired demo: %v", err)
	}
	if gotDemo.Status != job.StatusFailed {
		t.Fatalf("repaired demo status = %s, want failed", gotDemo.Status)
	}
	gotStream, err := streamRepo.Get(ctx, repairedStream.ID)
	if err != nil {
		t.Fatalf("Get repaired stream: %v", err)
	}
	if gotStream.Status != streamclips.StatusFailed {
		t.Fatalf("repaired stream status = %s, want failed", gotStream.Status)
	}
}

func createStartupStreamJob(t *testing.T, repo interface {
	Create(context.Context, *streamclips.Job) error
}, status streamclips.Status) streamclips.Job {
	t.Helper()
	j := streamclips.Job{
		Status:       status,
		SourcePath:   "streams/" + string(status) + ".mp4",
		SourceSHA256: "sha-" + string(status),
		Probe:        streamclips.SourceProbe{Width: 1920, Height: 1080},
	}
	if err := repo.Create(context.Background(), &j); err != nil {
		t.Fatalf("Create stream job %s: %v", status, err)
	}
	return j
}

func startupInterruptedObsCount(rec *obs.Recorder, stage string) int64 {
	var count int64
	for _, metric := range rec.Snapshot() {
		if metric.Name == "fragforge_errors_total" && metric.Labels["stage"] == stage && metric.Labels["class"] == interruptedClass {
			count += metric.Value
		}
	}
	return count
}
