package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// TestResolvePrecedence walks the documented resolution order: --url beats
// $ORCHESTRATOR_URL beats ports.json beats the dev default. The userData
// directory is redirected with the test-only FRAGFORGE_USERDATA_DIR seam so the
// ports.json branch runs on any OS.
func TestResolvePrecedence(t *testing.T) {
	tests := []struct {
		name       string
		flagURL    string
		envURL     string
		portsJSON  string // "" means write no ports.json
		wantURL    string
		wantSource string
	}{
		{
			name:       "flag wins over everything",
			flagURL:    "http://127.0.0.1:9999/",
			envURL:     "http://127.0.0.1:8888",
			portsJSON:  `{"orchestrator":7000}`,
			wantURL:    "http://127.0.0.1:9999",
			wantSource: sourceFlag,
		},
		{
			name:       "env wins over ports.json",
			envURL:     "http://127.0.0.1:8888/",
			portsJSON:  `{"orchestrator":7000}`,
			wantURL:    "http://127.0.0.1:8888",
			wantSource: sourceEnv,
		},
		{
			name:       "ports.json wins over default",
			portsJSON:  `{"orchestrator":7123,"web":7124}`,
			wantURL:    "http://127.0.0.1:7123",
			wantSource: sourcePorts,
		},
		{
			name:       "malformed ports.json falls through to default",
			portsJSON:  `{ this is not json `,
			wantURL:    tuiclient.DefaultBaseURL,
			wantSource: sourceDefault,
		},
		{
			name:       "zero orchestrator port falls through to default",
			portsJSON:  `{"orchestrator":0}`,
			wantURL:    tuiclient.DefaultBaseURL,
			wantSource: sourceDefault,
		},
		{
			name:       "missing ports.json falls through to default",
			portsJSON:  "",
			wantURL:    tuiclient.DefaultBaseURL,
			wantSource: sourceDefault,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(userDataDirEnv, dir)
			t.Setenv("ORCHESTRATOR_URL", tt.envURL)
			if tt.portsJSON != "" {
				if err := os.WriteFile(filepath.Join(dir, "ports.json"), []byte(tt.portsJSON), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			got := Resolve(tt.flagURL)
			if got.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", got.URL, tt.wantURL)
			}
			if got.Source != tt.wantSource {
				t.Errorf("Source = %q, want %q", got.Source, tt.wantSource)
			}
		})
	}
}

// TestPortFromPortsFile isolates the ports.json parser: a valid file yields the
// orchestrator port, and every unusable file yields 0 so Resolve can fall back.
func TestPortFromPortsFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"valid", `{"orchestrator":7123,"web":7124}`, 7123},
		{"missing orchestrator key", `{"web":7124}`, 0},
		{"zero port", `{"orchestrator":0}`, 0},
		{"negative port", `{"orchestrator":-5}`, 0},
		{"malformed", `not json`, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(userDataDirEnv, dir)
			if err := os.WriteFile(filepath.Join(dir, "ports.json"), []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := portFromPortsFile(); got != tt.want {
				t.Errorf("portFromPortsFile() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestPortFromPortsFileMissingDir returns 0 (not a panic) when ports.json does
// not exist in the userData directory.
func TestPortFromPortsFileMissingDir(t *testing.T) {
	t.Setenv(userDataDirEnv, t.TempDir()) // empty dir, no ports.json
	if got := portFromPortsFile(); got != 0 {
		t.Errorf("portFromPortsFile() = %d, want 0", got)
	}
}

// TestUserDataDirOverride confirms the test seam wins over the per-OS path.
func TestUserDataDirOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "custom")
	t.Setenv(userDataDirEnv, want)
	got, err := userDataDir()
	if err != nil {
		t.Fatalf("userDataDir: %v", err)
	}
	if got != want {
		t.Errorf("userDataDir() = %q, want %q", got, want)
	}
}
