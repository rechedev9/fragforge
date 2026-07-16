// Package mediafont provides the bundled fonts used by generated media.
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
	// FamilyName is the OpenType family (name ID 1) shared by both bundled
	// faces. libass resolves the upright face from FamilyName with Italic:0
	// and the italic face from FamilyName with Italic:1, so an ASS style asks
	// for the italic by setting Italic in the style, not by naming a separate
	// family.
	FamilyName = "Montserrat ExtraBold"
	FileName   = "Montserrat-ExtraBold.ttf"

	// ItalicFileName is the italic face materialized alongside the upright
	// one. ItalicFullName is its human-facing full name (name ID 4); it is not
	// the libass match key — the italic shares FamilyName and is selected via
	// the Italic flag.
	ItalicFileName = "Montserrat-ExtraBoldItalic.ttf"
	ItalicFullName = "Montserrat ExtraBold Italic"

	Version      = "v7.222"
	SourceCommit = "5dae7a4ef9c0bf9fe48dc54fd1076eefaa0a8c7e"

	EmbeddedSHA256       = "1b364c3400bf7b1cc2c47a25dd0d3edd8331da451412aa5539080f78f8f70b63"
	ItalicEmbeddedSHA256 = "0984784ee9883389e76bf2c7ceeeda848af26535ec111109fa92e18d448a4759"
)

//go:embed Montserrat-ExtraBold.ttf
var montserratExtraBold []byte

//go:embed Montserrat-ExtraBoldItalic.ttf
var montserratExtraBoldItalic []byte

// bundledFonts lists every face materialized into the fonts directory, in a
// stable order. The first entry is the primary face whose path Materialize
// returns.
var bundledFonts = []struct {
	fileName string
	sha256   string
	data     []byte
}{
	{FileName, EmbeddedSHA256, montserratExtraBold},
	{ItalicFileName, ItalicEmbeddedSHA256, montserratExtraBoldItalic},
}

var materializeMu sync.Mutex

// Materialize writes the embedded fonts to a stable per-user cache path and
// returns the primary (upright) TTF path for FFmpeg; the italic face is
// materialized into the same directory, so filepath.Dir of the returned path
// is the fontsdir libass should scan. A missing user cache falls back to the
// process temp root. Existing files are checksum-verified before reuse.
func Materialize() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil || root == "" {
		root = os.TempDir()
	}
	if root == "" {
		return "", fmt.Errorf("materialize %s: no user cache or temp directory available", FamilyName)
	}
	return materializeAt(filepath.Join(root, "FragForge", "fonts", Version))
}

func materializeAt(dir string) (string, error) {
	materializeMu.Lock()
	defer materializeMu.Unlock()

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("materialize %s: create cache directory: %w", FamilyName, err)
	}
	for _, font := range bundledFonts {
		if err := materializeFile(dir, font.fileName, font.sha256, font.data); err != nil {
			return "", err
		}
	}
	// bundledFonts[0] is the primary upright face; its directory is the fontsdir.
	return filepath.Join(dir, bundledFonts[0].fileName), nil
}

func materializeFile(dir, fileName, wantSHA string, data []byte) error {
	target := filepath.Join(dir, fileName)
	match, err := fileMatchesSHA(target, wantSHA)
	if err == nil && match {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("materialize %s: inspect cached font: %w", fileName, err)
	}

	tmp, err := os.CreateTemp(dir, ".montserrat-*.ttf")
	if err != nil {
		return fmt.Errorf("materialize %s: create temporary font: %w", fileName, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("materialize %s: set font permissions: %w", fileName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("materialize %s: write font: %w", fileName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("materialize %s: sync font: %w", fileName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("materialize %s: close font: %w", fileName, err)
	}
	match, err = fileMatchesSHA(target, wantSHA)
	if err == nil && match {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("materialize %s: recheck cached font: %w", fileName, err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		match, verifyErr := fileMatchesSHA(target, wantSHA)
		if verifyErr == nil && match {
			return nil
		}
		if verifyErr != nil && !os.IsNotExist(verifyErr) {
			return fmt.Errorf("materialize %s: install cached font: %w (verify competing target: %v)", fileName, err, verifyErr)
		}
		return fmt.Errorf("materialize %s: install cached font: %w", fileName, err)
	}
	return nil
}

func fileMatchesSHA(path, wantSHA string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return fmt.Sprintf("%x", h.Sum(nil)) == wantSHA, nil
}
