package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/tasks"
)

type captureEnqueuer struct {
	enqueued []*asynq.Task
}

func (c *captureEnqueuer) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	c.enqueued = append(c.enqueued, task)
	return &asynq.TaskInfo{ID: "test", Type: task.Type()}, nil
}

func (c *captureEnqueuer) types() []string {
	out := make([]string, 0, len(c.enqueued))
	for _, t := range c.enqueued {
		out = append(out, t.Type())
	}
	return out
}

func seedJob(t *testing.T, repo *memoryJobRepository, status job.Status) uuid.UUID {
	t.Helper()
	j := job.Job{Status: job.StatusQueued, DemoPath: "demos/test.dem"}
	if err := repo.Create(context.Background(), &j); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(context.Background(), j.ID, status, ""); err != nil {
		t.Fatal(err)
	}
	return j.ID
}

func mustStatus(t *testing.T, repo *memoryJobRepository, id uuid.UUID) job.Status {
	t.Helper()
	j, err := repo.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return j.Status
}

// A restart orphans inline tasks: jobs stuck mid-stage must be re-enqueued
// (pure stages) or rolled back to their last durable state (capture, which is
// never relaunched unattended) so the pipeline can advance again.
func TestRecoverInterruptedJobs(t *testing.T) {
	repo := newMemoryJobRepository()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	queue := &captureEnqueuer{}

	scanning := seedJob(t, repo, job.StatusScanning)
	parsing := seedJob(t, repo, job.StatusParsing)
	recording := seedJob(t, repo, job.StatusRecording)
	composing := seedJob(t, repo, job.StatusComposing)
	parsed := seedJob(t, repo, job.StatusParsed)
	failed := seedJob(t, repo, job.StatusFailed)

	recoverInterruptedJobs(context.Background(), repo, store, queue)

	if got, want := len(queue.enqueued), 2; got != want {
		t.Fatalf("enqueued tasks = %d (%v), want %d", got, queue.types(), want)
	}
	seen := map[string]bool{}
	for _, task := range queue.enqueued {
		seen[task.Type()] = true
	}
	if !seen[tasks.TypeScanRoster] || !seen[tasks.TypeParseDemo] {
		t.Fatalf("enqueued types = %v, want scan:roster and parse:demo", queue.types())
	}

	if got := mustStatus(t, repo, scanning); got != job.StatusScanning {
		t.Fatalf("scanning job status = %s, want scanning (task re-enqueued, not reset)", got)
	}
	if got := mustStatus(t, repo, parsing); got != job.StatusParsing {
		t.Fatalf("parsing job status = %s, want parsing (task re-enqueued, not reset)", got)
	}
	if got := mustStatus(t, repo, recording); got != job.StatusParsed {
		t.Fatalf("recording job status = %s, want parsed (capture is never relaunched unattended)", got)
	}
	if got := mustStatus(t, repo, composing); got != job.StatusRecorded {
		t.Fatalf("composing job status = %s, want recorded", got)
	}
	if got := mustStatus(t, repo, parsed); got != job.StatusParsed {
		t.Fatalf("parsed job status = %s, want parsed (untouched)", got)
	}
	if got := mustStatus(t, repo, failed); got != job.StatusFailed {
		t.Fatalf("failed job status = %s, want failed (untouched)", got)
	}
}

// A render-variant state stuck queued/rendering is re-enqueued when the job's
// generate intent can rebuild the task, and marked failed (retryable in the
// UI) when it cannot.
func TestRecoverInterruptedRenderStates(t *testing.T) {
	loadout := renderplan.LoadoutCatalog()[0]

	// Each subtest gets its own repo and store: recovery walks every job, so a
	// leftover queued state from one case would leak enqueues into the next.
	newFixture := func(t *testing.T) (*memoryJobRepository, storage.Storage) {
		t.Helper()
		repo := newMemoryJobRepository()
		store, err := storage.NewLocal(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return repo, store
	}

	putRenderState := func(t *testing.T, store storage.Storage, id uuid.UUID, status string) string {
		t.Helper()
		key, err := renderplan.RenderVariantStateKey(id, loadout.Variant)
		if err != nil {
			t.Fatal(err)
		}
		state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
			JobID:   id,
			Loadout: loadout,
			Status:  status,
		})
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Put(key, bytes.NewReader(b)); err != nil {
			t.Fatal(err)
		}
		return key
	}

	t.Run("with generate intent the render task is re-enqueued", func(t *testing.T) {
		queue := &captureEnqueuer{}
		repo, store := newFixture(t)
		id := seedJob(t, repo, job.StatusRecorded)
		key := putRenderState(t, store, id, renderplan.RenderVariantStatusQueued)
		intent := renderplan.GenerateIntent{Variant: loadout.Variant, Edit: renderplan.DefaultEditRequest()}
		b, err := json.Marshal(intent)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Put(artifacts.GenerateIntentKey(id), bytes.NewReader(b)); err != nil {
			t.Fatal(err)
		}

		recoverInterruptedJobs(context.Background(), repo, store, queue)

		if got, want := queue.types(), []string{tasks.TypeRenderVariant}; len(got) != 1 || got[0] != want[0] {
			t.Fatalf("enqueued = %v, want %v", got, want)
		}
		state, ok := readRenderState(store, key)
		if !ok || state.Status != renderplan.RenderVariantStatusQueued {
			t.Fatalf("render state = %q (ok=%v), want still queued", state.Status, ok)
		}
	})

	t.Run("without intent the state is marked failed for retry", func(t *testing.T) {
		queue := &captureEnqueuer{}
		repo, store := newFixture(t)
		id := seedJob(t, repo, job.StatusRecorded)
		key := putRenderState(t, store, id, renderplan.RenderVariantStatusRendering)

		recoverInterruptedJobs(context.Background(), repo, store, queue)

		if len(queue.enqueued) != 0 {
			t.Fatalf("enqueued = %v, want none", queue.types())
		}
		state, ok := readRenderState(store, key)
		if !ok {
			t.Fatal("render state missing after recovery")
		}
		if state.Status != renderplan.RenderVariantStatusFailed {
			t.Fatalf("render state = %q, want failed", state.Status)
		}
		if state.Error == "" {
			t.Fatal("failed render state should carry an actionable error")
		}
	})

	t.Run("terminal render states are untouched", func(t *testing.T) {
		queue := &captureEnqueuer{}
		repo, store := newFixture(t)
		id := seedJob(t, repo, job.StatusRecorded)
		key := putRenderState(t, store, id, renderplan.RenderVariantStatusReady)

		recoverInterruptedJobs(context.Background(), repo, store, queue)

		if len(queue.enqueued) != 0 {
			t.Fatalf("enqueued = %v, want none", queue.types())
		}
		state, ok := readRenderState(store, key)
		if !ok || state.Status != renderplan.RenderVariantStatusReady {
			t.Fatalf("render state = %q (ok=%v), want ready untouched", state.Status, ok)
		}
	})
}
