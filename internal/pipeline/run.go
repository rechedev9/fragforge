package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/reche/zackvideo/internal/composition"
	"github.com/reche/zackvideo/internal/recording"
)

func Run(ctx context.Context, cfg Config) (Result, error) {
	if err := cfg.validate(); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		return Result{}, err
	}

	recordingDir := filepath.Join(cfg.OutputDir, "recording")
	finalOutput := filepath.Join(cfg.OutputDir, "final.mp4")
	result := Result{
		KillPlan:          cfg.KillPlanPath,
		Demo:              cfg.DemoPath,
		OutputDir:         cfg.OutputDir,
		RecordingDir:      recordingDir,
		RecordingResult:   filepath.Join(recordingDir, "recording-result.json"),
		CompositionResult: filepath.Join(cfg.OutputDir, "composition-result.json"),
		FinalOutput:       finalOutput,
	}

	recordArgs := []string{
		"--killplan", cfg.KillPlanPath,
		"--demo", cfg.DemoPath,
		"--out", recordingDir,
		"--hlae", cfg.HLAEPath,
		"--cs2", cfg.CS2Path,
		"--timeout", cfg.RecordTimeout,
	}
	step, err := runStep(ctx, "record", cfg.RecorderPath, recordArgs...)
	result.Steps = append(result.Steps, step)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	if err := collectRecordingResult(&result); err != nil {
		result.Error = err.Error()
		return result, err
	}

	composeArgs := []string{
		"--recording-result", result.RecordingResult,
		"--out", finalOutput,
		"--timeout", cfg.ComposeTimeout,
	}
	if cfg.FFmpegPath != "" {
		composeArgs = append(composeArgs, "--ffmpeg", cfg.FFmpegPath)
	}
	step, err = runStep(ctx, "compose", cfg.ComposerPath, composeArgs...)
	result.Steps = append(result.Steps, step)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	if err := collectCompositionResult(&result); err != nil {
		result.Error = err.Error()
		return result, err
	}
	return result, nil
}

func WriteResult(path string, result Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func (c Config) validate() error {
	required := map[string]string{
		"killplan": c.KillPlanPath,
		"demo":     c.DemoPath,
		"out":      c.OutputDir,
		"hlae":     c.HLAEPath,
		"cs2":      c.CS2Path,
		"recorder": c.RecorderPath,
		"composer": c.ComposerPath,
	}
	for name, value := range required {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	if c.RecordTimeout == "" {
		return fmt.Errorf("record timeout is required")
	}
	if c.ComposeTimeout == "" {
		return fmt.Errorf("compose timeout is required")
	}
	return nil
}

func runStep(ctx context.Context, name, exe string, args ...string) (StepResult, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, exe, args...)
	output, err := cmd.CombinedOutput()
	step := StepResult{
		Name:            name,
		Command:         append([]string{exe}, args...),
		DurationSeconds: time.Since(start).Seconds(),
		Output:          string(output),
	}
	if err != nil {
		step.Error = err.Error()
		return step, fmt.Errorf("%s step failed: %w", name, err)
	}
	return step, nil
}

func collectRecordingResult(result *Result) error {
	var rr recording.RecordingResult
	if err := readJSON(result.RecordingResult, &rr); err != nil {
		return fmt.Errorf("read recording result: %w", err)
	}
	if rr.Error != "" {
		return fmt.Errorf("recording result error: %s", rr.Error)
	}
	for _, warning := range rr.Warnings {
		if warning == "" {
			continue
		}
		result.Warnings = append(result.Warnings, "recording: "+warning)
	}
	return nil
}

func collectCompositionResult(result *Result) error {
	var cr composition.Result
	if err := readJSON(result.CompositionResult, &cr); err != nil {
		return fmt.Errorf("read composition result: %w", err)
	}
	if cr.Error != "" {
		return fmt.Errorf("composition result error: %s", cr.Error)
	}
	for _, warning := range cr.Warnings {
		if warning == "" {
			continue
		}
		result.Warnings = append(result.Warnings, "composition: "+warning)
	}
	return nil
}

func readJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
