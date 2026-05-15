package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRunCallsRecorderThenComposer(t *testing.T) {
	dir := t.TempDir()
	recorder := fakeCommand(t, dir, "recorder")
	composer := fakeCommand(t, dir, "composer")

	result, err := Run(context.Background(), Config{
		KillPlanPath:   filepath.Join(dir, "plan.json"),
		DemoPath:       filepath.Join(dir, "demo.dem"),
		OutputDir:      filepath.Join(dir, "out"),
		HLAEPath:       filepath.Join(dir, "HLAE.exe"),
		CS2Path:        filepath.Join(dir, "cs2.exe"),
		RecorderPath:   recorder,
		ComposerPath:   composer,
		RecordTimeout:  "1m",
		ComposeTimeout: "1m",
	})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(result.Steps))
	}
	if result.Steps[0].Name != "record" || result.Steps[1].Name != "compose" {
		t.Fatalf("step names = %s, %s", result.Steps[0].Name, result.Steps[1].Name)
	}
	if result.RecordingResult != filepath.Join(dir, "out", "recording", "recording-result.json") {
		t.Fatalf("RecordingResult = %q", result.RecordingResult)
	}
	if result.FinalOutput != filepath.Join(dir, "out", "final.mp4") {
		t.Fatalf("FinalOutput = %q", result.FinalOutput)
	}
}

func TestRunStopsWhenRecorderFails(t *testing.T) {
	dir := t.TempDir()
	recorder := fakeFailingCommand(t, dir, "recorder")
	composer := fakeCommand(t, dir, "composer")

	result, err := Run(context.Background(), Config{
		KillPlanPath:   "plan.json",
		DemoPath:       "demo.dem",
		OutputDir:      filepath.Join(dir, "out"),
		HLAEPath:       "HLAE.exe",
		CS2Path:        "cs2.exe",
		RecorderPath:   recorder,
		ComposerPath:   composer,
		RecordTimeout:  "1m",
		ComposeTimeout: "1m",
	})
	if err == nil {
		t.Fatal("Run error = nil, want recorder failure")
	}
	if len(result.Steps) != 1 || result.Steps[0].Name != "record" {
		t.Fatalf("steps = %#v", result.Steps)
	}
}

func fakeCommand(t *testing.T, dir, name string) string {
	t.Helper()
	return writeCommand(t, dir, name, false)
}

func fakeFailingCommand(t *testing.T, dir, name string) string {
	t.Helper()
	return writeCommand(t, dir, name, true)
}

func writeCommand(t *testing.T, dir, name string, fail bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, name+".cmd")
		exit := "0"
		if fail {
			exit = "7"
		}
		body := "@echo off\r\necho " + name + "\r\nexit /b " + exit + "\r\n"
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	path := filepath.Join(dir, name)
	exit := "0"
	if fail {
		exit = "7"
	}
	body := "#!/bin/sh\necho " + name + "\nexit " + exit + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
