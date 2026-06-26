package httpapi

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Routes returns a chi router with all orchestrator routes wired.
func Routes(h *Handlers) chi.Router {
	r := chi.NewRouter()
	r.Use(h.rateLimiter.middleware)
	r.Use(crossSiteGuard)
	r.Use(h.requireMutationToken)
	r.Get("/healthz", h.Health)
	r.Get("/metrics", h.Metrics)
	r.Get("/", h.Workbench)
	r.Get("/ui/workspace", h.WorkbenchWorkspace)
	r.Get("/ui/jobs", h.WorkbenchJobs)
	r.Get("/ui/jobs/{id}", h.WorkbenchJob)
	r.Post("/ui/jobs", h.WorkbenchCreateJob)
	r.Post("/ui/jobs/{id}/parse", h.WorkbenchStartParse)
	r.Post("/ui/jobs/{id}/record", h.WorkbenchStartRecording)
	r.Post("/ui/jobs/{id}/render", h.WorkbenchStartRender)
	r.Post("/ui/jobs/{id}/agent/captions", h.WorkbenchStartCaptionAgent)
	r.Get("/api/loadouts", h.ListLoadouts)
	r.Get("/api/presets", h.ListPresets)
	r.Get("/api/songs", h.ListSongs)
	r.Get("/api/songs/{id}/audio", h.GetSongAudio)
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
		if h.mutationToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		if isMutationMethod(r.Method) {
			if !h.tokenMatches(r) {
				writeError(w, http.StatusUnauthorized, "mutation token required")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		// When the bind is exposed, reads of the API also require the token so an
		// untrusted network cannot enumerate jobs or stream artifacts. The
		// workbench shell (GET /) and other non-/api paths stay open so the
		// operator console still loads and can prompt for the token.
		if h.requireReadAuth && strings.HasPrefix(r.URL.Path, "/api/") && !h.tokenMatches(r) {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tokenMatches reports whether the request carries the configured mutation
// token, using a constant-time comparison to avoid leaking it via timing.
func (h *Handlers) tokenMatches(r *http.Request) bool {
	return subtle.ConstantTimeCompare([]byte(r.Header.Get("X-FragForge-Token")), []byte(h.mutationToken)) == 1
}

func isMutationMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
