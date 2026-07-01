package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
	AgentID string `json:"agent_id"`
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
