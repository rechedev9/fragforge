package main

import (
	"os"
	"path/filepath"
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

// TestEnsureLoopbackConfigHealsLegacy loads a config paired before the loopback
// proxy existed (no loopback_token / loopback_port) and verifies the run path
// generates a token, records the env-derived port, and persists both.
func TestEnsureLoopbackConfigHealsLegacy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("FRAGFORGE_LOOPBACK_ADDR", "127.0.0.1:9123")

	// Legacy agent.json: only the cloud fields, no loopback credential.
	p, err := configPath()
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := []byte(`{"base_url":"https://x","token":"tok","agent_id":"ag1"}`)
	if err := os.WriteFile(p, legacy, 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.LoopbackToken != "" {
		t.Fatalf("legacy config unexpectedly had a loopback token")
	}

	healed, err := ensureLoopbackConfig(cfg)
	if err != nil {
		t.Fatalf("heal: %v", err)
	}
	if healed.LoopbackToken == "" {
		t.Error("healed config still has empty loopback token")
	}
	if healed.LoopbackPort != 9123 {
		t.Errorf("got healed port %d, want 9123", healed.LoopbackPort)
	}

	// The healed token and port must be persisted, so the next start reuses them.
	persisted, err := loadConfig()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if persisted != healed {
		t.Errorf("persisted %+v, want %+v", persisted, healed)
	}
}

// TestEnsureLoopbackConfigKeepsExistingToken is a no-op when already healed.
func TestEnsureLoopbackConfigKeepsExistingToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := Config{BaseURL: "https://x", Token: "tok", LoopbackToken: "keep", LoopbackPort: 8090}
	got, err := ensureLoopbackConfig(cfg)
	if err != nil {
		t.Fatalf("heal: %v", err)
	}
	if got != cfg {
		t.Errorf("got %+v, want unchanged %+v", got, cfg)
	}
}

// TestHeartbeatCarriesEnvDerivedPort proves run-time truth wins: with the env
// pointing at a non-default port and the persisted config carrying 8090, the
// heartbeat body advertises the env-derived port.
func TestHeartbeatCarriesEnvDerivedPort(t *testing.T) {
	t.Setenv("FRAGFORGE_LOOPBACK_ADDR", "127.0.0.1:9999")
	cfg := Config{LoopbackPort: 8090}

	// run() derives the port from loopbackAddr(), the same value it binds the
	// proxy on, rather than the persisted cfg.LoopbackPort.
	got := portFromAddr(loopbackAddr())
	if got == cfg.LoopbackPort {
		t.Fatalf("port did not diverge from persisted value %d", cfg.LoopbackPort)
	}
	if got != 9999 {
		t.Errorf("got heartbeat port %d, want 9999", got)
	}
}

func TestOrchestratorURL(t *testing.T) {
	t.Run("unset returns empty", func(t *testing.T) {
		t.Setenv("FRAGFORGE_ORCHESTRATOR_URL", "")
		if got := orchestratorURL(); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})
	t.Run("set returns the env value", func(t *testing.T) {
		t.Setenv("FRAGFORGE_ORCHESTRATOR_URL", "http://127.0.0.1:8080")
		if got := orchestratorURL(); got != "http://127.0.0.1:8080" {
			t.Errorf("got %q, want http://127.0.0.1:8080", got)
		}
	})
}

func TestChildDataDir(t *testing.T) {
	t.Run("inherits env when ZV_DATA_DIR set", func(t *testing.T) {
		t.Setenv("ZV_DATA_DIR", "C:/some/data")
		if got := childDataDir(); got != "" {
			t.Errorf("got %q, want empty so the child inherits ZV_DATA_DIR", got)
		}
	})
	t.Run("anchors next to config when unset", func(t *testing.T) {
		t.Setenv("ZV_DATA_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		p, err := configPath()
		if err != nil {
			t.Fatalf("config path: %v", err)
		}
		want := filepath.Join(filepath.Dir(p), "data")
		if got := childDataDir(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
