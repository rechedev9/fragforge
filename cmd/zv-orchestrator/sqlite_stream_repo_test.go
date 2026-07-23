package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

// newTestSQLiteStreamRepo builds a sqlite stream job repository the same way
// main.go's sqlite branch does: sharing the *sql.DB opened by
// newSQLiteJobRepository rather than opening the file twice. This is the
// regression coverage for the bug where FragForge Studio (desktop, which runs
// the orchestrator with ZV_DATABASE_URL=sqlite) left streamRepo nil, so every
// /api/stream-jobs endpoint 500'd.
func newTestSQLiteStreamRepo(t *testing.T) *sqliteStreamJobRepository {
	t.Helper()
	jobRepo, err := newSQLiteJobRepository(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("newSQLiteJobRepository: %v", err)
	}
	t.Cleanup(func() { _ = jobRepo.Close() })

	streamRepo, err := newSQLiteStreamJobRepository(jobRepo.db)
	if err != nil {
		t.Fatalf("newSQLiteStreamJobRepository: %v", err)
	}
	return streamRepo
}

func TestSQLiteStreamRepoLifecycle(t *testing.T) {
	repo := newTestSQLiteStreamRepo(t)
	ctx := context.Background()

	j := &streamclips.Job{
		Status:       streamclips.StatusUploaded,
		SourcePath:   "streams/src.mp4",
		SourceSHA256: "abc123",
		Title:        "match stream",
		Probe:        streamclips.SourceProbe{Width: 1280, Height: 720},
	}
	if err := repo.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if j.ID == uuid.Nil {
		t.Fatal("Create did not assign an id")
	}
	if j.CreatedAt.IsZero() || j.UpdatedAt.IsZero() {
		t.Fatal("Create did not set timestamps")
	}

	got, err := repo.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != j.ID || got.SourcePath != j.SourcePath || got.Status != streamclips.StatusUploaded {
		t.Fatalf("Get: got %+v, want id=%s source_path=%s status=uploaded", got, j.ID, j.SourcePath)
	}
	if got.Probe.Width != 1280 || got.Probe.Height != 720 {
		t.Fatalf("Get: probe = %+v, want 1280x720", got.Probe)
	}

	if _, err := repo.Get(ctx, uuid.New()); !errors.Is(err, streamclips.ErrNotFound) {
		t.Fatalf("Get unknown: got %v, want ErrNotFound", err)
	}

	list, err := repo.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != j.ID {
		t.Fatalf("List: got %+v, want one job with id=%s", list, j.ID)
	}

	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 1, EndSeconds: 3}}
	if err := repo.SetEditPlan(ctx, j.ID, plan); err != nil {
		t.Fatalf("SetEditPlan: %v", err)
	}
	got, err = repo.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get after SetEditPlan: %v", err)
	}
	if got.Status != streamclips.StatusReady {
		t.Fatalf("status after SetEditPlan = %s, want ready", got.Status)
	}
	if len(got.EditPlan) == 0 {
		t.Fatal("Get after SetEditPlan: edit plan not persisted")
	}

	if err := repo.UpdateStatus(ctx, j.ID, streamclips.StatusRendering, ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, err = repo.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get after UpdateStatus: %v", err)
	}
	if got.Status != streamclips.StatusRendering {
		t.Fatalf("status after UpdateStatus = %s, want rendering", got.Status)
	}

	if err := repo.UpdateStatus(ctx, j.ID, streamclips.StatusFailed, "render exploded"); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}
	got, err = repo.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get after failed UpdateStatus: %v", err)
	}
	if got.Status != streamclips.StatusFailed || got.FailureReason != "render exploded" {
		t.Fatalf("got status=%s reason=%q, want failed/render exploded", got.Status, got.FailureReason)
	}

	probe := streamclips.SourceProbe{Width: 1920, Height: 1080, DurationSeconds: 42}
	if err := repo.SetAcquired(ctx, j.ID, probe, "def456", "Título de Twitch"); err != nil {
		t.Fatalf("SetAcquired: %v", err)
	}
	got, err = repo.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get after SetAcquired: %v", err)
	}
	if got.Status != streamclips.StatusReady {
		t.Fatalf("status after SetAcquired = %s, want ready", got.Status)
	}
	if got.FailureReason != "" {
		t.Fatalf("failure reason after SetAcquired = %q, want cleared", got.FailureReason)
	}
	if got.SourceSHA256 != "def456" {
		t.Fatalf("sha256 after SetAcquired = %q, want def456", got.SourceSHA256)
	}
	if got.Probe.Width != 1920 || got.Probe.Height != 1080 {
		t.Fatalf("probe after SetAcquired = %+v, want 1920x1080", got.Probe)
	}
	if got.Title != "match stream" {
		t.Fatalf("title after SetAcquired = %q, want user title preserved", got.Title)
	}
}

func TestSQLiteStreamRepoSetAcquiredFillsMissingTitle(t *testing.T) {
	repo := newTestSQLiteStreamRepo(t)
	ctx := context.Background()
	job := &streamclips.Job{Status: streamclips.StatusAcquiring, SourcePath: "streams/source.mp4"}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetAcquired(ctx, job.ID, streamclips.SourceProbe{Width: 1920, Height: 1080}, "sha", "Título de Twitch"); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Título de Twitch" {
		t.Fatalf("title = %q, want discovered Twitch title", got.Title)
	}
}

func TestSQLiteStreamRepoSeparatesAndClearsPrivateSourceURL(t *testing.T) {
	repo := newTestSQLiteStreamRepo(t)
	ctx := context.Background()
	const secret = "signed-private-value"
	job := &streamclips.Job{
		Status:     streamclips.StatusAcquiring,
		SourcePath: "streams/source.mp4",
		SourceURL:  "https://www.youtube.com/watch?v=abc123&token=" + secret,
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatal(err)
	}

	var private, public sql.NullString
	if err := repo.db.QueryRow(
		`SELECT source_url, public_source_url FROM stream_jobs WHERE id = ?`,
		job.ID.String(),
	).Scan(&private, &public); err != nil {
		t.Fatal(err)
	}
	if !private.Valid || !strings.Contains(private.String, secret) {
		t.Fatalf("private acquisition URL = %#v, want transient secret retained for worker", private)
	}
	if !public.Valid || strings.Contains(public.String, secret) || public.String != "https://www.youtube.com/watch?v=abc123" {
		t.Fatalf("public source URL = %#v, want redacted provider URL", public)
	}
	got, err := repo.Get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secret) {
		t.Fatalf("serialized job leaked private acquisition URL: %s", body)
	}

	if err := repo.SetAcquired(ctx, job.ID, streamclips.SourceProbe{Width: 1920}, "sha", "title"); err != nil {
		t.Fatal(err)
	}
	if err := repo.db.QueryRow(
		`SELECT source_url, public_source_url FROM stream_jobs WHERE id = ?`,
		job.ID.String(),
	).Scan(&private, &public); err != nil {
		t.Fatal(err)
	}
	if private.Valid {
		t.Fatalf("private acquisition URL survived successful download: %#v", private)
	}
	if !public.Valid || public.String != "https://www.youtube.com/watch?v=abc123" {
		t.Fatalf("public URL after acquisition = %#v", public)
	}
}

func TestSQLiteStreamRepoClearsPrivateSourceURLOnFailure(t *testing.T) {
	repo := newTestSQLiteStreamRepo(t)
	ctx := context.Background()
	job := &streamclips.Job{
		Status:     streamclips.StatusAcquiring,
		SourcePath: "streams/source.mp4",
		SourceURL:  "https://clips.twitch.tv/SomeSlug?token=private",
	}
	if err := repo.Create(ctx, job); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(ctx, job.ID, streamclips.StatusFailed, "download failed"); err != nil {
		t.Fatal(err)
	}
	var private sql.NullString
	if err := repo.db.QueryRow(`SELECT source_url FROM stream_jobs WHERE id = ?`, job.ID.String()).Scan(&private); err != nil {
		t.Fatal(err)
	}
	if private.Valid {
		t.Fatalf("private acquisition URL survived terminal failure: %#v", private)
	}
}

func TestSQLiteStreamRepoMigratesLegacySourceURLsWithoutRetainingSecrets(t *testing.T) {
	jobRepo, err := newSQLiteJobRepository(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = jobRepo.Close() })
	if _, err := jobRepo.db.Exec(`CREATE TABLE stream_jobs (
		id TEXT PRIMARY KEY, status TEXT NOT NULL, failure_reason TEXT,
		source_path TEXT NOT NULL, source_sha256 TEXT NOT NULL, source_url TEXT,
		title TEXT, probe TEXT NOT NULL, edit_plan TEXT,
		created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	completedID := uuid.New()
	rejectedID := uuid.New()
	if _, err := jobRepo.db.Exec(
		`INSERT INTO stream_jobs
		 (id,status,source_path,source_sha256,source_url,probe,created_at,updated_at)
		 VALUES (?,?,?,?,?,'{}',1,1), (?,?,?,?,?,'{}',1,1)`,
		completedID.String(), string(streamclips.StatusReady), "streams/a.mp4", "sha",
		"https://www.youtube.com/watch?v=abc123&token=legacy-secret",
		rejectedID.String(), string(streamclips.StatusAcquiring), "streams/b.mp4", "",
		"https://user:password@www.youtube.com/watch?v=abc123",
	); err != nil {
		t.Fatal(err)
	}
	if _, err := newSQLiteStreamJobRepository(jobRepo.db); err != nil {
		t.Fatal(err)
	}

	var private, public sql.NullString
	if err := jobRepo.db.QueryRow(
		`SELECT source_url, public_source_url FROM stream_jobs WHERE id = ?`,
		completedID.String(),
	).Scan(&private, &public); err != nil {
		t.Fatal(err)
	}
	if private.Valid || !public.Valid || public.String != "https://www.youtube.com/watch?v=abc123" {
		t.Fatalf("migrated completed source = private %#v public %#v", private, public)
	}

	var status string
	if err := jobRepo.db.QueryRow(
		`SELECT status, source_url, public_source_url FROM stream_jobs WHERE id = ?`,
		rejectedID.String(),
	).Scan(&status, &private, &public); err != nil {
		t.Fatal(err)
	}
	if status != string(streamclips.StatusFailed) || private.Valid || public.Valid {
		t.Fatalf("migrated rejected source = status %q private %#v public %#v", status, private, public)
	}
}

func TestSQLiteStreamRepoUpdateStatusUnknownJobReturnsNotFound(t *testing.T) {
	repo := newTestSQLiteStreamRepo(t)
	ctx := context.Background()

	if err := repo.UpdateStatus(ctx, uuid.New(), streamclips.StatusFailed, "nope"); !errors.Is(err, streamclips.ErrNotFound) {
		t.Fatalf("UpdateStatus unknown job: got %v, want ErrNotFound", err)
	}
}

func TestSQLiteStreamRepoSharesDBWithJobRepository(t *testing.T) {
	// Regression test for the desktop bug: the sqlite branch in main.go must
	// wire streamRepo from the same *sql.DB the job repository opened,
	// instead of leaving it nil. This exercises that construction path
	// directly rather than through main().
	dbPath := filepath.Join(t.TempDir(), "jobs.db")
	jobRepo, err := newSQLiteJobRepository(dbPath)
	if err != nil {
		t.Fatalf("newSQLiteJobRepository: %v", err)
	}
	defer func() { _ = jobRepo.Close() }()

	streamRepo, err := newSQLiteStreamJobRepository(jobRepo.db)
	if err != nil {
		t.Fatalf("newSQLiteStreamJobRepository: %v", err)
	}
	if streamRepo.db != jobRepo.db {
		t.Fatal("stream repo does not share the job repository's *sql.DB")
	}
}
