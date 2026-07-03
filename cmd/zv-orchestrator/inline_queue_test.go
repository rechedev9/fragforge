package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/tasks"
)

// Regression for the "cs2.exe is already running" failure: record:demo tasks
// must run on a dedicated serial lane, one at a time, whatever the shared
// concurrency is, and without starving other task types.
func TestInlineQueueRunsCaptureTasksSequentially(t *testing.T) {
	var mu sync.Mutex
	running, overlaps, doneCount := 0, 0, 0
	allDone := make(chan struct{})
	const captures = 3

	handlers := map[string]taskHandler{
		tasks.TypeRecordDemo: func(context.Context, *asynq.Task) error {
			mu.Lock()
			running++
			if running > 1 {
				overlaps++
			}
			mu.Unlock()
			time.Sleep(30 * time.Millisecond)
			mu.Lock()
			running--
			doneCount++
			if doneCount == captures {
				close(allDone)
			}
			mu.Unlock()
			return nil
		},
	}
	q := newInlineQueue(handlers, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	for i := 0; i < captures; i++ {
		task := asynq.NewTask(tasks.TypeRecordDemo, []byte{byte('a' + i)})
		if _, err := q.Enqueue(task); err != nil {
			t.Fatalf("Enqueue capture %d error = %v", i, err)
		}
	}

	select {
	case <-allDone:
	case <-time.After(5 * time.Second):
		t.Fatal("captures did not finish in time")
	}
	mu.Lock()
	defer mu.Unlock()
	if overlaps != 0 {
		t.Fatalf("capture overlaps = %d, want 0 (captures must run sequentially)", overlaps)
	}
}

// A running capture must not block the shared lanes: parse tasks keep flowing
// while record:demo occupies its own goroutine.
func TestInlineQueueCaptureDoesNotStarveOtherTasks(t *testing.T) {
	captureStarted := make(chan struct{})
	releaseCapture := make(chan struct{})
	parseDone := make(chan struct{})

	handlers := map[string]taskHandler{
		tasks.TypeRecordDemo: func(context.Context, *asynq.Task) error {
			close(captureStarted)
			<-releaseCapture
			return nil
		},
		tasks.TypeParseDemo: func(context.Context, *asynq.Task) error {
			close(parseDone)
			return nil
		},
	}
	q := newInlineQueue(handlers, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	if _, err := q.Enqueue(asynq.NewTask(tasks.TypeRecordDemo, []byte("r"))); err != nil {
		t.Fatalf("Enqueue record error = %v", err)
	}
	select {
	case <-captureStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("capture never started")
	}
	if _, err := q.Enqueue(asynq.NewTask(tasks.TypeParseDemo, []byte("p"))); err != nil {
		t.Fatalf("Enqueue parse error = %v", err)
	}
	select {
	case <-parseDone:
	case <-time.After(5 * time.Second):
		t.Fatal("parse task starved behind a running capture")
	}
	close(releaseCapture)
}

// Enqueue must honour asynq.Unique in inline mode: while an identical task is
// queued or running, a duplicate returns asynq.ErrDuplicateTask (the web
// reconcile loop re-POSTs record every tick and used to pile up duplicates);
// once the task finishes the same payload may be enqueued again.
func TestInlineQueueUniqueDeduplicates(t *testing.T) {
	started := make(chan struct{}, 8)
	release := make(chan struct{})

	handlers := map[string]taskHandler{
		tasks.TypeRecordDemo: func(context.Context, *asynq.Task) error {
			started <- struct{}{}
			<-release
			return nil
		},
	}
	q := newInlineQueue(handlers, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	task := asynq.NewTask(tasks.TypeRecordDemo, []byte("same-payload"))
	if _, err := q.Enqueue(task, asynq.Unique(time.Minute)); err != nil {
		t.Fatalf("first Enqueue error = %v", err)
	}
	<-started

	// While the identical task is running, a duplicate is rejected.
	if _, err := q.Enqueue(task, asynq.Unique(time.Minute)); !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("duplicate Enqueue error = %v, want asynq.ErrDuplicateTask", err)
	}
	// A different payload is not a duplicate.
	other := asynq.NewTask(tasks.TypeRecordDemo, []byte("other-payload"))
	if _, err := q.Enqueue(other, asynq.Unique(time.Minute)); err != nil {
		t.Fatalf("other-payload Enqueue error = %v", err)
	}

	// Once the task finishes, the same payload becomes enqueueable again. The
	// unique key is released just after the handler returns, so poll briefly.
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := q.Enqueue(task, asynq.Unique(time.Minute))
		if err == nil {
			break
		}
		if !errors.Is(err, asynq.ErrDuplicateTask) {
			t.Fatalf("re-Enqueue after completion error = %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("unique key was never released after the task finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Tasks enqueued without asynq.Unique are never deduplicated.
func TestInlineQueueWithoutUniqueDoesNotDeduplicate(t *testing.T) {
	var mu sync.Mutex
	count := 0
	done := make(chan struct{})
	handlers := map[string]taskHandler{
		tasks.TypeParseDemo: func(context.Context, *asynq.Task) error {
			mu.Lock()
			count++
			if count == 2 {
				close(done)
			}
			mu.Unlock()
			return nil
		},
	}
	q := newInlineQueue(handlers, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	q.Start(ctx)

	task := asynq.NewTask(tasks.TypeParseDemo, []byte("same"))
	for i := 0; i < 2; i++ {
		if _, err := q.Enqueue(task); err != nil {
			t.Fatalf("Enqueue %d error = %v", i, err)
		}
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("both identical non-unique tasks should run")
	}
}
