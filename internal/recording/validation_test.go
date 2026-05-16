package recording

import (
	"strings"
	"testing"

	"github.com/reche/zackvideo/internal/killplan"
)

func TestValidateArtifactsAcceptsCompleteSet(t *testing.T) {
	plan := testPlan()
	artifacts := []RecordingArtifact{
		{SegmentID: "seg-001", TakeID: "take0000", Role: "raw", Type: "video", Path: "take0000/video.mp4", DurationSeconds: 5},
		{SegmentID: "seg-001", TakeID: "take0000", Role: "raw", Type: "audio", Path: "take0000/audio.wav"},
		{SegmentID: "seg-001", TakeID: "take0000", Role: "segment", Type: "video", Path: "segments/seg-001.mp4", DurationSeconds: 5},
		{SegmentID: "seg-002", TakeID: "take0001", Role: "raw", Type: "video", Path: "take0001/video.mp4", DurationSeconds: 8},
		{SegmentID: "seg-002", TakeID: "take0001", Role: "raw", Type: "audio", Path: "take0001/audio.wav"},
		{SegmentID: "seg-002", TakeID: "take0001", Role: "segment", Type: "video", Path: "segments/seg-002.mp4", DurationSeconds: 8},
	}
	if warnings := ValidateArtifacts(plan, artifacts); len(warnings) != 0 {
		t.Fatalf("ValidateArtifacts warnings = %v", warnings)
	}
}

func TestValidateArtifactsReportsMissingOutputs(t *testing.T) {
	plan := testPlan()
	warnings := ValidateArtifacts(plan, []RecordingArtifact{
		{SegmentID: "seg-001", TakeID: "take0000", Role: "raw", Type: "video", Path: "take0000/video.mp4", DurationSeconds: 1},
	})
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{
		"raw take count = 1, want 2",
		"segment seg-001 missing raw audio",
		"segment seg-001 missing muxed clip",
		"segment seg-002 missing raw video",
		"segment seg-001 raw video duration",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings missing %q:\n%s", want, joined)
		}
	}
}

func TestValidateArtifactsUsesEffectiveRecordStart(t *testing.T) {
	plan := testPlan()
	plan.Segments = []RecordingSegment{
		{
			ID:        "seg-001",
			TickStart: 14029,
			TickEnd:   14770,
			Kills: []killplan.Kill{
				{Tick: 14221},
			},
		},
	}
	artifacts := []RecordingArtifact{
		{SegmentID: "seg-001", TakeID: "take0000", Role: "raw", Type: "video", Path: "take0000/video.mp4", DurationSeconds: 9.58},
		{SegmentID: "seg-001", TakeID: "take0000", Role: "raw", Type: "audio", Path: "take0000/audio.wav"},
		{SegmentID: "seg-001", TakeID: "take0000", Role: "segment", Type: "video", Path: "segments/seg-001.mp4", DurationSeconds: 9.57},
	}
	if warnings := ValidateArtifacts(plan, artifacts); len(warnings) != 0 {
		t.Fatalf("ValidateArtifacts warnings = %v", warnings)
	}
}
