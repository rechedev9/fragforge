package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"regexp"

	"github.com/rechedev9/fragforge/internal/obs"
)

var healthChallengePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Health is a cheap liveness probe that never touches the database.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	response := map[string]string{"service": "fragforge", "status": "ok"}
	challenge := r.URL.Query().Get("challenge")
	secret := h.discoverySecret
	if secret == "" {
		// Compatibility for CLI-launched local orchestrators, which predate the
		// desktop discovery secret and may already have a mutation token.
		secret = h.mutationToken
	}
	if secret != "" && requestIsLoopback(r) && healthChallengePattern.MatchString(challenge) {
		endpoint := healthEndpoint(r)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write([]byte(challenge + "\n" + endpoint))
		response["endpoint"] = endpoint
		response["proof"] = hex.EncodeToString(mac.Sum(nil))
	}
	_ = json.NewEncoder(w).Encode(response)
}

func requestIsLoopback(r *http.Request) bool {
	return IsLoopbackAddr(r.RemoteAddr)
}

func healthEndpoint(r *http.Request) string {
	if address, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		return address.String()
	}
	return r.Host
}

// Metrics serves the local pipeline counters in the Prometheus text exposition
// format so a Prometheus server can scrape them. The counters live in the
// shared obs directory written by the CLI, batch runs, and workers.
func (h *Handlers) Metrics(w http.ResponseWriter, r *http.Request) {
	rec, err := obs.New(obs.DefaultDir())
	if err != nil {
		http.Error(w, "metrics unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	obs.WritePrometheus(w, rec.Snapshot())
}
