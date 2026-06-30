package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
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
}

const (
	databaseURLMemory = "memory"
	queueModeRedis    = "redis"
	queueModeInline   = "inline"
)

func loadConfig() (config, error) {
	c := config{
		HTTPAddr:      envOr("ZV_HTTP_ADDR", "127.0.0.1:8080"),
		DatabaseURL:   os.Getenv("ZV_DATABASE_URL"),
		RedisAddr:     envOr("ZV_REDIS_ADDR", "localhost:6379"),
		QueueMode:     envOr("ZV_QUEUE_MODE", queueModeRedis),
		DataDir:       envOr("ZV_DATA_DIR", "./data"),
		MediaWorkDir:  os.Getenv("ZV_MEDIA_WORK_DIR"),
		RecorderPath:  os.Getenv("ZV_RECORDER_PATH"),
		ComposerPath:  os.Getenv("ZV_COMPOSER_PATH"),
		EditorPath:    os.Getenv("ZV_EDITOR_PATH"),
		HLAEPath:      os.Getenv("ZV_HLAE_PATH"),
		CS2Path:       os.Getenv("ZV_CS2_PATH"),
		RecordHUD:     os.Getenv("ZV_RECORD_HUD"),
		FFmpegPath:    os.Getenv("ZV_FFMPEG_PATH"),
		FFprobePath:   os.Getenv("ZV_FFPROBE_PATH"),
		MusicDir:      os.Getenv("ZV_MUSIC_DIR"),
		MutationToken: os.Getenv("ZV_MUTATION_TOKEN"),
		CodexPath:     os.Getenv("ZV_CODEX_PATH"),
		CodexModel:    os.Getenv("ZV_CODEX_MODEL"),
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("ZV_DATABASE_URL is required")
	}
	if c.DatabaseURL == databaseURLMemory && c.QueueMode == queueModeRedis {
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

// captureCapabilities is the readiness snapshot the HTTP layer serves at
// GET /api/capabilities and uses to gate record/generate. Worker enablement is
// fixed here at startup; the tool paths are reported so the web UI can tell the
// user which ones to set, and accessibility is re-checked per request.
func (c config) captureCapabilities(src captureToolSource) httpapi.Capabilities {
	recordTool := func(name, path string) httpapi.CaptureTool {
		return httpapi.CaptureTool{Name: name, Path: path, Source: src[name]}
	}
	// Render tools are not auto-detected, so their source is just env-or-none.
	renderTool := func(name, path string) httpapi.CaptureTool {
		source := "none"
		if path != "" {
			source = "env"
		}
		return httpapi.CaptureTool{Name: name, Path: path, Source: source}
	}
	return httpapi.Capabilities{
		RecordEnabled:  c.recordWorkerEnabled(),
		ComposeEnabled: c.composeWorkerEnabled(),
		RenderEnabled:  c.renderWorkerEnabled(),
		RecordTools: []httpapi.CaptureTool{
			recordTool("ZV_RECORDER_PATH", c.RecorderPath),
			recordTool("ZV_HLAE_PATH", c.HLAEPath),
			recordTool("ZV_CS2_PATH", c.CS2Path),
		},
		RenderTools: []httpapi.CaptureTool{
			renderTool("ZV_EDITOR_PATH", c.EditorPath),
			renderTool("ZV_FFMPEG_PATH", c.FFmpegPath),
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
