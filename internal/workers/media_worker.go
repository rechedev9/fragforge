package workers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/captions"
	"github.com/rechedev9/fragforge/internal/composition"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/generateintent"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/mediafont"
	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

const defaultMediaWorkerTimeout = "20m"

// errStreamRenderParentPromotion marks the narrow failure window after every
// render artifact and the authoritative rendered state are durable, but before
// the parent stream job can be promoted. The completion state must survive so
// startup reconciliation can finish that promotion after a restart.
var errStreamRenderParentPromotion = errors.New("stream render completed but parent status promotion failed")

var errStaleGenerateHandoff = errors.New("generate render handoff no longer owns the active run")

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

// Enqueuer is the desktop queue contract the record worker uses to chain a
// render after successful capture. The transition runs atomically with queue
// admission and receives a later non-nil decision if shutdown discards pending
// work; a nil Enqueuer disables chaining for the manual record path.
type Enqueuer interface {
	Enqueue(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error)
	EnqueueWithTransition(*asynq.Task, func(error) error, ...asynq.Option) (*asynq.TaskInfo, error)
}

// chainedRenderUniqueTTL deduplicates a chained render against a render the user
// may also have launched manually for the same job and variant within the day.
const chainedRenderUniqueTTL = 24 * time.Hour

// markFailed records a job's terminal failure on a fresh, short-lived context
// so the write survives a handler context already cancelled by an Asynq
// deadline or shutdown (pgxpool.Exec refuses to run on a cancelled context).
// The secondary error is logged rather than discarded: a job stranded in a
// non-terminal status is otherwise invisible to operators.
func markFailed(repo statusUpdater, id uuid.UUID, reason string) error {
	ctx, cancel := context.WithTimeout(context.Background(), failureWriteTimeout)
	defer cancel()
	if err := repo.UpdateStatus(ctx, id, job.StatusFailed, reason); err != nil {
		logWorkerError(id, "mark failed", err)
		return err
	}
	return nil
}

// recordTaskFailure records a job's failure, but only when the current Asynq
// attempt is terminal (returning the error now archives the task instead of
// scheduling another retry). For a retryable task an intermediate failure is
// left as the in-progress status so the job does not flap StatusFailed<->in
// progress across retries; the terminal failure is recorded once retries are
// exhausted.
func recordTaskFailure(ctx context.Context, repo statusUpdater, id uuid.UUID, taskType string, err error) error {
	if !taskIsTerminal(ctx) {
		logWorkerError(id, taskType+" will retry", err)
		return nil
	}
	if markErr := markFailed(repo, id, err.Error()); markErr != nil {
		return markErr
	}
	recordWorkerFailure(id, taskType, err)
	logWorkerTransition(id, taskType, job.StatusFailed)
	return nil
}

// taskIsTerminal reports whether the current Asynq attempt is the last one, so
// returning an error archives the task instead of retrying. Outside an Asynq
// task context (e.g. direct unit tests) it returns true so a failure is still
// recorded.
func taskIsTerminal(ctx context.Context) bool {
	if retried, maxRetry, ok := tasks.TaskAttempt(ctx); ok {
		return isTerminalAttempt(retried, maxRetry, true)
	}
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
	// MusicDir holds catalog tracks named "<key>.<ext>" that an edit plan's
	// MusicPlan can mix under the clip audio (same directory the songs API and
	// the reel render worker use).
	MusicDir string
	// XAIAPIKey configures the only supported stream-caption transcription pass
	// (internal/captions.XAITranscriber):
	// the worker extracts the selected source-audio range to speech-oriented
	// WAV, then xAI transcribes it with word-level timestamps (see
	// NewStreamRenderWorker).
	XAIAPIKey string
	//
	// A render honours EditPlan.Captions.Enabled only when xAI is configured;
	// the HTTP layer rejects a render start otherwise (see
	// requireCaptionsEnabled), but the worker still checks defensively since it
	// may run a redriven task from before the config changed.
}

// RecordWorker handles the "record:demo" Asynq task.
type RecordWorker struct {
	repo            StatusRepository
	storage         storage.Storage
	generateIntents *generateintent.Store
	cfg             RecordWorkerConfig
	runner          commandRunner
	enqueuer        Enqueuer
	// jobLocks serializes recording per job so two reels for the same job (each a
	// distinct, non-deduped task with different segment ids) never launch the
	// recorder concurrently or race on the job-level recording result. Process-
	// local: it covers the inline queue and a single orchestrator process.
	jobLocks sync.Map // uuid.UUID -> *sync.Mutex
}

// lockJob acquires the per-job recording lock and returns its release func.
func (w *RecordWorker) lockJob(id uuid.UUID) func() {
	m, _ := w.jobLocks.LoadOrStore(id, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func NewRecordWorker(repo StatusRepository, store storage.Storage, cfg RecordWorkerConfig) *RecordWorker {
	return &RecordWorker{
		repo:            repo,
		storage:         store,
		generateIntents: generateintent.New(store),
		cfg:             cfg,
		runner:          execCommandRunner{},
	}
}

// UseGenerateIntentStore shares guided-generate synchronization with the HTTP
// admission path. It is set once at startup before queue processing begins.
func (w *RecordWorker) UseGenerateIntentStore(store *generateintent.Store) {
	w.generateIntents = store
}

// UseEnqueuer wires the task queue the worker uses to chain a render after a
// successful capture. It is set once at startup, before the queue begins
// processing tasks, so no in-flight handler observes a half-set field.
func (w *RecordWorker) UseEnqueuer(e Enqueuer) {
	w.enqueuer = e
}

func (w *RecordWorker) HandleRecordDemo(ctx context.Context, t *asynq.Task) (retErr error) {
	var payload tasks.RecordDemoPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	var (
		generateIntent    renderplan.GenerateIntent
		hasGenerateIntent bool
		err               error
	)
	// Once the payload identifies the durable job, every terminal failure must
	// close that job's active generate/record lifecycle. In particular, early
	// repository and task-header errors happen before the recorder call and were
	// previously able to leave a guided generate request pending forever.
	defer func() {
		if retErr == nil {
			return
		}
		if hasGenerateIntent && taskIsTerminal(ctx) {
			_, err := w.generateIntents.Finish(payload.JobID, generateIntent.ActiveRunID, func() error {
				return recordTaskFailure(ctx, w.repo, payload.JobID, tasks.TypeRecordDemo, retErr)
			})
			if err != nil {
				logWorkerError(payload.JobID, "finish failed generate task", err)
			}
			return
		}
		_ = recordTaskFailure(ctx, w.repo, payload.JobID, tasks.TypeRecordDemo, retErr)
	}()
	generateIntent, hasGenerateIntent, err = tasks.GenerateIntentFromTask(t)
	if err != nil {
		return fmt.Errorf("decode record task generate intent: %w", err)
	}

	j, err := w.repo.Get(ctx, payload.JobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", payload.JobID, err)
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusRecording, ""); err != nil {
		return fmt.Errorf("mark recording: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeRecordDemo, job.StatusRecording)

	if err := w.record(ctx, j, payload.HUDMode, payload.SegmentIDs, payload.PortraitSafeKillfeed); err != nil {
		return err
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusRecorded, ""); err != nil {
		return fmt.Errorf("mark recorded: %w", err)
	}
	logWorkerTransition(j.ID, tasks.TypeRecordDemo, job.StatusRecorded)
	// A guided generate task carries its own immutable render intent, so another
	// accepted capture cannot change the treatment this capture chains. A
	// chaining failure must not fail capture; manual render remains a fallback.
	w.chainRender(j.ID, generateIntent, hasGenerateIntent)
	return nil
}

// chainRender enqueues a generate task's render intent after capture. It is
// best effort: every failure is logged and swallowed so successful capture is
// never reported as failed.
func (w *RecordWorker) chainRender(id uuid.UUID, intent renderplan.GenerateIntent, hasIntent bool) {
	if !hasIntent {
		return
	}
	if w.enqueuer == nil {
		w.failGenerateHandoff(id, intent, errors.New("render queue is not configured"))
		return
	}
	task, err := tasks.NewRenderVariantTask(id, intent.Variant, intent.MusicKey, intent.Edit)
	if err != nil {
		w.failGenerateHandoff(id, intent, fmt.Errorf("build chained render task: %w", err))
		return
	}
	admitted := false
	_, err = w.enqueuer.EnqueueWithTransition(task, func(decision error) error {
		switch {
		case decision == nil:
			// Publish Queued before the task is visible so a crash anywhere in the
			// handoff remains recoverable by the startup render-state sweep. If
			// completing the generate marker then fails, compensate Queued to
			// Failed before rejecting admission so the live UI is not stranded.
			owned, handoffErr := w.generateIntents.Finish(id, intent.ActiveRunID, func() error {
				return w.writeQueuedRenderState(id, intent.Variant)
			})
			if !owned {
				return errStaleGenerateHandoff
			}
			if handoffErr != nil {
				failedErr := w.writeFailedRenderState(id, intent.Variant, fmt.Sprintf("accept render handoff: %v", handoffErr))
				if failedErr != nil {
					return errors.Join(handoffErr, failedErr)
				}
				return errors.Join(handoffErr, w.completeGenerateIntent(id, intent.ActiveRunID))
			}
			admitted = true
			return nil
		case errors.Is(decision, asynq.ErrDuplicateTask):
			// Another task owns the render and its state; never downgrade it.
			_, err := w.generateIntents.Finish(id, intent.ActiveRunID, nil)
			return err
		default:
			if admitted {
				return w.writeFailedRenderState(id, intent.Variant, fmt.Sprintf("enqueue render: %v", decision))
			}
			_, err := w.generateIntents.Finish(id, intent.ActiveRunID, func() error {
				return w.writeFailedRenderState(id, intent.Variant, fmt.Sprintf("enqueue render: %v", decision))
			})
			return err
		}
	}, asynq.Unique(chainedRenderUniqueTTL))
	if err != nil {
		if errors.Is(err, asynq.ErrDuplicateTask) {
			logWorkerTransition(id, tasks.TypeRenderVariant, job.StatusRecorded)
			return
		}
		logWorkerError(id, "enqueue chained render", err)
		return
	}
	logWorkerTransition(id, tasks.TypeRenderVariant, job.StatusRecorded)
}

func (w *RecordWorker) failGenerateHandoff(id uuid.UUID, intent renderplan.GenerateIntent, cause error) {
	logWorkerError(id, "generate render handoff", cause)
	_, err := w.generateIntents.Finish(id, intent.ActiveRunID, func() error {
		return w.writeFailedRenderState(id, intent.Variant, cause.Error())
	})
	if err != nil {
		logWorkerError(id, "finish failed generate handoff", err)
	}
}

func (w *RecordWorker) completeGenerateIntent(id, runID uuid.UUID) error {
	return w.generateIntents.Complete(id, runID)
}

func (w *RecordWorker) writeQueuedRenderState(id uuid.UUID, variant string) error {
	return w.writeRenderState(id, variant, renderplan.RenderVariantStatusQueued, "")
}

func (w *RecordWorker) writeFailedRenderState(id uuid.UUID, variant, message string) error {
	return w.writeRenderState(id, variant, renderplan.RenderVariantStatusFailed, message)
}

func (w *RecordWorker) writeRenderState(id uuid.UUID, variant, status, message string) error {
	loadout, err := renderplan.LoadoutForVariant(variant)
	if err != nil {
		return err
	}
	state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:   id,
		Loadout: loadout,
		Status:  status,
		Error:   message,
	})
	if err != nil {
		return err
	}
	key, err := renderplan.RenderVariantStateKey(id, variant)
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return w.storage.Put(key, bytes.NewReader(b))
}

func (w *RecordWorker) record(ctx context.Context, j job.Job, hudMode string, segmentIDs []string, portraitSafeKillfeed bool) error {
	if j.KillPlan == nil {
		return fmt.Errorf("job %s has no kill plan", j.ID)
	}
	// Serialize recording per job: two reels for the same job (each a distinct,
	// non-deduped task with different segment ids) must not launch the recorder
	// concurrently or race on the job-level recording result.
	defer w.lockJob(j.ID)()

	// A reel records only its selected segment(s); an empty selection means the
	// whole kill plan (the CLI all-kills default). Resolve to concrete ids so
	// readiness, plan filtering, and accumulation all agree on the same set.
	requested := segmentIDs
	if len(requested) == 0 {
		requested = killPlanSegmentIDs(j.KillPlan)
	}
	// Scope the plan handed to the recorder, so everything downstream (HLAE script,
	// take mapping, recording result, render) derives from the chosen segments
	// alone. j is a value copy, so j.KillPlan stays the full plan for ordering.
	recordPlan, err := filterKillPlanSegments(j.KillPlan, requested)
	if err != nil {
		return err
	}
	// Persist the ordered segment ids this reel captures so the job poll scopes
	// capture progress to this reel, not the whole kill plan. Overwritten at the
	// start of every record task (last writer wins - it is the in-flight reel).
	if err := putCaptureSelection(w.storage, j.ID, killPlanSegmentIDs(recordPlan)); err != nil {
		return fmt.Errorf("persist capture selection: %w", err)
	}

	cfg := w.cfg.withDefaults()
	// A per-job preset HUD (e.g. "Clean POV") overrides the worker default.
	if hudMode != "" {
		cfg.HUDMode = hudMode
	}
	effectivePortraitSafeKillfeed := portraitSafeKillfeed && cfg.HUDMode == string(recording.HUDModeDeathnotices)
	expectedStream, err := normalizedRecordingStream(recordPlan, cfg.HUDMode, effectivePortraitSafeKillfeed)
	if err != nil {
		return fmt.Errorf("build recording profile: %w", err)
	}
	ready, keys, err := recordingOutputsReady(w.storage, j.ID, requested, expectedStream)
	if err != nil {
		return err
	}
	if ready {
		logWorkerSkip(j.ID, tasks.TypeRecordDemo, keys)
		return nil
	}

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
	if err := writeJSONFile(killPlanPath, recordPlan); err != nil {
		return fmt.Errorf("write kill plan: %w", err)
	}

	// Snapshot the previously stored result (earlier reels of this job) under the
	// per-job lock, before the upload below overwrites it.
	prev, hasPrev, err := tryDecodeStoredRecordingResult(w.storage, j.ID)
	if err != nil {
		return err
	}

	// Upload each segment clip to durable storage as the recorder finishes it,
	// so the job poll reports live capture progress mid-run. The watcher is
	// owned here: cancelled the moment the recorder exits and waited for before
	// results are read. Best-effort by construction — it can never fail the
	// task, and uploadRecordingOutputs below re-uploads every clip as the
	// authoritative reconciliation pass.
	watchCtx, stopWatch := context.WithCancel(ctx)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		newSegmentClipWatcher(w.storage, j.ID, filepath.Join(outDir, "segments")).watch(watchCtx, segmentWatchInterval)
	}()

	recorderArgs := []string{
		"--killplan", killPlanPath,
		"--demo", demoPath,
		"--out", outDir,
		"--hlae", cfg.HLAEPath,
		"--cs2", cfg.CS2Path,
		"--hud", cfg.HUDMode,
		"--timeout", cfg.Timeout,
	}
	if portraitSafeKillfeed && cfg.HUDMode == string(recording.HUDModeDeathnotices) {
		recorderArgs = append(recorderArgs, "--portrait-safe-killfeed")
	}
	_, runErr := w.runner.Run(ctx, cfg.RecorderPath, recorderArgs...)
	stopWatch()
	<-watchDone

	resultPath := filepath.Join(outDir, "recording-result.json")
	var result recording.RecordingResult
	if err := readJSONFile(resultPath, &result); err != nil {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("read recording result: %w", err)
	}
	var resultErr error
	if runErr == nil {
		resultErr = recording.ValidateRunResult(result)
		if resultErr == nil {
			result.CaptureRevision = uuid.NewString()
			if err := writeJSONFile(resultPath, result); err != nil {
				return fmt.Errorf("write recording revision: %w", err)
			}
		}
	}
	keys, err = uploadRecordingOutputs(w.storage, j.ID, outDir, resultPath, result)
	if err != nil {
		return err
	}
	logWorkerArtifacts(j.ID, tasks.TypeRecordDemo, keys)

	// Accumulate across reels. The recorder result is a single job-level file, but
	// reels are recorded one segment at a time, and uploadRecordingOutputs just
	// overwrote it with only this run's segments. On success, union this run over
	// the prior result (ordered by the kill plan) so result.json lists every
	// recorded segment. On failure, restore the prior result so a flaky capture
	// never drops an already-recorded reel. Clips live under per-segment keys and
	// survive either way.
	if hasPrev {
		durable := prev
		if runErr == nil && resultErr == nil {
			durable = result
			if recordingProfilesCompatible(prev, result) {
				durable = mergeRecordingResults(prev, result, j.KillPlan)
			}
		}
		if err := putRecordingResult(w.storage, j.ID, durable); err != nil {
			return fmt.Errorf("persist recording result: %w", err)
		}
	}

	if runErr != nil {
		return runErr
	}
	if resultErr != nil {
		return resultErr
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
	// transcribe runs the xAI captions pass, with a seam for unit tests.
	transcribe func(ctx context.Context, mediaPath, workDir, language string) ([]captions.WordCue, error)
}

func NewStreamRenderWorker(repo StreamRenderRepository, store storage.Storage, cfg StreamRenderWorkerConfig) *StreamRenderWorker {
	w := &StreamRenderWorker{
		repo:    repo,
		storage: store,
		cfg:     cfg,
		runner:  execCommandRunner{},
	}
	// Stream captions intentionally use xAI only. An unusable xAI transcript
	// publishes without captions rather than substituting text from another
	// engine; a transport/auth failure remains a hard render error.
	w.transcribe = func(ctx context.Context, mediaPath, workDir, language string) ([]captions.WordCue, error) {
		x := captions.XAITranscriber{APIKey: w.cfg.XAIAPIKey, Language: language}
		return transcribeCaptionsWithXAI(ctx, mediaPath, workDir, x.Transcribe)
	}
	return w
}

// singleLine flattens an error for a one-line render warning.
func singleLine(err error) string {
	return strings.Join(strings.Fields(err.Error()), " ")
}

// transcribeCaptionsWithXAI validates the single supported backend's result.
// An unusable transcript remains a soft error for burnClipCaptions; a dead
// context is always hard, even if xAI happened to return unusable cues while
// cancellation was propagating.
func transcribeCaptionsWithXAI(ctx context.Context, mediaPath, workDir string, transcribe func(context.Context, string, string) ([]captions.WordCue, error)) ([]captions.WordCue, error) {
	cues, err := transcribe(ctx, mediaPath, workDir)
	if err == nil {
		// Gameplay audio can make speech-to-text hallucinate; a real bad result
		// stretched two words across 11.8s of a 15s clip. Nothing reaches the burn
		// step without passing the shared transcript-quality gate.
		err = captions.ValidateTranscript(cues)
	}
	if err != nil {
		err = fmt.Errorf("xai: %w", err)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		if err == nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("%v: %w", err, ctxErr)
	}
	return cues, err
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
		if errors.Is(err, errStreamRenderParentPromotion) {
			logWorkerError(j.ID, tasks.TypeRenderStreamClip, err)
			return err
		}
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
	if _, ok := streamclips.VariantByName(variant); !ok {
		return fmt.Errorf("unsupported stream render variant %q (valid variants: %s)", variant, strings.Join(streamclips.VariantNames(), ", "))
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
	if plan.Captions.Enabled && !cfg.captionsConfigured() {
		return fmt.Errorf("edit plan enables captions but xAI is not configured (configure an xAI key in FragForge Studio Settings or set XAI_API_KEY, then restart)")
	}
	bannerFontPath := ""
	if plan.StreamerBanner.Nick != "" || plan.HasTextOverlays() {
		bannerFontPath = streamclips.FindBannerFont()
		if bannerFontPath == "" {
			return fmt.Errorf("render banner or text overlays: embedded font unavailable and no supported fallback font found")
		}
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
	var warnings []string
	musicPath := ""
	if plan.Music.Key != "" {
		if musicPath = resolveMusicFile(cfg.MusicDir, plan.Music.Key); musicPath == "" {
			// Requested music is unavailable; render without it rather than fail.
			warnings = append(warnings, fmt.Sprintf("music %q not found, rendering without music", plan.Music.Key))
		}
	}
	for _, clip := range plan.Clips {
		noticePaths, err := renderClipKillfeedNotices(workDir, clip)
		if err != nil {
			return err
		}
		textPaths, err := writeClipOverlayTexts(workDir, clip)
		if err != nil {
			return err
		}
		outPath := filepath.Join(outDir, clip.ID+".mp4")
		args, err := streamclips.BuildFFmpegArgs(streamclips.FFmpegInputs{
			SourcePath:          sourcePath,
			OutputPath:          outPath,
			MusicPath:           musicPath,
			BannerFontPath:      bannerFontPath,
			SourceHasAudio:      j.Probe.AudioCodec != "",
			KillfeedNoticePaths: noticePaths,
			TextOverlayPaths:    textPaths,
		}, plan, clip)
		if err != nil {
			return err
		}
		if _, err := w.runner.Run(runCtx, cfg.FFmpegPath, args...); err != nil {
			return fmt.Errorf("render clip %s: %w", clip.ID, err)
		}

		publishPath := outPath
		publishClipID := clip.ID
		if plan.Captions.Enabled {
			switch {
			case cfg.xaiConfigured() && j.Probe.AudioCodec == "":
				warnings = append(warnings, fmt.Sprintf("clip %s: source has no audio, publishing without captions", clip.ID))
			case clip.SourceAudioMuted():
				// Captions must describe what the viewer hears; a muted clip
				// would otherwise get subtitles narrating inaudible speech.
				warnings = append(warnings, fmt.Sprintf("clip %s: original audio is muted by the clip edit, publishing without captions", clip.ID))
			default:
				transcriptionPath := outPath
				// Cues transcribed from the rendered clip are already in output
				// time; cues from the source range must be mapped through the
				// speed edit before burning onto the sped-up output.
				cueSpeed := 1.0
				if cfg.xaiConfigured() {
					transcriptionPath, err = w.extractCaptionSourceAudio(runCtx, cfg, workDir, sourcePath, clip)
					if err != nil {
						return err
					}
					cueSpeed = clip.EffectiveSpeed()
				}
				captionedPath, warning, err := w.burnClipCaptions(runCtx, cfg, workDir, outPath, transcriptionPath, cueSpeed, j.ID, variant, clip.ID, plan.Captions.Language)
				if err != nil {
					return err
				}
				switch {
				case captionedPath != "":
					publishPath = captionedPath
					publishClipID = clip.ID + "_captioned"
				case warning != "":
					warnings = append(warnings, warning)
				}
			}
		}

		key, err := streamclips.RenderVideoKey(j.ID, variant, publishClipID)
		if err != nil {
			return err
		}
		if err := uploadFile(w.storage, key, publishPath); err != nil {
			return fmt.Errorf("upload stream clip %s: %w", clip.ID, err)
		}
		videos = append(videos, streamclips.NewVideoEntry(clip, key))
	}

	result, err := streamclips.NewRenderResult(j.ID, variant, videos, time.Now())
	if err != nil {
		return err
	}
	result.Warnings = warnings
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
	if err := w.writeStreamState(j.ID, variant, streamclips.StatusRendered, warnings, "", videos); err != nil {
		return err
	}
	logWorkerArtifacts(j.ID, tasks.TypeRenderStreamClip, []string{resultKey, galleryKey})
	if err := w.repo.UpdateStatus(ctx, j.ID, streamclips.StatusRendered, ""); err != nil {
		return errors.Join(
			errStreamRenderParentPromotion,
			fmt.Errorf("mark stream rendered: %w", err),
		)
	}

	return nil
}

// writeClipOverlayTexts materializes one text file per overlay so drawtext can
// read the user's text verbatim (expansion=none) instead of embedding it in
// the filtergraph, where escaping arbitrary characters is unreliable.
func writeClipOverlayTexts(workDir string, clip streamclips.ClipRange) ([]string, error) {
	if clip.Edit == nil || len(clip.Edit.TextOverlays) == 0 {
		return nil, nil
	}
	dir := filepath.Join(workDir, "overlay-text", clip.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	paths := make([]string, len(clip.Edit.TextOverlays))
	for i, overlay := range clip.Edit.TextOverlays {
		textPath := filepath.Join(dir, fmt.Sprintf("overlay%d.txt", i))
		if err := os.WriteFile(textPath, []byte(overlay.Text), 0o600); err != nil {
			return nil, fmt.Errorf("write text overlay for clip %s overlay %d: %w", clip.ID, i, err)
		}
		paths[i] = textPath
	}
	return paths, nil
}

// renderClipKillfeedNotices renders every kill in clip.KillfeedKills to a
// synthetic CS2 kill-notice PNG under <workDir>/killfeed/<clipID>/cue<i>_<j>.png
// and returns the paths index-aligned with clip.KillfeedSeconds (top-first per
// cue). A cue with no kills gets a nil entry so BuildFFmpegArgs falls back to a
// frozen crop of the killfeed region. Names are deterministic and files are
// overwritten, so a redriven task stays idempotent. It returns nil when the
// clip carries no kills at all.
func renderClipKillfeedNotices(workDir string, clip streamclips.ClipRange) ([][]string, error) {
	if len(clip.KillfeedKills) == 0 {
		return nil, nil
	}
	paths := make([][]string, len(clip.KillfeedSeconds))
	dir := filepath.Join(workDir, "killfeed", clip.ID)
	for i := range clip.KillfeedSeconds {
		if i >= len(clip.KillfeedKills) || len(clip.KillfeedKills[i]) == 0 {
			continue
		}
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, err
		}
		cuePaths := make([]string, len(clip.KillfeedKills[i]))
		for j, kill := range clip.KillfeedKills[i] {
			noticePath := filepath.Join(dir, fmt.Sprintf("cue%d_%d.png", i, j))
			if err := writeKillfeedNoticePNG(noticePath, kill); err != nil {
				return nil, fmt.Errorf("render killfeed notice for clip %s cue %d kill %d: %w", clip.ID, i, j, err)
			}
			cuePaths[j] = noticePath
		}
		paths[i] = cuePaths
	}
	return paths, nil
}

func writeKillfeedNoticePNG(path string, kill streamclips.KillfeedKill) error {
	// #nosec G304 -- path is constructed under the worker stage directory.
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := streamclips.EncodeNoticePNG(kill, f); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// extractCaptionSourceAudio materializes the selected range from the original
// stream source as speech-oriented mono PCM WAV for xAI transcription. xAI
// proved materially more reliable on this input than on the
// already composed/re-encoded vertical MP4, especially when the speaker
// switches between Spanish and English.
func (w *StreamRenderWorker) extractCaptionSourceAudio(ctx context.Context, cfg StreamRenderWorkerConfig, workDir, sourcePath string, clip streamclips.ClipRange) (string, error) {
	dir := filepath.Join(workDir, "caption-source-audio")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create xai caption audio directory: %w", err)
	}
	out := filepath.Join(dir, clip.ID+".wav")
	duration := clip.EndSeconds - clip.StartSeconds
	args := []string{
		"-y",
		"-ss", strconv.FormatFloat(clip.StartSeconds, 'f', 3, 64),
		"-t", strconv.FormatFloat(duration, 'f', 3, 64),
		"-i", sourcePath,
		"-map", "0:a:0",
		"-vn", "-sn", "-dn",
		"-c:a", "pcm_s16le",
		"-ac", "1",
		"-ar", "16000",
		out,
	}
	if _, err := w.runner.Run(ctx, cfg.FFmpegPath, args...); err != nil {
		return "", fmt.Errorf("extract source audio for xai captions on clip %s: %w", clip.ID, err)
	}
	return out, nil
}

// burnClipCaptions transcribes transcriptionPath with xAI and burns the
// resulting karaoke captions into a second copy, <clipID>_captioned.mp4, next to
// clipPath. It returns the captioned file's path on success. If xAI produces an
// unusable transcript, it returns ("", warning, nil) so the caller
// publishes the uncaptioned clip instead of failing the render; any other
// transcription or burn failure is returned as an error since captions were
// explicitly requested and a transcription backend is configured.
func (w *StreamRenderWorker) burnClipCaptions(ctx context.Context, cfg StreamRenderWorkerConfig, workDir, clipPath, transcriptionPath string, cueSpeed float64, id uuid.UUID, variant, clipID, language string) (captionedPath, warning string, err error) {
	cues, err := w.transcribe(ctx, transcriptionPath, workDir, language)
	if err != nil {
		if errors.Is(err, captions.ErrUnusableTranscript) {
			return "", fmt.Sprintf("clip %s: no usable transcript (%s), publishing without captions", clipID, singleLine(err)), nil
		}
		return "", "", fmt.Errorf("transcribe clip %s: %w", clipID, err)
	}
	if cueSpeed != 1 {
		for i := range cues {
			cues[i].StartSeconds /= cueSpeed
			cues[i].EndSeconds /= cueSpeed
		}
	}
	sort.SliceStable(cues, func(i, j int) bool {
		return cues[i].StartSeconds < cues[j].StartSeconds
	})
	fontPath, err := mediafont.Materialize()
	if err != nil {
		return "", "", fmt.Errorf("materialize caption font for clip %s: %w", clipID, err)
	}

	assContent, err := captions.BuildASS(cues, captions.DefaultStyle())
	if err != nil {
		return "", "", fmt.Errorf("build captions for clip %s: %w", clipID, err)
	}
	assPath := filepath.Join(workDir, "captions", clipID+".ass")
	if err := os.MkdirAll(filepath.Dir(assPath), 0o750); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(assPath, []byte(assContent), 0o600); err != nil {
		return "", "", fmt.Errorf("write captions for clip %s: %w", clipID, err)
	}
	captionKey, err := streamclips.RenderCaptionKey(id, variant, clipID)
	if err != nil {
		return "", "", err
	}
	if err := uploadFile(w.storage, captionKey, assPath); err != nil {
		return "", "", fmt.Errorf("upload captions for clip %s: %w", clipID, err)
	}

	out := filepath.Join(filepath.Dir(clipPath), clipID+"_captioned.mp4")
	args := []string{
		"-y",
		"-i", clipPath,
		"-vf", captions.BurnFilter(assPath, filepath.Dir(fontPath)),
		"-c:v", "libx264",
		"-preset", defaultStreamCaptionPreset,
		"-crf", strconv.Itoa(defaultStreamCaptionCRF),
		"-c:a", "copy",
		out,
	}
	if _, err := w.runner.Run(ctx, cfg.FFmpegPath, args...); err != nil {
		return "", "", fmt.Errorf("burn captions for clip %s: %w", clipID, err)
	}
	return out, "", nil
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

// xaiConfigured reports whether an xAI API key is set, the minimum needed to
// run the xAI cloud captions transcription pass.
func (c StreamRenderWorkerConfig) xaiConfigured() bool {
	return c.XAIAPIKey != ""
}

// captionsConfigured reports whether the only supported stream-caption
// backend is configured.
func (c StreamRenderWorkerConfig) captionsConfigured() bool {
	return c.xaiConfigured()
}

// Encoder settings for the captions burn-in pass, matching the first render
// pass (streamclips.BuildFFmpegArgs) so a captioned clip's video quality is
// consistent with its uncaptioned counterpart.
const (
	defaultStreamCaptionPreset = "slow"
	defaultStreamCaptionCRF    = 18
)

// streamStatusUpdater is the single method markStreamFailed needs; every
// stream repository the workers use (render, acquire) satisfies it.
type streamStatusUpdater interface {
	UpdateStatus(ctx context.Context, id uuid.UUID, s streamclips.Status, failureReason string) error
}

func markStreamFailed(repo streamStatusUpdater, id uuid.UUID, reason string) {
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
	if j.KillPlan == nil {
		return fmt.Errorf("job %s has no kill plan", j.ID)
	}
	recordingResult, err := readStoredRecordingResult(w.storage, j.ID)
	if err != nil {
		return err
	}
	cfg := w.cfg.withDefaults()
	musicPath := resolveMusicFile(cfg.MusicDir, musicKey)
	inputFingerprint, err := renderInputFingerprint(recordingResult, j.KillPlan, variant, musicKey, musicPath, edit)
	if err != nil {
		return fmt.Errorf("fingerprint render inputs: %w", err)
	}
	previousState, _, err := w.readRenderVariantState(j.ID, variant)
	if err != nil {
		return fmt.Errorf("read render state: %w", err)
	}
	ready, keys, err := renderVariantOutputsReady(w.storage, j.ID, variant, inputFingerprint)
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
		"--hook=" + strconv.FormatBool(edit.HookText),
		"--kill-counter=" + strconv.FormatBool(edit.KillCounter),
	}
	args = append(args, compileSegmentsArgs(recording.SegmentIDs(recordingResult))...)
	if edit.Intro {
		args = append(args, "--intro")
	}
	if edit.Outro {
		args = append(args, "--outro")
	}
	if edit.IntroText != "" {
		args = append(args, "--intro-text", edit.IntroText)
	}
	if edit.OutroText != "" {
		args = append(args, "--outro-text", edit.OutroText)
	}
	if cfg.FFmpegPath != "" {
		args = append(args, "--ffmpeg", cfg.FFmpegPath)
	}
	if cfg.FFprobePath != "" {
		args = append(args, "--ffprobe", cfg.FFprobePath)
	}
	if musicKey != "" {
		if musicPath != "" {
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
	result.InputFingerprint = inputFingerprint
	if cfg.FFprobePath != "" {
		if err := probeRenderResult(runCtx, w.runner, cfg.FFprobePath, &result); err != nil {
			result.Warnings = append(result.Warnings, "ffprobe quality metadata: "+err.Error())
		}
	}
	if err := writeJSONFile(resultPath, result); err != nil {
		return fmt.Errorf("write fingerprinted render result: %w", err)
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

// compileSegmentsArgs returns the zv-editor flags that compile a render's
// segments into one upload-ready Short. Per CLAUDE.md, a multi-segment
// selection renders as a single concatenated Short (matching the "zv short"
// CLI's --compile-segments behavior); a single segment keeps today's
// per-segment short unchanged.
func compileSegmentsArgs(segmentIDs []string) []string {
	if len(segmentIDs) < 2 {
		return nil
	}
	return []string{"--compile-segments", "--segments", strings.Join(segmentIDs, ",")}
}

// putCaptureSelection persists the ordered segment ids a record run will
// capture, so the job poll can scope capture progress to this reel.
func putCaptureSelection(store storage.Storage, id uuid.UUID, segmentIDs []string) error {
	b, err := json.Marshal(segmentIDs)
	if err != nil {
		return err
	}
	return store.Put(artifacts.CaptureSelectionKey(id), bytes.NewReader(b))
}

// killPlanSegmentIDs lists every segment id in the plan, in plan order.
func killPlanSegmentIDs(plan *killplan.Plan) []string {
	ids := make([]string, 0, len(plan.Segments))
	for _, s := range plan.Segments {
		ids = append(ids, s.ID)
	}
	return ids
}

// filterKillPlanSegments returns a copy of plan containing only the segments
// whose ID is in ids, preserving the plan's segment order (never the request
// order). It errors when the selection matches no segment so a stale request
// never launches the recorder with an empty plan.
func filterKillPlanSegments(plan *killplan.Plan, ids []string) (*killplan.Plan, error) {
	keep := make(map[string]bool, len(ids))
	for _, id := range ids {
		keep[id] = true
	}
	out := *plan
	out.Segments = nil
	for _, s := range plan.Segments {
		if keep[s.ID] {
			out.Segments = append(out.Segments, s)
		}
	}
	if len(out.Segments) == 0 {
		return nil, fmt.Errorf("no kill-plan segments match requested ids %v", ids)
	}
	return &out, nil
}

func isSegmentClip(a recording.RecordingArtifact) bool {
	return a.Role == "segment" && a.Type == "video" && a.SegmentID != ""
}

// tryDecodeStoredRecordingResult returns the stored recording result, reporting
// whether one exists. A missing result is not an error (the first reel of a job).
func tryDecodeStoredRecordingResult(store storage.Storage, id uuid.UUID) (recording.RecordingResult, bool, error) {
	exists, err := store.Exists(recording.ResultArtifactKey(id))
	if err != nil || !exists {
		return recording.RecordingResult{}, false, err
	}
	result, err := decodeStoredRecordingResult(store, id)
	if err != nil {
		return recording.RecordingResult{}, false, err
	}
	return result, true, nil
}

// normalizedRecordingStream resolves the exact stream profile the recorder CLI
// will use for this task. NewPlanFromKillPlan owns default normalization (FPS,
// dimensions, CRF, deathnotice safe zone and lifetime), so worker idempotency
// changes automatically when any output-affecting recorder default changes.
func normalizedRecordingStream(plan *killplan.Plan, hudMode string, portraitSafeKillfeed bool) (recording.StreamConfig, error) {
	stream := recording.DefaultStreamConfig()
	stream.HUDMode = recording.HUDMode(hudMode)
	stream.PortraitSafeKillfeed = portraitSafeKillfeed
	normalized, err := recording.NewPlanFromKillPlan(*plan, "profile.dem", "profile", stream)
	if err != nil {
		return recording.StreamConfig{}, err
	}
	return normalized.Stream, nil
}

func recordingProfilesCompatible(a, b recording.RecordingResult) bool {
	return a.Plan.Stream == b.Plan.Stream
}

// mergeRecordingResults unions a freshly recorded result over a previously
// stored one so the job-level recording result accumulates every segment any
// reel has recorded. The new run wins for segments it covers; segments only in
// prev are carried forward (their clips still live in storage under their
// per-segment keys). The merged segments are reordered to follow fullPlan so the
// result stays in kill-plan order regardless of which reel recorded when.
func mergeRecordingResults(prev, next recording.RecordingResult, fullPlan *killplan.Plan) recording.RecordingResult {
	merged := next
	haveSegment := make(map[string]bool, len(next.Plan.Segments))
	for _, s := range next.Plan.Segments {
		haveSegment[s.ID] = true
	}
	for _, s := range prev.Plan.Segments {
		if !haveSegment[s.ID] {
			merged.Plan.Segments = append(merged.Plan.Segments, s)
		}
	}
	order := make(map[string]int, len(fullPlan.Segments))
	for i, s := range fullPlan.Segments {
		order[s.ID] = i
	}
	sort.SliceStable(merged.Plan.Segments, func(a, b int) bool {
		return order[merged.Plan.Segments[a].ID] < order[merged.Plan.Segments[b].ID]
	})
	haveClip := make(map[string]bool, len(next.Artifacts))
	for _, a := range next.Artifacts {
		if isSegmentClip(a) {
			haveClip[a.SegmentID] = true
		}
	}
	for _, a := range prev.Artifacts {
		if isSegmentClip(a) && !haveClip[a.SegmentID] {
			merged.Artifacts = append(merged.Artifacts, a)
		}
	}
	return merged
}

// putRecordingResult overwrites the durable recording result with result.
func putRecordingResult(store storage.Storage, id uuid.UUID, result recording.RecordingResult) error {
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return store.Put(recording.ResultArtifactKey(id), bytes.NewReader(b))
}

// recordingOutputsReady reports whether the recorder can be skipped because
// every requested segment's clip is already present. Callers resolve the
// effective segment ids (whole-demo mode passes every plan segment), so a reel
// scoped to one clip is never wrongly skipped against the job-level result.json,
// which holds only the last run's segments until the accumulate step unions it.
func recordingOutputsReady(store storage.Storage, id uuid.UUID, requested []string, expectedStream recording.StreamConfig) (bool, []string, error) {
	if len(requested) == 0 {
		return false, nil, nil
	}
	resultKey := recording.ResultArtifactKey(id)
	exists, err := store.Exists(resultKey)
	if err != nil || !exists {
		return false, nil, err
	}
	result, err := decodeStoredRecordingResult(store, id)
	if err != nil || result.Error != "" {
		return false, nil, err
	}
	if result.Plan.Stream != expectedStream {
		return false, nil, nil
	}

	recorded := make(map[string]bool)
	for _, a := range result.Artifacts {
		if isSegmentClip(a) {
			recorded[a.SegmentID] = true
		}
	}
	scriptKey := recording.ScriptArtifactKey(id)
	if ok, err := store.Exists(scriptKey); err != nil || !ok {
		return false, nil, err
	}
	keys := []string{resultKey, scriptKey}
	for _, segID := range requested {
		if !recorded[segID] {
			return false, nil, nil
		}
		clipKey, err := recording.SegmentClipArtifactKey(id, segID)
		if err != nil {
			return false, nil, err
		}
		ok, err := store.Exists(clipKey)
		if err != nil || !ok {
			return false, nil, err
		}
		keys = append(keys, clipKey)
	}
	return true, keys, nil
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

type renderMusicInput struct {
	Key       string `json:"key,omitempty"`
	Available bool   `json:"available"`
	SHA256    string `json:"sha256,omitempty"`
}

type renderFingerprintInput struct {
	SchemaVersion string                    `json:"schema_version"`
	Variant       string                    `json:"variant"`
	Edit          renderplan.EditRequest    `json:"edit"`
	Music         renderMusicInput          `json:"music"`
	Recording     recording.RecordingResult `json:"recording"`
	KillPlan      killplan.Plan             `json:"kill_plan"`
}

func renderInputFingerprint(result recording.RecordingResult, plan *killplan.Plan, variant, musicKey, musicPath string, edit renderplan.EditRequest) (string, error) {
	music := renderMusicInput{Key: musicKey, Available: musicPath != ""}
	if musicPath != "" {
		f, err := os.Open(musicPath)
		if err != nil {
			return "", fmt.Errorf("open music: %w", err)
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil {
			return "", fmt.Errorf("hash music: %w", copyErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close music: %w", closeErr)
		}
		music.SHA256 = fmt.Sprintf("%x", h.Sum(nil))
	}
	doc := renderFingerprintInput{
		SchemaVersion: "1.0",
		Variant:       variant,
		Edit:          edit,
		Music:         music,
		Recording:     result,
		KillPlan:      *plan,
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum), nil
}

func renderVariantOutputsReady(store storage.Storage, id uuid.UUID, variant, expectedFingerprint string) (bool, []string, error) {
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
	if result.InputFingerprint == "" || result.InputFingerprint != expectedFingerprint {
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
	// A per-segment render is only reusable if it already produced a short for
	// every segment in the (possibly grown) recording result. When a later reel
	// records an additional segment, this forces a re-render so the new clip gets
	// its short instead of being skipped as "already rendered".
	covers, err := renderCoversRecordedSegments(store, id, result)
	if err != nil {
		return false, nil, err
	}
	if !covers {
		return false, nil, nil
	}
	return true, keys, nil
}

// compilationSegmentID is the synthetic segment id the editor uses for a single
// all-kills compilation short (see internal/editor/manifest.go).
const compilationSegmentID = "demo-compilation"

// renderCoversRecordedSegments reports whether render already has a short for
// every recorded segment that actually has a clip. Coverage is measured against
// clip-bearing artifacts, not all plan segments, because the editor only emits a
// short for segments with a recorded clip - a plan segment with no clip (a
// partial capture) must not make coverage permanently unsatisfiable. A
// compilation render (one "demo-compilation" short) is always treated as
// covered. An unreadable recording result falls back to covered, leaving the
// key-based readiness check authoritative.
func renderCoversRecordedSegments(store storage.Storage, id uuid.UUID, render editor.Result) (bool, error) {
	exists, err := store.Exists(recording.ResultArtifactKey(id))
	if err != nil || !exists {
		return true, err
	}
	rec, err := decodeStoredRecordingResult(store, id)
	if err != nil {
		return true, nil
	}
	rendered := make(map[string]bool, len(render.Shorts))
	for _, s := range render.Shorts {
		if s.SegmentID == compilationSegmentID {
			return true, nil
		}
		rendered[s.SegmentID] = true
	}
	for _, a := range rec.Artifacts {
		if isSegmentClip(a) && !rendered[a.SegmentID] {
			return false, nil
		}
	}
	return true, nil
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
