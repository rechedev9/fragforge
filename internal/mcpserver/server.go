// Package mcpserver exposes the FragForge demo-to-Short pipeline as MCP tools
// over stdio, so coding agents (Claude Code, Codex) can drive the desktop app
// the same way the terminal UI does. It is a pure thin client of the
// orchestrator HTTP API: every tool maps onto an internal/tuiclient method (plus
// one synthesized next_step reconciler), and it imports no server-side domain
// packages. Diagnostics go to stderr only - stdout carries the MCP JSON-RPC wire
// and must stay clean.
package mcpserver

import (
	"context"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// Options configures Run.
type Options struct {
	// URL is the orchestrator base URL from --url; empty triggers discovery
	// (see Resolve).
	URL string
	// Token is the X-FragForge-Token from --token; empty falls back to
	// $ZV_MUTATION_TOKEN inside tuiclient.
	Token string
	// Stderr receives startup and diagnostic logging. It must not be stdout,
	// which the stdio transport owns. A nil Stderr silences logging.
	Stderr io.Writer
	// Version is the build version string reported as the MCP server's
	// implementation version.
	Version string
}

// Run resolves the orchestrator URL, builds the client, registers every tool,
// and serves MCP over stdio until the client disconnects or ctx is cancelled.
// It always proceeds to serve even when the orchestrator is unreachable: MCP
// clients launch the server eagerly, and each tool re-probes health and returns
// orchestrator_unavailable until the desktop app is up.
func Run(ctx context.Context, opts Options) error {
	res := Resolve(opts.URL)
	client := tuiclient.New(tuiclient.Config{BaseURL: res.URL, Token: opts.Token})

	healthClient := &http.Client{Timeout: 2 * time.Second}
	d := deps{
		client: client,
		healthy: func(ctx context.Context) bool {
			return probeHealth(ctx, healthClient, client.BaseURL())
		},
	}

	logOut := opts.Stderr
	if logOut == nil {
		logOut = io.Discard
	}
	logger := log.New(logOut, "zv mcp: ", 0)
	logger.Printf("orchestrator %s (source: %s) reachable=%t", client.BaseURL(), res.Source, d.healthy(ctx))

	srv := mcp.NewServer(&mcp.Implementation{Name: "fragforge", Version: opts.Version}, nil)
	registerTools(srv, d)
	return srv.Run(ctx, &mcp.StdioTransport{})
}
