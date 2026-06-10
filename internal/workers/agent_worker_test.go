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

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/tasks"
)

func TestAgentWorkerWritesCaptionCandidates(t *testing.T) {
	store := newFakeStorage()
	id := uuid.New()
	_ = store.Put(artifacts.MomentsKey(id), bytes.NewReader([]byte(`{"moments":[{"id":"mom-001"}]}`)))
	packKey, err := artifacts.RenderVariantPackManifestKey(id, editor.PresetShortNaturalHQ2Full)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(packKey, bytes.NewReader([]byte(`{"items":[{"segment_id":"seg-001"}]}`)))

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
	task, err := tasks.NewCodexAgentTask(id, editor.PresetShortNaturalHQ2Full, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		t.Fatal(err)
	}

	if err := w.HandleCodexAgent(context.Background(), task); err != nil {
		t.Fatalf("HandleCodexAgent error = %v", err)
	}
	resultKey, err := artifacts.RenderVariantAgentResultKey(id, editor.PresetShortNaturalHQ2Full, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		t.Fatal(err)
	}
	var result renderplan.AgentResult
	if err := json.Unmarshal(store.files[resultKey], &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ready" || len(result.Titles) != 1 || result.Titles[0] != "t1" {
		t.Fatalf("result = %#v", result)
	}
	contextKey, err := artifacts.RenderVariantAgentContextKey(id, editor.PresetShortNaturalHQ2Full, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.files[contextKey]; !ok {
		t.Fatalf("missing context key %s", contextKey)
	}
}

func TestAgentWorkerRejectsUnknownKind(t *testing.T) {
	store := newFakeStorage()
	w := NewAgentWorker(store, AgentWorkerConfig{CodexPath: "codex"})
	payload, _ := json.Marshal(tasks.CodexAgentPayload{JobID: uuid.New(), Variant: "natural-hq2-full", Kind: "other"})
	err := w.HandleCodexAgent(context.Background(), asynq.NewTask(tasks.TypeCodexAgent, payload))
	if err == nil {
		t.Fatal("HandleCodexAgent error = nil, want unknown kind")
	}
}
