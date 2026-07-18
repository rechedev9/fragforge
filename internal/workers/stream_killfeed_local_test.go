package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/streamkillfeed"
	"github.com/rechedev9/fragforge/internal/tasks"
)

func TestPrepareLocalKillfeedAnalysisKeepsAutomaticRowsAfterReviewedKillImport(t *testing.T) {
	store := newFakeStorage()
	jobID := uuid.New()
	const sourceSHA = "local-cli-source"
	if err := store.Put(streamclips.SourceKey(jobID), strings.NewReader("source-video")); err != nil {
		t.Fatal(err)
	}

	plan := streamclips.DefaultEditPlan()
	plan.KillfeedCrop = &streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	eventA := exactKillfeedEvent()
	eventA.EventID = "adjacent-a"
	eventB := exactKillfeedEvent()
	eventB.EventID = "adjacent-b"
	eventB.SourcePTS = eventA.SourcePTS + 1
	eventB.OnsetStartPTS = eventB.SourcePTS
	eventB.OnsetEndPTS = eventB.SourcePTS
	eventB.CueSeconds = eventB.TimeBase.Seconds(eventB.SourcePTS)
	eventB.SamplePTS = eventA.SamplePTS + 1
	eventB.SampleSeconds = eventB.TimeBase.Seconds(eventB.SamplePTS)
	eventB.Mode = streamkillfeed.ModeBurst
	eventB.Rows[0].Fingerprint = "adjacent-b-row"
	reviewedKill := streamclips.KillfeedKill{
		AttackerSide: "CT", AttackerName: "hero", VictimSide: "T",
		VictimName: "villain", Weapon: "ak47",
	}
	plan.Clips = []streamclips.ClipRange{{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 2,
		KillfeedSeconds: []float64{eventA.CueSeconds, eventB.CueSeconds},
		KillfeedKills: [][]streamclips.KillfeedKill{
			{reviewedKill}, {reviewedKill},
		},
		KillfeedCueProvenance: []streamclips.KillfeedCueProvenance{
			{CueSeconds: eventA.CueSeconds, Origin: streamclips.KillfeedCueAutomatic},
			{CueSeconds: eventB.CueSeconds, Origin: streamclips.KillfeedCueAutomatic},
		},
	}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	job := streamclips.Job{
		ID: jobID, Status: streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(jobID), SourceSHA256: sourceSHA,
		Probe:    streamclips.SourceProbe{DurationSeconds: 2, VideoTimeBase: "1/30000"},
		EditPlan: planJSON,
	}
	repo := newFakeStreamRepo(job)
	red := solidPNGForTest(t, 0xff, 0, 0)
	blue := solidPNGForTest(t, 0, 0, 0xff)
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg",
	})
	w.killfeedScanner = &fakeKillfeedScanner{eventsByClip: map[string][]streamkillfeed.Event{
		"clip-001": {eventA, eventB},
	}}
	w.extractKillfeedRows = func(
		_ context.Context,
		_ string,
		_ streamclips.SourceProbe,
		event streamkillfeed.Event,
	) ([][]byte, error) {
		if event.EventID == eventA.EventID {
			return [][]byte{red}, nil
		}
		return [][]byte{blue}, nil
	}

	analyzed, err := w.PrepareLocalKillfeedAnalysis(context.Background(), job, plan)
	if err != nil {
		t.Fatalf("PrepareLocalKillfeedAnalysis error = %v", err)
	}
	if analyzed.KillfeedAnalysis == nil {
		t.Fatal("analyzed plan has no durable killfeed metadata")
	}
	for _, event := range []streamkillfeed.Event{eventA, eventB} {
		provenance, ok := analyzed.Clips[0].KillfeedProvenanceAt(event.CueSeconds)
		if !ok || provenance.Origin != streamclips.KillfeedCueAutomatic || provenance.EventID != event.EventID {
			t.Fatalf("cue %.9f provenance = %#v / %v, want bound automatic event %s", event.CueSeconds, provenance, ok, event.EventID)
		}
	}
	if got := analyzed.Clips[0].KillfeedSeconds; len(got) != 2 || got[0] != eventA.CueSeconds || got[1] != eventB.CueSeconds {
		t.Fatalf("analyzed cues = %#v, want both adjacent source PTS", got)
	}
	active, exists, err := readKillfeedAnalysisState(store, streamclips.KillfeedAnalysisKey(jobID))
	if err != nil || !exists {
		t.Fatalf("read local active analysis: exists=%v err=%v", exists, err)
	}
	if active.Status != streamclips.KillfeedAnalysisApplied || len(active.Clips[0].Events) != 2 {
		t.Fatalf("local analysis = status %s events %d, want applied/2", active.Status, len(active.Clips[0].Events))
	}
	for _, artifact := range []struct {
		eventID string
		want    []byte
	}{{eventA.EventID, red}, {eventB.EventID, blue}} {
		key, err := streamclips.KillfeedEventRowKey(jobID, active.GenerationID, "clip-001", artifact.eventID, 0)
		if err != nil {
			t.Fatal(err)
		}
		rc, err := store.Open(key)
		if err != nil {
			t.Fatalf("open %s row: %v", artifact.eventID, err)
		}
		got, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read %s row: %v / %v", artifact.eventID, readErr, closeErr)
		}
		if !bytes.Equal(got, artifact.want) {
			t.Fatalf("%s row contains another event", artifact.eventID)
		}
	}

	job.EditPlan, err = json.Marshal(analyzed)
	if err != nil {
		t.Fatal(err)
	}
	repo.jobs[jobID] = job
	materialized := make(map[string][]byte)
	w.runner = &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
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
	task, err := tasks.NewRenderStreamClipTask(jobID, analyzed.Variant)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("exact local render error = %v", err)
	}
	if !bytes.Equal(materialized[eventA.EventID], red) || !bytes.Equal(materialized[eventB.EventID], blue) {
		t.Fatalf("render did not consume each adjacent event's exact row: %#v", materialized)
	}
	filter := argValue(w.runner.(*fakeRunner).calls[0].args, "-filter_complex")
	if strings.Contains(filter, "killfeedin") {
		t.Fatalf("local automatic render used approximate full-column freeze: %s", filter)
	}
}

func TestBindLocalKillfeedCueProvenanceKeepsLegacyReviewedCueManual(t *testing.T) {
	const reviewedCue = 1.25
	const unresolvedCue = 1.75
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{
		ID:              "clip-001",
		StartSeconds:    0,
		EndSeconds:      2,
		KillfeedSeconds: []float64{reviewedCue, unresolvedCue},
		KillfeedKills: [][]streamclips.KillfeedKill{{{
			AttackerSide: "CT", AttackerName: "hero", VictimSide: "T",
			VictimName: "villain", Weapon: "ak47",
		}}, nil},
	}}
	state := streamclips.KillfeedAnalysisState{Clips: []streamclips.KillfeedAnalysisClip{{
		ClipID: "clip-001",
		Events: []streamclips.KillfeedAnalysisEvent{
			{EventID: "reviewed-event", CueSeconds: reviewedCue},
			{EventID: "automatic-event", CueSeconds: unresolvedCue},
		},
	}}}

	got, err := bindLocalKillfeedCueProvenance(plan, state)
	if err != nil {
		t.Fatal(err)
	}
	reviewed, ok := got.Clips[0].KillfeedProvenanceAt(reviewedCue)
	if !ok || reviewed.Origin != streamclips.KillfeedCueManual || reviewed.EventID != "" {
		t.Fatalf("reviewed cue provenance = %#v / %v, want manual without event", reviewed, ok)
	}
	unresolved, ok := got.Clips[0].KillfeedProvenanceAt(unresolvedCue)
	if !ok || unresolved.Origin != streamclips.KillfeedCueAutomatic || unresolved.EventID != "automatic-event" {
		t.Fatalf("unresolved cue provenance = %#v / %v, want automatic event", unresolved, ok)
	}
}
