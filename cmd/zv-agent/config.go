package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultLoopbackAddr = "127.0.0.1:8090"
	defaultWebOrigin    = "https://app.fragforge.gg"
)

// loopbackAddr is the proxy bind address (FRAGFORGE_LOOPBACK_ADDR, default
// 127.0.0.1:8090).
func loopbackAddr() string {
	if v := os.Getenv("FRAGFORGE_LOOPBACK_ADDR"); v != "" {
		return v
	}
	return defaultLoopbackAddr
}

// loopbackPort extracts the port from loopbackAddr; it is the port the browser
// connects to and the value registered with the control plane. Falls back to
// 8090 if the address has no parseable port.
func loopbackPort() int {
	_, portStr, err := net.SplitHostPort(loopbackAddr())
	if err != nil {
		return 8090
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return 8090
	}
	return p
}

// webOrigins is the CORS allowlist (FRAGFORGE_WEB_ORIGIN, comma-separated,
// default https://app.fragforge.gg).
func webOrigins() []string {
	raw := os.Getenv("FRAGFORGE_WEB_ORIGIN")
	if raw == "" {
		raw = defaultWebOrigin
	}
	var out []string
	for o := range strings.SplitSeq(raw, ",") {
		if o = strings.TrimSpace(o); o != "" {
			out = append(out, o)
		}
	}
	return out
}

type Config struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
	AgentID string `json:"agent_id"`
	// LoopbackToken is the Bearer credential the browser must present to the
	// local data-plane proxy. Generated at pair time, never sent to the cloud
	// except as registration metadata in pair/heartbeat.
	LoopbackToken string `json:"loopback_token"`
	// LoopbackPort is the proxy port the browser connects to (the port of
	// FRAGFORGE_LOOPBACK_ADDR), registered with the control plane so
	// GET /api/pc/status can hand it to the browser.
	LoopbackPort int `json:"loopback_port"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fragforge", "agent.json"), nil
}

func loadConfig() (Config, error) {
	p, err := configPath()
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return Config{}, err
	}
	var c Config
	return c, json.Unmarshal(b, &c)
}

func saveConfig(c Config) error {
	p, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o600)
}
