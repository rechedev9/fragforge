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
	var errs []error
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
			errs = append(errs, fmt.Errorf("list %s jobs: %w", status, err))
			continue
		}
		sort.Slice(jobs, func(i, k int) bool { return jobs[i].ID.String() < jobs[k].ID.String() })
		for _, j := range jobs {
			reason := interruptedDemoJobReason(status)
			if err := repo.UpdateStatus(ctx, j.ID, job.StatusFailed, reason); err != nil {
				errs = append(errs, fmt.Errorf("fail interrupted %s job %s: %w", status, j.ID, err))
				continue
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
	return swept, errors.Join(errs...)
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
func sweepInterruptedDemoRenderStates(ctx context.Context, repo interruptSweeper, store storage.Storage, rec *obs.Recorder) (int, error) {
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
			found, readErr := readSweepJSON(store, key, &state)
			if !found {
				if readErr != nil {
					errs = append(errs, fmt.Errorf("open demo render state %s: %w", key, readErr))
				}
				continue
			}
			if readErr == nil && state.Status != renderplan.RenderVariantStatusQueued && state.Status != renderplan.RenderVariantStatusRendering {
				continue
			}

			if readErr != nil || !validDemoRenderStateIdentity(state, j.ID, loadout.Variant) {
				state, err = renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
					JobID:   j.ID,
					Loadout: loadout,
					Status:  renderplan.RenderVariantStatusFailed,
					Error:   interruptedDemoRenderReason,
					Now:     now,
				})
				if err != nil {
					errs = append(errs, fmt.Errorf("build failed demo render state for job %s variant %s: %w", j.ID, loadout.Variant, err))
					continue
				}
			} else {
				state.Status = renderplan.RenderVariantStatusFailed
				state.Error = interruptedDemoRenderReason
				state.UpdatedAt = now
			}
			if err := writeSweepJSON(store, key, state); err != nil {
				writeErr := fmt.Errorf("write failed demo render state %s: %w", key, err)
				if readErr != nil {
					writeErr = errors.Join(fmt.Errorf("decode demo render state %s: %w", key, readErr), writeErr)
				}
				errs = append(errs, writeErr)
				continue
			}
			swept++
			recordInterruptedRender(rec, j.DemoPath, j.TargetSteamID, interruptedDemoRenderReason)
		}
	}
	return swept, errors.Join(errs...)
}

func validDemoRenderStateIdentity(state renderplan.RenderVariantState, jobID uuid.UUID, variant string) bool {
	return state.JobID == jobID && state.Variant == variant && state.SchemaVersion != "" && !state.CreatedAt.IsZero()
}

// sweepInterruptedGenerateRuns fails guided work whose intent still owns an
// active run, then retires the marker so an explicit retry can be admitted.
// Already-failed jobs and stale markers on other terminal states only need the
// marker repair. Malformed artifacts are replaced with a valid idle intent.
func sweepInterruptedGenerateRuns(ctx context.Context, repo interruptSweeper, store storage.Storage, rec *obs.Recorder) (int, error) {
	jobs, listErr := listAllDemoJobs(ctx, repo)
	var errs []error
	if listErr != nil {
		errs = append(errs, listErr)
	}

	swept := 0
	for _, j := range jobs {
		var intent renderplan.GenerateIntent
		found, readErr := readSweepJSON(store, artifacts.GenerateIntentKey(j.ID), &intent)
		if !found {
			if readErr != nil {
				errs = append(errs, fmt.Errorf("open generate intent for job %s: %w", j.ID, readErr))
			}
			continue
		}
		intent = intent.Normalize()
		intentErr := intent.Validate()
		if readErr == nil && intentErr == nil && intent.ActiveRunID == uuid.Nil {
			continue
		}

		activeEvidence := readErr != nil || intent.ActiveRunID != uuid.Nil
		failJob := activeEvidence && (j.Status == job.StatusParsed || j.Status == job.StatusRecorded || j.Status == job.StatusRecording)
		if failJob {
			if err := repo.UpdateStatus(ctx, j.ID, job.StatusFailed, interruptedGenerateReason); err != nil {
				errs = append(errs, fmt.Errorf("fail interrupted generate job %s: %w", j.ID, err))
				// Keep the marker when the terminal status did not persist. It is
				// the next startup's evidence that process-local work was lost.
				continue
			}
		}

		if readErr != nil || intentErr != nil {
			idle, idleErr := idleGenerateIntent()
			if idleErr != nil {
				errs = append(errs, fmt.Errorf("build idle generate intent for job %s: %w", j.ID, idleErr))
				continue
			}
			intent = idle
		} else {
			intent.ActiveRunID = uuid.Nil
		}
		key := artifacts.GenerateIntentKey(j.ID)
		if err := writeSweepJSON(store, key, intent); err != nil {
			repairErr := fmt.Errorf("write idle generate intent for job %s: %w", j.ID, err)
			if readErr != nil {
				repairErr = errors.Join(fmt.Errorf("decode generate intent for job %s: %w", j.ID, readErr), repairErr)
			} else if intentErr != nil {
				repairErr = errors.Join(fmt.Errorf("validate generate intent for job %s: %w", j.ID, intentErr), repairErr)
			}
			errs = append(errs, repairErr)
			continue
		}

		swept++
		if failJob && rec != nil {
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

func idleGenerateIntent() (renderplan.GenerateIntent, error) {
	loadouts := renderplan.LoadoutCatalog()
	if len(loadouts) == 0 {
		return renderplan.GenerateIntent{}, fmt.Errorf("render loadout catalog is empty")
	}
	return renderplan.GenerateIntent{
		Variant: loadouts[0].Variant,
		Edit:    renderplan.DefaultEditRequest(),
	}, nil
}

// sweepInterruptedStreamJobs fails accepted acquisition and render tasks that
// cannot survive the desktop process stopping. Uploaded, ready, rendered, and
// already-failed jobs remain untouched.
func sweepInterruptedStreamJobs(ctx context.Context, repo streamInterruptSweeper, rec *obs.Recorder) (int, error) {
	return sweepInterruptedStreamJobsAfterRenderStates(ctx, repo, rec, streamRenderSweepResult{auditComplete: true})
}

func sweepInterruptedStreamJobsAfterRenderStates(
	ctx context.Context,
	repo streamInterruptSweeper,
	rec *obs.Recorder,
	renderStates streamRenderSweepResult,
) (int, error) {
	swept := 0
	var errs []error
	statuses := []streamclips.Status{streamclips.StatusAcquiring}
	if renderStates.auditComplete {
		statuses = append(statuses, streamclips.StatusRendering)
	}
	for _, status := range statuses {
		jobs, err := repo.ListByStatus(ctx, status)
		if err != nil {
			errs = append(errs, fmt.Errorf("list %s stream jobs: %w", status, err))
			continue
		}
		sort.Slice(jobs, func(i, k int) bool { return jobs[i].ID.String() < jobs[k].ID.String() })
		for _, j := range jobs {
			if status == streamclips.StatusRendering && renderStates.completed(j.ID) {
				// A rendered state is written only after the result, gallery, and
				// videos are durable. If promoting the parent failed, preserve this
				// authoritative completion and let fatal startup reconciliation retry.
				continue
			}
			reason, stage := interruptedStreamJob(status)
			if err := repo.UpdateStatus(ctx, j.ID, streamclips.StatusFailed, reason); err != nil {
				errs = append(errs, fmt.Errorf("fail interrupted %s stream job %s: %w", status, j.ID, err))
				continue
			}
			swept++
			if rec != nil && !(status == streamclips.StatusRendering && renderStates.interrupted(j.ID)) {
				_ = rec.RecordError(obs.Event{
					Stage:   stage,
					Class:   interruptedClass,
					Message: reason,
					Demo:    j.SourcePath,
				})
			}
		}
	}
	return swept, errors.Join(errs...)
}

func interruptedStreamJob(status streamclips.Status) (reason, stage string) {
	if status == streamclips.StatusAcquiring {
		return interruptedStreamAcquire, obs.StageStreamAcquire
	}
	return interruptedStreamRender, obs.StageRender
}

type streamRenderSweepResult struct {
	Reconciled      int
	auditComplete   bool
	completedJobs   map[uuid.UUID]struct{}
	interruptedJobs map[uuid.UUID]struct{}
}

func (r streamRenderSweepResult) completed(id uuid.UUID) bool {
	_, ok := r.completedJobs[id]
	return ok
}

func (r streamRenderSweepResult) interrupted(id uuid.UUID) bool {
	_, ok := r.interruptedJobs[id]
	return ok
}

// sweepInterruptedStreamRenderStates fails rendering state documents for every
// known stream job, independent of the parent job's current status. Existing
// warnings, video entries, artifact keys, and other state data are preserved.
func sweepInterruptedStreamRenderStates(ctx context.Context, repo streamInterruptSweeper, store storage.Storage, rec *obs.Recorder) (int, error) {
	result, err := reconcileInterruptedStreamRenderStates(ctx, repo, store, rec)
	return result.Reconciled, err
}

func reconcileInterruptedStreamRenderStates(
	ctx context.Context,
	repo streamInterruptSweeper,
	store storage.Storage,
	rec *obs.Recorder,
) (streamRenderSweepResult, error) {
	jobs, listErr := listAllStreamJobs(ctx, repo)
	var errs []error
	if listErr != nil {
		errs = append(errs, listErr)
	}
	result := streamRenderSweepResult{
		completedJobs:   make(map[uuid.UUID]struct{}),
		interruptedJobs: make(map[uuid.UUID]struct{}),
	}
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
			found, readErr := readSweepJSON(store, key, &state)
			if !found {
				if readErr != nil {
					errs = append(errs, fmt.Errorf("open stream render state %s: %w", key, readErr))
				}
				continue
			}

			identityValid := readErr == nil && validStreamRenderStateIdentity(state, j.ID, variant)
			statusValid := false
			if readErr == nil {
				_, statusErr := streamclips.ParseStatus(string(state.Status))
				statusValid = statusErr == nil
			}
			if identityValid && statusValid {
				switch state.Status {
				case streamclips.StatusRendered:
					result.completedJobs[j.ID] = struct{}{}
					continue
				case streamclips.StatusRendering:
					// Repaired below.
				default:
					continue
				}
			}
			result.interruptedJobs[j.ID] = struct{}{}

			if !identityValid || !statusValid {
				state, err = streamclips.NewRenderState(j.ID, variant, streamclips.StatusFailed, nil, interruptedStreamRender, nil)
				if err != nil {
					errs = append(errs, fmt.Errorf("build failed stream render state for job %s variant %s: %w", j.ID, variant, err))
					continue
				}
				state.UpdatedAt = now
			} else {
				state.Status = streamclips.StatusFailed
				state.Error = interruptedStreamRender
				state.UpdatedAt = now
			}
			if err := writeSweepJSON(store, key, state); err != nil {
				writeErr := fmt.Errorf("write failed stream render state %s: %w", key, err)
				if readErr != nil {
					writeErr = errors.Join(fmt.Errorf("decode stream render state %s: %w", key, readErr), writeErr)
				}
				errs = append(errs, writeErr)
				continue
			}
			result.Reconciled++
			recordInterruptedRender(rec, j.SourcePath, "", interruptedStreamRender)
		}
		if j.Status == streamclips.StatusRendering && result.completed(j.ID) {
			if err := repo.UpdateStatus(ctx, j.ID, streamclips.StatusRendered, ""); err != nil {
				errs = append(errs, fmt.Errorf("promote completed stream render job %s: %w", j.ID, err))
				continue
			}
			result.Reconciled++
		}
	}
	err := errors.Join(errs...)
	result.auditComplete = err == nil
	return result, err
}

func validStreamRenderStateIdentity(state streamclips.RenderState, jobID uuid.UUID, variant string) bool {
	expected, err := streamclips.NewRenderState(jobID, variant, streamclips.StatusReady, nil, "", nil)
	if err != nil {
		return false
	}
	return state.JobID == jobID &&
		state.Variant == variant &&
		state.ResultKey == expected.ResultKey &&
		state.GalleryKey == expected.GalleryKey &&
		state.ArtifactDir == expected.ArtifactDir
}

func recordInterruptedRender(rec *obs.Recorder, source, target, message string) {
	if rec == nil {
		return
	}
	_ = rec.RecordError(obs.Event{
		Stage:   obs.StageRender,
		Class:   interruptedClass,
		Message: message,
		Demo:    source,
		Target:  target,
	})
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
		return true, err
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
