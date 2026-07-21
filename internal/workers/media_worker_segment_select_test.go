package workers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/rules"
)

// multiSegmentKillPlan builds a kill plan with one segment per id, so tests can
// assert that the recorder is only handed the segments a reel selected.
func multiSegmentKillPlan(ids ...string) killplan.Plan {
	plan := minimalKillPlan()
	plan.Segments = nil
	for i, id := range ids {
		start := 64 * (i + 1)
		plan.Segments = append(plan.Segments, killplan.Segment{
			ID:        id,
			Round:     i + 1,
			TickStart: start,
			TickEnd:   start + 64,
		})
	}
	return plan
}

// planRecorderRunner mimics zv-recorder: it records exactly the segments present
// in the kill plan it is handed (writing one clip per segment plus the result),
// and records the segment ids it saw into seen so the test can assert scoping.
func planRecorderRunner(t *testing.T, seen *[]string) *fakeRunner {
	t.Helper()
	return &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		outDir := argValue(args, "--out")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatal(err)
		}
		var plan killplan.Plan
		if err := readJSONFile(argValue(args, "--killplan"), &plan); err != nil {
			t.Fatalf("read killplan: %v", err)
		}
		scriptPath := filepath.Join(outDir, "recording.js")
		if err := os.WriteFile(scriptPath, []byte("script"), 0o644); err != nil {
			t.Fatal(err)
		}
		stream := recording.DefaultStreamConfig()
		stream.HUDMode = recording.HUDMode(argValue(args, "--hud"))
		stream.PortraitSafeKillfeed = hasArg(args, "--portrait-safe-killfeed")
		recordingPlan, err := recording.NewPlanFromKillPlan(plan, "demo.dem", outDir, stream)
		if err != nil {
			t.Fatalf("build recording plan: %v", err)
		}
		result := recording.RecordingResult{
			Plan:   recordingPlan,
			Script: scriptPath,
		}
		for _, s := range plan.Segments {
			if seen != nil {
				*seen = append(*seen, s.ID)
			}
			segPath := filepath.Join(outDir, "segments", s.ID+".mp4")
			if err := os.MkdirAll(filepath.Dir(segPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(segPath, []byte("clip"), 0o644); err != nil {
				t.Fatal(err)
			}
			result.Artifacts = append(result.Artifacts, recording.RecordingArtifact{
				SegmentID: s.ID,
				Role:      "segment",
				Type:      "video",
				Path:      segPath,
				SizeBytes: 4,
			})
		}
		if err := writeJSONFile(filepath.Join(outDir, "recording-result.json"), result); err != nil {
			t.Fatal(err)
		}
		return []byte("recorded"), nil
	}}
}

func storedRecordingResult(t *testing.T, store *fakeStorage, id uuid.UUID) recording.RecordingResult {
	t.Helper()
	result, err := decodeStoredRecordingResult(store, id)
	if err != nil {
		t.Fatalf("decode stored recording result: %v", err)
	}
	return result
}

func TestRecordWorkerFiltersKillPlanToSelectedSegment(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := multiSegmentKillPlan("seg-001", "seg-002", "seg-003")
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusParsed, DemoPath: "demos/test.dem", Rules: rules.Default(), KillPlan: &plan}
	_ = store.Put("demos/test.dem", bytes.NewReader([]byte("demo")))

	var seen []string
	w := newRecordWorkerForTest(repo, store, t)
	w.runner = planRecorderRunner(t, &seen)

	if err := w.HandleRecordDemo(context.Background(), recordTaskFor(t, id, []string{"seg-002"})); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}

	if len(seen) != 1 || seen[0] != "seg-002" {
		t.Fatalf("recorder saw segments %v, want [seg-002] only", seen)
	}
	if _, ok := store.files[mustSegmentClipKey(t, id, "seg-002")]; !ok {
		t.Fatal("storage missing seg-002 clip")
	}
	for _, unwanted := range []string{"seg-001", "seg-003"} {
		if _, ok := store.files[mustSegmentClipKey(t, id, unwanted)]; ok {
			t.Fatalf("storage unexpectedly has %s clip", unwanted)
		}
	}
}

func TestRecordWorkerRecordsAllSegmentsWhenNoneSelected(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := multiSegmentKillPlan("seg-001", "seg-002", "seg-003")
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusParsed, DemoPath: "demos/test.dem", Rules: rules.Default(), KillPlan: &plan}
	_ = store.Put("demos/test.dem", bytes.NewReader([]byte("demo")))

	var seen []string
	w := newRecordWorkerForTest(repo, store, t)
	w.runner = planRecorderRunner(t, &seen)

	if err := w.HandleRecordDemo(context.Background(), recordTaskFor(t, id, nil)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("recorder saw %v, want all 3 segments", seen)
	}
}

func TestRecordWorkerRerecordsAndAccumulatesAcrossReels(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := multiSegmentKillPlan("seg-001", "seg-002", "seg-003")
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusParsed, DemoPath: "demos/test.dem", Rules: rules.Default(), KillPlan: &plan}
	_ = store.Put("demos/test.dem", bytes.NewReader([]byte("demo")))

	var seen []string
	w := newRecordWorkerForTest(repo, store, t)
	w.runner = planRecorderRunner(t, &seen)

	// First reel records seg-001.
	if err := w.HandleRecordDemo(context.Background(), recordTaskFor(t, id, []string{"seg-001"})); err != nil {
		t.Fatalf("first record error = %v", err)
	}
	// Second reel for a different clip must re-run the recorder, not skip.
	if err := w.HandleRecordDemo(context.Background(), recordTaskFor(t, id, []string{"seg-002"})); err != nil {
		t.Fatalf("second record error = %v", err)
	}
	if want := []string{"seg-001", "seg-002"}; len(seen) != 2 || seen[0] != want[0] || seen[1] != want[1] {
		t.Fatalf("recorder saw %v across reels, want %v (re-record, not skip)", seen, want)
	}

	// The job-level result must accumulate both segments so the render can find
	// either clip; without the merge the second run would clobber the first.
	result := storedRecordingResult(t, store, id)
	got := map[string]bool{}
	for _, sid := range recording.SegmentIDs(result) {
		got[sid] = true
	}
	if !got["seg-001"] || !got["seg-002"] {
		t.Fatalf("stored result segments = %v, want both seg-001 and seg-002", recording.SegmentIDs(result))
	}
	for _, sid := range []string{"seg-001", "seg-002"} {
		if _, ok := store.files[mustSegmentClipKey(t, id, sid)]; !ok {
			t.Fatalf("storage missing %s clip after second reel", sid)
		}
	}
}

func TestRecordWorkerSkipsWhenSelectedSegmentAlreadyRecorded(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := multiSegmentKillPlan("seg-001", "seg-002")
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusParsed, DemoPath: "demos/test.dem", Rules: rules.Default(), KillPlan: &plan}
	_ = store.Put("demos/test.dem", bytes.NewReader([]byte("demo")))

	var seen []string
	w := newRecordWorkerForTest(repo, store, t)
	w.runner = planRecorderRunner(t, &seen)

	for i := range 2 {
		if err := w.HandleRecordDemo(context.Background(), recordTaskFor(t, id, []string{"seg-001"})); err != nil {
			t.Fatalf("record %d error = %v", i, err)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("recorder ran %d times for the same segment, want 1 (idempotent skip)", len(seen))
	}
}

func TestRecordWorkerInvalidatesGameplayCaptureWhenPortraitSafetyChanges(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := multiSegmentKillPlan("seg-001", "seg-002")
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusParsed, DemoPath: "demos/test.dem", Rules: rules.Default(), KillPlan: &plan}
	_ = store.Put("demos/test.dem", bytes.NewReader([]byte("demo")))

	var seen []string
	w := newRecordWorkerForTest(repo, store, t)
	w.runner = planRecorderRunner(t, &seen)

	// The old Full HUD profile records one segment without a portrait-safe native
	// killfeed. A later portrait-safe Full HUD request must not reuse or merge it.
	if err := w.HandleRecordDemo(context.Background(), recordTaskWithCaptureProfile(t, id, "gameplay", []string{"seg-001"}, false)); err != nil {
		t.Fatalf("unsafe record error = %v", err)
	}
	if err := w.HandleRecordDemo(context.Background(), recordTaskWithCaptureProfile(t, id, "gameplay", []string{"seg-002"}, true)); err != nil {
		t.Fatalf("portrait-safe record error = %v", err)
	}
	if len(seen) != 2 {
		t.Fatalf("recorder runs = %d, want 2 after portrait profile changed", len(seen))
	}
	result := storedRecordingResult(t, store, id)
	if got := recording.SegmentIDs(result); len(got) != 1 || got[0] != "seg-002" {
		t.Fatalf("segments after profile change = %v, want only [seg-002]", got)
	}
	if !result.Plan.Stream.PortraitSafeKillfeed {
		t.Fatal("stored profile is not portrait-safe")
	}

	// Re-recording seg-001 under the new profile may now accumulate with seg-002;
	// a fourth identical request proves the resulting profile remains idempotent.
	portraitTask := func(segmentID string) *asynq.Task {
		return recordTaskWithCaptureProfile(t, id, "gameplay", []string{segmentID}, true)
	}
	if err := w.HandleRecordDemo(context.Background(), portraitTask("seg-001")); err != nil {
		t.Fatalf("portrait-safe backfill error = %v", err)
	}
	if err := w.HandleRecordDemo(context.Background(), portraitTask("seg-001")); err != nil {
		t.Fatalf("portrait-safe retry error = %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("recorder runs = %d, want 3 after identical retry skips", len(seen))
	}
	result = storedRecordingResult(t, store, id)
	if got := recording.SegmentIDs(result); len(got) != 2 || got[0] != "seg-001" || got[1] != "seg-002" {
		t.Fatalf("segments after compatible merge = %v, want [seg-001 seg-002]", got)
	}
}

func TestRecordWorkerFailedReelPreservesPriorReelResult(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := multiSegmentKillPlan("seg-001", "seg-002")
	repo.jobs[id] = &job.Job{ID: id, Status: job.StatusParsed, DemoPath: "demos/test.dem", Rules: rules.Default(), KillPlan: &plan}
	_ = store.Put("demos/test.dem", bytes.NewReader([]byte("demo")))

	w := newRecordWorkerForTest(repo, store, t)

	// First reel records seg-001 successfully.
	w.runner = planRecorderRunner(t, nil)
	if err := w.HandleRecordDemo(context.Background(), recordTaskFor(t, id, []string{"seg-001"})); err != nil {
		t.Fatalf("first record error = %v", err)
	}

	// Second reel for seg-002 fails inside the recorder.
	w.runner = &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		outDir := argValue(args, "--out")
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatal(err)
		}
		failed := recording.RecordingResult{
			Plan: recording.RecordingPlan{
				DemoPath: "demo.dem", OutputDir: outDir, TargetSteamID64: "76561197960265729",
				TargetAccountID: 1, Tickrate: 64, Stream: recording.DefaultStreamConfig(),
				Segments: []recording.RecordingSegment{{ID: "seg-002", TickStart: 128, TickEnd: 192}},
			},
			Error: "recorder boom",
		}
		if err := writeJSONFile(filepath.Join(outDir, "recording-result.json"), failed); err != nil {
			t.Fatal(err)
		}
		return nil, errors.New("recorder boom")
	}}
	if err := w.HandleRecordDemo(context.Background(), recordTaskFor(t, id, []string{"seg-002"})); err == nil {
		t.Fatal("second record should have failed")
	}

	// The first reel's segment must survive the failed second reel.
	result := storedRecordingResult(t, store, id)
	if result.Error != "" {
		t.Fatalf("stored result Error = %q, want preserved good result", result.Error)
	}
	ids := recording.SegmentIDs(result)
	if len(ids) != 1 || ids[0] != "seg-001" {
		t.Fatalf("stored result segments = %v, want [seg-001] preserved", ids)
	}
	if _, ok := store.files[mustSegmentClipKey(t, id, "seg-001")]; !ok {
		t.Fatal("seg-001 clip lost after failed second reel")
	}
}

func TestRenderCoversToleratesPlanSegmentWithoutClip(t *testing.T) {
	store := newFakeStorage()
	id := uuid.New()
	// Partial capture: the plan lists seg-001 and seg-002 but only seg-001 has a
	// clip. The editor only renders clip-bearing segments, so coverage must not
	// demand a short for seg-002 (which would loop the render forever).
	rec := recording.RecordingResult{
		Plan:      recording.RecordingPlan{Segments: []recording.RecordingSegment{{ID: "seg-001"}, {ID: "seg-002"}}},
		Artifacts: []recording.RecordingArtifact{{SegmentID: "seg-001", Role: "segment", Type: "video"}},
	}
	if err := putRecordingResult(store, id, rec); err != nil {
		t.Fatal(err)
	}
	covered, err := renderCoversRecordedSegments(store, id, editor.Result{
		Shorts: []editor.ShortResult{{SegmentID: "seg-001"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !covered {
		t.Fatal("a plan segment without a clip must not make render coverage unsatisfiable")
	}
}

func TestRenderVariantOutputsReadyRequiresSegmentCoverage(t *testing.T) {
	store := newFakeStorage()
	id := uuid.New()

	// Recording result holds two segments (two reels recorded).
	rec := recording.RecordingResult{
		Plan: recording.RecordingPlan{Segments: []recording.RecordingSegment{{ID: "seg-001"}, {ID: "seg-002"}}},
		Artifacts: []recording.RecordingArtifact{
			{SegmentID: "seg-001", Role: "segment", Type: "video"},
			{SegmentID: "seg-002", Role: "segment", Type: "video"},
		},
	}
	if err := putRecordingResult(store, id, rec); err != nil {
		t.Fatal(err)
	}

	covered, err := renderCoversRecordedSegments(store, id, editor.Result{
		Shorts: []editor.ShortResult{{SegmentID: "seg-001"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if covered {
		t.Fatal("render covering only seg-001 should NOT cover a 2-segment recording")
	}

	covered, err = renderCoversRecordedSegments(store, id, editor.Result{
		Shorts: []editor.ShortResult{{SegmentID: "seg-001"}, {SegmentID: "seg-002"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !covered {
		t.Fatal("render covering both segments should be considered covered")
	}

	// A compilation render is always treated as covered (different render mode).
	covered, err = renderCoversRecordedSegments(store, id, editor.Result{
		Shorts: []editor.ShortResult{{SegmentID: compilationSegmentID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !covered {
		t.Fatal("compilation render should be treated as covered")
	}
}

func TestRenderVariantOutputsReadyRequiresMatchingInputFingerprint(t *testing.T) {
	store := newFakeStorage()
	id := uuid.New()
	plan := minimalKillPlan()
	rec := recordingResultWithSegment("", "C:/stale/seg-001.mp4")
	rec.CaptureRevision = "capture-1"
	if err := putRecordingResult(store, id, rec); err != nil {
		t.Fatal(err)
	}
	edit := renderplan.DefaultEditRequest()
	fingerprint, err := renderInputFingerprint(rec, &plan, editor.PresetViral60Clean, "", "", 0, edit)
	if err != nil {
		t.Fatal(err)
	}
	putJSON(t, store, mustRenderVariantResultKey(t, id, editor.PresetViral60Clean), editor.Result{
		Preset:           editor.PresetViral60Clean,
		InputFingerprint: fingerprint,
		Shorts:           []editor.ShortResult{{SegmentID: "seg-001"}},
	})
	_ = store.Put(mustRenderVariantPackManifestKey(t, id, editor.PresetViral60Clean), bytes.NewReader([]byte("pack")))
	_ = store.Put(mustRenderVariantGalleryKey(t, id, editor.PresetViral60Clean), bytes.NewReader([]byte("gallery")))

	ready, _, err := renderVariantOutputsReady(store, id, editor.PresetViral60Clean, fingerprint)
	if err != nil || !ready {
		t.Fatalf("matching inputs ready/error = %v/%v, want true/nil", ready, err)
	}

	recaptured := rec
	recaptured.CaptureRevision = "capture-2"
	changedCapture, err := renderInputFingerprint(recaptured, &plan, editor.PresetViral60Clean, "", "", 0, edit)
	if err != nil {
		t.Fatal(err)
	}
	changedEdit := edit
	changedEdit.Transition = renderplan.TransitionWhip
	changedTreatment, err := renderInputFingerprint(rec, &plan, editor.PresetViral60Clean, "", "", 0, changedEdit)
	if err != nil {
		t.Fatal(err)
	}
	musicPath := filepath.Join(t.TempDir(), "phonk.wav")
	if err := os.WriteFile(musicPath, []byte("music-v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	changedMusic, err := renderInputFingerprint(rec, &plan, editor.PresetViral60Clean, "phonk", musicPath, 0, edit)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(musicPath, []byte("music-v2"), 0o600); err != nil {
		t.Fatal(err)
	}
	changedMusicContent, err := renderInputFingerprint(rec, &plan, editor.PresetViral60Clean, "phonk", musicPath, 0, edit)
	if err != nil {
		t.Fatal(err)
	}
	if changedMusic == changedMusicContent {
		t.Fatal("music content change did not change render fingerprint")
	}
	changedMusicVolume, err := renderInputFingerprint(rec, &plan, editor.PresetViral60Clean, "phonk", musicPath, 0.5, edit)
	if err != nil {
		t.Fatal(err)
	}
	if changedMusicVolume == changedMusicContent {
		t.Fatal("music volume change did not change render fingerprint")
	}

	for name, candidate := range map[string]string{
		"capture revision": changedCapture,
		"edit treatment":   changedTreatment,
		"music":            changedMusic,
		"music content":    changedMusicContent,
		"music volume":     changedMusicVolume,
		"legacy empty":     "",
	} {
		t.Run(name, func(t *testing.T) {
			ready, _, err := renderVariantOutputsReady(store, id, editor.PresetViral60Clean, candidate)
			if err != nil {
				t.Fatal(err)
			}
			if ready {
				t.Fatal("stale render inputs were reused")
			}
		})
	}
}
