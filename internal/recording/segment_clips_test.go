package recording

import (
	"strings"
	"testing"
)

func TestResolveSegmentClipsUsesPlanOrder(t *testing.T) {
	result := RecordingResult{
		Plan: RecordingPlan{
			Segments: []RecordingSegment{
				{ID: "seg-001"},
				{ID: "seg-002"},
			},
		},
		Artifacts: []RecordingArtifact{
			{SegmentID: "seg-002", Role: "segment", Type: "video", Path: "segments/seg-002.mp4", DurationSeconds: 8},
			{SegmentID: "seg-001", Role: "raw", Type: "video", Path: "take0000/video.mp4"},
			{SegmentID: "seg-001", Role: "segment", Type: "video", Path: "segments/seg-001.mp4", DurationSeconds: 12},
		},
	}

	clips, warnings, err := ResolveSegmentClips(result)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(clips) != 2 {
		t.Fatalf("clips len = %d, want 2", len(clips))
	}
	if clips[0].SegmentID != "seg-001" || clips[1].SegmentID != "seg-002" {
		t.Fatalf("clips order = %s, %s", clips[0].SegmentID, clips[1].SegmentID)
	}
	if clips[0].DurationSeconds != 12 || clips[0].Artifact.Path != "segments/seg-001.mp4" {
		t.Fatalf("first clip projection = %#v, want selected artifact and duration", clips[0])
	}
}

func TestResolveSegmentClipsMissingSegmentIsFatal(t *testing.T) {
	result := RecordingResult{
		Plan: RecordingPlan{
			Segments: []RecordingSegment{{ID: "seg-001"}},
		},
	}
	clips, warnings, err := ResolveSegmentClips(result)
	if err == nil || !strings.Contains(err.Error(), "seg-001") {
		t.Fatalf("err = %v, want fatal error mentioning seg-001", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none (a missing clip is fatal, not a warning)", warnings)
	}
	if len(clips) != 0 {
		t.Fatalf("clips len = %d, want 0", len(clips))
	}
}

func TestResolveSegmentClipsDuplicateIsNonFatal(t *testing.T) {
	result := RecordingResult{
		Plan: RecordingPlan{
			Segments: []RecordingSegment{{ID: "seg-001"}},
		},
		Artifacts: []RecordingArtifact{
			{SegmentID: "seg-001", Role: "segment", Type: "video", Path: "segments/seg-001-b.mp4", DurationSeconds: 5},
			{SegmentID: "seg-001", Role: "segment", Type: "video", Path: "segments/seg-001-a.mp4", DurationSeconds: 6},
		},
	}
	clips, warnings, err := ResolveSegmentClips(result)
	if err != nil {
		t.Fatalf("err = %v, want nil (duplicate clips are resolved deterministically, not fatal)", err)
	}
	if len(clips) != 1 {
		t.Fatalf("clips len = %d, want 1", len(clips))
	}
	if clips[0].Path != "segments/seg-001-a.mp4" {
		t.Fatalf("chosen clip = %s, want lexicographically first segments/seg-001-a.mp4", clips[0].Path)
	}
	if clips[0].DurationSeconds != 6 || clips[0].Artifact.Path != clips[0].Path {
		t.Fatalf("chosen clip projection = %#v, want artifact from lexicographically first path", clips[0])
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "seg-001") {
		t.Fatalf("warnings = %v, want one duplicate-clip warning", warnings)
	}
}
