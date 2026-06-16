package main

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/rules"
)

type orchestratorJobRepository interface {
	Create(context.Context, *job.Job) error
	Get(context.Context, uuid.UUID) (job.Job, error)
	GetMeta(context.Context, uuid.UUID) (job.Job, error)
	List(context.Context, int) ([]job.Job, error)
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
