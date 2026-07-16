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

func TestEmbeddedFontChecksums(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"upright", montserratExtraBold, EmbeddedSHA256},
		{"italic", montserratExtraBoldItalic, ItalicEmbeddedSHA256},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fmt.Sprintf("%x", sha256.Sum256(tt.data))
			if got != tt.want {
				t.Fatalf("embedded font SHA256 = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMaterializeAtWritesBothFacesAndRepairsCorruptCache(t *testing.T) {
	dir := t.TempDir()
	path, err := materializeAt(dir)
	if err != nil {
		t.Fatalf("materializeAt error = %v", err)
	}
	if path != filepath.Join(dir, FileName) {
		t.Fatalf("font path = %q, want stable upright cache path", path)
	}

	// Both faces land in the same directory so filepath.Dir(path) is the fontsdir.
	uprightMatch, err := fileMatchesSHA(filepath.Join(dir, FileName), EmbeddedSHA256)
	if err != nil || !uprightMatch {
		t.Fatalf("upright face match = %v, error = %v", uprightMatch, err)
	}
	italicMatch, err := fileMatchesSHA(filepath.Join(dir, ItalicFileName), ItalicEmbeddedSHA256)
	if err != nil || !italicMatch {
		t.Fatalf("italic face match = %v, error = %v", italicMatch, err)
	}

	// Corrupt each face in turn and confirm materializeAt repairs it back to the
	// embedded bytes, so both the upright and italic faces stay covered.
	faces := []struct {
		name     string
		fileName string
		wantSHA  string
	}{
		{"upright", FileName, EmbeddedSHA256},
		{"italic", ItalicFileName, ItalicEmbeddedSHA256},
	}
	for _, face := range faces {
		t.Run("repairs "+face.name, func(t *testing.T) {
			target := filepath.Join(dir, face.fileName)
			if err := os.WriteFile(target, []byte("corrupt"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := materializeAt(dir); err != nil {
				t.Fatalf("materializeAt repair error = %v", err)
			}
			repaired, err := fileMatchesSHA(target, face.wantSHA)
			if err != nil || !repaired {
				t.Fatalf("repaired %s match = %v, error = %v", face.name, repaired, err)
			}
		})
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
