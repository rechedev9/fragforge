package rhythm

import (
	"math"
	"testing"

	"github.com/reche/zackvideo/internal/killplan"
)

func TestAnalyzeSamplesDetectsBeatGrid(t *testing.T) {
	const sampleRate = 22050
	const duration = 12.0
	const bpm = 103.0
	period := 60 / bpm
	samples := makePulseTrain(sampleRate, duration, 0.2, period)

	got := AnalyzeSamples(samples, sampleRate, samplesConfig{
		MinBPM:            90,
		MaxBPM:            115,
		KillOffsetSeconds: 0.10,
		MaxBeats:          20,
	})

	if math.Abs(got.EstimatedBPM-bpm) > 2 {
		t.Fatalf("estimated bpm = %.2f, want near %.2f", got.EstimatedBPM, bpm)
	}
	if len(got.BeatTimesSeconds) < 10 {
		t.Fatalf("beat count = %d, want at least 10", len(got.BeatTimesSeconds))
	}
	if math.Abs(got.BeatTimesSeconds[0]-0.2) > 0.08 {
		t.Fatalf("first beat = %.3f, want near 0.2", got.BeatTimesSeconds[0])
	}
}

func TestBuildSegmentSyncAlignsFirstKillAfterBeat(t *testing.T) {
	plan := killplan.NewPlan()
	plan.Demo.Tickrate = 64
	plan.Segments = []killplan.Segment{
		{
			ID:        "seg-001",
			Round:     4,
			TickStart: 640,
			TickEnd:   1280,
			Kills:     []killplan.Kill{{Tick: 832, Weapon: "awp"}},
		},
	}
	beats := []float64{0.5, 1.0, 1.5, 2.0, 2.5}

	got := BuildSegmentSync(plan, beats, 0.10)

	if len(got) != 1 {
		t.Fatalf("sync entries = %d, want 1", len(got))
	}
	if got[0].SegmentID != "seg-001" {
		t.Fatalf("segment id = %q, want seg-001", got[0].SegmentID)
	}
	if got[0].DeltaToBeatMilliseconds != 100 {
		t.Fatalf("delta to beat = %dms, want 100ms", got[0].DeltaToBeatMilliseconds)
	}
	if got[0].TimelineStartSeconds < 0 {
		t.Fatalf("timeline start = %.3f, want non-negative", got[0].TimelineStartSeconds)
	}
}

func makePulseTrain(sampleRate int, duration, phase, period float64) []float64 {
	total := int(duration * float64(sampleRate))
	samples := make([]float64, total)
	width := int(0.035 * float64(sampleRate))
	for t := phase; t < duration; t += period {
		center := int(t * float64(sampleRate))
		for i := -width; i <= width; i++ {
			idx := center + i
			if idx < 0 || idx >= len(samples) {
				continue
			}
			x := float64(i) / float64(width)
			samples[idx] += math.Exp(-x * x * 8)
		}
	}
	return samples
}
