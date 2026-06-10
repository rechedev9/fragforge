package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

const (
	maxStreamVideoBytes     = 8 << 30
	maxStreamMultipartBytes = maxStreamVideoBytes + 2<<20
	streamRenderUniqueTTL   = 24 * time.Hour
	defaultStreamListLimit  = 50
	streamerVerticalVariant = streamclips.VariantStreamerVerticalStack
)

type createStreamJobConfig struct {
	Title string `json:"title,omitempty"`
}

func (h *Handlers) CreateStreamJob(w http.ResponseWriter, r *http.Request) {
	if !h.streamReady(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxStreamMultipartBytes)
	// #nosec G120 -- r.Body is capped with MaxBytesReader immediately above.
	if err := r.ParseMultipartForm(multipartMemBudget); err != nil {
		writeError(w, http.StatusBadRequest, "parsing multipart form: "+err.Error())
		return
	}
	file, _, err := r.FormFile("video")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing video file: "+err.Error())
		return
	}
	defer file.Close()

	var cfg createStreamJobConfig
	if raw := r.FormValue("config"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid config JSON: "+err.Error())
			return
		}
	}

	tmp, err := os.CreateTemp("", "zv-stream-upload-*.mp4")
	if err != nil {
		internalError(w, "create temp stream upload", err)
		return
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	h256 := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h256), file); err != nil {
		internalError(w, "copy stream upload", err)
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		internalError(w, "rewind stream upload", err)
		return
	}

	id := uuid.New()
	probe := streamclips.SourceProbe{}
	if h.streamProber != nil {
		probe, err = h.streamProber.Probe(r.Context(), tmp.Name())
		if err != nil {
			writeError(w, http.StatusBadRequest, "probe video: "+err.Error())
			return
		}
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		internalError(w, "rewind stream upload for storage", err)
		return
	}
	sourceKey := streamclips.SourceKey(id)
	if err := h.storage.Put(sourceKey, tmp); err != nil {
		internalError(w, "store stream source", err)
		return
	}

	j := &streamclips.Job{
		ID:           id,
		Status:       streamclips.StatusUploaded,
		SourcePath:   sourceKey,
		SourceSHA256: hex.EncodeToString(h256.Sum(nil)),
		Title:        cfg.Title,
		Probe:        probe,
	}
	if err := h.streamRepo.Create(r.Context(), j); err != nil {
		internalError(w, "create stream job", err)
		return
	}
	plan := streamclips.DefaultEditPlan()
	if err := h.writeStreamEditPlanArtifact(j.ID, plan); err != nil {
		internalError(w, "write default stream edit plan", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     j.ID,
		"status": j.Status,
		"probe":  j.Probe,
	})
}

func (h *Handlers) ListStreamJobs(w http.ResponseWriter, r *http.Request) {
	if !h.streamReady(w) {
		return
	}
	limit := defaultStreamListLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "limit must be an integer from 1 to 100")
			return
		}
		limit = parsed
	}
	jobs, err := h.streamRepo.List(r.Context(), limit)
	if err != nil {
		internalError(w, "list stream jobs", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (h *Handlers) GetStreamJob(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (h *Handlers) GetStreamSource(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	h.streamStorageKey(w, "video/mp4", j.SourcePath)
}

func (h *Handlers) GetStreamEditPlan(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	if len(j.EditPlan) > 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(j.EditPlan)
		return
	}
	key := streamclips.EditPlanKey(j.ID)
	rc, err := h.storage.Open(key)
	if err == nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, rc)
		return
	}
	if !storageNotExist(err) {
		internalError(w, "open stream edit plan", err)
		return
	}
	writeJSON(w, http.StatusOK, streamclips.DefaultEditPlan())
}

func (h *Handlers) PutStreamEditPlan(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	var plan streamclips.EditPlan
	if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
		writeError(w, http.StatusBadRequest, "invalid edit plan JSON")
		return
	}
	plan = streamclips.NormalizeEditPlan(plan)
	plan.UpdatedAt = time.Now().UTC()
	if err := plan.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.streamRepo.SetEditPlan(r.Context(), j.ID, plan); err != nil {
		if errors.Is(err, streamclips.ErrNotFound) {
			writeError(w, http.StatusNotFound, "stream job not found")
			return
		}
		internalError(w, "save stream edit plan", err)
		return
	}
	if err := h.writeStreamEditPlanArtifact(j.ID, plan); err != nil {
		internalError(w, "write stream edit plan artifact", err)
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (h *Handlers) StartStreamRender(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	if variant != streamerVerticalVariant {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported stream render variant %q", variant))
		return
	}
	if j.Status != streamclips.StatusReady && j.Status != streamclips.StatusRendered {
		writeError(w, http.StatusConflict, fmt.Sprintf("stream job is not ready to render (status=%s)", j.Status))
		return
	}
	task, err := tasks.NewRenderStreamClipTask(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state := h.newStreamRenderState(j.ID, variant, streamclips.StatusRendering, nil, "", nil)
	if err := h.writeStreamRenderState(state); err != nil {
		internalError(w, "write stream render state", err)
		return
	}
	if _, err := h.queue.Enqueue(task, asynq.Unique(streamRenderUniqueTTL)); err != nil {
		if errors.Is(err, asynq.ErrDuplicateTask) {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"id":        j.ID,
				"variant":   variant,
				"status":    state.Status,
				"duplicate": true,
			})
			return
		}
		internalError(w, "enqueue stream render", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":      j.ID,
		"task":    tasks.TypeRenderStreamClip,
		"variant": variant,
		"status":  state.Status,
	})
}

func (h *Handlers) GetStreamRender(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	key, err := streamclips.RenderStateKey(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rc, err := h.storage.Open(key)
	if err == nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, rc)
		return
	}
	if !storageNotExist(err) {
		internalError(w, "open stream render state", err)
		return
	}
	state := h.newStreamRenderState(j.ID, variant, j.Status, nil, j.FailureReason, nil)
	writeJSON(w, http.StatusOK, state)
}

func (h *Handlers) GetStreamGallery(w http.ResponseWriter, r *http.Request) {
	h.streamStreamRenderArtifact(w, r, "text/html; charset=utf-8", streamclips.RenderGalleryKey)
}

func (h *Handlers) GetStreamVideo(w http.ResponseWriter, r *http.Request) {
	clipID := chi.URLParam(r, "clip_id")
	h.streamStreamRenderArtifact(w, r, "video/mp4", func(id uuid.UUID, variant string) (string, error) {
		return streamclips.RenderVideoKey(id, variant, clipID)
	})
}

func (h *Handlers) streamReady(w http.ResponseWriter) bool {
	if h.streamRepo == nil {
		writeError(w, http.StatusNotImplemented, "stream jobs are not configured")
		return false
	}
	return true
}

func (h *Handlers) loadStreamJob(w http.ResponseWriter, r *http.Request) (streamclips.Job, bool) {
	if !h.streamReady(w) {
		return streamclips.Job{}, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid stream job id")
		return streamclips.Job{}, false
	}
	j, err := h.streamRepo.Get(r.Context(), id)
	if errors.Is(err, streamclips.ErrNotFound) {
		writeError(w, http.StatusNotFound, "stream job not found")
		return streamclips.Job{}, false
	}
	if err != nil {
		internalError(w, "load stream job", err)
		return streamclips.Job{}, false
	}
	return j, true
}

func (h *Handlers) streamStreamRenderArtifact(w http.ResponseWriter, r *http.Request, contentType string, keyFn func(uuid.UUID, string) (string, error)) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	key, err := keyFn(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.streamStorageKey(w, contentType, key)
}

func (h *Handlers) streamStorageKey(w http.ResponseWriter, contentType, key string) {
	rc, err := h.storage.Open(key)
	if err != nil {
		writeError(w, http.StatusNotFound, "stream artifact not found")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (h *Handlers) writeStreamEditPlanArtifact(id uuid.UUID, plan streamclips.EditPlan) error {
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return h.storage.Put(streamclips.EditPlanKey(id), bytes.NewReader(append(b, '\n')))
}

func (h *Handlers) newStreamRenderState(id uuid.UUID, variant string, status streamclips.Status, warnings []string, errMsg string, videos []streamclips.VideoEntry) streamclips.RenderState {
	resultKey, _ := streamclips.RenderResultKey(id, variant)
	galleryKey, _ := streamclips.RenderGalleryKey(id, variant)
	prefix, _ := streamclips.RenderPrefix(id, variant)
	return streamclips.RenderState{
		JobID:       id,
		Variant:     variant,
		Status:      status,
		ResultKey:   resultKey,
		GalleryKey:  galleryKey,
		ArtifactDir: prefix,
		Warnings:    warnings,
		Error:       errMsg,
		Videos:      videos,
		UpdatedAt:   time.Now().UTC(),
	}
}

func (h *Handlers) writeStreamRenderState(state streamclips.RenderState) error {
	key, err := streamclips.RenderStateKey(state.JobID, state.Variant)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return h.storage.Put(key, bytes.NewReader(append(b, '\n')))
}

func storageNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
