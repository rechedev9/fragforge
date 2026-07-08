package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// stubBackend is the proxy's upstream; it records whether a request reached it
// and echoes a fixed body so proxied 200s are distinguishable.
func stubBackend(t *testing.T, reached *bool) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*reached = true
		_, _ = w.Write([]byte("upstream-ok"))
	})
}

func TestLoopbackHandlerAuth(t *testing.T) {
	const token = "secret-token"
	origins := []string{"https://app.fragforge.gg"}

	tests := []struct {
		name        string
		method      string
		authHeader  string
		wantStatus  int
		wantReached bool
	}{
		{"missing token 401", http.MethodGet, "", http.StatusUnauthorized, false},
		{"wrong token 401", http.MethodGet, "Bearer nope", http.StatusUnauthorized, false},
		{"malformed header 401", http.MethodGet, "Basic secret-token", http.StatusUnauthorized, false},
		{"valid token 200", http.MethodGet, "Bearer " + token, http.StatusOK, true},
		{"valid token post 200", http.MethodPost, "Bearer " + token, http.StatusOK, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reached bool
			h := newLoopbackHandler(stubBackend(t, &reached), token, origins)
			req := httptest.NewRequest(tt.method, "/api/jobs", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d", rec.Code, tt.wantStatus)
			}
			if reached != tt.wantReached {
				t.Errorf("got upstream reached %v, want %v", reached, tt.wantReached)
			}
		})
	}
}

func TestLoopbackHandlerCORS(t *testing.T) {
	origins := []string{"https://app.fragforge.gg", "https://staging.fragforge.gg"}
	tests := []struct {
		name       string
		origin     string
		wantEcho   string
		wantMethod bool
	}{
		{"allowed origin echoed", "https://app.fragforge.gg", "https://app.fragforge.gg", true},
		{"second allowed origin echoed", "https://staging.fragforge.gg", "https://staging.fragforge.gg", true},
		{"denied origin not echoed", "https://evil.example", "", false},
		{"no origin", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var reached bool
			h := newLoopbackHandler(stubBackend(t, &reached), "tok", origins)
			req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
			req.Header.Set("Authorization", "Bearer tok")
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tt.wantEcho {
				t.Errorf("got Allow-Origin %q, want %q", got, tt.wantEcho)
			}
			if got := rec.Header().Get("Vary"); got != "Origin" {
				t.Errorf("got Vary %q, want Origin", got)
			}
			gotMethod := rec.Header().Get("Access-Control-Allow-Methods") != ""
			if gotMethod != tt.wantMethod {
				t.Errorf("got Allow-Methods present %v, want %v", gotMethod, tt.wantMethod)
			}
		})
	}
}

func TestLoopbackHandlerPreflightBypassesAuth(t *testing.T) {
	origins := []string{"https://app.fragforge.gg"}
	var reached bool
	h := newLoopbackHandler(stubBackend(t, &reached), "tok", origins)

	req := httptest.NewRequest(http.MethodOptions, "/api/jobs", nil)
	req.Header.Set("Origin", "https://app.fragforge.gg")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Private-Network", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("got status %d, want 204", rec.Code)
	}
	if reached {
		t.Errorf("preflight reached upstream, want short-circuit")
	}
	if got := rec.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Errorf("got Allow-Private-Network %q, want true", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.fragforge.gg" {
		t.Errorf("got Allow-Origin %q, want the request origin", got)
	}
}

func TestLoopbackHandlerPreflightWithoutPNAHeader(t *testing.T) {
	h := newLoopbackHandler(stubBackend(t, new(bool)), "tok", []string{"https://app.fragforge.gg"})
	req := httptest.NewRequest(http.MethodOptions, "/api/jobs", nil)
	req.Header.Set("Origin", "https://app.fragforge.gg")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Private-Network"); got != "" {
		t.Errorf("got Allow-Private-Network %q, want empty when not requested", got)
	}
}

func TestValidateLoopbackAddr(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wantErr bool
	}{
		{"loopback ipv4 default", "127.0.0.1:8090", false},
		{"loopback ipv4 dynamic", "127.0.0.1:0", false},
		{"localhost", "localhost:8090", false},
		{"loopback ipv6", "[::1]:8090", false},
		{"non-loopback ipv4", "0.0.0.0:8090", true},
		{"lan ip", "192.168.1.5:8090", true},
		{"missing port", "127.0.0.1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLoopbackAddr(tt.addr)
			if (err != nil) != tt.wantErr {
				t.Errorf("got err %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunLoopbackRejectsNonLoopbackAddr(t *testing.T) {
	err := RunLoopback(context.Background(), LoopbackConfig{Addr: "0.0.0.0:8090", Token: "tok"})
	if err == nil {
		t.Fatal("got nil error, want rejection of non-loopback bind")
	}
}

// buildStubOrchestrator compiles a tiny stand-in for zv-orchestrator whose
// behavior is chosen by mode: "serve" binds ZV_HTTP_ADDR and answers /healthz
// and any other path; "die" exits non-zero immediately.
func buildStubOrchestrator(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	body := `package main

import (
	"net/http"
	"os"
)

func main() {
	if ` + boolLit(mode == "die") + ` {
		os.Exit(3)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("stub-orchestrator"))
	})
	_ = http.ListenAndServe(os.Getenv("ZV_HTTP_ADDR"), mux)
}
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "stub-orchestrator"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)
	goExe, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("find go binary: %v", err)
	}
	if o, err := exec.Command(goExe, "build", "-o", out, src).CombinedOutput(); err != nil {
		t.Fatalf("build stub orchestrator: %v\n%s", err, o)
	}
	return out
}

func boolLit(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestRunLoopbackSupervisesChild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a stub binary; skipped in -short")
	}
	orch := buildStubOrchestrator(t, "serve")
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- RunLoopback(ctx, LoopbackConfig{
			Addr:             "127.0.0.1:0",
			Token:            "tok",
			Origins:          []string{"https://app.fragforge.gg"},
			OrchestratorPath: orch,
			DataDir:          t.TempDir(),
		})
	}()

	// Cancelling the context must terminate the child and unblock RunLoopback.
	// Give the child time to come up first.
	time.Sleep(300 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Fatalf("RunLoopback returned %v, want context.Canceled or nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunLoopback did not return after context cancel")
	}
}

func TestRunLoopbackChildDies(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a stub binary; skipped in -short")
	}
	orch := buildStubOrchestrator(t, "die")
	err := RunLoopback(context.Background(), LoopbackConfig{
		Addr:             "127.0.0.1:0",
		Token:            "tok",
		OrchestratorPath: orch,
	})
	if err == nil {
		t.Fatal("got nil error, want non-nil when child exits before ready")
	}
}
