package generateintent

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/renderplan"
)

type blockingStorage struct {
	mu           sync.Mutex
	files        map[string][]byte
	blockClose   bool
	readStarted  chan struct{}
	releaseClose chan struct{}
}

func (s *blockingStorage) Put(key string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.files[key] = b
	s.mu.Unlock()
	return nil
}

func (s *blockingStorage) Open(key string) (io.ReadCloser, error) {
	s.mu.Lock()
	b, ok := s.files[key]
	block := s.blockClose
	if block {
		s.blockClose = false
	}
	s.mu.Unlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	if !block {
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	close(s.readStarted)
	return &blockingReadCloser{Reader: bytes.NewReader(b), release: s.releaseClose}, nil
}

func (s *blockingStorage) Exists(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.files[key]
	return ok, nil
}

type blockingReadCloser struct {
	*bytes.Reader
	release <-chan struct{}
}

func (r *blockingReadCloser) Close() error {
	<-r.release
	return nil
}

func TestCompleteCannotEraseNewerAcceptedRun(t *testing.T) {
	id := uuid.New()
	oldIntent := renderplan.GenerateIntent{
		Variant:     editor.PresetViral60Clean,
		Edit:        renderplan.DefaultEditRequest(),
		ActiveRunID: uuid.New(),
	}
	newIntent := oldIntent
	newIntent.ActiveRunID = uuid.New()
	initial, err := json.Marshal(oldIntent)
	if err != nil {
		t.Fatal(err)
	}
	backend := &blockingStorage{
		files:        map[string][]byte{artifacts.GenerateIntentKey(id): initial},
		blockClose:   true,
		readStarted:  make(chan struct{}),
		releaseClose: make(chan struct{}),
	}
	store := New(backend)

	completeDone := make(chan error, 1)
	go func() { completeDone <- store.Complete(id, oldIntent.ActiveRunID) }()
	<-backend.readStarted

	writeDone := make(chan error, 1)
	writeStarted := make(chan struct{})
	go func() {
		close(writeStarted)
		writeDone <- store.Write(id, newIntent)
	}()
	<-writeStarted
	var earlyWriteErr error
	writeFinishedEarly := false
	select {
	case earlyWriteErr = <-writeDone:
		writeFinishedEarly = true
	case <-time.After(100 * time.Millisecond):
	}

	close(backend.releaseClose)
	if err := <-completeDone; err != nil {
		t.Fatalf("Complete error = %v", err)
	}
	if writeFinishedEarly {
		t.Fatalf("new run write completed before old compare-and-write: %v", earlyWriteErr)
	}
	if writeErr := <-writeDone; writeErr != nil {
		t.Fatalf("Write error = %v", writeErr)
	}
	got, ok, err := store.Read(id)
	if err != nil || !ok {
		t.Fatalf("Read = (%#v, %v, %v)", got, ok, err)
	}
	if got.ActiveRunID != newIntent.ActiveRunID {
		t.Fatalf("ActiveRunID = %s, want newer run %s", got.ActiveRunID, newIntent.ActiveRunID)
	}
}

func TestBeginRejectsOverlappingRunBeforeEligibilityCheck(t *testing.T) {
	id := uuid.New()
	active := renderplan.GenerateIntent{
		Variant:     editor.PresetViral60Clean,
		Edit:        renderplan.DefaultEditRequest(),
		ActiveRunID: uuid.New(),
	}
	backend := &blockingStorage{files: make(map[string][]byte)}
	store := New(backend)
	if err := store.Write(id, active); err != nil {
		t.Fatal(err)
	}
	eligibilityCalled := false
	next := active
	next.ActiveRunID = uuid.New()
	err := store.Begin(id, next, func() error {
		eligibilityCalled = true
		return nil
	})
	if !errors.Is(err, ErrActiveRun) {
		t.Fatalf("Begin error = %v, want ErrActiveRun", err)
	}
	if eligibilityCalled {
		t.Fatal("eligibility ran while an active run already owned the job")
	}
}

func TestFinishDoesNotPublishForStaleRun(t *testing.T) {
	id := uuid.New()
	current := renderplan.GenerateIntent{
		Variant:     editor.PresetViral60Clean,
		Edit:        renderplan.DefaultEditRequest(),
		ActiveRunID: uuid.New(),
	}
	backend := &blockingStorage{files: make(map[string][]byte)}
	store := New(backend)
	if err := store.Write(id, current); err != nil {
		t.Fatal(err)
	}
	published := false
	owned, err := store.Finish(id, uuid.New(), func() error {
		published = true
		return nil
	})
	if err != nil {
		t.Fatalf("Finish error = %v", err)
	}
	if owned || published {
		t.Fatalf("Finish = owned %v published %v, want false/false", owned, published)
	}
	got, ok, err := store.Read(id)
	if err != nil || !ok {
		t.Fatalf("Read = (%#v, %v, %v)", got, ok, err)
	}
	if got.ActiveRunID != current.ActiveRunID {
		t.Fatalf("ActiveRunID = %s, want %s", got.ActiveRunID, current.ActiveRunID)
	}
}
