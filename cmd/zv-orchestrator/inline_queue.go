package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/tasks"
)

type taskHandler func(context.Context, *asynq.Task) error

// inlineQueue runs tasks in-process for the memory/sqlite modes. Two lanes:
//
//   - a shared pool of `concurrency` goroutines for every regular task, and
//   - a single dedicated goroutine for capture (record:demo) tasks, so
//     captures run strictly one at a time (HLAE/CS2 is a machine-global
//     resource) and a queued capture never starves parse/render tasks by
//     occupying a shared slot.
//
// Enqueue also honours asynq.Unique: a task enqueued with the option is
// dropped with asynq.ErrDuplicateTask while an identical task (same type and
// payload) is still queued or running. Without this the web reconcile loop,
// which re-POSTs record on every poll tick, piled up duplicate capture tasks.
type inlineQueue struct {
	handlers    map[string]taskHandler
	concurrency int

	ctx          context.Context
	tasks        chan *inlineTask
	captureTasks chan *inlineTask
	wg           sync.WaitGroup

	mu     sync.Mutex
	unique map[string]struct{}
}

type inlineTask struct {
	task *asynq.Task
	// uniqueKey is non-empty when the task was enqueued with asynq.Unique;
	// the key is released when the task finishes.
	uniqueKey string
}

// captureLaneBuffer bounds how many captures may wait in line. Unique dedup
// keeps re-drives out, so the buffer only has to hold genuinely distinct
// pending captures (one per reel the user queued up).
const captureLaneBuffer = 32

func newInlineQueue(handlers map[string]taskHandler, concurrency int) *inlineQueue {
	if concurrency < 1 {
		concurrency = 1
	}
	return &inlineQueue{
		handlers:     handlers,
		concurrency:  concurrency,
		tasks:        make(chan *inlineTask, concurrency*2),
		captureTasks: make(chan *inlineTask, captureLaneBuffer),
		unique:       make(map[string]struct{}),
	}
}

func (q *inlineQueue) Start(ctx context.Context) {
	q.ctx = ctx
	for i := 0; i < q.concurrency; i++ {
		q.wg.Add(1)
		go q.run(ctx, q.tasks)
	}
	q.wg.Add(1)
	go q.run(ctx, q.captureTasks)
}

func (q *inlineQueue) Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if task == nil {
		return nil, fmt.Errorf("inline queue cannot enqueue nil task")
	}
	if _, ok := q.handlers[task.Type()]; !ok {
		return nil, fmt.Errorf("inline queue handler is not configured for %s", task.Type())
	}
	if q.ctx == nil {
		return nil, fmt.Errorf("inline queue is not started")
	}

	it := &inlineTask{task: task}
	if hasUniqueOption(opts) {
		it.uniqueKey = task.Type() + "\x00" + string(task.Payload())
		q.mu.Lock()
		if _, dup := q.unique[it.uniqueKey]; dup {
			q.mu.Unlock()
			return nil, asynq.ErrDuplicateTask
		}
		q.unique[it.uniqueKey] = struct{}{}
		q.mu.Unlock()
	}

	lane := q.tasks
	if task.Type() == tasks.TypeRecordDemo {
		lane = q.captureTasks
	}
	select {
	case <-q.ctx.Done():
		q.releaseUnique(it.uniqueKey)
		return nil, q.ctx.Err()
	case lane <- it:
		return &asynq.TaskInfo{
			ID:        fmt.Sprintf("inline-%d", time.Now().UnixNano()),
			Queue:     "inline",
			Type:      task.Type(),
			Payload:   task.Payload(),
			Headers:   task.Headers(),
			State:     asynq.TaskStatePending,
			Retried:   0,
			MaxRetry:  0,
			Retention: 0,
		}, nil
	}
}

func (q *inlineQueue) Shutdown(ctx context.Context) {
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

func (q *inlineQueue) run(ctx context.Context, lane chan *inlineTask) {
	defer q.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case it := <-lane:
			if it == nil || it.task == nil {
				continue
			}
			handler := q.handlers[it.task.Type()]
			if handler == nil {
				log.Printf("inline queue: no handler for %s", it.task.Type())
				q.releaseUnique(it.uniqueKey)
				continue
			}
			if err := handler(ctx, it.task); err != nil {
				log.Printf("inline queue: %s failed: %v", it.task.Type(), err)
			}
			q.releaseUnique(it.uniqueKey)
		}
	}
}

func (q *inlineQueue) releaseUnique(key string) {
	if key == "" {
		return
	}
	q.mu.Lock()
	delete(q.unique, key)
	q.mu.Unlock()
}

func hasUniqueOption(opts []asynq.Option) bool {
	for _, opt := range opts {
		if opt != nil && opt.Type() == asynq.UniqueOpt {
			return true
		}
	}
	return false
}
