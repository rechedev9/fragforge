package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/composition"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

const defaultMediaWorkerTimeout = "20m"

// failureWriteTimeout bounds the fresh-context status write performed when a
// task fails. The handler context is frequently already cancelled at that
// point (Asynq deadline or shutdown), so the terminal StatusFailed write needs
// its own context to land in the database.
const failureWriteTimeout = 5 * time.Second

// StatusRepository is the subset of *job.Repository needed by media workers.
type StatusRepository interface {
	Get(ctx context.Context, id uuid.UUID) (job.Job, error)
	GetMeta(ctx context.Context, id uuid.UUID) (job.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, s job.Status, failureReason string) error
}

// statusUpdater is the single method markFailed needs; both StatusRepository
// and JobRepository satisfy it.
type statusUpdater interface {
	UpdateStatus(ctx context.Context, id uuid.UUID, s job.Status, failureReason string) error
}

// markFailed records a job's terminal failure on a fresh, short-lived context
// so the write survives a handler context already cancelled by an Asynq
// deadline or shutdown (pgxpool.Exec refuses to run on a cancelled context).
// The secondary error is logged rather than discarded: a job stranded in a
// non-terminal status is otherwise invisible to operators.
func markFailed(repo statusUpdater, id uuid.UUID, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), failureWriteTimeout)
	defer cancel()
	if err := repo.UpdateStatus(ctx, id, job.StatusFailed, reason); err != nil {
		logWorkerError(id, "mark failed", err)
	}
}

// recordTaskFailure records a job's failure, but only when the current Asynq
// attempt is terminal (returning the error now archives the task instead of
// scheduling another retry). For a retryable task an intermediate failure is
// left as the in-progress status so the job does not flap StatusFailed<->in
// progress across retries; the terminal failure is recorded once retries are
// exhausted.
func recordTaskFailure(ctx context.Context, repo statusUpdater, id uuid.UUID, taskType string, err error) {
	if !taskIsTerminal(ctx) {
		logWorkerError(id, taskType+" will retry", err)
		return
	}
	markFailed(repo, id, err.Error())
	logWorkerTransition(id, taskType, job.StatusFailed)
}

// taskIsTerminal reports whether the current Asynq attempt is the last one, so
// returning an error archives the task instead of retrying. Outside an Asynq
// task context (e.g. direct unit tests) it returns true so a failure is still
// recorded.
func taskIsTerminal(ctx context.Context) bool {
	retried, ok1 := asynq.GetRetryCount(ctx)
	maxRetry, ok2 := asynq.GetMaxRetry(ctx)
	return isTerminalAttempt(retried, maxRetry, ok1 && ok2)
}

// isTerminalAttempt holds the retry arithmetic separately so it can be tested
// without an Asynq task context.
func isTerminalAttempt(retried, maxRetry int, inTask bool) bool {
	if !inTask {
		return true
	}
	return retried >= maxRetry
}

type commandRunner interface {
	Run(ctx context.Context, exe string, args ...string) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	// #nosec G204 -- media workers execute configured local binaries with argument slices, not shell strings.
	cmd := exec.CommandContext(ctx, exe, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return out, nil
	}
	if text := strings.TrimSpace(string(out)); text != "" {
		return out, fmt.Errorf("%s failed: %w: %s", exe, err, text)
	}
	return out, fmt.Errorf("%s failed: %w", exe, err)
}

type RecordWorkerConfig struct {
	WorkDir      string
	RecorderPath string
	HLAEPath     string
	CS2Path      string
	Timeout      string
}

type ComposeWorkerConfig struct {
	WorkDir      string
	ComposerPath string
	FFmpegPath   string
	Timeout      string
}

type RenderWorkerConfig struct {
	WorkDir     string
	EditorPath  string
	FFmpegPath  string
	FFprobePath string
	Timeout     string
}

type StreamRenderRepository interface {
	Get(ctx context.Context, id uuid.UUID) (streamclips.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, s streamclips.Status, failureReason string) error
}

type StreamRenderWorkerConfig struct {
	WorkDir    string
	FFmpegPath string
	Timeout    string
}

// RecordWorker handles the "record:demo" Asynq task.
type RecordWorker struct {
	repo    StatusRepository
	storage storage.Storage
	cfg     RecordWorkerConfig
	runner  commandRunner
}

func NewRecordWorker(repo StatusRepository, store storage.Storage, cfg RecordWorkerConfig) *RecordWorker {
	return &RecordWorker{
		repo:    repo,
		storage: store,
		cfg:     cfg,
		runner:  execCommandRunner{},
	}
}

func (w *RecordWorker) HandleRecordDemo(ctx context.Context, t *asynq.Task) error {
	var payload tasks.RecordDemoPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}

	j, err := w.repo.Get(ctx, payload.JobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", payload.JobID, err)
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusRecording, ""); err != nil {
		return fmt.Errorf("mark recording: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeRecordDemo, job.StatusRecording)

	if err := w.record(ctx, j); err != nil {
		recordTaskFailure(ctx, w.repo, j.ID, tasks.TypeRecordDemo, err)
		return err
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusRecorded, ""); err != nil {
		return fmt.Errorf("mark recorded: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeRecordDemo, job.StatusRecorded)
	return nil
}

func (w *RecordWorker) record(ctx context.Context, j job.Job) error {
	if j.KillPlan == nil {
		return fmt.Errorf("job %s has no kill plan", j.ID)
	}
	ready, keys, err := recordingOutputsReady(w.storage, j.ID)
	if err != nil {
		return err
	}
	if ready {
		logWorkerSkip(j.ID, tasks.TypeRecordDemo, keys)
		return nil
	}

	cfg := w.cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}

	workDir, cleanup, err := prepareStageDir(cfg.WorkDir, j.ID, "record")
	if err != nil {
		return err
	}
	defer cleanup()

	demoPath := filepath.Join(workDir, "demo.dem")
	killPlanPath := filepath.Join(workDir, "killplan.json")
	outDir := filepath.Join(workDir, "out")
	if err := copyStorageToFile(w.storage, j.DemoPath, demoPath); err != nil {
		return fmt.Errorf("materialize demo: %w", err)
	}
	if err := writeJSONFile(killPlanPath, j.KillPlan); err != nil {
		return fmt.Errorf("write kill plan: %w", err)
	}

	_, runErr := w.runner.Run(ctx, cfg.RecorderPath,
		"--killplan", killPlanPath,
		"--demo", demoPath,
		"--out", outDir,
		"--hlae", cfg.HLAEPath,
		"--cs2", cfg.CS2Path,
		"--timeout", cfg.Timeout,
	)

	resultPath := filepath.Join(outDir, "recording-result.json")
	var result recording.RecordingResult
	if err := readJSONFile(resultPath, &result); err != nil {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("read recording result: %w", err)
	}
	keys, err = uploadRecordingOutputs(w.storage, j.ID, outDir, resultPath, result)
	if err != nil {
		return err
	}
	logWorkerArtifacts(j.ID, tasks.TypeRecordDemo, keys)
	if runErr != nil {
		return runErr
	}
	if result.Error != "" {
		return fmt.Errorf("recording result error: %s", result.Error)
	}
	return nil
}

func (c RecordWorkerConfig) withDefaults() RecordWorkerConfig {
	if c.Timeout == "" {
		c.Timeout = defaultMediaWorkerTimeout
	}
	return c
}

func (c RecordWorkerConfig) validate() error {
	required := map[string]string{
		"recorder": c.RecorderPath,
		"hlae":     c.HLAEPath,
		"cs2":      c.CS2Path,
		"timeout":  c.Timeout,
	}
	for name, value := range required {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

// ComposeWorker handles the "compose:final" Asynq task.
type ComposeWorker struct {
	repo    StatusRepository
	storage storage.Storage
	cfg     ComposeWorkerConfig
	runner  commandRunner
}

func NewComposeWorker(repo StatusRepository, store storage.Storage, cfg ComposeWorkerConfig) *ComposeWorker {
	return &ComposeWorker{
		repo:    repo,
		storage: store,
		cfg:     cfg,
		runner:  execCommandRunner{},
	}
}

func (w *ComposeWorker) HandleComposeFinal(ctx context.Context, t *asynq.Task) error {
	var payload tasks.ComposeFinalPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}

	j, err := w.repo.GetMeta(ctx, payload.JobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", payload.JobID, err)
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusComposing, ""); err != nil {
		return fmt.Errorf("mark composing: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeComposeFinal, job.StatusComposing)

	if err := w.compose(ctx, j); err != nil {
		recordTaskFailure(ctx, w.repo, j.ID, tasks.TypeComposeFinal, err)
		return err
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusComposed, ""); err != nil {
		return fmt.Errorf("mark composed: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeComposeFinal, job.StatusComposed)
	return nil
}

func (w *ComposeWorker) compose(ctx context.Context, j job.Job) error {
	ready, keys, err := compositionOutputsReady(w.storage, j.ID)
	if err != nil {
		return err
	}
	if ready {
		logWorkerSkip(j.ID, tasks.TypeComposeFinal, keys)
		return nil
	}

	cfg := w.cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}

	workDir, cleanup, err := prepareStageDir(cfg.WorkDir, j.ID, "compose")
	if err != nil {
		return err
	}
	defer cleanup()

	recordingResult, err := readStoredRecordingResult(w.storage, j.ID)
	if err != nil {
		return err
	}
	localRecordingResult := filepath.Join(workDir, "recording-result.json")
	if err := localizeSegmentClips(w.storage, j.ID, workDir, &recordingResult); err != nil {
		return err
	}
	if err := writeJSONFile(localRecordingResult, recordingResult); err != nil {
		return fmt.Errorf("write localized recording result: %w", err)
	}

	finalPath := filepath.Join(workDir, "final.mp4")
	args := []string{
		"--recording-result", localRecordingResult,
		"--out", finalPath,
		"--timeout", cfg.Timeout,
	}
	if cfg.FFmpegPath != "" {
		args = append(args, "--ffmpeg", cfg.FFmpegPath)
	}
	_, runErr := w.runner.Run(ctx, cfg.ComposerPath, args...)

	resultPath := filepath.Join(workDir, "composition-result.json")
	var result composition.Result
	if err := readJSONFile(resultPath, &result); err != nil {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("read composition result: %w", err)
	}
	if err := uploadFile(w.storage, artifacts.CompositionResultKey(j.ID), resultPath); err != nil {
		return fmt.Errorf("upload composition result: %w", err)
	}
	if runErr != nil {
		return runErr
	}
	if result.Error != "" {
		return fmt.Errorf("composition result error: %s", result.Error)
	}
	if err := uploadFile(w.storage, artifacts.FinalMP4Key(j.ID), finalPath); err != nil {
		return fmt.Errorf("upload final mp4: %w", err)
	}
	logWorkerArtifacts(j.ID, tasks.TypeComposeFinal, []string{
		artifacts.CompositionResultKey(j.ID),
		artifacts.FinalMP4Key(j.ID),
	})
	return nil
}

func (c ComposeWorkerConfig) withDefaults() ComposeWorkerConfig {
	if c.Timeout == "" {
		c.Timeout = defaultMediaWorkerTimeout
	}
	return c
}

func (c ComposeWorkerConfig) validate() error {
	required := map[string]string{
		"composer": c.ComposerPath,
		"timeout":  c.Timeout,
	}
	for name, value := range required {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

// RenderWorker handles the "render:variant" Asynq task.
type RenderWorker struct {
	repo    StatusRepository
	storage storage.Storage
	cfg     RenderWorkerConfig
	runner  commandRunner
}

func NewRenderWorker(repo StatusRepository, store storage.Storage, cfg RenderWorkerConfig) *RenderWorker {
	return &RenderWorker{
		repo:    repo,
		storage: store,
		cfg:     cfg,
		runner:  execCommandRunner{},
	}
}

// StreamRenderWorker handles "render:stream-clip" tasks.
type StreamRenderWorker struct {
	repo    StreamRenderRepository
	storage storage.Storage
	cfg     StreamRenderWorkerConfig
	runner  commandRunner
}

func NewStreamRenderWorker(repo StreamRenderRepository, store storage.Storage, cfg StreamRenderWorkerConfig) *StreamRenderWorker {
	return &StreamRenderWorker{
		repo:    repo,
		storage: store,
		cfg:     cfg,
		runner:  execCommandRunner{},
	}
}

func (w *StreamRenderWorker) HandleRenderStreamClip(ctx context.Context, t *asynq.Task) error {
	var payload tasks.RenderStreamClipPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	j, err := w.repo.Get(ctx, payload.JobID)
	if err != nil {
		return fmt.Errorf("load stream job %s: %w", payload.JobID, err)
	}
	if err := w.render(ctx, j, payload.Variant); err != nil {
		markStreamFailed(w.repo, j.ID, err.Error())
		if stateErr := w.writeStreamState(j.ID, payload.Variant, streamclips.StatusFailed, nil, err.Error(), nil); stateErr != nil {
			logWorkerError(j.ID, "write failed stream render state", stateErr)
		}
		logWorkerError(j.ID, tasks.TypeRenderStreamClip, err)
		return err
	}
	return nil
}

func (w *StreamRenderWorker) render(ctx context.Context, j streamclips.Job, variant string) error {
	if variant != streamclips.VariantStreamerVerticalStack {
		return fmt.Errorf("unsupported stream render variant %q", variant)
	}
	cfg := w.cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	if len(j.EditPlan) == 0 {
		return fmt.Errorf("stream job %s has no edit plan", j.ID)
	}
	var plan streamclips.EditPlan
	if err := json.Unmarshal(j.EditPlan, &plan); err != nil {
		return fmt.Errorf("decode edit plan: %w", err)
	}
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.Validate(); err != nil {
		return err
	}
	if len(plan.Clips) == 0 {
		return fmt.Errorf("edit plan has no clips")
	}

	if err := w.repo.UpdateStatus(ctx, j.ID, streamclips.StatusRendering, ""); err != nil {
		return fmt.Errorf("mark stream rendering: %w", err)
	}
	if err := w.writeStreamState(j.ID, variant, streamclips.StatusRendering, nil, "", nil); err != nil {
		return err
	}

	workDir, cleanup, err := prepareStageDir(cfg.WorkDir, j.ID, "stream-render")
	if err != nil {
		return err
	}
	defer cleanup()

	sourcePath := filepath.Join(workDir, "source.mp4")
	if err := copyStorageToFile(w.storage, j.SourcePath, sourcePath); err != nil {
		return fmt.Errorf("materialize stream source: %w", err)
	}
	outDir := filepath.Join(workDir, "out", "shortslistosparasubir")
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, cfg.timeoutDuration())
	defer cancel()
	var videos []streamclips.VideoEntry
	for _, clip := range plan.Clips {
		outPath := filepath.Join(outDir, clip.ID+".mp4")
		args, err := streamclips.BuildFFmpegArgs(sourcePath, outPath, plan, clip)
		if err != nil {
			return err
		}
		if _, err := w.runner.Run(runCtx, cfg.FFmpegPath, args...); err != nil {
			return fmt.Errorf("render clip %s: %w", clip.ID, err)
		}
		key, err := streamclips.RenderVideoKey(j.ID, variant, clip.ID)
		if err != nil {
			return err
		}
		if err := uploadFile(w.storage, key, outPath); err != nil {
			return fmt.Errorf("upload stream clip %s: %w", clip.ID, err)
		}
		videos = append(videos, streamclips.VideoEntry{
			ClipID:          clip.ID,
			Title:           clip.Title,
			Key:             key,
			DurationSeconds: clip.EndSeconds - clip.StartSeconds,
		})
	}

	result := streamclips.RenderResult{
		SchemaVersion: "1.0",
		JobID:         j.ID,
		Variant:       variant,
		Clips:         videos,
		RenderedAt:    time.Now().UTC(),
	}
	resultKey, err := streamclips.RenderResultKey(j.ID, variant)
	if err != nil {
		return err
	}
	if err := putJSONToStorage(w.storage, resultKey, result); err != nil {
		return fmt.Errorf("write stream render result: %w", err)
	}
	galleryKey, err := streamclips.RenderGalleryKey(j.ID, variant)
	if err != nil {
		return err
	}
	if err := w.storage.Put(galleryKey, strings.NewReader(streamGalleryHTML(j, videos))); err != nil {
		return fmt.Errorf("write stream gallery: %w", err)
	}
	if err := w.writeStreamState(j.ID, variant, streamclips.StatusRendered, nil, "", videos); err != nil {
		return err
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, streamclips.StatusRendered, ""); err != nil {
		return fmt.Errorf("mark stream rendered: %w", err)
	}
	logWorkerArtifacts(j.ID, tasks.TypeRenderStreamClip, []string{resultKey, galleryKey})
	return nil
}

func (w *StreamRenderWorker) writeStreamState(id uuid.UUID, variant string, status streamclips.Status, warnings []string, errMsg string, videos []streamclips.VideoEntry) error {
	resultKey, err := streamclips.RenderResultKey(id, variant)
	if err != nil {
		return err
	}
	galleryKey, err := streamclips.RenderGalleryKey(id, variant)
	if err != nil {
		return err
	}
	prefix, err := streamclips.RenderPrefix(id, variant)
	if err != nil {
		return err
	}
	state := streamclips.RenderState{
		JobID:       id,
		Variant:     variant,
		Status:      status,
		ResultKey:   resultKey,
		GalleryKey:  galleryKey,
		ArtifactDir: prefix,
		Warnings:    warnings,
		Error:       errMsg,
		Videos:      videos,
		UpdatedAt:   time.Now().UTC(),
	}
	key, err := streamclips.RenderStateKey(id, variant)
	if err != nil {
		return err
	}
	return putJSONToStorage(w.storage, key, state)
}

func (c StreamRenderWorkerConfig) withDefaults() StreamRenderWorkerConfig {
	if c.Timeout == "" {
		c.Timeout = defaultMediaWorkerTimeout
	}
	return c
}

func (c StreamRenderWorkerConfig) validate() error {
	if c.FFmpegPath == "" {
		return fmt.Errorf("ffmpeg is required")
	}
	if _, err := time.ParseDuration(c.Timeout); err != nil {
		return fmt.Errorf("timeout must be a duration: %w", err)
	}
	return nil
}

func (c StreamRenderWorkerConfig) timeoutDuration() time.Duration {
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return 20 * time.Minute
	}
	return d
}

func markStreamFailed(repo StreamRenderRepository, id uuid.UUID, reason string) {
	ctx, cancel := context.WithTimeout(context.Background(), failureWriteTimeout)
	defer cancel()
	if err := repo.UpdateStatus(ctx, id, streamclips.StatusFailed, reason); err != nil {
		logWorkerError(id, "mark stream failed", err)
	}
}

func streamGalleryHTML(j streamclips.Job, videos []streamclips.VideoEntry) string {
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Streamer clips</title></head><body>")
	b.WriteString("<h1>")
	b.WriteString(j.Title)
	b.WriteString("</h1>")
	for _, video := range videos {
		b.WriteString("<section><h2>")
		b.WriteString(video.ClipID)
		b.WriteString("</h2><video controls src=\"videos/")
		b.WriteString(video.ClipID)
		b.WriteString("\"></video></section>")
	}
	b.WriteString("</body></html>")
	return b.String()
}

func (w *RenderWorker) HandleRenderVariant(ctx context.Context, t *asynq.Task) error {
	var payload tasks.RenderVariantPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}

	j, err := w.repo.Get(ctx, payload.JobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", payload.JobID, err)
	}
	variant := payload.Variant
	if variant == "" {
		variant = editor.DefaultPreset().Name
	}
	if err := w.render(ctx, j, variant); err != nil {
		logWorkerError(j.ID, tasks.TypeRenderVariant, err)
		return err
	}
	return nil
}

func (w *RenderWorker) render(ctx context.Context, j job.Job, variant string) (err error) {
	loadout, err := renderplan.LoadoutForVariant(variant)
	if err != nil {
		return err
	}
	previousState, _, err := w.readRenderVariantState(j.ID, variant)
	if err != nil {
		return fmt.Errorf("read render state: %w", err)
	}
	ready, keys, err := renderVariantOutputsReady(w.storage, j.ID, variant)
	if err != nil {
		return err
	}
	if ready {
		state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
			JobID:    j.ID,
			Loadout:  loadout,
			Status:   renderplan.RenderVariantStatusReady,
			Previous: previousState,
		})
		if err != nil {
			return err
		}
		if err := w.writeRenderVariantState(state); err != nil {
			return fmt.Errorf("write ready render state: %w", err)
		}
		logWorkerSkip(j.ID, tasks.TypeRenderVariant, keys)
		return nil
	}
	if j.KillPlan == nil {
		return fmt.Errorf("job %s has no kill plan", j.ID)
	}

	cfg := w.cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:    j.ID,
		Loadout:  loadout,
		Status:   renderplan.RenderVariantStatusRendering,
		Previous: previousState,
	})
	if err != nil {
		return err
	}
	if err := w.writeRenderVariantState(state); err != nil {
		return fmt.Errorf("write rendering state: %w", err)
	}
	currentState := &state
	var result editor.Result
	defer func() {
		if err == nil {
			return
		}
		failedState, stateErr := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
			JobID:    j.ID,
			Loadout:  loadout,
			Status:   renderplan.RenderVariantStatusFailed,
			Warnings: result.Warnings,
			Error:    renderVariantFailureMessage(result, err),
			Previous: currentState,
		})
		if stateErr != nil {
			err = fmt.Errorf("%w; build failed render state: %v", err, stateErr)
			return
		}
		if writeErr := w.writeRenderVariantState(failedState); writeErr != nil {
			err = fmt.Errorf("%w; write failed render state: %v", err, writeErr)
		}
	}()

	workDir, cleanup, err := prepareStageDir(cfg.WorkDir, j.ID, "render")
	if err != nil {
		return err
	}
	defer cleanup()

	recordingResult, err := readStoredRecordingResult(w.storage, j.ID)
	if err != nil {
		return err
	}
	localRecordingResult := filepath.Join(workDir, "recording-result.json")
	if err := localizeSegmentClips(w.storage, j.ID, workDir, &recordingResult); err != nil {
		return err
	}
	if err := writeJSONFile(localRecordingResult, recordingResult); err != nil {
		return fmt.Errorf("write localized recording result: %w", err)
	}
	localKillPlan := filepath.Join(workDir, "killplan.json")
	if err := writeJSONFile(localKillPlan, j.KillPlan); err != nil {
		return fmt.Errorf("write kill plan: %w", err)
	}

	outDir := filepath.Join(workDir, "out")
	publishDir := filepath.Join(outDir, "shortslistosparasubir")
	if err := w.writeEditDocument(outDir, j.ID, loadout, recordingResult); err != nil {
		return err
	}
	args := []string{
		"--recording-result", localRecordingResult,
		"--killplan", localKillPlan,
		"--out", outDir,
		"--publish-dir", publishDir,
		"--preset", loadout.Preset,
	}
	if cfg.FFmpegPath != "" {
		args = append(args, "--ffmpeg", cfg.FFmpegPath)
	}
	if cfg.FFprobePath != "" {
		args = append(args, "--ffprobe", cfg.FFprobePath)
	}

	runCtx, cancel := context.WithTimeout(ctx, cfg.timeoutDuration())
	defer cancel()
	_, runErr := w.runner.Run(runCtx, cfg.EditorPath, args...)

	resultPath := filepath.Join(outDir, "shorts-result.json")
	if err := readJSONFile(resultPath, &result); err != nil {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("read render result: %w", err)
	}
	if cfg.FFprobePath != "" {
		if err := probeRenderResult(runCtx, w.runner, cfg.FFprobePath, &result); err != nil {
			result.Warnings = append(result.Warnings, "ffprobe quality metadata: "+err.Error())
		}
		if err := writeJSONFile(resultPath, result); err != nil {
			return fmt.Errorf("write probed render result: %w", err)
		}
	}
	keys, err = uploadRenderVariantOutputs(w.storage, j.ID, variant, outDir, publishDir, resultPath, result)
	if err != nil {
		return err
	}
	logWorkerArtifacts(j.ID, tasks.TypeRenderVariant, keys)
	if runErr != nil {
		return runErr
	}
	if result.Error != "" {
		return fmt.Errorf("render result error: %s", result.Error)
	}
	readyState, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:    j.ID,
		Loadout:  loadout,
		Status:   renderplan.RenderVariantStatusReady,
		Warnings: result.Warnings,
		Previous: currentState,
	})
	if err != nil {
		return err
	}
	if err := w.writeRenderVariantState(readyState); err != nil {
		return fmt.Errorf("write ready render state: %w", err)
	}
	return nil
}

func (w *RenderWorker) writeEditDocument(outDir string, id uuid.UUID, loadout renderplan.Loadout, result recording.RecordingResult) error {
	doc, err := renderplan.NewEditDocumentForLoadout(renderplan.NewEditDocumentForLoadoutOptions{
		JobID:      id,
		Loadout:    loadout,
		SegmentIDs: recordingSegmentIDs(result),
	})
	if err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(outDir, "edit-document.json"), doc)
}

func (w *RenderWorker) readRenderVariantState(id uuid.UUID, variant string) (*renderplan.RenderVariantState, bool, error) {
	key, err := artifacts.RenderVariantStatusKey(id, variant)
	if err != nil {
		return nil, false, err
	}
	rc, err := w.storage.Open(key)
	if err != nil {
		if !storage.IsNotExist(err) {
			return nil, false, err
		}
		return nil, false, nil
	}
	defer rc.Close()
	var state renderplan.RenderVariantState
	if err := json.NewDecoder(rc).Decode(&state); err != nil {
		return nil, false, err
	}
	return &state, true, nil
}

func (w *RenderWorker) writeRenderVariantState(state renderplan.RenderVariantState) error {
	key, err := artifacts.RenderVariantStatusKey(state.JobID, state.Variant)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return w.storage.Put(key, bytes.NewReader(b))
}

func renderVariantFailureMessage(result editor.Result, err error) string {
	if result.Error != "" {
		return result.Error
	}
	return err.Error()
}

type ffprobeJSON struct {
	Streams []struct {
		CodecName  string `json:"codec_name"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		RFrameRate string `json:"r_frame_rate"`
		Duration   string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		Size     string `json:"size"`
	} `json:"format"`
}

func probeRenderResult(ctx context.Context, runner commandRunner, ffprobePath string, result *editor.Result) error {
	var firstErr error
	for i := range result.Shorts {
		short := &result.Shorts[i]
		path := short.PublishPath
		role := "publish"
		if path == "" {
			path = short.Output
			role = "short"
		}
		if path == "" {
			continue
		}
		artifact, err := probeVideoArtifact(ctx, runner, ffprobePath, short.SegmentID, role, path)
		if err != nil {
			artifact = recording.RecordingArtifact{
				SegmentID:  short.SegmentID,
				Role:       role,
				Type:       "video",
				Path:       path,
				ProbeError: err.Error(),
			}
			if firstErr == nil {
				firstErr = err
			}
		}
		if role == "publish" {
			short.PublishArtifact = artifact
		} else {
			short.OutputArtifact = artifact
		}
	}
	return firstErr
}

func probeVideoArtifact(ctx context.Context, runner commandRunner, ffprobePath, segmentID, role, path string) (recording.RecordingArtifact, error) {
	out, err := runner.Run(ctx, ffprobePath,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height,r_frame_rate,duration:format=duration,size",
		"-of", "json",
		path,
	)
	artifact := recording.RecordingArtifact{
		SegmentID: segmentID,
		Role:      role,
		Type:      "video",
		Path:      path,
	}
	if stat, statErr := os.Stat(path); statErr == nil {
		artifact.SizeBytes = stat.Size()
	}
	if err != nil {
		artifact.ProbeError = err.Error()
		return artifact, err
	}
	var probe ffprobeJSON
	if err := json.Unmarshal(out, &probe); err != nil {
		artifact.ProbeError = err.Error()
		return artifact, err
	}
	if len(probe.Streams) > 0 {
		stream := probe.Streams[0]
		artifact.Codec = stream.CodecName
		artifact.Width = stream.Width
		artifact.Height = stream.Height
		artifact.FrameRate = stream.RFrameRate
		if seconds := parseProbeFloat(stream.Duration); seconds > 0 {
			artifact.DurationSeconds = seconds
		}
	}
	if artifact.DurationSeconds == 0 {
		artifact.DurationSeconds = parseProbeFloat(probe.Format.Duration)
	}
	if size := parseProbeInt(probe.Format.Size); size > 0 {
		artifact.SizeBytes = size
	}
	return artifact, nil
}

func parseProbeFloat(value string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return f
}

func parseProbeInt(value string) int64 {
	i, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return i
}

func (c RenderWorkerConfig) withDefaults() RenderWorkerConfig {
	if c.Timeout == "" {
		c.Timeout = defaultMediaWorkerTimeout
	}
	return c
}

func (c RenderWorkerConfig) validate() error {
	required := map[string]string{
		"editor":  c.EditorPath,
		"timeout": c.Timeout,
	}
	for name, value := range required {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if _, err := time.ParseDuration(c.Timeout); err != nil {
		return fmt.Errorf("timeout must be a duration: %w", err)
	}
	return nil
}

func (c RenderWorkerConfig) timeoutDuration() time.Duration {
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return 20 * time.Minute
	}
	return d
}

func prepareStageDir(root string, id uuid.UUID, stage string) (string, func(), error) {
	base := root
	cleanup := func() {}
	if base == "" {
		base = os.TempDir()
	}
	if err := os.MkdirAll(base, 0o750); err != nil {
		return "", nil, err
	}
	dir, err := os.MkdirTemp(base, fmt.Sprintf("zv-%s-%s-", stage, id))
	if err != nil {
		return "", nil, err
	}
	if root == "" {
		cleanup = func() { _ = os.RemoveAll(dir) }
	}
	return dir, cleanup, nil
}

func copyStorageToFile(store storage.Storage, key, path string) error {
	rc, err := store.Open(key)
	if err != nil {
		return err
	}
	defer rc.Close()

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	// #nosec G304 -- path is constructed under the worker stage directory.
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, rc)
	return err
}

func uploadFile(store storage.Storage, key, path string) error {
	// #nosec G304 -- path is produced by recorder/composer stage outputs before upload.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return store.Put(key, f)
}

func uploadOptionalFile(store storage.Storage, key, path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, uploadFile(store, key, path)
}

func uploadRecordingOutputs(store storage.Storage, id uuid.UUID, outDir, resultPath string, result recording.RecordingResult) ([]string, error) {
	keys := []string{artifacts.RecordingResultKey(id)}
	if err := uploadFile(store, artifacts.RecordingResultKey(id), resultPath); err != nil {
		return nil, fmt.Errorf("upload recording result: %w", err)
	}

	scriptPath := result.Script
	if scriptPath == "" {
		scriptPath = filepath.Join(outDir, "recording.js")
	}
	uploadedScript, err := uploadOptionalFile(store, artifacts.RecordingScriptKey(id), scriptPath)
	if err != nil {
		return nil, fmt.Errorf("upload recording script: %w", err)
	}
	if uploadedScript {
		keys = append(keys, artifacts.RecordingScriptKey(id))
	} else if result.Error == "" {
		return nil, fmt.Errorf("recording script not found at %s", scriptPath)
	}

	uploadedSegments := 0
	for _, artifact := range result.Artifacts {
		if artifact.Role != "segment" || artifact.Type != "video" || artifact.SegmentID == "" {
			continue
		}
		key, err := artifacts.SegmentClipKey(id, artifact.SegmentID)
		if err != nil {
			return nil, err
		}
		if err := uploadFile(store, key, artifact.Path); err != nil {
			return nil, fmt.Errorf("upload segment %s: %w", artifact.SegmentID, err)
		}
		keys = append(keys, key)
		uploadedSegments++
	}
	if result.Error == "" && uploadedSegments == 0 {
		return nil, fmt.Errorf("recording result has no segment clips")
	}
	return keys, nil
}

func uploadRenderVariantOutputs(store storage.Storage, id uuid.UUID, variant, outDir, publishDir, resultPath string, result editor.Result) ([]string, error) {
	resultKey, err := artifacts.RenderVariantResultKey(id, variant)
	if err != nil {
		return nil, err
	}
	keys := []string{resultKey}
	if err := uploadFile(store, resultKey, resultPath); err != nil {
		return nil, fmt.Errorf("upload render result: %w", err)
	}

	editDocumentKey, err := artifacts.RenderVariantEditDocumentKey(id, variant)
	if err != nil {
		return nil, err
	}
	if uploaded, err := uploadOptionalFile(store, editDocumentKey, filepath.Join(outDir, "edit-document.json")); err != nil {
		return nil, fmt.Errorf("upload edit document: %w", err)
	} else if uploaded {
		keys = append(keys, editDocumentKey)
	}

	editManifestKey, err := artifacts.RenderVariantEditManifestKey(id, variant)
	if err != nil {
		return nil, err
	}
	if uploaded, err := uploadOptionalFile(store, editManifestKey, filepath.Join(outDir, "edit-manifest.json")); err != nil {
		return nil, fmt.Errorf("upload edit manifest: %w", err)
	} else if uploaded {
		keys = append(keys, editManifestKey)
	}

	packKey, err := artifacts.RenderVariantPackManifestKey(id, variant)
	if err != nil {
		return nil, err
	}
	if uploaded, err := uploadOptionalFile(store, packKey, filepath.Join(publishDir, "pack-manifest.json")); err != nil {
		return nil, fmt.Errorf("upload pack manifest: %w", err)
	} else if uploaded {
		keys = append(keys, packKey)
	}

	summaryKey, err := artifacts.RenderVariantPublishSummaryKey(id, variant)
	if err != nil {
		return nil, err
	}
	if uploaded, err := uploadOptionalFile(store, summaryKey, result.SummaryPath); err != nil {
		return nil, fmt.Errorf("upload publish summary: %w", err)
	} else if uploaded {
		keys = append(keys, summaryKey)
	}

	galleryKey, err := artifacts.RenderVariantGalleryKey(id, variant)
	if err != nil {
		return nil, err
	}
	if uploaded, err := uploadOptionalFile(store, galleryKey, result.GalleryPath); err != nil {
		return nil, fmt.Errorf("upload gallery: %w", err)
	} else if uploaded {
		keys = append(keys, galleryKey)
	}

	for _, short := range result.Shorts {
		name := short.SegmentID
		if name == "" {
			continue
		}
		videoPath := short.PublishPath
		if videoPath == "" {
			videoPath = short.Output
		}
		if videoPath != "" {
			videoKey, err := artifacts.RenderVariantVideoKey(id, variant, name)
			if err != nil {
				return nil, err
			}
			if uploaded, err := uploadOptionalFile(store, videoKey, videoPath); err != nil {
				return nil, fmt.Errorf("upload render video %s: %w", name, err)
			} else if uploaded {
				keys = append(keys, videoKey)
			}
		}
		if short.CoverPath != "" {
			coverKey, err := artifacts.RenderVariantCoverKey(id, variant, name)
			if err != nil {
				return nil, err
			}
			if uploaded, err := uploadOptionalFile(store, coverKey, short.CoverPath); err != nil {
				return nil, fmt.Errorf("upload render cover %s: %w", name, err)
			} else if uploaded {
				keys = append(keys, coverKey)
			}
		}
		if short.CaptionPath != "" {
			captionKey, err := artifacts.RenderVariantCaptionKey(id, variant, name)
			if err != nil {
				return nil, err
			}
			if uploaded, err := uploadOptionalFile(store, captionKey, short.CaptionPath); err != nil {
				return nil, fmt.Errorf("upload render caption %s: %w", name, err)
			} else if uploaded {
				keys = append(keys, captionKey)
			}
		}
		if short.RenderLogPath != "" {
			logKey, err := artifacts.RenderVariantLogKey(id, variant, name+"-render")
			if err != nil {
				return nil, err
			}
			if uploaded, err := uploadOptionalFile(store, logKey, short.RenderLogPath); err != nil {
				return nil, fmt.Errorf("upload render log %s: %w", name, err)
			} else if uploaded {
				keys = append(keys, logKey)
			}
		}
	}

	if result.Error == "" && len(result.Shorts) == 0 {
		return nil, fmt.Errorf("render result has no shorts")
	}
	return keys, nil
}

func recordingSegmentIDs(result recording.RecordingResult) []string {
	seen := map[string]bool{}
	var ids []string
	for _, segment := range result.Plan.Segments {
		if segment.ID == "" || seen[segment.ID] {
			continue
		}
		seen[segment.ID] = true
		ids = append(ids, segment.ID)
	}
	return ids
}

func decodeStoredRecordingResult(store storage.Storage, id uuid.UUID) (recording.RecordingResult, error) {
	rc, err := store.Open(artifacts.RecordingResultKey(id))
	if err != nil {
		return recording.RecordingResult{}, fmt.Errorf("open recording result: %w", err)
	}
	defer rc.Close()

	var result recording.RecordingResult
	if err := json.NewDecoder(rc).Decode(&result); err != nil {
		return recording.RecordingResult{}, fmt.Errorf("decode recording result: %w", err)
	}
	return result, nil
}

func readStoredRecordingResult(store storage.Storage, id uuid.UUID) (recording.RecordingResult, error) {
	result, err := decodeStoredRecordingResult(store, id)
	if err != nil {
		return recording.RecordingResult{}, err
	}
	if result.Error != "" {
		return recording.RecordingResult{}, fmt.Errorf("recording result error: %s", result.Error)
	}
	return result, nil
}

func recordingOutputsReady(store storage.Storage, id uuid.UUID) (bool, []string, error) {
	resultKey := artifacts.RecordingResultKey(id)
	exists, err := store.Exists(resultKey)
	if err != nil || !exists {
		return false, nil, err
	}
	result, err := decodeStoredRecordingResult(store, id)
	if err != nil || result.Error != "" {
		return false, nil, err
	}

	scriptKey := artifacts.RecordingScriptKey(id)
	scriptExists, err := store.Exists(scriptKey)
	if err != nil || !scriptExists {
		return false, nil, err
	}

	keys := []string{resultKey, scriptKey}
	segments := 0
	for _, artifact := range result.Artifacts {
		if artifact.Role != "segment" || artifact.Type != "video" || artifact.SegmentID == "" {
			continue
		}
		key, err := artifacts.SegmentClipKey(id, artifact.SegmentID)
		if err != nil {
			return false, nil, err
		}
		exists, err := store.Exists(key)
		if err != nil || !exists {
			return false, nil, err
		}
		keys = append(keys, key)
		segments++
	}
	return segments > 0, keys, nil
}

func compositionOutputsReady(store storage.Storage, id uuid.UUID) (bool, []string, error) {
	resultKey := artifacts.CompositionResultKey(id)
	finalKey := artifacts.FinalMP4Key(id)
	resultExists, err := store.Exists(resultKey)
	if err != nil || !resultExists {
		return false, nil, err
	}
	finalExists, err := store.Exists(finalKey)
	if err != nil || !finalExists {
		return false, nil, err
	}

	rc, err := store.Open(resultKey)
	if err != nil {
		return false, nil, fmt.Errorf("open composition result: %w", err)
	}
	defer rc.Close()
	var result composition.Result
	if err := json.NewDecoder(rc).Decode(&result); err != nil {
		return false, nil, fmt.Errorf("decode composition result: %w", err)
	}
	if result.Error != "" {
		return false, nil, nil
	}
	return true, []string{resultKey, finalKey}, nil
}

func renderVariantOutputsReady(store storage.Storage, id uuid.UUID, variant string) (bool, []string, error) {
	resultKey, err := artifacts.RenderVariantResultKey(id, variant)
	if err != nil {
		return false, nil, err
	}
	exists, err := store.Exists(resultKey)
	if err != nil || !exists {
		return false, nil, err
	}
	rc, err := store.Open(resultKey)
	if err != nil {
		return false, nil, fmt.Errorf("open render result: %w", err)
	}
	defer rc.Close()
	var result editor.Result
	if err := json.NewDecoder(rc).Decode(&result); err != nil {
		return false, nil, fmt.Errorf("decode render result: %w", err)
	}
	if result.Error != "" {
		return false, nil, nil
	}
	keys := []string{resultKey}
	for _, keyFn := range []func(uuid.UUID, string) (string, error){
		artifacts.RenderVariantPackManifestKey,
		artifacts.RenderVariantGalleryKey,
	} {
		key, err := keyFn(id, variant)
		if err != nil {
			return false, nil, err
		}
		exists, err := store.Exists(key)
		if err != nil || !exists {
			return false, nil, err
		}
		keys = append(keys, key)
	}
	return true, keys, nil
}

func localizeSegmentClips(store storage.Storage, id uuid.UUID, workDir string, result *recording.RecordingResult) error {
	localized := 0
	for i := range result.Artifacts {
		artifact := &result.Artifacts[i]
		if artifact.Role != "segment" || artifact.Type != "video" || artifact.SegmentID == "" {
			continue
		}
		localPath := filepath.Join(workDir, "segments", artifact.SegmentID+".mp4")
		key, err := artifacts.SegmentClipKey(id, artifact.SegmentID)
		if err != nil {
			return err
		}
		if err := copyStorageToFile(store, key, localPath); err != nil {
			return fmt.Errorf("localize segment %s: %w", artifact.SegmentID, err)
		}
		artifact.Path = localPath
		localized++
	}
	if localized == 0 {
		return fmt.Errorf("recording result has no segment clips")
	}
	return nil
}

func readJSONFile(path string, dst any) error {
	// #nosec G304 -- worker JSON paths are generated inside the stage work directory.
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func putJSONToStorage(store storage.Storage, key string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return store.Put(key, bytes.NewReader(append(b, '\n')))
}
