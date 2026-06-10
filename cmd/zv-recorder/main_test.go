package main

import (
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/recording"
)

func TestCS2LaunchCommandLineUsesWindowedMode(t *testing.T) {
	plan := recording.RecordingPlan{
		DemoPath: `C:\demos\match.dem`,
		Stream: recording.StreamConfig{
			Width:  1920,
			Height: 1080,
		},
	}

	got := cs2LaunchCommandLine(plan, `C:\runs\recording.js`)

	if !strings.Contains(got, "-windowed") {
		t.Fatalf("cs2LaunchCommandLine() = %q, want -windowed", got)
	}
	if strings.Index(got, "-windowed") > strings.Index(got, "-w 1920") {
		t.Fatalf("cs2LaunchCommandLine() = %q, want -windowed before resolution flags", got)
	}
}
