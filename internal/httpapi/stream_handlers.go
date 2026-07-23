package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
	"github.com/rechedev9/fragforge/internal/vodfetch"
)

const (
	maxStreamVideoBytes     = 8 << 30
	maxStreamMultipartBytes = maxStreamVideoBytes + 2<<20
	streamRenderUniqueTTL   = 24 * time.Hour
	defaultStreamListLimit  = 50
)

type createStreamJobConfig struct {
	Title string `json:"title,omitempty"`
}

// createStreamJobFromURLRequest is the JSON body for POST /api/stream-jobs
// when acquiring a source video by URL instead of uploading it directly.
type createStreamJobFromURLRequest struct {
	SourceURL string `json:"source_url"`
	Title     string `json:"title,omitempty"`
}

type streamVariantSummary struct {
	Name             string  `json:"name"`
	Label            string  `json:"label"`
	Description      string  `json:"description"`
	Default          bool    `json:"default"`
	FullFrame        bool    `json:"full_frame"`
	OutputWidth      int     `json:"output_width"`
	FaceOutputHeight int     `json:"face_output_height,omitempty"`
	GameOutputHeight int     `json:"game_output_height"`
	BannerPositionY  float64 `json:"default_banner_position_y"`
}

// ListStreamVariants exposes the stream layout registry so MCP and web clients
// discover the same live names and geometry the render handler validates.
func (h *Handlers) ListStreamVariants(w http.ResponseWriter, _ *http.Request) {
	defaultName := streamclips.DefaultVariant().Name
	variants := make([]streamVariantSummary, 0, len(streamclips.VariantNames()))
	for _, name := range streamclips.VariantNames() {
		variant, ok := streamclips.VariantByName(name)
		if !ok {
			continue
		}
		variants = append(variants, streamVariantSummary{
			Name:             variant.Name,
			Label:            variant.Label,
			Description:      variant.Description,
			Default:          variant.Name == defaultName,
			FullFrame:        variant.FullFrame,
			OutputWidth:      variant.OutputWidth,
			FaceOutputHeight: variant.FaceOutputHeight,
			GameOutputHeight: variant.GameOutputHeight,
			BannerPositionY:  variant.DefaultBannerPositionY,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"default":  defaultName,
		"variants": variants,
	})
}

func (h *Handlers) CreateStreamJob(w http.ResponseWriter, r *http.Request) {
	if !h.streamReady(w) {
		return
	}
	if isJSONContentType(r.Header.Get("Content-Type")) {
		h.createStreamJobFromURL(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxStreamMultipartBytes)
	// #nosec G120 -- r.Body is capped with MaxBytesReader immediately above.
	if err := r.ParseMultipartForm(multipartMemBudget); err != nil {
		writeError(w, http.StatusBadRequest, "parsing multipart form: "+err.Error())
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
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

// createStreamJobFromURL handles POST /api/stream-jobs with a JSON body
// {"source_url": ..., "title": ...}: it validates the URL, creates the job in
// "acquiring" status, and enqueues the download. The AcquireWorker fills in
// the source, probe, and sha256 and moves the job to "ready".
func (h *Handlers) createStreamJobFromURL(w http.ResponseWriter, r *http.Request) {
	if !h.requireYtdlpEnabled(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	var req createStreamJobFromURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "stream job JSON is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid stream job JSON")
		return
	}
	source, err := vodfetch.ValidateSource(req.SourceURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid source_url: "+err.Error())
		return
	}

	id := uuid.New()
	j := &streamclips.Job{
		ID:              id,
		Status:          streamclips.StatusAcquiring,
		SourcePath:      streamclips.SourceKey(id),
		SourceURL:       source.AcquisitionURL,
		PublicSourceURL: source.PublicURL,
		Title:           req.Title,
	}
	if err := h.streamRepo.Create(r.Context(), j); err != nil {
		internalError(w, "create stream job", err)
		return
	}

	task, err := tasks.NewStreamAcquireTask(j.ID)
	if err != nil {
		internalError(w, "build stream acquire task", err)
		return
	}
	if _, err := h.queue.EnqueueWithTransition(task, func(decision error) error {
		return h.persistStreamAcquireQueueDecision(j.ID, decision)
	}, asynq.MaxRetry(0)); err != nil {
		internalError(w, "enqueue stream acquire task", err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":     j.ID,
		"status": j.Status,
		"probe":  j.Probe,
	})
}

// persistStreamAcquireQueueDecision prevents URL-backed jobs from remaining
// "acquiring" after the process-local queue rejects or discards their task.
func (h *Handlers) persistStreamAcquireQueueDecision(id uuid.UUID, decision error) error {
	if decision == nil {
		return nil
	}
	markCtx, markCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer markCancel()
	if err := h.streamRepo.UpdateStatus(markCtx, id, streamclips.StatusFailed, "enqueue stream acquire task: "+decision.Error()); err != nil {
		return fmt.Errorf("mark stream job failed after acquire queue decision: %w", err)
	}
	return nil
}

// isJSONContentType reports whether the request's Content-Type is (or starts
// with) application/json, ignoring an optional charset parameter.
func isJSONContentType(contentType string) bool {
	return strings.HasPrefix(strings.TrimSpace(contentType), "application/json")
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
	// Stream uploads accept several containers, while the durable storage key
	// intentionally normalizes every source to source.mp4. Ask serveArtifact to
	// detect the stored bytes instead of claiming that every upload is MP4.
	h.streamStorageKey(w, r, "", j.SourcePath)
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

// currentStreamEditPlan returns the job's edit plan the same way
// GetStreamEditPlan does (the job row, else the storage artifact, else the
// default plan), so StartStreamRender can gate on plan.Captions without
// duplicating the fallback chain.
func (h *Handlers) currentStreamEditPlan(j streamclips.Job) (streamclips.EditPlan, error) {
	if len(j.EditPlan) > 0 {
		var plan streamclips.EditPlan
		if err := json.Unmarshal(j.EditPlan, &plan); err != nil {
			return streamclips.EditPlan{}, err
		}
		return plan, nil
	}
	rc, err := h.storage.Open(streamclips.EditPlanKey(j.ID))
	if err == nil {
		defer rc.Close()
		var plan streamclips.EditPlan
		if err := json.NewDecoder(rc).Decode(&plan); err != nil {
			return streamclips.EditPlan{}, err
		}
		return plan, nil
	}
	if !storageNotExist(err) {
		return streamclips.EditPlan{}, err
	}
	return streamclips.DefaultEditPlan(), nil
}

func (h *Handlers) PutStreamEditPlan(w http.ResponseWriter, r *http.Request) {
	// Serialize every edit-plan mutation with caption review, killfeed apply, and
	// render admission so none can validate plan A and overwrite a newer plan B.
	releaseJob := h.lockStreamJobRequest(r)
	defer releaseJob()
	h.streamPlanMu.Lock()
	defer h.streamPlanMu.Unlock()
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	if j.Status == streamclips.StatusRendering {
		writeError(w, http.StatusConflict, "stream edit plan cannot change while a render is running")
		return
	}
	previousPlan, err := h.currentStreamEditPlan(j)
	if err != nil {
		internalError(w, "load previous stream edit plan", err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	var plan streamclips.EditPlan
	if err := json.NewDecoder(r.Body).Decode(&plan); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "edit plan JSON is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid edit plan JSON")
		return
	}
	plan = streamclips.NormalizeEditPlan(plan)
	invalidateChangedCaptionReviews(previousPlan, &plan)
	reconcileKillfeedAnalysis(previousPlan, &plan)
	plan.UpdatedAt = time.Now().UTC()
	if err := plan.ValidateForSourceDuration(j.Probe.DurationSeconds); err != nil {
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
	expectedPlanUpdatedAt, hasExpectedPlanUpdatedAt, ok := decodeExpectedStreamPlanRevision(w, r)
	if !ok {
		return
	}
	releaseJob := h.lockStreamJobRequest(r)
	defer releaseJob()
	h.streamPlanMu.Lock()
	defer h.streamPlanMu.Unlock()

	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	layout, ok := streamclips.VariantByName(variant)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported stream render variant %q (valid variants: %s)", variant, strings.Join(streamclips.VariantNames(), ", ")))
		return
	}
	if j.Status != streamclips.StatusReady && j.Status != streamclips.StatusRendered {
		writeError(w, http.StatusConflict, fmt.Sprintf("stream job is not ready to render (status=%s)", j.Status))
		return
	}
	plan, err := h.currentStreamEditPlan(j)
	if err != nil {
		internalError(w, "load stream edit plan", err)
		return
	}
	plan = streamclips.NormalizeEditPlan(plan)
	if hasExpectedPlanUpdatedAt && !plan.UpdatedAt.Equal(expectedPlanUpdatedAt) {
		writeError(w, http.StatusConflict, "stream edit plan changed after approval; review the latest plan before rendering")
		return
	}
	hadClipsBeforeMigration := len(plan.Clips) > 0
	migrationApplied := false
	if migrated, changed := streamclips.MigrateLegacySourceDuration(plan, j.Probe.DurationSeconds); changed {
		plan = migrated
		migrationApplied = true
	}
	if migrationApplied && hasExpectedPlanUpdatedAt {
		writeError(w, http.StatusConflict, "stream edit plan requires migration after approval; save and review the migrated plan before rendering")
		return
	}
	if err := plan.ValidateForSourceDuration(j.Probe.DurationSeconds); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if plan.Variant != variant {
		writeError(w, http.StatusConflict, fmt.Sprintf(
			"stream render variant %q does not match edit plan variant %q; save the requested variant before rendering",
			variant, plan.Variant,
		))
		return
	}
	if migrationApplied && hadClipsBeforeMigration && len(plan.Clips) == 0 {
		writeError(w, http.StatusBadRequest, "stream edit plan has no clips after source-duration migration")
		return
	}
	if !layout.FullFrame && !plan.FaceCropReviewed {
		writeError(w, http.StatusConflict, "facecam crop requires explicit review before rendering")
		return
	}
	if j.Probe.AudioCodec != "" && plan.CaptionsNeedBackend() {
		writeError(w, http.StatusConflict, "captions require review before rendering; generate caption candidates and approve every audible clip")
		return
	}
	renderIntent := tasks.StreamRenderIntent{AttemptID: uuid.New()}
	if plan.KillfeedCrop != nil {
		analysis, current, err := h.currentAppliedStreamKillfeedAnalysis(j, plan)
		if err != nil {
			internalError(w, "validate applied stream killfeed analysis", err)
			return
		}
		if !current {
			writeError(w, http.StatusConflict, "clean killfeed requires the current temporal analysis generation to be applied before rendering")
			return
		}
		if err := validateRenderableKillfeedCues(plan, analysis); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		renderIntent.KillfeedGeneration = analysis.GenerationID
		renderIntent.KillfeedFingerprint = analysis.Fingerprint
	}
	renderIntent.EditPlanFingerprint, err = streamclips.EditPlanFingerprint(plan)
	if err != nil {
		internalError(w, "fingerprint stream edit plan", err)
		return
	}
	task, err := tasks.NewBoundRenderStreamClipTask(j.ID, variant, renderIntent)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	previousState, hadPreviousState, err := h.readStreamRenderState(j.ID, variant)
	if err != nil {
		internalError(w, "load previous stream render state", err)
		return
	}
	state, err := streamclips.NewRenderState(j.ID, variant, streamclips.StatusRendering, nil, "", nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state.AttemptID = renderIntent.AttemptID
	if hadPreviousState {
		state.PreservePublishedRender(previousState)
	}
	_, err = h.queue.EnqueueWithTransition(task, func(decision error) error {
		switch {
		case decision == nil:
			return h.writeStreamRenderState(state)
		case errors.Is(decision, asynq.ErrDuplicateTask):
			existing, ok, readErr := h.readStreamRenderState(j.ID, variant)
			if readErr != nil {
				return readErr
			}
			if ok {
				state = existing
			}
			return nil
		default:
			failedState := state
			failedState.Status = streamclips.StatusFailed
			failedState.Error = "enqueue render: " + decision.Error()
			failedState.UpdatedAt = time.Now().UTC()
			return h.writeStreamRenderState(failedState)
		}
	}, asynq.Unique(streamRenderUniqueTTL))
	if err != nil {
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

type startStreamRenderRequest struct {
	ExpectedEditPlanUpdatedAt string `json:"expected_edit_plan_updated_at"`
}

func decodeExpectedStreamPlanRevision(w http.ResponseWriter, r *http.Request) (time.Time, bool, bool) {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		return time.Time{}, false, true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input startStreamRenderRequest
	if err := decoder.Decode(&input); err != nil {
		if errors.Is(err, io.EOF) {
			return time.Time{}, false, true
		}
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "stream render request JSON is too large")
			return time.Time{}, false, false
		}
		writeError(w, http.StatusBadRequest, "invalid stream render request JSON")
		return time.Time{}, false, false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "stream render request must contain one JSON object")
		return time.Time{}, false, false
	}
	if input.ExpectedEditPlanUpdatedAt == "" {
		return time.Time{}, false, true
	}
	expected, err := time.Parse(time.RFC3339Nano, input.ExpectedEditPlanUpdatedAt)
	if err != nil {
		writeError(w, http.StatusBadRequest, "expected_edit_plan_updated_at must be an RFC3339 timestamp")
		return time.Time{}, false, false
	}
	return expected, true, true
}

func invalidateChangedCaptionReviews(previous streamclips.EditPlan, current *streamclips.EditPlan) {
	if current == nil {
		return
	}
	previous = streamclips.NormalizeEditPlan(previous)
	byID := make(map[string]streamclips.ClipRange, len(previous.Clips))
	for _, clip := range previous.Clips {
		byID[clip.ID] = clip
	}
	for i := range current.Clips {
		old, ok := byID[current.Clips[i].ID]
		if !ok {
			continue
		}
		oldFingerprint, oldErr := streamclips.CaptionClipFingerprint("", old)
		newFingerprint, newErr := streamclips.CaptionClipFingerprint("", current.Clips[i])
		if oldErr == nil && newErr == nil && oldFingerprint == newFingerprint {
			continue
		}
		current.Clips[i].CaptionWords = nil
		current.Clips[i].CaptionReviewed = false
	}
}

// reconcileKillfeedAnalysis treats analysis metadata as server-owned. A PUT
// may omit it (older web/CLI clients do), but cannot forge or alter it. Once an
// applied generation's crop or ordered clip bounds change, every event it
// supplied is cleared together so no stale cue survives under fresh metadata.
func reconcileKillfeedAnalysis(previous streamclips.EditPlan, current *streamclips.EditPlan) {
	if current == nil {
		return
	}
	previous = streamclips.NormalizeEditPlan(previous)
	current.KillfeedAnalysis = nil
	if previous.KillfeedAnalysis == nil {
		return
	}
	inputsMatch := previous.KillfeedCrop != nil && current.KillfeedCrop != nil
	if inputsMatch {
		oldFingerprint, oldErr := streamclips.KillfeedAnalysisFingerprint(
			"edit-plan-source", *previous.KillfeedCrop, previous.Clips,
		)
		newFingerprint, newErr := streamclips.KillfeedAnalysisFingerprint(
			"edit-plan-source", *current.KillfeedCrop, current.Clips,
		)
		inputsMatch = oldErr == nil && newErr == nil && oldFingerprint == newFingerprint
	}
	if inputsMatch {
		metadata := *previous.KillfeedAnalysis
		current.KillfeedAnalysis = &metadata
		return
	}
	for i := range current.Clips {
		current.Clips[i].KillfeedSeconds = nil
		current.Clips[i].KillfeedKills = nil
		current.Clips[i].KillfeedCueProvenance = nil
	}
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
	state, err := streamclips.NewRenderState(j.ID, variant, j.Status, nil, j.FailureReason, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *Handlers) GetStreamGallery(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	state, exists, err := h.readStreamRenderState(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if exists {
		if state.HasPublishedRender() && state.GalleryKey != "" {
			h.streamStorageKey(w, r, "text/html; charset=utf-8", state.GalleryKey)
			return
		}
		writeError(w, http.StatusNotFound, "stream render gallery is not ready")
		return
	}
	// Compatibility for render states produced before revision-scoped
	// publication made status.json the authoritative pointer.
	key, err := streamclips.RenderGalleryKey(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.streamStorageKey(w, r, "text/html; charset=utf-8", key)
}

func (h *Handlers) GetStreamVideo(w http.ResponseWriter, r *http.Request) {
	clipID := chi.URLParam(r, "clip_id")
	h.streamStreamRenderArtifact(w, r, "video/mp4", func(id uuid.UUID, variant string) (string, error) {
		return h.streamVideoKey(id, variant, clipID)
	})
}

func (h *Handlers) GetStreamDeliveryArtifact(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	name := chi.URLParam(r, "name")
	state, exists, err := h.readStreamRenderState(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !exists || !state.HasPublishedRender() {
		writeError(w, http.StatusNotFound, "stream delivery pack is not ready")
		return
	}
	for _, artifact := range state.Delivery {
		if artifact.Name != name {
			continue
		}
		contentType := "application/octet-stream"
		switch path.Ext(name) {
		case ".mp4":
			contentType = "video/mp4"
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".json":
			contentType = "application/json"
		case ".txt", ".ass":
			contentType = "text/plain; charset=utf-8"
		}
		h.streamStorageKey(w, r, contentType, artifact.Key)
		return
	}
	writeError(w, http.StatusNotFound, "stream delivery artifact not found")
}

// streamVideoKey resolves the storage key for a rendered clip. The render
// result is the source of truth because the published entry may not sit at
// the plain clip key (a captioned render publishes <clip_id>_captioned.mp4);
// recomputing the key from the clip id alone 404s those clips. Falls back to
// the conventional key when the result is missing or does not list the clip.
func (h *Handlers) streamVideoKey(id uuid.UUID, variant, clipID string) (string, error) {
	fallback, err := streamclips.RenderVideoKey(id, variant, clipID)
	if err != nil {
		return "", err
	}
	state, exists, err := h.readStreamRenderState(id, variant)
	if err != nil {
		return "", err
	}
	if exists {
		if !state.HasPublishedRender() {
			return "", fmt.Errorf("stream render video is not ready")
		}
		for _, video := range state.Videos {
			if video.ClipID == clipID && video.Key != "" {
				return video.Key, nil
			}
		}
		if len(state.Videos) > 0 {
			return "", fmt.Errorf("stream render clip %q not found", clipID)
		}
	}
	resultKey, err := streamclips.RenderResultKey(id, variant)
	if err != nil {
		return fallback, nil
	}
	rc, err := h.storage.Open(resultKey)
	if err != nil {
		return fallback, nil
	}
	defer rc.Close()
	var result streamclips.RenderResult
	if err := json.NewDecoder(rc).Decode(&result); err != nil {
		return fallback, nil
	}
	for _, video := range result.Clips {
		if video.ClipID == clipID && video.Key != "" {
			return video.Key, nil
		}
	}
	return fallback, nil
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

// lockStreamJobRequest coordinates the short HTTP validation+persistence
// critical section with the stream worker's render claim and final commit. A
// malformed id is handled by loadStreamJob and needs no keyed lock.
func (h *Handlers) lockStreamJobRequest(r *http.Request) func() {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return func() {}
	}
	return h.streamJobLocks.Lock(id)
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
	h.streamStorageKey(w, r, contentType, key)
}

func (h *Handlers) streamStorageKey(w http.ResponseWriter, r *http.Request, contentType, key string) {
	rc, err := h.storage.Open(key)
	if err != nil {
		writeError(w, http.StatusNotFound, "stream artifact not found")
		return
	}
	serveArtifact(w, r, contentType, rc)
}

func (h *Handlers) writeStreamEditPlanArtifact(id uuid.UUID, plan streamclips.EditPlan) error {
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return h.storage.Put(streamclips.EditPlanKey(id), bytes.NewReader(append(b, '\n')))
}

func (h *Handlers) writeStreamRenderState(state streamclips.RenderState) error {
	if err := streamclips.ValidateRenderStateArtifacts(state); err != nil {
		return fmt.Errorf("validate stream render state artifacts: %w", err)
	}
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

func (h *Handlers) readStreamRenderState(id uuid.UUID, variant string) (streamclips.RenderState, bool, error) {
	key, err := streamclips.RenderStateKey(id, variant)
	if err != nil {
		return streamclips.RenderState{}, false, err
	}
	rc, err := h.storage.Open(key)
	if err != nil {
		if storageNotExist(err) {
			return streamclips.RenderState{}, false, nil
		}
		return streamclips.RenderState{}, false, err
	}
	defer rc.Close()
	var state streamclips.RenderState
	if err := json.NewDecoder(rc).Decode(&state); err != nil {
		return streamclips.RenderState{}, false, fmt.Errorf("decode stream render state: %w", err)
	}
	if state.JobID != id || state.Variant != variant {
		return streamclips.RenderState{}, false, fmt.Errorf("stream render state identity does not match request")
	}
	if err := streamclips.ValidateRenderStateArtifacts(state); err != nil {
		return streamclips.RenderState{}, false, fmt.Errorf("validate stream render state: %w", err)
	}
	return state, true, nil
}

func storageNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
