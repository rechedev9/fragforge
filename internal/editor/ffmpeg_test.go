package editor

import (
	"os"
	"strings"
	"testing"
)

func TestCommandWithFilterComplexScriptKeepsShortFilterInline(t *testing.T) {
	command := []string{"ffmpeg", "-filter_complex", "scale=1080:1920", "out.mp4"}

	got, cleanup, err := commandWithFilterComplexScript(command)
	if err != nil {
		t.Fatalf("commandWithFilterComplexScript() error = %v", err)
	}
	defer cleanup()

	if strings.Join(got, "\x00") != strings.Join(command, "\x00") {
		t.Fatalf("commandWithFilterComplexScript() = %#v, want original command", got)
	}
}

func TestCommandWithFilterComplexScriptSpillsLongFilter(t *testing.T) {
	filter := strings.Repeat("scale=1080:1920,", filterComplexScriptThreshold)
	command := []string{"ffmpeg", "-filter_complex", filter, "-map", "[v]", "out.mp4"}

	got, cleanup, err := commandWithFilterComplexScript(command)
	if err != nil {
		t.Fatalf("commandWithFilterComplexScript() error = %v", err)
	}

	if got[1] != "-filter_complex_script" {
		t.Fatalf("filter flag = %q, want -filter_complex_script", got[1])
	}
	if got[2] == filter {
		t.Fatalf("filter script path still contains inline filter")
	}
	b, err := os.ReadFile(got[2])
	if err != nil {
		t.Fatalf("read filter script: %v", err)
	}
	if string(b) != filter {
		t.Fatalf("filter script contents changed")
	}

	cleanup()
	if _, err := os.Stat(got[2]); !os.IsNotExist(err) {
		t.Fatalf("filter script still exists after cleanup: %v", err)
	}
}
