package main

import (
	"os"
	"runtime"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := Config{BaseURL: "https://x", Token: "tok", AgentID: "ag1", LoopbackToken: "lbtok", LoopbackPort: 8090}
	if err := saveConfig(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	p, _ := configPath()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Windows has no POSIX permission bits: WriteFile(0600) stats as 0666
	// there, so the owner-only check is only meaningful on POSIX hosts.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("got perms %v, want 0600", info.Mode().Perm())
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
