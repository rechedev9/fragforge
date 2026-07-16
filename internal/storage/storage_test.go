package storage

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"
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

func TestLocalPutKeepsCompletePreviousContentVisibleUntilReplacement(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal error = %v", err)
	}

	const key = "streams/render/status.json"
	oldContent := []byte(`{"status":"rendering","progress":25}`)
	newContent := []byte(`{"status":"done","progress":100}`)
	if err := store.Put(key, bytes.NewReader(oldContent)); err != nil {
		t.Fatalf("initial Put error = %v", err)
	}

	release := make(chan struct{})
	reader := &blockingReader{
		reader:  bytes.NewReader(newContent),
		started: make(chan struct{}),
		release: release,
	}
	putDone := make(chan error, 1)
	go func() {
		putDone <- store.Put(key, reader)
	}()

	select {
	case <-reader.started:
	case err := <-putDone:
		close(release)
		t.Fatalf("replacement Put finished before reading input: %v", err)
	case <-time.After(2 * time.Second):
		close(release)
		if _, finished := waitForPut(putDone); !finished {
			t.Fatal("replacement Put remained blocked after releasing its reader")
		}
		t.Fatal("replacement Put did not start copying")
	}

	oldHandle, err := store.Open(key)
	if err != nil {
		close(release)
		if _, finished := waitForPut(putDone); !finished {
			t.Fatal("replacement Put remained blocked after Open failed")
		}
		t.Fatalf("Open during Put error = %v", err)
	}
	close(release)
	putErr, finished := waitForPut(putDone)
	if !finished {
		closeErr := oldHandle.Close()
		cleanupErr, cleanupFinished := waitForPut(putDone)
		t.Fatalf("replacement Put did not finish while old handle remained open (close error: %v, finished after close: %v, Put error: %v)", closeErr, cleanupFinished, cleanupErr)
	}

	during, readErr := io.ReadAll(oldHandle)
	closeErr := oldHandle.Close()
	if readErr != nil {
		t.Fatalf("read old handle after Put error = %v", readErr)
	}
	if !bytes.Equal(during, oldContent) {
		t.Fatalf("old handle after Put returned %q, want old complete content %q", during, oldContent)
	}
	if closeErr != nil {
		t.Fatalf("close old handle error = %v", closeErr)
	}
	if putErr != nil {
		t.Fatalf("replacement Put error = %v", putErr)
	}
	after, err := readLocalArtifact(store, key)
	if err != nil {
		t.Fatalf("Open after Put error = %v", err)
	}
	if !bytes.Equal(after, newContent) {
		t.Fatalf("Open after Put returned %q, want new complete content %q", after, newContent)
	}
}

func waitForPut(done <-chan error) (error, bool) {
	select {
	case err := <-done:
		return err, true
	case <-time.After(2 * time.Second):
		return nil, false
	}
}

type blockingReader struct {
	reader  *bytes.Reader
	started chan struct{}
	release chan struct{}
	blocked bool
}

func (r *blockingReader) Read(p []byte) (int, error) {
	if !r.blocked {
		r.blocked = true
		close(r.started)
		<-r.release
	}
	return r.reader.Read(p)
}

func readLocalArtifact(store *Local, key string) ([]byte, error) {
	rc, err := store.Open(key)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(rc)
	closeErr := rc.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
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

func TestLocalResolvePathStaysUnderRootAndRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocal(dir)
	if err != nil {
		t.Fatalf("NewLocal error = %v", err)
	}
	want := []byte("stream bytes")
	if err := store.Put("streams/source.mp4", bytes.NewReader(want)); err != nil {
		t.Fatalf("Put error = %v", err)
	}

	path, err := store.ResolvePath("streams/source.mp4")
	if err != nil {
		t.Fatalf("ResolvePath error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read resolved path: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("resolved file content = %q, want %q", got, want)
	}

	if _, err := store.ResolvePath("../escaped.mp4"); err == nil {
		t.Error("expected error resolving escaping key")
	}
}

func TestLocalDeleteTreeRemovesNestedDirIdempotently(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal error = %v", err)
	}
	keys := []string{
		"jobs/job-1/recording/result.json",
		"jobs/job-1/renders/viral/video.mp4",
	}
	for _, key := range keys {
		if err := store.Put(key, bytes.NewReader([]byte("x"))); err != nil {
			t.Fatalf("Put(%q) error = %v", key, err)
		}
	}
	// A sibling job must survive the delete.
	if err := store.Put("jobs/job-2/keep.json", bytes.NewReader([]byte("y"))); err != nil {
		t.Fatalf("Put sibling error = %v", err)
	}

	if err := store.DeleteTree("jobs/job-1"); err != nil {
		t.Fatalf("DeleteTree error = %v", err)
	}
	for _, key := range keys {
		exists, err := store.Exists(key)
		if err != nil {
			t.Fatalf("Exists(%q) error = %v", key, err)
		}
		if exists {
			t.Errorf("key %q still present after DeleteTree", key)
		}
	}
	if exists, _ := store.Exists("jobs/job-2/keep.json"); !exists {
		t.Error("sibling job removed by DeleteTree")
	}

	// Deleting a now-missing tree is a no-op.
	if err := store.DeleteTree("jobs/job-1"); err != nil {
		t.Fatalf("second DeleteTree error = %v", err)
	}
}

func TestLocalDeleteTreeRejectsEmptyAndTraversalKeys(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocal(dir)
	if err != nil {
		t.Fatalf("NewLocal error = %v", err)
	}
	if err := store.Put("jobs/job-1/keep.json", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("Put error = %v", err)
	}
	for _, key := range []string{"", "..", "../escaped", "jobs/../.."} {
		if err := store.DeleteTree(key); err == nil {
			t.Errorf("DeleteTree(%q) = nil, want error", key)
		}
	}
	// The storage root and its contents must be untouched by the rejected keys.
	if exists, _ := store.Exists("jobs/job-1/keep.json"); !exists {
		t.Error("a rejected DeleteTree removed stored content")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("storage root missing after rejected DeleteTree: %v", err)
	}
}

func TestLocalAllowsDotsInsideFileName(t *testing.T) {
	store, _ := NewLocal(t.TempDir())
	if err := store.Put("demos/match..v1.dem", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatalf("Put with dots in file name error = %v", err)
	}
}
