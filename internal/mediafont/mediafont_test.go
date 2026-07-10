package mediafont

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestEmbeddedFontChecksum(t *testing.T) {
	got := fmt.Sprintf("%x", sha256.Sum256(montserratExtraBold))
	if got != EmbeddedSHA256 {
		t.Fatalf("embedded font SHA256 = %q, want %q", got, EmbeddedSHA256)
	}
}

func TestMaterializeAtIsStableAndRepairsCorruptCache(t *testing.T) {
	dir := t.TempDir()
	path, err := materializeAt(dir)
	if err != nil {
		t.Fatalf("materializeAt error = %v", err)
	}
	if path != filepath.Join(dir, FileName) {
		t.Fatalf("font path = %q, want stable cache path", path)
	}
	if err := os.WriteFile(path, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := materializeAt(dir)
	if err != nil {
		t.Fatalf("materializeAt repair error = %v", err)
	}
	match, err := fileMatchesEmbedded(got)
	if err != nil || !match {
		t.Fatalf("repaired font match = %v, error = %v", match, err)
	}
}

func TestMaterializeAtConcurrentCallsShareOneFile(t *testing.T) {
	dir := t.TempDir()
	const calls = 12
	paths := make(chan string, calls)
	errs := make(chan error, calls)
	var wg sync.WaitGroup
	for range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := materializeAt(dir)
			paths <- path
			errs <- err
		}()
	}
	wg.Wait()
	close(paths)
	close(errs)

	want := filepath.Join(dir, FileName)
	for err := range errs {
		if err != nil {
			t.Fatalf("materializeAt error = %v", err)
		}
	}
	for path := range paths {
		if path != want {
			t.Fatalf("font path = %q, want %q", path, want)
		}
	}
}

func TestMaterializeAtReturnsUsefulCacheError(t *testing.T) {
	parent := t.TempDir()
	blocked := filepath.Join(parent, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := materializeAt(filepath.Join(blocked, "fonts"))
	if err == nil || !strings.Contains(err.Error(), "create cache directory") {
		t.Fatalf("materializeAt error = %v, want cache directory context", err)
	}
}
