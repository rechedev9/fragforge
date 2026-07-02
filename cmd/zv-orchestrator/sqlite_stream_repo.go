package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

// sqliteStreamJobRepository persists streamer-clip jobs (internal/streamclips)
// in the same local SQLite database as sqliteJobRepository, so the
// stream-jobs API works on the local desktop studio, which has no Postgres.
// It shares the *sql.DB opened by newSQLiteJobRepository (see main.go)
// instead of opening the database file a second time: a single connection
// (db.SetMaxOpenConns(1) is set once, by the job repository) serializes all
// writers across both tables.
type sqliteStreamJobRepository struct {
	db *sql.DB
}

// newSQLiteStreamJobRepository ensures the stream_jobs table exists on db and
// returns a repository backed by it. db is expected to already have its
// pragmas set (WAL, busy_timeout) by whoever opened it.
func newSQLiteStreamJobRepository(db *sql.DB) (*sqliteStreamJobRepository, error) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS stream_jobs (
		id             TEXT    PRIMARY KEY,
		status         TEXT    NOT NULL,
		failure_reason TEXT,
		source_path    TEXT    NOT NULL,
		source_sha256  TEXT    NOT NULL,
		source_url     TEXT,
		title          TEXT,
		probe          TEXT    NOT NULL,
		edit_plan      TEXT,
		created_at     INTEGER NOT NULL,
		updated_at     INTEGER NOT NULL
	)`); err != nil {
		return nil, fmt.Errorf("create stream_jobs table: %w", err)
	}
	return &sqliteStreamJobRepository{db: db}, nil
}

func (r *sqliteStreamJobRepository) Create(ctx context.Context, j *streamclips.Job) error {
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
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO stream_jobs (id, status, failure_reason, source_path, source_sha256, source_url, title, probe, edit_plan, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		j.ID.String(), string(j.Status), nullableText(j.FailureReason), j.SourcePath, j.SourceSHA256,
		nullableText(j.SourceURL), nullableText(j.Title), probeJSON, nullableEditPlan(j.EditPlan),
		now.UnixNano(), now.UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("insert stream job: %w", err)
	}
	return nil
}

func (r *sqliteStreamJobRepository) Get(ctx context.Context, id uuid.UUID) (streamclips.Job, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, status, COALESCE(failure_reason,''), source_path, source_sha256,
		        COALESCE(source_url,''), COALESCE(title,''), probe, edit_plan, created_at, updated_at
		 FROM stream_jobs WHERE id = ?`, id.String())
	return scanSQLiteStreamJob(row)
}

func (r *sqliteStreamJobRepository) List(ctx context.Context, limit int) ([]streamclips.Job, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, status, COALESCE(failure_reason,''), source_path, source_sha256,
		        COALESCE(source_url,''), COALESCE(title,''), probe, edit_plan, created_at, updated_at
		 FROM stream_jobs ORDER BY updated_at DESC, created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query stream jobs: %w", err)
	}
	defer rows.Close()

	out := []streamclips.Job{}
	for rows.Next() {
		j, err := scanSQLiteStreamJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stream jobs: %w", err)
	}
	return out, nil
}

func (r *sqliteStreamJobRepository) UpdateStatus(ctx context.Context, id uuid.UUID, status streamclips.Status, failureReason string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE stream_jobs SET status = ?, failure_reason = ?, updated_at = ? WHERE id = ?`,
		string(status), nullableText(failureReason), time.Now().UTC().UnixNano(), id.String(),
	)
	if err != nil {
		return fmt.Errorf("update stream job status: %w", err)
	}
	return checkStreamJobRowsAffected(res)
}

// SetAcquired records a successful acquire-by-URL download: the probed source
// metadata and sha256, moving the job to "ready". It clears any prior failure
// reason so a retried acquire does not leave a stale message behind.
func (r *sqliteStreamJobRepository) SetAcquired(ctx context.Context, id uuid.UUID, probe streamclips.SourceProbe, sha256 string) error {
	probeJSON, err := json.Marshal(probe)
	if err != nil {
		return fmt.Errorf("marshal probe: %w", err)
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE stream_jobs SET probe = ?, source_sha256 = ?, status = ?, failure_reason = NULL, updated_at = ? WHERE id = ?`,
		probeJSON, sha256, string(streamclips.StatusReady), time.Now().UTC().UnixNano(), id.String(),
	)
	if err != nil {
		return fmt.Errorf("update stream job acquired: %w", err)
	}
	return checkStreamJobRowsAffected(res)
}

func (r *sqliteStreamJobRepository) SetEditPlan(ctx context.Context, id uuid.UUID, plan streamclips.EditPlan) error {
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal edit plan: %w", err)
	}
	res, err := r.db.ExecContext(ctx,
		`UPDATE stream_jobs SET edit_plan = ?, status = ?, failure_reason = NULL, updated_at = ? WHERE id = ?`,
		b, string(streamclips.StatusReady), time.Now().UTC().UnixNano(), id.String(),
	)
	if err != nil {
		return fmt.Errorf("update stream edit plan: %w", err)
	}
	return checkStreamJobRowsAffected(res)
}

// sqlScanner is satisfied by both *sql.Row and *sql.Rows, so scanSQLiteStreamJob
// works for Get (one row) and List (many rows) alike.
type sqlScanner interface {
	Scan(dest ...any) error
}

func scanSQLiteStreamJob(row sqlScanner) (streamclips.Job, error) {
	var j streamclips.Job
	var idStr, statusRaw string
	var probeJSON, planJSON []byte
	var createdNano, updatedNano int64
	err := row.Scan(&idStr, &statusRaw, &j.FailureReason, &j.SourcePath, &j.SourceSHA256,
		&j.SourceURL, &j.Title, &probeJSON, &planJSON, &createdNano, &updatedNano)
	if errors.Is(err, sql.ErrNoRows) {
		return streamclips.Job{}, streamclips.ErrNotFound
	}
	if err != nil {
		return streamclips.Job{}, fmt.Errorf("scan stream job: %w", err)
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return streamclips.Job{}, fmt.Errorf("parse stream job id: %w", err)
	}
	j.ID = id
	status, err := streamclips.ParseStatus(statusRaw)
	if err != nil {
		return streamclips.Job{}, err
	}
	j.Status = status
	if len(probeJSON) > 0 {
		if err := json.Unmarshal(probeJSON, &j.Probe); err != nil {
			return streamclips.Job{}, fmt.Errorf("unmarshal probe: %w", err)
		}
	}
	if len(planJSON) > 0 {
		j.EditPlan = append(json.RawMessage(nil), planJSON...)
	}
	j.CreatedAt = time.Unix(0, createdNano).UTC()
	j.UpdatedAt = time.Unix(0, updatedNano).UTC()
	return j, nil
}

// checkStreamJobRowsAffected turns a zero-row UPDATE into streamclips.ErrNotFound,
// matching the postgres repository's RowsAffected() == 0 semantics.
func checkStreamJobRowsAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return streamclips.ErrNotFound
	}
	return nil
}

// nullableText maps an empty string to SQL NULL so COALESCE(...,”) in the
// SELECTs above round-trips "unset" the same way the postgres repository's
// nullable TEXT columns do.
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableEditPlan(plan json.RawMessage) any {
	if len(plan) == 0 {
		return nil
	}
	return []byte(plan)
}
