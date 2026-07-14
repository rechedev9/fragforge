package httpapi

import (
	"net/http"
	"os"
)

// CaptureTool is one external tool the media pipeline needs. Configured means a
// path is set; Accessible means that path exists on disk right now. Accessible
// is recomputed per request so a freshly installed binary flips without a
// restart, even though worker enablement itself is frozen at startup.
type CaptureTool struct {
	Name       string `json:"name"`   // the env var, e.g. "ZV_HLAE_PATH"
	Path       string `json:"path"`   // resolved path, "" when not found
	Source     string `json:"source"` // "env" (set explicitly) | "detected" (auto-found) | "none"
	Configured bool   `json:"configured"`
	Accessible bool   `json:"accessible"`
}

// Capabilities is the orchestrator's capture-readiness snapshot: which media
// workers were enabled at startup and the tool paths each needs. The enabled
// flags are fixed at startup (workers are wired once); the per-tool paths are
// re-checked for accessibility on every GET /api/capabilities.
type Capabilities struct {
	RecordEnabled  bool
	ComposeEnabled bool
	RenderEnabled  bool
	// YtdlpEnabled reports whether acquisition-by-URL (POST /api/stream-jobs
	// with a source_url) can run: a yt-dlp binary is configured.
	YtdlpEnabled bool
	// WhisperEnabled reports whether a stream render can burn in captions using
	// the local whisper.cpp backend: both a whisper-cli binary and a model
	// file are configured.
	WhisperEnabled bool
	// XAIEnabled reports whether a stream render can burn in captions using the
	// xAI cloud backend: an xAI API key is configured. The key itself is never
	// reported, only this boolean.
	XAIEnabled  bool
	RecordTools []CaptureTool // recorder, HLAE, CS2
	RenderTools []CaptureTool // editor, ffmpeg
	StreamTools []CaptureTool // yt-dlp, whisper binary, whisper model
}

// captionsEnabled reports whether at least one captions transcription
// backend (xAI or local whisper) is configured.
func (c Capabilities) captionsEnabled() bool {
	return c.WhisperEnabled || c.XAIEnabled
}

// GetCapabilities handles GET /api/capabilities. It is read-only: the web UI
// polls it to tell the user whether gameplay capture is configured and, when it
// is not, exactly which tool paths to set. It never enqueues work.
func (h *Handlers) GetCapabilities(w http.ResponseWriter, _ *http.Request) {
	c := h.capabilities
	writeJSON(w, http.StatusOK, map[string]any{
		"auth": map[string]any{
			"read_requires_token": h.requireReadAuth && h.mutationToken != "",
		},
		"record":  map[string]any{"enabled": c.RecordEnabled, "tools": resolveTools(c.RecordTools)},
		"render":  map[string]any{"enabled": c.RenderEnabled, "tools": resolveTools(c.RenderTools)},
		"compose": map[string]any{"enabled": c.ComposeEnabled},
		"stream": map[string]any{
			"ytdlp_enabled":   c.YtdlpEnabled,
			"whisper_enabled": c.WhisperEnabled,
			"xai_enabled":     c.XAIEnabled,
			"tools":           resolveTools(c.StreamTools),
		},
	})
}

// resolveTools fills Configured/Accessible from the current path and disk state,
// leaving the startup-provided Name/Path untouched.
func resolveTools(tools []CaptureTool) []CaptureTool {
	out := make([]CaptureTool, len(tools))
	for i, t := range tools {
		t.Configured = t.Path != ""
		t.Accessible = t.Configured && pathExists(t.Path)
		out[i] = t
	}
	return out
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// requireRecordEnabled reports whether gameplay capture is configured. When it
// is not, it writes a 409 naming the env vars to set, so an unconfigured record
// attempt fails as an actionable 4xx the web client can surface, instead of
// enqueuing a task no worker will consume (which previously left the reel stuck
// at QUEUED with only a server-side 500).
func (h *Handlers) requireRecordEnabled(w http.ResponseWriter) bool {
	if h.capabilities.RecordEnabled {
		return true
	}
	writeError(w, http.StatusConflict, "recording is not configured on this machine; set ZV_RECORDER_PATH, ZV_HLAE_PATH and ZV_CS2_PATH and restart the orchestrator")
	return false
}

// requireYtdlpEnabled reports whether acquisition-by-URL is configured. When
// it is not, it writes a 409 naming the env var to set, so POST
// /api/stream-jobs with a source_url fails as an actionable 4xx instead of
// creating a job no worker will ever advance.
func (h *Handlers) requireYtdlpEnabled(w http.ResponseWriter) bool {
	if h.capabilities.YtdlpEnabled {
		return true
	}
	writeError(w, http.StatusConflict, "acquiring a stream job by URL is not configured on this machine; install yt-dlp on PATH (or set ZV_YTDLP_PATH) and restart the orchestrator")
	return false
}

// requireCaptionsEnabled reports whether burned-in captions are configured,
// via either the xAI cloud backend or the local whisper backend. When
// neither is, it writes a 409 naming the env vars to set, so starting a
// render with captions enabled fails fast instead of the worker failing
// mid-render.
func (h *Handlers) requireCaptionsEnabled(w http.ResponseWriter) bool {
	if h.capabilities.captionsEnabled() {
		return true
	}
	writeError(w, http.StatusConflict, "captions are enabled in the edit plan but no transcription backend is configured on this machine; configure an xAI key in FragForge Studio Settings (or set XAI_API_KEY), or set ZV_WHISPER_PATH and ZV_WHISPER_MODEL, then restart")
	return false
}
