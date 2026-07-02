package main

import (
	"context"
	"errors"
	"path/filepath"
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
	if err := repo.SetAcquired(ctx, j.ID, probe, "def456"); err != nil {
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
