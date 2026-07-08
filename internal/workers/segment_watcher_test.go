package workers

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/storage"
)

// countingStore wraps a Storage and counts Put calls per key, so tests can
// assert the watcher uploads each clip exactly once.
type countingStore struct {
	storage.Storage
	puts map[string]int
}

func newCountingStore(t *testing.T) *countingStore {
	t.Helper()
	local, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	return &countingStore{Storage: local, puts: map[string]int{}}
}

func (c *countingStore) Put(key string, r io.Reader) error {
	c.puts[key]++
	return c.Storage.Put(key, r)
}

func writeClip(t *testing.T, dir, name string, size int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), bytes.Repeat([]byte("x"), size), 0o600); err != nil {
		t.Fatal(err)
	}
}

func segmentKey(t *testing.T, id uuid.UUID, segmentID string) string {
	t.Helper()
	key, err := artifacts.SegmentClipKey(id, segmentID)
	if err != nil {
		t.Fatalf("SegmentClipKey(%q): %v", segmentID, err)
	}
	return key
}

// TestSegmentClipWatcherGrowingDir drives the watcher tick by tick over a
// recorder segments dir that grows the way a live capture grows it: a clip
// appears, keeps growing for a while, stabilizes, and a later clip follows.
// Only stable clips upload, each exactly once.
func TestSegmentClipWatcherGrowingDir(t *testing.T) {
	store := newCountingStore(t)
	jobID := uuid.New()
	dir := filepath.Join(t.TempDir(), "segments")
	w := newSegmentClipWatcher(store, jobID, dir)

	// No segments dir yet: nothing uploads, nothing panics.
	if got := w.tick(); got != nil {
		t.Fatalf("tick on missing dir = %v, want nil", got)
	}

	// First sighting of a clip is never uploaded (it may still be growing).
	writeClip(t, dir, "s1.mp4", 4)
	if got := w.tick(); got != nil {
		t.Fatalf("tick on first sighting = %v, want nil", got)
	}

	// The clip grew since the last tick: still not stable.
	writeClip(t, dir, "s1.mp4", 8)
	if got := w.tick(); got != nil {
		t.Fatalf("tick on growing clip = %v, want nil", got)
	}

	// Unchanged across two ticks: stable, upload once.
	got := w.tick()
	if len(got) != 1 || got[0] != "s1" {
		t.Fatalf("tick on stable clip = %v, want [s1]", got)
	}
	if ok, _ := store.Exists(segmentKey(t, jobID, "s1")); !ok {
		t.Fatal("s1 clip not uploaded to storage")
	}

	// A second clip lands later; s1 is never re-uploaded.
	writeClip(t, dir, "s2.mp4", 6)
	if got := w.tick(); got != nil {
		t.Fatalf("tick on first sighting of s2 = %v, want nil", got)
	}
	got = w.tick()
	if len(got) != 1 || got[0] != "s2" {
		t.Fatalf("tick on stable s2 = %v, want [s2]", got)
	}
	if n := store.puts[segmentKey(t, jobID, "s1")]; n != 1 {
		t.Fatalf("s1 uploaded %d times, want exactly 1", n)
	}
	if n := store.puts[segmentKey(t, jobID, "s2")]; n != 1 {
		t.Fatalf("s2 uploaded %d times, want exactly 1", n)
	}

	// Further ticks stay quiet.
	if got := w.tick(); got != nil {
		t.Fatalf("idle tick = %v, want nil", got)
	}
}

func TestSegmentClipWatcherSkipsUnstableAndJunk(t *testing.T) {
	store := newCountingStore(t)
	jobID := uuid.New()
	dir := filepath.Join(t.TempDir(), "segments")

	// Empty files, non-mp4 temp names, and subdirectories never upload.
	writeClip(t, dir, "empty.mp4", 0)
	writeClip(t, dir, "s1.mp4.part", 8)
	if err := os.MkdirAll(filepath.Join(dir, "nested.mp4"), 0o750); err != nil {
		t.Fatal(err)
	}

	w := newSegmentClipWatcher(store, jobID, dir)
	for i := 0; i < 3; i++ {
		if got := w.tick(); got != nil {
			t.Fatalf("tick %d = %v, want nil", i, got)
		}
	}
	if len(store.puts) != 0 {
		t.Fatalf("puts = %v, want none", store.puts)
	}
}

// TestSegmentClipWatcherOverwriteIsNotAnError: uploading a clip whose key
// already exists in storage (e.g. a re-record of the same segment) overwrites
// idempotently instead of erroring or duplicating.
func TestSegmentClipWatcherOverwriteIsNotAnError(t *testing.T) {
	store := newCountingStore(t)
	jobID := uuid.New()
	dir := filepath.Join(t.TempDir(), "segments")

	key := segmentKey(t, jobID, "s1")
	if err := store.Storage.Put(key, bytes.NewReader([]byte("old"))); err != nil {
		t.Fatal(err)
	}

	w := newSegmentClipWatcher(store, jobID, dir)
	writeClip(t, dir, "s1.mp4", 8)
	w.tick() // first sighting
	got := w.tick()
	if len(got) != 1 || got[0] != "s1" {
		t.Fatalf("tick = %v, want [s1]", got)
	}
	rc, err := store.Open(key)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 8 {
		t.Fatalf("stored clip size = %d, want the new 8-byte clip", len(b))
	}
	if n := store.puts[key]; n != 1 {
		t.Fatalf("watcher puts = %d, want exactly 1", n)
	}
}
