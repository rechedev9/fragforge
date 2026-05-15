package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/reche/zackvideo/internal/artifacts"
	"github.com/reche/zackvideo/internal/composition"
	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/recording"
	"github.com/reche/zackvideo/internal/rules"
	"github.com/reche/zackvideo/internal/tasks"
)

type runnerCall struct {
	exe  string
	args []string
}

type fakeRunner struct {
	calls []runnerCall
	fn    func(context.Context, string, ...string) ([]byte, error)
}

func (f *fakeRunner) Run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, runnerCall{exe: exe, args: append([]string(nil), args...)})
	if f.fn == nil {
		return nil, nil
	}
	return f.fn(ctx, exe, args...)
}

func TestRecordWorkerStoresOutputsAndMarksRecorded(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
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

	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
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
		result := recordingResultWithSegment(scriptPath, segmentPath)
		if err := writeJSONFile(filepath.Join(outDir, "recording-result.json"), result); err != nil {
			t.Fatal(err)
		}
		return []byte("recorded"), nil
	}}
	w := NewRecordWorker(repo, store, RecordWorkerConfig{
		WorkDir:      t.TempDir(),
		RecorderPath: "zv-recorder",
		HLAEPath:     "HLAE.exe",
		CS2Path:      "cs2.exe",
	})
	w.runner = runner

	task := recordTask(t, id)
	if err := w.HandleRecordDemo(context.Background(), task); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}

	if repo.jobs[id].Status != job.StatusRecorded {
		t.Fatalf("Status = %s, want recorded", repo.jobs[id].Status)
	}
	for _, key := range []string{
		artifacts.RecordingResultKey(id),
		artifacts.RecordingScriptKey(id),
		mustSegmentClipKey(t, id, "seg-001"),
	} {
		if _, ok := store.files[key]; !ok {
			t.Fatalf("storage missing %s", key)
		}
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
	if got := argValue(runner.calls[0].args, "--timeout"); got != defaultMediaWorkerTimeout {
		t.Fatalf("--timeout = %q, want %q", got, defaultMediaWorkerTimeout)
	}
}

func TestRecordWorkerFailsWithoutKillPlan(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	repo.jobs[id] = &job.Job{
		ID:       id,
		Status:   job.StatusParsed,
		DemoPath: "demos/test.dem",
		Rules:    rules.Default(),
	}

	w := NewRecordWorker(repo, store, RecordWorkerConfig{
		WorkDir:      t.TempDir(),
		RecorderPath: "zv-recorder",
		HLAEPath:     "HLAE.exe",
		CS2Path:      "cs2.exe",
	})
	err := w.HandleRecordDemo(context.Background(), recordTask(t, id))
	if err == nil || !strings.Contains(err.Error(), "no kill plan") {
		t.Fatalf("HandleRecordDemo error = %v, want no kill plan", err)
	}
	if repo.jobs[id].Status != job.StatusFailed {
		t.Fatalf("Status = %s, want failed", repo.jobs[id].Status)
	}
}

func TestComposeWorkerLocalizesSegmentsAndStoresFinal(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default()}
	putJSON(t, store, artifacts.RecordingResultKey(id), recordingResultWithSegment("", "C:/stale/seg-001.mp4"))
	_ = store.Put(mustSegmentClipKey(t, id, "seg-001"), bytes.NewReader([]byte("clip")))

	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		recordingResultPath := argValue(args, "--recording-result")
		outPath := argValue(args, "--out")
		var result recording.RecordingResult
		if err := readJSONFile(recordingResultPath, &result); err != nil {
			t.Fatal(err)
		}
		gotPath := result.Artifacts[0].Path
		if strings.Contains(gotPath, "stale") {
			t.Fatalf("segment path was not localized: %s", gotPath)
		}
		b, err := os.ReadFile(gotPath)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != "clip" {
			t.Fatalf("localized segment = %q, want clip", b)
		}
		if err := os.WriteFile(outPath, []byte("final"), 0o644); err != nil {
			t.Fatal(err)
		}
		composed := composition.Result{
			RecordingResult: recordingResultPath,
			Output:          outPath,
			OutputArtifact: recording.RecordingArtifact{
				Role:      "final",
				Type:      "video",
				Path:      outPath,
				SizeBytes: 5,
			},
		}
		if err := writeJSONFile(filepath.Join(filepath.Dir(outPath), "composition-result.json"), composed); err != nil {
			t.Fatal(err)
		}
		return []byte("composed"), nil
	}}
	w := NewComposeWorker(repo, store, ComposeWorkerConfig{
		WorkDir:      t.TempDir(),
		ComposerPath: "zv-composer",
	})
	w.runner = runner

	if err := w.HandleComposeFinal(context.Background(), composeTask(t, id)); err != nil {
		t.Fatalf("HandleComposeFinal error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusComposed {
		t.Fatalf("Status = %s, want composed", repo.jobs[id].Status)
	}
	for _, key := range []string{artifacts.CompositionResultKey(id), artifacts.FinalMP4Key(id)} {
		if _, ok := store.files[key]; !ok {
			t.Fatalf("storage missing %s", key)
		}
	}
}

func TestComposeWorkerMarksFailedOnResultError(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default()}
	putJSON(t, store, artifacts.RecordingResultKey(id), recordingResultWithSegment("", "C:/stale/seg-001.mp4"))
	_ = store.Put(mustSegmentClipKey(t, id, "seg-001"), bytes.NewReader([]byte("clip")))

	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		outPath := argValue(args, "--out")
		result := composition.Result{Output: outPath, Error: "bad compose"}
		if err := writeJSONFile(filepath.Join(filepath.Dir(outPath), "composition-result.json"), result); err != nil {
			t.Fatal(err)
		}
		return []byte("bad"), nil
	}}
	w := NewComposeWorker(repo, store, ComposeWorkerConfig{
		WorkDir:      t.TempDir(),
		ComposerPath: "zv-composer",
	})
	w.runner = runner

	err := w.HandleComposeFinal(context.Background(), composeTask(t, id))
	if err == nil || !strings.Contains(err.Error(), "bad compose") {
		t.Fatalf("HandleComposeFinal error = %v, want bad compose", err)
	}
	if repo.jobs[id].Status != job.StatusFailed {
		t.Fatalf("Status = %s, want failed", repo.jobs[id].Status)
	}
	if _, ok := store.files[artifacts.CompositionResultKey(id)]; !ok {
		t.Fatalf("storage missing failed composition result")
	}
}

func minimalKillPlan() killplan.Plan {
	plan := killplan.NewPlan()
	plan.Demo.Tickrate = 64
	plan.Target.SteamID64 = "76561197960265729"
	plan.Rules = rules.Default()
	plan.Segments = []killplan.Segment{{
		ID:        "seg-001",
		Round:     1,
		TickStart: 64,
		TickEnd:   128,
	}}
	return plan
}

func recordingResultWithSegment(scriptPath, segmentPath string) recording.RecordingResult {
	return recording.RecordingResult{
		Plan: recording.RecordingPlan{
			DemoPath:        "demo.dem",
			OutputDir:       "out",
			TargetSteamID64: "76561197960265729",
			TargetAccountID: 1,
			Tickrate:        64,
			Stream:          recording.DefaultStreamConfig(),
			Segments: []recording.RecordingSegment{{
				ID:        "seg-001",
				TickStart: 64,
				TickEnd:   128,
			}},
		},
		Script: scriptPath,
		Artifacts: []recording.RecordingArtifact{{
			SegmentID: "seg-001",
			Role:      "segment",
			Type:      "video",
			Path:      segmentPath,
			SizeBytes: 4,
		}},
	}
}

func recordTask(t *testing.T, id uuid.UUID) *asynq.Task {
	t.Helper()
	task, err := tasks.NewRecordDemoTask(id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func composeTask(t *testing.T, id uuid.UUID) *asynq.Task {
	t.Helper()
	task, err := tasks.NewComposeFinalTask(id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func argValue(args []string, key string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}

func putJSON(t *testing.T, store *fakeStorage, key string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(key, bytes.NewReader(b)); err != nil {
		t.Fatal(err)
	}
}

func mustSegmentClipKey(t *testing.T, id uuid.UUID, segmentID string) string {
	t.Helper()
	key, err := artifacts.SegmentClipKey(id, segmentID)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
