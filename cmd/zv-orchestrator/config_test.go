package main

import (
	"os"
	"path/filepath"
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

func TestLoadConfigRejectsLANBindEvenWithMutationToken(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "postgres://example")
	t.Setenv("ZV_HTTP_ADDR", "0.0.0.0:8080")
	t.Setenv("ZV_MUTATION_TOKEN", strings.Repeat("a", 64))

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig error = nil, want cleartext non-loopback bind rejected")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("loadConfig error = %q, want loopback requirement", err)
	}
}

func TestLoadConfigRequiresStrongSessionCapability(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{name: "missing"},
		{name: "short", token: "local-token"},
		{name: "uppercase hex", token: strings.Repeat("A", 64)},
		{name: "non hex", token: strings.Repeat("g", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ZV_DATABASE_URL", "memory")
			t.Setenv("ZV_MUTATION_TOKEN", tt.token)
			_, err := loadConfig()
			if err == nil {
				t.Fatal("loadConfig error = nil, want invalid session capability rejected")
			}
			if strings.Contains(err.Error(), tt.token) && tt.token != "" {
				t.Fatalf("loadConfig error reflected capability: %q", err)
			}
		})
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
	t.Setenv(mutationTokenEnvironmentVariable, strings.Repeat("b", 64))
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

func TestClearLegacyCaptionCredentialsEnvironment(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("ZV_DATABASE_URL", "memory")
	t.Setenv(legacyGroqAPIKeyVariable, "legacy-team-secret")
	t.Setenv(legacyGroqAPIKeyOverrideVariable, "legacy-override-secret")

	if _, err := loadConfig(); err != nil {
		t.Fatalf("loadConfig error = %v", err)
	}
	if err := clearLegacyCaptionCredentialsEnvironment(); err != nil {
		t.Fatalf("clearLegacyCaptionCredentialsEnvironment error = %v", err)
	}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, legacyGroqAPIKeyVariable) || strings.EqualFold(name, legacyGroqAPIKeyOverrideVariable) {
			t.Fatalf("environment still contains legacy caption credential %q", name)
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

func TestLoadConfigMusicDirDefaultsUnderDataDir(t *testing.T) {
	// Local Studio never sets ZV_MUSIC_DIR, so an empty value must resolve to the
	// on-disk library the repo ships at <DataDir>/music. Otherwise the songs API
	// returns an empty catalog and the web background-music picker stays blank.
	tests := []struct {
		name     string
		musicDir string
		dataDir  string
		want     string
	}{
		{
			name:     "explicit env wins verbatim",
			musicDir: "C:\\custom\\songs",
			dataDir:  "C:\\zv-data",
			want:     "C:\\custom\\songs",
		},
		{
			name:    "empty env defaults under configured data dir",
			dataDir: "C:\\zv-data",
			want:    filepath.Join("C:\\zv-data", "music"),
		},
		{
			name: "empty env defaults under default data dir",
			want: filepath.Join("./data", "music"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearConfigEnv(t)
			t.Setenv("ZV_DATABASE_URL", "memory")
			if tt.dataDir != "" {
				t.Setenv("ZV_DATA_DIR", tt.dataDir)
			}
			if tt.musicDir != "" {
				t.Setenv("ZV_MUSIC_DIR", tt.musicDir)
			}
			cfg, err := loadConfig()
			if err != nil {
				t.Fatalf("loadConfig error = %v", err)
			}
			if got, want := cfg.MusicDir, tt.want; got != want {
				t.Fatalf("MusicDir = %q, want %q", got, want)
			}
		})
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ZV_HTTP_ADDR",
		"ZV_DATABASE_URL",
		"ZV_DATA_DIR",
		"ZV_MUSIC_DIR",
		"ZV_RECORD_HUD",
		"ZV_YTDLP_PATH",
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
		"FIRECRAWL_API_KEY",
	} {
		t.Setenv(key, "")
	}
	t.Setenv(mutationTokenEnvironmentVariable, strings.Repeat("a", 64))
}
