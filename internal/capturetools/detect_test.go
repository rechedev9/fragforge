package capturetools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDetectExplicitPathsWin(t *testing.T) {
	paths := Paths{
		Recorder: filepath.FromSlash("/explicit/zv-recorder"),
		Editor:   filepath.FromSlash("/explicit/zv-editor"),
		FFmpeg:   filepath.FromSlash("/explicit/ffmpeg"),
	}
	got, sources := Detect(paths)

	if got.Recorder != paths.Recorder || got.Editor != paths.Editor || got.FFmpeg != paths.FFmpeg {
		t.Fatalf("Detect() paths = %#v, want explicit paths preserved", got)
	}
	for _, name := range []string{"ZV_RECORDER_PATH", "ZV_EDITOR_PATH", "ZV_FFMPEG_PATH"} {
		if sources[name] != SourceEnvironment {
			t.Errorf("%s source = %q, want %q", name, sources[name], SourceEnvironment)
		}
	}
	for _, name := range []string{"ZV_HLAE_PATH", "ZV_CS2_PATH", "ZV_FFPROBE_PATH"} {
		if source := sources[name]; source != SourceDetected && source != SourceNone {
			t.Errorf("%s source = %q, want detected or none", name, source)
		}
	}
}

func TestDetectFindsSibling(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	siblings := map[string]string{}
	for _, name := range []string{"zv-editor", "zv-composer"} {
		filename := name
		if runtime.GOOS == "windows" {
			filename += ".exe"
		}
		path := filepath.Join(filepath.Dir(exe), filename)
		if err := os.WriteFile(path, []byte("stub"), 0o700); err != nil {
			t.Skipf("cannot write next to test binary: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(path) })
		siblings[name] = path
	}

	paths, sources := Detect(Paths{})
	if paths.Editor != siblings["zv-editor"] {
		t.Errorf("Editor = %q, want %q", paths.Editor, siblings["zv-editor"])
	}
	if sources["ZV_EDITOR_PATH"] != SourceDetected {
		t.Errorf("editor source = %q, want %q", sources["ZV_EDITOR_PATH"], SourceDetected)
	}
	if paths.Composer != siblings["zv-composer"] {
		t.Errorf("Composer = %q, want %q", paths.Composer, siblings["zv-composer"])
	}
	if sources["ZV_COMPOSER_PATH"] != SourceDetected {
		t.Errorf("composer source = %q, want %q", sources["ZV_COMPOSER_PATH"], SourceDetected)
	}
}

func TestResolveToolReportsInaccessibleExplicitPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.exe")
	tool := ResolveTool("ZV_HLAE_PATH", missing, Sources{"ZV_HLAE_PATH": SourceEnvironment})
	if !tool.Configured || tool.Accessible || tool.Path != missing || tool.Source != SourceEnvironment {
		t.Fatalf("ResolveTool() = %#v, want configured inaccessible env path", tool)
	}
}

func TestResolveToolRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	tool := ResolveTool("ZV_EDITOR_PATH", dir, Sources{"ZV_EDITOR_PATH": SourceEnvironment})
	if !tool.Configured || tool.Accessible {
		t.Fatalf("ResolveTool() = %#v, want configured inaccessible directory", tool)
	}
}

func TestResolveToolRequiresExecutePermissionOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not use Unix execute permission bits")
	}
	path := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(path, []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := ResolveTool("ZV_EDITOR_PATH", path, Sources{"ZV_EDITOR_PATH": SourceEnvironment})
	if tool.Accessible {
		t.Fatalf("ResolveTool() = %#v, want non-executable file rejected", tool)
	}
}

func TestResolveToolRequiresExecutableExtensionOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows uses PATHEXT to identify executable files")
	}
	path := filepath.Join(t.TempDir(), "not-a-tool.txt")
	if err := os.WriteFile(path, []byte("stub"), 0o700); err != nil {
		t.Fatal(err)
	}
	tool := ResolveTool("ZV_EDITOR_PATH", path, Sources{"ZV_EDITOR_PATH": SourceEnvironment})
	if tool.Accessible {
		t.Fatalf("ResolveTool() = %#v, want non-executable extension rejected", tool)
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
	if err := os.WriteFile(real, []byte("x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := firstExisting(filepath.Join(dir, "missing"), real); got != real {
		t.Errorf("firstExisting = %q, want %q", got, real)
	}
	if got := firstExisting("", filepath.Join(dir, "nope")); got != "" {
		t.Errorf("firstExisting = %q, want empty", got)
	}
}

func TestSteamPathFromRegOutput(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{name: "standard", out: "\r\nHKEY_CURRENT_USER\\Software\\Valve\\Steam\r\n    SteamPath    REG_SZ    d:/steam\r\n", want: `d:/steam`},
		{name: "spaces", out: "    SteamPath    REG_SZ    D:\\My Games\\Steam\r\n", want: `D:\My Games\Steam`},
		{name: "missing", out: "HKEY_CURRENT_USER\\Software\\Valve\\Steam\r\n"},
		{name: "empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := steamPathFromRegOutput(tt.out); got != tt.want {
				t.Errorf("steamPathFromRegOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}
