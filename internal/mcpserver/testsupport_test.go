package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// newStubServer starts an httptest server from mux, adding the GET /healthz
// route that requireOrchestrator probes so every stub answers the pre-flight
// reachability check. Tests register only the API routes they exercise.
func newStubServer(t *testing.T, mux *http.ServeMux) *httptest.Server {
	t.Helper()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// depsFor builds a deps whose client and health probe both target srv, so tool
// handlers exercise the real requireOrchestrator loopback probe end to end.
func depsFor(t *testing.T, srv *httptest.Server) deps {
	t.Helper()
	hc := srv.Client()
	client := tuiclient.New(tuiclient.Config{BaseURL: srv.URL, HTTPClient: hc})
	return deps{
		client:  client,
		healthy: func(ctx context.Context) bool { return probeHealth(ctx, hc, client.BaseURL()) },
	}
}

// unavailableDeps builds a deps whose orchestrator is unreachable: the health
// probe always fails and the client points at a closed loopback port.
func unavailableDeps() deps {
	client := tuiclient.New(tuiclient.Config{BaseURL: "http://127.0.0.1:1"})
	return deps{
		client:  client,
		healthy: func(context.Context) bool { return false },
	}
}

// newMCPSession registers d's tools on a real MCP server and connects a real
// MCP client over an in-memory transport, returning the client session. This
// drives every tool through the SDK's schema validation and dispatch, exactly
// as a coding agent would over stdio.
func newMCPSession(t *testing.T, d deps) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: "fragforge", Version: "test"}, nil)
	registerTools(srv, d)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

// callTool invokes name with args and fails on a protocol-level error (a
// tool-level error surfaces as res.IsError, which the caller inspects).
func callTool(t *testing.T, cs *mcp.ClientSession, name string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: protocol error: %v", name, err)
	}
	return res
}

// resultText returns the first text content block of a tool result, which for
// both success (structured JSON) and error (toolError JSON) paths carries the
// JSON body an agent reads.
func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *mcp.TextContent", res.Content[0])
	}
	return tc.Text
}

// decodeOK asserts res is a success and decodes its JSON body into out.
func decodeOK(t *testing.T, res *mcp.CallToolResult, out any) {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool returned isError, body = %s", resultText(t, res))
	}
	if err := json.Unmarshal([]byte(resultText(t, res)), out); err != nil {
		t.Fatalf("decode result body: %v\nbody = %s", err, resultText(t, res))
	}
}

// decodeErr asserts res is a tool-level error and decodes its structured body.
func decodeErr(t *testing.T, res *mcp.CallToolResult) toolError {
	t.Helper()
	if !res.IsError {
		t.Fatalf("tool did not return isError, body = %s", resultText(t, res))
	}
	var te toolError
	if err := json.Unmarshal([]byte(resultText(t, res)), &te); err != nil {
		t.Fatalf("decode error body: %v\nbody = %s", err, resultText(t, res))
	}
	return te
}

// writeJSON is the stub-side response helper.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
