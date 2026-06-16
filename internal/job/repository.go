package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/rules"
)

// ErrNotFound is returned by Get when no job has the requested id.
var ErrNotFound = errors.New("job not found")

// ErrConflict is returned when an operation is rejected because the job is not
// in a state that allows it (e.g. a parse request for a job that was never
// scanned). It maps to HTTP 409 at the API boundary.
var ErrConflict = errors.New("job state conflict")

// Repository persists Jobs in Postgres.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository wraps a pgx pool for job persistence.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts the job. Sets ID if zero, plus CreatedAt/UpdatedAt.
func (r *Repository) Create(ctx context.Context, j *Job) error {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	now := time.Now().UTC()
	j.CreatedAt = now
	j.UpdatedAt = now

	rulesJSON, err := json.Marshal(j.Rules)
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO jobs (id, status, demo_path, demo_sha256, target_steamid, rules, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		j.ID, j.Status.String(), j.DemoPath, j.DemoSHA256, j.TargetSteamID, rulesJSON, j.CreatedAt, j.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

// Get returns the job with the given id or ErrNotFound.
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (Job, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, status, COALESCE(failure_reason,''), demo_path, demo_sha256,
		        target_steamid, rules, kill_plan, created_at, updated_at
		 FROM jobs WHERE id = $1`, id,
	)
	var j Job
	var statusStr string
	var rulesJSON []byte
	var planJSON []byte
	err := row.Scan(&j.ID, &statusStr, &j.FailureReason, &j.DemoPath, &j.DemoSHA256,
		&j.TargetSteamID, &rulesJSON, &planJSON, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("scan job: %w", err)
	}

	j.Status, err = ParseStatus(statusStr)
	if err != nil {
		return Job{}, err
	}
	if err := json.Unmarshal(rulesJSON, &j.Rules); err != nil {
		return Job{}, fmt.Errorf("unmarshal rules: %w", err)
	}
	if len(planJSON) > 0 {
		var p killplan.Plan
		if err := json.Unmarshal(planJSON, &p); err != nil {
			return Job{}, fmt.Errorf("unmarshal kill_plan: %w", err)
		}
		j.KillPlan = &p
	}
	return j, nil
}

// GetMeta returns the job without its kill_plan. kill_plan is by far the largest
// column on the row (the full segment plan as JSONB), so callers that only need
// status and metadata — the parser and compose workers — must avoid fetching and
// unmarshalling it on every task. The returned Job has a nil KillPlan.
func (r *Repository) GetMeta(ctx context.Context, id uuid.UUID) (Job, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, status, COALESCE(failure_reason,''), demo_path, demo_sha256,
		        target_steamid, rules, created_at, updated_at
		 FROM jobs WHERE id = $1`, id,
	)
	var j Job
	var statusStr string
	var rulesJSON []byte
	err := row.Scan(&j.ID, &statusStr, &j.FailureReason, &j.DemoPath, &j.DemoSHA256,
		&j.TargetSteamID, &rulesJSON, &j.CreatedAt, &j.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("scan job: %w", err)
	}

	j.Status, err = ParseStatus(statusStr)
	if err != nil {
		return Job{}, err
	}
	if err := json.Unmarshal(rulesJSON, &j.Rules); err != nil {
		return Job{}, fmt.Errorf("unmarshal rules: %w", err)
	}
	return j, nil
}

// List returns recent jobs without the large kill_plan payload.
func (r *Repository) List(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, status, COALESCE(failure_reason,''), demo_path, demo_sha256,
		        target_steamid, rules, created_at, updated_at
		 FROM jobs ORDER BY updated_at DESC, created_at DESC LIMIT $1`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query jobs: %w", err)
	}
	defer rows.Close()

	jobs := []Job{}
	for rows.Next() {
		var j Job
		var statusStr string
		var rulesJSON []byte
		if err := rows.Scan(&j.ID, &statusStr, &j.FailureReason, &j.DemoPath, &j.DemoSHA256,
			&j.TargetSteamID, &rulesJSON, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		var err error
		j.Status, err = ParseStatus(statusStr)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rulesJSON, &j.Rules); err != nil {
			return nil, fmt.Errorf("unmarshal rules: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate jobs: %w", err)
	}
	return jobs, nil
}

// UpdateStatus moves the job to a new status. failureReason is set when status=failed.
func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status Status, failureReason string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE jobs SET status = $2, failure_reason = NULLIF($3,''), updated_at = NOW()
		 WHERE id = $1`,
		id, status.String(), failureReason,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetParseInputs atomically claims a scanned (or already parsed) job for a new
// parse: in a single status-gated update it records the picked target and rules
// and moves the job to "parsing". The status guard closes the check-then-act
// race when two parse requests arrive together — only the first update affects a
// row. It returns ErrConflict when the job is not in a parseable state and
// ErrNotFound when the job does not exist.
func (r *Repository) SetParseInputs(ctx context.Context, id uuid.UUID, steamID string, rl rules.Rules) error {
	rulesJSON, err := json.Marshal(rl)
	if err != nil {
		return fmt.Errorf("marshal rules: %w", err)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE jobs SET target_steamid = $2, rules = $3, status = $4, updated_at = NOW()
		 WHERE id = $1 AND status IN ('scanned', 'parsed')`,
		id, steamID, rulesJSON, StatusParsing.String(),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return r.parseInputConflict(ctx, id)
	}
	return nil
}

// parseInputConflict tells a missing job apart from a wrong-state one after a
// status-gated SetParseInputs update affected no rows.
func (r *Repository) parseInputConflict(ctx context.Context, id uuid.UUID) error {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id = $1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return ErrConflict
}

// SetKillPlan persists the kill plan JSONB.
func (r *Repository) SetKillPlan(ctx context.Context, id uuid.UUID, plan killplan.Plan) error {
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE jobs SET kill_plan = $2, updated_at = NOW() WHERE id = $1`,
		id, planJSON,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
