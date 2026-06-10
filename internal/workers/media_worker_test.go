package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/reche/zackvideo/internal/artifacts"
	"github.com/reche/zackvideo/internal/composition"
	"github.com/reche/zackvideo/internal/editor"
	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/recording"
	"github.com/reche/zackvideo/internal/renderplan"
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

func TestRecordWorkerSkipsWhenOutputsAlreadyExist(t *testing.T) {
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
	putJSON(t, store, artifacts.RecordingResultKey(id), recordingResultWithSegment("", "stale-local.mp4"))
	_ = store.Put(artifacts.RecordingScriptKey(id), bytes.NewReader([]byte("script")))
	_ = store.Put(mustSegmentClipKey(t, id, "seg-001"), bytes.NewReader([]byte("clip")))

	runner := &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("runner should not be called when recording outputs already exist")
		return nil, nil
	}}
	w := NewRecordWorker(repo, store, RecordWorkerConfig{})
	w.runner = runner

	if err := w.HandleRecordDemo(context.Background(), recordTask(t, id)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusRecorded {
		t.Fatalf("Status = %s, want recorded", repo.jobs[id].Status)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestPrepareStageDirCleansTempWorkDirWhenRootEmpty(t *testing.T) {
	dir, cleanup, err := prepareStageDir("", uuid.New(), "record")
	if err != nil {
		t.Fatalf("prepareStageDir error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("work dir not created: %v", err)
	}

	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("work dir still exists after cleanup, err=%v", err)
	}
}

func TestPrepareStageDirKeepsExplicitWorkDir(t *testing.T) {
	root := t.TempDir()
	dir, cleanup, err := prepareStageDir(root, uuid.New(), "record")
	if err != nil {
		t.Fatalf("prepareStageDir error = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("work dir not created: %v", err)
	}

	cleanup()
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("explicit work dir removed, err=%v", err)
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

func TestComposeWorkerSkipsWhenOutputsAlreadyExist(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default()}
	putJSON(t, store, artifacts.CompositionResultKey(id), composition.Result{Output: "final.mp4"})
	_ = store.Put(artifacts.FinalMP4Key(id), bytes.NewReader([]byte("final")))

	runner := &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("runner should not be called when composition outputs already exist")
		return nil, nil
	}}
	w := NewComposeWorker(repo, store, ComposeWorkerConfig{})
	w.runner = runner

	if err := w.HandleComposeFinal(context.Background(), composeTask(t, id)); err != nil {
		t.Fatalf("HandleComposeFinal error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusComposed {
		t.Fatalf("Status = %s, want composed", repo.jobs[id].Status)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestRenderWorkerLocalizesSegmentsAndStoresVariantOutputs(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := minimalKillPlan()
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
	putJSON(t, store, artifacts.RecordingResultKey(id), recordingResultWithSegment("", "C:/stale/seg-001.mp4"))
	_ = store.Put(mustSegmentClipKey(t, id, "seg-001"), bytes.NewReader([]byte("clip")))

	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		recordingResultPath := argValue(args, "--recording-result")
		outDir := argValue(args, "--out")
		publishDir := argValue(args, "--publish-dir")
		if got := argValue(args, "--preset"); got != editor.PresetShortNaturalHQ2Full {
			t.Fatalf("--preset = %q, want %q", got, editor.PresetShortNaturalHQ2Full)
		}
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

		videoPath := filepath.Join(publishDir, "seg-001.mp4")
		coverPath := filepath.Join(publishDir, "seg-001.cover.jpg")
		captionPath := filepath.Join(publishDir, "seg-001.caption.txt")
		logPath := filepath.Join(outDir, "logs", "seg-001-render.log")
		for _, file := range []struct {
			path string
			body string
		}{
			{filepath.Join(outDir, "edit-manifest.json"), `{"shorts":[]}`},
			{filepath.Join(publishDir, "pack-manifest.json"), `{"items":[]}`},
			{filepath.Join(publishDir, "index.html"), `<html></html>`},
			{filepath.Join(publishDir, "summary.md"), `summary`},
			{videoPath, "video"},
			{coverPath, "cover"},
			{captionPath, "caption"},
			{logPath, "log"},
		} {
			if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file.path, []byte(file.body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		rendered := editor.Result{
			Preset:      editor.PresetShortNaturalHQ2Full,
			OutputDir:   outDir,
			PublishDir:  publishDir,
			GalleryPath: filepath.Join(publishDir, "index.html"),
			SummaryPath: filepath.Join(publishDir, "summary.md"),
			Shorts: []editor.ShortResult{{
				SegmentID:     "seg-001",
				Output:        videoPath,
				PublishPath:   videoPath,
				CoverPath:     coverPath,
				CaptionPath:   captionPath,
				RenderLogPath: logPath,
			}},
		}
		if err := writeJSONFile(filepath.Join(outDir, "shorts-result.json"), rendered); err != nil {
			t.Fatal(err)
		}
		return []byte("rendered"), nil
	}}
	w := NewRenderWorker(repo, store, RenderWorkerConfig{
		WorkDir:    t.TempDir(),
		EditorPath: "zv-editor",
		FFmpegPath: "ffmpeg",
	})
	w.runner = runner

	if err := w.HandleRenderVariant(context.Background(), renderTask(t, id, editor.PresetShortNaturalHQ2Full)); err != nil {
		t.Fatalf("HandleRenderVariant error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusRecorded {
		t.Fatalf("Status = %s, want unchanged recorded", repo.jobs[id].Status)
	}
	for _, key := range []string{
		mustRenderVariantResultKey(t, id, editor.PresetShortNaturalHQ2Full),
		mustRenderVariantStatusKey(t, id, editor.PresetShortNaturalHQ2Full),
		mustRenderVariantEditDocumentKey(t, id, editor.PresetShortNaturalHQ2Full),
		mustRenderVariantEditManifestKey(t, id, editor.PresetShortNaturalHQ2Full),
		mustRenderVariantPackManifestKey(t, id, editor.PresetShortNaturalHQ2Full),
		mustRenderVariantPublishSummaryKey(t, id, editor.PresetShortNaturalHQ2Full),
		mustRenderVariantGalleryKey(t, id, editor.PresetShortNaturalHQ2Full),
		mustRenderVariantVideoKey(t, id, editor.PresetShortNaturalHQ2Full, "seg-001"),
		mustRenderVariantCoverKey(t, id, editor.PresetShortNaturalHQ2Full, "seg-001"),
		mustRenderVariantCaptionKey(t, id, editor.PresetShortNaturalHQ2Full, "seg-001"),
		mustRenderVariantLogKey(t, id, editor.PresetShortNaturalHQ2Full, "seg-001-render"),
	} {
		if _, ok := store.files[key]; !ok {
			t.Fatalf("storage missing %s", key)
		}
	}
	if !strings.Contains(string(store.files[mustRenderVariantEditDocumentKey(t, id, editor.PresetShortNaturalHQ2Full)]), "shortslistosparasubir") {
		t.Fatalf("edit document missing upload-ready root: %s", store.files[mustRenderVariantEditDocumentKey(t, id, editor.PresetShortNaturalHQ2Full)])
	}
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(store.files[mustRenderVariantStatusKey(t, id, editor.PresetShortNaturalHQ2Full)], &state); err != nil {
		t.Fatal(err)
	}
	if got, want := state.Status, renderplan.RenderVariantStatusReady; got != want {
		t.Fatalf("render state = %q, want %q", got, want)
	}
}

func TestRenderWorkerWritesFailedStateWhenEditorFails(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := minimalKillPlan()
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
	putJSON(t, store, artifacts.RecordingResultKey(id), recordingResultWithSegment("", "C:/stale/seg-001.mp4"))
	_ = store.Put(mustSegmentClipKey(t, id, "seg-001"), bytes.NewReader([]byte("clip")))

	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		outDir := argValue(args, "--out")
		publishDir := argValue(args, "--publish-dir")
		if err := os.MkdirAll(publishDir, 0o750); err != nil {
			t.Fatal(err)
		}
		videoPath := filepath.Join(publishDir, "seg-001.mp4")
		if err := os.WriteFile(videoPath, []byte("mp4"), 0o600); err != nil {
			t.Fatal(err)
		}
		result := editor.Result{
			Preset: editor.PresetShortNaturalHQ2Full,
			Error:  "encoder failed",
			Shorts: []editor.ShortResult{{
				SegmentID:   "seg-001",
				PublishPath: videoPath,
				PublishArtifact: recording.RecordingArtifact{
					Path:      videoPath,
					SizeBytes: 3,
				},
			}},
		}
		if err := writeJSONFile(filepath.Join(outDir, "shorts-result.json"), result); err != nil {
			t.Fatal(err)
		}
		return nil, errors.New("zv-editor failed")
	}}
	w := NewRenderWorker(repo, store, RenderWorkerConfig{
		WorkDir:    t.TempDir(),
		EditorPath: "zv-editor",
	})
	w.runner = runner

	err := w.HandleRenderVariant(context.Background(), renderTask(t, id, editor.PresetShortNaturalHQ2Full))
	if err == nil {
		t.Fatal("HandleRenderVariant error = nil, want failure")
	}
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(store.files[mustRenderVariantStatusKey(t, id, editor.PresetShortNaturalHQ2Full)], &state); err != nil {
		t.Fatal(err)
	}
	if got, want := state.Status, renderplan.RenderVariantStatusFailed; got != want {
		t.Fatalf("render state = %q, want %q", got, want)
	}
	if state.Error != "encoder failed" {
		t.Fatalf("state error = %q, want encoder failed", state.Error)
	}
}

func TestRenderWorkerRejectsUnknownVariant(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := minimalKillPlan()
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
	runner := &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("runner should not be called for an unknown variant")
		return nil, nil
	}}
	w := NewRenderWorker(repo, store, RenderWorkerConfig{WorkDir: t.TempDir(), EditorPath: "zv-editor"})
	w.runner = runner

	err := w.HandleRenderVariant(context.Background(), renderTask(t, id, "made-up-preset"))
	if err == nil {
		t.Fatal("HandleRenderVariant error = nil, want unknown variant error")
	}
	if !strings.Contains(err.Error(), "unknown render variant") || !strings.Contains(err.Error(), editor.PresetViral60) {
		t.Fatalf("error = %v, want unknown render variant listing valid presets", err)
	}
}

func TestRenderWorkerDefaultsToViral60WhenVariantEmpty(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := minimalKillPlan()
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
	defaultVariant := editor.DefaultPreset().Name
	putJSON(t, store, mustRenderVariantResultKey(t, id, defaultVariant), editor.Result{Preset: defaultVariant})
	_ = store.Put(mustRenderVariantPackManifestKey(t, id, defaultVariant), bytes.NewReader([]byte("pack")))
	_ = store.Put(mustRenderVariantGalleryKey(t, id, defaultVariant), bytes.NewReader([]byte("<html></html>")))

	runner := &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("runner should not be called when default variant outputs already exist")
		return nil, nil
	}}
	w := NewRenderWorker(repo, store, RenderWorkerConfig{WorkDir: t.TempDir(), EditorPath: "zv-editor"})
	w.runner = runner

	payload, err := json.Marshal(tasks.RenderVariantPayload{JobID: id})
	if err != nil {
		t.Fatal(err)
	}
	task := asynq.NewTask(tasks.TypeRenderVariant, payload)
	if err := w.HandleRenderVariant(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderVariant error = %v", err)
	}
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(store.files[mustRenderVariantStatusKey(t, id, defaultVariant)], &state); err != nil {
		t.Fatal(err)
	}
	if got, want := state.Variant, defaultVariant; got != want {
		t.Fatalf("state variant = %q, want %q", got, want)
	}
	if got, want := state.Status, renderplan.RenderVariantStatusReady; got != want {
		t.Fatalf("render state = %q, want %q", got, want)
	}
}

func TestProbeRenderResultUpdatesPublishArtifact(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "seg-001.mp4")
	if err := os.WriteFile(videoPath, []byte("mp4"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := editor.Result{
		Shorts: []editor.ShortResult{{
			SegmentID:   "seg-001",
			PublishPath: videoPath,
		}},
	}
	runner := &fakeRunner{fn: func(_ context.Context, exe string, args ...string) ([]byte, error) {
		if exe != "ffprobe" {
			t.Fatalf("exe = %q, want ffprobe", exe)
		}
		if got := args[len(args)-1]; got != videoPath {
			t.Fatalf("last arg = %q, want %q", got, videoPath)
		}
		return []byte(`{"streams":[{"codec_name":"h264","width":1080,"height":1920,"r_frame_rate":"60/1","duration":"12.5"}],"format":{"duration":"12.5","size":"12345"}}`), nil
	}}

	if err := probeRenderResult(context.Background(), runner, "ffprobe", &result); err != nil {
		t.Fatalf("probeRenderResult error = %v", err)
	}
	got := result.Shorts[0].PublishArtifact
	if got.Codec != "h264" || got.Width != 1080 || got.Height != 1920 || got.DurationSeconds != 12.5 || got.SizeBytes != 12345 {
		t.Fatalf("artifact = %#v", got)
	}
}

func TestRenderWorkerSkipsWhenVariantOutputsAlreadyExist(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := minimalKillPlan()
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
	putJSON(t, store, mustRenderVariantResultKey(t, id, editor.PresetShortNaturalHQ2Full), editor.Result{
		Preset: editor.PresetShortNaturalHQ2Full,
		Shorts: []editor.ShortResult{{
			SegmentID: "seg-001",
		}},
	})
	_ = store.Put(mustRenderVariantPackManifestKey(t, id, editor.PresetShortNaturalHQ2Full), bytes.NewReader([]byte("pack")))
	_ = store.Put(mustRenderVariantGalleryKey(t, id, editor.PresetShortNaturalHQ2Full), bytes.NewReader([]byte("gallery")))

	runner := &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("runner should not be called when render variant outputs already exist")
		return nil, nil
	}}
	w := NewRenderWorker(repo, store, RenderWorkerConfig{})
	w.runner = runner

	if err := w.HandleRenderVariant(context.Background(), renderTask(t, id, editor.PresetShortNaturalHQ2Full)); err != nil {
		t.Fatalf("HandleRenderVariant error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestIsTerminalAttempt(t *testing.T) {
	cases := []struct {
		name              string
		retried, maxRetry int
		inTask            bool
		want              bool
	}{
		{"outside asynq task context", 0, 0, false, true},
		{"no-retry task first attempt", 0, 0, true, true},
		{"retryable task mid-flight", 3, 25, true, false},
		{"retryable task final attempt", 25, 25, true, true},
		{"retryable task past max", 26, 25, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTerminalAttempt(tc.retried, tc.maxRetry, tc.inTask); got != tc.want {
				t.Errorf("isTerminalAttempt(%d, %d, %v) = %v, want %v", tc.retried, tc.maxRetry, tc.inTask, got, tc.want)
			}
		})
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

func renderTask(t *testing.T, id uuid.UUID, variant string) *asynq.Task {
	t.Helper()
	task, err := tasks.NewRenderVariantTask(id, variant)
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

func mustRenderVariantResultKey(t *testing.T, id uuid.UUID, variant string) string {
	t.Helper()
	key, err := artifacts.RenderVariantResultKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustRenderVariantStatusKey(t *testing.T, id uuid.UUID, variant string) string {
	t.Helper()
	key, err := artifacts.RenderVariantStatusKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustRenderVariantEditDocumentKey(t *testing.T, id uuid.UUID, variant string) string {
	t.Helper()
	key, err := artifacts.RenderVariantEditDocumentKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustRenderVariantEditManifestKey(t *testing.T, id uuid.UUID, variant string) string {
	t.Helper()
	key, err := artifacts.RenderVariantEditManifestKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustRenderVariantPackManifestKey(t *testing.T, id uuid.UUID, variant string) string {
	t.Helper()
	key, err := artifacts.RenderVariantPackManifestKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustRenderVariantPublishSummaryKey(t *testing.T, id uuid.UUID, variant string) string {
	t.Helper()
	key, err := artifacts.RenderVariantPublishSummaryKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustRenderVariantGalleryKey(t *testing.T, id uuid.UUID, variant string) string {
	t.Helper()
	key, err := artifacts.RenderVariantGalleryKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustRenderVariantVideoKey(t *testing.T, id uuid.UUID, variant, name string) string {
	t.Helper()
	key, err := artifacts.RenderVariantVideoKey(id, variant, name)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustRenderVariantCoverKey(t *testing.T, id uuid.UUID, variant, name string) string {
	t.Helper()
	key, err := artifacts.RenderVariantCoverKey(id, variant, name)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustRenderVariantCaptionKey(t *testing.T, id uuid.UUID, variant, name string) string {
	t.Helper()
	key, err := artifacts.RenderVariantCaptionKey(id, variant, name)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustRenderVariantLogKey(t *testing.T, id uuid.UUID, variant, name string) string {
	t.Helper()
	key, err := artifacts.RenderVariantLogKey(id, variant, name)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
