package main

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/obs"
	"github.com/rechedev9/fragforge/internal/rules"
)

// seedJob inserts a job in the given status and returns its id.
func seedJob(t *testing.T, repo interruptSweeperRepo, status job.Status) job.Job {
	t.Helper()
	j := &job.Job{
		Status:        job.StatusQueued,
		DemoPath:      "demos/" + status.String() + ".dem",
		DemoSHA256:    "sha-" + status.String(),
		TargetSteamID: "76561198000000000",
		Rules:         rules.Default(),
	}
	if err := repo.Create(context.Background(), j); err != nil {
		t.Fatalf("Create(%s): %v", status, err)
	}
	if status != job.StatusQueued {
		if err := repo.UpdateStatus(context.Background(), j.ID, status, ""); err != nil {
			t.Fatalf("UpdateStatus(%s): %v", status, err)
		}
	}
	return *j
}

// interruptSweeperRepo is the repo surface the sweep test drives: enough to seed
// jobs and read them back. Both the memory and sqlite repos satisfy it.
type interruptSweeperRepo interface {
	interruptSweeper
	Create(context.Context, *job.Job) error
	Get(context.Context, uuid.UUID) (job.Job, error)
}

func TestSweepInterruptedJobsFailsOnlyTransientStates(t *testing.T) {
	repos := map[string]interruptSweeperRepo{
		"memory": newMemoryJobRepository(),
		"sqlite": newTestSQLiteRepo(t),
	}

	transient := []job.Status{
		job.StatusScanning,
		job.StatusParsing,
		job.StatusRecording,
		job.StatusComposing,
	}
	// Stable and terminal statuses that a restart must never touch.
	untouched := []job.Status{
		job.StatusQueued,
		job.StatusScanned,
		job.StatusParsed,
		job.StatusRecorded,
		job.StatusComposed,
		job.StatusDone,
		job.StatusFailed,
	}

	for name, repo := range repos {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			transientIDs := map[job.Status]job.Job{}
			for _, s := range transient {
				transientIDs[s] = seedJob(t, repo, s)
			}
			untouchedIDs := map[job.Status]job.Job{}
			for _, s := range untouched {
				untouchedIDs[s] = seedJob(t, repo, s)
			}

			rec, err := obs.New(t.TempDir())
			if err != nil {
				t.Fatalf("obs.New: %v", err)
			}

			swept, err := sweepInterruptedJobs(ctx, repo, rec)
			if err != nil {
				t.Fatalf("sweepInterruptedJobs: %v", err)
			}
			if swept != len(transient) {
				t.Errorf("swept = %d, want %d", swept, len(transient))
			}

			for s, seeded := range transientIDs {
				got, err := repo.Get(ctx, seeded.ID)
				if err != nil {
					t.Fatalf("Get(%s): %v", s, err)
				}
				if got.Status != job.StatusFailed {
					t.Errorf("status after sweep for %s = %s, want failed", s, got.Status)
				}
				if !strings.Contains(got.FailureReason, "interrupted") ||
					!strings.Contains(got.FailureReason, "mid-"+s.String()) {
					t.Errorf("failure reason for %s = %q, want interrupted mid-%s", s, got.FailureReason, s)
				}
			}

			for s, seeded := range untouchedIDs {
				got, err := repo.Get(ctx, seeded.ID)
				if err != nil {
					t.Fatalf("Get(%s): %v", s, err)
				}
				if got.Status != s {
					t.Errorf("status for %s changed to %s, want untouched", s, got.Status)
				}
			}

			// Each swept failure is recorded once through obs (class=interrupted).
			var interruptErrors int64
			for _, m := range rec.Snapshot() {
				if m.Name == "fragforge_errors_total" && m.Labels["class"] == interruptedClass {
					interruptErrors += m.Value
				}
			}
			if interruptErrors != int64(len(transient)) {
				t.Errorf("obs interrupted errors = %d, want %d", interruptErrors, len(transient))
			}
		})
	}
}
