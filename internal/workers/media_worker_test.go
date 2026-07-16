package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/composition"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/tasks"
)

type runnerCall struct {
	exe  string
	args []string
}

type fakeRunner struct {
	mu    sync.Mutex
	calls []runnerCall
	fn    func(context.Context, string, ...string) ([]byte, error)
}

func (f *fakeRunner) Run(ctx context.Context, exe string, args ...string) ([]byte, error) {
	// The render worker probes shorts concurrently, so guard the call log.
	f.mu.Lock()
	f.calls = append(f.calls, runnerCall{exe: exe, args: append([]string(nil), args...)})
	f.mu.Unlock()
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
		recording.ResultArtifactKey(id),
		recording.ScriptArtifactKey(id),
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

func TestRecordWorkerHUDFromPayloadOverridesDefault(t *testing.T) {
	cases := []struct {
		name                 string
		hud                  string
		portraitSafeKillfeed bool
		wantHUD              string
		wantPortraitFlag     bool
	}{
		{name: "preset clean overrides default", hud: "clean", wantHUD: "clean"},
		{name: "empty payload keeps worker default", hud: "", wantHUD: "deathnotices"},
		{name: "vertical killfeed configures portrait safe capture", hud: "deathnotices", portraitSafeKillfeed: true, wantHUD: "deathnotices", wantPortraitFlag: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			store := newFakeStorage()
			id := uuid.New()
			plan := minimalKillPlan()
			repo.jobs[id] = &job.Job{ID: id, Status: job.StatusParsed, DemoPath: "demos/test.dem", Rules: rules.Default(), KillPlan: &plan}
			_ = store.Put("demos/test.dem", bytes.NewReader([]byte("demo")))

			runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
				outDir := argValue(args, "--out")
				scriptPath := filepath.Join(outDir, "recording.js")
				segmentPath := filepath.Join(outDir, "segments", "seg-001.mp4")
				_ = os.MkdirAll(filepath.Dir(segmentPath), 0o755)
				_ = os.WriteFile(scriptPath, []byte("script"), 0o644)
				_ = os.WriteFile(segmentPath, []byte("clip"), 0o644)
				_ = writeJSONFile(filepath.Join(outDir, "recording-result.json"), recordingResultWithSegment(scriptPath, segmentPath))
				return []byte("recorded"), nil
			}}
			// Worker default HUD is "deathnotices" (withDefaults); the payload may override it.
			w := NewRecordWorker(repo, store, RecordWorkerConfig{WorkDir: t.TempDir(), RecorderPath: "zv-recorder", HLAEPath: "HLAE.exe", CS2Path: "cs2.exe"})
			w.runner = runner

			task, err := tasks.NewRecordDemoTask(id, tc.hud, nil, tc.portraitSafeKillfeed)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.HandleRecordDemo(context.Background(), task); err != nil {
				t.Fatalf("HandleRecordDemo error = %v", err)
			}
			if got := argValue(runner.calls[0].args, "--hud"); got != tc.wantHUD {
				t.Fatalf("--hud = %q, want %q", got, tc.wantHUD)
			}
			if got := hasArg(runner.calls[0].args, "--portrait-safe-killfeed"); got != tc.wantPortraitFlag {
				t.Fatalf("--portrait-safe-killfeed present = %v, want %v", got, tc.wantPortraitFlag)
			}
		})
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
	putJSON(t, store, recording.ResultArtifactKey(id), recordingResultWithSegment("", "stale-local.mp4"))
	_ = store.Put(recording.ScriptArtifactKey(id), bytes.NewReader([]byte("script")))
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
	putJSON(t, store, recording.ResultArtifactKey(id), recordingResultWithSegment("", "C:/stale/seg-001.mp4"))
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
	for _, key := range []string{composition.ResultArtifactKey(id), composition.FinalArtifactKey(id)} {
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
	putJSON(t, store, recording.ResultArtifactKey(id), recordingResultWithSegment("", "C:/stale/seg-001.mp4"))
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
	if _, ok := store.files[composition.ResultArtifactKey(id)]; !ok {
		t.Fatalf("storage missing failed composition result")
	}
}

func TestComposeWorkerSkipsWhenOutputsAlreadyExist(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default()}
	putJSON(t, store, composition.ResultArtifactKey(id), composition.Result{Output: "final.mp4"})
	_ = store.Put(composition.FinalArtifactKey(id), bytes.NewReader([]byte("final")))

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
	putJSON(t, store, recording.ResultArtifactKey(id), recordingResultWithSegment("", "C:/stale/seg-001.mp4"))
	_ = store.Put(mustSegmentClipKey(t, id, "seg-001"), bytes.NewReader([]byte("clip")))

	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		recordingResultPath := argValue(args, "--recording-result")
		outDir := argValue(args, "--out")
		publishDir := argValue(args, "--publish-dir")
		if got := argValue(args, "--preset"); got != editor.PresetViral60Clean {
			t.Fatalf("--preset = %q, want %q", got, editor.PresetViral60Clean)
		}
		for _, check := range []struct {
			key  string
			want string
		}{
			{"--output-format", renderplan.FormatLandscape16x9},
			{"--kill-effect", renderplan.KillEffectFreezeFlash},
			{"--transition", renderplan.TransitionDip},
		} {
			if got := argValue(args, check.key); got != check.want {
				t.Fatalf("%s = %q, want %q", check.key, got, check.want)
			}
		}
		if !hasArg(args, "--hook=true") || !hasArg(args, "--kill-counter=false") {
			t.Fatalf("editor args missing explicit automatic text values: %#v", args)
		}
		if !hasArg(args, "--intro") || !hasArg(args, "--outro") {
			t.Fatalf("editor args missing intro/outro flags: %#v", args)
		}
		if got := argValue(args, "--intro-text"); got != "Watch this ace" {
			t.Fatalf("--intro-text = %q, want %q", got, "Watch this ace")
		}
		if got := argValue(args, "--outro-text"); got != "follow for more" {
			t.Fatalf("--outro-text = %q, want %q", got, "follow for more")
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
			Preset:      editor.PresetViral60Clean,
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

	task, err := tasks.NewRenderVariantTask(id, editor.PresetViral60Clean, "", 0, renderplan.EditRequest{
		Format:      renderplan.FormatLandscape16x9,
		KillEffect:  renderplan.KillEffectFreezeFlash,
		Transition:  renderplan.TransitionDip,
		Intro:       true,
		Outro:       true,
		IntroText:   "Watch this ace",
		OutroText:   "follow for more",
		HookText:    true,
		KillCounter: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderVariant(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderVariant error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusRecorded {
		t.Fatalf("Status = %s, want unchanged recorded", repo.jobs[id].Status)
	}
	for _, key := range []string{
		mustRenderVariantResultKey(t, id, editor.PresetViral60Clean),
		mustRenderVariantStatusKey(t, id, editor.PresetViral60Clean),
		mustRenderVariantEditDocumentKey(t, id, editor.PresetViral60Clean),
		mustRenderVariantEditManifestKey(t, id, editor.PresetViral60Clean),
		mustRenderVariantPackManifestKey(t, id, editor.PresetViral60Clean),
		mustRenderVariantPublishSummaryKey(t, id, editor.PresetViral60Clean),
		mustRenderVariantGalleryKey(t, id, editor.PresetViral60Clean),
		mustRenderVariantVideoKey(t, id, editor.PresetViral60Clean, "seg-001"),
		mustRenderVariantCoverKey(t, id, editor.PresetViral60Clean, "seg-001"),
		mustRenderVariantCaptionKey(t, id, editor.PresetViral60Clean, "seg-001"),
		mustRenderVariantLogKey(t, id, editor.PresetViral60Clean, "seg-001-render"),
	} {
		if _, ok := store.files[key]; !ok {
			t.Fatalf("storage missing %s", key)
		}
	}
	if !strings.Contains(string(store.files[mustRenderVariantEditDocumentKey(t, id, editor.PresetViral60Clean)]), "shortslistosparasubir") {
		t.Fatalf("edit document missing upload-ready root: %s", store.files[mustRenderVariantEditDocumentKey(t, id, editor.PresetViral60Clean)])
	}
	var doc renderplan.EditDocument
	if err := json.Unmarshal(store.files[mustRenderVariantEditDocumentKey(t, id, editor.PresetViral60Clean)], &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Edit.Format != renderplan.FormatLandscape16x9 || doc.LoadoutSnapshot.Output.AspectRatio != "16:9" || doc.LoadoutSnapshot.Output.Width != 1920 || doc.LoadoutSnapshot.Output.Height != 1080 {
		t.Fatalf("edit document = %#v", doc)
	}
	if !doc.Edit.HookText || doc.Edit.KillCounter {
		t.Fatalf("edit document automatic text = hook %v / counter %v, want true / false", doc.Edit.HookText, doc.Edit.KillCounter)
	}
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(store.files[mustRenderVariantStatusKey(t, id, editor.PresetViral60Clean)], &state); err != nil {
		t.Fatal(err)
	}
	if got, want := state.Status, renderplan.RenderVariantStatusReady; got != want {
		t.Fatalf("render state = %q, want %q", got, want)
	}
	var storedResult editor.Result
	if err := json.Unmarshal(store.files[mustRenderVariantResultKey(t, id, editor.PresetViral60Clean)], &storedResult); err != nil {
		t.Fatal(err)
	}
	if storedResult.InputFingerprint == "" {
		t.Fatal("stored render result is missing input fingerprint")
	}
}

func TestCompileSegmentsArgs(t *testing.T) {
	tests := []struct {
		name       string
		segmentIDs []string
		want       []string
	}{
		{
			name:       "no segments",
			segmentIDs: nil,
			want:       nil,
		},
		{
			name:       "single segment keeps today's per-segment render",
			segmentIDs: []string{"seg-001"},
			want:       nil,
		},
		{
			name:       "two segments compile into one short in plan order",
			segmentIDs: []string{"seg-001", "seg-004"},
			want:       []string{"--compile-segments", "--segments", "seg-001,seg-004"},
		},
		{
			name:       "three segments join all ids in order",
			segmentIDs: []string{"seg-003", "seg-001", "seg-002"},
			want:       []string{"--compile-segments", "--segments", "seg-003,seg-001,seg-002"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compileSegmentsArgs(tt.segmentIDs)
			if len(got) != len(tt.want) {
				t.Fatalf("compileSegmentsArgs(%v) = %v, want %v", tt.segmentIDs, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("compileSegmentsArgs(%v) = %v, want %v", tt.segmentIDs, got, tt.want)
				}
			}
		})
	}
}

func TestRenderWorkerCompilesMultipleSegmentsIntoOneShort(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := multiSegmentKillPlan("seg-001", "seg-002")
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
	rec := recording.RecordingResult{
		Plan: recording.RecordingPlan{
			DemoPath:        "demo.dem",
			OutputDir:       "out",
			TargetSteamID64: "76561197960265729",
			TargetAccountID: 1,
			Tickrate:        64,
			Stream:          recording.DefaultStreamConfig(),
			Segments: []recording.RecordingSegment{
				{ID: "seg-001", TickStart: 64, TickEnd: 128},
				{ID: "seg-002", TickStart: 128, TickEnd: 192},
			},
		},
		Artifacts: []recording.RecordingArtifact{
			{SegmentID: "seg-001", Role: "segment", Type: "video", Path: "C:/stale/seg-001.mp4", SizeBytes: 4},
			{SegmentID: "seg-002", Role: "segment", Type: "video", Path: "C:/stale/seg-002.mp4", SizeBytes: 4},
		},
	}
	putJSON(t, store, recording.ResultArtifactKey(id), rec)
	_ = store.Put(mustSegmentClipKey(t, id, "seg-001"), bytes.NewReader([]byte("clip1")))
	_ = store.Put(mustSegmentClipKey(t, id, "seg-002"), bytes.NewReader([]byte("clip2")))

	const compiledID = "demo-compilation"
	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if !hasArg(args, "--compile-segments") {
			t.Fatalf("editor args missing --compile-segments for a 2-segment render: %#v", args)
		}
		if got, want := argValue(args, "--segments"), "seg-001,seg-002"; got != want {
			t.Fatalf("--segments = %q, want %q", got, want)
		}
		outDir := argValue(args, "--out")
		publishDir := argValue(args, "--publish-dir")
		videoPath := filepath.Join(publishDir, compiledID+".mp4")
		coverPath := filepath.Join(publishDir, compiledID+".cover.jpg")
		captionPath := filepath.Join(publishDir, compiledID+".caption.txt")
		logPath := filepath.Join(outDir, "logs", compiledID+"-render.log")
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
			Preset:      editor.PresetViral60Clean,
			OutputDir:   outDir,
			PublishDir:  publishDir,
			GalleryPath: filepath.Join(publishDir, "index.html"),
			SummaryPath: filepath.Join(publishDir, "summary.md"),
			// A compiled render emits exactly one short covering every selected
			// segment, not one short per segment.
			Shorts: []editor.ShortResult{{
				SegmentID:     compiledID,
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

	if err := w.HandleRenderVariant(context.Background(), renderTask(t, id, editor.PresetViral60Clean)); err != nil {
		t.Fatalf("HandleRenderVariant error = %v", err)
	}

	// The published output is one compiled reel, not per-segment shorts: only
	// the "demo-compilation" video/cover/caption keys exist.
	for _, key := range []string{
		mustRenderVariantVideoKey(t, id, editor.PresetViral60Clean, compiledID),
		mustRenderVariantCoverKey(t, id, editor.PresetViral60Clean, compiledID),
		mustRenderVariantCaptionKey(t, id, editor.PresetViral60Clean, compiledID),
	} {
		if _, ok := store.files[key]; !ok {
			t.Fatalf("storage missing compiled short artifact %s", key)
		}
	}
	for _, segmentID := range []string{"seg-001", "seg-002"} {
		key, err := artifacts.RenderVariantVideoKey(id, editor.PresetViral60Clean, segmentID)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := store.files[key]; ok {
			t.Fatalf("storage has a per-segment short %s; multi-segment renders must compile into one reel", key)
		}
	}

	var state renderplan.RenderVariantState
	if err := json.Unmarshal(store.files[mustRenderVariantStatusKey(t, id, editor.PresetViral60Clean)], &state); err != nil {
		t.Fatal(err)
	}
	if got, want := state.Status, renderplan.RenderVariantStatusReady; got != want {
		t.Fatalf("render state = %q, want %q", got, want)
	}

	// The result artifact is the source the videos-listing endpoints
	// (GetRenderPublishBoard, GetRenderVariant) read from; confirm it reports
	// the single compiled short so the API/web exposes exactly one video.
	var result editor.Result
	if err := json.Unmarshal(store.files[mustRenderVariantResultKey(t, id, editor.PresetViral60Clean)], &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Shorts) != 1 || result.Shorts[0].SegmentID != compiledID {
		t.Fatalf("render result shorts = %#v, want exactly one %q short", result.Shorts, compiledID)
	}
}

func TestRenderWorkerWritesFailedStateWhenEditorFails(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := minimalKillPlan()
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
	putJSON(t, store, recording.ResultArtifactKey(id), recordingResultWithSegment("", "C:/stale/seg-001.mp4"))
	_ = store.Put(mustSegmentClipKey(t, id, "seg-001"), bytes.NewReader([]byte("clip")))

	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		outDir := argValue(args, "--out")
		publishDir := argValue(args, "--publish-dir")
		if hasArg(args, "--intro-text") || hasArg(args, "--outro-text") {
			t.Fatalf("editor args = %#v, want no bookend text flags when unset", args)
		}
		if err := os.MkdirAll(publishDir, 0o750); err != nil {
			t.Fatal(err)
		}
		videoPath := filepath.Join(publishDir, "seg-001.mp4")
		if err := os.WriteFile(videoPath, []byte("mp4"), 0o600); err != nil {
			t.Fatal(err)
		}
		result := editor.Result{
			Preset: editor.PresetViral60Clean,
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

	err := w.HandleRenderVariant(context.Background(), renderTask(t, id, editor.PresetViral60Clean))
	if err == nil {
		t.Fatal("HandleRenderVariant error = nil, want failure")
	}
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(store.files[mustRenderVariantStatusKey(t, id, editor.PresetViral60Clean)], &state); err != nil {
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
	if !strings.Contains(err.Error(), "unknown render variant") || !strings.Contains(err.Error(), editor.PresetViral60Clean) {
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
	recordingResult := recordingResultWithSegment("", "C:/stale/seg-001.mp4")
	recordingResult.CaptureRevision = "capture-1"
	putJSON(t, store, recording.ResultArtifactKey(id), recordingResult)
	fingerprint, err := renderInputFingerprint(recordingResult, &plan, defaultVariant, "", "", 0, renderplan.DefaultEditRequest())
	if err != nil {
		t.Fatal(err)
	}
	putJSON(t, store, mustRenderVariantResultKey(t, id, defaultVariant), editor.Result{
		Preset:           defaultVariant,
		InputFingerprint: fingerprint,
		Shorts:           []editor.ShortResult{{SegmentID: "seg-001"}},
	})
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
	recordingResult := recordingResultWithSegment("", "C:/stale/seg-001.mp4")
	recordingResult.CaptureRevision = "capture-1"
	putJSON(t, store, recording.ResultArtifactKey(id), recordingResult)
	fingerprint, err := renderInputFingerprint(recordingResult, &plan, editor.PresetViral60Clean, "", "", 0, renderplan.DefaultEditRequest())
	if err != nil {
		t.Fatal(err)
	}
	putJSON(t, store, mustRenderVariantResultKey(t, id, editor.PresetViral60Clean), editor.Result{
		Preset:           editor.PresetViral60Clean,
		InputFingerprint: fingerprint,
		Shorts: []editor.ShortResult{{
			SegmentID: "seg-001",
		}},
	})
	_ = store.Put(mustRenderVariantPackManifestKey(t, id, editor.PresetViral60Clean), bytes.NewReader([]byte("pack")))
	_ = store.Put(mustRenderVariantGalleryKey(t, id, editor.PresetViral60Clean), bytes.NewReader([]byte("gallery")))

	runner := &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("runner should not be called when render variant outputs already exist")
		return nil, nil
	}}
	w := NewRenderWorker(repo, store, RenderWorkerConfig{})
	w.runner = runner

	if err := w.HandleRenderVariant(context.Background(), renderTask(t, id, editor.PresetViral60Clean)); err != nil {
		t.Fatalf("HandleRenderVariant error = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0", len(runner.calls))
	}
}

func TestRenderWorkerRerunsWhenCachedInputsChange(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*recording.RecordingResult, *renderplan.EditRequest)
		musicKey  string
		withMusic bool
	}{
		{
			name: "capture revision",
			mutate: func(result *recording.RecordingResult, _ *renderplan.EditRequest) {
				result.CaptureRevision = "capture-2"
			},
		},
		{
			name: "edit treatment",
			mutate: func(_ *recording.RecordingResult, edit *renderplan.EditRequest) {
				edit.Transition = renderplan.TransitionWhip
			},
		},
		{name: "music", musicKey: "phonk", withMusic: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			store := newFakeStorage()
			id := uuid.New()
			plan := minimalKillPlan()
			repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
			rec := recordingResultWithSegment("", "C:/stale/seg-001.mp4")
			rec.CaptureRevision = "capture-1"
			cachedFingerprint, err := renderInputFingerprint(rec, &plan, editor.PresetViral60Clean, "", "", 0, renderplan.DefaultEditRequest())
			if err != nil {
				t.Fatal(err)
			}
			putJSON(t, store, mustRenderVariantResultKey(t, id, editor.PresetViral60Clean), editor.Result{
				Preset:           editor.PresetViral60Clean,
				InputFingerprint: cachedFingerprint,
				Shorts:           []editor.ShortResult{{SegmentID: "seg-001"}},
			})
			_ = store.Put(mustRenderVariantPackManifestKey(t, id, editor.PresetViral60Clean), bytes.NewReader([]byte("pack")))
			_ = store.Put(mustRenderVariantGalleryKey(t, id, editor.PresetViral60Clean), bytes.NewReader([]byte("gallery")))

			edit := renderplan.DefaultEditRequest()
			if tc.mutate != nil {
				tc.mutate(&rec, &edit)
			}
			putJSON(t, store, recording.ResultArtifactKey(id), rec)
			_ = store.Put(mustSegmentClipKey(t, id, "seg-001"), bytes.NewReader([]byte("clip")))
			musicDir := t.TempDir()
			if tc.withMusic {
				if err := os.WriteFile(filepath.Join(musicDir, tc.musicKey+".wav"), []byte("music"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			wantErr := errors.New("rerender invoked")
			runner := &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
				return nil, wantErr
			}}
			w := NewRenderWorker(repo, store, RenderWorkerConfig{
				WorkDir:    t.TempDir(),
				EditorPath: "zv-editor",
				MusicDir:   musicDir,
			})
			w.runner = runner
			task, err := tasks.NewRenderVariantTask(id, editor.PresetViral60Clean, tc.musicKey, 0, edit)
			if err != nil {
				t.Fatal(err)
			}
			err = w.HandleRenderVariant(context.Background(), task)
			if !errors.Is(err, wantErr) {
				t.Fatalf("HandleRenderVariant error = %v, want rerender sentinel", err)
			}
			if len(runner.calls) != 1 {
				t.Fatalf("runner calls = %d, want 1 for stale cache", len(runner.calls))
			}
		})
	}
}

func TestRenderWorkerPassesMusicVolume(t *testing.T) {
	cases := []struct {
		name      string
		volume    float64
		wantFlag  bool
		wantValue string
	}{
		{"custom volume threads to editor", 0.35, true, "0.35"},
		{"default volume omits flag", 0, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			store := newFakeStorage()
			id := uuid.New()
			plan := minimalKillPlan()
			repo.jobs[id] = &job.Job{ID: id, Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
			putJSON(t, store, recording.ResultArtifactKey(id), recordingResultWithSegment("", "C:/stale/seg-001.mp4"))
			_ = store.Put(mustSegmentClipKey(t, id, "seg-001"), bytes.NewReader([]byte("clip")))

			musicDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(musicDir, "phonk.wav"), []byte("music"), 0o600); err != nil {
				t.Fatal(err)
			}
			var gotArgs []string
			wantErr := errors.New("stop after args")
			runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
				gotArgs = append([]string(nil), args...)
				return nil, wantErr
			}}
			w := NewRenderWorker(repo, store, RenderWorkerConfig{
				WorkDir:    t.TempDir(),
				EditorPath: "zv-editor",
				MusicDir:   musicDir,
			})
			w.runner = runner

			task, err := tasks.NewRenderVariantTask(id, editor.PresetViral60Clean, "phonk", tc.volume, renderplan.DefaultEditRequest())
			if err != nil {
				t.Fatal(err)
			}
			if err := w.HandleRenderVariant(context.Background(), task); !errors.Is(err, wantErr) {
				t.Fatalf("HandleRenderVariant error = %v, want stop sentinel", err)
			}
			if !hasArg(gotArgs, "--music") {
				t.Fatalf("editor args missing --music: %#v", gotArgs)
			}
			if got := hasArg(gotArgs, "--music-volume"); got != tc.wantFlag {
				t.Fatalf("--music-volume present = %v, want %v: %#v", got, tc.wantFlag, gotArgs)
			}
			if tc.wantFlag {
				if got := argValue(gotArgs, "--music-volume"); got != tc.wantValue {
					t.Fatalf("--music-volume = %q, want %q", got, tc.wantValue)
				}
			}
		})
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

func TestTaskIsTerminalUsesInlineAttemptContext(t *testing.T) {
	tests := []struct {
		name     string
		retried  int
		maxRetry int
		want     bool
	}{
		{name: "intermediate attempt", retried: 0, maxRetry: 1, want: false},
		{name: "final attempt", retried: 1, maxRetry: 1, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tasks.WithTaskAttempt(context.Background(), tt.retried, tt.maxRetry)
			if got := taskIsTerminal(ctx); got != tt.want {
				t.Errorf("taskIsTerminal() = %v, want %v", got, tt.want)
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
	plan := minimalKillPlan()
	stream := recording.DefaultStreamConfig()
	stream.HUDMode = recording.HUDModeDeathnotices
	recordingPlan, err := recording.NewPlanFromKillPlan(plan, "demo.dem", "out", stream)
	if err != nil {
		panic(fmt.Sprintf("build test recording plan: %v", err))
	}
	return recording.RecordingResult{
		Plan:   recordingPlan,
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
	return recordTaskFor(t, id, nil)
}

func recordTaskFor(t *testing.T, id uuid.UUID, segmentIDs []string) *asynq.Task {
	t.Helper()
	return recordTaskWithCaptureProfile(t, id, "", segmentIDs, false)
}

func recordTaskWithCaptureProfile(t *testing.T, id uuid.UUID, hudMode string, segmentIDs []string, portraitSafeKillfeed bool) *asynq.Task {
	t.Helper()
	task, err := tasks.NewRecordDemoTask(id, hudMode, segmentIDs, portraitSafeKillfeed)
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
	task, err := tasks.NewRenderVariantTask(id, variant, "", 0, renderplan.EditRequest{})
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

func hasArg(args []string, key string) bool {
	for _, arg := range args {
		if arg == key {
			return true
		}
	}
	return false
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
	key, err := recording.SegmentClipArtifactKey(id, segmentID)
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
