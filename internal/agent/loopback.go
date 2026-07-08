package agent

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/rechedev9/fragforge/internal/httpapi"
)

// LoopbackConfig configures the loopback data-plane proxy the agent fronts the
// child orchestrator with.
type LoopbackConfig struct {
	// Addr is the proxy bind address (FRAGFORGE_LOOPBACK_ADDR). It must resolve
	// to a loopback host; non-loopback binds are rejected.
	Addr string
	// Token is the Bearer credential required on every non-preflight request.
	Token string
	// Origins is the CORS allowlist; Access-Control-Allow-Origin echoes a request
	// Origin only when it appears here.
	Origins []string
	// OrchestratorPath is the child binary to run. Empty resolves the default
	// zv-orchestrator next to this executable or on PATH.
	OrchestratorPath string
	// DataDir, when set, is passed to the child as ZV_DATA_DIR so its sqlite job
	// history lands alongside the agent's data.
	DataDir string
}

// orchestratorReadyTimeout bounds how long RunLoopback waits for the child
// orchestrator to answer GET /healthz before giving up. The child auto-detects
// capture tools on startup, so allow generous headroom.
const orchestratorReadyTimeout = 30 * time.Second

// RunLoopback starts and supervises a child zv-orchestrator bound to a dynamic
// loopback port and fronts it with an auth+CORS reverse proxy on cfg.Addr.
//
// The child is owned here: it is started, waited on in a dedicated goroutine,
// and terminated when ctx is cancelled or the proxy fails. If the child exits
// on its own, RunLoopback returns a non-nil error so a supervisor can restart
// the agent (in-process restart is out of scope).
func RunLoopback(ctx context.Context, cfg LoopbackConfig) error {
	if err := validateLoopbackAddr(cfg.Addr); err != nil {
		return err
	}
	// Defense in depth: an empty token would make authorized match any (or no)
	// credential, so refuse to run an unauthenticated data plane. The agent
	// self-heals a legacy config to a real token before it reaches here.
	if cfg.Token == "" {
		return fmt.Errorf("loopback token is required")
	}
	orch := cfg.OrchestratorPath
	if orch == "" {
		orch = resolveOrchestrator()
	}

	port, err := freeLoopbackPort()
	if err != nil {
		return err
	}
	childAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	target := &url.URL{Scheme: "http", Host: childAddr}

	// #nosec G204 -- orch is a fixed FragForge binary name resolved on PATH or
	// next to this executable, not attacker-controlled input.
	child := exec.Command(orch)
	child.Env = append(os.Environ(),
		"ZV_HTTP_ADDR="+childAddr,
		"ZV_DATABASE_URL=sqlite",
		"ZV_QUEUE_MODE=inline",
	)
	if cfg.DataDir != "" {
		child.Env = append(child.Env, "ZV_DATA_DIR="+cfg.DataDir)
	}
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		return fmt.Errorf("start orchestrator: %w", err)
	}

	// The child goroutine's owner is RunLoopback: it delivers the single Wait
	// result on childExit (buffered so the goroutine never blocks on exit).
	childExit := make(chan error, 1)
	go func() { childExit <- child.Wait() }()

	if err := waitOrchestratorReady(ctx, target, childExit); err != nil {
		_ = child.Process.Kill()
		return err
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           newLoopbackHandler(proxy, cfg.Token, cfg.Origins),
		ReadHeaderTimeout: 10 * time.Second,
	}
	srvErr := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srvErr <- err
		}
	}()

	var (
		retErr    error
		childDied bool
	)
	select {
	case <-ctx.Done():
		retErr = ctx.Err()
	case err := <-childExit:
		childDied = true
		retErr = fmt.Errorf("orchestrator exited: %w", err)
	case err := <-srvErr:
		retErr = fmt.Errorf("loopback proxy: %w", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(shutdownCtx)
	cancel()
	if !childDied {
		_ = child.Process.Kill()
		<-childExit
	}
	return retErr
}

// waitOrchestratorReady polls the child's /healthz until it answers, the child
// exits, ctx is cancelled, or the ready timeout elapses.
func waitOrchestratorReady(ctx context.Context, target *url.URL, childExit <-chan error) error {
	deadline := time.After(orchestratorReadyTimeout)
	healthURL := target.String() + "/healthz"
	client := &http.Client{Timeout: 2 * time.Second}
	// Exponential backoff: the child's cold start (DB init, capture-tool
	// detection) takes hundreds of ms to seconds, so a flat fast tick would
	// mostly burn connection-refused dials.
	wait := 50 * time.Millisecond
	const maxWait = 500 * time.Millisecond
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return fmt.Errorf("build healthz request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-childExit:
			return fmt.Errorf("orchestrator exited before ready: %w", err)
		case <-deadline:
			return fmt.Errorf("orchestrator not ready after %s", orchestratorReadyTimeout)
		case <-time.After(wait):
			if wait *= 2; wait > maxWait {
				wait = maxWait
			}
		}
	}
}

// newLoopbackHandler wraps the reverse proxy with the loopback data-plane
// policy: CORS allowlist, Private Network Access preflight, and Bearer auth.
// CORS preflight (OPTIONS) bypasses auth so the browser can probe before it
// holds the token; every other request needs a constant-time-matched token.
func newLoopbackHandler(proxy http.Handler, token string, origins []string) http.Handler {
	allow := make(map[string]bool, len(origins))
	for _, o := range origins {
		if o = strings.TrimSpace(o); o != "" {
			allow[o] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Vary: Origin always, so shared caches never serve an allow-origin
		// header keyed to a different requester.
		w.Header().Add("Vary", "Origin")
		if origin := r.Header.Get("Origin"); origin != "" && allow[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}
		if r.Method == http.MethodOptions {
			if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
				w.Header().Set("Access-Control-Allow-Private-Network", "true")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !authorized(r, token) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func authorized(r *http.Request, token string) bool {
	// An empty configured token must authorize nothing: ConstantTimeCompare of
	// two empty strings is 1, which would let a bare "Authorization: Bearer "
	// (or a legacy tokenless config) through.
	if token == "" {
		return false
	}
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimPrefix(h, prefix)
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// validateLoopbackAddr rejects a bind that is not a loopback host, so the
// data-plane proxy can never be reachable off the machine. It wraps the shared
// httpapi.IsLoopbackAddr predicate with the agent's error message.
func validateLoopbackAddr(addr string) error {
	if !httpapi.IsLoopbackAddr(addr) {
		return fmt.Errorf("loopback addr %q must bind a loopback host", addr)
	}
	return nil
}

// freeLoopbackPort reserves an ephemeral loopback port from the OS and returns
// it, closing the listener so the child can bind it. This has a small TOCTOU
// window, accepted because the child binds immediately after.
func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve loopback port: %w", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// resolveOrchestrator finds the zv-orchestrator binary next to this executable
// first, then on PATH, mirroring how the zv launcher resolves its subcommands.
func resolveOrchestrator() string {
	name := "zv-orchestrator"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if exe, err := os.Executable(); err == nil {
		cand := filepath.Join(filepath.Dir(exe), name)
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	if found, err := exec.LookPath(name); err == nil {
		return found
	}
	return name
}
