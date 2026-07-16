package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDetectCaptureToolsEnvWins(t *testing.T) {
	cfg := config{
		RecorderPath: filepath.FromSlash("/explicit/zv-recorder"),
		EditorPath:   filepath.FromSlash("/explicit/zv-editor"),
		FFmpegPath:   filepath.FromSlash("/explicit/ffmpeg"),
	}
	got, src := detectCaptureTools(cfg)

	if got.RecorderPath != filepath.FromSlash("/explicit/zv-recorder") {
		t.Errorf("RecorderPath = %q, want the explicit value kept", got.RecorderPath)
	}
	if got.EditorPath != filepath.FromSlash("/explicit/zv-editor") {
		t.Errorf("EditorPath = %q, want the explicit value kept", got.EditorPath)
	}
	if got.FFmpegPath != filepath.FromSlash("/explicit/ffmpeg") {
		t.Errorf("FFmpegPath = %q, want the explicit value kept", got.FFmpegPath)
	}
	for _, name := range []string{"ZV_RECORDER_PATH", "ZV_EDITOR_PATH", "ZV_FFMPEG_PATH"} {
		if src[name] != "env" {
			t.Errorf("%s source = %q, want env", name, src[name])
		}
	}
	// Unset tools resolve to detected (on a host with the tool) or none, never "env".
	for _, name := range []string{"ZV_HLAE_PATH", "ZV_CS2_PATH", "ZV_FFPROBE_PATH"} {
		if s := src[name]; s != "detected" && s != "none" {
			t.Errorf("%s source = %q, want detected or none", name, s)
		}
	}
}

func TestDetectCaptureToolsEnablesRenderWorkerFromSibling(t *testing.T) {
	// detectSibling probes next to os.Executable(), which under `go test` is the
	// test binary in a scratch dir; plant a zv-editor there to simulate the
	// desktop layout (all pipeline binaries staged in one bin/ directory).
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	name := "zv-editor"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	sibling := filepath.Join(filepath.Dir(exe), name)
	if err := os.WriteFile(sibling, []byte("stub"), 0o700); err != nil {
		t.Skipf("cannot write next to the test binary: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(sibling) })

	got, src := detectCaptureTools(config{})
	if got.EditorPath != sibling {
		t.Errorf("EditorPath = %q, want %q", got.EditorPath, sibling)
	}
	if src["ZV_EDITOR_PATH"] != "detected" {
		t.Errorf("editor source = %q, want detected", src["ZV_EDITOR_PATH"])
	}
	if !got.renderWorkerEnabled() {
		t.Error("render worker disabled, want enabled with detected editor")
	}
}
