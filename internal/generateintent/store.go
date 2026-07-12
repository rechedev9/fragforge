// Package generateintent owns the durable, job-scoped state for guided
// generate runs.
package generateintent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/storage"
)

const lockStripeCount = 64

// ErrActiveRun reports that a job already owns guided-generate work.
var ErrActiveRun = errors.New("generate run is already active")

// Store serializes read-modify-write operations for a job's generate intent.
// A single Store is shared by the desktop HTTP and worker runtimes, making a
// conditional completion atomic with accepting a newer run.
type Store struct {
	storage storage.Storage
	locks   [lockStripeCount]sync.Mutex
}

// New returns a generate-intent store backed by artifacts.
func New(store storage.Storage) *Store {
	return &Store{storage: store}
}

// Write atomically publishes the latest accepted intent for id.
func (s *Store) Write(id uuid.UUID, intent renderplan.GenerateIntent) error {
	lock := s.lock(id)
	lock.Lock()
	defer lock.Unlock()

	return s.write(id, intent)
}

// Begin publishes intent only while no prior run owns the job and ready still
// reports that adjacent render state is idle. ready runs inside the same job
// lock used by Finish and WhileIdle; it must not call this Store recursively.
func (s *Store) Begin(id uuid.UUID, intent renderplan.GenerateIntent, ready func() error) error {
	lock := s.lock(id)
	lock.Lock()
	defer lock.Unlock()

	current, ok, err := s.read(id)
	if err != nil {
		return err
	}
	if ok && current.ActiveRunID != uuid.Nil {
		return fmt.Errorf("%w for job %s", ErrActiveRun, id)
	}
	if ready != nil {
		if err := ready(); err != nil {
			return err
		}
	}
	return s.write(id, intent)
}

// Read returns the latest accepted intent for id, when present.
func (s *Store) Read(id uuid.UUID) (renderplan.GenerateIntent, bool, error) {
	lock := s.lock(id)
	lock.Lock()
	defer lock.Unlock()

	return s.read(id)
}

// Complete clears ActiveRunID only when runID still owns the latest intent.
// The comparison and write share the same critical section as Write, so an
// older run can never erase a newer accepted run.
func (s *Store) Complete(id, runID uuid.UUID) error {
	_, err := s.Finish(id, runID, nil)
	return err
}

// Finish runs persist and clears ActiveRunID only when runID still owns the
// latest intent. persist and marker completion share the job lock, preventing
// a stale run from publishing state over a newer choice. persist must not call
// this Store recursively.
func (s *Store) Finish(id, runID uuid.UUID, persist func() error) (bool, error) {
	if runID == uuid.Nil {
		return false, nil
	}
	lock := s.lock(id)
	lock.Lock()
	defer lock.Unlock()

	intent, ok, err := s.read(id)
	if err != nil || !ok || intent.ActiveRunID != runID {
		return false, err
	}
	if persist != nil {
		if err := persist(); err != nil {
			return true, err
		}
	}
	intent.ActiveRunID = uuid.Nil
	return true, s.write(id, intent)
}

// WhileIdle runs persist only when no guided run currently owns id. It closes
// the admission race between manual render state and Begin. persist must not
// call this Store recursively.
func (s *Store) WhileIdle(id uuid.UUID, persist func() error) error {
	lock := s.lock(id)
	lock.Lock()
	defer lock.Unlock()

	intent, ok, err := s.read(id)
	if err != nil {
		return err
	}
	if ok && intent.ActiveRunID != uuid.Nil {
		return fmt.Errorf("%w for job %s", ErrActiveRun, id)
	}
	return persist()
}

func (s *Store) lock(id uuid.UUID) *sync.Mutex {
	return &s.locks[int(id[0])%len(s.locks)]
}

func (s *Store) write(id uuid.UUID, intent renderplan.GenerateIntent) error {
	b, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal generate intent: %w", err)
	}
	if err := s.storage.Put(artifacts.GenerateIntentKey(id), bytes.NewReader(b)); err != nil {
		return fmt.Errorf("write generate intent: %w", err)
	}
	return nil
}

func (s *Store) read(id uuid.UUID) (renderplan.GenerateIntent, bool, error) {
	rc, err := s.storage.Open(artifacts.GenerateIntentKey(id))
	if err != nil {
		if storage.IsNotExist(err) {
			return renderplan.GenerateIntent{}, false, nil
		}
		return renderplan.GenerateIntent{}, false, fmt.Errorf("open generate intent: %w", err)
	}
	var intent renderplan.GenerateIntent
	if err := json.NewDecoder(rc).Decode(&intent); err != nil {
		return renderplan.GenerateIntent{}, false, fmt.Errorf("decode generate intent: %w", errors.Join(err, rc.Close()))
	}
	if err := rc.Close(); err != nil {
		return renderplan.GenerateIntent{}, false, fmt.Errorf("close generate intent: %w", err)
	}
	return intent, true, nil
}
