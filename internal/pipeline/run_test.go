package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if len(result.Warnings) != 0 {
		t.Fatalf("Warnings = %v", result.Warnings)
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

func TestRunPropagatesResultWarnings(t *testing.T) {
	dir := t.TempDir()
	recorder := fakeCommandWithWarning(t, dir, "recorder", "low disk")
	composer := fakeCommandWithWarning(t, dir, "composer", "duration drift")

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
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	joined := strings.Join(result.Warnings, "\n")
	for _, want := range []string{"recording: low disk", "composition: duration drift"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings missing %q:\n%s", want, joined)
		}
	}
}

func TestRunFailsOnSubResultError(t *testing.T) {
	dir := t.TempDir()
	recorder := fakeCommandWithResultError(t, dir, "recorder", "bad recording")
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
		t.Fatal("Run error = nil, want recording result error")
	}
	if !strings.Contains(result.Error, "bad recording") {
		t.Fatalf("Result.Error = %q", result.Error)
	}
}

func fakeCommand(t *testing.T, dir, name string) string {
	t.Helper()
	return writeCommand(t, dir, name, false, "", "")
}

func fakeFailingCommand(t *testing.T, dir, name string) string {
	t.Helper()
	return writeCommand(t, dir, name, true, "", "")
}

func fakeCommandWithWarning(t *testing.T, dir, name, warning string) string {
	t.Helper()
	return writeCommand(t, dir, name, false, warning, "")
}

func fakeCommandWithResultError(t *testing.T, dir, name, resultError string) string {
	t.Helper()
	return writeCommand(t, dir, name, false, "", resultError)
}

func writeCommand(t *testing.T, dir, name string, fail bool, warning, resultError string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, name+".cmd")
		exit := "0"
		if fail {
			exit = "7"
		}
		body := fakeWindowsCommand(name, exit, warning, resultError)
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
	body := fakeUnixCommand(name, exit, warning, resultError)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeWindowsCommand(name, exit, warning, resultError string) string {
	return "@echo off\r\n" +
		"set out=\r\n" +
		":loop\r\n" +
		"if \"%~1\"==\"\" goto done\r\n" +
		"if \"%~1\"==\"--out\" set \"out=%~2\"\r\n" +
		"shift\r\n" +
		"goto loop\r\n" +
		":done\r\n" +
		"if \"" + name + "\"==\"recorder\" goto recorder\r\n" +
		"if \"" + name + "\"==\"composer\" goto composer\r\n" +
		"goto finish\r\n" +
		":recorder\r\n" +
		"if not exist \"%out%\" mkdir \"%out%\"\r\n" +
		">\"%out%\\recording-result.json\" echo {\"warnings\":[\"" + warning + "\"],\"error\":\"" + resultError + "\"}\r\n" +
		"goto finish\r\n" +
		":composer\r\n" +
		"for %%I in (\"%out%\") do set \"dir=%%~dpI\"\r\n" +
		"if not exist \"%dir%\" mkdir \"%dir%\"\r\n" +
		">\"%out%\" echo fake\r\n" +
		">\"%dir%composition-result.json\" echo {\"warnings\":[\"" + warning + "\"],\"error\":\"" + resultError + "\"}\r\n" +
		":finish\r\n" +
		"echo " + name + "\r\n" +
		"exit /b " + exit + "\r\n"
}

func fakeUnixCommand(name, exit, warning, resultError string) string {
	return "#!/bin/sh\n" +
		"out=\n" +
		"while [ \"$#\" -gt 0 ]; do\n" +
		"  if [ \"$1\" = \"--out\" ]; then shift; out=\"$1\"; fi\n" +
		"  shift\n" +
		"done\n" +
		"if [ \"" + name + "\" = \"recorder\" ]; then\n" +
		"  mkdir -p \"$out\"\n" +
		"  printf '{\"warnings\":[\"" + warning + "\"],\"error\":\"" + resultError + "\"}\\n' > \"$out/recording-result.json\"\n" +
		"fi\n" +
		"if [ \"" + name + "\" = \"composer\" ]; then\n" +
		"  mkdir -p \"$(dirname \"$out\")\"\n" +
		"  printf fake > \"$out\"\n" +
		"  printf '{\"warnings\":[\"" + warning + "\"],\"error\":\"" + resultError + "\"}\\n' > \"$(dirname \"$out\")/composition-result.json\"\n" +
		"fi\n" +
		"echo " + name + "\n" +
		"exit " + exit + "\n"
}
