package streamclips

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("stream job not found")

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, j *Job) error {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	now := time.Now().UTC()
	j.CreatedAt = now
	j.UpdatedAt = now
	probeJSON, err := json.Marshal(j.Probe)
	if err != nil {
		return fmt.Errorf("marshal probe: %w", err)
	}
	_, err = r.pool.Exec(ctx,
		`INSERT INTO stream_jobs (id, status, source_path, source_sha256, title, probe, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		j.ID, string(j.Status), j.SourcePath, j.SourceSHA256, j.Title, probeJSON, j.CreatedAt, j.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert stream job: %w", err)
	}
	return nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (Job, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, status, COALESCE(failure_reason,''), source_path, source_sha256,
		        COALESCE(title,''), probe, edit_plan, created_at, updated_at
		 FROM stream_jobs WHERE id = $1`, id)
	return scanJob(row)
}

func (r *Repository) List(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, status, COALESCE(failure_reason,''), source_path, source_sha256,
		        COALESCE(title,''), probe, edit_plan, created_at, updated_at
		 FROM stream_jobs ORDER BY updated_at DESC, created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query stream jobs: %w", err)
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stream jobs: %w", err)
	}
	return jobs, nil
}

func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status Status, failureReason string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE stream_jobs SET status = $2, failure_reason = NULLIF($3,''), updated_at = NOW()
		 WHERE id = $1`,
		id, string(status), failureReason)
	if err != nil {
		return fmt.Errorf("update stream job status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) SetEditPlan(ctx context.Context, id uuid.UUID, plan EditPlan) error {
	plan = NormalizeEditPlan(plan)
	if err := plan.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal edit plan: %w", err)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE stream_jobs SET edit_plan = $2, status = $3, failure_reason = NULL, updated_at = NOW()
		 WHERE id = $1`,
		id, b, string(StatusReady))
	if err != nil {
		return fmt.Errorf("update stream edit plan: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	var statusRaw string
	var probeJSON []byte
	var planJSON []byte
	err := row.Scan(&j.ID, &statusRaw, &j.FailureReason, &j.SourcePath, &j.SourceSHA256,
		&j.Title, &probeJSON, &planJSON, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("scan stream job: %w", err)
	}
	status, err := ParseStatus(statusRaw)
	if err != nil {
		return Job{}, err
	}
	j.Status = status
	if len(probeJSON) > 0 {
		if err := json.Unmarshal(probeJSON, &j.Probe); err != nil {
			return Job{}, fmt.Errorf("unmarshal probe: %w", err)
		}
	}
	if len(planJSON) > 0 {
		j.EditPlan = append(json.RawMessage(nil), planJSON...)
	}
	return j, nil
}
