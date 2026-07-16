package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	tasktypes "github.com/rechedev9/fragforge/internal/tasks"
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

func TestDefaultInlineTaskPolicy(t *testing.T) {
	tests := []struct {
		name        string
		taskType    string
		wantTimeout time.Duration
		wantRetries int
	}{
		{name: "parse demo", taskType: tasktypes.TypeParseDemo, wantTimeout: 15 * time.Minute, wantRetries: 1},
		{name: "scan roster", taskType: tasktypes.TypeScanRoster, wantTimeout: 15 * time.Minute, wantRetries: 1},
		{name: "record demo", taskType: tasktypes.TypeRecordDemo},
		{name: "compose final", taskType: tasktypes.TypeComposeFinal},
		{name: "render variant", taskType: tasktypes.TypeRenderVariant},
		{name: "codex agent", taskType: tasktypes.TypeCodexAgent},
		{name: "render stream clip", taskType: tasktypes.TypeRenderStreamClip},
		{name: "stream acquire", taskType: tasktypes.TypeStreamAcquire},
		{name: "unknown", taskType: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultInlineTaskPolicy(tt.taskType)
			if got.attemptTimeout != tt.wantTimeout {
				t.Errorf("attemptTimeout = %v, want %v", got.attemptTimeout, tt.wantTimeout)
			}
			if got.maxRetries != tt.wantRetries {
				t.Errorf("maxRetries = %d, want %d", got.maxRetries, tt.wantRetries)
			}
		})
	}
}

func TestInlineQueueReportsTaskTypePolicy(t *testing.T) {
	handled := make(chan struct{}, 1)
	queue := startTestInlineQueue(t, map[string]taskHandler{
		tasktypes.TypeParseDemo: func(context.Context, *asynq.Task) error {
			handled <- struct{}{}
			return nil
		},
	}, 1)

	info, err := queue.Enqueue(asynq.NewTask(tasktypes.TypeParseDemo, nil), asynq.MaxRetry(1))
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if info.MaxRetry != 1 {
		t.Errorf("TaskInfo.MaxRetry = %d, want 1", info.MaxRetry)
	}
	if info.Timeout != 15*time.Minute {
		t.Errorf("TaskInfo.Timeout = %v, want 15m", info.Timeout)
	}
	<-handled
}

func TestInlineQueueRetriesPureTaskOnceAfterFailure(t *testing.T) {
	queue := newInlineQueue(nil, 1)
	var calls int
	queued := inlineTask{
		task: asynq.NewTask(tasktypes.TypeParseDemo, nil),
		policy: inlineTaskPolicy{
			attemptTimeout: time.Minute,
			maxRetries:     1,
		},
	}
	err, _ := queue.handle(context.Background(), queued, func(context.Context, *asynq.Task) error {
		calls++
		if calls == 1 {
			return errors.New("transient parse failure")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("handle() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("handler calls = %d, want 2", calls)
	}
}

func TestInlineQueueProvidesAttemptMetadataToHandlers(t *testing.T) {
	queue := newInlineQueue(nil, 1)
	queued := inlineTask{
		task: asynq.NewTask(tasktypes.TypeParseDemo, nil),
		policy: inlineTaskPolicy{
			attemptTimeout: time.Minute,
			maxRetries:     1,
		},
	}
	type attempt struct {
		retried  int
		maxRetry int
	}
	var got []attempt
	err, _ := queue.handle(context.Background(), queued, func(ctx context.Context, _ *asynq.Task) error {
		retried, maxRetry, ok := tasktypes.TaskAttempt(ctx)
		if !ok {
			t.Fatal("TaskAttempt metadata missing")
		}
		got = append(got, attempt{retried: retried, maxRetry: maxRetry})
		if retried == 0 {
			return errors.New("retry parse")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("handle() error = %v", err)
	}
	want := []attempt{{retried: 0, maxRetry: 1}, {retried: 1, maxRetry: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("attempt metadata = %#v, want %#v", got, want)
	}
}

func TestInlineQueueStopsAfterPureTaskRetryIsExhausted(t *testing.T) {
	queue := newInlineQueue(nil, 1)
	var calls int
	wantErr := errors.New("persistent parse failure")
	queued := inlineTask{
		task:   asynq.NewTask(tasktypes.TypeScanRoster, nil),
		policy: inlineTaskPolicy{maxRetries: 1},
	}
	err, _ := queue.handle(context.Background(), queued, func(context.Context, *asynq.Task) error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("handle() error = %v, want %v", err, wantErr)
	}
	if calls != 2 {
		t.Fatalf("handler calls = %d, want 2", calls)
	}
}

func TestInlineQueueAppliesDeadlineToEachPureTaskAttempt(t *testing.T) {
	queue := newInlineQueue(nil, 1)
	const attemptTimeout = 50 * time.Millisecond
	queued := inlineTask{
		task: asynq.NewTask(tasktypes.TypeParseDemo, nil),
		policy: inlineTaskPolicy{
			attemptTimeout: attemptTimeout,
			maxRetries:     1,
		},
	}
	started := time.Now()
	var calls int
	err, _ := queue.handle(context.Background(), queued, func(ctx context.Context, _ *asynq.Task) error {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("handler context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining > attemptTimeout {
			t.Fatalf("handler deadline remaining = %v, want (0, %v]", remaining, attemptTimeout)
		}
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("handle() error = %v, want context.DeadlineExceeded", err)
	}
	if calls != 2 {
		t.Fatalf("handler calls = %d, want 2", calls)
	}
	if elapsed := time.Since(started); elapsed < 2*attemptTimeout {
		t.Fatalf("handler elapsed = %v, want at least %v", elapsed, 2*attemptTimeout)
	}
}

func TestInlineQueueDoesNotRetryAfterParentCancellation(t *testing.T) {
	queue := newInlineQueue(nil, 1)
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	queued := inlineTask{
		task:   asynq.NewTask(tasktypes.TypeParseDemo, nil),
		policy: inlineTaskPolicy{maxRetries: 1},
	}
	err, handlerStarted := queue.handle(ctx, queued, func(context.Context, *asynq.Task) error {
		calls++
		cancel()
		return errors.New("parse interrupted")
	})
	if err == nil {
		t.Fatal("handle() error = nil")
	}
	if !handlerStarted {
		t.Fatal("handle() handlerStarted = false, want true")
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
}

func TestInlineQueueDoesNotRetryMediaTasks(t *testing.T) {
	queue := newInlineQueue(nil, 1)
	var calls int
	wantErr := errors.New("recording failed")
	queued := inlineTask{
		task:   asynq.NewTask(tasktypes.TypeRecordDemo, nil),
		policy: defaultInlineTaskPolicy(tasktypes.TypeRecordDemo),
	}
	err, _ := queue.handle(context.Background(), queued, func(context.Context, *asynq.Task) error {
		calls++
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("handle() error = %v, want %v", err, wantErr)
	}
	if calls != 1 {
		t.Fatalf("handler calls = %d, want 1", calls)
	}
}

func TestInlineQueueKeepsUniqueLeaseAcrossAutomaticRetry(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	retryStarted := make(chan struct{})
	releaseRetry := make(chan struct{})
	var calls atomic.Int32
	queue := startTestInlineQueue(t, map[string]taskHandler{
		tasktypes.TypeParseDemo: func(context.Context, *asynq.Task) error {
			switch calls.Add(1) {
			case 1:
				close(firstStarted)
				<-releaseFirst
				return errors.New("retry parse")
			case 2:
				close(retryStarted)
				<-releaseRetry
			}
			return nil
		},
	}, 1)
	task := asynq.NewTask(tasktypes.TypeParseDemo, []byte("same payload"))

	if _, err := queue.Enqueue(task, asynq.Unique(time.Minute)); err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	<-firstStarted
	if _, err := queue.Enqueue(task, asynq.Unique(time.Minute)); !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("duplicate during first attempt error = %v, want ErrDuplicateTask", err)
	}
	close(releaseFirst)
	<-retryStarted
	if _, err := queue.Enqueue(task, asynq.Unique(time.Minute)); !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("duplicate during retry error = %v, want ErrDuplicateTask", err)
	}
	close(releaseRetry)

	deadline := time.Now().Add(time.Second)
	for {
		_, err := queue.Enqueue(task, asynq.Unique(time.Minute))
		if err == nil {
			break
		}
		if !errors.Is(err, asynq.ErrDuplicateTask) {
			t.Fatalf("Enqueue() after retry success error = %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("unique lease remained after retry success: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestInlineQueueRunsAcceptedTransitionBeforeTaskIsVisible(t *testing.T) {
	transitionComplete := atomic.Bool{}
	handled := make(chan bool, 1)
	queue := startTestInlineQueue(t, map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			handled <- transitionComplete.Load()
			return nil
		},
	}, 1)

	_, err := queue.EnqueueWithTransition(asynq.NewTask("render", nil), func(decision error) error {
		if decision != nil {
			t.Fatalf("transition decision = %v, want accepted", decision)
		}
		transitionComplete.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("EnqueueWithTransition() error = %v", err)
	}
	if visibleAfterTransition := <-handled; !visibleAfterTransition {
		t.Fatal("handler observed task before accepted transition completed")
	}
}

func TestInlineQueueTransitionFailurePreventsTaskPublication(t *testing.T) {
	handled := make(chan struct{}, 1)
	queue := startTestInlineQueue(t, map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			handled <- struct{}{}
			return nil
		},
	}, 1)
	wantErr := errors.New("persist queued state")
	task := asynq.NewTask("render", []byte("same"))

	_, err := queue.EnqueueWithTransition(task, func(error) error {
		return wantErr
	}, asynq.Unique(time.Minute))
	if !errors.Is(err, wantErr) {
		t.Fatalf("EnqueueWithTransition() error = %v, want %v", err, wantErr)
	}
	select {
	case <-handled:
		t.Fatal("handler received task after accepted transition failed")
	case <-time.After(20 * time.Millisecond):
	}
	if _, err := queue.Enqueue(task, asynq.Unique(time.Minute)); err != nil {
		t.Fatalf("Enqueue() after transition failure error = %v", err)
	}
	<-handled
}

func TestInlineQueueDuplicateTransitionObservesActiveTerminalState(t *testing.T) {
	state := "queued"
	ready := make(chan struct{})
	release := make(chan struct{})
	queue := startTestInlineQueue(t, map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			state = "ready"
			close(ready)
			<-release
			return nil
		},
	}, 1)
	task := asynq.NewTask("render", []byte("same"))
	if _, err := queue.EnqueueWithTransition(task, func(decision error) error {
		if decision != nil {
			t.Fatalf("first transition decision = %v", decision)
		}
		state = "queued"
		return nil
	}, asynq.Unique(time.Minute)); err != nil {
		t.Fatalf("first EnqueueWithTransition() error = %v", err)
	}
	<-ready

	seen := ""
	_, err := queue.EnqueueWithTransition(task, func(decision error) error {
		if !errors.Is(decision, asynq.ErrDuplicateTask) {
			t.Fatalf("duplicate transition decision = %v", decision)
		}
		seen = state
		return nil
	}, asynq.Unique(time.Minute))
	if !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("duplicate EnqueueWithTransition() error = %v", err)
	}
	if seen != "ready" || state != "ready" {
		t.Fatalf("duplicate transition saw/state = %q/%q, want ready/ready", seen, state)
	}
	close(release)
}

func TestInlineQueueRunsFullTransitionBeforeReturning(t *testing.T) {
	queue := newInlineQueue(map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error { return nil },
	}, 1)
	queue.ctx = context.Background()
	queue.tasks.max = 1
	queue.tasks.tasks = append(queue.tasks.tasks, inlineTask{id: "filler"})
	transitionCalled := false

	_, err := queue.EnqueueWithTransition(asynq.NewTask("render", nil), func(decision error) error {
		if !errors.Is(decision, errInlineQueueFull) {
			t.Fatalf("transition decision = %v, want errInlineQueueFull", decision)
		}
		transitionCalled = true
		return nil
	}, asynq.Unique(time.Minute))
	if !errors.Is(err, errInlineQueueFull) {
		t.Fatalf("EnqueueWithTransition() error = %v, want errInlineQueueFull", err)
	}
	if !transitionCalled {
		t.Fatal("full transition was not called")
	}
	queue.closePending()
}

func TestInlineQueueRunsTransitionForPreAdmissionRejections(t *testing.T) {
	tests := []struct {
		name  string
		queue *inlineQueue
		task  *asynq.Task
		opts  []asynq.Option
	}{
		{
			name:  "nil task",
			queue: newInlineQueue(map[string]taskHandler{"render": func(context.Context, *asynq.Task) error { return nil }}, 1),
		},
		{
			name:  "missing handler",
			queue: newInlineQueue(map[string]taskHandler{}, 1),
			task:  asynq.NewTask("render", nil),
		},
		{
			name: "not started",
			queue: newInlineQueue(map[string]taskHandler{
				"render": func(context.Context, *asynq.Task) error { return nil },
			}, 1),
			task: asynq.NewTask("render", nil),
		},
		{
			name: "unsupported option",
			queue: newInlineQueue(map[string]taskHandler{
				"render": func(context.Context, *asynq.Task) error { return nil },
			}, 1),
			task: asynq.NewTask("render", nil),
			opts: []asynq.Option{asynq.Timeout(time.Minute)},
		},
		{
			name: "retry policy mismatch",
			queue: newInlineQueue(map[string]taskHandler{
				"render": func(context.Context, *asynq.Task) error { return nil },
			}, 1),
			task: asynq.NewTask("render", nil),
			opts: []asynq.Option{asynq.MaxRetry(1)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name != "not started" && tt.name != "nil task" {
				tt.queue.ctx = context.Background()
			}
			called := false
			_, err := tt.queue.EnqueueWithTransition(tt.task, func(decision error) error {
				if decision == nil {
					t.Fatal("transition decision = nil, want rejection")
				}
				called = true
				return nil
			}, tt.opts...)
			if err == nil {
				t.Fatal("EnqueueWithTransition() error = nil")
			}
			if !called {
				t.Fatal("rejection transition was not called")
			}
		})
	}
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

func TestInlineQueueUniqueIdentityIgnoresTaskHeaders(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	queue := startTestInlineQueue(t, map[string]taskHandler{
		tasktypes.TypeRecordDemo: func(context.Context, *asynq.Task) error {
			close(started)
			<-release
			return nil
		},
	}, 1)
	payload := []byte(`{"job_id":"same-capture"}`)
	first := asynq.NewTaskWithHeaders(tasktypes.TypeRecordDemo, payload, map[string]string{"intent": "first"})
	second := asynq.NewTaskWithHeaders(tasktypes.TypeRecordDemo, payload, map[string]string{"intent": "second"})

	if _, err := queue.Enqueue(first, asynq.Unique(time.Minute)); err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	<-started
	if _, err := queue.Enqueue(second, asynq.Unique(time.Minute)); !errors.Is(err, asynq.ErrDuplicateTask) {
		t.Fatalf("header-only duplicate error = %v, want ErrDuplicateTask", err)
	}
	close(release)
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
	if err := queue.push(ctx, inlineTask{id: "one"}, nil); err != nil {
		t.Fatalf("first push() error = %v", err)
	}
	if err := queue.push(ctx, inlineTask{id: "two"}, nil); err != nil {
		t.Fatalf("second push() error = %v", err)
	}
	if err := queue.push(ctx, inlineTask{id: "three"}, nil); !errors.Is(err, errInlineQueueFull) {
		t.Fatalf("full push() error = %v, want errInlineQueueFull", err)
	}
	if task, ok := queue.pop(); !ok || task.id != "one" {
		t.Fatalf("pop() = (%q, %t), want (one, true)", task.id, ok)
	}
	if err := queue.push(ctx, inlineTask{id: "three"}, nil); err != nil {
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

func TestInlineQueueCloseCompensatesOutsideUniqueLockBeforeLeaseRelease(t *testing.T) {
	releaseActive := make(chan struct{})
	activeStarted := make(chan struct{}, 1)
	queue := newInlineQueue(map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			activeStarted <- struct{}{}
			<-releaseActive
			return nil
		},
	}, 1)
	queue.Start(context.Background())

	if _, err := queue.Enqueue(asynq.NewTask("render", []byte("active"))); err != nil {
		t.Fatalf("active Enqueue() error = %v", err)
	}
	<-activeStarted

	var decisions []error
	discardRanOutsideUniqueLock := false
	leasePresentDuringDiscard := false
	_, err := queue.EnqueueWithTransition(
		asynq.NewTask("render", []byte("pending")),
		func(decision error) error {
			decisions = append(decisions, decision)
			if errors.Is(decision, errInlineQueueDiscarded) {
				discardRanOutsideUniqueLock = queue.uniqueMu.TryLock()
				if discardRanOutsideUniqueLock {
					leasePresentDuringDiscard = len(queue.uniqueLocks) == 1
					queue.uniqueMu.Unlock()
				}
			}
			return nil
		},
		asynq.Unique(time.Minute),
	)
	if err != nil {
		t.Fatalf("pending EnqueueWithTransition() error = %v", err)
	}

	queue.closePending()
	if len(decisions) != 2 {
		t.Fatalf("transition decisions = %d, want 2", len(decisions))
	}
	if decisions[0] != nil {
		t.Fatalf("accepted transition decision = %v, want nil", decisions[0])
	}
	if decisions[1] != errInlineQueueDiscarded {
		t.Fatalf("discard transition decision = %v, want %v", decisions[1], errInlineQueueDiscarded)
	}
	if !discardRanOutsideUniqueLock {
		t.Fatal("discard transition ran while unique lock was held")
	}
	if !leasePresentDuringDiscard {
		t.Fatal("discard transition ran after unique lease release")
	}
	if got := len(queue.uniqueLocks); got != 0 {
		t.Fatalf("unique locks after compensation = %d, want 0", got)
	}

	close(releaseActive)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	queue.Shutdown(shutdownCtx)
}

func TestInlineQueueConcurrentCloseWaitsForDiscardCompensation(t *testing.T) {
	releaseActive := make(chan struct{})
	activeStarted := make(chan struct{}, 1)
	queue := newInlineQueue(map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			activeStarted <- struct{}{}
			<-releaseActive
			return nil
		},
	}, 1)
	queue.Start(context.Background())

	if _, err := queue.Enqueue(asynq.NewTask("render", []byte("active"))); err != nil {
		t.Fatalf("active Enqueue() error = %v", err)
	}
	<-activeStarted

	discardStarted := make(chan struct{})
	releaseDiscard := make(chan struct{})
	if _, err := queue.EnqueueWithTransition(
		asynq.NewTask("render", []byte("pending")),
		func(decision error) error {
			if errors.Is(decision, errInlineQueueDiscarded) {
				close(discardStarted)
				<-releaseDiscard
			}
			return nil
		},
	); err != nil {
		t.Fatalf("pending EnqueueWithTransition() error = %v", err)
	}

	firstDone := make(chan struct{})
	go func() {
		queue.closePending()
		close(firstDone)
	}()
	<-discardStarted

	secondDone := make(chan struct{})
	go func() {
		queue.closePending()
		close(secondDone)
	}()
	select {
	case <-secondDone:
		close(releaseDiscard)
		close(releaseActive)
		t.Fatal("concurrent close returned before discard compensation finished")
	case <-time.After(20 * time.Millisecond):
	}

	close(releaseDiscard)
	for name, done := range map[string]<-chan struct{}{
		"first close":  firstDone,
		"second close": secondDone,
	} {
		select {
		case <-done:
		case <-time.After(time.Second):
			close(releaseActive)
			t.Fatalf("%s did not finish", name)
		}
	}

	close(releaseActive)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	queue.Shutdown(shutdownCtx)
}

func TestInlineQueueShutdownBoundsDiscardCompensation(t *testing.T) {
	releaseActive := make(chan struct{})
	activeStarted := make(chan struct{}, 1)
	queue := newInlineQueue(map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			activeStarted <- struct{}{}
			<-releaseActive
			return nil
		},
	}, 1)
	queue.Start(context.Background())

	if _, err := queue.Enqueue(asynq.NewTask("render", []byte("active"))); err != nil {
		t.Fatalf("active Enqueue() error = %v", err)
	}
	<-activeStarted

	discardStarted := make(chan struct{})
	releaseDiscard := make(chan struct{})
	if _, err := queue.EnqueueWithTransition(
		asynq.NewTask("render", []byte("pending")),
		func(decision error) error {
			if errors.Is(decision, errInlineQueueDiscarded) {
				close(discardStarted)
				<-releaseDiscard
			}
			return nil
		},
		asynq.Unique(time.Minute),
	); err != nil {
		t.Fatalf("pending EnqueueWithTransition() error = %v", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	started := time.Now()
	err := queue.Shutdown(shutdownCtx)
	shutdownCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown() error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("Shutdown() elapsed = %s, want bounded by context", elapsed)
	}
	select {
	case <-discardStarted:
	default:
		t.Fatal("discard transition did not start")
	}
	queue.uniqueMu.Lock()
	remainingLocks := len(queue.uniqueLocks)
	queue.uniqueMu.Unlock()
	if remainingLocks != 0 {
		t.Fatalf("unique locks after timed-out shutdown = %d, want 0", remainingLocks)
	}

	close(releaseDiscard)
	close(releaseActive)
	done := make(chan struct{})
	go func() {
		queue.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("workers did not stop after test releases")
	}
}

func TestInlineQueueShutdownHonorsCallerDeadlineAfterCancellationStartsClose(t *testing.T) {
	releaseActive := make(chan struct{})
	activeStarted := make(chan struct{}, 1)
	queue := newInlineQueue(map[string]taskHandler{
		"render": func(context.Context, *asynq.Task) error {
			activeStarted <- struct{}{}
			<-releaseActive
			return nil
		},
	}, 1)
	workerCtx, cancelWorkers := context.WithCancel(context.Background())
	queue.Start(workerCtx)

	if _, err := queue.Enqueue(asynq.NewTask("render", []byte("active"))); err != nil {
		t.Fatalf("active Enqueue() error = %v", err)
	}
	<-activeStarted

	discardStarted := make(chan struct{})
	releaseDiscard := make(chan struct{})
	if _, err := queue.EnqueueWithTransition(
		asynq.NewTask("render", []byte("pending")),
		func(decision error) error {
			if errors.Is(decision, errInlineQueueDiscarded) {
				close(discardStarted)
				<-releaseDiscard
			}
			return nil
		},
	); err != nil {
		t.Fatalf("pending EnqueueWithTransition() error = %v", err)
	}

	cancelWorkers()
	select {
	case <-discardStarted:
	case <-time.After(time.Second):
		close(releaseActive)
		t.Fatal("cancellation did not start discard compensation")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	started := time.Now()
	err := queue.Shutdown(shutdownCtx)
	shutdownCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		close(releaseDiscard)
		close(releaseActive)
		t.Fatalf("Shutdown() error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		close(releaseDiscard)
		close(releaseActive)
		t.Fatalf("Shutdown() elapsed = %s, want bounded by caller context", elapsed)
	}

	close(releaseDiscard)
	close(releaseActive)
	select {
	case <-queue.closeDone:
	case <-time.After(time.Second):
		t.Fatal("cancellation-owned close did not finish after test release")
	}
	done := make(chan struct{})
	go func() {
		queue.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("workers did not stop after test releases")
	}
}

func TestInlineQueueCompensatesPoppedTaskCanceledBeforeFirstAttempt(t *testing.T) {
	queue := newInlineQueue(nil, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var decisions []error
	discardRanOutsideUniqueLock := false
	leasePresentDuringDiscard := false
	queued := inlineTask{
		task: asynq.NewTask("render", []byte("popped")),
		id:   "inline-popped",
		transition: func(decision error) error {
			decisions = append(decisions, decision)
			discardRanOutsideUniqueLock = queue.uniqueMu.TryLock()
			if discardRanOutsideUniqueLock {
				leasePresentDuringDiscard = len(queue.uniqueLocks) == 1
				queue.uniqueMu.Unlock()
			}
			return nil
		},
		unique: true,
		uniqueKey: inlineUniqueKey{
			queue:       inlineDefaultQueue,
			taskType:    "render",
			payloadHash: sha256.Sum256([]byte("popped")),
		},
	}
	queue.uniqueLocks[queued.uniqueKey] = inlineUniqueLock{
		taskID:    queued.id,
		expiresAt: time.Now().Add(time.Minute),
	}

	handlerCalls := 0
	err := queue.process(ctx, queued, func(context.Context, *asynq.Task) error {
		handlerCalls++
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("process() error = %v, want context.Canceled", err)
	}
	if handlerCalls != 0 {
		t.Fatalf("handler calls = %d, want 0", handlerCalls)
	}
	if len(decisions) != 1 || decisions[0] != errInlineQueueDiscarded {
		t.Fatalf("transition decisions = %v, want [%v]", decisions, errInlineQueueDiscarded)
	}
	if !discardRanOutsideUniqueLock {
		t.Fatal("discard transition ran while unique lock was held")
	}
	if !leasePresentDuringDiscard {
		t.Fatal("discard transition ran after unique lease release")
	}
	if got := len(queue.uniqueLocks); got != 0 {
		t.Fatalf("unique locks after compensation = %d, want 0", got)
	}
}

func TestInlineQueueDoesNotCompensatePoppedTaskCanceledAfterHandlerStarts(t *testing.T) {
	queue := newInlineQueue(nil, 1)
	ctx, cancel := context.WithCancel(context.Background())
	wantErr := errors.New("render interrupted")
	transitionCalls := 0
	queued := inlineTask{
		task: asynq.NewTask("render", []byte("popped")),
		id:   "inline-popped",
		transition: func(error) error {
			transitionCalls++
			return nil
		},
		unique: true,
		uniqueKey: inlineUniqueKey{
			queue:       inlineDefaultQueue,
			taskType:    "render",
			payloadHash: sha256.Sum256([]byte("popped")),
		},
	}
	queue.uniqueLocks[queued.uniqueKey] = inlineUniqueLock{
		taskID:    queued.id,
		expiresAt: time.Now().Add(time.Minute),
	}

	handlerCalls := 0
	err := queue.process(ctx, queued, func(context.Context, *asynq.Task) error {
		handlerCalls++
		cancel()
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("process() error = %v, want %v", err, wantErr)
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}
	if transitionCalls != 0 {
		t.Fatalf("discard transition calls = %d, want 0", transitionCalls)
	}
	if got := len(queue.uniqueLocks); got != 0 {
		t.Fatalf("unique locks after handler failure = %d, want 0", got)
	}
}

func TestInlineQueueDoesNotCompensateNormalHandlerFailure(t *testing.T) {
	queue := newInlineQueue(nil, 1)
	wantErr := errors.New("render failed")
	transitionCalls := 0
	queued := inlineTask{
		task: asynq.NewTask("render", nil),
		id:   "inline-failed",
		transition: func(error) error {
			transitionCalls++
			return nil
		},
	}

	err := queue.process(context.Background(), queued, func(context.Context, *asynq.Task) error {
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("process() error = %v, want %v", err, wantErr)
	}
	if transitionCalls != 0 {
		t.Fatalf("discard transition calls = %d, want 0", transitionCalls)
	}
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

func TestInlineQueueSerializesCaptureTasks(t *testing.T) {
	const captures = 4
	var (
		mu         sync.Mutex
		running    int
		maxRunning int
	)
	done := make(chan struct{}, captures)
	queue := startTestInlineQueue(t, map[string]taskHandler{
		tasktypes.TypeRecordDemo: func(context.Context, *asynq.Task) error {
			mu.Lock()
			running++
			if running > maxRunning {
				maxRunning = running
			}
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			running--
			mu.Unlock()
			done <- struct{}{}
			return nil
		},
	}, 4)

	for i := 0; i < captures; i++ {
		if _, err := queue.Enqueue(asynq.NewTask(tasktypes.TypeRecordDemo, []byte{byte(i)})); err != nil {
			t.Fatalf("capture %d Enqueue() error = %v", i, err)
		}
	}
	deadline := time.After(5 * time.Second)
	for i := 0; i < captures; i++ {
		select {
		case <-done:
		case <-deadline:
			t.Fatalf("capture task %d did not finish", i)
		}
	}
	mu.Lock()
	got := maxRunning
	mu.Unlock()
	if got != 1 {
		t.Fatalf("concurrent capture tasks = %d, want 1", got)
	}
}

func TestInlineQueueRunsOtherTasksWhileCaptureIsActive(t *testing.T) {
	captureStarted := make(chan struct{})
	releaseCapture := make(chan struct{})
	defer close(releaseCapture)
	parsed := make(chan struct{}, 1)
	queue := startTestInlineQueue(t, map[string]taskHandler{
		tasktypes.TypeRecordDemo: func(context.Context, *asynq.Task) error {
			close(captureStarted)
			<-releaseCapture
			return nil
		},
		tasktypes.TypeParseDemo: func(context.Context, *asynq.Task) error {
			parsed <- struct{}{}
			return nil
		},
	}, 1)

	if _, err := queue.Enqueue(asynq.NewTask(tasktypes.TypeRecordDemo, nil)); err != nil {
		t.Fatalf("capture Enqueue() error = %v", err)
	}
	select {
	case <-captureStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("capture task did not start")
	}
	if _, err := queue.Enqueue(asynq.NewTask(tasktypes.TypeParseDemo, nil), asynq.MaxRetry(1)); err != nil {
		t.Fatalf("parse Enqueue() error = %v", err)
	}
	select {
	case <-parsed:
	case <-time.After(5 * time.Second):
		t.Fatal("parse task did not run while a capture held the serial lane")
	}
}

func TestInlineQueueDiscardsPendingCaptureTasksOnShutdown(t *testing.T) {
	captureStarted := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := newInlineQueue(map[string]taskHandler{
		tasktypes.TypeRecordDemo: func(context.Context, *asynq.Task) error {
			close(captureStarted)
			<-release
			return nil
		},
	}, 1)
	queue.Start(ctx)

	if _, err := queue.Enqueue(asynq.NewTask(tasktypes.TypeRecordDemo, []byte("hold"))); err != nil {
		t.Fatalf("blocking capture Enqueue() error = %v", err)
	}
	select {
	case <-captureStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("blocking capture task did not start")
	}

	var (
		decisionMu sync.Mutex
		decisions  []error
	)
	_, err := queue.EnqueueWithTransition(asynq.NewTask(tasktypes.TypeRecordDemo, []byte("pending")), func(decision error) error {
		decisionMu.Lock()
		decisions = append(decisions, decision)
		decisionMu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("pending capture EnqueueWithTransition() error = %v", err)
	}

	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer shutdownCancel()
	// The blocking capture keeps its worker busy past the deadline; Shutdown
	// still compensates every pending task before it returns.
	queue.Shutdown(shutdownCtx)

	decisionMu.Lock()
	got := append([]error(nil), decisions...)
	decisionMu.Unlock()
	if len(got) != 2 || got[0] != nil || !errors.Is(got[1], errInlineQueueDiscarded) {
		t.Fatalf("pending capture transition decisions = %v, want [nil errInlineQueueDiscarded]", got)
	}
}
