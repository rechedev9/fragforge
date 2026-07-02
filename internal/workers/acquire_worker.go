package workers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/obs"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
	"github.com/rechedev9/fragforge/internal/vodfetch"
)

// StreamAcquireRepository is the subset of the stream job repository the
// AcquireWorker needs.
type StreamAcquireRepository interface {
	Get(ctx context.Context, id uuid.UUID) (streamclips.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, s streamclips.Status, failureReason string) error
	SetAcquired(ctx context.Context, id uuid.UUID, probe streamclips.SourceProbe, sha256 string) error
}

// AcquireWorkerConfig configures the "stream:acquire" worker.
type AcquireWorkerConfig struct {
	WorkDir     string
	YtdlpPath   string
	FFprobePath string
	Timeout     string
}

// AcquireWorker handles the "stream:acquire" Asynq task: it downloads a
// stream job's source_url with yt-dlp, probes the result, and moves the job
// to "ready". It never retries automatically (the task is enqueued with
// asynq.MaxRetry(0)): a failed download is an expensive, often user-fixable
// problem (wrong URL, private clip), so it is left failed for the user to
// retry explicitly rather than Asynq retrying blindly.
type AcquireWorker struct {
	repo    StreamAcquireRepository
	storage storage.Storage
	cfg     AcquireWorkerConfig
	fetcher vodfetch.Fetcher
	prober  streamclips.Prober
}

func NewAcquireWorker(repo StreamAcquireRepository, store storage.Storage, cfg AcquireWorkerConfig) *AcquireWorker {
	return &AcquireWorker{
		repo:    repo,
		storage: store,
		cfg:     cfg,
		fetcher: vodfetch.Fetcher{BinaryPath: cfg.YtdlpPath},
		prober:  streamclips.FFprobeProber{Path: cfg.FFprobePath},
	}
}

func (w *AcquireWorker) HandleStreamAcquire(ctx context.Context, t *asynq.Task) error {
	var payload tasks.StreamAcquirePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	j, err := w.repo.Get(ctx, payload.JobID)
	if err != nil {
		return fmt.Errorf("load stream job %s: %w", payload.JobID, err)
	}
	if err := w.acquire(ctx, j); err != nil {
		markStreamFailed(w.repo, j.ID, err.Error())
		recordStreamAcquireFailure(j.ID, err)
		logWorkerError(j.ID, tasks.TypeStreamAcquire, err)
		return err
	}
	logWorkerArtifacts(j.ID, tasks.TypeStreamAcquire, []string{j.SourcePath})
	return nil
}

func (w *AcquireWorker) acquire(ctx context.Context, j streamclips.Job) error {
	if j.SourceURL == "" {
		return fmt.Errorf("stream job %s has no source url", j.ID)
	}
	cfg := w.cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}

	workDir, cleanup, err := prepareStageDir(cfg.WorkDir, j.ID, "stream-acquire")
	if err != nil {
		return err
	}
	defer cleanup()

	destPath := filepath.Join(workDir, "source.mp4")
	sourceKey := streamclips.SourceKey(j.ID)

	// Idempotent: a retried/redriven acquire skips the download when the
	// durable source artifact already exists and just re-probes it.
	exists, err := w.storage.Exists(sourceKey)
	if err != nil {
		return fmt.Errorf("check stream source artifact: %w", err)
	}
	if exists {
		logWorkerSkip(j.ID, tasks.TypeStreamAcquire, []string{sourceKey})
		if err := copyStorageToFile(w.storage, sourceKey, destPath); err != nil {
			return fmt.Errorf("materialize existing stream source: %w", err)
		}
	} else {
		runCtx, cancel := context.WithTimeout(ctx, cfg.timeoutDuration())
		defer cancel()
		if _, err := w.fetcher.Download(runCtx, j.SourceURL, destPath); err != nil {
			return err
		}
		if err := uploadFile(w.storage, sourceKey, destPath); err != nil {
			return fmt.Errorf("upload stream source: %w", err)
		}
	}

	probe, err := w.prober.Probe(ctx, destPath)
	if err != nil {
		return fmt.Errorf("probe stream source: %w", err)
	}
	sha, err := sha256File(destPath)
	if err != nil {
		return fmt.Errorf("hash stream source: %w", err)
	}

	if err := w.repo.SetAcquired(ctx, j.ID, probe, sha); err != nil {
		return fmt.Errorf("mark stream job acquired: %w", err)
	}
	// Seed the default edit plan artifact so GetStreamEditPlan has something
	// to serve immediately, mirroring the multipart upload path.
	plan := streamclips.DefaultEditPlan()
	if err := writeStreamEditPlanArtifact(w.storage, j.ID, plan); err != nil {
		return fmt.Errorf("write default stream edit plan: %w", err)
	}
	return nil
}

func (c AcquireWorkerConfig) withDefaults() AcquireWorkerConfig {
	if c.Timeout == "" {
		c.Timeout = defaultMediaWorkerTimeout
	}
	return c
}

func (c AcquireWorkerConfig) validate() error {
	if _, err := time.ParseDuration(c.Timeout); err != nil {
		return fmt.Errorf("timeout must be a duration: %w", err)
	}
	return nil
}

func (c AcquireWorkerConfig) timeoutDuration() time.Duration {
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return 20 * time.Minute
	}
	return d
}

// sha256File hashes a local file's contents.
func sha256File(path string) (string, error) {
	// #nosec G304 -- path is produced by the acquire worker's own stage dir.
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeStreamEditPlanArtifact writes the edit plan JSON artifact, mirroring
// the httpapi handler's helper of the same shape for the multipart upload
// path (see internal/httpapi/stream_handlers.go); duplicated here rather than
// exported across the package boundary since it is a two-line storage write.
func writeStreamEditPlanArtifact(store storage.Storage, id uuid.UUID, plan streamclips.EditPlan) error {
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	return store.Put(streamclips.EditPlanKey(id), bytes.NewReader(append(b, '\n')))
}

// acquireFailureClass classifies an acquire failure by vodfetch's sentinel
// errors so obs/journal entries and metrics distinguish "source not found"
// from "auth required" from "unavailable" from a generic error.
func acquireFailureClass(err error) string {
	switch {
	case errors.Is(err, vodfetch.ErrNotFound):
		return "not_found"
	case errors.Is(err, vodfetch.ErrAuthRequired):
		return "auth_required"
	case errors.Is(err, vodfetch.ErrUnavailable):
		return "unavailable"
	default:
		return "error"
	}
}

// recordStreamAcquireFailure appends a terminal acquire failure to the local
// obs journal, mirroring recordWorkerFailure but under the stream_acquire
// stage with a class derived from the vodfetch sentinel error so acquire
// failures are distinguishable from the rest of the "worker" stage.
func recordStreamAcquireFailure(id uuid.UUID, err error) {
	rec := obs.Default()
	if rec == nil {
		return
	}
	_ = rec.RecordError(obs.Event{
		Stage:   obs.StageStreamAcquire,
		Class:   acquireFailureClass(err),
		Message: id.String() + ": " + err.Error(),
	})
}
