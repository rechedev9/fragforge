package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rechedev9/fragforge/internal/httpapi"
)

type config struct {
	HTTPAddr          string
	DatabaseURL       string
	RedisAddr         string
	QueueMode         string
	DataDir           string
	WorkerConcurrency int
	MediaWorkDir      string
	RecorderPath      string
	ComposerPath      string
	EditorPath        string
	HLAEPath          string
	CS2Path           string
	RecordHUD         string
	FFmpegPath        string
	FFprobePath       string
	MusicDir          string
	RecordTimeout     string
	ComposeTimeout    string
	RenderTimeout     string
	MutationToken     string
	CodexPath         string
	CodexModel        string
	AgentTimeout      string
	YtdlpPath         string
	WhisperPath       string
	WhisperModelPath  string
	GroqAPIKey        string
	GroqModel         string
}

const (
	databaseURLMemory = "memory"
	// databaseURLSQLite selects the on-disk SQLite job repository. Accepts the
	// bare value "sqlite" (stores <DataDir>/jobs.db) or "sqlite:<path>".
	databaseURLSQLite = "sqlite"
	queueModeRedis    = "redis"
	queueModeInline   = "inline"
)

// isLocalDatabase reports whether the database URL selects a single-machine
// backend (in-memory or SQLite) rather than Postgres. Local backends run the
// queue inline, since there is no Redis alongside them.
func isLocalDatabase(url string) bool {
	return url == databaseURLMemory || url == databaseURLSQLite || strings.HasPrefix(url, databaseURLSQLite+":")
}

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
		RedisAddr:        envOr("ZV_REDIS_ADDR", "localhost:6379"),
		QueueMode:        envOr("ZV_QUEUE_MODE", queueModeRedis),
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
		CodexPath:        os.Getenv("ZV_CODEX_PATH"),
		CodexModel:       os.Getenv("ZV_CODEX_MODEL"),
		YtdlpPath:        os.Getenv("ZV_YTDLP_PATH"),
		WhisperPath:      os.Getenv("ZV_WHISPER_PATH"),
		WhisperModelPath: os.Getenv("ZV_WHISPER_MODEL"),
		// GROQ_API_KEY is the key's conventional name across Groq's own tooling;
		// ZV_GROQ_API_KEY is an explicit override for when a user-level
		// GROQ_API_KEY is set for something unrelated. Neither is auto-detected
		// (an API key cannot be probed on PATH or disk).
		GroqAPIKey: firstNonEmpty(os.Getenv("ZV_GROQ_API_KEY"), os.Getenv("GROQ_API_KEY")),
		GroqModel:  os.Getenv("ZV_GROQ_MODEL"),
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("ZV_DATABASE_URL is required")
	}
	if isLocalDatabase(c.DatabaseURL) && c.QueueMode == queueModeRedis {
		c.QueueMode = queueModeInline
	}
	if c.QueueMode != queueModeRedis && c.QueueMode != queueModeInline {
		return c, fmt.Errorf("ZV_QUEUE_MODE must be %q or %q", queueModeRedis, queueModeInline)
	}
	if !isLoopbackHTTPAddr(c.HTTPAddr) && c.MutationToken == "" {
		return c, fmt.Errorf("ZV_MUTATION_TOKEN is required when ZV_HTTP_ADDR is not loopback")
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
	if err := c.validateMediaConfig(); err != nil {
		return c, err
	}
	return c, nil
}

func (c config) recordWorkerEnabled() bool {
	// All three are needed to record; with auto-detection a path may be filled
	// for some tools and not others, so require the full set (validateMediaConfig
	// still enforces the all-or-nothing contract for explicitly-set env).
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

func (c config) groqEnabled() bool {
	return c.GroqAPIKey != ""
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

// firstNonEmpty returns the first non-empty value, or "" if all are empty.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func isLoopbackHTTPAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
