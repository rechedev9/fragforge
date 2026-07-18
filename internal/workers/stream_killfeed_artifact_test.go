package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/streamkillfeed"
	"github.com/rechedev9/fragforge/internal/tasks"
)

func TestStreamKillfeedWorkerPersistsNearbyEventsAndBurstRowsSeparately(t *testing.T) {
	store := newFakeStorage()
	jobID := uuid.New()
	generationID := uuid.New()
	plan, state := queuedKillfeedAnalysisFixture(t, jobID, generationID)
	plan.Clips = plan.Clips[:1]
	state.Clips = state.Clips[:1]
	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(
		state.SourceSHA256, state.KillfeedCrop, plan.Clips,
	)
	if err != nil {
		t.Fatal(err)
	}
	state.Fingerprint = fingerprint
	if err := store.Put(streamclips.SourceKey(jobID), strings.NewReader("source-video")); err != nil {
		t.Fatal(err)
	}
	putKillfeedStateForTest(t, store, streamclips.KillfeedAnalysisKey(jobID), state)
	generationKey, err := streamclips.KillfeedAnalysisGenerationKey(jobID, generationID)
	if err != nil {
		t.Fatal(err)
	}
	putKillfeedStateForTest(t, store, generationKey, state)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: jobID, Status: streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(jobID), SourceSHA256: state.SourceSHA256,
		Probe:    streamclips.SourceProbe{DurationSeconds: 5, VideoTimeBase: "1/30000"},
		EditPlan: planJSON,
	})

	eventA := exactKillfeedEvent()
	eventA.EventID = "event-a"
	eventB := exactKillfeedEvent()
	eventB.EventID = "event-b"
	eventB.SourcePTS = 16001
	eventB.OnsetStartPTS = 15000
	eventB.OnsetEndPTS = 16001
	eventB.CueSeconds = float64(eventB.SourcePTS) / 30000
	eventB.SamplePTS = 25001
	eventB.SampleSeconds = float64(eventB.SamplePTS) / 30000
	eventB.Mode = streamkillfeed.ModeBurst
	eventB.Rows = []streamkillfeed.RowEvidence{
		{
			OnsetRowIndex: 0, SampleRowIndex: 0, Fingerprint: "event-b-row-0",
			OnsetBounds:  streamclips.NoticeRow{X: 10, Y: 20, Width: 100, Height: 30},
			SampleBounds: streamclips.NoticeRow{X: 10, Y: 20, Width: 100, Height: 30},
		},
		{
			OnsetRowIndex: 1, SampleRowIndex: 1, Fingerprint: "event-b-row-1",
			OnsetBounds:  streamclips.NoticeRow{X: 10, Y: 55, Width: 100, Height: 30},
			SampleBounds: streamclips.NoticeRow{X: 10, Y: 55, Width: 100, Height: 30},
		},
	}
	red := solidPNGForTest(t, 0xff, 0, 0)
	green := solidPNGForTest(t, 0, 0xff, 0)
	blue := solidPNGForTest(t, 0, 0, 0xff)
	scanner := &fakeKillfeedScanner{eventsByClip: map[string][]streamkillfeed.Event{
		"clip-001": {eventA, eventB},
	}}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg",
	})
	w.killfeedScanner = scanner
	w.extractKillfeedRows = func(
		_ context.Context,
		_ string,
		_ streamclips.SourceProbe,
		event streamkillfeed.Event,
	) ([][]byte, error) {
		switch event.EventID {
		case "event-a":
			return [][]byte{red}, nil
		case "event-b":
			return [][]byte{green, blue}, nil
		default:
			t.Fatalf("unexpected event extraction %q", event.EventID)
			return nil, nil
		}
	}
	task, err := tasks.NewGenerateStreamKillfeedTask(jobID, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleGenerateStreamKillfeed(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	wantRows := map[string][]byte{
		"event-a/0": red,
		"event-b/0": green,
		"event-b/1": blue,
	}
	for identity, want := range wantRows {
		parts := strings.Split(identity, "/")
		rowIndex := 0
		if parts[1] == "1" {
			rowIndex = 1
		}
		key, err := streamclips.KillfeedEventRowKey(
			jobID, generationID, "clip-001", parts[0], rowIndex,
		)
		if err != nil {
			t.Fatal(err)
		}
		rc, err := store.Open(key)
		if err != nil {
			t.Fatalf("open %s: %v", identity, err)
		}
		got, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %s: %v / %v", identity, readErr, closeErr)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("artifact %s contains another event's row", identity)
		}
	}
	got, exists, err := readKillfeedAnalysisState(store, generationKey)
	if err != nil || !exists {
		t.Fatalf("read generation: exists=%v err=%v", exists, err)
	}
	if got.Status != streamclips.KillfeedAnalysisReady || len(got.Clips[0].Events) != 2 {
		t.Fatalf("analysis = status %s events %d, want ready/2", got.Status, len(got.Clips[0].Events))
	}
}

func TestStreamKillfeedWorkerOmitsLeftCensoredEventAndFinishesReady(t *testing.T) {
	store := newFakeStorage()
	jobID := uuid.New()
	generationID := uuid.New()
	plan, state := queuedKillfeedAnalysisFixture(t, jobID, generationID)
	plan.Clips = plan.Clips[:1]
	state.Clips = state.Clips[:1]
	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(
		state.SourceSHA256, state.KillfeedCrop, plan.Clips,
	)
	if err != nil {
		t.Fatal(err)
	}
	state.Fingerprint = fingerprint
	if err := store.Put(streamclips.SourceKey(jobID), strings.NewReader("source-video")); err != nil {
		t.Fatal(err)
	}
	putKillfeedStateForTest(t, store, streamclips.KillfeedAnalysisKey(jobID), state)
	generationKey, err := streamclips.KillfeedAnalysisGenerationKey(jobID, generationID)
	if err != nil {
		t.Fatal(err)
	}
	putKillfeedStateForTest(t, store, generationKey, state)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: jobID, Status: streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(jobID), SourceSHA256: state.SourceSHA256,
		Probe:    streamclips.SourceProbe{DurationSeconds: 5, VideoTimeBase: "1/30000"},
		EditPlan: planJSON,
	})
	unresolved := exactKillfeedEvent()
	unresolved.EventID = "preexisting"
	unresolved.SourcePTS = 0
	unresolved.OnsetStartPTS = 0
	unresolved.OnsetEndPTS = 0
	unresolved.CueSeconds = 0
	unresolved.SamplePTS = 9000
	unresolved.SampleSeconds = 0.3
	unresolved.Mode = streamkillfeed.ModeUnresolved
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg",
	})
	w.killfeedScanner = &fakeKillfeedScanner{eventsByClip: map[string][]streamkillfeed.Event{
		"clip-001": {unresolved},
	}}
	w.extractKillfeedRows = func(
		context.Context,
		string,
		streamclips.SourceProbe,
		streamkillfeed.Event,
	) ([][]byte, error) {
		t.Fatal("left-censored event must not be captured")
		return nil, nil
	}
	task, err := tasks.NewGenerateStreamKillfeedTask(jobID, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleGenerateStreamKillfeed(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	got, exists, err := readKillfeedAnalysisState(store, generationKey)
	if err != nil || !exists {
		t.Fatalf("read generation: exists=%v err=%v", exists, err)
	}
	if got.Status != streamclips.KillfeedAnalysisReady || len(got.Clips[0].Events) != 0 {
		t.Fatalf("analysis = status %s events %d, want ready/0", got.Status, len(got.Clips[0].Events))
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "left-censored") {
		t.Fatalf("top-level warnings = %v, want left-censored warning", got.Warnings)
	}
	if len(got.Clips[0].Warnings) != 1 || !strings.Contains(got.Clips[0].Warnings[0], "left-censored") {
		t.Fatalf("clip warnings = %v, want left-censored warning", got.Clips[0].Warnings)
	}
}

func TestStreamRenderWorkerUsesDistinctExactArtifactsForNearbyCues(t *testing.T) {
	store := newFakeStorage()
	jobID, plan := newReadyStreamJobWithCaptions(t, store, false)
	plan.KillfeedCrop = &streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	eventA := durableKillfeedEvent(exactKillfeedEvent())
	eventA.EventID = "event-a"
	eventBSource := exactKillfeedEvent()
	eventBSource.EventID = "event-b"
	eventBSource.SourcePTS = 16001
	eventBSource.OnsetStartPTS = 15000
	eventBSource.OnsetEndPTS = 16001
	eventBSource.CueSeconds = float64(eventBSource.SourcePTS) / 30000
	eventBSource.SamplePTS = 25001
	eventBSource.SampleSeconds = float64(eventBSource.SamplePTS) / 30000
	eventB := durableKillfeedEvent(eventBSource)
	eventB.Rows = append(eventB.Rows, eventB.Rows[0])
	eventB.Rows[1].Fingerprint = "event-b-second-row"
	eventB.Rows[1].OnsetRowIndex = 1
	eventB.Rows[1].SampleRowIndex = 1
	eventB.Rows[1].OnsetBounds.Y += 35
	eventB.Rows[1].SampleBounds.Y += 35
	plan.Clips[0].KillfeedSeconds = []float64{eventA.CueSeconds, eventB.CueSeconds}
	plan.Clips[0].KillfeedKills = [][]streamclips.KillfeedKill{
		{},
		{{
			AttackerSide: "CT", AttackerName: "ocr-resolved", VictimSide: "T",
			VictimName: "only-one-of-two-rows", Weapon: "ak47",
		}},
	}
	plan.Clips[0].KillfeedCueProvenance = []streamclips.KillfeedCueProvenance{
		{CueSeconds: eventA.CueSeconds, Origin: streamclips.KillfeedCueAutomatic, EventID: eventA.EventID},
		{CueSeconds: eventB.CueSeconds, Origin: streamclips.KillfeedCueAutomatic, EventID: eventB.EventID},
	}
	const sourceSHA = "nearby-events-source"
	analysis := appliedKillfeedStateForEvents(
		t, store, jobID, sourceSHA, &plan,
		[]streamclips.KillfeedAnalysisEvent{eventA, eventB},
	)
	red := solidPNGForTest(t, 0xff, 0, 0)
	green := solidPNGForTest(t, 0, 0xff, 0)
	blue := solidPNGForTest(t, 0, 0, 0xff)
	for _, artifact := range []struct {
		eventID  string
		rowIndex int
		data     []byte
	}{
		{eventID: "event-a", rowIndex: 0, data: red},
		{eventID: "event-b", rowIndex: 0, data: green},
		{eventID: "event-b", rowIndex: 1, data: blue},
	} {
		key, err := streamclips.KillfeedEventRowKey(
			jobID, analysis.GenerationID, "clip-001", artifact.eventID, artifact.rowIndex,
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Put(key, bytes.NewReader(artifact.data)); err != nil {
			t.Fatal(err)
		}
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: jobID, Status: streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(jobID), SourceSHA256: sourceSHA,
		Probe: streamclips.SourceProbe{DurationSeconds: 2}, EditPlan: planJSON,
	})
	materialized := make(map[string][]byte)
	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		for _, arg := range args {
			slash := filepath.ToSlash(arg)
			if !strings.Contains(slash, "/killfeed-captured/clip-001/") || !strings.HasSuffix(slash, ".png") {
				continue
			}
			data, err := os.ReadFile(arg)
			if err != nil {
				return nil, err
			}
			materialized[filepath.Base(filepath.Dir(arg))] = data
		}
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("video"), 0o600)
	}}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg",
		RequireAppliedKillfeedAnalysis: true,
	})
	w.runner = runner
	planFingerprint, err := streamclips.EditPlanFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasks.NewBoundRenderStreamClipTask(jobID, streamclips.VariantStreamer4060, tasks.StreamRenderIntent{
		AttemptID:           uuid.New(),
		EditPlanFingerprint: planFingerprint,
		KillfeedGeneration:  analysis.GenerationID,
		KillfeedFingerprint: analysis.Fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	seedBoundStreamRenderAttemptForTest(t, w, jobID, streamclips.VariantStreamer4060, task)
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(materialized["event-a"], red) {
		t.Fatal("event A render input contains event B or is missing")
	}
	if !bytes.Equal(materialized["event-b"], blue) {
		t.Fatal("event B render input contains event A or is missing")
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want one render", len(runner.calls))
	}
	filter := argValue(runner.calls[0].args, "-filter_complex")
	for _, want := range []string{"nsharp0_0", "nsharp1_0", "nsharp1_1"} {
		if !strings.Contains(filter, want) {
			t.Fatalf("filter missing distinct cue input %q: %s", want, filter)
		}
	}
	if strings.Contains(filter, "killfeedin") {
		t.Fatalf("automatic render fell back to full-column crop: %s", filter)
	}
}

func TestAnalyzedKillfeedRejectsManualCueChangeInsteadOfUsingPositionalArtifact(t *testing.T) {
	store := newFakeStorage()
	jobID := uuid.New()
	analysis := streamclips.KillfeedAnalysisState{
		JobID: jobID, GenerationID: uuid.New(), Status: streamclips.KillfeedAnalysisApplied,
		Clips: []streamclips.KillfeedAnalysisClip{{
			ClipID: "clip-001", StartSeconds: 0, EndSeconds: 2,
			Events: []streamclips.KillfeedAnalysisEvent{durableKillfeedEvent(exactKillfeedEvent())},
		}},
	}
	clip := streamclips.ClipRange{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 2,
		KillfeedSeconds: []float64{0.49}, KillfeedKills: [][]streamclips.KillfeedKill{{}},
	}
	w := NewStreamRenderWorker(newFakeStreamRepo(), store, StreamRenderWorkerConfig{})
	_, err := w.materializeAnalyzedKillfeedNotices(t.TempDir(), jobID, analysis, clip)
	if err == nil || !strings.Contains(err.Error(), "no exact captured killfeed event") {
		t.Fatalf("materialize error = %v, want exact-cue rejection", err)
	}
}

func TestAnalyzedKillfeedExplicitManualCueUsesSyntheticNoticeAtMatchingTime(t *testing.T) {
	store := newFakeStorage()
	jobID := uuid.New()
	event := durableKillfeedEvent(exactKillfeedEvent())
	analysis := streamclips.KillfeedAnalysisState{
		JobID: jobID, GenerationID: uuid.New(), Status: streamclips.KillfeedAnalysisApplied,
		Clips: []streamclips.KillfeedAnalysisClip{{
			ClipID: "clip-001", StartSeconds: 0, EndSeconds: 2,
			Events: []streamclips.KillfeedAnalysisEvent{event},
		}},
	}
	clip := streamclips.ClipRange{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 2,
		KillfeedSeconds: []float64{event.CueSeconds},
		KillfeedKills: [][]streamclips.KillfeedKill{{{
			AttackerSide: "CT", AttackerName: "manual", VictimSide: "T",
			VictimName: "reviewed", Weapon: "ak47",
		}}},
		KillfeedCueProvenance: []streamclips.KillfeedCueProvenance{{
			CueSeconds: event.CueSeconds, Origin: streamclips.KillfeedCueManual,
		}},
	}
	w := NewStreamRenderWorker(newFakeStreamRepo(), store, StreamRenderWorkerConfig{})
	paths, err := w.materializeAnalyzedKillfeedNotices(t.TempDir(), jobID, analysis, clip)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || len(paths[0]) != 1 {
		t.Fatalf("manual notice paths = %#v, want one synthetic notice", paths)
	}
	if strings.Contains(filepath.ToSlash(paths[0][0]), "/killfeed-captured/") {
		t.Fatalf("manual cue used automatic capture %q", paths[0][0])
	}
}

func TestAutomaticKillfeedProvenanceNeverRendersWithoutExactAnalysis(t *testing.T) {
	plan := streamclips.DefaultEditPlan()
	plan.KillfeedCrop = &streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	plan.Clips = []streamclips.ClipRange{{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 2,
		KillfeedSeconds: []float64{0.5},
		KillfeedKills: [][]streamclips.KillfeedKill{{{
			AttackerSide: "CT", AttackerName: "ocr", VictimSide: "T",
			VictimName: "reviewed", Weapon: "ak47",
		}}},
		KillfeedCueProvenance: []streamclips.KillfeedCueProvenance{{
			CueSeconds: 0.5, Origin: streamclips.KillfeedCueAutomatic,
		}},
	}}
	w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{})
	_, err := w.appliedKillfeedAnalysis(streamclips.Job{SourceSHA256: "source"}, plan)
	if err == nil || !strings.Contains(err.Error(), "require exact applied analysis") {
		t.Fatalf("appliedKillfeedAnalysis error = %v, want exact-analysis gate", err)
	}
}

func TestAppliedKillfeedUsesGenerationAsAuthorityWhenPointerBodyIsStale(t *testing.T) {
	store := newFakeStorage()
	jobID, plan := newReadyStreamJobWithCaptions(t, store, false)
	plan.KillfeedCrop = &streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	const sourceSHA = "pointer-stale-source"
	generation := appliedKillfeedStateForEvents(t, store, jobID, sourceSHA, &plan, []streamclips.KillfeedAnalysisEvent{})
	pointer := generation
	pointer.Status = streamclips.KillfeedAnalysisReady
	pointer.Fingerprint = "stale-pointer-body"
	putKillfeedStateForTest(t, store, streamclips.KillfeedAnalysisKey(jobID), pointer)
	w := NewStreamRenderWorker(newFakeStreamRepo(), store, StreamRenderWorkerConfig{
		RequireAppliedKillfeedAnalysis: true,
	})
	got, err := w.appliedKillfeedAnalysis(streamclips.Job{
		ID: jobID, SourceSHA256: sourceSHA,
	}, plan)
	if err != nil {
		t.Fatalf("appliedKillfeedAnalysis error = %v", err)
	}
	if got.GenerationID != generation.GenerationID || got.Status != streamclips.KillfeedAnalysisApplied {
		t.Fatalf("generation = %s/%s, want authoritative applied %s", got.GenerationID, got.Status, generation.GenerationID)
	}
}

func TestSupersededKillfeedRenderStopsWithoutFailingParentJob(t *testing.T) {
	store := newFakeStorage()
	jobID, plan := newReadyStreamJobWithCaptions(t, store, false)
	plan.KillfeedCrop = &streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	const sourceSHA = "superseded-source"
	oldState := appliedKillfeedStateForEvents(t, store, jobID, sourceSHA, &plan, []streamclips.KillfeedAnalysisEvent{})
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: jobID, Status: streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(jobID), SourceSHA256: sourceSHA,
		Probe: streamclips.SourceProbe{DurationSeconds: 2}, EditPlan: planJSON,
	})
	newState := oldState
	newState.GenerationID = uuid.New()
	newState.Status = streamclips.KillfeedAnalysisQueued
	newState.UpdatedAt = time.Now().UTC()
	putKillfeedStateForTest(t, store, streamclips.KillfeedAnalysisKey(jobID), newState)
	runner := &fakeRunner{}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg",
		RequireAppliedKillfeedAnalysis: true,
	})
	w.runner = runner
	planFingerprint, err := streamclips.EditPlanFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasks.NewBoundRenderStreamClipTask(jobID, streamclips.VariantStreamer4060, tasks.StreamRenderIntent{
		AttemptID:           uuid.New(),
		EditPlanFingerprint: planFingerprint,
		KillfeedGeneration:  oldState.GenerationID,
		KillfeedFingerprint: oldState.Fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	seedBoundStreamRenderAttemptForTest(t, w, jobID, streamclips.VariantStreamer4060, task)
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("superseded render returned retryable error: %v", err)
	}
	if got := repo.jobs[jobID]; got.Status != streamclips.StatusReady || got.FailureReason != "" {
		t.Fatalf("parent job = status %s failure %q, want unchanged ready", got.Status, got.FailureReason)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want zero for superseded task", len(runner.calls))
	}
	stateKey, err := streamclips.RenderStateKey(jobID, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	rc, err := store.Open(stateKey)
	if err != nil {
		t.Fatal(err)
	}
	var renderState streamclips.RenderState
	decodeErr := json.NewDecoder(rc).Decode(&renderState)
	closeErr := rc.Close()
	if decodeErr != nil || closeErr != nil {
		t.Fatalf("read render state: %v / %v", decodeErr, closeErr)
	}
	if renderState.Status != streamclips.StatusFailed || !strings.Contains(renderState.Error, "no longer current") {
		t.Fatalf("render state = status %s error %q, want actionable superseded failure", renderState.Status, renderState.Error)
	}
}

func solidPNGForTest(t *testing.T, red, green, blue uint8) []byte {
	t.Helper()
	const sourceWidth = 100
	const sourceHeight = 30
	width := (sourceWidth*streamclips.KillfeedNoticeHeight + sourceHeight/2) / sourceHeight
	return solidSizedPNGForTest(t, width, streamclips.KillfeedNoticeHeight, red, green, blue)
}

func solidSizedPNGForTest(t *testing.T, width, height int, red, green, blue uint8) []byte {
	t.Helper()
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < frame.Bounds().Dy(); y++ {
		for x := 0; x < frame.Bounds().Dx(); x++ {
			frame.SetRGBA(x, y, color.RGBA{R: red, G: green, B: blue, A: 0xff})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, frame); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func truncatePNGAfterValidConfigForTest(t *testing.T, valid []byte) []byte {
	t.Helper()
	for size := len(valid) - 1; size > 8; size-- {
		candidate := valid[:size]
		if _, err := png.DecodeConfig(bytes.NewReader(candidate)); err != nil {
			continue
		}
		if _, err := png.Decode(bytes.NewReader(candidate)); err != nil {
			return append([]byte(nil), candidate...)
		}
	}
	t.Fatal("could not construct a truncated PNG with a valid configuration header")
	return nil
}

func appliedKillfeedStateForEvents(
	t *testing.T,
	store *fakeStorage,
	jobID uuid.UUID,
	sourceSHA string,
	plan *streamclips.EditPlan,
	events []streamclips.KillfeedAnalysisEvent,
) streamclips.KillfeedAnalysisState {
	t.Helper()
	if plan.KillfeedCrop == nil {
		t.Fatal("test plan has no killfeed crop")
	}
	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(
		sourceSHA, *plan.KillfeedCrop, plan.Clips,
	)
	if err != nil {
		t.Fatal(err)
	}
	generationID := uuid.New()
	now := time.Now().UTC()
	plan.KillfeedAnalysis = &streamclips.KillfeedAnalysisMetadata{
		GenerationID: generationID, Fingerprint: fingerprint, AppliedAt: now,
	}
	state := streamclips.KillfeedAnalysisState{
		JobID: jobID, GenerationID: generationID,
		Status: streamclips.KillfeedAnalysisApplied, SourceSHA256: sourceSHA,
		KillfeedCrop: *plan.KillfeedCrop, Fingerprint: fingerprint,
		Clips: []streamclips.KillfeedAnalysisClip{{
			ClipID: plan.Clips[0].ID, StartSeconds: plan.Clips[0].StartSeconds,
			EndSeconds: plan.Clips[0].EndSeconds, Events: events,
		}},
		UpdatedAt: now,
	}
	putKillfeedStateForTest(t, store, streamclips.KillfeedAnalysisKey(jobID), state)
	key, err := streamclips.KillfeedAnalysisGenerationKey(jobID, generationID)
	if err != nil {
		t.Fatal(err)
	}
	putKillfeedStateForTest(t, store, key, state)
	return state
}
