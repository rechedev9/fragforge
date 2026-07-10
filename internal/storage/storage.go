// Package storage abstracts where the orchestrator keeps demo files and
// other artifacts. V1 ships a Local filesystem implementation; future
// slices can add an S3/MinIO backend behind the same interface.
package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// IsNotExist reports whether err means the requested storage key is absent.
func IsNotExist(err error) bool {
	return os.IsNotExist(err)
}

// Storage is the minimal contract for reading and writing artifact blobs.
type Storage interface {
	Put(key string, r io.Reader) error
	Open(key string) (io.ReadCloser, error)
	Exists(key string) (bool, error)
}

// Local implements Storage backed by the local filesystem under a root dir.
type Local struct {
	root string
}

// NewLocal returns a Local rooted at the given absolute or relative path.
// The root directory is created if it does not exist.
func NewLocal(root string) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, err
	}
	return &Local{root: abs}, nil
}

// Put atomically replaces the file at key with r's contents. Readers see either
// the previous complete artifact or the new complete artifact, never a partial
// write.
func (l *Local) Put(key string, r io.Reader) error {
	path, err := l.resolve(key)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("storage: create artifact directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("storage: create temporary artifact: %w", err)
	}
	tempPath := temp.Name()
	keepTemp := true
	defer func() {
		if keepTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := io.Copy(temp, r); err != nil {
		return fmt.Errorf("storage: copy temporary artifact: %w", errors.Join(err, temp.Close()))
	}
	if err := temp.Sync(); err != nil {
		return fmt.Errorf("storage: sync temporary artifact: %w", errors.Join(err, temp.Close()))
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("storage: close temporary artifact: %w", err)
	}
	if err := replaceLocalFile(tempPath, path); err != nil {
		return fmt.Errorf("storage: replace artifact: %w", err)
	}
	keepTemp = false
	return nil
}

// Open returns a ReadCloser for the file at key.
func (l *Local) Open(key string) (io.ReadCloser, error) {
	path, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- path is resolved under Local.root by resolve.
	return openLocalFile(path)
}

// List returns the base file names present directly under the directory at key,
// non-recursively. A missing directory yields no names (not an error), so
// callers can list an artifact dir a stage has not written yet.
func (l *Local) List(key string) ([]string, error) {
	dir, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// Delete removes the file at key inside the storage root. A missing key is
// not an error, so deletes are idempotent and safe to retry.
func (l *Local) Delete(key string) error {
	path, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Exists reports whether key exists inside the storage root.
func (l *Local) Exists(key string) (bool, error) {
	path, err := l.resolve(key)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (l *Local) resolve(key string) (string, error) {
	if strings.HasPrefix(key, "/") || strings.HasPrefix(key, `\`) {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || filepath.IsAbs(clean) {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage: invalid key %q", key)
	}
	abs := filepath.Join(l.root, clean)
	rel, err := filepath.Rel(l.root, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", errors.New("storage: key escapes root")
	}
	return abs, nil
}
