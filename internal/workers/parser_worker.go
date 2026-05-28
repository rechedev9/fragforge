// Package workers implements the Asynq task handlers that drive the
// orchestrator's pipeline. Each worker is a thin glue layer that pulls
// a job from the repository, delegates the heavy lifting to a domain
// package (parser, composer, ...), and writes the result back.
package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"

	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/parser"
	"github.com/reche/zackvideo/internal/storage"
	"github.com/reche/zackvideo/internal/tasks"
)

// JobRepository is the subset of *job.Repository the worker needs.
type JobRepository interface {
	Get(ctx context.Context, id uuid.UUID) (job.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, s job.Status, failureReason string) error
	SetKillPlan(ctx context.Context, id uuid.UUID, plan killplan.Plan) error
}

// ParserWorker handles the "parse:demo" Asynq task.
type ParserWorker struct {
	repo    JobRepository
	storage storage.Storage
}

// NewParserWorker returns a worker that processes parse:demo tasks.
func NewParserWorker(repo JobRepository, store storage.Storage) *ParserWorker {
	return &ParserWorker{repo: repo, storage: store}
}

// HandleParseDemo is the Asynq handler signature.
func (w *ParserWorker) HandleParseDemo(ctx context.Context, t *asynq.Task) error {
	var payload tasks.ParseDemoPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}

	j, err := w.repo.Get(ctx, payload.JobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", payload.JobID, err)
	}

	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusParsing, ""); err != nil {
		return fmt.Errorf("mark parsing: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeParseDemo, job.StatusParsing)

	plan, parseErr := w.parse(ctx, j)
	if parseErr != nil {
		recordTaskFailure(ctx, w.repo, j.ID, tasks.TypeParseDemo, parseErr)
		return parseErr
	}

	if err := w.repo.SetKillPlan(ctx, j.ID, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	logWorkerArtifacts(j.ID, tasks.TypeParseDemo, []string{"kill_plan"})
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusParsed, ""); err != nil {
		return fmt.Errorf("mark parsed: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeParseDemo, job.StatusParsed)
	return nil
}

func (w *ParserWorker) parse(ctx context.Context, j job.Job) (killplan.Plan, error) {
	rc, err := w.storage.Open(j.DemoPath)
	if err != nil {
		return killplan.Plan{}, fmt.Errorf("open demo: %w", err)
	}
	defer rc.Close()

	// demoinfocs needs an io.ReadSeeker for CS2 demos; copy to a temp file
	// to give it one without buffering the whole demo in memory.
	tmp, err := os.CreateTemp("", "zv-demo-*.dem")
	if err != nil {
		return killplan.Plan{}, fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	if _, err := io.Copy(tmp, rc); err != nil {
		return killplan.Plan{}, fmt.Errorf("write temp demo: %w", err)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return killplan.Plan{}, err
	}

	p := demoinfocs.NewParser(tmp)
	defer p.Close()

	meta := parser.PlanMeta{
		DemoPath: j.DemoPath,
		SHA256:   j.DemoSHA256,
	}
	plan, err := parser.RunWithContext(ctx, p, j.TargetSteamID, j.Rules, meta, parser.RunOptions{SegmentMode: parser.SegmentModeKills})
	if err != nil {
		if errors.Is(err, parser.ErrTargetNotFound) {
			return killplan.Plan{}, fmt.Errorf("target steamid %s not found in demo", j.TargetSteamID)
		}
		return killplan.Plan{}, err
	}
	return plan, nil
}
