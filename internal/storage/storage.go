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

// Put writes r's contents to the file at key inside the storage root.
func (l *Local) Put(key string, r io.Reader) error {
	path, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	// #nosec G304 -- path is resolved under Local.root by resolve.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

// Open returns a ReadCloser for the file at key.
func (l *Local) Open(key string) (io.ReadCloser, error) {
	path, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- path is resolved under Local.root by resolve.
	return os.Open(path)
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
