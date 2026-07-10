// Package mediafont provides the bundled font used by generated media.
package mediafont

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	FamilyName     = "Montserrat ExtraBold"
	FileName       = "Montserrat-ExtraBold.ttf"
	Version        = "v7.222"
	SourceCommit   = "5dae7a4ef9c0bf9fe48dc54fd1076eefaa0a8c7e"
	EmbeddedSHA256 = "1b364c3400bf7b1cc2c47a25dd0d3edd8331da451412aa5539080f78f8f70b63"
)

//go:embed Montserrat-ExtraBold.ttf
var montserratExtraBold []byte

var materializeMu sync.Mutex

// Materialize writes the embedded font to a stable per-user cache path and
// returns the TTF path for FFmpeg. A missing user cache falls back to the
// process temp root. Existing files are checksum-verified before reuse.
func Materialize() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil || root == "" {
		root = os.TempDir()
	}
	if root == "" {
		return "", fmt.Errorf("materialize Montserrat ExtraBold: no user cache or temp directory available")
	}
	return materializeAt(filepath.Join(root, "FragForge", "fonts", Version))
}

func materializeAt(dir string) (string, error) {
	materializeMu.Lock()
	defer materializeMu.Unlock()

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("materialize Montserrat ExtraBold: create cache directory: %w", err)
	}
	target := filepath.Join(dir, FileName)
	match, err := fileMatchesEmbedded(target)
	if err == nil && match {
		return target, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("materialize Montserrat ExtraBold: inspect cached font: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".montserrat-*.ttf")
	if err != nil {
		return "", fmt.Errorf("materialize Montserrat ExtraBold: create temporary font: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return "", fmt.Errorf("materialize Montserrat ExtraBold: set font permissions: %w", err)
	}
	if _, err := tmp.Write(montserratExtraBold); err != nil {
		tmp.Close()
		return "", fmt.Errorf("materialize Montserrat ExtraBold: write font: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", fmt.Errorf("materialize Montserrat ExtraBold: sync font: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("materialize Montserrat ExtraBold: close font: %w", err)
	}
	match, err = fileMatchesEmbedded(target)
	if err == nil && match {
		return target, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("materialize Montserrat ExtraBold: recheck cached font: %w", err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		match, verifyErr := fileMatchesEmbedded(target)
		if verifyErr == nil && match {
			return target, nil
		}
		if verifyErr != nil && !os.IsNotExist(verifyErr) {
			return "", fmt.Errorf("materialize Montserrat ExtraBold: install cached font: %w (verify competing target: %v)", err, verifyErr)
		}
		return "", fmt.Errorf("materialize Montserrat ExtraBold: install cached font: %w", err)
	}
	return target, nil
}

func fileMatchesEmbedded(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return fmt.Sprintf("%x", h.Sum(nil)) == EmbeddedSHA256, nil
}
