package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/tasks"
)

func TestAgentWorkerWritesCaptionCandidates(t *testing.T) {
	store := newFakeStorage()
	id := uuid.New()
	agentArtifacts, err := renderplan.NewAgentArtifacts(id, editor.PresetViral60Clean, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(agentArtifacts.MomentsKey, bytes.NewReader([]byte(`{"moments":[{"id":"mom-001"}]}`)))
	_ = store.Put(agentArtifacts.PackManifestKey, bytes.NewReader([]byte(`{"items":[{"segment_id":"seg-001"}]}`)))

	runner := &fakeRunner{fn: func(_ context.Context, exe string, args ...string) ([]byte, error) {
		if exe != "codex" {
			t.Fatalf("exe = %q, want codex", exe)
		}
		for _, arg := range args {
			if arg == "--ask-for-approval" {
				t.Fatal("codex exec args must not include unsupported --ask-for-approval")
			}
		}
		out := argValue(args, "--output-last-message")
		if out == "" {
			t.Fatal("missing --output-last-message")
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out, []byte(`{"titles":["t1"],"captions":["c1"],"hashtags":["#CS2"],"notes":["n1"]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		return []byte("ok"), nil
	}}
	w := NewAgentWorker(store, AgentWorkerConfig{
		WorkDir:   t.TempDir(),
		CodexPath: "codex",
	})
	w.runner = runner
	task, err := tasks.NewCodexAgentTask(id, editor.PresetViral60Clean, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		t.Fatal(err)
	}

	if err := w.HandleCodexAgent(context.Background(), task); err != nil {
		t.Fatalf("HandleCodexAgent error = %v", err)
	}
	var result renderplan.AgentResult
	if err := json.Unmarshal(store.files[agentArtifacts.ResultKey], &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ready" || len(result.Titles) != 1 || result.Titles[0] != "t1" {
		t.Fatalf("result = %#v", result)
	}
	if _, ok := store.files[agentArtifacts.ContextKey]; !ok {
		t.Fatalf("missing context key %s", agentArtifacts.ContextKey)
	}
}

func TestAgentWorkerRejectsUnknownKind(t *testing.T) {
	store := newFakeStorage()
	w := NewAgentWorker(store, AgentWorkerConfig{CodexPath: "codex"})
	payload, _ := json.Marshal(tasks.CodexAgentPayload{JobID: uuid.New(), Variant: "viral-60-clean", Kind: "other"})
	err := w.HandleCodexAgent(context.Background(), asynq.NewTask(tasks.TypeCodexAgent, payload))
	if err == nil {
		t.Fatal("HandleCodexAgent error = nil, want unknown kind")
	}
}
