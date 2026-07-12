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

var errInlineQueueFull = errors.New("inline queue is full")

type inlineTask struct {
	task      *asynq.Task
	id        string
	policy    inlineTaskPolicy
	uniqueKey inlineUniqueKey
	unique    bool
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

func (q *inlineTaskQueue) push(ctx context.Context, task inlineTask) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if q.closed {
		return fmt.Errorf("inline queue is shut down")
	}
	if len(q.tasks) >= q.max {
		return errInlineQueueFull
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

	ctx       context.Context
	tasks     *inlineTaskQueue
	wg        sync.WaitGroup
	stopClose func() bool
	nextID    atomic.Uint64

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
		handlers:    handlers,
		concurrency: concurrency,
		tasks:       newInlineTaskQueue(maxPending),
		uniqueLocks: make(map[inlineUniqueKey]inlineUniqueLock),
		now:         time.Now,
	}
}

func (q *inlineQueue) Start(ctx context.Context) {
	q.ctx = ctx
	q.stopClose = context.AfterFunc(ctx, q.closePending)
	for i := 0; i < q.concurrency; i++ {
		q.wg.Add(1)
		go q.run(ctx)
	}
}

func (q *inlineQueue) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if task == nil {
		return nil, fmt.Errorf("inline queue cannot enqueue nil task")
	}
	if handler, ok := q.handlers[task.Type()]; !ok || handler == nil {
		return nil, fmt.Errorf("inline queue handler is not configured for %s", task.Type())
	}
	if q.ctx == nil {
		return nil, fmt.Errorf("inline queue is not started")
	}
	options, err := parseInlineEnqueueOptions(opts)
	if err != nil {
		return nil, err
	}
	id := fmt.Sprintf("inline-%d", q.nextID.Add(1))
	policy := defaultInlineTaskPolicy(task.Type())
	if options.maxRetry != nil && *options.maxRetry != policy.maxRetries {
		return nil, fmt.Errorf(
			"inline queue retry policy for %s is %d, got %d",
			task.Type(), policy.maxRetries, *options.maxRetry,
		)
	}
	queued := inlineTask{task: task, id: id, policy: policy}
	if options.uniqueTTL > 0 {
		queued.unique = true
		queued.uniqueKey = inlineUniqueKey{
			queue:       options.queue,
			taskType:    task.Type(),
			payloadHash: sha256.Sum256(task.Payload()),
		}
	}
	if err := q.push(queued, options.uniqueTTL); err != nil {
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

func (q *inlineQueue) Shutdown(ctx context.Context) {
	if q.stopClose != nil {
		q.stopClose()
	}
	q.closePending()
	done := make(chan struct{})
	go func() {
		q.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func (q *inlineQueue) closePending() {
	q.uniqueMu.Lock()
	defer q.uniqueMu.Unlock()
	for _, task := range q.tasks.close() {
		q.releaseUniqueLocked(task)
	}
}

func (q *inlineQueue) run(ctx context.Context) {
	defer q.wg.Done()
	for {
		queued, ok := q.tasks.pop()
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
		err := q.handle(ctx, queued, handler)
		// The uniqueness lease belongs to the logical task, including all of
		// its attempts. Release it only after success, retry exhaustion, or
		// parent cancellation so the desktop's explicit Retry action can enqueue
		// the task again after the queue no longer owns it.
		q.releaseUnique(queued)
		if err != nil {
			log.Printf("inline queue: %s failed: %v", task.Type(), err)
		}
	}
}

func (q *inlineQueue) handle(ctx context.Context, queued inlineTask, handler taskHandler) error {
	var err error
	for attempt := 0; attempt <= queued.policy.maxRetries; attempt++ {
		if parentErr := ctx.Err(); parentErr != nil {
			return parentErr
		}

		attemptCtx := tasks.WithTaskAttempt(ctx, attempt, queued.policy.maxRetries)
		cancel := func() {}
		if queued.policy.attemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(attemptCtx, queued.policy.attemptTimeout)
		}
		err = handler(attemptCtx, queued.task)
		cancel()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil || attempt == queued.policy.maxRetries {
			return err
		}
		log.Printf(
			"inline queue: %s attempt %d failed, retrying: %v",
			queued.task.Type(), attempt+1, err,
		)
	}
	return err
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

func (q *inlineQueue) push(task inlineTask, uniqueTTL time.Duration) error {
	if !task.unique {
		return q.tasks.push(q.ctx, task)
	}
	q.uniqueMu.Lock()
	defer q.uniqueMu.Unlock()

	now := q.now()
	if lock, ok := q.uniqueLocks[task.uniqueKey]; ok && now.Before(lock.expiresAt) {
		return asynq.ErrDuplicateTask
	}
	if err := q.tasks.push(q.ctx, task); err != nil {
		return err
	}
	q.uniqueLocks[task.uniqueKey] = inlineUniqueLock{
		taskID:    task.id,
		expiresAt: now.Add(uniqueTTL),
	}
	return nil
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
