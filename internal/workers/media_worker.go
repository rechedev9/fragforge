package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

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

// Bounded fan-out for the render worker's per-short I/O. Probing and localizing
// run one external/IO op per short; doing them concurrently (capped) turns an
// N-short serial wait into roughly N/limit while keeping disk and subprocess
// pressure sane on a single BYO box.
const (
	probeConcurrency    = 4
	localizeConcurrency = 6
)

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
	// HUDMode is the in-game HUD the recorder captures with (gameplay, clean, or
	// deathnotices). The viral short wants a HUD-less POV with the deathnotices
	// killfeed, so it defaults to "deathnotices" (see withDefaults).
	HUDMode string
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
	// MusicDir holds music tracks named "<key>.<ext>" that a render can mix in
	// (see RenderVariantPayload.MusicKey). Empty disables music mixing.
	MusicDir string
}

// resolveMusicFile returns the first existing track file for key in dir, or ""
// when dir is unset, key is unsafe, or nothing matches. key is validated
// upstream; the separator check is defence in depth against path traversal.
func resolveMusicFile(dir, key string) string {
	if dir == "" || key == "" || strings.ContainsAny(key, `/\`) || strings.Contains(key, "..") {
		return ""
	}
	for _, ext := range []string{".m4a", ".mp3", ".ogg", ".opus", ".wav", ".aac"} {
		p := filepath.Join(dir, key+ext)
		if info, statErr := os.Stat(p); statErr == nil && !info.IsDir() {
			return p
		}
	}
	return ""
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

	if err := w.record(ctx, j, payload.HUDMode); err != nil {
		recordTaskFailure(ctx, w.repo, j.ID, tasks.TypeRecordDemo, err)
		return err
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusRecorded, ""); err != nil {
		return fmt.Errorf("mark recorded: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeRecordDemo, job.StatusRecorded)
	return nil
}

func (w *RecordWorker) record(ctx context.Context, j job.Job, hudMode string) error {
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
	// A per-job preset HUD (e.g. "Clean POV") overrides the worker default.
	if hudMode != "" {
		cfg.HUDMode = hudMode
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
		"--hud", cfg.HUDMode,
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
	if err := recording.ValidateRunResult(result); err != nil {
		return err
	}
	return nil
}

func (c RecordWorkerConfig) withDefaults() RecordWorkerConfig {
	if c.Timeout == "" {
		c.Timeout = defaultMediaWorkerTimeout
	}
	if c.HUDMode == "" {
		// The viral short is a HUD-less POV with the in-game deathnotices killfeed
		// (the editor crops that killfeed into its overlay), not the full scoreboard
		// HUD the recorder defaults to.
		c.HUDMode = string(recording.HUDModeDeathnotices)
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
	if err := uploadFile(w.storage, composition.ResultArtifactKey(j.ID), resultPath); err != nil {
		return fmt.Errorf("upload composition result: %w", err)
	}
	if runErr != nil {
		return runErr
	}
	if err := composition.ValidateUploadResult(result); err != nil {
		return err
	}
	if err := uploadFile(w.storage, composition.FinalArtifactKey(j.ID), finalPath); err != nil {
		return fmt.Errorf("upload final mp4: %w", err)
	}
	logWorkerArtifacts(j.ID, tasks.TypeComposeFinal, []string{
		composition.ResultArtifactKey(j.ID),
		composition.FinalArtifactKey(j.ID),
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
		videos = append(videos, streamclips.NewVideoEntry(clip, key))
	}

	result, err := streamclips.NewRenderResult(j.ID, variant, videos, time.Now())
	if err != nil {
		return err
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
	if err := w.storage.Put(galleryKey, strings.NewReader(streamclips.RenderGalleryHTML(j, videos))); err != nil {
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
	state, err := streamclips.NewRenderState(id, variant, status, warnings, errMsg, videos)
	if err != nil {
		return err
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
	if err := w.render(ctx, j, variant, payload.MusicKey, payload.Edit); err != nil {
		logWorkerError(j.ID, tasks.TypeRenderVariant, err)
		return err
	}
	return nil
}

func (w *RenderWorker) render(ctx context.Context, j job.Job, variant, musicKey string, edit renderplan.EditRequest) (err error) {
	edit = renderplan.NormalizeEditRequest(edit)
	if err := edit.Validate(); err != nil {
		return err
	}
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
			Error:    renderplan.RenderVariantFailureMessage(result, err),
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
	if err := w.writeEditDocument(outDir, j.ID, loadout, recordingResult, edit); err != nil {
		return err
	}
	args := []string{
		"--recording-result", localRecordingResult,
		"--killplan", localKillPlan,
		"--out", outDir,
		"--publish-dir", publishDir,
		"--preset", loadout.Preset,
		"--output-format", edit.Format,
		"--kill-effect", edit.KillEffect,
		"--transition", edit.Transition,
	}
	if edit.Intro {
		args = append(args, "--intro")
	}
	if edit.Outro {
		args = append(args, "--outro")
	}
	if cfg.FFmpegPath != "" {
		args = append(args, "--ffmpeg", cfg.FFmpegPath)
	}
	if cfg.FFprobePath != "" {
		args = append(args, "--ffprobe", cfg.FFprobePath)
	}
	if musicKey != "" {
		if musicPath := resolveMusicFile(cfg.MusicDir, musicKey); musicPath != "" {
			args = append(args, "--music", musicPath)
		} else {
			// Requested music is unavailable; render without it rather than fail.
			logWorkerError(j.ID, tasks.TypeRenderVariant, fmt.Errorf("music %q not found in %q; rendering without music", musicKey, cfg.MusicDir))
		}
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
	if err := renderplan.ValidateRenderVariantRunResult(result); err != nil {
		return err
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

func (w *RenderWorker) writeEditDocument(outDir string, id uuid.UUID, loadout renderplan.Loadout, result recording.RecordingResult, edit renderplan.EditRequest) error {
	doc, err := renderplan.NewEditDocumentForLoadout(renderplan.NewEditDocumentForLoadoutOptions{
		JobID:      id,
		Loadout:    loadout,
		SegmentIDs: recording.SegmentIDs(result),
		Edit:       edit,
	})
	if err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(outDir, "edit-document.json"), doc)
}

func (w *RenderWorker) readRenderVariantState(id uuid.UUID, variant string) (*renderplan.RenderVariantState, bool, error) {
	key, err := renderplan.RenderVariantStateKey(id, variant)
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
	key, err := renderplan.RenderVariantStateKey(state.JobID, state.Variant)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return w.storage.Put(key, bytes.NewReader(b))
}

func probeRenderResult(ctx context.Context, runner commandRunner, ffprobePath string, result *editor.Result) error {
	// Each short probes an independent file and writes only its own struct, so
	// the probes run concurrently (bounded) and the per-short writes never race.
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	sem := make(chan struct{}, probeConcurrency)
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
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			artifact, err := probeVideoArtifact(ctx, runner, ffprobePath, short.SegmentID, role, path)
			if err != nil {
				artifact = recording.RecordingArtifact{
					SegmentID:  short.SegmentID,
					Role:       role,
					Type:       "video",
					Path:       path,
					ProbeError: err.Error(),
				}
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
			if role == "publish" {
				short.PublishArtifact = artifact
			} else {
				short.OutputArtifact = artifact
			}
		}()
	}
	wg.Wait()
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
	if err := recording.ApplyProbeOutput(&artifact, out); err != nil {
		artifact.ProbeError = err.Error()
		return artifact, err
	}
	return artifact, nil
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
	targets, err := recording.NewUploadTargets(recording.NewUploadTargetsOptions{
		JobID:      id,
		OutDir:     outDir,
		ResultPath: resultPath,
		Result:     result,
	})
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(targets))
	for _, target := range targets {
		uploaded := false
		if target.Required {
			if err := uploadFile(store, target.Key, target.Path); err != nil {
				if target.MissingMessage != "" && os.IsNotExist(err) {
					return nil, errors.New(target.MissingMessage)
				}
				return nil, fmt.Errorf("upload %s: %w", target.Label, err)
			}
			uploaded = true
		} else if ok, err := uploadOptionalFile(store, target.Key, target.Path); err != nil {
			return nil, fmt.Errorf("upload %s: %w", target.Label, err)
		} else {
			uploaded = ok
		}
		if !uploaded {
			continue
		}
		keys = append(keys, target.Key)
	}
	if err := recording.ValidateUploadResult(result); err != nil {
		return nil, err
	}
	return keys, nil
}

func uploadRenderVariantOutputs(store storage.Storage, id uuid.UUID, variant, outDir, publishDir, resultPath string, result editor.Result) ([]string, error) {
	targets, err := renderplan.NewRenderVariantUploadTargets(renderplan.NewRenderVariantUploadTargetsOptions{
		JobID:      id,
		Variant:    variant,
		OutDir:     outDir,
		PublishDir: publishDir,
		ResultPath: resultPath,
		Result:     result,
	})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(targets))
	for _, target := range targets {
		if target.Required {
			if err := uploadFile(store, target.Key, target.Path); err != nil {
				return nil, fmt.Errorf("upload %s: %w", target.Label, err)
			}
			keys = append(keys, target.Key)
			continue
		}
		if uploaded, err := uploadOptionalFile(store, target.Key, target.Path); err != nil {
			return nil, fmt.Errorf("upload %s: %w", target.Label, err)
		} else if uploaded {
			keys = append(keys, target.Key)
		}
	}

	if err := renderplan.ValidateRenderVariantUploadResult(result); err != nil {
		return nil, err
	}
	return keys, nil
}

func decodeStoredRecordingResult(store storage.Storage, id uuid.UUID) (recording.RecordingResult, error) {
	rc, err := store.Open(recording.ResultArtifactKey(id))
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
	if err := recording.ValidateRunResult(result); err != nil {
		return recording.RecordingResult{}, err
	}
	return result, nil
}

func recordingOutputsReady(store storage.Storage, id uuid.UUID) (bool, []string, error) {
	resultKey := recording.ResultArtifactKey(id)
	exists, err := store.Exists(resultKey)
	if err != nil || !exists {
		return false, nil, err
	}
	result, err := decodeStoredRecordingResult(store, id)
	if err != nil || result.Error != "" {
		return false, nil, err
	}

	readyArtifacts, err := recording.NewReadyArtifacts(id, result)
	if err != nil {
		return false, nil, err
	}
	keys := []string{readyArtifacts.ResultKey}
	for _, key := range readyArtifacts.RequiredKeys {
		exists, err := store.Exists(key)
		if err != nil || !exists {
			return false, nil, err
		}
		keys = append(keys, key)
	}
	return readyArtifacts.SegmentCount > 0, keys, nil
}

func compositionOutputsReady(store storage.Storage, id uuid.UUID) (bool, []string, error) {
	resultKey := composition.ResultArtifactKey(id)
	resultExists, err := store.Exists(resultKey)
	if err != nil || !resultExists {
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
	readyArtifacts := composition.NewReadyArtifacts(id, result)
	keys := []string{readyArtifacts.ResultKey}
	for _, key := range readyArtifacts.RequiredKeys {
		exists, err := store.Exists(key)
		if err != nil || !exists {
			return false, nil, err
		}
		keys = append(keys, key)
	}
	return true, keys, nil
}

func renderVariantOutputsReady(store storage.Storage, id uuid.UUID, variant string) (bool, []string, error) {
	readyArtifacts, err := renderplan.NewRenderVariantReadyArtifacts(id, variant)
	if err != nil {
		return false, nil, err
	}
	resultKey := readyArtifacts.ResultKey
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
	for _, key := range readyArtifacts.RequiredKeys {
		exists, err := store.Exists(key)
		if err != nil || !exists {
			return false, nil, err
		}
		keys = append(keys, key)
	}
	return true, keys, nil
}

func localizeSegmentClips(store storage.Storage, id uuid.UUID, workDir string, result *recording.RecordingResult) error {
	localizations, err := recording.NewSegmentClipLocalizations(id, workDir, *result)
	if err != nil {
		return err
	}
	if len(localizations) == 0 {
		return fmt.Errorf("recording result has no segment clips")
	}
	// Each localization copies a distinct clip and updates a distinct artifact
	// index, so the copies run concurrently (bounded) without racing on the slice.
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	sem := make(chan struct{}, localizeConcurrency)
	for _, localization := range localizations {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if copyErr := copyStorageToFile(store, localization.Key, localization.LocalPath); copyErr != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("localize segment %s: %w", localization.SegmentID, copyErr)
				}
				mu.Unlock()
				return
			}
			result.Artifacts[localization.ArtifactIndex].Path = localization.LocalPath
		}()
	}
	wg.Wait()
	return firstErr
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
