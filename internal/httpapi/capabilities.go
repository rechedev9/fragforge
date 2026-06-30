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
	RecordTools    []CaptureTool // recorder, HLAE, CS2
	RenderTools    []CaptureTool // editor, ffmpeg
}

// GetCapabilities handles GET /api/capabilities. It is read-only: the web UI
// polls it to tell the user whether gameplay capture is configured and, when it
// is not, exactly which tool paths to set. It never enqueues work.
func (h *Handlers) GetCapabilities(w http.ResponseWriter, _ *http.Request) {
	c := h.capabilities
	writeJSON(w, http.StatusOK, map[string]any{
		"record":  map[string]any{"enabled": c.RecordEnabled, "tools": resolveTools(c.RecordTools)},
		"render":  map[string]any{"enabled": c.RenderEnabled, "tools": resolveTools(c.RenderTools)},
		"compose": map[string]any{"enabled": c.ComposeEnabled},
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
