package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/tasks"
)

type taskHandler func(context.Context, *asynq.Task) error

const inlineDefaultQueue = "inline"

const inlineMinimumPendingTasks = 1024

const inlineParseAttemptTimeout = 15 * time.Minute

const inlineDiscardCompensationTimeout = 10 * time.Second

var (
	errInlineQueueFull      = errors.New("inline queue is full")
	errInlineQueueDiscarded = errors.New("inline queue task discarded during shutdown")
)

type inlineTask struct {
	task       *asynq.Task
	id         string
	policy     inlineTaskPolicy
	transition func(error) error
	uniqueKey  inlineUniqueKey
	unique     bool
}

// inlineTaskPolicy describes work the desktop queue owns after a task leaves
// the FIFO. Only deterministic parser work is safe to retry automatically;
// recording and other media work remain terminal after one attempt.
type inlineTaskPolicy struct {
	attemptTimeout time.Duration
	maxRetries     int
}

func defaultInlineTaskPolicy(taskType string) inlineTaskPolicy {
	switch taskType {
	case tasks.TypeParseDemo, tasks.TypeScanRoster:
		return inlineTaskPolicy{
			attemptTimeout: inlineParseAttemptTimeout,
			maxRetries:     1,
		}
	default:
		return inlineTaskPolicy{}
	}
}

type inlineUniqueKey struct {
	queue       string
	taskType    string
	payloadHash [sha256.Size]byte
}

type inlineUniqueLock struct {
	taskID    string
	expiresAt time.Time
}

// isCaptureTaskType reports whether a task records with CS2/HLAE. Capture
// tasks run on a dedicated serial lane: the recorder launches a single
// cs2.exe, so two concurrent captures — even for different jobs — collide
// with "cs2.exe is already running". Every other task type keeps the
// concurrent worker pool, so parses and renders still progress while a
// capture is running.
func isCaptureTaskType(taskType string) bool {
	return taskType == tasks.TypeRecordDemo
}

type inlineTaskQueue struct {
	mu     sync.Mutex
	ready  *sync.Cond
	tasks  []inlineTask
	closed bool
	max    int
}

func newInlineTaskQueue(maxPending int) *inlineTaskQueue {
	queue := &inlineTaskQueue{max: maxPending}
	queue.ready = sync.NewCond(&queue.mu)
	return queue
}

func (q *inlineTaskQueue) push(ctx context.Context, task inlineTask, transition func(error) error) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return applyInlineEnqueueTransition(transition, err)
	}
	if q.closed {
		return applyInlineEnqueueTransition(transition, fmt.Errorf("inline queue is shut down"))
	}
	if len(q.tasks) >= q.max {
		return applyInlineEnqueueTransition(transition, errInlineQueueFull)
	}
	if err := applyInlineEnqueueTransition(transition, nil); err != nil {
		return err
	}
	q.tasks = append(q.tasks, task)
	q.ready.Signal()
	return nil
}

func (q *inlineTaskQueue) pop() (inlineTask, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.tasks) == 0 && !q.closed {
		q.ready.Wait()
	}
	if q.closed {
		return inlineTask{}, false
	}
	task := q.tasks[0]
	q.tasks[0] = inlineTask{}
	q.tasks = q.tasks[1:]
	if len(q.tasks) == 0 {
		q.tasks = nil
	}
	return task, true
}

func (q *inlineTaskQueue) close() []inlineTask {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil
	}
	q.closed = true
	discarded := q.tasks
	q.tasks = nil
	q.ready.Broadcast()
	return discarded
}

type inlineQueue struct {
	handlers    map[string]taskHandler
	concurrency int

	ctx          context.Context
	tasks        *inlineTaskQueue
	captureTasks *inlineTaskQueue
	wg           sync.WaitGroup
	stopClose    func() bool
	nextID       atomic.Uint64

	closeMu      sync.Mutex
	closeStarted bool
	closeDone    chan struct{}
	closeErr     error

	uniqueMu    sync.Mutex
	uniqueLocks map[inlineUniqueKey]inlineUniqueLock
	now         func() time.Time
}

func newInlineQueue(handlers map[string]taskHandler, concurrency int) *inlineQueue {
	if concurrency < 1 {
		concurrency = 1
	}
	maxPending := inlineMinimumPendingTasks
	if workerBuffer := concurrency * 2; workerBuffer > maxPending {
		maxPending = workerBuffer
	}
	return &inlineQueue{
		handlers:     handlers,
		concurrency:  concurrency,
		tasks:        newInlineTaskQueue(maxPending),
		captureTasks: newInlineTaskQueue(maxPending),
		closeDone:    make(chan struct{}),
		uniqueLocks:  make(map[inlineUniqueKey]inlineUniqueLock),
		now:          time.Now,
	}
}

func (q *inlineQueue) Start(ctx context.Context) {
	q.ctx = ctx
	q.stopClose = context.AfterFunc(ctx, q.closePending)
	for i := 0; i < q.concurrency; i++ {
		q.wg.Add(1)
		go q.run(ctx, q.tasks)
	}
	// The capture lane always has exactly one worker; see isCaptureTaskType.
	q.wg.Add(1)
	go q.run(ctx, q.captureTasks)
}

func (q *inlineQueue) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return q.enqueue(task, nil, opts...)
}

// EnqueueWithTransition serializes a caller-owned state transition with the
// queue's admission decision. The callback receives nil for accepted work or
// the rejection error for duplicate/full/shutdown decisions. Accepted work is
// not visible to workers until the callback succeeds. If shutdown later
// discards accepted work before completion, the callback runs again with
// errInlineQueueDiscarded so its durable state can be compensated. The
// admission invocation runs while queue locks serialize the transition with
// task visibility, so it must not call Enqueue or EnqueueWithTransition on
// this queue. The later discard invocation runs without those queue locks.
func (q *inlineQueue) EnqueueWithTransition(task *asynq.Task, transition func(error) error, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return q.enqueue(task, transition, opts...)
}

func (q *inlineQueue) enqueue(task *asynq.Task, transition func(error) error, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if task == nil {
		return nil, applyInlineEnqueueTransition(transition, fmt.Errorf("inline queue cannot enqueue nil task"))
	}
	if handler, ok := q.handlers[task.Type()]; !ok || handler == nil {
		return nil, applyInlineEnqueueTransition(transition, fmt.Errorf("inline queue handler is not configured for %s", task.Type()))
	}
	if q.ctx == nil {
		return nil, applyInlineEnqueueTransition(transition, fmt.Errorf("inline queue is not started"))
	}
	options, err := parseInlineEnqueueOptions(opts)
	if err != nil {
		return nil, applyInlineEnqueueTransition(transition, err)
	}
	id := fmt.Sprintf("inline-%d", q.nextID.Add(1))
	policy := defaultInlineTaskPolicy(task.Type())
	if options.maxRetry != nil && *options.maxRetry != policy.maxRetries {
		return nil, applyInlineEnqueueTransition(transition, fmt.Errorf(
			"inline queue retry policy for %s is %d, got %d",
			task.Type(), policy.maxRetries, *options.maxRetry,
		))
	}
	queued := inlineTask{task: task, id: id, policy: policy, transition: transition}
	if options.uniqueTTL > 0 {
		queued.unique = true
		queued.uniqueKey = inlineUniqueKey{
			queue:       options.queue,
			taskType:    task.Type(),
			payloadHash: sha256.Sum256(task.Payload()),
		}
	}
	if err := q.push(queued, options.uniqueTTL, transition); err != nil {
		return nil, err
	}
	return &asynq.TaskInfo{
		ID:        id,
		Queue:     options.queue,
		Type:      task.Type(),
		Payload:   task.Payload(),
		Headers:   task.Headers(),
		State:     asynq.TaskStatePending,
		Retried:   0,
		MaxRetry:  policy.maxRetries,
		Timeout:   policy.attemptTimeout,
		Retention: 0,
	}, nil
}

func (q *inlineQueue) Shutdown(ctx context.Context) error {
	if q.stopClose != nil {
		q.stopClose()
	}
	closeErr := q.closePendingWithin(ctx)
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return closeErr
	case <-ctx.Done():
		return errors.Join(closeErr, ctx.Err())
	}
}

func (q *inlineQueue) closePending() {
	ctx, cancel := context.WithTimeout(context.Background(), inlineDiscardCompensationTimeout)
	defer cancel()
	if err := q.closePendingWithin(ctx); err != nil {
		log.Printf("inline queue: close pending tasks: %v", err)
	}
}

func (q *inlineQueue) closePendingWithin(ctx context.Context) error {
	q.closeMu.Lock()
	if q.closeStarted {
		done := q.closeDone
		q.closeMu.Unlock()
		select {
		case <-done:
			q.closeMu.Lock()
			err := q.closeErr
			q.closeMu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	q.closeStarted = true
	q.closeMu.Unlock()

	err := q.finishClosePending(ctx)
	q.closeMu.Lock()
	q.closeErr = err
	close(q.closeDone)
	q.closeMu.Unlock()
	return err
}

func (q *inlineQueue) finishClosePending(ctx context.Context) error {
	// Serialize closing with unique admission, then release the lock before
	// durable compensation. Each lease remains installed until its callback
	// finishes, so a re-drive cannot overtake the state transition.
	q.uniqueMu.Lock()
	discarded := q.tasks.close()
	discarded = append(discarded, q.captureTasks.close()...)
	q.uniqueMu.Unlock()

	var errs []error
	for index, task := range discarded {
		if err := ctx.Err(); err != nil {
			for _, remaining := range discarded[index:] {
				q.releaseUnique(remaining)
			}
			errs = append(errs, fmt.Errorf(
				"discard compensation stopped with %d task(s) unresolved: %w",
				len(discarded)-index,
				err,
			))
			break
		}
		if err := q.compensateDiscardedWithin(ctx, task); err != nil {
			errs = append(errs, err)
			q.releaseUnique(task)
			if ctx.Err() != nil {
				for _, remaining := range discarded[index+1:] {
					q.releaseUnique(remaining)
				}
				errs = append(errs, fmt.Errorf(
					"discard compensation stopped with %d task(s) unresolved: %w",
					len(discarded)-index,
					ctx.Err(),
				))
				break
			}
			continue
		}
		q.releaseUnique(task)
	}
	return errors.Join(errs...)
}

func (q *inlineQueue) run(ctx context.Context, lane *inlineTaskQueue) {
	defer q.wg.Done()
	for {
		queued, ok := lane.pop()
		if !ok {
			return
		}
		task := queued.task
		if task == nil {
			continue
		}
		handler := q.handlers[task.Type()]
		if handler == nil {
			log.Printf("inline queue: no handler for %s", task.Type())
			continue
		}
		err := q.process(ctx, queued, handler)
		if err != nil {
			log.Printf("inline queue: %s failed: %v", task.Type(), err)
		}
	}
}

func (q *inlineQueue) process(ctx context.Context, queued inlineTask, handler taskHandler) error {
	err, handlerStarted := q.handle(ctx, queued, handler)
	if !handlerStarted && ctx.Err() != nil {
		// The task has already left the pending FIFO, so closePending cannot see
		// it. Compensate only when cancellation wins before the first handler
		// attempt. Once a handler starts, its own terminal path owns the durable
		// failure detail and must not be overwritten by admission compensation.
		q.compensateDiscarded(queued)
		q.releaseUnique(queued)
		return err
	}
	// The uniqueness lease belongs to the logical task, including all of its
	// attempts. Release it only after success or retry exhaustion so the
	// desktop's explicit Retry action can enqueue the task again after the
	// queue no longer owns it.
	q.releaseUnique(queued)
	return err
}

func (q *inlineQueue) handle(ctx context.Context, queued inlineTask, handler taskHandler) (error, bool) {
	var err error
	handlerStarted := false
	for attempt := 0; attempt <= queued.policy.maxRetries; attempt++ {
		if parentErr := ctx.Err(); parentErr != nil {
			return parentErr, handlerStarted
		}

		attemptCtx := tasks.WithTaskAttempt(ctx, attempt, queued.policy.maxRetries)
		cancel := func() {}
		if queued.policy.attemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(attemptCtx, queued.policy.attemptTimeout)
		}
		handlerStarted = true
		err = handler(attemptCtx, queued.task)
		cancel()
		if err == nil {
			return nil, handlerStarted
		}
		if ctx.Err() != nil {
			return err, handlerStarted
		}
		if attempt == queued.policy.maxRetries {
			return err, handlerStarted
		}
		log.Printf(
			"inline queue: %s attempt %d failed, retrying: %v",
			queued.task.Type(), attempt+1, err,
		)
	}
	return err, handlerStarted
}

func (q *inlineQueue) compensateDiscarded(task inlineTask) {
	if err := applyInlineDiscardTransition(task); err != nil {
		log.Printf("inline queue: %v", err)
	}
}

func (q *inlineQueue) compensateDiscardedWithin(ctx context.Context, task inlineTask) error {
	if task.transition == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() {
		done <- applyInlineDiscardTransition(task)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("compensate discarded task %s: %w", task.id, ctx.Err())
	}
}

func applyInlineDiscardTransition(task inlineTask) error {
	if task.transition == nil {
		return nil
	}
	if err := task.transition(errInlineQueueDiscarded); err != nil {
		return fmt.Errorf("compensate discarded task %s: %w", task.id, err)
	}
	return nil
}

type inlineEnqueueOptions struct {
	queue     string
	uniqueTTL time.Duration
	maxRetry  *int
}

func parseInlineEnqueueOptions(opts []asynq.Option) (inlineEnqueueOptions, error) {
	// Asynq does not expose options attached inside NewTask. Every current
	// uniqueness call site supplies Unique directly to Enqueue, which is the
	// option surface this local queue deliberately implements.
	result := inlineEnqueueOptions{queue: inlineDefaultQueue}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		switch opt.Type() {
		case asynq.QueueOpt:
			queue, ok := opt.Value().(string)
			if !ok {
				return inlineEnqueueOptions{}, fmt.Errorf("inline queue option %s has invalid queue value", opt.String())
			}
			if strings.TrimSpace(queue) == "" {
				return inlineEnqueueOptions{}, fmt.Errorf("queue name must contain one or more characters")
			}
			result.queue = queue
		case asynq.UniqueOpt:
			ttl, ok := opt.Value().(time.Duration)
			if !ok {
				return inlineEnqueueOptions{}, fmt.Errorf("inline queue option %s has invalid unique TTL", opt.String())
			}
			if ttl < time.Second {
				return inlineEnqueueOptions{}, fmt.Errorf("unique TTL cannot be less than 1s")
			}
			result.uniqueTTL = ttl
		case asynq.MaxRetryOpt:
			retries, ok := opt.Value().(int)
			if !ok {
				return inlineEnqueueOptions{}, fmt.Errorf("inline queue option %s has invalid retry value", opt.String())
			}
			result.maxRetry = &retries
		default:
			return inlineEnqueueOptions{}, fmt.Errorf("inline queue does not support option %s", opt.String())
		}
	}
	return result, nil
}

func (q *inlineQueue) push(task inlineTask, uniqueTTL time.Duration, transition func(error) error) error {
	lane := q.tasks
	if isCaptureTaskType(task.task.Type()) {
		lane = q.captureTasks
	}
	if !task.unique {
		return lane.push(q.ctx, task, transition)
	}
	q.uniqueMu.Lock()
	defer q.uniqueMu.Unlock()

	now := q.now()
	if lock, ok := q.uniqueLocks[task.uniqueKey]; ok && now.Before(lock.expiresAt) {
		return applyInlineEnqueueTransition(transition, asynq.ErrDuplicateTask)
	}
	if err := lane.push(q.ctx, task, transition); err != nil {
		return err
	}
	q.uniqueLocks[task.uniqueKey] = inlineUniqueLock{
		taskID:    task.id,
		expiresAt: now.Add(uniqueTTL),
	}
	return nil
}

func applyInlineEnqueueTransition(transition func(error) error, decision error) error {
	if transition == nil {
		return decision
	}
	if err := transition(decision); err != nil {
		if decision == nil {
			return fmt.Errorf("apply accepted inline queue transition: %w", err)
		}
		return fmt.Errorf("apply rejected inline queue transition after %v: %w", decision, err)
	}
	return decision
}

func (q *inlineQueue) releaseUnique(task inlineTask) {
	if !task.unique {
		return
	}
	q.uniqueMu.Lock()
	defer q.uniqueMu.Unlock()
	q.releaseUniqueLocked(task)
}

func (q *inlineQueue) releaseUniqueLocked(task inlineTask) {
	if !task.unique {
		return
	}
	lock, ok := q.uniqueLocks[task.uniqueKey]
	if ok && lock.taskID == task.id {
		delete(q.uniqueLocks, task.uniqueKey)
	}
}
