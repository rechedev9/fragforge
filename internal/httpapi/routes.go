package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Routes returns a chi router with all orchestrator routes wired.
func Routes(h *Handlers) chi.Router {
	r := chi.NewRouter()
	r.Use(h.requireMutationToken)
	r.Get("/", h.Workbench)
	r.Get("/api/loadouts", h.ListLoadouts)
	r.Get("/api/presets", h.ListPresets)
	r.Post("/api/jobs", h.CreateJob)
	r.Get("/api/jobs", h.ListJobs)
	r.Get("/api/jobs/{id}", h.GetJob)
	r.Get("/api/jobs/{id}/plan", h.GetPlan)
	r.Get("/api/jobs/{id}/roster", h.GetRoster)
	r.Post("/api/jobs/{id}/parse", h.StartParse)
	r.Get("/api/jobs/{id}/moments", h.GetMoments)
	r.Get("/api/jobs/{id}/final", h.GetFinal)
	r.Post("/api/jobs/{id}/record", h.StartRecording)
	r.Post("/api/jobs/{id}/compose", h.StartComposition)
	r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)
	r.Get("/api/jobs/{id}/renders/{variant}", h.GetRenderVariant)
	r.Get("/api/jobs/{id}/renders/{variant}/publish", h.GetRenderPublishBoard)
	r.Post("/api/jobs/{id}/renders/{variant}/publish/uploaded", h.SetRenderUploaded)
	r.Get("/api/jobs/{id}/renders/{variant}/quality", h.GetRenderQuality)
	r.Post("/api/jobs/{id}/renders/{variant}/agent/captions", h.StartCaptionAgent)
	r.Get("/api/jobs/{id}/renders/{variant}/agent/captions", h.GetCaptionAgent)
	r.Get("/api/jobs/{id}/renders/{variant}/pack", h.GetRenderPack)
	r.Get("/api/jobs/{id}/renders/{variant}/edit-document", h.GetRenderEditDocument)
	r.Get("/api/jobs/{id}/renders/{variant}/gallery", h.GetRenderGallery)
	r.Get("/api/jobs/{id}/renders/{variant}/videos/{name}", h.GetRenderVideo)
	r.Get("/api/jobs/{id}/renders/{variant}/covers/{name}", h.GetRenderCover)
	r.Get("/api/jobs/{id}/renders/{variant}/captions/{name}", h.GetRenderCaption)
	r.Post("/api/stream-jobs", h.CreateStreamJob)
	r.Get("/api/stream-jobs", h.ListStreamJobs)
	r.Get("/api/stream-jobs/{id}", h.GetStreamJob)
	r.Get("/api/stream-jobs/{id}/source", h.GetStreamSource)
	r.Get("/api/stream-jobs/{id}/edit-plan", h.GetStreamEditPlan)
	r.Put("/api/stream-jobs/{id}/edit-plan", h.PutStreamEditPlan)
	r.Post("/api/stream-jobs/{id}/renders/{variant}", h.StartStreamRender)
	r.Get("/api/stream-jobs/{id}/renders/{variant}", h.GetStreamRender)
	r.Get("/api/stream-jobs/{id}/renders/{variant}/gallery", h.GetStreamGallery)
	r.Get("/api/stream-jobs/{id}/renders/{variant}/videos/{clip_id}", h.GetStreamVideo)
	return r
}

func (h *Handlers) requireMutationToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.mutationToken == "" || !isMutationMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("X-FragForge-Token") != h.mutationToken {
			writeError(w, http.StatusUnauthorized, "mutation token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isMutationMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
