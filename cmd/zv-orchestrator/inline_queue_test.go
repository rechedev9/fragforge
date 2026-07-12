package main

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

func startTestInlineQueue(t *testing.T, handlers map[string]taskHandler, concurrency int) *inlineQueue {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	queue := newInlineQueue(handlers, concurrency)
	queue.Start(ctx)
	t.Cleanup(func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		queue.Shutdown(shutdownCtx)
	})
	return queue
}

func TestInlineQueueRejectsDuplicateUntilSuccessfulTaskFinishes(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	queue := startTestInlineQueue(t, map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			started <- struct{}{}
			<-release
			return nil
		},
	}, 1)
	task := asynq.NewTask("render", []byte(`{"job_id":"one"}`))

	first, err := queue.Enqueue(task, asynq.Unique(time.Minute))
	if err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	<-started
	if _, err := queue.Enqueue(task, asynq.Unique(time.Minute)); !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("duplicate Enqueue() error = %v, want ErrDuplicateTask", err)
	}

	close(release)
	var second *asynq.TaskInfo
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		second, err = queue.Enqueue(task, asynq.Unique(time.Minute))
		if err == nil {
			break
		}
		if !errors.Is(err, asynq.ErrDuplicateTask) {
			t.Fatalf("Enqueue() after success error = %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	if err != nil {
		t.Fatalf("Enqueue() remained locked after success: %v", err)
	}
	if first.ID == second.ID {
		t.Fatalf("task IDs = %q and %q, want distinct IDs", first.ID, second.ID)
	}
	if first.Queue != "inline" || second.Queue != "inline" {
		t.Fatalf("queues = %q, %q, want inline", first.Queue, second.Queue)
	}
}

func TestInlineQueueAllowsOnlyOneConcurrentUniqueEnqueue(t *testing.T) {
	release := make(chan struct{})
	queue := startTestInlineQueue(t, map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			<-release
			return nil
		},
	}, 1)
	task := asynq.NewTask("render", []byte("same payload"))

	const callers = 32
	var accepted atomic.Int32
	var duplicates atomic.Int32
	var unexpected atomic.Int32
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := queue.Enqueue(task, asynq.Unique(time.Minute))
			switch {
			case err == nil:
				accepted.Add(1)
			case errors.Is(err, asynq.ErrDuplicateTask):
				duplicates.Add(1)
			default:
				unexpected.Add(1)
			}
		}()
	}
	wg.Wait()
	close(release)

	if got := accepted.Load(); got != 1 {
		t.Fatalf("accepted = %d, want 1", got)
	}
	if got := duplicates.Load(); got != callers-1 {
		t.Fatalf("duplicates = %d, want %d", got, callers-1)
	}
	if got := unexpected.Load(); got != 0 {
		t.Fatalf("unexpected errors = %d, want 0", got)
	}
}

func TestInlineQueueReleasesFailedTaskLockForManualRetry(t *testing.T) {
	handled := make(chan struct{}, 2)
	queue := startTestInlineQueue(t, map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			handled <- struct{}{}
			return errors.New("render failed")
		},
	}, 1)
	task := asynq.NewTask("render", []byte("same payload"))

	if _, err := queue.Enqueue(task, asynq.Unique(time.Hour)); err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	<-handled
	deadline := time.Now().Add(time.Second)
	for {
		_, err := queue.Enqueue(task, asynq.Unique(time.Hour))
		if err == nil {
			break
		}
		if !errors.Is(err, asynq.ErrDuplicateTask) {
			t.Fatalf("manual retry Enqueue() error = %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("manual retry remained locked after failure: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	<-handled
}

func TestInlineQueueUniqueLockExpiresWhileTaskIsActive(t *testing.T) {
	started := make(chan int, 2)
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	probeHandled := make(chan struct{}, 1)
	var calls atomic.Int32
	queue := startTestInlineQueue(t, map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			call := int(calls.Add(1))
			started <- call
			switch call {
			case 1:
				<-firstRelease
			case 2:
				<-secondRelease
			}
			return nil
		},
		"probe": func(context.Context, *asynq.Task) error {
			probeHandled <- struct{}{}
			return nil
		},
	}, 2)
	now := time.Unix(1_700_000_000, 0)
	queue.now = func() time.Time { return now }
	task := asynq.NewTask("render", []byte("same payload"))

	if _, err := queue.Enqueue(task, asynq.Unique(2*time.Second)); err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	if got := <-started; got != 1 {
		t.Fatalf("first handler call = %d, want 1", got)
	}
	if _, err := queue.Enqueue(task, asynq.Unique(2*time.Second)); !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("duplicate Enqueue() error = %v, want ErrDuplicateTask", err)
	}
	now = now.Add(3 * time.Second)
	if _, err := queue.Enqueue(task, asynq.Unique(2*time.Second)); err != nil {
		t.Fatalf("Enqueue() after TTL error = %v", err)
	}
	if got := <-started; got != 2 {
		t.Fatalf("second handler call = %d, want 2", got)
	}

	close(firstRelease)
	if _, err := queue.Enqueue(asynq.NewTask("probe", nil)); err != nil {
		t.Fatalf("probe Enqueue() error = %v", err)
	}
	<-probeHandled // proves the first worker completed its old-lock release
	if _, err := queue.Enqueue(task, asynq.Unique(2*time.Second)); !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("duplicate after old task completion error = %v, want ErrDuplicateTask", err)
	}
	close(secondRelease)
}

func TestInlineQueueUniquenessIncludesQueueTypeAndPayload(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	handler := func(context.Context, *asynq.Task) error {
		started <- struct{}{}
		<-release
		return nil
	}
	queue := startTestInlineQueue(t, map[string]taskHandler{
		"render":  handler,
		"caption": handler,
	}, 4)

	cases := []struct {
		task  *asynq.Task
		queue string
	}{
		{task: asynq.NewTask("render", []byte("one")), queue: "critical"},
		{task: asynq.NewTask("render", []byte("one")), queue: "default"},
		{task: asynq.NewTask("render", []byte("two")), queue: "critical"},
		{task: asynq.NewTask("caption", []byte("one")), queue: "critical"},
	}
	for _, tc := range cases {
		info, err := queue.Enqueue(tc.task, asynq.Queue(tc.queue), asynq.Unique(time.Minute))
		if err != nil {
			t.Fatalf("Enqueue(%s, %s) error = %v", tc.task.Type(), tc.queue, err)
		}
		if info.Queue != tc.queue {
			t.Fatalf("TaskInfo.Queue = %q, want %q", info.Queue, tc.queue)
		}
	}
	for range cases {
		<-started
	}
	if _, err := queue.Enqueue(
		asynq.NewTask("render", []byte("one")),
		asynq.Queue("critical"),
		asynq.Unique(time.Minute),
	); !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("exact duplicate error = %v, want ErrDuplicateTask", err)
	}
	close(release)
}

func TestInlineQueueValidatesSupportedOptions(t *testing.T) {
	queue := startTestInlineQueue(t, map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error { return nil },
	}, 1)
	task := asynq.NewTask("render", nil)

	if _, err := queue.Enqueue(task, asynq.Unique(time.Second-time.Nanosecond)); err == nil {
		t.Fatal("Enqueue() with short Unique TTL error = nil")
	}
	if _, err := queue.Enqueue(task, asynq.Queue(" ")); err == nil {
		t.Fatal("Enqueue() with blank queue error = nil")
	}
	if _, err := queue.Enqueue(task, asynq.MaxRetry(0)); err != nil {
		t.Fatalf("Enqueue() with MaxRetry(0) error = %v", err)
	}
	unsupported := []asynq.Option{
		asynq.MaxRetry(1),
		asynq.Timeout(time.Minute),
		asynq.Deadline(time.Now().Add(time.Minute)),
		asynq.ProcessIn(time.Minute),
		asynq.TaskID("fixed"),
		asynq.Retention(time.Minute),
		asynq.Group("batch"),
	}
	for _, opt := range unsupported {
		if _, err := queue.Enqueue(task, opt); err == nil {
			t.Fatalf("Enqueue() with unsupported %s error = nil", opt.String())
		}
	}
}

func TestInlineTaskQueueBoundsPendingWorkWithoutBlocking(t *testing.T) {
	queue := newInlineTaskQueue(2)
	ctx := context.Background()
	if err := queue.push(ctx, inlineTask{id: "one"}); err != nil {
		t.Fatalf("first push() error = %v", err)
	}
	if err := queue.push(ctx, inlineTask{id: "two"}); err != nil {
		t.Fatalf("second push() error = %v", err)
	}
	if err := queue.push(ctx, inlineTask{id: "three"}); !errors.Is(err, errInlineQueueFull) {
		t.Fatalf("full push() error = %v, want errInlineQueueFull", err)
	}
	if task, ok := queue.pop(); !ok || task.id != "one" {
		t.Fatalf("pop() = (%q, %t), want (one, true)", task.id, ok)
	}
	if err := queue.push(ctx, inlineTask{id: "three"}); err != nil {
		t.Fatalf("push() after pop error = %v", err)
	}
}

func TestInlineQueueUniqueAcceptanceIsAtomicWithQueueCapacity(t *testing.T) {
	queue := newInlineQueue(map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error { return nil },
	}, 1)
	queue.ctx = context.Background()
	queue.tasks.mu.Lock()
	queue.tasks.max = 1
	queue.tasks.tasks = append(queue.tasks.tasks, inlineTask{id: "filler"})

	results := make(chan error, 2)
	enqueue := func() {
		_, err := queue.Enqueue(
			asynq.NewTask("render", []byte("same")),
			asynq.Unique(time.Minute),
		)
		results <- err
	}
	go enqueue()

	deadline := time.Now().Add(time.Second)
	for queue.uniqueMu.TryLock() {
		queue.uniqueMu.Unlock()
		if time.Now().After(deadline) {
			queue.tasks.mu.Unlock()
			t.Fatal("first enqueue did not reach the atomic acceptance section")
		}
		runtime.Gosched()
	}
	// The first enqueue now owns uniqueMu and is waiting for the task-queue
	// lock. A second identical enqueue must wait for that acceptance decision;
	// it cannot observe a lease for work that may still fail to enter the FIFO.
	go enqueue()
	queue.tasks.mu.Unlock()

	for range 2 {
		if err := <-results; !errors.Is(err, errInlineQueueFull) {
			t.Fatalf("Enqueue() error = %v, want errInlineQueueFull", err)
		}
	}
	queue.uniqueMu.Lock()
	locks := len(queue.uniqueLocks)
	queue.uniqueMu.Unlock()
	if locks != 0 {
		t.Fatalf("unique locks = %d, want 0", locks)
	}
	queue.closePending()
}

func TestInlineQueueCloseReleasesDiscardedUniqueLeases(t *testing.T) {
	ctx := context.Background()
	releaseActive := make(chan struct{})
	activeStarted := make(chan struct{}, 1)
	queue := newInlineQueue(map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			activeStarted <- struct{}{}
			<-releaseActive
			return nil
		},
	}, 1)
	queue.Start(ctx)

	if _, err := queue.Enqueue(asynq.NewTask("render", []byte("active"))); err != nil {
		t.Fatalf("active Enqueue() error = %v", err)
	}
	<-activeStarted
	if _, err := queue.Enqueue(
		asynq.NewTask("render", []byte("queued")),
		asynq.Unique(time.Minute),
	); err != nil {
		t.Fatalf("queued Enqueue() error = %v", err)
	}
	queue.uniqueMu.Lock()
	locksBefore := len(queue.uniqueLocks)
	queue.uniqueMu.Unlock()
	if locksBefore != 1 {
		t.Fatalf("unique locks before close = %d, want 1", locksBefore)
	}

	queue.closePending()
	queue.uniqueMu.Lock()
	locksAfter := len(queue.uniqueLocks)
	queue.uniqueMu.Unlock()
	if locksAfter != 0 {
		t.Fatalf("unique locks after close = %d, want 0", locksAfter)
	}

	close(releaseActive)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	queue.Shutdown(shutdownCtx)
}

func TestInlineQueueReleasesLockWhenContextIsCanceledBeforePush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	queue := newInlineQueue(map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error { return nil },
	}, 1)
	queue.Start(ctx)
	cancel()
	if _, err := queue.Enqueue(
		asynq.NewTask("render", []byte("unique")),
		asynq.Unique(time.Minute),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Enqueue() error = %v, want context.Canceled", err)
	}
	queue.uniqueMu.Lock()
	locks := len(queue.uniqueLocks)
	queue.uniqueMu.Unlock()
	if locks != 0 {
		t.Fatalf("unique locks = %d, want 0", locks)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	queue.Shutdown(shutdownCtx)
}

func TestInlineQueueWorkerEnqueueCannotDeadlockBehindQueuedTasks(t *testing.T) {
	parentsStarted := make(chan struct{}, 2)
	releaseParents := make(chan struct{})
	parentResults := make(chan error, 2)
	childrenHandled := make(chan struct{}, 6)
	var queue *inlineQueue
	handlers := map[string]taskHandler{
		"parent": func(context.Context, *asynq.Task) error {
			parentsStarted <- struct{}{}
			<-releaseParents
			_, err := queue.Enqueue(asynq.NewTask("child", []byte("chained")))
			parentResults <- err
			return err
		},
		"child": func(context.Context, *asynq.Task) error {
			childrenHandled <- struct{}{}
			return nil
		},
	}
	queue = startTestInlineQueue(t, handlers, 2)

	for range 2 {
		if _, err := queue.Enqueue(asynq.NewTask("parent", nil)); err != nil {
			t.Fatalf("parent Enqueue() error = %v", err)
		}
	}
	for range 2 {
		<-parentsStarted
	}
	// This filled the old channel (capacity = 2 * concurrency). Both active
	// parents then blocked trying to enqueue their chained child, leaving no
	// worker able to drain the channel.
	for i := range 4 {
		if _, err := queue.Enqueue(asynq.NewTask("child", []byte{byte(i)})); err != nil {
			t.Fatalf("filler child %d Enqueue() error = %v", i, err)
		}
	}
	close(releaseParents)

	deadline := time.After(time.Second)
	for range 2 {
		select {
		case err := <-parentResults:
			if err != nil {
				t.Fatalf("chained Enqueue() error = %v", err)
			}
		case <-deadline:
			t.Fatal("workers deadlocked while enqueueing chained tasks")
		}
	}
	for range 6 {
		select {
		case <-childrenHandled:
		case <-deadline:
			t.Fatal("queued child tasks did not drain")
		}
	}
}
