package httpapi

import "github.com/go-chi/chi/v5"

// Routes returns a chi router with all orchestrator routes wired.
func Routes(h *Handlers) chi.Router {
	r := chi.NewRouter()
	r.Post("/api/jobs", h.CreateJob)
	r.Get("/api/jobs/{id}", h.GetJob)
	r.Get("/api/jobs/{id}/plan", h.GetPlan)
	return r
}
