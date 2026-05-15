package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type config struct {
	HTTPAddr          string
	DatabaseURL       string
	RedisAddr         string
	DataDir           string
	WorkerConcurrency int
	MediaWorkDir      string
	RecorderPath      string
	ComposerPath      string
	HLAEPath          string
	CS2Path           string
	FFmpegPath        string
	RecordTimeout     string
	ComposeTimeout    string
}

func loadConfig() (config, error) {
	c := config{
		HTTPAddr:     envOr("ZV_HTTP_ADDR", ":8080"),
		DatabaseURL:  os.Getenv("ZV_DATABASE_URL"),
		RedisAddr:    envOr("ZV_REDIS_ADDR", "localhost:6379"),
		DataDir:      envOr("ZV_DATA_DIR", "./data"),
		RecorderPath: os.Getenv("ZV_RECORDER_PATH"),
		ComposerPath: os.Getenv("ZV_COMPOSER_PATH"),
		HLAEPath:     os.Getenv("ZV_HLAE_PATH"),
		CS2Path:      os.Getenv("ZV_CS2_PATH"),
		FFmpegPath:   os.Getenv("ZV_FFMPEG_PATH"),
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("ZV_DATABASE_URL is required")
	}
	c.MediaWorkDir = envOr("ZV_MEDIA_WORK_DIR", filepath.Join(c.DataDir, "work"))

	concRaw := envOr("ZV_WORKER_CONCURRENCY", "2")
	conc, err := strconv.Atoi(concRaw)
	if err != nil || conc < 1 {
		return c, fmt.Errorf("ZV_WORKER_CONCURRENCY must be a positive integer, got %q", concRaw)
	}
	c.WorkerConcurrency = conc

	c.RecordTimeout, err = durationEnv("ZV_RECORD_TIMEOUT", "20m")
	if err != nil {
		return c, err
	}
	c.ComposeTimeout, err = durationEnv("ZV_COMPOSE_TIMEOUT", "20m")
	if err != nil {
		return c, err
	}
	if err := c.validateMediaConfig(); err != nil {
		return c, err
	}
	return c, nil
}

func (c config) recordWorkerEnabled() bool {
	return c.RecorderPath != ""
}

func (c config) composeWorkerEnabled() bool {
	return c.ComposerPath != ""
}

func (c config) validateMediaConfig() error {
	recordValues := map[string]string{
		"ZV_RECORDER_PATH": c.RecorderPath,
		"ZV_HLAE_PATH":     c.HLAEPath,
		"ZV_CS2_PATH":      c.CS2Path,
	}
	anySet := false
	for _, value := range recordValues {
		anySet = anySet || value != ""
	}
	if !anySet {
		return nil
	}
	for key, value := range recordValues {
		if value == "" {
			return fmt.Errorf("%s is required when record worker config is set", key)
		}
	}
	return nil
}

func durationEnv(key, def string) (string, error) {
	raw := envOr(key, def)
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return "", fmt.Errorf("%s must be a positive duration, got %q", key, raw)
	}
	return d.String(), nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
