package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCapabilitiesJSONReportsReadyTools(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ZV_RECORDER_PATH", "ZV_HLAE_PATH", "ZV_CS2_PATH", "ZV_COMPOSER_PATH", "ZV_EDITOR_PATH", "ZV_FFMPEG_PATH", "ZV_FFPROBE_PATH"} {
		path := filepath.Join(dir, name+".exe")
		if err := os.WriteFile(path, []byte("stub"), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(name, path)
	}

	var stdout, stderr strings.Builder
	code := runCapabilities([]string{"--format", "json"}, &stdout, &stderr)
	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	var report localCapabilities
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if !report.LocalStudioReady || !report.Record.Ready || !report.Compose.Ready || !report.Render.Ready {
		t.Fatalf("report = %#v, want all local stages ready", report)
	}
	for _, group := range []localCapabilityGroup{report.Record, report.Compose, report.Render} {
		for _, tool := range group.Tools {
			if tool.Source != "env" || !tool.Configured || !tool.Accessible || tool.Path == "" {
				t.Fatalf("tool = %#v, want accessible explicit path", tool)
			}
		}
	}
}

func TestRunCapabilitiesJSONReportsMissingToolsWithoutFailing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.exe")
	for _, name := range []string{"ZV_RECORDER_PATH", "ZV_HLAE_PATH", "ZV_CS2_PATH", "ZV_COMPOSER_PATH", "ZV_EDITOR_PATH", "ZV_FFMPEG_PATH", "ZV_FFPROBE_PATH"} {
		t.Setenv(name, missing)
	}

	var stdout, stderr strings.Builder
	code := runCapabilities([]string{"--format=json"}, &stdout, &stderr)
	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	var report localCapabilities
	if err := json.Unmarshal([]byte(stdout.String()), &report); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if report.LocalStudioReady || report.Record.Ready || report.Compose.Ready || report.Render.Ready {
		t.Fatalf("report = %#v, want unavailable tools reported as normal state", report)
	}
}

func TestRunCapabilitiesRejectsUnexpectedArgs(t *testing.T) {
	var stdout, stderr strings.Builder
	code := runCapabilities([]string{"extra"}, &stdout, &stderr)
	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if got, want := stderr.String(), "error: unexpected extra args for \"capabilities\"\n"+capabilitiesUsage; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}
