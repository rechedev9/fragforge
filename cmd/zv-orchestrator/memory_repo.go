package main

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

type orchestratorJobRepository interface {
	Create(context.Context, *job.Job) error
	Get(context.Context, uuid.UUID) (job.Job, error)
	GetMeta(context.Context, uuid.UUID) (job.Job, error)
	List(context.Context, int) ([]job.Job, error)
	ListByStatus(context.Context, job.Status) ([]job.Job, error)
	UpdateStatus(context.Context, uuid.UUID, job.Status, string) error
	SetParseInputs(context.Context, uuid.UUID, string, rules.Rules) error
	SetKillPlan(context.Context, uuid.UUID, killplan.Plan) error
}

type memoryJobRepository struct {
	mu   sync.RWMutex
	jobs map[uuid.UUID]job.Job
}

func newMemoryJobRepository() *memoryJobRepository {
	return &memoryJobRepository{jobs: map[uuid.UUID]job.Job{}}
}

func (r *memoryJobRepository) Create(ctx context.Context, j *job.Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	now := time.Now().UTC()
	j.CreatedAt = now
	j.UpdatedAt = now
	stored := cloneJob(*j)
	r.jobs[stored.ID] = stored
	*j = cloneJob(stored)
	return nil
}

func (r *memoryJobRepository) Get(ctx context.Context, id uuid.UUID) (job.Job, error) {
	if err := ctx.Err(); err != nil {
		return job.Job{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	j, ok := r.jobs[id]
	if !ok {
		return job.Job{}, job.ErrNotFound
	}
	return cloneJob(j), nil
}

func (r *memoryJobRepository) GetMeta(ctx context.Context, id uuid.UUID) (job.Job, error) {
	j, err := r.Get(ctx, id)
	if err != nil {
		return job.Job{}, err
	}
	j.KillPlan = nil
	return j, nil
}

func (r *memoryJobRepository) List(ctx context.Context, limit int) ([]job.Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]job.Job, 0, len(r.jobs))
	for _, j := range r.jobs {
		copied := cloneJob(j)
		copied.KillPlan = nil
		out = append(out, copied)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *memoryJobRepository) ListByStatus(ctx context.Context, status job.Status) ([]job.Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []job.Job{}
	for _, j := range r.jobs {
		if j.Status == status {
			out = append(out, cloneJob(j))
		}
	}
	return out, nil
}

func (r *memoryJobRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status job.Status, failureReason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return job.ErrNotFound
	}
	j.Status = status
	j.FailureReason = failureReason
	j.UpdatedAt = time.Now().UTC()
	r.jobs[id] = j
	return nil
}

func (r *memoryJobRepository) SetParseInputs(ctx context.Context, id uuid.UUID, steamID string, rl rules.Rules) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return job.ErrNotFound
	}
	if j.Status != job.StatusScanned && j.Status != job.StatusParsed {
		return job.ErrConflict
	}
	j.TargetSteamID = steamID
	j.Rules = rl
	j.Status = job.StatusParsing
	j.UpdatedAt = time.Now().UTC()
	r.jobs[id] = j
	return nil
}

func (r *memoryJobRepository) SetKillPlan(ctx context.Context, id uuid.UUID, plan killplan.Plan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return job.ErrNotFound
	}
	planCopy := plan
	j.KillPlan = &planCopy
	j.UpdatedAt = time.Now().UTC()
	r.jobs[id] = j
	return nil
}

func cloneJob(j job.Job) job.Job {
	if j.KillPlan != nil {
		plan := *j.KillPlan
		j.KillPlan = &plan
	}
	return j
}

// memoryStreamJobRepository is the in-memory equivalent of
// streamclips.Repository, used when ZV_DATABASE_URL=memory (Local Studio)
// so the streamer-clips flow, including acquisition-by-URL, works without
// Postgres.
type memoryStreamJobRepository struct {
	mu   sync.RWMutex
	jobs map[uuid.UUID]streamclips.Job
}

func newMemoryStreamJobRepository() *memoryStreamJobRepository {
	return &memoryStreamJobRepository{jobs: map[uuid.UUID]streamclips.Job{}}
}

func (r *memoryStreamJobRepository) Create(ctx context.Context, j *streamclips.Job) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	now := time.Now().UTC()
	j.CreatedAt = now
	j.UpdatedAt = now
	stored := cloneStreamJob(*j)
	r.jobs[stored.ID] = stored
	*j = cloneStreamJob(stored)
	return nil
}

func (r *memoryStreamJobRepository) Get(ctx context.Context, id uuid.UUID) (streamclips.Job, error) {
	if err := ctx.Err(); err != nil {
		return streamclips.Job{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	j, ok := r.jobs[id]
	if !ok {
		return streamclips.Job{}, streamclips.ErrNotFound
	}
	return cloneStreamJob(j), nil
}

func (r *memoryStreamJobRepository) List(ctx context.Context, limit int) ([]streamclips.Job, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]streamclips.Job, 0, len(r.jobs))
	for _, j := range r.jobs {
		out = append(out, cloneStreamJob(j))
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].UpdatedAt.Equal(out[k].UpdatedAt) {
			return out[i].CreatedAt.After(out[k].CreatedAt)
		}
		return out[i].UpdatedAt.After(out[k].UpdatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *memoryStreamJobRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status streamclips.Status, failureReason string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return streamclips.ErrNotFound
	}
	j.Status = status
	j.FailureReason = failureReason
	j.UpdatedAt = time.Now().UTC()
	r.jobs[id] = j
	return nil
}

func (r *memoryStreamJobRepository) SetEditPlan(ctx context.Context, id uuid.UUID, plan streamclips.EditPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return streamclips.ErrNotFound
	}
	j.EditPlan = append(json.RawMessage(nil), b...)
	j.Status = streamclips.StatusReady
	j.FailureReason = ""
	j.UpdatedAt = time.Now().UTC()
	r.jobs[id] = j
	return nil
}

func (r *memoryStreamJobRepository) SetAcquired(ctx context.Context, id uuid.UUID, probe streamclips.SourceProbe, sha256 string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return streamclips.ErrNotFound
	}
	j.Probe = probe
	j.SourceSHA256 = sha256
	j.Status = streamclips.StatusReady
	j.FailureReason = ""
	j.UpdatedAt = time.Now().UTC()
	r.jobs[id] = j
	return nil
}

func cloneStreamJob(j streamclips.Job) streamclips.Job {
	if j.EditPlan != nil {
		j.EditPlan = append(json.RawMessage(nil), j.EditPlan...)
	}
	return j
}
