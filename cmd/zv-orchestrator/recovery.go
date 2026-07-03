package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/httpapi"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/tasks"
)

// recoverInterruptedJobs re-drives work a previous process left mid-stage.
// Inline-mode tasks live only in the dead process's memory, so after a crash
// or restart a job could sit in scanning/parsing/recording/composing forever:
// no worker will ever pick it up again, and the web reconcile loop treats
// those states as "in progress, wait" and never re-drives them either.
//
// Recovery policy, by how expensive a re-run is:
//
//   - scanning/parsing: pure, cheap, inputs durable (demo blob + parse inputs
//     on the job row) - re-enqueue the lost task.
//   - recording: a capture costs minutes and a GPU, so it is never relaunched
//     unattended (same rule as retries). Roll back to parsed; already-captured
//     segment clips are durable per-segment artifacts, so the next record
//     request skips them and only captures what is missing.
//   - composing: roll back to recorded so the client can restart composition.
//   - render variant states stuck queued/rendering while the job itself is
//     durable: re-enqueue the render when the job's generate intent can
//     reconstruct the task; otherwise mark the state failed with a clear
//     reason so the UI offers a retry instead of an infinite spinner.
//
// Only called in inline queue mode: with Redis, pending tasks survive the
// restart in Redis itself and would double-run if recovered here too.
func recoverInterruptedJobs(ctx context.Context, repo orchestratorJobRepository, store storage.Storage, queue httpapi.Enqueuer) {
	jobs, err := repo.List(ctx, 100)
	if err != nil {
		log.Printf("recovery: list jobs: %v", err)
		return
	}
	for _, j := range jobs {
		switch j.Status {
		case job.StatusScanning:
			requeueTask(queue, j, tasks.NewScanRosterTask)
		case job.StatusParsing:
			requeueTask(queue, j, tasks.NewParseDemoTask)
		case job.StatusRecording:
			rollBack(ctx, repo, j, job.StatusParsed)
		case job.StatusComposing:
			rollBack(ctx, repo, j, job.StatusRecorded)
		case job.StatusRecorded, job.StatusComposed, job.StatusDone:
			recoverRenderStates(store, queue, j)
		}
	}
}

// recoveryUniqueTTL matches the handlers' enqueue dedup window so a recovery
// enqueue and a client re-drive of the same work collapse into one task.
const recoveryUniqueTTL = time.Minute

func requeueTask(queue httpapi.Enqueuer, j job.Job, build func(id uuid.UUID) (*asynq.Task, error)) {
	task, err := build(j.ID)
	if err != nil {
		log.Printf("recovery: job %s (%s): build task: %v", j.ID, j.Status, err)
		return
	}
	if _, err := queue.Enqueue(task, asynq.MaxRetry(0), asynq.Unique(recoveryUniqueTTL)); err != nil && !errors.Is(err, asynq.ErrDuplicateTask) {
		log.Printf("recovery: job %s (%s): enqueue %s: %v", j.ID, j.Status, task.Type(), err)
		return
	}
	log.Printf("recovery: job %s: re-enqueued %s lost in a previous process", j.ID, task.Type())
}

func rollBack(ctx context.Context, repo orchestratorJobRepository, j job.Job, to job.Status) {
	if err := repo.UpdateStatus(ctx, j.ID, to, ""); err != nil {
		log.Printf("recovery: job %s: roll back %s -> %s: %v", j.ID, j.Status, to, err)
		return
	}
	log.Printf("recovery: job %s: rolled back %s -> %s (task lost in a previous process)", j.ID, j.Status, to)
}

// recoverRenderStates unsticks render-variant states a dead process left in
// queued/rendering. When the job's generate intent still describes the render
// (music, edit), the task is rebuilt and re-enqueued; renders are pure, so a
// re-run is safe. Without an intent the exact music/edit choices are gone, so
// the state is marked failed with an actionable reason and the UI's retry
// re-drives it with the client's own parameters.
func recoverRenderStates(store storage.Storage, queue httpapi.Enqueuer, j job.Job) {
	for _, loadout := range renderplan.LoadoutCatalog() {
		key, err := renderplan.RenderVariantStateKey(j.ID, loadout.Variant)
		if err != nil {
			continue
		}
		state, ok := readRenderState(store, key)
		if !ok || (state.Status != renderplan.RenderVariantStatusQueued && state.Status != renderplan.RenderVariantStatusRendering) {
			continue
		}
		if intent, ok := readGenerateIntent(store, j.ID); ok && intent.Variant == loadout.Variant {
			task, err := tasks.NewRenderVariantTask(j.ID, intent.Variant, intent.MusicKey, intent.Edit)
			if err == nil {
				if _, err := queue.Enqueue(task, asynq.Unique(recoveryUniqueTTL)); err == nil || errors.Is(err, asynq.ErrDuplicateTask) {
					log.Printf("recovery: job %s: re-enqueued render %s lost in a previous process", j.ID, loadout.Variant)
					continue
				}
			}
			log.Printf("recovery: job %s: rebuild render %s failed, marking state failed: %v", j.ID, loadout.Variant, err)
		}
		failed, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
			JobID:    j.ID,
			Loadout:  loadout,
			Status:   renderplan.RenderVariantStatusFailed,
			Error:    "render interrupted by an orchestrator restart; start the render again",
			Previous: &state,
		})
		if err != nil {
			log.Printf("recovery: job %s: build failed render state %s: %v", j.ID, loadout.Variant, err)
			continue
		}
		if err := writeRenderState(store, key, failed); err != nil {
			log.Printf("recovery: job %s: write render state %s: %v", j.ID, loadout.Variant, err)
			continue
		}
		log.Printf("recovery: job %s: render %s was interrupted mid-run; marked failed for retry", j.ID, loadout.Variant)
	}
}

func readRenderState(store storage.Storage, key string) (renderplan.RenderVariantState, bool) {
	rc, err := store.Open(key)
	if err != nil {
		return renderplan.RenderVariantState{}, false
	}
	defer rc.Close()
	var state renderplan.RenderVariantState
	if err := json.NewDecoder(rc).Decode(&state); err != nil {
		return renderplan.RenderVariantState{}, false
	}
	return state, true
}

func writeRenderState(store storage.Storage, key string, state renderplan.RenderVariantState) error {
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return store.Put(key, bytes.NewReader(b))
}

func readGenerateIntent(store storage.Storage, id uuid.UUID) (renderplan.GenerateIntent, bool) {
	rc, err := store.Open(artifacts.GenerateIntentKey(id))
	if err != nil {
		return renderplan.GenerateIntent{}, false
	}
	defer rc.Close()
	var intent renderplan.GenerateIntent
	if err := json.NewDecoder(rc).Decode(&intent); err != nil {
		return renderplan.GenerateIntent{}, false
	}
	return intent, true
}
