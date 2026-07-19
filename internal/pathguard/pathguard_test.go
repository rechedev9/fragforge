package pathguard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRejectOutputAliasesDetectsEqualAndHardlinkedInputs(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.dem")
	if err := os.WriteFile(input, []byte("demo"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		output string
	}{
		{name: "equal", output: input},
		{name: "cleaned", output: filepath.Join(dir, ".", "source.dem")},
	}
	hardlink := filepath.Join(dir, "source-link.dem")
	if err := os.Link(input, hardlink); err == nil {
		tests = append(tests, struct {
			name   string
			output string
		}{name: "hardlink", output: hardlink})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RejectOutputAliases(tt.output, Input{Flag: "--demo", Path: input})
			if err == nil || !strings.Contains(err.Error(), "must not overwrite --demo") {
				t.Fatalf("RejectOutputAliases error = %v", err)
			}
		})
	}
}

func TestRejectOutputAliasesAllowsDistinctOutput(t *testing.T) {
	dir := t.TempDir()
	err := RejectOutputAliases(filepath.Join(dir, "players.json"), Input{Flag: "--demo", Path: filepath.Join(dir, "source.dem")})
	if err != nil {
		t.Fatalf("RejectOutputAliases error = %v", err)
	}
}

func TestRejectInputsWithinDirectory(t *testing.T) {
	dir := t.TempDir()
	publishDir := filepath.Join(dir, "run", "shortslistosparasubir")
	inside := filepath.Join(publishDir, "old.mp4")
	outside := filepath.Join(dir, "source.mp4")
	if err := RejectInputsWithinDirectory(publishDir, Input{Flag: "--input", Path: inside}); err == nil || !strings.Contains(err.Error(), "must not be inside publish directory") {
		t.Fatalf("inside error = %v", err)
	}
	if err := RejectInputsWithinDirectory(publishDir, Input{Flag: "--input", Path: outside}); err != nil {
		t.Fatalf("outside error = %v", err)
	}
}

func TestRejectInputsWithinDirectoryAcrossVolumes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-letter volumes only exist on Windows")
	}
	// filepath.Rel returns a hard error across volumes on Windows; an input on a
	// different drive than the output directory is simply not inside it.
	publishDir := `C:\Users\example\run\shortslistosparasubir`
	input := `D:\media\clip.mp4`
	if err := RejectInputsWithinDirectory(publishDir, Input{Flag: "--input", Path: input}); err != nil {
		t.Fatalf("cross-volume error = %v", err)
	}
}
