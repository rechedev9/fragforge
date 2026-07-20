// Package httpapi exposes the orchestrator's HTTP endpoints.
package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/composition"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/generateintent"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/moments"
	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
	"github.com/rechedev9/fragforge/internal/voiceprofile"
)

const (
	maxDemoBytes       = 500 << 20            // 500 MiB demo cap
	maxMultipartBytes  = maxDemoBytes + 1<<20 // allow multipart headers around the demo
	multipartMemBudget = 32 << 20             // 32 MiB in-memory; spill beyond
	maxJSONBodyBytes   = 1 << 20              // JSON control documents are small
	renderUniqueTTL    = 24 * time.Hour
)

var errGenerateRenderActive = errors.New("a render is already active for this job")

// JobRepository is the subset of *job.Repository used by handlers.
type JobRepository interface {
	Create(ctx context.Context, j *job.Job) error
	Get(ctx context.Context, id uuid.UUID) (job.Job, error)
	// GetStatus returns segmentCount only while the job is recording.
	GetStatus(ctx context.Context, id uuid.UUID) (status job.Status, failureReason string, segmentCount int, err error)
	List(ctx context.Context, limit int) ([]job.Job, error)
	ListBySeries(ctx context.Context, seriesID string) ([]job.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, s job.Status, failureReason string) error
	SetParseInputs(ctx context.Context, id uuid.UUID, steamID string, r rules.Rules) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type StreamJobRepository interface {
	Create(ctx context.Context, j *streamclips.Job) error
	Get(ctx context.Context, id uuid.UUID) (streamclips.Job, error)
	List(ctx context.Context, limit int) ([]streamclips.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, s streamclips.Status, failureReason string) error
	SetEditPlan(ctx context.Context, id uuid.UUID, plan streamclips.EditPlan) error
	SetAcquired(ctx context.Context, id uuid.UUID, probe streamclips.SourceProbe, sha256, discoveredTitle string) error
}

// Enqueuer is the desktop queue contract used by handlers. A transition runs
// inside the queue's admission boundary before accepted work becomes visible;
// accepted pending work receives a later non-nil transition if shutdown
// discards it before a handler takes ownership.
type Enqueuer interface {
	Enqueue(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
	EnqueueWithTransition(*asynq.Task, func(error) error, ...asynq.Option) (*asynq.TaskInfo, error)
}

// Handlers bundles the dependencies needed by every endpoint.
type Handlers struct {
	repo             JobRepository
	streamRepo       StreamJobRepository
	streamPlanMu     sync.Mutex
	streamJobLocks   *streamclips.JobLocks
	storage          storage.Storage
	generateIntents  *generateintent.Store
	voiceProfiles    *voiceprofile.Store
	queue            Enqueuer
	mutationToken    string
	discoverySecret  string
	requireReadAuth  bool
	rateLimiter      *rateLimiter
	streamProber     streamclips.Prober
	musicDir         string
	capabilities     Capabilities
	youtubeTrends    YouTubeTrends
	publishAssistant *publishAssistantCache
	// ffmpegPath and xaiAPIKey back the killfeed-read endpoint: ffmpeg extracts
	// the cue frame and the xAI vision client reads it. killfeedVisionBaseURL
	// overrides the xAI base URL (tests point it at an httptest fake).
	ffmpegPath            string
	xaiAPIKey             string
	killfeedVisionBaseURL string
	// killfeedFrame extracts a single source frame at atSeconds. It defaults to
	// an ffmpeg-backed extractor; killfeedTimeline extracts a short, low-rate
	// frame sequence in one ffmpeg process; killfeedNoticeRows detects highlighted
	// rows. Tests replace these seams so they never shell out.
	killfeedFrame      func(ctx context.Context, sourceKey string, atSeconds float64) (image.Image, error)
	killfeedTimeline   func(ctx context.Context, sourceKey string, startSeconds, endSeconds float64, crop *streamclips.CropRect) ([]timedKillfeedRows, error)
	killfeedNoticeRows func(image.Image, *streamclips.CropRect) []streamclips.NoticeRow
}

type Option func(*Handlers)

// WithMutationToken requires mutating requests to send X-FragForge-Token.
func WithMutationToken(token string) Option {
	return func(h *Handlers) {
		h.mutationToken = token
	}
}

// WithDiscoverySecret authenticates loopback service discovery without
// reusing the mutation credential. Desktop supplies a fresh value per boot.
func WithDiscoverySecret(secret string) Option {
	return func(h *Handlers) {
		h.discoverySecret = secret
	}
}

// WithRequireReadAuth also gates non-mutation /api reads behind the mutation
// token. It is meant for exposed (non-loopback) binds and has no effect unless a
// mutation token is configured. Loopback deployments leave this off.
func WithRequireReadAuth(require bool) Option {
	return func(h *Handlers) {
		h.requireReadAuth = require
	}
}

// WithRateLimit throttles requests per client IP. When rps <= 0 the limiter is a
// no-op pass-through, which keeps loopback deployments unthrottled.
func WithRateLimit(rps float64, burst int) Option {
	return func(h *Handlers) {
		h.rateLimiter = newRateLimiter(rps, burst)
	}
}

func WithStreamRepository(repo StreamJobRepository) Option {
	return func(h *Handlers) {
		h.streamRepo = repo
	}
}

// WithStreamJobLocks shares per-job render admission/finalization locks with
// the stream worker. The local orchestrator must pass the same instance to
// both owners; tests and handler-only deployments receive a private instance.
func WithStreamJobLocks(locks *streamclips.JobLocks) Option {
	return func(h *Handlers) {
		if locks != nil {
			h.streamJobLocks = locks
		}
	}
}

func WithStreamProber(prober streamclips.Prober) Option {
	return func(h *Handlers) {
		h.streamProber = prober
	}
}

// WithGenerateIntentStore shares guided-generate synchronization with the
// record worker. Desktop startup supplies one store to both owners so an old
// completion cannot race with accepting a newer run.
func WithGenerateIntentStore(store *generateintent.Store) Option {
	return func(h *Handlers) {
		h.generateIntents = store
	}
}

// WithCapabilities records which media workers are enabled and the tool paths
// they use, so GET /api/capabilities can report readiness and the record/
// generate handlers can reject a capture attempt with a clear 409 instead of
// enqueuing a task no worker will ever consume.
func WithCapabilities(c Capabilities) Option {
	return func(h *Handlers) {
		h.capabilities = c
	}
}

// WithPublishAssistantTrends enables optional Firecrawl discovery. Missing
// public trend data never makes the manual publishing assistant unavailable.
func WithPublishAssistantTrends(trends YouTubeTrends) Option {
	return func(h *Handlers) {
		h.youtubeTrends = trends
	}
}

// NewHandlers constructs an HTTP handler set.
func NewHandlers(repo JobRepository, store storage.Storage, queue Enqueuer, opts ...Option) *Handlers {
	h := &Handlers{
		repo:             repo,
		storage:          store,
		generateIntents:  generateintent.New(store),
		voiceProfiles:    voiceprofile.New(store),
		queue:            queue,
		publishAssistant: newPublishAssistantCache(),
		streamJobLocks:   streamclips.NewJobLocks(),
	}
	for _, opt := range opts {
		opt(h)
	}
	if h.killfeedFrame == nil {
		h.killfeedFrame = h.extractKillfeedFrame
	}
	if h.killfeedTimeline == nil {
		h.killfeedTimeline = h.extractKillfeedTimeline
	}
	if h.killfeedNoticeRows == nil {
		h.killfeedNoticeRows = streamclips.DetectNoticeRows
	}
	return h
}

// createJobConfig is the JSON document sent in the "config" multipart field.
type createJobConfig struct {
	TargetSteamID string       `json:"target_steamid"`
	Rules         *rules.Rules `json:"rules,omitempty"`
}

// maxDemoFileNameRunes caps the stored original demo file name so a hostile or
// accidental upload cannot bloat the persisted job document.
const maxDemoFileNameRunes = 128

// sanitizeDemoFileName reduces an uploaded multipart file name to a safe display
// name: it strips any directory prefix using either separator (a client may send
// a Windows path or a URL-style one, so filepath.Base alone is not enough), drops
// control characters and invisible format characters (Cf: RTL overrides,
// zero-width runes, BOM) that could spoof the displayed name, and caps the
// result at maxDemoFileNameRunes runes. It returns "" when nothing usable
// remains, so the caller leaves the field empty.
func sanitizeDemoFileName(name string) string {
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			continue
		}
		b.WriteRune(r)
	}
	cleaned := strings.TrimSpace(b.String())
	if runes := []rune(cleaned); len(runes) > maxDemoFileNameRunes {
		cleaned = strings.TrimSpace(string(runes[:maxDemoFileNameRunes]))
	}
	return cleaned
}

// isDemoHeader reports whether the leading bytes look like a CS2 (Source 2) or
// legacy GOTV (Source 1) demo.
func isDemoHeader(header []byte) bool {
	return bytes.HasPrefix(header, []byte("PBDEMS2")) || bytes.HasPrefix(header, []byte("HL2DEMO"))
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
	file, fileHeader, err := r.FormFile("demo")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing demo file: "+err.Error())
		return
	}
	defer file.Close()
	var demoFileName string
	if fileHeader != nil {
		demoFileName = sanitizeDemoFileName(fileHeader.Filename)
	}

	// Peek the demo magic bytes before persisting so non-demo uploads are
	// rejected at the door. io.ReadFull tolerates a short read via ErrUnexpectedEOF.
	var header [8]byte
	n, err := io.ReadFull(file, header[:])
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		internalError(w, "read demo header", err)
		return
	}
	if !isDemoHeader(header[:n]) {
		writeError(w, http.StatusBadRequest, "uploaded file is not a CS2 demo")
		return
	}
	// Stitch the peeked bytes back ahead of the remaining stream so the upload is
	// neither truncated nor read twice.
	demo := io.MultiReader(bytes.NewReader(header[:n]), file)

	var cfg createJobConfig
	if raw := r.FormValue("config"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid config JSON: "+err.Error())
			return
		}
	}
	// target_steamid is optional: when present the job parses straight away;
	// when absent it runs a roster scan first so the user can pick a target.
	if cfg.TargetSteamID != "" {
		if _, err := strconv.ParseUint(cfg.TargetSteamID, 10, 64); err != nil {
			writeError(w, http.StatusBadRequest, "target_steamid must be a 64-bit unsigned integer")
			return
		}
	}

	effectiveRules := rules.Default()
	if cfg.Rules != nil {
		effectiveRules = *cfg.Rules
		if err := effectiveRules.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid rules: "+err.Error())
			return
		}
	}

	// series_id is an optional client-minted UUID that groups the demos of one
	// bo3/bo5 series. When present it must be a valid UUID; it is stored in the
	// canonical lowercase form so ListBySeries matches regardless of casing.
	var seriesID string
	if raw := strings.TrimSpace(r.FormValue("series_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "series_id must be a valid UUID")
			return
		}
		seriesID = parsed.String()
	}

	j := &job.Job{
		ID:            uuid.New(),
		Status:        job.StatusQueued,
		SeriesID:      seriesID,
		DemoFileName:  demoFileName,
		TargetSteamID: cfg.TargetSteamID,
		Rules:         effectiveRules,
	}
	key := fmt.Sprintf("demos/%s.dem", j.ID)
	j.DemoPath = key

	// Stream upload to storage while hashing in one pass.
	h256 := sha256.New()
	tee := io.TeeReader(demo, h256)
	if err := h.storage.Put(key, tee); err != nil {
		internalError(w, "store demo", err)
		return
	}
	j.DemoSHA256 = hex.EncodeToString(h256.Sum(nil))

	if err := h.repo.Create(r.Context(), j); err != nil {
		internalError(w, "create job", err)
		return
	}

	// With a target the job parses immediately; without one it scans the roster
	// so the user can pick a target before the full parse.
	taskKind := "parse"
	build := tasks.NewParseDemoTask
	if j.TargetSteamID == "" {
		taskKind = "scan"
		build = tasks.NewScanRosterTask
	}
	task, err := build(j.ID)
	if err != nil {
		internalError(w, "build "+taskKind+" task", err)
		return
	}
	if _, err := h.queue.EnqueueWithTransition(task, func(decision error) error {
		return h.persistJobQueueDecision(j.ID, taskKind, decision)
	}); err != nil {
		internalError(w, "enqueue "+taskKind+" task", err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     j.ID,
		"status": j.Status,
	})
}

// ListJobs handles GET /api/jobs. With ?series_id=<uuid> it returns only that
// series' jobs ordered by creation time ascending (id as a deterministic
// tie-break); otherwise it returns the recent jobs (?limit).
func (h *Handlers) ListJobs(w http.ResponseWriter, r *http.Request) {
	if raw := r.URL.Query().Get("series_id"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "series_id must be a valid UUID")
			return
		}
		jobs, err := h.repo.ListBySeries(r.Context(), parsed.String())
		if err != nil {
			internalError(w, "list jobs by series", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
		return
	}
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
	Label             string `json:"label,omitempty"`
	Description       string `json:"description"`
	Default           bool   `json:"default"`
	HUDMode           string `json:"hud_mode,omitempty"`
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
			Label:             preset.Label,
			Description:       preset.Description,
			Default:           preset.Name == defaultName,
			HUDMode:           preset.HUDMode,
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
	if r.URL.Query().Get("view") == "status" {
		h.writeJobStatus(w, r, id)
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
	writeJSON(w, http.StatusOK, h.jobResponse(j))
}

// writeJobStatus serves the lightweight ?view=status representation. The
// default GetJob response remains the complete job for existing API/MCP users.
func (h *Handlers) writeJobStatus(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	status, failureReason, segmentCount, err := h.repo.GetStatus(r.Context(), id)
	if errors.Is(err, job.ErrNotFound) {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	if err != nil {
		internalError(w, "get job status", err)
		return
	}
	resp := jobStatusResponse{Status: status, FailureReason: failureReason}
	if progress, ok := captureProgressWithTotal(h.storage, id, status, segmentCount); ok {
		resp.Progress = &progress
	}
	writeJSON(w, http.StatusOK, resp)
}

type jobStatusResponse struct {
	Status        job.Status           `json:"status"`
	FailureReason string               `json:"failure_reason,omitempty"`
	Progress      *captureProgressView `json:"progress,omitempty"`
}

// jobResponse is the GET /api/jobs/{id} body: the job plus optional capture
// progress. Progress is present only while the job is capturing and at least one
// segment clip exists (see captureProgress); otherwise the field is omitted and
// the response is byte-for-byte the raw job as before.
type jobResponse struct {
	job.Job
	Progress *captureProgressView `json:"progress,omitempty"`
}

func (h *Handlers) jobResponse(j job.Job) jobResponse {
	resp := jobResponse{Job: j}
	if progress, ok := captureProgress(h.storage, j); ok {
		resp.Progress = &progress
	}
	return resp
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
	if rc, err := h.storage.Open(moments.ArtifactKey(j.ID)); err == nil {
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

// GetRoster handles GET /api/jobs/{id}/roster. It streams the roster scan
// result stored by the scan worker, already shaped as { "players": [...] }.
func (h *Handlers) GetRoster(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	rc, err := h.storage.Open(artifacts.RosterKey(j.ID))
	if err != nil {
		if storage.IsNotExist(err) {
			// Either the scan is still running or this job was created with a
			// target and never scanned.
			writeError(w, http.StatusConflict, "roster not ready")
			return
		}
		internalError(w, "open roster artifact", err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

// startParseRequest is the JSON body for POST /api/jobs/{id}/parse.
type startParseRequest struct {
	TargetSteamID string       `json:"target_steamid"`
	Rules         *rules.Rules `json:"rules,omitempty"`
}

// StartParse handles POST /api/jobs/{id}/parse. After a roster scan it records
// the picked target (and optional rules) and enqueues the full parse.
func (h *Handlers) StartParse(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	// Friendly early-out with the current status. The race-safe guard is the
	// status-gated SetParseInputs below, which atomically claims the job, so a
	// second concurrent request that slips past this check still conflicts there.
	if j.Status != job.StatusScanned && j.Status != job.StatusParsed {
		writeError(w, http.StatusConflict, fmt.Sprintf("job is not ready to parse (status=%s)", j.Status))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	var req startParseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writeError(w, http.StatusRequestEntityTooLarge, "parse request JSON is too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid parse request JSON")
		return
	}
	if _, err := strconv.ParseUint(req.TargetSteamID, 10, 64); err != nil {
		writeError(w, http.StatusBadRequest, "target_steamid must be a 64-bit unsigned integer")
		return
	}

	effectiveRules := j.Rules
	if req.Rules != nil {
		effectiveRules = *req.Rules
		if err := effectiveRules.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, "invalid rules: "+err.Error())
			return
		}
	}

	if err := h.repo.SetParseInputs(r.Context(), j.ID, req.TargetSteamID, effectiveRules); err != nil {
		switch {
		case errors.Is(err, job.ErrNotFound):
			writeError(w, http.StatusNotFound, "job not found")
		case errors.Is(err, job.ErrConflict):
			writeError(w, http.StatusConflict, "job is no longer ready to parse")
		default:
			internalError(w, "set parse inputs", err)
		}
		return
	}

	task, err := tasks.NewParseDemoTask(j.ID)
	if err != nil {
		internalError(w, "build parse task", err)
		return
	}
	if _, err := h.queue.EnqueueWithTransition(task, func(decision error) error {
		return h.persistJobQueueDecision(j.ID, "parse", decision)
	}); err != nil {
		internalError(w, "enqueue parse task", err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":     j.ID,
		"status": job.StatusParsing,
	})
}

// persistJobQueueDecision keeps a persisted active-looking job state aligned
// with ownership by the process-local queue. The queue calls it once during
// admission and again if accepted pending work is discarded during shutdown.
func (h *Handlers) persistJobQueueDecision(id uuid.UUID, taskKind string, decision error) error {
	if decision == nil {
		return nil
	}
	markCtx, markCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer markCancel()
	if err := h.repo.UpdateStatus(markCtx, id, job.StatusFailed, "enqueue "+taskKind+" task: "+decision.Error()); err != nil {
		return fmt.Errorf("mark job failed after %s queue decision: %w", taskKind, err)
	}
	return nil
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
	rc, err := h.storage.Open(composition.FinalArtifactKey(j.ID))
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

// validateSegmentSelection rejects any requested segment id that is not in the
// job's kill plan, writing a 400 and returning false. An empty selection means
// "record every segment" and always passes. Callers guarantee a non-nil kill
// plan via their readiness check before calling this.
func validateSegmentSelection(w http.ResponseWriter, j job.Job, ids []string) bool {
	if len(ids) == 0 {
		return true
	}
	valid := make(map[string]bool, len(j.KillPlan.Segments))
	for _, s := range j.KillPlan.Segments {
		valid[s.ID] = true
	}
	for _, id := range ids {
		if !valid[id] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown segment id %q", id))
			return false
		}
	}
	return true
}

// StartRecording handles POST /api/jobs/{id}/record.
func (h *Handlers) StartRecording(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	// Parsed/Recorded are the normal entry points. Failed is allowed too so a
	// failed capture can be retried in place (the .dem and kill plan are still
	// there); the KillPlan==nil guard still rejects a job that failed before it
	// was ever parsed.
	if (j.Status != job.StatusParsed && j.Status != job.StatusRecorded && j.Status != job.StatusFailed) || j.KillPlan == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("job is not ready to record (status=%s)", j.Status))
		return
	}
	if !h.requireRecordEnabled(w) {
		return
	}
	// Optional JSON body { "preset": "<name>" } selects the recording HUD from
	// the shared preset registry (so a "Clean POV" reel records HUD-less). An
	// empty or absent body keeps the recorder's default HUD.
	var hudMode string
	var segmentIDs []string
	var portraitSafeKillfeed bool
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
		var req struct {
			Preset     string                 `json:"preset"`
			SegmentIDs []string               `json:"segment_ids"`
			Edit       renderplan.EditRequest `json:"edit"`
		}
		switch err := json.NewDecoder(r.Body).Decode(&req); {
		case err == nil, errors.Is(err, io.EOF):
			if req.Preset != "" {
				preset, ok := editor.PresetByName(req.Preset)
				if !ok {
					writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown preset %q", req.Preset))
					return
				}
				hudMode = preset.HUDMode
				edit := renderplan.NormalizeEditRequest(req.Edit)
				if err := edit.Validate(); err != nil {
					writeError(w, http.StatusBadRequest, err.Error())
					return
				}
				portraitSafeKillfeed = preset.HUDMode == string(recording.HUDModeDeathnotices) && edit.Format == renderplan.FormatShort9x16
			}
			if !validateSegmentSelection(w, j, req.SegmentIDs) {
				return
			}
			segmentIDs = req.SegmentIDs
		default:
			writeError(w, http.StatusBadRequest, "invalid record request JSON")
			return
		}
	}
	task, err := tasks.NewRecordDemoTask(j.ID, hudMode, segmentIDs, portraitSafeKillfeed)
	if err != nil {
		internalError(w, "build record task", err)
		return
	}
	// The job stays in its parsed/recorded state on enqueue failure so the
	// client can retry the POST once the queue recovers.
	if _, err := h.queue.Enqueue(task, asynq.MaxRetry(0), asynq.Unique(renderUniqueTTL)); err != nil {
		// A duplicate is success: the reconcile loop re-POSTs record on every tick
		// until the worker dequeues the unique task, so a 202 here keeps the reel
		// advancing instead of being marked failed mid-capture.
		if errors.Is(err, asynq.ErrDuplicateTask) {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"id":        j.ID,
				"task":      tasks.TypeRecordDemo,
				"duplicate": true,
			})
			return
		}
		internalError(w, "enqueue record task", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":   j.ID,
		"task": tasks.TypeRecordDemo,
	})
}

// StartGenerate handles POST /api/jobs/{id}/generate. It captures the full
// one-click choice (preset, music, edit) as the job's generate intent and
// enqueues the recording. The record worker reads the intent on success and
// enqueues the matching render, so the user acts once and the chosen treatment
// flows automatically from capture to upload pack.
func (h *Handlers) StartGenerate(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	// Same entry points as recording: a parsed job, or a recorded/failed job
	// being re-run in place. The kill plan must exist before we can record.
	if (j.Status != job.StatusParsed && j.Status != job.StatusRecorded && j.Status != job.StatusFailed) || j.KillPlan == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("job is not ready to generate (status=%s)", j.Status))
		return
	}
	if !h.requireRecordEnabled(w) {
		return
	}
	var req struct {
		Preset     string                 `json:"preset"`
		Music      string                 `json:"music"`
		Edit       renderplan.EditRequest `json:"edit"`
		SegmentIDs []string               `json:"segment_ids"`
	}
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
		switch err := json.NewDecoder(r.Body).Decode(&req); {
		case err == nil, errors.Is(err, io.EOF):
		default:
			writeError(w, http.StatusBadRequest, "invalid generate request JSON")
			return
		}
	}
	preset, ok := editor.PresetByName(req.Preset)
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unknown preset %q", req.Preset))
		return
	}
	if !validateSegmentSelection(w, j, req.SegmentIDs) {
		return
	}
	intent := renderplan.GenerateIntent{
		Variant:     preset.Name,
		MusicKey:    req.Music,
		Edit:        renderplan.NormalizeEditRequest(req.Edit),
		ActiveRunID: uuid.New(),
		AcceptedAt:  time.Now().UTC(),
	}
	if err := intent.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Build the render task now so an invalid music key fails fast here rather
	// than silently dropping the chained render later in the record worker.
	if _, err := tasks.NewRenderVariantTask(j.ID, intent.Variant, intent.MusicKey, 0, intent.Edit); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	portraitSafeKillfeed := preset.HUDMode == string(recording.HUDModeDeathnotices) && intent.Edit.Format == renderplan.FormatShort9x16
	recordTask, err := tasks.NewGenerateRecordDemoTask(j.ID, preset.HUDMode, req.SegmentIDs, portraitSafeKillfeed, intent)
	if err != nil {
		internalError(w, "build record task", err)
		return
	}
	// The task header is the worker's immutable source of truth. The job-scoped
	// artifact is the latest accepted choice shown by the workbench; duplicate
	// and rejected admissions must not replace the active choice.
	accepted := false
	_, err = h.queue.EnqueueWithTransition(recordTask, func(decision error) error {
		switch {
		case decision == nil:
			if err := h.generateIntents.Begin(j.ID, intent, func() error {
				return h.requireGenerateRenderIdle(j.ID)
			}); err != nil {
				return err
			}
			accepted = true
			return nil
		case errors.Is(decision, asynq.ErrDuplicateTask):
			existing, ok, readErr := h.readGenerateIntent(j.ID)
			if readErr != nil {
				return readErr
			}
			if ok {
				intent = existing
			}
			return nil
		default:
			if accepted {
				_, err := h.generateIntents.Finish(j.ID, intent.ActiveRunID, func() error {
					return h.persistJobQueueDecision(j.ID, "generate record", decision)
				})
				return err
			}
			return nil
		}
	}, asynq.MaxRetry(0), asynq.Unique(renderUniqueTTL))
	if err != nil {
		// A duplicate is success (see StartRecording): a re-drive must not flip a
		// reel that is already capturing to failed.
		if errors.Is(err, asynq.ErrDuplicateTask) {
			writeJSON(w, http.StatusAccepted, map[string]any{
				"id":        j.ID,
				"task":      tasks.TypeRecordDemo,
				"variant":   intent.Variant,
				"duplicate": true,
			})
			return
		}
		if errors.Is(err, generateintent.ErrActiveRun) || errors.Is(err, errGenerateRenderActive) {
			writeError(w, http.StatusConflict, "job already has active generate or render work")
			return
		}
		internalError(w, "enqueue record task", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":      j.ID,
		"task":    tasks.TypeRecordDemo,
		"variant": intent.Variant,
	})
}

func (h *Handlers) requireGenerateRenderIdle(id uuid.UUID) error {
	for _, loadout := range renderplan.LoadoutCatalog() {
		state, ok, err := h.readRenderVariantState(id, loadout.Variant)
		if err != nil {
			return fmt.Errorf("read %s render state: %w", loadout.Variant, err)
		}
		if ok && (state.Status == renderplan.RenderVariantStatusQueued || state.Status == renderplan.RenderVariantStatusRendering) {
			return fmt.Errorf("%w: %s is %s", errGenerateRenderActive, loadout.Variant, state.Status)
		}
	}
	return nil
}

func (h *Handlers) writeGenerateIntent(id uuid.UUID, intent renderplan.GenerateIntent) error {
	return h.generateIntents.Write(id, intent)
}

func (h *Handlers) readGenerateIntent(id uuid.UUID) (renderplan.GenerateIntent, bool, error) {
	return h.generateIntents.Read(id)
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

// renderMusicRequest is the "music" field of a render request. It accepts
// either a bare track key ("phonk-01") or an object {"key","volume"} so a
// client can also set the music mix gain. Volume is in (0,1]; 0 means the
// render default. Accepting both keeps older string-only clients working.
type renderMusicRequest struct {
	Key    string
	Volume float64
}

func (m *renderMusicRequest) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		return json.Unmarshal(trimmed, &m.Key)
	}
	var obj struct {
		Key    string  `json:"key"`
		Volume float64 `json:"volume"`
	}
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return err
	}
	m.Key = obj.Key
	m.Volume = obj.Volume
	return nil
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
	// Optional JSON body { "music": "<track-key>", "edit": {...} } selects a
	// track to mix in. "music" also accepts an object {"key","volume"} so the
	// client can set the music gain; volume is in (0,1], 0 means the default.
	var musicKey string
	var musicVolume float64
	editRequest := renderplan.DefaultEditRequest()
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
		var req struct {
			Music renderMusicRequest     `json:"music"`
			Edit  renderplan.EditRequest `json:"edit"`
		}
		switch err := json.NewDecoder(r.Body).Decode(&req); {
		case err == nil, errors.Is(err, io.EOF):
			musicKey = req.Music.Key
			musicVolume = req.Music.Volume
			if musicVolume < 0 || musicVolume > 1 {
				writeError(w, http.StatusBadRequest, "music volume must be between 0 and 1")
				return
			}
			editRequest = renderplan.NormalizeEditRequest(req.Edit)
			if err := editRequest.Validate(); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
		default:
			writeError(w, http.StatusBadRequest, "invalid render request JSON")
			return
		}
	}
	task, err := tasks.NewRenderVariantTask(j.ID, variant, musicKey, musicVolume, editRequest)
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
	_, err = h.queue.EnqueueWithTransition(task, func(decision error) error {
		switch {
		case decision == nil:
			return h.generateIntents.WhileIdle(j.ID, func() error {
				return h.writeRenderVariantState(state)
			})
		case errors.Is(decision, asynq.ErrDuplicateTask):
			existing, ok, readErr := h.readRenderVariantState(j.ID, variant)
			if readErr != nil {
				return readErr
			}
			if ok {
				state = *existing
			}
			return nil
		default:
			failedState, stateErr := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
				JobID:    j.ID,
				Loadout:  loadout,
				Status:   renderplan.RenderVariantStatusFailed,
				Error:    "enqueue render task: " + decision.Error(),
				Previous: &state,
			})
			if stateErr != nil {
				return stateErr
			}
			return h.writeRenderVariantState(failedState)
		}
	}, asynq.Unique(renderUniqueTTL))
	if err != nil {
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
		if errors.Is(err, generateintent.ErrActiveRun) {
			writeError(w, http.StatusConflict, "guided generation is active for this job")
			return
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
		h.writeRenderVariant(w, state)
		return
	}
	resultRef, err := renderplan.NewRenderVariantArtifactRef(j.ID, variant, renderplan.RenderVariantArtifactResult, "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rc, err := h.storage.Open(resultRef.Key)
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
	h.writeRenderVariant(w, &state)
}

// renderArtifactLister is the optional storage capability GetRenderVariant uses
// to report the reel's real artifact file names. Local filesystem storage
// implements it; a backend without listing reports empty arrays.
type renderArtifactLister interface {
	List(prefix string) ([]string, error)
}

// listArtifactDir lists the base file names in the storage directory that holds
// key (e.g. the segments dir for a segment-clip key, or the videos dir for a
// render-video key). ok is false when the backend cannot list directories or the
// listing failed; a directory a stage has not written yet lists as empty with
// ok true. Callers build their own key and filter the returned names.
func listArtifactDir(store storage.Storage, key string) ([]string, bool) {
	lister, ok := store.(renderArtifactLister)
	if !ok {
		return nil, false
	}
	files, err := lister.List(path.Dir(key))
	if err != nil {
		return nil, false
	}
	return files, true
}

// renderVariantResponse augments the durable render state with the reel's real
// on-disk artifact names, so the client addresses the reel's video and cover by
// the names the editor actually wrote instead of guessing them from segment ids.
type renderVariantResponse struct {
	*renderplan.RenderVariantState
	Videos []string `json:"videos"`
	Covers []string `json:"covers"`
}

// artifactNamePlaceholder is a valid artifact token used only to resolve a
// variant's artifact directory key; its base name is discarded.
const artifactNamePlaceholder = "placeholder"

// writeRenderVariant writes the render-variant state plus the reel's real video
// and cover artifact names (empty arrays when the variant has none yet).
func (h *Handlers) writeRenderVariant(w http.ResponseWriter, state *renderplan.RenderVariantState) {
	videos, err := h.listRenderArtifactNames(state.JobID, state.Variant, renderplan.RenderVariantArtifactVideo)
	if err != nil {
		internalError(w, "list render videos", err)
		return
	}
	covers, err := h.listRenderArtifactNames(state.JobID, state.Variant, renderplan.RenderVariantArtifactCover)
	if err != nil {
		internalError(w, "list render covers", err)
		return
	}
	writeJSON(w, http.StatusOK, renderVariantResponse{RenderVariantState: state, Videos: videos, Covers: covers})
}

// listRenderArtifactNames returns the artifact names (file base names, extension
// stripped) present under the variant's directory for the given kind, reusing
// the same key resolution the videos/{name} and covers/{name} handlers use. The
// result is empty when the backend cannot list or the directory is absent.
func (h *Handlers) listRenderArtifactNames(id uuid.UUID, variant string, kind renderplan.RenderVariantArtifactKind) ([]string, error) {
	ref, err := renderplan.NewRenderVariantArtifactRef(id, variant, kind, artifactNamePlaceholder)
	if err != nil {
		return nil, err
	}
	files, ok := listArtifactDir(h.storage, ref.Key)
	if !ok {
		return []string{}, nil
	}
	ext := path.Ext(ref.Key)
	names := make([]string, 0, len(files))
	for _, f := range files {
		if ext != "" && !strings.HasSuffix(f, ext) {
			continue
		}
		names = append(names, strings.TrimSuffix(f, ext))
	}
	return names, nil
}

func (h *Handlers) readRenderVariantState(id uuid.UUID, variant string) (*renderplan.RenderVariantState, bool, error) {
	key, err := renderplan.RenderVariantStateKey(id, variant)
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
	key, err := renderplan.RenderVariantStateKey(state.JobID, state.Variant)
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
	key, err := renderplan.RenderVariantStateKey(id, variant)
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
	result, _, ok := h.loadRenderResult(w, j.ID, variant)
	if !ok {
		return
	}
	segmentIDs := make([]string, 0, len(result.Shorts))
	for _, short := range result.Shorts {
		segmentIDs = append(segmentIDs, short.SegmentID)
	}
	board, err := renderplan.NewPublishBoardForVariant(renderplan.NewPublishBoardForVariantOptions{
		JobID:          j.ID,
		Variant:        variant,
		SegmentIDs:     segmentIDs,
		Warnings:       result.Warnings,
		Error:          result.Error,
		ArtifactExists: h.storage.Exists,
	})
	if err != nil {
		internalError(w, "build publish board", err)
		return
	}
	writeJSON(w, http.StatusOK, board)
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
	agentArtifacts, err := renderplan.NewAgentArtifacts(j.ID, variant, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		internalError(w, "build agent artifact keys", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":          j.ID,
		"task":        tasks.TypeCodexAgent,
		"variant":     variant,
		"kind":        renderplan.AgentKindCaptionCandidates,
		"context_key": agentArtifacts.ContextKey,
		"result_key":  agentArtifacts.ResultKey,
	})
}

// GetCaptionAgent handles GET /api/jobs/{id}/renders/{variant}/agent/captions.
func (h *Handlers) GetCaptionAgent(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	agentArtifacts, err := renderplan.NewAgentArtifacts(j.ID, variant, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rc, err := h.storage.Open(agentArtifacts.ResultKey)
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
	h.streamRenderVariantArtifact(w, r, "application/json", renderplan.RenderVariantArtifactPackManifest, "")
}

// GetRenderEditDocument streams the stable edit intent document.
func (h *Handlers) GetRenderEditDocument(w http.ResponseWriter, r *http.Request) {
	h.streamRenderVariantArtifact(w, r, "application/json", renderplan.RenderVariantArtifactEditDocument, "")
}

// GetRenderGallery streams the render variant publish gallery.
func (h *Handlers) GetRenderGallery(w http.ResponseWriter, r *http.Request) {
	h.streamRenderVariantArtifact(w, r, "text/html; charset=utf-8", renderplan.RenderVariantArtifactGallery, "")
}

// GetRenderVideo streams one render variant MP4 artifact.
func (h *Handlers) GetRenderVideo(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	h.streamRenderVariantArtifact(w, r, "video/mp4", renderplan.RenderVariantArtifactVideo, name)
}

// renderArtifactDeleter is the optional storage capability DeleteRenderVideo
// needs. Local filesystem storage implements it; a backend without delete
// support makes the endpoint report 501 rather than pretending to delete.
type renderArtifactDeleter interface {
	Delete(key string) error
}

// DeleteRenderVideo handles DELETE /api/jobs/{id}/renders/{variant}/videos/{name}:
// it removes one reel's video plus its cover and caption artifacts so the user
// can clear finished reels from the library and free disk space. Idempotent —
// deleting an already-deleted reel succeeds.
func (h *Handlers) DeleteRenderVideo(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	if _, err := renderplan.LoadoutForVariant(variant); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	deleter, ok := h.storage.(renderArtifactDeleter)
	if !ok {
		writeError(w, http.StatusNotImplemented, "storage backend does not support delete")
		return
	}
	name := chi.URLParam(r, "name")
	kinds := []renderplan.RenderVariantArtifactKind{
		renderplan.RenderVariantArtifactVideo,
		renderplan.RenderVariantArtifactCover,
		renderplan.RenderVariantArtifactCaption,
	}
	for _, kind := range kinds {
		ref, err := renderplan.NewRenderVariantArtifactRef(j.ID, variant, kind, name)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := deleter.Delete(ref.Key); err != nil {
			internalError(w, "delete render artifact", err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// jobArtifactDeleter is the optional storage capability DeleteJob needs: a
// single-file Delete for the stored demo copy and a recursive DeleteTree for
// the job's artifact directory. Local filesystem storage implements it; a
// backend without delete support makes the endpoint report 501 rather than
// pretending to delete.
type jobArtifactDeleter interface {
	Delete(key string) error
	DeleteTree(key string) error
}

// jobIsInFlight reports whether a stage is actively working on the job's files
// or processes, so deleting now would race that work. queued is included: a
// parse/scan task may be about to run against the stored demo.
func jobIsInFlight(s job.Status) bool {
	switch s {
	case job.StatusQueued, job.StatusScanning, job.StatusParsing, job.StatusRecording, job.StatusComposing:
		return true
	default:
		return false
	}
}

// DeleteJob handles DELETE /api/jobs/{id}: it removes a job together with its
// artifact tree (jobs/<id>) and its stored demo copy (demos/<id>.dem) so the
// user can clear a demo from the library and reclaim disk space. Settled jobs
// (scanned, parsed, recorded, composed, done, failed) delete; a job with work
// in flight is refused with 409 until it settles. The job row is removed last
// so a failed artifact delete leaves the row in place to retry. Idempotent —
// a repeat delete after success returns 404.
func (h *Handlers) DeleteJob(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	if jobIsInFlight(j.Status) {
		writeError(w, http.StatusConflict, fmt.Sprintf("job is %s; wait for it to settle before deleting", j.Status))
		return
	}
	deleter, ok := h.storage.(jobArtifactDeleter)
	if !ok {
		writeError(w, http.StatusNotImplemented, "storage backend does not support delete")
		return
	}
	if err := deleter.DeleteTree(fmt.Sprintf("jobs/%s", j.ID)); err != nil {
		internalError(w, "delete job artifacts", err)
		return
	}
	if err := deleter.Delete(fmt.Sprintf("demos/%s.dem", j.ID)); err != nil {
		internalError(w, "delete job demo", err)
		return
	}
	if err := h.repo.Delete(r.Context(), j.ID); err != nil {
		internalError(w, "delete job", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetRenderCover streams one render variant cover artifact.
func (h *Handlers) GetRenderCover(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	h.streamRenderVariantArtifact(w, r, "image/jpeg", renderplan.RenderVariantArtifactCover, name)
}

// GetRenderCaption streams one render variant caption artifact.
func (h *Handlers) GetRenderCaption(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	h.streamRenderVariantArtifact(w, r, "text/plain; charset=utf-8", renderplan.RenderVariantArtifactCaption, name)
}

func (h *Handlers) streamRenderVariantArtifact(w http.ResponseWriter, r *http.Request, contentType string, kind renderplan.RenderVariantArtifactKind, segmentID string) {
	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	if _, err := renderplan.LoadoutForVariant(variant); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ref, err := renderplan.NewRenderVariantArtifactRef(j.ID, variant, kind, segmentID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rc, err := h.storage.Open(ref.Key)
	if err != nil {
		writeError(w, http.StatusNotFound, "render artifact not found")
		return
	}
	serveArtifact(w, r, contentType, rc)
}

// serveArtifact writes an artifact body with the given content type. An empty
// type asks Go to sniff the stored bytes, which is useful when the durable key
// intentionally omits the uploaded source container. When the storage reader
// is seekable (the local filesystem backend hands out *os.File), it serves
// through http.ServeContent so Range requests are honoured. Non-seekable
// backends sniff a bounded prefix before streaming the complete body.
func serveArtifact(w http.ResponseWriter, r *http.Request, contentType string, rc io.ReadCloser) {
	defer rc.Close()
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, "", time.Time{}, rs)
		return
	}
	var body io.Reader = rc
	if contentType == "" {
		buffered := bufio.NewReader(rc)
		prefix, _ := buffered.Peek(512)
		w.Header().Set("Content-Type", http.DetectContentType(prefix))
		body = buffered
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, body)
}

func (h *Handlers) loadRenderResult(w http.ResponseWriter, id uuid.UUID, variant string) (editor.Result, string, bool) {
	resultRef, err := renderplan.NewRenderVariantArtifactRef(id, variant, renderplan.RenderVariantArtifactResult, "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return editor.Result{}, "", false
	}
	rc, err := h.storage.Open(resultRef.Key)
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
	return result, resultRef.Key, true
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
