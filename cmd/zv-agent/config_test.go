package main

import (
	"os"
	"testing"
)

func TestConfigRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	want := Config{BaseURL: "https://x", Token: "tok", AgentID: "ag1"}
	if err := saveConfig(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	p, _ := configPath()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
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
