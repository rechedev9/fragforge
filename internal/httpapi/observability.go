package httpapi

import (
	"net/http"

	"github.com/rechedev9/fragforge/internal/obs"
)

// Health is a cheap liveness probe that never touches the database.
func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
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
