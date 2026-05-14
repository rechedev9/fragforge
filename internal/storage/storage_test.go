package storage

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestLocalPutAndOpenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocal(dir)
	if err != nil {
		t.Fatalf("NewLocal error = %v", err)
	}

	want := []byte("demo bytes")
	if err := store.Put("demos/abc.dem", bytes.NewReader(want)); err != nil {
		t.Fatalf("Put error = %v", err)
	}

	rc, err := store.Open("demos/abc.dem")
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if !bytes.Equal(got, want) {
		t.Errorf("Open returned %q, want %q", got, want)
	}
}

func TestLocalOpenMissingReturnsErrNotExist(t *testing.T) {
	store, _ := NewLocal(t.TempDir())
	_, err := store.Open("nope.dem")
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Errorf("expected file-not-found error, got %v", err)
	}
}

func TestLocalRejectsEscapingKeys(t *testing.T) {
	store, _ := NewLocal(t.TempDir())
	err := store.Put("../escaped.dem", bytes.NewReader([]byte("x")))
	if err == nil {
		t.Error("expected error rejecting key with ..")
	}
}
