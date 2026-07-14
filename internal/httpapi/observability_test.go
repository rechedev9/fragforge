package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/obs"
)

func TestHealthHandler(t *testing.T) {
	h := &Handlers{discoverySecret: "discovery-secret", mutationToken: "mutation-secret"}
	rr := httptest.NewRecorder()
	challenge := strings.Repeat("a", 64)
	req := httptest.NewRequest("GET", "/healthz?challenge="+challenge, nil)
	req.RemoteAddr = "127.0.0.1:43210"
	h.Health(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"status":"ok"`) {
		t.Errorf("body: got %q", rr.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if got, want := body["service"], "fragforge"; got != want {
		t.Errorf("service: got %q want %q", got, want)
	}
	endpoint := body["endpoint"]
	mac := hmac.New(sha256.New, []byte("discovery-secret"))
	_, _ = mac.Write([]byte(challenge + "\n" + endpoint))
	if got, want := body["proof"], hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Errorf("proof: got %q want %q", got, want)
	}
	if strings.Contains(rr.Body.String(), "discovery-secret") || strings.Contains(rr.Body.String(), "mutation-secret") {
		t.Fatalf("health response exposed a secret: %q", rr.Body.String())
	}
}

func TestHealthHandlerFallsBackToMutationTokenForLoopbackCLI(t *testing.T) {
	h := &Handlers{mutationToken: "cli-token"}
	challenge := strings.Repeat("b", 64)
	req := httptest.NewRequest("GET", "/healthz?challenge="+challenge, nil)
	req.RemoteAddr = "[::1]:43210"
	rr := httptest.NewRecorder()
	h.Health(rr, req)

	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	endpoint := body["endpoint"]
	mac := hmac.New(sha256.New, []byte("cli-token"))
	_, _ = mac.Write([]byte(challenge + "\n" + endpoint))
	if got, want := body["proof"], hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Errorf("proof: got %q want %q", got, want)
	}
}

func TestHealthHandlerDoesNotProveIdentityToRemoteClients(t *testing.T) {
	h := &Handlers{discoverySecret: "discovery-secret", mutationToken: "mutation-secret"}
	challenge := strings.Repeat("c", 64)
	req := httptest.NewRequest("GET", "/healthz?challenge="+challenge, nil)
	// A remote peer cannot opt into local treatment by claiming a loopback Host.
	req.Host = "127.0.0.1:8080"
	req.RemoteAddr = "203.0.113.20:43210"
	rr := httptest.NewRecorder()
	h.Health(rr, req)

	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if _, ok := body["endpoint"]; ok {
		t.Errorf("remote health response has endpoint: %q", rr.Body.String())
	}
	if _, ok := body["proof"]; ok {
		t.Errorf("remote health response has proof: %q", rr.Body.String())
	}
}

func TestHealthHandlerBindsProofToListenerRatherThanClaimedHost(t *testing.T) {
	h := &Handlers{discoverySecret: "discovery-secret"}
	challenge := strings.Repeat("d", 64)
	req := httptest.NewRequest("GET", "/healthz?challenge="+challenge, nil)
	req.RemoteAddr = "127.0.0.1:43210"
	req.Host = "spoofed.example:9999"
	listener := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, listener))
	rr := httptest.NewRecorder()
	h.Health(rr, req)

	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if got, want := body["endpoint"], listener.String(); got != want {
		t.Errorf("endpoint: got %q want listener %q", got, want)
	}
}

func TestHealthHandlerIgnoresInvalidChallenge(t *testing.T) {
	h := &Handlers{discoverySecret: "discovery-secret"}
	req := httptest.NewRequest("GET", "/healthz?challenge=not-hex", nil)
	req.RemoteAddr = "127.0.0.1:43210"
	rr := httptest.NewRecorder()
	h.Health(rr, req)

	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if _, ok := body["proof"]; ok {
		t.Errorf("invalid challenge produced proof: %q", rr.Body.String())
	}
}

func TestMetricsHandlerServesCounters(t *testing.T) {
	t.Setenv("ZV_DATA_DIR", t.TempDir())
	rec, err := obs.New(obs.DefaultDir())
	if err != nil {
		t.Fatalf("obs.New: %v", err)
	}
	if err := rec.RecordError(obs.Event{Stage: obs.StageHTTP, Class: "boom", Message: "x"}); err != nil {
		t.Fatalf("RecordError: %v", err)
	}

	h := &Handlers{}
	rr := httptest.NewRecorder()
	h.Metrics(rr, httptest.NewRequest("GET", "/metrics", nil))
	if rr.Code != 200 {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `fragforge_errors_total{class="boom",stage="http"} 1`) {
		t.Errorf("metrics body missing seeded counter:\n%s", body)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type: got %q", ct)
	}
}
