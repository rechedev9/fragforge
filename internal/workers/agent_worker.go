package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/tasks"
)

const defaultAgentWorkerTimeout = "5m"

type AgentWorkerConfig struct {
	WorkDir   string
	CodexPath string
	Model     string
	Timeout   string
}

type AgentWorker struct {
	storage storage.Storage
	cfg     AgentWorkerConfig
	runner  commandRunner
}

func NewAgentWorker(store storage.Storage, cfg AgentWorkerConfig) *AgentWorker {
	return &AgentWorker{storage: store, cfg: cfg, runner: execCommandRunner{}}
}

func (w *AgentWorker) HandleCodexAgent(ctx context.Context, t *asynq.Task) error {
	var payload tasks.CodexAgentPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if payload.Kind != renderplan.AgentKindCaptionCandidates {
		return fmt.Errorf("unknown agent kind %q", payload.Kind)
	}
	cfg := w.cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	if err := w.runCaptionCandidates(ctx, payload.JobID, payload.Variant, payload.Kind, cfg); err != nil {
		logWorkerError(payload.JobID, tasks.TypeCodexAgent, err)
		return err
	}
	return nil
}

func (w *AgentWorker) runCaptionCandidates(ctx context.Context, id uuid.UUID, variant, kind string, cfg AgentWorkerConfig) error {
	contextKey, err := artifacts.RenderVariantAgentContextKey(id, variant, kind)
	if err != nil {
		return err
	}
	resultKey, err := artifacts.RenderVariantAgentResultKey(id, variant, kind)
	if err != nil {
		return err
	}
	momentsKey := artifacts.MomentsKey(id)
	packKey, err := artifacts.RenderVariantPackManifestKey(id, variant)
	if err != nil {
		return err
	}
	var momentsDoc any
	if err := readStoredJSON(w.storage, momentsKey, &momentsDoc); err != nil {
		return fmt.Errorf("read moments: %w", err)
	}
	var packManifest any
	if err := readStoredJSON(w.storage, packKey, &packManifest); err != nil {
		return fmt.Errorf("read pack manifest: %w", err)
	}
	agentContext := renderplan.NewAgentContext(id, variant, kind, momentsKey, packKey, momentsDoc, packManifest)
	contextBytes, err := json.MarshalIndent(agentContext, "", "  ")
	if err != nil {
		return err
	}
	if err := w.storage.Put(contextKey, bytes.NewReader(contextBytes)); err != nil {
		return fmt.Errorf("upload agent context: %w", err)
	}

	workDir, cleanup, err := prepareStageDir(cfg.WorkDir, id, "agent")
	if err != nil {
		return err
	}
	defer cleanup()
	outputPath := filepath.Join(workDir, "codex-agent-result.json")
	prompt := captionCandidatePrompt(string(contextBytes))
	args := []string{
		"exec",
		"--sandbox", "read-only",
		"--ephemeral",
		"--output-last-message", outputPath,
	}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	args = append(args, prompt)
	runCtx, cancel := context.WithTimeout(ctx, cfg.timeoutDuration())
	defer cancel()
	if _, err := w.runner.Run(runCtx, cfg.CodexPath, args...); err != nil {
		return w.writeAgentFailure(id, variant, kind, contextKey, resultKey, err)
	}
	resultBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("read codex output: %w", err)
	}
	result, err := parseAgentResult(id, variant, kind, contextKey, resultKey, resultBytes)
	if err != nil {
		return w.writeAgentFailure(id, variant, kind, contextKey, resultKey, err)
	}
	return w.writeAgentResult(resultKey, result)
}

func captionCandidatePrompt(contextJSON string) string {
	return strings.Join([]string{
		"You are FragForge's local editorial assistant.",
		"Use only the JSON context below. Do not read files, run commands, or ask questions.",
		"Return strict JSON with keys: titles, captions, hashtags, notes.",
		"Write 3 concise YouTube Shorts title candidates, 3 captions in Spanish, and 6 hashtags.",
		"Context JSON:",
		contextJSON,
	}, "\n")
}

func parseAgentResult(id uuid.UUID, variant, kind, contextKey, resultKey string, b []byte) (renderplan.AgentResult, error) {
	var payload struct {
		Titles   []string `json:"titles"`
		Captions []string `json:"captions"`
		Hashtags []string `json:"hashtags"`
		Notes    []string `json:"notes"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(b), &payload); err != nil {
		return renderplan.AgentResult{}, fmt.Errorf("decode codex JSON: %w", err)
	}
	result := renderplan.NewAgentResult(id, variant, kind, "ready", contextKey, resultKey)
	result.Titles = payload.Titles
	result.Captions = payload.Captions
	result.Hashtags = payload.Hashtags
	result.Notes = payload.Notes
	result.Raw = string(bytes.TrimSpace(b))
	return result, nil
}

func (w *AgentWorker) writeAgentFailure(id uuid.UUID, variant, kind, contextKey, resultKey string, cause error) error {
	result := renderplan.NewAgentResult(id, variant, kind, "failed", contextKey, resultKey)
	result.Error = cause.Error()
	if err := w.writeAgentResult(resultKey, result); err != nil {
		return fmt.Errorf("%w; write agent failure: %v", cause, err)
	}
	return cause
}

func (w *AgentWorker) writeAgentResult(key string, result renderplan.AgentResult) error {
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return w.storage.Put(key, bytes.NewReader(b))
}

func readStoredJSON(store storage.Storage, key string, out any) error {
	rc, err := store.Open(key)
	if err != nil {
		return err
	}
	defer rc.Close()
	return json.NewDecoder(rc).Decode(out)
}

func (c AgentWorkerConfig) withDefaults() AgentWorkerConfig {
	if c.Timeout == "" {
		c.Timeout = defaultAgentWorkerTimeout
	}
	return c
}

func (c AgentWorkerConfig) validate() error {
	if c.CodexPath == "" {
		return fmt.Errorf("codex path is required")
	}
	if _, err := time.ParseDuration(c.Timeout); err != nil {
		return fmt.Errorf("agent timeout must be a duration: %w", err)
	}
	return nil
}

func (c AgentWorkerConfig) timeoutDuration() time.Duration {
	d, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}
