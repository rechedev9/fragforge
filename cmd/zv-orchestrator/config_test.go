package main

import "testing"

func TestLoadConfigAllowsParserOnlyMode(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "postgres://example")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if cfg.recordWorkerEnabled() {
		t.Fatal("record worker enabled, want disabled")
	}
	if cfg.composeWorkerEnabled() {
		t.Fatal("compose worker enabled, want disabled")
	}
	if cfg.MediaWorkDir != "data\\work" && cfg.MediaWorkDir != "data/work" {
		t.Fatalf("MediaWorkDir = %q, want data/work", cfg.MediaWorkDir)
	}
}

func TestLoadConfigRejectsPartialRecordWorkerConfig(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "postgres://example")
	t.Setenv("ZV_RECORDER_PATH", "zv-recorder.exe")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig error = nil, want missing HLAE/CS2 error")
	}
}

func TestLoadConfigEnablesMediaWorkers(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "postgres://example")
	t.Setenv("ZV_RECORDER_PATH", "zv-recorder.exe")
	t.Setenv("ZV_HLAE_PATH", "HLAE.exe")
	t.Setenv("ZV_CS2_PATH", "cs2.exe")
	t.Setenv("ZV_COMPOSER_PATH", "zv-composer.exe")
	t.Setenv("ZV_FFMPEG_PATH", "ffmpeg.exe")
	t.Setenv("ZV_RECORD_TIMEOUT", "30m")
	t.Setenv("ZV_COMPOSE_TIMEOUT", "10m")
	t.Setenv("ZV_MEDIA_WORK_DIR", "C:\\zv-work")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if !cfg.recordWorkerEnabled() {
		t.Fatal("record worker disabled, want enabled")
	}
	if !cfg.composeWorkerEnabled() {
		t.Fatal("compose worker disabled, want enabled")
	}
	if cfg.RecordTimeout != "30m0s" {
		t.Fatalf("RecordTimeout = %q, want 30m0s", cfg.RecordTimeout)
	}
	if cfg.ComposeTimeout != "10m0s" {
		t.Fatalf("ComposeTimeout = %q, want 10m0s", cfg.ComposeTimeout)
	}
	if cfg.MediaWorkDir != "C:\\zv-work" {
		t.Fatalf("MediaWorkDir = %q, want C:\\zv-work", cfg.MediaWorkDir)
	}
}

func TestLoadConfigRejectsInvalidDuration(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "postgres://example")
	t.Setenv("ZV_RECORD_TIMEOUT", "soon")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig error = nil, want invalid duration")
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ZV_HTTP_ADDR",
		"ZV_DATABASE_URL",
		"ZV_REDIS_ADDR",
		"ZV_DATA_DIR",
		"ZV_WORKER_CONCURRENCY",
		"ZV_MEDIA_WORK_DIR",
		"ZV_RECORDER_PATH",
		"ZV_COMPOSER_PATH",
		"ZV_HLAE_PATH",
		"ZV_CS2_PATH",
		"ZV_FFMPEG_PATH",
		"ZV_RECORD_TIMEOUT",
		"ZV_COMPOSE_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}
