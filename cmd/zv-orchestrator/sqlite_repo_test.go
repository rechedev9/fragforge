package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/rules"
)

func newTestSQLiteRepo(t *testing.T) *sqliteJobRepository {
	t.Helper()
	repo, err := newSQLiteJobRepository(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatalf("newSQLiteJobRepository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func TestSQLiteRepoCreateAndGet(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	j := &job.Job{Status: job.StatusScanned, DemoPath: "m.dem", DemoSHA256: "abc"}
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
	if got.ID != j.ID || got.DemoPath != "m.dem" || got.Status != job.StatusScanned {
		t.Fatalf("Get: got %+v, want id=%s demo=m.dem status=scanned", got, j.ID)
	}

	if _, err := repo.Get(ctx, uuid.New()); !errors.Is(err, job.ErrNotFound) {
		t.Fatalf("Get unknown: got %v, want ErrNotFound", err)
	}
}

func TestSQLiteRepoGetMetaAndListStripKillPlan(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	j := &job.Job{Status: job.StatusScanned}
	if err := repo.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.SetKillPlan(ctx, j.ID, killplan.Plan{}); err != nil {
		t.Fatalf("SetKillPlan: %v", err)
	}

	full, err := repo.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if full.KillPlan == nil {
		t.Fatal("Get: want KillPlan set, got nil")
	}

	meta, err := repo.GetMeta(ctx, j.ID)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if meta.KillPlan != nil {
		t.Fatal("GetMeta: want KillPlan nil, got non-nil")
	}

	list, err := repo.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List: got %d jobs, want 1", len(list))
	}
	if list[0].KillPlan != nil {
		t.Fatal("List: want KillPlan nil, got non-nil")
	}
}

func TestSQLiteRepoListOrdersByUpdatedThenLimits(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	var ids []uuid.UUID
	for range 3 {
		j := &job.Job{Status: job.StatusScanned}
		if err := repo.Create(ctx, j); err != nil {
			t.Fatalf("Create: %v", err)
		}
		ids = append(ids, j.ID)
		time.Sleep(2 * time.Millisecond) // distinct created/updated timestamps
	}
	// Touch the first-created job so it becomes the most-recently-updated.
	if err := repo.UpdateStatus(ctx, ids[0], job.StatusParsing, ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	list, err := repo.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List: got %d, want 3", len(list))
	}
	if list[0].ID != ids[0] {
		t.Fatalf("List order: got head %s, want the just-updated %s", list[0].ID, ids[0])
	}

	limited, err := repo.List(ctx, 2)
	if err != nil {
		t.Fatalf("List limited: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("List limit: got %d, want 2", len(limited))
	}
}

func TestSQLiteRepoUpdateStatus(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	j := &job.Job{Status: job.StatusRecording}
	if err := repo.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpdateStatus(ctx, j.ID, job.StatusFailed, "boom"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, err := repo.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != job.StatusFailed || got.FailureReason != "boom" {
		t.Fatalf("UpdateStatus: got status=%s reason=%q, want failed/boom", got.Status, got.FailureReason)
	}
	if err := repo.UpdateStatus(ctx, uuid.New(), job.StatusDone, ""); !errors.Is(err, job.ErrNotFound) {
		t.Fatalf("UpdateStatus unknown: got %v, want ErrNotFound", err)
	}
}

func TestSQLiteRepoSetParseInputs(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ctx := context.Background()

	// scanned -> parsing is allowed and records the target.
	scanned := &job.Job{Status: job.StatusScanned}
	if err := repo.Create(ctx, scanned); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.SetParseInputs(ctx, scanned.ID, "76561199237188983", rules.Rules{}); err != nil {
		t.Fatalf("SetParseInputs: %v", err)
	}
	got, err := repo.Get(ctx, scanned.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != job.StatusParsing || got.TargetSteamID != "76561199237188983" {
		t.Fatalf("SetParseInputs: got status=%s target=%q, want parsing/76561199237188983", got.Status, got.TargetSteamID)
	}

	// A job that was never scanned is a conflict, not a silent success.
	queued := &job.Job{Status: job.StatusQueued}
	if err := repo.Create(ctx, queued); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.SetParseInputs(ctx, queued.ID, "1", rules.Rules{}); !errors.Is(err, job.ErrConflict) {
		t.Fatalf("SetParseInputs wrong state: got %v, want ErrConflict", err)
	}

	if err := repo.SetParseInputs(ctx, uuid.New(), "1", rules.Rules{}); !errors.Is(err, job.ErrNotFound) {
		t.Fatalf("SetParseInputs unknown: got %v, want ErrNotFound", err)
	}
}

// The whole point of the SQLite repo: job state outlives the process. Create a
// job, close the database, reopen the same file, and the job is still there.
func TestSQLiteRepoPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "jobs.db")

	repo, err := newSQLiteJobRepository(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	j := &job.Job{Status: job.StatusParsed, DemoPath: "keep.dem", TargetSteamID: "76561199237188983"}
	if err := repo.Create(ctx, j); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.SetKillPlan(ctx, j.ID, killplan.Plan{}); err != nil {
		t.Fatalf("SetKillPlan: %v", err)
	}
	if err := repo.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := newSQLiteJobRepository(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	got, err := reopened.Get(ctx, j.ID)
	if err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
	if got.DemoPath != "keep.dem" || got.TargetSteamID != "76561199237188983" || got.Status != job.StatusParsed {
		t.Fatalf("after reopen: got %+v, want demo=keep.dem status=parsed", got)
	}
	if got.KillPlan == nil {
		t.Fatal("after reopen: want KillPlan persisted, got nil")
	}
}
