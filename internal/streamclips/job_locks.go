package streamclips

import (
	"sync"

	"github.com/google/uuid"
)

// JobLocks serializes short state transitions for one stream job while still
// allowing unrelated jobs to progress independently. A render uses the same
// instance as the HTTP edit-plan handlers so claiming/finalizing a render and
// mutating its plan cannot pass each other between validation and persistence.
type JobLocks struct {
	mu    sync.Mutex
	locks map[uuid.UUID]*jobLock
}

type jobLock struct {
	mu   sync.Mutex
	refs int
}

func NewJobLocks() *JobLocks {
	return &JobLocks{locks: make(map[uuid.UUID]*jobLock)}
}

// Lock acquires id's lock and returns an idempotent release function.
func (l *JobLocks) Lock(id uuid.UUID) func() {
	if l == nil {
		return func() {}
	}
	l.mu.Lock()
	entry := l.locks[id]
	if entry == nil {
		entry = &jobLock{}
		l.locks[id] = entry
	}
	entry.refs++
	l.mu.Unlock()

	entry.mu.Lock()
	var once sync.Once
	return func() {
		once.Do(func() {
			entry.mu.Unlock()
			l.mu.Lock()
			entry.refs--
			if entry.refs == 0 {
				delete(l.locks, id)
			}
			l.mu.Unlock()
		})
	}
}
