package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/obs"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

// interruptedClass is the stable obs error class for jobs failed by the startup
// sweep, so they group together in the journal and metrics.
const interruptedClass = "interrupted"

const (
	interruptedQueuedJobReason  = "interrupted: the orchestrator restarted before queued work started"
	interruptedDemoRenderReason = "interrupted: the orchestrator restarted before render completed"
	interruptedGenerateReason   = "interrupted: the orchestrator restarted before guided generation reached render handoff"
	interruptedStreamAcquire    = "interrupted: the orchestrator restarted before stream acquisition completed"
	interruptedStreamRender     = "interrupted: the orchestrator restarted before stream render completed"
)

// interruptedStages maps each demo job status whose inline work cannot survive
// a process restart to the obs stage label the interruption is recorded under.
var interruptedStages = map[job.Status]string{
	job.StatusQueued:    obs.StageParse,
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

// streamInterruptSweeper is the uncapped repository surface required to find
// stream work stranded by a process restart. The ordinary List method remains
// capped for the HTTP API; startup reconciliation must inspect every record.
type streamInterruptSweeper interface {
	ListByStatus(context.Context, streamclips.Status) ([]streamclips.Job, error)
	UpdateStatus(context.Context, uuid.UUID, streamclips.Status, string) error
}

// sweepInterruptedJobs fails every demo job whose accepted inline task was
// lost when the previous process stopped. This includes queued work: unlike a
// durable broker, the desktop's in-process queue cannot resume it after a
// restart. It runs once after the repo is ready and before serving traffic.
//
// The obs recording is best-effort: a recorder error must not stop the sweep or
// block startup, so it is logged-and-ignored, not returned.
func sweepInterruptedJobs(ctx context.Context, repo interruptSweeper, rec *obs.Recorder) (int, error) {
	swept := 0
	// Order the statuses for deterministic logs and tests.
	for _, status := range []job.Status{
		job.StatusQueued,
		job.StatusScanning,
		job.StatusParsing,
		job.StatusRecording,
		job.StatusComposing,
	} {
		jobs, err := repo.ListByStatus(ctx, status)
		if err != nil {
			return swept, fmt.Errorf("list %s jobs: %w", status, err)
		}
		sort.Slice(jobs, func(i, k int) bool { return jobs[i].ID.String() < jobs[k].ID.String() })
		for _, j := range jobs {
			reason := interruptedDemoJobReason(status)
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

func interruptedDemoJobReason(status job.Status) string {
	if status == job.StatusQueued {
		return interruptedQueuedJobReason
	}
	return fmt.Sprintf("interrupted: the orchestrator restarted mid-%s", status)
}

// sweepInterruptedDemoRenderStates fails queued or rendering variant state
// documents left by the previous process. Parent job status is deliberately
// irrelevant: render variants have their own lifecycle and a stale render can
// belong to a recorded, composed, done, or failed job.
func sweepInterruptedDemoRenderStates(ctx context.Context, repo interruptSweeper, store storage.Storage) (int, error) {
	jobs, listErr := listAllDemoJobs(ctx, repo)
	var errs []error
	if listErr != nil {
		errs = append(errs, listErr)
	}
	swept := 0
	now := time.Now().UTC()
	for _, j := range jobs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		for _, loadout := range renderplan.LoadoutCatalog() {
			key, err := renderplan.RenderVariantStateKey(j.ID, loadout.Variant)
			if err != nil {
				errs = append(errs, fmt.Errorf("resolve demo render state for job %s variant %s: %w", j.ID, loadout.Variant, err))
				continue
			}
			var state renderplan.RenderVariantState
			found, err := readSweepJSON(store, key, &state)
			if err != nil {
				errs = append(errs, fmt.Errorf("read demo render state %s: %w", key, err))
				continue
			}
			if !found || (state.Status != renderplan.RenderVariantStatusQueued && state.Status != renderplan.RenderVariantStatusRendering) {
				continue
			}
			state.Status = renderplan.RenderVariantStatusFailed
			state.Error = interruptedDemoRenderReason
			state.UpdatedAt = now
			if err := writeSweepJSON(store, key, state); err != nil {
				errs = append(errs, fmt.Errorf("write demo render state %s: %w", key, err))
				continue
			}
			swept++
		}
	}
	return swept, errors.Join(errs...)
}

// sweepInterruptedGenerateRuns fails a guided request whose intent still owns
// an active run id. The record worker clears that marker atomically with render
// admission, so unlike the display fields in the same artifact it unambiguously
// identifies accepted process-local work that did not survive the restart.
func sweepInterruptedGenerateRuns(ctx context.Context, repo interruptSweeper, store storage.Storage, rec *obs.Recorder) (int, error) {
	var jobs []job.Job
	var errs []error
	for _, status := range []job.Status{job.StatusParsed, job.StatusRecorded} {
		found, err := repo.ListByStatus(ctx, status)
		if err != nil {
			errs = append(errs, fmt.Errorf("list %s jobs for generate intent sweep: %w", status, err))
			continue
		}
		jobs = append(jobs, found...)
	}
	sort.Slice(jobs, func(i, k int) bool { return jobs[i].ID.String() < jobs[k].ID.String() })

	swept := 0
	for _, j := range jobs {
		var intent renderplan.GenerateIntent
		found, err := readSweepJSON(store, artifacts.GenerateIntentKey(j.ID), &intent)
		if err != nil {
			errs = append(errs, fmt.Errorf("read generate intent for job %s: %w", j.ID, err))
			continue
		}
		if !found {
			continue
		}
		if intent.ActiveRunID == uuid.Nil {
			continue
		}
		if err := repo.UpdateStatus(ctx, j.ID, job.StatusFailed, interruptedGenerateReason); err != nil {
			errs = append(errs, fmt.Errorf("fail interrupted generate job %s: %w", j.ID, err))
			continue
		}
		swept++
		if rec != nil {
			_ = rec.RecordError(obs.Event{
				Stage:   obs.StageRecord,
				Class:   interruptedClass,
				Message: interruptedGenerateReason,
				Demo:    j.DemoPath,
				Target:  j.TargetSteamID,
			})
		}
	}
	return swept, errors.Join(errs...)
}

// sweepInterruptedStreamJobs fails accepted acquisition and render tasks that
// cannot survive the desktop process stopping. Uploaded, ready, rendered, and
// already-failed jobs remain untouched.
func sweepInterruptedStreamJobs(ctx context.Context, repo streamInterruptSweeper, rec *obs.Recorder) (int, error) {
	swept := 0
	for _, status := range []streamclips.Status{streamclips.StatusAcquiring, streamclips.StatusRendering} {
		jobs, err := repo.ListByStatus(ctx, status)
		if err != nil {
			return swept, fmt.Errorf("list %s stream jobs: %w", status, err)
		}
		sort.Slice(jobs, func(i, k int) bool { return jobs[i].ID.String() < jobs[k].ID.String() })
		for _, j := range jobs {
			reason, stage := interruptedStreamJob(status)
			if err := repo.UpdateStatus(ctx, j.ID, streamclips.StatusFailed, reason); err != nil {
				return swept, fmt.Errorf("fail interrupted %s stream job %s: %w", status, j.ID, err)
			}
			swept++
			if rec != nil {
				_ = rec.RecordError(obs.Event{
					Stage:   stage,
					Class:   interruptedClass,
					Message: reason,
					Demo:    j.SourcePath,
				})
			}
		}
	}
	return swept, nil
}

func interruptedStreamJob(status streamclips.Status) (reason, stage string) {
	if status == streamclips.StatusAcquiring {
		return interruptedStreamAcquire, obs.StageStreamAcquire
	}
	return interruptedStreamRender, obs.StageRender
}

// sweepInterruptedStreamRenderStates fails rendering state documents for every
// known stream job, independent of the parent job's current status. Existing
// warnings, video entries, artifact keys, and other state data are preserved.
func sweepInterruptedStreamRenderStates(ctx context.Context, repo streamInterruptSweeper, store storage.Storage) (int, error) {
	jobs, listErr := listAllStreamJobs(ctx, repo)
	var errs []error
	if listErr != nil {
		errs = append(errs, listErr)
	}
	swept := 0
	now := time.Now().UTC()
	for _, j := range jobs {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		for _, variant := range streamclips.VariantNames() {
			key, err := streamclips.RenderStateKey(j.ID, variant)
			if err != nil {
				errs = append(errs, fmt.Errorf("resolve stream render state for job %s variant %s: %w", j.ID, variant, err))
				continue
			}
			var state streamclips.RenderState
			found, err := readSweepJSON(store, key, &state)
			if err != nil {
				errs = append(errs, fmt.Errorf("read stream render state %s: %w", key, err))
				continue
			}
			if !found || state.Status != streamclips.StatusRendering {
				continue
			}
			state.Status = streamclips.StatusFailed
			state.Error = interruptedStreamRender
			state.UpdatedAt = now
			if err := writeSweepJSON(store, key, state); err != nil {
				errs = append(errs, fmt.Errorf("write stream render state %s: %w", key, err))
				continue
			}
			swept++
		}
	}
	return swept, errors.Join(errs...)
}

func listAllDemoJobs(ctx context.Context, repo interruptSweeper) ([]job.Job, error) {
	var jobs []job.Job
	var errs []error
	for _, status := range []job.Status{
		job.StatusQueued,
		job.StatusScanning,
		job.StatusScanned,
		job.StatusParsing,
		job.StatusParsed,
		job.StatusRecording,
		job.StatusRecorded,
		job.StatusComposing,
		job.StatusComposed,
		job.StatusDone,
		job.StatusFailed,
	} {
		found, err := repo.ListByStatus(ctx, status)
		if err != nil {
			errs = append(errs, fmt.Errorf("list %s jobs for render sweep: %w", status, err))
			continue
		}
		jobs = append(jobs, found...)
	}
	sort.Slice(jobs, func(i, k int) bool { return jobs[i].ID.String() < jobs[k].ID.String() })
	return jobs, errors.Join(errs...)
}

func listAllStreamJobs(ctx context.Context, repo streamInterruptSweeper) ([]streamclips.Job, error) {
	var jobs []streamclips.Job
	var errs []error
	for _, status := range []streamclips.Status{
		streamclips.StatusAcquiring,
		streamclips.StatusUploaded,
		streamclips.StatusReady,
		streamclips.StatusRendering,
		streamclips.StatusRendered,
		streamclips.StatusFailed,
	} {
		found, err := repo.ListByStatus(ctx, status)
		if err != nil {
			errs = append(errs, fmt.Errorf("list %s stream jobs for render sweep: %w", status, err))
			continue
		}
		jobs = append(jobs, found...)
	}
	sort.Slice(jobs, func(i, k int) bool { return jobs[i].ID.String() < jobs[k].ID.String() })
	return jobs, errors.Join(errs...)
}

func readSweepJSON(store storage.Storage, key string, dst any) (bool, error) {
	rc, err := store.Open(key)
	if err != nil {
		if storage.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer rc.Close()
	if err := json.NewDecoder(rc).Decode(dst); err != nil {
		return false, err
	}
	return true, nil
}

func writeSweepJSON(store storage.Storage, key string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return store.Put(key, bytes.NewReader(append(b, '\n')))
}
