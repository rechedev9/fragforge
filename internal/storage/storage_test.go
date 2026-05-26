package storage

import (
	"bytes"
	"io"
	"os"
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
	exists, err := store.Exists("demos/abc.dem")
	if err != nil {
		t.Fatalf("Exists error = %v", err)
	}
	if !exists {
		t.Fatal("Exists = false, want true")
	}
}

func TestLocalOpenMissingReturnsErrNotExist(t *testing.T) {
	store, _ := NewLocal(t.TempDir())
	_, err := store.Open("nope.dem")
	if !os.IsNotExist(err) {
		t.Errorf("expected file-not-found error, got %v", err)
	}
	exists, err := store.Exists("nope.dem")
	if err != nil {
		t.Fatalf("Exists(missing) error = %v", err)
	}
	if exists {
		t.Fatal("Exists(missing) = true, want false")
	}
}

func TestLocalRejectsEscapingKeys(t *testing.T) {
	store, _ := NewLocal(t.TempDir())
	for _, key := range []string{
		"../escaped.dem",
		"demos/../../escaped.dem",
		"/absolute.dem",
	} {
		err := store.Put(key, bytes.NewReader([]byte("x")))
		if err == nil {
			t.Errorf("expected error rejecting key %q", key)
		}
	}
}

func TestLocalAllowsDotsInsideFileName(t *testing.T) {
	store, _ := NewLocal(t.TempDir())
	if err := store.Put("demos/match..v1.dem", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("Put with dots in file name error = %v", err)
	}
}
