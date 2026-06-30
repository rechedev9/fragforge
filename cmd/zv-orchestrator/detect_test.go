package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectCaptureToolsEnvWins(t *testing.T) {
	cfg := config{RecorderPath: filepath.FromSlash("/explicit/zv-recorder")}
	got, src := detectCaptureTools(cfg)

	if got.RecorderPath != filepath.FromSlash("/explicit/zv-recorder") {
		t.Errorf("RecorderPath = %q, want the explicit value kept", got.RecorderPath)
	}
	if src["ZV_RECORDER_PATH"] != "env" {
		t.Errorf("recorder source = %q, want env", src["ZV_RECORDER_PATH"])
	}
	// Unset tools resolve to detected (on a host with the tool) or none, never "env".
	for _, name := range []string{"ZV_HLAE_PATH", "ZV_CS2_PATH"} {
		if s := src[name]; s != "detected" && s != "none" {
			t.Errorf("%s source = %q, want detected or none", name, s)
		}
	}
}

func TestSteamLibraryPaths(t *testing.T) {
	dir := t.TempDir()
	vdf := filepath.Join(dir, "libraryfolders.vdf")
	content := "\"libraryfolders\"\n{\n\t\"0\"\n\t{\n\t\t\"path\"\t\t\"C:\\\\Program Files (x86)\\\\Steam\"\n\t}\n\t\"1\"\n\t{\n\t\t\"path\"\t\t\"D:\\\\SteamLibrary\"\n\t}\n}\n"
	if err := os.WriteFile(vdf, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got := steamLibraryPaths(vdf)
	want := []string{`C:\Program Files (x86)\Steam`, `D:\SteamLibrary`}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if steamLibraryPaths(filepath.Join(dir, "missing.vdf")) != nil {
		t.Error("missing vdf should yield nil")
	}
}

func TestFirstExisting(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.exe")
	if err := os.WriteFile(real, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := firstExisting(filepath.Join(dir, "missing"), real); got != real {
		t.Errorf("firstExisting = %q, want %q", got, real)
	}
	if got := firstExisting("", filepath.Join(dir, "nope")); got != "" {
		t.Errorf("firstExisting = %q, want empty", got)
	}
}
