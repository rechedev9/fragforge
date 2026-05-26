// Package httpapi exposes the orchestrator's HTTP endpoints.
package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/reche/zackvideo/internal/artifacts"
	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/rules"
	"github.com/reche/zackvideo/internal/storage"
	"github.com/reche/zackvideo/internal/tasks"
)

const (
	maxDemoBytes       = 500 << 20            // 500 MiB demo cap
	maxMultipartBytes  = maxDemoBytes + 1<<20 // allow multipart headers around the demo
	multipartMemBudget = 32 << 20             // 32 MiB in-memory; spill beyond
)

// JobRepository is the subset of *job.Repository used by handlers.
type JobRepository interface {
	Create(ctx context.Context, j *job.Job) error
	Get(ctx context.Context, id uuid.UUID) (job.Job, error)
}

// Enqueuer is the subset of *asynq.Client used by handlers.
type Enqueuer interface {
	Enqueue(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
}

// Handlers bundles the dependencies needed by every endpoint.
type Handlers struct {
	repo    JobRepository
	storage storage.Storage
	queue   Enqueuer
}

// NewHandlers constructs an HTTP handler set.
func NewHandlers(repo JobRepository, store storage.Storage, queue Enqueuer) *Handlers {
	return &Handlers{repo: repo, storage: store, queue: queue}
}

// createJobConfig is the JSON document sent in the "config" multipart field.
type createJobConfig struct {
	TargetSteamID string       `json:"target_steamid"`
	Rules         *rules.Rules `json:"rules,omitempty"`
}

// CreateJob handles POST /api/jobs.
func (h *Handlers) CreateJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxMultipartBytes)

	// #nosec G120 -- r.Body is capped with MaxBytesReader immediately above.
	if err := r.ParseMultipartForm(multipartMemBudget); err != nil {
		writeError(w, http.StatusBadRequest, "parsing multipart form: "+err.Error())
		return
	}
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
		writeError(w, http.StatusInternalServerError, "storing demo: "+err.Error())
		return
	}
	j.DemoSHA256 = hex.EncodeToString(h256.Sum(nil))

	if err := h.repo.Create(r.Context(), j); err != nil {
		writeError(w, http.StatusInternalServerError, "creating job: "+err.Error())
		return
	}

	task, err := tasks.NewParseDemoTask(j.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "building task: "+err.Error())
		return
	}
	if _, err := h.queue.Enqueue(task); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueueing task: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     j.ID,
		"status": j.Status,
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if j.KillPlan == nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("job not ready (status=%s)", j.Status))
		return
	}
	writeJSON(w, http.StatusOK, j.KillPlan)
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
		writeError(w, http.StatusInternalServerError, "building task: "+err.Error())
		return
	}
	if _, err := h.queue.Enqueue(task, asynq.MaxRetry(0)); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueueing task: "+err.Error())
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
		writeError(w, http.StatusInternalServerError, "building task: "+err.Error())
		return
	}
	if _, err := h.queue.Enqueue(task); err != nil {
		writeError(w, http.StatusInternalServerError, "enqueueing task: "+err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":   j.ID,
		"task": tasks.TypeComposeFinal,
	})
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
		writeError(w, http.StatusInternalServerError, err.Error())
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
