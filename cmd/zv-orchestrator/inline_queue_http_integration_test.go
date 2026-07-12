package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/httpapi"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

func TestInlineQueueShutdownCompensatesAcceptedParseThroughHTTP(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewLocal: %v", err)
	}
	j := job.Job{
		Status:        job.StatusScanned,
		DemoPath:      "demos/restart.dem",
		DemoSHA256:    "sha-restart",
		TargetSteamID: "76561198000000000",
		Rules:         rules.Default(),
	}
	if err := repo.Create(context.Background(), &j); err != nil {
		t.Fatalf("Create job: %v", err)
	}

	queue, cancelWorkers := startBlockedInlineQueue(t, tasks.TypeParseDemo)
	h := httpapi.NewHandlers(repo, store, queue)
	routes := httpapi.Routes(h)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/jobs/"+j.ID.String()+"/parse",
		strings.NewReader(`{"target_steamid":"76561198000000000"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	routes.ServeHTTP(rw, req)
	if rw.Code != http.StatusAccepted {
		cancelAndShutdownInlineQueue(t, queue, cancelWorkers)
		t.Fatalf("StartParse status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}

	cancelAndShutdownInlineQueue(t, queue, cancelWorkers)
	got, err := repo.Get(context.Background(), j.ID)
	if err != nil {
		t.Fatalf("Get job after shutdown: %v", err)
	}
	if got.Status != job.StatusFailed || !strings.Contains(got.FailureReason, errInlineQueueDiscarded.Error()) {
		t.Fatalf("job after shutdown = status %s reason %q, want failed discard reason", got.Status, got.FailureReason)
	}
}

func TestInlineQueueShutdownCompensatesAcceptedStreamAcquireThroughHTTP(t *testing.T) {
	jobRepo, err := newSQLiteJobRepository(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("newSQLiteJobRepository: %v", err)
	}
	t.Cleanup(func() { _ = jobRepo.Close() })
	streamRepo, err := newSQLiteStreamJobRepository(jobRepo.db)
	if err != nil {
		t.Fatalf("newSQLiteStreamJobRepository: %v", err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewLocal: %v", err)
	}

	queue, cancelWorkers := startBlockedInlineQueue(t, tasks.TypeStreamAcquire)
	h := httpapi.NewHandlers(
		jobRepo,
		store,
		queue,
		httpapi.WithStreamRepository(streamRepo),
		httpapi.WithCapabilities(httpapi.Capabilities{YtdlpEnabled: true}),
	)
	routes := httpapi.Routes(h)
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/stream-jobs",
		strings.NewReader(`{"source_url":"https://clips.twitch.tv/SomeSlug"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	routes.ServeHTTP(rw, req)
	if rw.Code != http.StatusAccepted {
		cancelAndShutdownInlineQueue(t, queue, cancelWorkers)
		t.Fatalf("create stream status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	var response struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &response); err != nil {
		cancelAndShutdownInlineQueue(t, queue, cancelWorkers)
		t.Fatalf("decode stream response: %v", err)
	}

	cancelAndShutdownInlineQueue(t, queue, cancelWorkers)
	got, err := streamRepo.Get(context.Background(), response.ID)
	if err != nil {
		t.Fatalf("Get stream after shutdown: %v", err)
	}
	if got.Status != streamclips.StatusFailed || !strings.Contains(got.FailureReason, errInlineQueueDiscarded.Error()) {
		t.Fatalf("stream after shutdown = status %q reason %q, want failed discard reason", got.Status, got.FailureReason)
	}
}

func startBlockedInlineQueue(t *testing.T, taskType string) (*inlineQueue, context.CancelFunc) {
	t.Helper()
	started := make(chan struct{})
	var startedOnce sync.Once
	queue := newInlineQueue(map[string]taskHandler{
		taskType: func(ctx context.Context, _ *asynq.Task) error {
			startedOnce.Do(func() { close(started) })
			<-ctx.Done()
			return ctx.Err()
		},
	}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	queue.Start(ctx)
	if _, err := queue.Enqueue(asynq.NewTask(taskType, []byte("occupy worker"))); err != nil {
		cancelAndShutdownInlineQueue(t, queue, cancel)
		t.Fatalf("enqueue blocking task: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		cancelAndShutdownInlineQueue(t, queue, cancel)
		t.Fatal("blocking task did not start")
	}
	return queue, cancel
}

func cancelAndShutdownInlineQueue(t *testing.T, queue *inlineQueue, cancel context.CancelFunc) {
	t.Helper()
	cancel()
	ctx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	if err := queue.Shutdown(ctx); err != nil {
		t.Fatalf("inline queue shutdown: %v", err)
	}
}
