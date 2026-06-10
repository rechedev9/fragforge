package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hibiken/asynq"
)

type taskHandler func(context.Context, *asynq.Task) error

type inlineQueue struct {
	handlers    map[string]taskHandler
	concurrency int

	ctx   context.Context
	tasks chan *asynq.Task
	wg    sync.WaitGroup
}

func newInlineQueue(handlers map[string]taskHandler, concurrency int) *inlineQueue {
	if concurrency < 1 {
		concurrency = 1
	}
	return &inlineQueue{
		handlers:    handlers,
		concurrency: concurrency,
		tasks:       make(chan *asynq.Task, concurrency*2),
	}
}

func (q *inlineQueue) Start(ctx context.Context) {
	q.ctx = ctx
	for i := 0; i < q.concurrency; i++ {
		q.wg.Add(1)
		go q.run(ctx)
	}
}

func (q *inlineQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	if task == nil {
		return nil, fmt.Errorf("inline queue cannot enqueue nil task")
	}
	if _, ok := q.handlers[task.Type()]; !ok {
		return nil, fmt.Errorf("inline queue handler is not configured for %s", task.Type())
	}
	if q.ctx == nil {
		return nil, fmt.Errorf("inline queue is not started")
	}
	select {
	case <-q.ctx.Done():
		return nil, q.ctx.Err()
	case q.tasks <- task:
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

func (q *inlineQueue) run(ctx context.Context) {
	defer q.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-q.tasks:
			if task == nil {
				continue
			}
			handler := q.handlers[task.Type()]
			if handler == nil {
				log.Printf("inline queue: no handler for %s", task.Type())
				continue
			}
			if err := handler(ctx, task); err != nil {
				log.Printf("inline queue: %s failed: %v", task.Type(), err)
			}
		}
	}
}
