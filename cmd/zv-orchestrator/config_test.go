package main

import (
	"os"
	"strings"
	"testing"
)

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

func TestLoadConfigReadsDiscoverySecret(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "postgres://example")
	secret := strings.Repeat("a", 64)
	t.Setenv("ZV_DISCOVERY_SECRET", secret)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if got, want := cfg.DiscoverySecret, secret; got != want {
		t.Fatalf("DiscoverySecret = %q, want %q", got, want)
	}
}

func TestLoadConfigRejectsMalformedDiscoverySecretWithoutReflectingIt(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "memory")
	secret := "do-not-reflect-this-value"
	t.Setenv("ZV_DISCOVERY_SECRET", secret)

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig error = nil, want invalid discovery secret rejected")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("loadConfig error reflected discovery secret: %q", err)
	}
	if !strings.Contains(err.Error(), "32 random bytes encoded as lowercase hex") {
		t.Fatalf("loadConfig error = %q, want format guidance", err)
	}
}

func TestClearDiscoverySecretEnvironmentKeepsLoadedConfigOnlyInMemory(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "memory")
	t.Setenv("ZV_DISCOVERY_SECRET", strings.Repeat("a", 64))
	t.Setenv("zv_discovery_secret", strings.Repeat("b", 64))

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if err := clearDiscoverySecretEnvironment(); err != nil {
		t.Fatalf("clearDiscoverySecretEnvironment error = %v", err)
	}
	if cfg.DiscoverySecret == "" {
		t.Fatal("DiscoverySecret is empty after load, want credential retained in config memory")
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, discoverySecretEnvironmentVariable) {
			t.Fatalf("environment still contains %q after credential cleanup", name)
		}
	}
}

func TestClearSubprocessCredentialEnvironmentKeepsLoadedConfigOnlyInMemory(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "memory")
	t.Setenv(mutationTokenEnvironmentVariable, "mutation-secret")
	t.Setenv(firecrawlAPIKeyEnvironmentVariable, "firecrawl-secret")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if err := clearSubprocessCredentialEnvironment(); err != nil {
		t.Fatalf("clearSubprocessCredentialEnvironment error = %v", err)
	}
	if cfg.MutationToken == "" || cfg.FirecrawlAPIKey == "" {
		t.Fatal("loaded config lost a credential after environment cleanup")
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, mutationTokenEnvironmentVariable) || strings.EqualFold(name, firecrawlAPIKeyEnvironmentVariable) {
			t.Fatalf("environment still contains %q after credential cleanup", name)
		}
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

func TestLoadConfigXAIAPIKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantKey string
	}{
		{name: "configured", key: "xai-abc", wantKey: "xai-abc"},
		{name: "unset leaves xai disabled", wantKey: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ZV_DATABASE_URL", "memory")
			if tt.key != "" {
				t.Setenv("XAI_API_KEY", tt.key)
			}
			cfg, err := loadConfig()
			if err != nil {
				t.Fatalf("loadConfig error = %v", err)
			}
			if cfg.XAIAPIKey != tt.wantKey {
				t.Fatalf("XAIAPIKey = %q, want %q", cfg.XAIAPIKey, tt.wantKey)
			}
			if got, want := cfg.xaiEnabled(), tt.wantKey != ""; got != want {
				t.Fatalf("xaiEnabled() = %v, want %v", got, want)
			}
		})
	}
}

func TestLoadConfigGroqAPIKey(t *testing.T) {
	tests := []struct {
		name        string
		env         map[string]string
		wantKey     string
		wantModel   string
		wantCorrect string
	}{
		{
			name:        "unset leaves groq disabled",
			wantCorrect: defaultGroqCorrectionModel,
		},
		{
			name:        "conventional GROQ_API_KEY",
			env:         map[string]string{"GROQ_API_KEY": "groq-abc"},
			wantKey:     "groq-abc",
			wantCorrect: defaultGroqCorrectionModel,
		},
		{
			name: "ZV_GROQ_API_KEY override wins",
			env: map[string]string{
				"GROQ_API_KEY":    "user-level-key",
				"ZV_GROQ_API_KEY": "fragforge-key",
			},
			wantKey:     "fragforge-key",
			wantCorrect: defaultGroqCorrectionModel,
		},
		{
			name: "model and correction model overrides",
			env: map[string]string{
				"GROQ_API_KEY":             "groq-abc",
				"ZV_GROQ_MODEL":            "whisper-large-v3",
				"ZV_GROQ_CORRECTION_MODEL": "some-other-model",
			},
			wantKey:     "groq-abc",
			wantModel:   "whisper-large-v3",
			wantCorrect: "some-other-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ZV_DATABASE_URL", "memory")
			for name, value := range tt.env {
				t.Setenv(name, value)
			}
			cfg, err := loadConfig()
			if err != nil {
				t.Fatalf("loadConfig error = %v", err)
			}
			if cfg.GroqAPIKey != tt.wantKey {
				t.Fatalf("GroqAPIKey = %q, want %q", cfg.GroqAPIKey, tt.wantKey)
			}
			if cfg.GroqModel != tt.wantModel {
				t.Fatalf("GroqModel = %q, want %q", cfg.GroqModel, tt.wantModel)
			}
			if cfg.GroqCorrectionModel != tt.wantCorrect {
				t.Fatalf("GroqCorrectionModel = %q, want %q", cfg.GroqCorrectionModel, tt.wantCorrect)
			}
			if got, want := cfg.groqEnabled(), tt.wantKey != ""; got != want {
				t.Fatalf("groqEnabled() = %v, want %v", got, want)
			}
		})
	}
}

func TestClearGroqAPIKeyEnvironmentKeepsLoadedConfigOnlyInMemory(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "memory")
	t.Setenv("GROQ_API_KEY", "groq-team-secret")
	t.Setenv("ZV_GROQ_API_KEY", "groq-override-secret")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if err := clearGroqAPIKeyEnvironment(); err != nil {
		t.Fatalf("clearGroqAPIKeyEnvironment error = %v", err)
	}
	if cfg.GroqAPIKey == "" {
		t.Fatal("GroqAPIKey is empty after load, want the credential retained in config memory")
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, groqAPIKeyEnvironmentVariable) || strings.EqualFold(name, groqAPIKeyOverrideVariable) {
			t.Fatalf("environment still contains %q after credential cleanup", name)
		}
	}
}

func TestClearXAIAPIKeyEnvironmentKeepsLoadedConfigOnlyInMemory(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "memory")
	t.Setenv("XAI_API_KEY", "xai-team-secret")
	// On Unix this is a separate variable; on Windows it exercises the native
	// case-insensitive environment. Either way, no casing variant may survive.
	t.Setenv("xai_api_key", "lowercase-team-secret")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if err := clearXAIAPIKeyEnvironment(); err != nil {
		t.Fatalf("clearXAIAPIKeyEnvironment error = %v", err)
	}
	if cfg.XAIAPIKey == "" {
		t.Fatal("XAIAPIKey is empty after load, want the credential retained in config memory")
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, xaiAPIKeyEnvironmentVariable) {
			t.Fatalf("environment still contains %q after credential cleanup", name)
		}
	}
}

func TestLoadConfigFirecrawlAPIKey(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "memory")
	t.Setenv("FIRECRAWL_API_KEY", "fc-test-secret")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if cfg.FirecrawlAPIKey != "fc-test-secret" || !cfg.firecrawlEnabled() {
		t.Fatalf("firecrawl config = %q enabled=%v", cfg.FirecrawlAPIKey, cfg.firecrawlEnabled())
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
		"ZV_DISCOVERY_SECRET",
		"ZV_CODEX_PATH",
		"ZV_CODEX_MODEL",
		"ZV_AGENT_TIMEOUT",
		"XAI_API_KEY",
		"GROQ_API_KEY",
		"ZV_GROQ_API_KEY",
		"ZV_GROQ_MODEL",
		"ZV_GROQ_CORRECTION_MODEL",
		"FIRECRAWL_API_KEY",
	} {
		t.Setenv(key, "")
	}
}
