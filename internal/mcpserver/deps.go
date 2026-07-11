package mcpserver

import (
	"context"
	"io"
	"net/http"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// Poll-hint seconds embedded in start_* outputs and not_ready errors, so agents
// pace their polling instead of hammering the API. Parse-family stages settle in
// seconds; record/render/compose take minutes.
const (
	pollHintParse   = 2
	pollHintMedia   = 10
	pollHintDefault = 5
)

// deps is the shared context every tool handler needs: the orchestrator client
// plus a cheap health re-probe. It is a plain value passed by copy; the client
// and probe are safe for concurrent use.
type deps struct {
	client  *tuiclient.Client
	healthy func(ctx context.Context) bool
}

// requireOrchestrator re-probes the orchestrator before a tool touches the API.
// It returns a non-nil *toolError when unreachable, covering both "the desktop
// app was not up when the agent registered the server" and "the app closed
// mid-session". The probe is a loopback healthz GET, so it is cheap.
func (d deps) requireOrchestrator(ctx context.Context) *toolError {
	if d.healthy(ctx) {
		return nil
	}
	te := unavailableError(d.client.BaseURL())
	return &te
}

// capabilityKind names a capture stage gated client-side before a mutate call.
type capabilityKind int

const (
	capabilityRecord capabilityKind = iota
	capabilityRender
)

// requireCapability fetches the orchestrator capabilities and returns a non-nil
// *toolError when the requested stage is not configured, so start_recording and
// start_render fail fast with capability_missing instead of a server 4xx. The
// extra loopback round-trip buys a clear, agent-actionable error.
func (d deps) requireCapability(ctx context.Context, kind capabilityKind) *toolError {
	caps, err := d.client.Capabilities(ctx)
	if err != nil {
		te := classify(ctx, d, err, "")
		return &te
	}
	var (
		enabled bool
		name    string
	)
	switch kind {
	case capabilityRecord:
		enabled, name = caps.Record.Enabled, "record"
	case capabilityRender:
		enabled, name = caps.Render.Enabled, "render"
	}
	if enabled {
		return nil
	}
	te := capabilityMissingError(name)
	return &te
}

// probeHealth reports whether the orchestrator answers GET /healthz with a 2xx.
// tuiclient has no healthz method (healthz is not part of the job API), so this
// is a local helper.
func probeHealth(ctx context.Context, hc *http.Client, baseURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
