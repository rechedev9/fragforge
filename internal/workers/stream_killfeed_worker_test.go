package workers

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/streamkillfeed"
	"github.com/rechedev9/fragforge/internal/tasks"
)

type fakeKillfeedScanner struct {
	eventsByClip map[string][]streamkillfeed.Event
	paths        []string
	sources      [][]byte
	scan         func(streamclips.ClipRange)
}

func (f *fakeKillfeedScanner) Scan(
	_ context.Context,
	sourcePath string,
	_ streamclips.SourceProbe,
	_ streamclips.CropRect,
	clip streamclips.ClipRange,
) ([]streamkillfeed.Event, error) {
	f.paths = append(f.paths, sourcePath)
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, err
	}
	f.sources = append(f.sources, source)
	if f.scan != nil {
		f.scan(clip)
	}
	return append([]streamkillfeed.Event(nil), f.eventsByClip[clip.ID]...), nil
}

func TestStreamKillfeedWorkerPersistsReadyPTSWithEmptyKillsWithoutXAI(t *testing.T) {
	store := newFakeStorage()
	id := uuid.New()
	generationID := uuid.New()
	plan, state := queuedKillfeedAnalysisFixture(t, id, generationID)
	_ = store.Put(streamclips.SourceKey(id), strings.NewReader("source-video"))
	putKillfeedStateForTest(t, store, streamclips.KillfeedAnalysisKey(id), state)
	generationKey, err := streamclips.KillfeedAnalysisGenerationKey(id, generationID)
	if err != nil {
		t.Fatal(err)
	}
	putKillfeedStateForTest(t, store, generationKey, state)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID:           id,
		Status:       streamclips.StatusReady,
		SourcePath:   streamclips.SourceKey(id),
		SourceSHA256: state.SourceSHA256,
		Probe: streamclips.SourceProbe{
			DurationSeconds: 5,
			VideoTimeBase:   "1/30000",
		},
		EditPlan: planJSON,
	})
	scanner := &fakeKillfeedScanner{eventsByClip: map[string][]streamkillfeed.Event{
		"clip-001": {exactKillfeedEvent()},
		"clip-002": {},
	}}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir:                        t.TempDir(),
		FFmpegPath:                     "ffmpeg",
		RequireAppliedKillfeedAnalysis: true,
	})
	w.killfeedScanner = scanner
	w.extractKillfeedRows = func(
		context.Context,
		string,
		streamclips.SourceProbe,
		streamkillfeed.Event,
	) ([][]byte, error) {
		return [][]byte{solidPNGForTest(t, 0xff, 0, 0)}, nil
	}

	task, err := tasks.NewGenerateStreamKillfeedTask(id, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleGenerateStreamKillfeed(context.Background(), task); err != nil {
		t.Fatalf("HandleGenerateStreamKillfeed error = %v", err)
	}

	got, exists, err := readKillfeedAnalysisState(store, generationKey)
	if err != nil || !exists {
		t.Fatalf("read generation state: exists=%v err=%v", exists, err)
	}
	if got.Status != streamclips.KillfeedAnalysisReady {
		t.Fatalf("status = %s, want ready", got.Status)
	}
	if len(got.Clips) != 2 || len(got.Clips[0].Events) != 1 || len(got.Clips[1].Events) != 0 {
		t.Fatalf("clips = %#v, want one event then zero events", got.Clips)
	}
	event := got.Clips[0].Events[0]
	if event.SourcePTS != 15000 || event.TimeBase != (streamclips.KillfeedTimeBase{Num: 1, Den: 30000}) {
		t.Fatalf("event clock = pts %d time_base %+v", event.SourcePTS, event.TimeBase)
	}
	if event.Kills == nil || len(event.Kills) != 0 {
		t.Fatalf("event kills = %#v, want a non-nil empty array without xAI", event.Kills)
	}
	if len(scanner.paths) != 2 || scanner.paths[0] != scanner.paths[1] {
		t.Fatalf("scanner source paths = %v, want one materialized source reused", scanner.paths)
	}
	if len(scanner.sources) != 2 || string(scanner.sources[0]) != "source-video" ||
		string(scanner.sources[1]) != "source-video" {
		t.Fatalf("materialized sources = %q", scanner.sources)
	}
	active, exists, err := readKillfeedAnalysisState(store, streamclips.KillfeedAnalysisKey(id))
	if err != nil || !exists {
		t.Fatalf("read active pointer: exists=%v err=%v", exists, err)
	}
	if active.Status != streamclips.KillfeedAnalysisQueued {
		t.Fatalf("active pointer status = %s, want HTTP-owned queued pointer", active.Status)
	}
}

func TestStreamKillfeedWorkerCannotOverwriteSupersedingGeneration(t *testing.T) {
	store := newFakeStorage()
	id := uuid.New()
	oldGenerationID := uuid.New()
	plan, oldState := queuedKillfeedAnalysisFixture(t, id, oldGenerationID)
	plan.Clips = plan.Clips[:1]
	oldState.Clips = oldState.Clips[:1]
	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(
		oldState.SourceSHA256, oldState.KillfeedCrop, plan.Clips,
	)
	if err != nil {
		t.Fatal(err)
	}
	oldState.Fingerprint = fingerprint
	_ = store.Put(streamclips.SourceKey(id), strings.NewReader("source-video"))
	putKillfeedStateForTest(t, store, streamclips.KillfeedAnalysisKey(id), oldState)
	oldGenerationKey, err := streamclips.KillfeedAnalysisGenerationKey(id, oldGenerationID)
	if err != nil {
		t.Fatal(err)
	}
	putKillfeedStateForTest(t, store, oldGenerationKey, oldState)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id),
		SourceSHA256: oldState.SourceSHA256,
		Probe:        streamclips.SourceProbe{DurationSeconds: 5, VideoTimeBase: "1/30000"},
		EditPlan:     planJSON,
	})

	newGenerationID := uuid.New()
	newState := oldState
	newState.GenerationID = newGenerationID
	newState.Status = streamclips.KillfeedAnalysisQueued
	newState.UpdatedAt = time.Now().UTC()
	newGenerationKey, err := streamclips.KillfeedAnalysisGenerationKey(id, newGenerationID)
	if err != nil {
		t.Fatal(err)
	}
	scanner := &fakeKillfeedScanner{eventsByClip: map[string][]streamkillfeed.Event{
		"clip-001": {exactKillfeedEvent()},
	}}
	scanner.scan = func(streamclips.ClipRange) {
		putKillfeedStateForTest(t, store, newGenerationKey, newState)
		putKillfeedStateForTest(t, store, streamclips.KillfeedAnalysisKey(id), newState)
	}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir:                        t.TempDir(),
		FFmpegPath:                     "ffmpeg",
		RequireAppliedKillfeedAnalysis: true,
	})
	w.killfeedScanner = scanner

	task, err := tasks.NewGenerateStreamKillfeedTask(id, oldGenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleGenerateStreamKillfeed(context.Background(), task); err != nil {
		t.Fatalf("superseded HandleGenerateStreamKillfeed error = %v, want nil", err)
	}

	active, exists, err := readKillfeedAnalysisState(store, streamclips.KillfeedAnalysisKey(id))
	if err != nil || !exists {
		t.Fatalf("read active pointer: exists=%v err=%v", exists, err)
	}
	if active.GenerationID != newGenerationID || active.Status != streamclips.KillfeedAnalysisQueued {
		t.Fatalf("active state = generation %s status %s, want newer queued state", active.GenerationID, active.Status)
	}
	newGeneration, exists, err := readKillfeedAnalysisState(store, newGenerationKey)
	if err != nil || !exists {
		t.Fatalf("read newer generation: exists=%v err=%v", exists, err)
	}
	if newGeneration.GenerationID != newGenerationID || len(newGeneration.Clips[0].Events) != 0 {
		t.Fatalf("newer generation was overwritten: %#v", newGeneration)
	}
	oldGeneration, exists, err := readKillfeedAnalysisState(store, oldGenerationKey)
	if err != nil || !exists {
		t.Fatalf("read older generation: exists=%v err=%v", exists, err)
	}
	if oldGeneration.Status != streamclips.KillfeedAnalysisAnalyzing || len(oldGeneration.Clips[0].Events) != 0 {
		t.Fatalf("old generation final state = %#v, want last owned analyzing state", oldGeneration)
	}
}

func TestStreamKillfeedWorkerClosesStillActiveStaleGeneration(t *testing.T) {
	store := newFakeStorage()
	id := uuid.New()
	generationID := uuid.New()
	plan, state := queuedKillfeedAnalysisFixture(t, id, generationID)
	_ = store.Put(streamclips.SourceKey(id), strings.NewReader("source-video"))
	putKillfeedStateForTest(t, store, streamclips.KillfeedAnalysisKey(id), state)
	generationKey, err := streamclips.KillfeedAnalysisGenerationKey(id, generationID)
	if err != nil {
		t.Fatal(err)
	}
	putKillfeedStateForTest(t, store, generationKey, state)
	// The same generation remains selected, but a plan edit has changed its
	// source-bound fingerprint before the worker begins.
	plan.Clips[0].EndSeconds -= 0.25
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id),
		SourceSHA256: state.SourceSHA256,
		Probe:        streamclips.SourceProbe{DurationSeconds: 5, VideoTimeBase: "1/30000"},
		EditPlan:     planJSON,
	})
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg",
	})
	w.killfeedScanner = &fakeKillfeedScanner{scan: func(streamclips.ClipRange) {
		t.Fatal("stale inputs reached scanner")
	}}
	task, err := tasks.NewGenerateStreamKillfeedTask(id, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleGenerateStreamKillfeed(context.Background(), task); err != nil {
		t.Fatalf("stale active generation error = %v, want terminal nil", err)
	}
	got, exists, err := readKillfeedAnalysisState(store, generationKey)
	if err != nil || !exists {
		t.Fatalf("read stale generation: exists=%v err=%v", exists, err)
	}
	if got.Status != streamclips.KillfeedAnalysisFailed || !strings.Contains(got.Error, "inputs changed") {
		t.Fatalf("stale generation = status %s error %q, want terminal failed", got.Status, got.Error)
	}
}

func TestStreamRenderWorkerRejectsKillfeedWithoutAppliedAnalysisBeforeFFmpeg(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, false)
	crop := streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	plan.KillfeedCrop = &crop
	plan.Clips[0].KillfeedSeconds = []float64{0.5}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id),
		SourceSHA256: "source-sha", Probe: streamclips.SourceProbe{DurationSeconds: 2},
		EditPlan: planJSON,
	})
	runner := &fakeRunner{}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir:                        t.TempDir(),
		FFmpegPath:                     "ffmpeg",
		RequireAppliedKillfeedAnalysis: true,
	})
	w.runner = runner
	planFingerprint, err := streamclips.EditPlanFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasks.NewBoundRenderStreamClipTask(id, streamclips.VariantStreamer4060, tasks.StreamRenderIntent{
		AttemptID:           uuid.New(),
		EditPlanFingerprint: planFingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	seedBoundStreamRenderAttemptForTest(t, w, id, streamclips.VariantStreamer4060, task)
	err = w.HandleRenderStreamClip(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "killfeed analysis must be applied") {
		t.Fatalf("HandleRenderStreamClip error = %v, want applied-analysis gate", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("FFmpeg calls = %d, want zero", len(runner.calls))
	}
}

func queuedKillfeedAnalysisFixture(
	t *testing.T,
	jobID, generationID uuid.UUID,
) (streamclips.EditPlan, streamclips.KillfeedAnalysisState) {
	t.Helper()
	plan := streamclips.DefaultEditPlan()
	plan.KillfeedCrop = &streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	plan.Clips = []streamclips.ClipRange{
		{ID: "clip-001", StartSeconds: 0, EndSeconds: 2},
		{ID: "clip-002", StartSeconds: 2, EndSeconds: 4},
	}
	const sourceSHA = "source-sha"
	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(sourceSHA, *plan.KillfeedCrop, plan.Clips)
	if err != nil {
		t.Fatal(err)
	}
	state := streamclips.KillfeedAnalysisState{
		JobID:        jobID,
		GenerationID: generationID,
		Status:       streamclips.KillfeedAnalysisQueued,
		SourceSHA256: sourceSHA,
		KillfeedCrop: *plan.KillfeedCrop,
		Fingerprint:  fingerprint,
		Clips: []streamclips.KillfeedAnalysisClip{
			{ClipID: "clip-001", StartSeconds: 0, EndSeconds: 2, Events: []streamclips.KillfeedAnalysisEvent{}},
			{ClipID: "clip-002", StartSeconds: 2, EndSeconds: 4, Events: []streamclips.KillfeedAnalysisEvent{}},
		},
		UpdatedAt: time.Now().UTC(),
	}
	return plan, state
}

func exactKillfeedEvent() streamkillfeed.Event {
	return streamkillfeed.Event{
		EventID:       "event-001",
		SourcePTS:     15000,
		TimeBase:      streamkillfeed.TimeBase{Num: 1, Den: 30000},
		CueSeconds:    0.5,
		OnsetStartPTS: 15000,
		OnsetEndPTS:   15000,
		SamplePTS:     24000,
		SampleSeconds: 0.8,
		Mode:          streamkillfeed.ModeAlignedFrame,
		Rows: []streamkillfeed.RowEvidence{{
			OnsetRowIndex:  0,
			SampleRowIndex: 0,
			Fingerprint:    "row-fingerprint",
			OnsetBounds:    streamclips.NoticeRow{X: 1, Y: 2, Width: 100, Height: 30},
			SampleBounds:   streamclips.NoticeRow{X: 1, Y: 2, Width: 100, Height: 30},
		}},
	}
}

func putKillfeedStateForTest(
	t *testing.T,
	store *fakeStorage,
	key string,
	state streamclips.KillfeedAnalysisState,
) {
	t.Helper()
	if err := putJSONToStorage(store, key, state); err != nil {
		t.Fatal(err)
	}
}

func applyKillfeedAnalysisForRenderTest(
	t *testing.T,
	store *fakeStorage,
	jobID uuid.UUID,
	sourceSHA string,
	plan *streamclips.EditPlan,
) {
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
	plan.KillfeedAnalysis = &streamclips.KillfeedAnalysisMetadata{
		GenerationID: generationID,
		Fingerprint:  fingerprint,
		AppliedAt:    time.Now().UTC(),
	}
	state := streamclips.KillfeedAnalysisState{
		JobID:        jobID,
		GenerationID: generationID,
		Status:       streamclips.KillfeedAnalysisApplied,
		SourceSHA256: sourceSHA,
		KillfeedCrop: *plan.KillfeedCrop,
		Fingerprint:  fingerprint,
		Clips:        make([]streamclips.KillfeedAnalysisClip, len(plan.Clips)),
		UpdatedAt:    time.Now().UTC(),
	}
	for i, clip := range plan.Clips {
		state.Clips[i] = streamclips.KillfeedAnalysisClip{
			ClipID:       clip.ID,
			StartSeconds: clip.StartSeconds,
			EndSeconds:   clip.EndSeconds,
			Events:       []streamclips.KillfeedAnalysisEvent{},
		}
	}
	putKillfeedStateForTest(t, store, streamclips.KillfeedAnalysisKey(jobID), state)
	key, err := streamclips.KillfeedAnalysisGenerationKey(jobID, generationID)
	if err != nil {
		t.Fatal(err)
	}
	putKillfeedStateForTest(t, store, key, state)
}
