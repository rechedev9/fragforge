package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/tasks"
)

type fakeEnqueuer struct {
	mu    sync.Mutex
	tasks []*asynq.Task
}

func (e *fakeEnqueuer) Enqueue(t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.tasks = append(e.tasks, t)
	return &asynq.TaskInfo{ID: "x"}, nil
}

// recordRunnerWithSegment returns a runner that materializes one segment clip
// and a recording result, mirroring the happy path of the recorder CLI.
func recordRunnerWithSegment(t *testing.T) *fakeRunner {
	t.Helper()
	return &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		outDir := argValue(args, "--out")
		if outDir == "" {
			t.Fatal("runner args missing --out")
		}
		scriptPath := filepath.Join(outDir, "recording.js")
		segmentPath := filepath.Join(outDir, "segments", "seg-001.mp4")
		if err := os.MkdirAll(filepath.Dir(segmentPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(scriptPath, []byte("script"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(segmentPath, []byte("clip"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := writeJSONFile(filepath.Join(outDir, "recording-result.json"), recordingResultWithSegment(scriptPath, segmentPath)); err != nil {
			t.Fatal(err)
		}
		return []byte("recorded"), nil
	}}
}

func parsedRecordJob(store *fakeStorage) (*fakeRepo, uuid.UUID) {
	repo := newFakeRepo()
	id := uuid.New()
	plan := minimalKillPlan()
	repo.jobs[id] = &job.Job{
		ID:       id,
		Status:   job.StatusParsed,
		DemoPath: "demos/test.dem",
		Rules:    rules.Default(),
		KillPlan: &plan,
	}
	_ = store.Put("demos/test.dem", bytes.NewReader([]byte("demo")))
	return repo, id
}

func newRecordWorkerForTest(repo *fakeRepo, store *fakeStorage, t *testing.T) *RecordWorker {
	t.Helper()
	w := NewRecordWorker(repo, store, RecordWorkerConfig{
		WorkDir:      t.TempDir(),
		RecorderPath: "zv-recorder",
		HLAEPath:     "HLAE.exe",
		CS2Path:      "cs2.exe",
	})
	w.runner = recordRunnerWithSegment(t)
	return w
}

func TestRecordWorkerChainsRenderFromGenerateIntent(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	edit := renderplan.EditRequest{
		Format:     renderplan.FormatShort9x16,
		KillEffect: renderplan.KillEffectVelocity,
		Transition: renderplan.TransitionWhip,
		Intro:      true,
	}
	intent := renderplan.GenerateIntent{Variant: editor.PresetCleanPOV60, MusicKey: "phonk-01", Edit: edit}
	putJSON(t, store, artifacts.GenerateIntentKey(id), intent)

	enq := &fakeEnqueuer{}
	w := newRecordWorkerForTest(repo, store, t)
	w.UseEnqueuer(enq)

	if err := w.HandleRecordDemo(context.Background(), recordTask(t, id)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusRecorded {
		t.Fatalf("Status = %s, want recorded", repo.jobs[id].Status)
	}
	if len(enq.tasks) != 1 {
		t.Fatalf("chained tasks = %d, want 1", len(enq.tasks))
	}
	if got := enq.tasks[0].Type(); got != tasks.TypeRenderVariant {
		t.Fatalf("chained task type = %q, want %q", got, tasks.TypeRenderVariant)
	}
	var payload tasks.RenderVariantPayload
	if err := json.Unmarshal(enq.tasks[0].Payload(), &payload); err != nil {
		t.Fatalf("unmarshal render payload: %v", err)
	}
	if payload.Variant != editor.PresetCleanPOV60 || payload.MusicKey != "phonk-01" || payload.Edit != edit {
		t.Fatalf("render payload = %#v, want variant=%s music=phonk-01 edit=%#v", payload, editor.PresetCleanPOV60, edit)
	}

	// The queued render state is surfaced immediately so the UI does not flash
	// back to "not started" while the render worker spins up.
	raw, ok := store.files[mustRenderVariantStatusKey(t, id, editor.PresetCleanPOV60)]
	if !ok {
		t.Fatal("queued render state not written")
	}
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal render state: %v", err)
	}
	if state.Status != renderplan.RenderVariantStatusQueued {
		t.Fatalf("render state status = %q, want queued", state.Status)
	}
}

func TestRecordWorkerWithoutIntentDoesNotChain(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	enq := &fakeEnqueuer{}
	w := newRecordWorkerForTest(repo, store, t)
	w.UseEnqueuer(enq)

	if err := w.HandleRecordDemo(context.Background(), recordTask(t, id)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusRecorded {
		t.Fatalf("Status = %s, want recorded", repo.jobs[id].Status)
	}
	if len(enq.tasks) != 0 {
		t.Fatalf("chained tasks = %d, want 0 without an intent", len(enq.tasks))
	}
}

func TestRecordWorkerNilEnqueuerSkipsChaining(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	putJSON(t, store, artifacts.GenerateIntentKey(id), renderplan.GenerateIntent{
		Variant: editor.PresetViral60Clean,
		Edit:    renderplan.DefaultEditRequest(),
	})
	// No UseEnqueuer: a worker built without a queue must skip chaining cleanly.
	w := newRecordWorkerForTest(repo, store, t)

	if err := w.HandleRecordDemo(context.Background(), recordTask(t, id)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusRecorded {
		t.Fatalf("Status = %s, want recorded", repo.jobs[id].Status)
	}
}
