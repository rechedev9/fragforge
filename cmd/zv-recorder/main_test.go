package main

import (
	"os"
	"path/filepath"
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

func TestPatchWindowedVideoSettings(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		want        string
		wantChanged bool
	}{
		{
			name: "forces fullscreen and borderless off",
			content: "\t\"setting.fullscreen\"\t\t\"1\"\n" +
				"\t\"setting.nowindowborder\"\t\t\"1\"\n" +
				"\t\"setting.defaultres\"\t\t\"1920\"\n",
			want: "\t\"setting.fullscreen\"\t\t\"0\"\n" +
				"\t\"setting.nowindowborder\"\t\t\"0\"\n" +
				"\t\"setting.defaultres\"\t\t\"1920\"\n",
			wantChanged: true,
		},
		{
			name: "already windowed is untouched",
			content: "\t\"setting.fullscreen\"\t\t\"0\"\n" +
				"\t\"setting.nowindowborder\"\t\t\"0\"\n",
			want: "\t\"setting.fullscreen\"\t\t\"0\"\n" +
				"\t\"setting.nowindowborder\"\t\t\"0\"\n",
			wantChanged: false,
		},
		{
			name:        "absent settings stay absent",
			content:     "\t\"setting.defaultres\"\t\t\"1920\"\n",
			want:        "\t\"setting.defaultres\"\t\t\"1920\"\n",
			wantChanged: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := patchWindowedVideoSettings(tt.content)
			if got != tt.want {
				t.Errorf("content = %q, want %q", got, tt.want)
			}
			if changed != tt.wantChanged {
				t.Errorf("changed = %v, want %v", changed, tt.wantChanged)
			}
		})
	}
}

func TestForceWindowedVideoConfigPatchesAndRestores(t *testing.T) {
	steam := t.TempDir()
	cfgDir := filepath.Join(steam, "userdata", "50084006", "730", "local", "cfg")
	if err := os.MkdirAll(cfgDir, 0o750); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(cfgDir, "cs2_video.txt")
	original := "\t\"setting.fullscreen\"\t\t\"1\"\n\t\"setting.nowindowborder\"\t\t\"1\"\n"
	if err := os.WriteFile(cfgPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cs2 := filepath.Join(steam, "steamapps", "common", "Counter-Strike Global Offensive", "game", "bin", "win64", "cs2.exe")

	restore := forceWindowedVideoConfig(cs2)
	patched, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(patched), `"setting.fullscreen"		"0"`) || !strings.Contains(string(patched), `"setting.nowindowborder"		"0"`) {
		t.Fatalf("patched config = %q, want fullscreen and borderless forced off", patched)
	}

	restore()
	restored, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != original {
		t.Fatalf("restored config = %q, want original %q", restored, original)
	}
}

func TestIsHookErrorWindowTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  bool
	}{
		{"afxhooksource2 dialog", "Error - AfxHookSource2", true},
		{"afxhooksource dialog", "Error - AfxHookSource", true},
		{"afxhookgold dialog", "Error - AfxHookGold", true},
		{"game window", "Counter-Strike 2", false},
		{"empty", "", false},
		{"na placeholder", "N/A", false},
		{"errors plural prefix", "Errors - Afx", false},
		{"lowercase is case sensitive", "error - afxhooksource2", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHookErrorWindowTitle(tt.title); got != tt.want {
				t.Errorf("isHookErrorWindowTitle(%q) = %v, want %v", tt.title, got, tt.want)
			}
		})
	}
}

func TestParseTasklistVerboseCSV(t *testing.T) {
	tests := []struct {
		name        string
		out         string
		image       string
		wantRunning bool
		wantTitle   string
	}{
		{
			name:        "running with normal title",
			out:         `"cs2.exe","12345","Console","1","2,345,678 K","Running","DESKTOP\user","0:01:23","Counter-Strike 2"` + "\n",
			image:       "cs2.exe",
			wantRunning: true,
			wantTitle:   "Counter-Strike 2",
		},
		{
			name:        "running with hook-crash dialog title",
			out:         `"cs2.exe","12345","Console","1","2,345,678 K","Running","DESKTOP\user","0:01:23","Error - AfxHookSource2"` + "\n",
			image:       "cs2.exe",
			wantRunning: true,
			wantTitle:   "Error - AfxHookSource2",
		},
		{
			name:        "no matching tasks line",
			out:         "INFO: No tasks are running which match the specified criteria.\n",
			image:       "cs2.exe",
			wantRunning: false,
			wantTitle:   "",
		},
		{
			name:        "empty output",
			out:         "",
			image:       "cs2.exe",
			wantRunning: false,
			wantTitle:   "",
		},
		{
			name:        "case-insensitive image match",
			out:         `"CS2.EXE","12345","Console","1","2,345,678 K","Running","DESKTOP\user","0:01:23","Counter-Strike 2"` + "\n",
			image:       "cs2.exe",
			wantRunning: true,
			wantTitle:   "Counter-Strike 2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRunning, gotTitle := parseTasklistVerboseCSV(tt.out, tt.image)
			if gotRunning != tt.wantRunning {
				t.Errorf("running = %v, want %v", gotRunning, tt.wantRunning)
			}
			if gotTitle != tt.wantTitle {
				t.Errorf("title = %q, want %q", gotTitle, tt.wantTitle)
			}
		})
	}
}

func TestSteamRootFromCS2Path(t *testing.T) {
	cs2 := filepath.FromSlash("D:/SteamLibrary/steamapps/common/Counter-Strike Global Offensive/game/bin/win64/cs2.exe")
	if got, want := steamRootFromCS2Path(cs2), filepath.FromSlash("D:/SteamLibrary"); got != want {
		t.Errorf("steamRootFromCS2Path = %q, want %q", got, want)
	}
	if got := steamRootFromCS2Path(filepath.FromSlash("C:/tools/cs2.exe")); got != "" {
		t.Errorf("steamRootFromCS2Path outside steamapps = %q, want empty", got)
	}
}
