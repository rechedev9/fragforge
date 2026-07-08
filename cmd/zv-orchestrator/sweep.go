package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/obs"
)

// interruptedClass is the stable obs error class for jobs failed by the startup
// sweep, so they group together in the journal and metrics.
const interruptedClass = "interrupted"

// interruptedStages maps each transient in-flight job status - a status that
// means a worker was actively running when the process died - to the obs stage
// label the interruption is recorded under. Stable in-between states (queued,
// scanned, parsed, recorded, composed) and terminal states (done, failed) are
// intentionally absent: nothing is mid-flight there, so a restart is not a
// failure for them.
var interruptedStages = map[job.Status]string{
	job.StatusScanning:  obs.StageParse, // roster scan runs in the parser worker
	job.StatusParsing:   obs.StageParse,
	job.StatusRecording: obs.StageRecord,
	job.StatusComposing: obs.StageCompose,
}

// interruptSweeper is the slice of the job repository the startup sweep needs.
type interruptSweeper interface {
	ListByStatus(context.Context, job.Status) ([]job.Job, error)
	UpdateStatus(context.Context, uuid.UUID, job.Status, string) error
}

// sweepInterruptedJobs fails every job left in a transient in-flight status by a
// previous process that crashed or was quit mid-stage. Without it, such a job
// shows as forever "recording"/"composing" in the UI (observed: a job sat at
// "recording" for 5 days after the app was quit mid-capture). It runs once at
// startup, after the repo is ready and before serving traffic, and returns the
// number of jobs it failed.
//
// Queue nuance: with the inline queue nothing survives a restart, so failing is
// always correct. With asynq a redelivered task will re-drive the status when it
// actually runs, so failing here first is still safe - the job simply moves
// failed -> parsing/... again once the task executes.
//
// The obs recording is best-effort: a recorder error must not stop the sweep or
// block startup, so it is logged-and-ignored, not returned.
func sweepInterruptedJobs(ctx context.Context, repo interruptSweeper, rec *obs.Recorder) (int, error) {
	swept := 0
	// Order the statuses for deterministic logs and tests.
	for _, status := range []job.Status{
		job.StatusScanning,
		job.StatusParsing,
		job.StatusRecording,
		job.StatusComposing,
	} {
		jobs, err := repo.ListByStatus(ctx, status)
		if err != nil {
			return swept, fmt.Errorf("list %s jobs: %w", status, err)
		}
		for _, j := range jobs {
			reason := fmt.Sprintf("interrupted: the orchestrator restarted mid-%s", status)
			if err := repo.UpdateStatus(ctx, j.ID, job.StatusFailed, reason); err != nil {
				return swept, fmt.Errorf("fail interrupted %s job %s: %w", status, j.ID, err)
			}
			swept++
			if rec != nil {
				_ = rec.RecordError(obs.Event{
					Stage:   interruptedStages[status],
					Class:   interruptedClass,
					Message: reason,
					Demo:    j.DemoPath,
					Target:  j.TargetSteamID,
				})
			}
		}
	}
	return swept, nil
}
