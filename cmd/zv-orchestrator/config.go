package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rechedev9/fragforge/internal/httpapi"
)

type config struct {
	HTTPAddr            string
	DatabaseURL         string
	DataDir             string
	WorkerConcurrency   int
	MediaWorkDir        string
	RecorderPath        string
	ComposerPath        string
	EditorPath          string
	HLAEPath            string
	CS2Path             string
	RecordHUD           string
	FFmpegPath          string
	FFprobePath         string
	MusicDir            string
	RecordTimeout       string
	ComposeTimeout      string
	RenderTimeout       string
	MutationToken       string
	DiscoverySecret     string
	CodexPath           string
	CodexModel          string
	AgentTimeout        string
	YtdlpPath           string
	WhisperPath         string
	WhisperModelPath    string
	XAIAPIKey           string
	GroqAPIKey          string
	GroqModel           string
	GroqCorrectionModel string
	FirecrawlAPIKey     string
}

const (
	databaseURLMemory                  = "memory"
	xaiAPIKeyEnvironmentVariable       = "XAI_API_KEY"
	groqAPIKeyEnvironmentVariable      = "GROQ_API_KEY"
	groqAPIKeyOverrideVariable         = "ZV_GROQ_API_KEY"
	discoverySecretEnvironmentVariable = "ZV_DISCOVERY_SECRET"
	// defaultGroqCorrectionModel post-corrects Groq transcripts contextually;
	// see internal/captions.GroqTranscriber.CorrectionModel.
	defaultGroqCorrectionModel = "llama-3.3-70b-versatile"
	// databaseURLSQLite selects the on-disk SQLite job repository. Accepts the
	// bare value "sqlite" (stores <DataDir>/jobs.db) or "sqlite:<path>".
	databaseURLSQLite = "sqlite"
)

// sqlitePath resolves the SQLite file path from the database URL, defaulting to
// <dataDir>/jobs.db when only "sqlite" (no explicit path) is given.
func sqlitePath(url, dataDir string) string {
	path := strings.TrimPrefix(url, databaseURLSQLite+":")
	if path == "" || url == databaseURLSQLite {
		return filepath.Join(dataDir, "jobs.db")
	}
	return path
}

func loadConfig() (config, error) {
	c := config{
		HTTPAddr:         envOr("ZV_HTTP_ADDR", "127.0.0.1:8080"),
		DatabaseURL:      os.Getenv("ZV_DATABASE_URL"),
		DataDir:          envOr("ZV_DATA_DIR", "./data"),
		MediaWorkDir:     os.Getenv("ZV_MEDIA_WORK_DIR"),
		RecorderPath:     os.Getenv("ZV_RECORDER_PATH"),
		ComposerPath:     os.Getenv("ZV_COMPOSER_PATH"),
		EditorPath:       os.Getenv("ZV_EDITOR_PATH"),
		HLAEPath:         os.Getenv("ZV_HLAE_PATH"),
		CS2Path:          os.Getenv("ZV_CS2_PATH"),
		RecordHUD:        os.Getenv("ZV_RECORD_HUD"),
		FFmpegPath:       os.Getenv("ZV_FFMPEG_PATH"),
		FFprobePath:      os.Getenv("ZV_FFPROBE_PATH"),
		MusicDir:         os.Getenv("ZV_MUSIC_DIR"),
		MutationToken:    os.Getenv("ZV_MUTATION_TOKEN"),
		DiscoverySecret:  os.Getenv(discoverySecretEnvironmentVariable),
		CodexPath:        os.Getenv("ZV_CODEX_PATH"),
		CodexModel:       os.Getenv("ZV_CODEX_MODEL"),
		YtdlpPath:        os.Getenv("ZV_YTDLP_PATH"),
		WhisperPath:      os.Getenv("ZV_WHISPER_PATH"),
		WhisperModelPath: os.Getenv("ZV_WHISPER_MODEL"),
		// Cloud transcription credentials are not auto-detected because an API
		// key cannot be probed on PATH or disk. xAI is the preferred captions
		// backend; Groq is the fallback when xAI fails or finds no words.
		XAIAPIKey: os.Getenv(xaiAPIKeyEnvironmentVariable),
		// GROQ_API_KEY is the key's conventional name across Groq's own tooling;
		// ZV_GROQ_API_KEY is an explicit override for when a user-level
		// GROQ_API_KEY is set for something unrelated.
		GroqAPIKey:          firstNonEmpty(os.Getenv(groqAPIKeyOverrideVariable), os.Getenv(groqAPIKeyEnvironmentVariable)),
		GroqModel:           os.Getenv("ZV_GROQ_MODEL"),
		GroqCorrectionModel: envOr("ZV_GROQ_CORRECTION_MODEL", defaultGroqCorrectionModel),
		// Firecrawl enriches strategy suggestions with public CS2 trend
		// references. It is optional and never sent to the web renderer.
		FirecrawlAPIKey: os.Getenv("FIRECRAWL_API_KEY"),
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("ZV_DATABASE_URL is required")
	}
	if !httpapi.IsLoopbackAddr(c.HTTPAddr) && c.MutationToken == "" {
		return c, fmt.Errorf("ZV_MUTATION_TOKEN is required when ZV_HTTP_ADDR is not loopback")
	}
	if c.DiscoverySecret != "" && !validDiscoverySecret(c.DiscoverySecret) {
		return c, fmt.Errorf("ZV_DISCOVERY_SECRET must be 32 random bytes encoded as lowercase hex")
	}

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
	c.RenderTimeout, err = durationEnv("ZV_RENDER_TIMEOUT", "20m")
	if err != nil {
		return c, err
	}
	c.AgentTimeout, err = durationEnv("ZV_AGENT_TIMEOUT", "5m")
	if err != nil {
		return c, err
	}
	return c, nil
}

func validDiscoverySecret(secret string) bool {
	if len(secret) != 64 || secret != strings.ToLower(secret) {
		return false
	}
	decoded, err := hex.DecodeString(secret)
	return err == nil && len(decoded) == 32
}

// clearXAIAPIKeyEnvironment keeps the credential in config memory while
// preventing editor, recorder, FFmpeg, HLAE, CS2, and other subprocesses from
// inheriting it. EqualFold also removes casing variants on Windows, where
// environment variable names are case-insensitive.
func clearXAIAPIKeyEnvironment() error {
	return clearEnvironmentVariable(xaiAPIKeyEnvironmentVariable)
}

// clearGroqAPIKeyEnvironment does the same for the Groq captions fallback
// credential, in both its conventional and override names.
func clearGroqAPIKeyEnvironment() error {
	if err := clearEnvironmentVariable(groqAPIKeyEnvironmentVariable); err != nil {
		return err
	}
	return clearEnvironmentVariable(groqAPIKeyOverrideVariable)
}

// clearDiscoverySecretEnvironment prevents media and agent subprocesses from
// inheriting the desktop-only discovery credential after config has loaded it.
func clearDiscoverySecretEnvironment() error {
	return clearEnvironmentVariable(discoverySecretEnvironmentVariable)
}

func clearEnvironmentVariable(variable string) error {
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(name, variable) {
			continue
		}
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("unset %s: %w", variable, err)
		}
	}
	return nil
}

func (c config) recordWorkerEnabled() bool {
	// All three are needed to record. A partial set (explicit env for one tool,
	// auto-detection expected to fill the rest) is not an error: after
	// detection, an incomplete trio just leaves the worker disabled, and the
	// startup log plus /api/capabilities say which tool is missing.
	return c.RecorderPath != "" && c.HLAEPath != "" && c.CS2Path != ""
}

func (c config) composeWorkerEnabled() bool {
	return c.ComposerPath != ""
}

func (c config) renderWorkerEnabled() bool {
	return c.EditorPath != ""
}

func (c config) streamRenderWorkerEnabled() bool {
	return c.FFmpegPath != ""
}

func (c config) agentWorkerEnabled() bool {
	return c.CodexPath != ""
}

func (c config) ytdlpEnabled() bool {
	return c.YtdlpPath != ""
}

func (c config) whisperEnabled() bool {
	return c.WhisperPath != "" && c.WhisperModelPath != ""
}

func (c config) xaiEnabled() bool {
	return c.XAIAPIKey != ""
}

func (c config) groqEnabled() bool {
	return c.GroqAPIKey != ""
}

func (c config) firecrawlEnabled() bool {
	return c.FirecrawlAPIKey != ""
}

// streamAcquireWorkerEnabled reports whether the acquire-by-URL worker can
// run. It only needs yt-dlp; stream rendering (ffmpeg) is gated separately by
// streamRenderWorkerEnabled.
func (c config) streamAcquireWorkerEnabled() bool {
	return c.ytdlpEnabled()
}

// captureCapabilities is the readiness snapshot the HTTP layer serves at
// GET /api/capabilities and uses to gate record/generate. Worker enablement is
// fixed here at startup; the tool paths are reported so the web UI can tell the
// user which ones to set, and accessibility is re-checked per request.
func (c config) captureCapabilities(src captureToolSource) httpapi.Capabilities {
	tool := func(name, path string) httpapi.CaptureTool {
		return httpapi.CaptureTool{Name: name, Path: path, Source: src[name]}
	}
	return httpapi.Capabilities{
		RecordEnabled:  c.recordWorkerEnabled(),
		ComposeEnabled: c.composeWorkerEnabled(),
		RenderEnabled:  c.renderWorkerEnabled(),
		YtdlpEnabled:   c.ytdlpEnabled(),
		WhisperEnabled: c.whisperEnabled(),
		XAIEnabled:     c.xaiEnabled(),
		GroqEnabled:    c.groqEnabled(),
		RecordTools: []httpapi.CaptureTool{
			tool("ZV_RECORDER_PATH", c.RecorderPath),
			tool("ZV_HLAE_PATH", c.HLAEPath),
			tool("ZV_CS2_PATH", c.CS2Path),
		},
		RenderTools: []httpapi.CaptureTool{
			tool("ZV_EDITOR_PATH", c.EditorPath),
			tool("ZV_FFMPEG_PATH", c.FFmpegPath),
		},
		StreamTools: []httpapi.CaptureTool{
			tool("ZV_YTDLP_PATH", c.YtdlpPath),
			tool("ZV_WHISPER_PATH", c.WhisperPath),
			tool("ZV_WHISPER_MODEL", c.WhisperModelPath),
		},
	}
}

// missingRecordTools returns configured record tool paths ("NAME=path") that do
// not exist on disk, so startup can warn non-fatally about a misconfig such as
// the wrong HLAE install, without blocking a parse-only run.
func (c config) missingRecordTools() []string {
	var missing []string
	for _, t := range []struct{ name, path string }{
		{"ZV_RECORDER_PATH", c.RecorderPath},
		{"ZV_HLAE_PATH", c.HLAEPath},
		{"ZV_CS2_PATH", c.CS2Path},
	} {
		if t.path == "" {
			continue
		}
		if _, err := os.Stat(t.path); err != nil {
			missing = append(missing, t.name+"="+t.path)
		}
	}
	return missing
}

// missingRecordConfig lists the record-trio env names still empty after
// auto-detection when at least one is set, so main can log why the record
// worker is disabled instead of fatally rejecting a partial trio (which used
// to kill the desktop app, whose launcher sets only ZV_HLAE_PATH).
func (c config) missingRecordConfig() []string {
	recordValues := []struct{ name, value string }{
		{"ZV_RECORDER_PATH", c.RecorderPath},
		{"ZV_HLAE_PATH", c.HLAEPath},
		{"ZV_CS2_PATH", c.CS2Path},
	}
	anySet := false
	for _, t := range recordValues {
		anySet = anySet || t.value != ""
	}
	if !anySet {
		return nil
	}
	var missing []string
	for _, t := range recordValues {
		if t.value == "" {
			missing = append(missing, t.name)
		}
	}
	return missing
}

func durationEnv(key, def string) (string, error) {
	raw := envOr(key, def)
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return "", fmt.Errorf("%s must be a positive duration, got %q", key, raw)
	}
	return d.String(), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
