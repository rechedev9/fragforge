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
	if cfg.renderWorkerEnabled() {
		t.Fatal("render worker enabled, want disabled")
	}
	if cfg.agentWorkerEnabled() {
		t.Fatal("agent worker enabled, want disabled")
	}
	if cfg.MediaWorkDir != "" {
		t.Fatalf("MediaWorkDir = %q, want empty default for temp cleanup", cfg.MediaWorkDir)
	}
	if cfg.HTTPAddr != "127.0.0.1:8080" {
		t.Fatalf("HTTPAddr = %q, want loopback default", cfg.HTTPAddr)
	}
}

func TestLoadConfigRejectsLANBindWithoutMutationToken(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "postgres://example")
	t.Setenv("ZV_HTTP_ADDR", "0.0.0.0:8080")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig error = nil, want mutation token requirement")
	}
}

func TestLoadConfigAllowsLANBindWithMutationToken(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "postgres://example")
	t.Setenv("ZV_HTTP_ADDR", "0.0.0.0:8080")
	t.Setenv("ZV_MUTATION_TOKEN", "local-token")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if cfg.MutationToken != "local-token" {
		t.Fatalf("MutationToken = %q, want local-token", cfg.MutationToken)
	}
}

func TestLoadConfigAllowsPartialRecordWorkerConfig(t *testing.T) {
	// Regression: a partially-set record trio must not kill the boot. The
	// desktop app passes only ZV_HLAE_PATH (its provisioned HLAE) and relies on
	// auto-detection for the recorder and CS2; validation used to run before
	// detection and log.Fatal on the incomplete trio, so the whole app failed
	// to start. Incompleteness after detection just leaves the record worker
	// disabled, which capabilities and the startup log already explain.
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "postgres://example")
	t.Setenv("ZV_RECORDER_PATH", "zv-recorder.exe")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error = %v, want partial record config accepted", err)
	}
	if cfg.recordWorkerEnabled() {
		t.Fatal("record worker enabled with a partial trio, want disabled")
	}
}

func TestLoadConfigEnablesMediaWorkers(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "postgres://example")
	t.Setenv("ZV_RECORDER_PATH", "zv-recorder.exe")
	t.Setenv("ZV_HLAE_PATH", "HLAE.exe")
	t.Setenv("ZV_CS2_PATH", "cs2.exe")
	t.Setenv("ZV_COMPOSER_PATH", "zv-composer.exe")
	t.Setenv("ZV_EDITOR_PATH", "zv-editor.exe")
	t.Setenv("ZV_FFMPEG_PATH", "ffmpeg.exe")
	t.Setenv("ZV_FFPROBE_PATH", "ffprobe.exe")
	t.Setenv("ZV_RECORD_TIMEOUT", "30m")
	t.Setenv("ZV_COMPOSE_TIMEOUT", "10m")
	t.Setenv("ZV_RENDER_TIMEOUT", "12m")
	t.Setenv("ZV_CODEX_PATH", "codex.exe")
	t.Setenv("ZV_CODEX_MODEL", "gpt-5.4")
	t.Setenv("ZV_AGENT_TIMEOUT", "3m")
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
	if !cfg.renderWorkerEnabled() {
		t.Fatal("render worker disabled, want enabled")
	}
	if cfg.RecordTimeout != "30m0s" {
		t.Fatalf("RecordTimeout = %q, want 30m0s", cfg.RecordTimeout)
	}
	if cfg.ComposeTimeout != "10m0s" {
		t.Fatalf("ComposeTimeout = %q, want 10m0s", cfg.ComposeTimeout)
	}
	if cfg.RenderTimeout != "12m0s" {
		t.Fatalf("RenderTimeout = %q, want 12m0s", cfg.RenderTimeout)
	}
	if cfg.MediaWorkDir != "C:\\zv-work" {
		t.Fatalf("MediaWorkDir = %q, want C:\\zv-work", cfg.MediaWorkDir)
	}
	if !cfg.agentWorkerEnabled() {
		t.Fatal("agent worker disabled, want enabled")
	}
	if cfg.AgentTimeout != "3m0s" || cfg.CodexModel != "gpt-5.4" {
		t.Fatalf("agent config = timeout %q model %q", cfg.AgentTimeout, cfg.CodexModel)
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
		"ZV_DATA_DIR",
		"ZV_WORKER_CONCURRENCY",
		"ZV_MEDIA_WORK_DIR",
		"ZV_RECORDER_PATH",
		"ZV_COMPOSER_PATH",
		"ZV_EDITOR_PATH",
		"ZV_HLAE_PATH",
		"ZV_CS2_PATH",
		"ZV_FFMPEG_PATH",
		"ZV_FFPROBE_PATH",
		"ZV_RECORD_TIMEOUT",
		"ZV_COMPOSE_TIMEOUT",
		"ZV_RENDER_TIMEOUT",
		"ZV_MUTATION_TOKEN",
		"ZV_CODEX_PATH",
		"ZV_CODEX_MODEL",
		"ZV_AGENT_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}
