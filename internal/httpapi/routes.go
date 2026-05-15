package httpapi

import "github.com/go-chi/chi/v5"

// Routes returns a chi router with all orchestrator routes wired.
func Routes(h *Handlers) chi.Router {
	r := chi.NewRouter()
	r.Post("/api/jobs", h.CreateJob)
	r.Get("/api/jobs/{id}", h.GetJob)
	r.Get("/api/jobs/{id}/plan", h.GetPlan)
	r.Get("/api/jobs/{id}/final", h.GetFinal)
	r.Post("/api/jobs/{id}/record", h.StartRecording)
	r.Post("/api/jobs/{id}/compose", h.StartComposition)
	return r
}
