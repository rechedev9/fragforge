// Package httpapi exposes the orchestrator's HTTP endpoints.
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
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/moments"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

const (
	maxDemoBytes       = 500 << 20            // 500 MiB demo cap
	maxMultipartBytes  = maxDemoBytes + 1<<20 // allow multipart headers around the demo
	multipartMemBudget = 32 << 20             // 32 MiB in-memory; spill beyond
	maxJSONBodyBytes   = 1 << 20              // JSON control documents are small
	renderUniqueTTL    = 24 * time.Hour
)

// JobRepository is the subset of *job.Repository used by handlers.
type JobRepository interface {
	Create(ctx context.Context, j *job.Job) error
	Get(ctx context.Context, id uuid.UUID) (job.Job, error)
	List(ctx context.Context, limit int) ([]job.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, s job.Status, failureReason string) error
}

type StreamJobRepository interface {
	Create(ctx context.Context, j *streamclips.Job) error
	Get(ctx context.Context, id uuid.UUID) (streamclips.Job, error)
	List(ctx context.Context, limit int) ([]streamclips.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, s streamclips.Status, failureReason string) error
	SetEditPlan(ctx context.Context, id uuid.UUID, plan streamclips.EditPlan) error
}

// Enqueuer is the subset of *asynq.Client used by handlers.
type Enqueuer interface {
	Enqueue(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}

// Handlers bundles the dependencies needed by every endpoint.
type Handlers struct {
	repo          JobRepository
	streamRepo    StreamJobRepository
	storage       storage.Storage
	queue         Enqueuer
	mutationToken string
	streamProber  streamclips.Prober
}

type Option func(*Handlers)

// WithMutationToken requires mutating requests to send X-FragForge-Token.
func WithMutationToken(token string) Option {
	return func(h *Handlers) {
		h.mutationToken = token
	}
}

func WithStreamRepository(repo StreamJobRepository) Option {
	return func(h *Handlers) {
		h.streamRepo = repo
	}
}

func WithStreamProber(prober streamclips.Prober) Option {
	return func(h *Handlers) {
		h.streamProber = prober
	}
}

// NewHandlers constructs an HTTP handler set.
func NewHandlers(repo JobRepository, store storage.Storage, queue Enqueuer, opts ...Option) *Handlers {
	h := &Handlers{repo: repo, storage: store, queue: queue}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// createJobConfig is the JSON document sent in the "config" multipart field.
type createJobConfig struct {
	TargetSteamID string       `json:"target_steamid"`
	Rules         *rules.Rules `json:"rules,omitempty"`
}

type uploadStatusRequest struct {
	Uploaded bool `json:"uploaded"`
}

type uploadStatusDocument struct {
	SchemaVersion string    `json:"schema_version"`
	JobID         uuid.UUID `json:"job_id"`
	Variant       string    `json:"variant"`
	Uploaded      bool      `json:"uploaded"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// CreateJob handles POST /api/jobs.
func (h *Handlers) CreateJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBytes)

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
	file, _, err := r.FormFile("demo")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing demo file: "+err.Error())
		return
	}
	defer file.Close()

	var cfg createJobConfig
	if raw := r.FormValue("config"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid config JSON: "+err.Error())
			return
		}
	}
	if cfg.TargetSteamID == "" {
		writeError(w, http.StatusBadRequest, "target_steamid is required")
		return
	}
	if _, err := strconv.ParseUint(cfg.TargetSteamID, 10, 64); err != nil {
		writeError(w, http.StatusBadRequest, "target_steamid must be a 64-bit unsigned integer")
		return
	}

	effectiveRules := rules.Default()
	if cfg.Rules != nil {
		effectiveRules = *cfg.Rules
		if err := effectiveRules.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid rules: "+err.Error())
			return
		}
	}

	j := &job.Job{
		ID:            uuid.New(),
		Status:        job.StatusQueued,
		TargetSteamID: cfg.TargetSteamID,
		Rules:         effectiveRules,
	}
	key := fmt.Sprintf("demos/%s.dem", j.ID)
	j.DemoPath = key

	// Stream upload to storage while hashing in one pass.
	h256 := sha256.New()
	tee := io.TeeReader(file, h256)
	if err := h.storage.Put(key, tee); err != nil {
		internalError(w, "store demo", err)
		return
	}
	j.DemoSHA256 = hex.EncodeToString(h256.Sum(nil))

	if err := h.repo.Create(r.Context(), j); err != nil {
		internalError(w, "create job", err)
		return
	}

	task, err := tasks.NewParseDemoTask(j.ID)
	if err != nil {
		internalError(w, "build parse task", err)
		return
	}
	if _, err := h.queue.Enqueue(task); err != nil {
		// The job row and demo blob are already persisted. Mark the job failed
		// so it is not stranded in "queued" with no task to advance it; the row
		// stays visible and auditable instead of silently orphaned. Use a fresh,
		// short-lived context so the compensating write lands even if the request
		// context is already cancelled (client disconnect or proxy deadline).
		markCtx, markCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if uerr := h.repo.UpdateStatus(markCtx, j.ID, job.StatusFailed, "enqueue parse task: "+err.Error()); uerr != nil {
			log.Printf("httpapi: mark job %s failed after enqueue error: %v", j.ID, uerr)
		}
		markCancel()
		internalError(w, "enqueue parse task", err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     j.ID,
		"status": j.Status,
	})
}

// ListJobs handles GET /api/jobs.
func (h *Handlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "limit must be an integer from 1 to 100")
			return
		}
		limit = parsed
	}
	jobs, err := h.repo.List(r.Context(), limit)
	if err != nil {
		internalError(w, "list jobs", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

// ListLoadouts handles GET /api/loadouts.
func (h *Handlers) ListLoadouts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"loadouts": renderplan.LoadoutCatalog()})
}

// presetSummary is the UI-facing view of one editor render preset.
type presetSummary struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	Default           bool   `json:"default"`
	FPS               int    `json:"fps"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
	EffectsPreset     string `json:"effects_preset,omitempty"`
	HQFilters         bool   `json:"hq_filters"`
	AudioNormalize    bool   `json:"audio_normalize"`
	QualityChecks     bool   `json:"quality_checks"`
	CoverSheets       bool   `json:"cover_sheets"`
	TemporalSmoothing bool   `json:"temporal_smoothing"`
}

// ListPresets handles GET /api/presets. It exposes the editor preset registry
// so UIs can derive their variant lists instead of hardcoding them.
func (h *Handlers) ListPresets(w http.ResponseWriter, r *http.Request) {
	defaultName := editor.DefaultPreset().Name
	names := editor.PresetNames()
	presets := make([]presetSummary, 0, len(names))
	for _, name := range names {
		preset, ok := editor.PresetByName(name)
		if !ok {
			continue
		}
		presets = append(presets, presetSummary{
			Name:              preset.Name,
			Description:       preset.Description,
			Default:           preset.Name == defaultName,
			FPS:               preset.FPS,
			Width:             preset.Width,
			Height:            preset.Height,
			EffectsPreset:     preset.EffectsPreset,
			HQFilters:         preset.HQFilters,
			AudioNormalize:    preset.AudioNormalize,
			QualityChecks:     preset.QualityChecks,
			CoverSheets:       preset.CoverSheets,
			TemporalSmoothing: preset.TemporalSmoothing,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"default": defaultName,
		"presets": presets,
	})
}

// GetJob handles GET /api/jobs/{id}.
func (h *Handlers) GetJob(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	j, err := h.repo.Get(r.Context(), id)
	if errors.Is(err, job.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		internalError(w, "get job", err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

// GetPlan handles GET /api/jobs/{id}/plan.
func (h *Handlers) GetPlan(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return
	}
	j, err := h.repo.Get(r.Context(), id)
	if errors.Is(err, job.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		internalError(w, "get plan", err)
		return
	}
	if j.KillPlan == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("job not ready (status=%s)", j.Status))
		return
	}
	writeJSON(w, http.StatusOK, j.KillPlan)
}

// GetMoments handles GET /api/jobs/{id}/moments.
func (h *Handlers) GetMoments(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	if rc, err := h.storage.Open(artifacts.MomentsKey(j.ID)); err == nil {
		defer rc.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, rc)
		return
	} else if !storage.IsNotExist(err) {
		internalError(w, "open moments artifact", err)
		return
	}
	if j.KillPlan == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("job moments are not ready (status=%s)", j.Status))
		return
	}
	writeJSON(w, http.StatusOK, moments.Build(j.ID, *j.KillPlan))
}

// GetFinal handles GET /api/jobs/{id}/final.
func (h *Handlers) GetFinal(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	if j.Status != job.StatusComposed && j.Status != job.StatusDone {
		writeError(w, http.StatusConflict, fmt.Sprintf("job final is not ready (status=%s)", j.Status))
		return
	}
	rc, err := h.storage.Open(artifacts.FinalMP4Key(j.ID))
	if err != nil {
		writeError(w, http.StatusNotFound, "final artifact not found")
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-final.mp4"`, j.ID))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

// StartRecording handles POST /api/jobs/{id}/record.
func (h *Handlers) StartRecording(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	if (j.Status != job.StatusParsed && j.Status != job.StatusRecorded) || j.KillPlan == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("job is not ready to record (status=%s)", j.Status))
		return
	}
	task, err := tasks.NewRecordDemoTask(j.ID)
	if err != nil {
		internalError(w, "build record task", err)
		return
	}
	// The job stays in its parsed/recorded state on enqueue failure so the
	// client can retry the POST once the queue recovers.
	if _, err := h.queue.Enqueue(task, asynq.MaxRetry(0)); err != nil {
		internalError(w, "enqueue record task", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":   j.ID,
		"task": tasks.TypeRecordDemo,
	})
}

// StartComposition handles POST /api/jobs/{id}/compose.
func (h *Handlers) StartComposition(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	if j.Status != job.StatusRecorded && j.Status != job.StatusComposed {
		writeError(w, http.StatusConflict, fmt.Sprintf("job is not ready to compose (status=%s)", j.Status))
		return
	}
	task, err := tasks.NewComposeFinalTask(j.ID)
	if err != nil {
		internalError(w, "build compose task", err)
		return
	}
	// The job stays in its recorded/composed state on enqueue failure so the
	// client can retry the POST once the queue recovers.
	if _, err := h.queue.Enqueue(task); err != nil {
		internalError(w, "enqueue compose task", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":   j.ID,
		"task": tasks.TypeComposeFinal,
	})
}

// StartRenderVariant handles POST /api/jobs/{id}/renders/{variant}.
func (h *Handlers) StartRenderVariant(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	if j.Status != job.StatusRecorded && j.Status != job.StatusComposed && j.Status != job.StatusDone {
		writeError(w, http.StatusConflict, fmt.Sprintf("job is not ready to render (status=%s)", j.Status))
		return
	}
	loadout, err := renderplan.LoadoutForVariant(variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	task, err := tasks.NewRenderVariantTask(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	previous, _, err := h.readRenderVariantState(j.ID, variant)
	if err != nil {
		internalError(w, "read render state", err)
		return
	}
	state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:    j.ID,
		Loadout:  loadout,
		Status:   renderplan.RenderVariantStatusQueued,
		Previous: previous,
	})
	if err != nil {
		internalError(w, "build render state", err)
		return
	}
	if err := h.writeRenderVariantState(state); err != nil {
		internalError(w, "write render state", err)
		return
	}
	if _, err := h.queue.Enqueue(task, asynq.Unique(renderUniqueTTL)); err != nil {
		if errors.Is(err, asynq.ErrDuplicateTask) {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"id":         j.ID,
				"task":       tasks.TypeRenderVariant,
				"variant":    variant,
				"status":     state.Status,
				"status_key": mustRenderVariantStatusKey(j.ID, variant),
				"duplicate":  true,
			})
			return
		}
		failedState, stateErr := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
			JobID:    j.ID,
			Loadout:  loadout,
			Status:   renderplan.RenderVariantStatusFailed,
			Error:    "enqueue render task: " + err.Error(),
			Previous: &state,
		})
		if stateErr == nil {
			if writeErr := h.writeRenderVariantState(failedState); writeErr != nil {
				log.Printf("httpapi: write failed render state for %s/%s: %v", j.ID, variant, writeErr)
			}
		}
		internalError(w, "enqueue render task", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":         j.ID,
		"task":       tasks.TypeRenderVariant,
		"variant":    variant,
		"status":     state.Status,
		"status_key": mustRenderVariantStatusKey(j.ID, variant),
	})
}

// GetRenderVariant handles GET /api/jobs/{id}/renders/{variant}.
func (h *Handlers) GetRenderVariant(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	if _, err := renderplan.LoadoutForVariant(variant); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if state, ok, err := h.readRenderVariantState(j.ID, variant); err != nil {
		internalError(w, "read render state", err)
		return
	} else if ok {
		writeJSON(w, http.StatusOK, state)
		return
	}
	resultKey, err := artifacts.RenderVariantResultKey(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rc, err := h.storage.Open(resultKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "render variant not found")
		return
	}
	defer rc.Close()

	var result editor.Result
	if err := json.NewDecoder(rc).Decode(&result); err != nil {
		internalError(w, "decode render result", err)
		return
	}
	status := "ready"
	if result.Error != "" {
		status = "failed"
	}
	loadout, err := renderplan.LoadoutForVariant(variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:    j.ID,
		Loadout:  loadout,
		Status:   status,
		Warnings: result.Warnings,
		Error:    result.Error,
	})
	if err != nil {
		internalError(w, "build render state", err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (h *Handlers) readRenderVariantState(id uuid.UUID, variant string) (*renderplan.RenderVariantState, bool, error) {
	key, err := artifacts.RenderVariantStatusKey(id, variant)
	if err != nil {
		return nil, false, err
	}
	rc, err := h.storage.Open(key)
	if err != nil {
		if !storage.IsNotExist(err) {
			return nil, false, err
		}
		return nil, false, nil
	}
	defer rc.Close()
	var state renderplan.RenderVariantState
	if err := json.NewDecoder(rc).Decode(&state); err != nil {
		return nil, false, err
	}
	return &state, true, nil
}

func (h *Handlers) writeRenderVariantState(state renderplan.RenderVariantState) error {
	key, err := artifacts.RenderVariantStatusKey(state.JobID, state.Variant)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return h.storage.Put(key, bytes.NewReader(b))
}

func mustRenderVariantStatusKey(id uuid.UUID, variant string) string {
	key, err := artifacts.RenderVariantStatusKey(id, variant)
	if err != nil {
		return ""
	}
	return key
}

// GetRenderPublishBoard handles GET /api/jobs/{id}/renders/{variant}/publish.
func (h *Handlers) GetRenderPublishBoard(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	result, resultKey, ok := h.loadRenderResult(w, j.ID, variant)
	if !ok {
		return
	}
	packKey, err := artifacts.RenderVariantPackManifestKey(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	galleryKey, err := artifacts.RenderVariantGalleryKey(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	summaryKey, err := artifacts.RenderVariantPublishSummaryKey(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items := make([]renderplan.PublishBoardItem, 0, len(result.Shorts))
	for _, short := range result.Shorts {
		if short.SegmentID == "" {
			continue
		}
		videoKey, err := artifacts.RenderVariantVideoKey(j.ID, variant, short.SegmentID)
		if err != nil {
			internalError(w, "build video artifact key", err)
			return
		}
		coverKey, err := artifacts.RenderVariantCoverKey(j.ID, variant, short.SegmentID)
		if err != nil {
			internalError(w, "build cover artifact key", err)
			return
		}
		captionKey, err := artifacts.RenderVariantCaptionKey(j.ID, variant, short.SegmentID)
		if err != nil {
			internalError(w, "build caption artifact key", err)
			return
		}
		videoReady, err := h.storage.Exists(videoKey)
		if err != nil {
			internalError(w, "check video artifact", err)
			return
		}
		coverReady, err := h.storage.Exists(coverKey)
		if err != nil {
			internalError(w, "check cover artifact", err)
			return
		}
		captionReady, err := h.storage.Exists(captionKey)
		if err != nil {
			internalError(w, "check caption artifact", err)
			return
		}
		items = append(items, renderplan.PublishBoardItem{
			SegmentID:    short.SegmentID,
			VideoKey:     videoKey,
			CoverKey:     coverKey,
			CaptionKey:   captionKey,
			VideoReady:   videoReady,
			CoverReady:   coverReady,
			CaptionReady: captionReady,
		})
	}
	writeJSON(w, http.StatusOK, renderplan.NewPublishBoard(renderplan.NewPublishBoardOptions{
		JobID:           j.ID,
		Variant:         variant,
		UploadReadyRoot: "shortslistosparasubir",
		RenderResultKey: resultKey,
		PackManifestKey: packKey,
		GalleryKey:      galleryKey,
		PublishSummary:  summaryKey,
		Uploaded:        h.renderVariantUploaded(j.ID, variant),
		Items:           items,
		Warnings:        result.Warnings,
		Error:           result.Error,
	}))
}

// SetRenderUploaded handles POST /api/jobs/{id}/renders/{variant}/publish/uploaded.
func (h *Handlers) SetRenderUploaded(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	if _, err := renderplan.LoadoutForVariant(variant); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	var req uploadStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "upload status JSON is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid upload status JSON")
		return
	}
	key, err := artifacts.RenderVariantUploadStatusKey(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	doc := uploadStatusDocument{
		SchemaVersion: "1.0",
		JobID:         j.ID,
		Variant:       variant,
		Uploaded:      req.Uploaded,
		UpdatedAt:     time.Now().UTC(),
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		internalError(w, "marshal upload status", err)
		return
	}
	if err := h.storage.Put(key, bytes.NewReader(b)); err != nil {
		internalError(w, "write upload status", err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

// StartCaptionAgent handles POST /api/jobs/{id}/renders/{variant}/agent/captions.
func (h *Handlers) StartCaptionAgent(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	if _, err := renderplan.LoadoutForVariant(variant); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	task, err := tasks.NewCodexAgentTask(j.ID, variant, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := h.queue.Enqueue(task, asynq.Unique(renderUniqueTTL)); err != nil {
		if !errors.Is(err, asynq.ErrDuplicateTask) {
			internalError(w, "enqueue codex agent task", err)
			return
		}
	}
	resultKey, err := artifacts.RenderVariantAgentResultKey(j.ID, variant, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		internalError(w, "build agent result key", err)
		return
	}
	contextKey, err := artifacts.RenderVariantAgentContextKey(j.ID, variant, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		internalError(w, "build agent context key", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":          j.ID,
		"task":        tasks.TypeCodexAgent,
		"variant":     variant,
		"kind":        renderplan.AgentKindCaptionCandidates,
		"context_key": contextKey,
		"result_key":  resultKey,
	})
}

// GetCaptionAgent handles GET /api/jobs/{id}/renders/{variant}/agent/captions.
func (h *Handlers) GetCaptionAgent(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	key, err := artifacts.RenderVariantAgentResultKey(j.ID, variant, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rc, err := h.storage.Open(key)
	if err != nil {
		if storage.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "agent result not found")
			return
		}
		internalError(w, "open agent result", err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (h *Handlers) renderVariantUploaded(id uuid.UUID, variant string) bool {
	key, err := artifacts.RenderVariantUploadStatusKey(id, variant)
	if err != nil {
		return false
	}
	rc, err := h.storage.Open(key)
	if err != nil {
		return false
	}
	defer rc.Close()
	var doc uploadStatusDocument
	if err := json.NewDecoder(rc).Decode(&doc); err != nil {
		return false
	}
	return doc.Uploaded
}

// GetRenderQuality handles GET /api/jobs/{id}/renders/{variant}/quality.
func (h *Handlers) GetRenderQuality(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	result, _, ok := h.loadRenderResult(w, j.ID, variant)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, renderplan.NewQualityReport(j.ID, variant, result))
}

// GetRenderPack streams the render variant publish-pack manifest.
func (h *Handlers) GetRenderPack(w http.ResponseWriter, r *http.Request) {
	h.streamRenderVariantArtifact(w, r, "application/json", func(id uuid.UUID, variant string) (string, error) {
		return artifacts.RenderVariantPackManifestKey(id, variant)
	})
}

// GetRenderEditDocument streams the stable edit intent document.
func (h *Handlers) GetRenderEditDocument(w http.ResponseWriter, r *http.Request) {
	h.streamRenderVariantArtifact(w, r, "application/json", func(id uuid.UUID, variant string) (string, error) {
		return artifacts.RenderVariantEditDocumentKey(id, variant)
	})
}

// GetRenderGallery streams the render variant publish gallery.
func (h *Handlers) GetRenderGallery(w http.ResponseWriter, r *http.Request) {
	h.streamRenderVariantArtifact(w, r, "text/html; charset=utf-8", func(id uuid.UUID, variant string) (string, error) {
		return artifacts.RenderVariantGalleryKey(id, variant)
	})
}

// GetRenderVideo streams one render variant MP4 artifact.
func (h *Handlers) GetRenderVideo(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	h.streamRenderVariantArtifact(w, r, "video/mp4", func(id uuid.UUID, variant string) (string, error) {
		return artifacts.RenderVariantVideoKey(id, variant, name)
	})
}

// GetRenderCover streams one render variant cover artifact.
func (h *Handlers) GetRenderCover(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	h.streamRenderVariantArtifact(w, r, "image/jpeg", func(id uuid.UUID, variant string) (string, error) {
		return artifacts.RenderVariantCoverKey(id, variant, name)
	})
}

// GetRenderCaption streams one render variant caption artifact.
func (h *Handlers) GetRenderCaption(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	h.streamRenderVariantArtifact(w, r, "text/plain; charset=utf-8", func(id uuid.UUID, variant string) (string, error) {
		return artifacts.RenderVariantCaptionKey(id, variant, name)
	})
}

func (h *Handlers) streamRenderVariantArtifact(w http.ResponseWriter, r *http.Request, contentType string, keyFn func(uuid.UUID, string) (string, error)) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	if _, err := renderplan.LoadoutForVariant(variant); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key, err := keyFn(j.ID, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rc, err := h.storage.Open(key)
	if err != nil {
		writeError(w, http.StatusNotFound, "render artifact not found")
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (h *Handlers) loadRenderResult(w http.ResponseWriter, id uuid.UUID, variant string) (editor.Result, string, bool) {
	resultKey, err := artifacts.RenderVariantResultKey(id, variant)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return editor.Result{}, "", false
	}
	rc, err := h.storage.Open(resultKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "render variant not found")
		return editor.Result{}, "", false
	}
	defer rc.Close()
	var result editor.Result
	if err := json.NewDecoder(rc).Decode(&result); err != nil {
		internalError(w, "decode render result", err)
		return editor.Result{}, "", false
	}
	return result, resultKey, true
}

func (h *Handlers) loadJob(w http.ResponseWriter, r *http.Request) (job.Job, bool) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid job id")
		return job.Job{}, false
	}
	j, err := h.repo.Get(r.Context(), id)
	if errors.Is(err, job.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return job.Job{}, false
	}
	if err != nil {
		internalError(w, "load job", err)
		return job.Job{}, false
	}
	return j, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// internalError logs the underlying error at the boundary and returns a generic
// 500 to the client so driver/SQL/storage internals are not exposed.
func internalError(w http.ResponseWriter, op string, err error) {
	log.Printf("httpapi: %s: %v", op, err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
