package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/reche/zackvideo/internal/artifacts"
	"github.com/reche/zackvideo/internal/composition"
	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/recording"
	"github.com/reche/zackvideo/internal/storage"
	"github.com/reche/zackvideo/internal/tasks"
)

const defaultMediaWorkerTimeout = "20m"

// StatusRepository is the subset of *job.Repository needed by media workers.
type StatusRepository interface {
	Get(ctx context.Context, id uuid.UUID) (job.Job, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, s job.Status, failureReason string) error
}

type commandRunner interface {
	Run(ctx context.Context, exe string, args ...string) ([]byte, error)
}

type execCommandRunner struct{}

func (execCommandRunner) Run(ctx context.Context, exe string, args ...string) ([]byte, error) {
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

	if err := w.record(ctx, j); err != nil {
		_ = w.repo.UpdateStatus(ctx, j.ID, job.StatusFailed, err.Error())
		return err
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusRecorded, ""); err != nil {
		return fmt.Errorf("mark recorded: %w", err)
	}
	return nil
}

func (w *RecordWorker) record(ctx context.Context, j job.Job) error {
	cfg := w.cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	if j.KillPlan == nil {
		return fmt.Errorf("job %s has no kill plan", j.ID)
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
	if err := uploadRecordingOutputs(w.storage, j.ID, outDir, resultPath, result); err != nil {
		return err
	}
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

	j, err := w.repo.Get(ctx, payload.JobID)
	if err != nil {
		return fmt.Errorf("load job %s: %w", payload.JobID, err)
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusComposing, ""); err != nil {
		return fmt.Errorf("mark composing: %w", err)
	}

	if err := w.compose(ctx, j); err != nil {
		_ = w.repo.UpdateStatus(ctx, j.ID, job.StatusFailed, err.Error())
		return err
	}
	if err := w.repo.UpdateStatus(ctx, j.ID, job.StatusComposed, ""); err != nil {
		return fmt.Errorf("mark composed: %w", err)
	}
	return nil
}

func (w *ComposeWorker) compose(ctx context.Context, j job.Job) error {
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

func prepareStageDir(root string, id uuid.UUID, stage string) (string, func(), error) {
	base := root
	cleanup := func() {}
	if base == "" {
		base = os.TempDir()
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
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

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, rc)
	return err
}

func uploadFile(store storage.Storage, key, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return store.Put(key, f)
}

func uploadOptionalFile(store storage.Storage, key, path string) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return uploadFile(store, key, path)
}

func uploadRecordingOutputs(store storage.Storage, id uuid.UUID, outDir, resultPath string, result recording.RecordingResult) error {
	if err := uploadFile(store, artifacts.RecordingResultKey(id), resultPath); err != nil {
		return fmt.Errorf("upload recording result: %w", err)
	}

	scriptPath := result.Script
	if scriptPath == "" {
		scriptPath = filepath.Join(outDir, "recording.js")
	}
	if err := uploadOptionalFile(store, artifacts.RecordingScriptKey(id), scriptPath); err != nil {
		return fmt.Errorf("upload recording script: %w", err)
	}

	uploadedSegments := 0
	for _, artifact := range result.Artifacts {
		if artifact.Role != "segment" || artifact.Type != "video" || artifact.SegmentID == "" {
			continue
		}
		key, err := artifacts.SegmentClipKey(id, artifact.SegmentID)
		if err != nil {
			return err
		}
		if err := uploadFile(store, key, artifact.Path); err != nil {
			return fmt.Errorf("upload segment %s: %w", artifact.SegmentID, err)
		}
		uploadedSegments++
	}
	if result.Error == "" && uploadedSegments == 0 {
		return fmt.Errorf("recording result has no segment clips")
	}
	return nil
}

func readStoredRecordingResult(store storage.Storage, id uuid.UUID) (recording.RecordingResult, error) {
	rc, err := store.Open(artifacts.RecordingResultKey(id))
	if err != nil {
		return recording.RecordingResult{}, fmt.Errorf("open recording result: %w", err)
	}
	defer rc.Close()

	var result recording.RecordingResult
	if err := json.NewDecoder(rc).Decode(&result); err != nil {
		return recording.RecordingResult{}, fmt.Errorf("decode recording result: %w", err)
	}
	if result.Error != "" {
		return recording.RecordingResult{}, fmt.Errorf("recording result error: %s", result.Error)
	}
	return result, nil
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
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
