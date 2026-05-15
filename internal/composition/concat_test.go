package composition

import (
	"strings"
	"testing"

	"github.com/reche/zackvideo/internal/recording"
)

func TestSegmentClipsFromRecordingUsesPlanOrder(t *testing.T) {
	result := recording.RecordingResult{
		Plan: recording.RecordingPlan{
			Segments: []recording.RecordingSegment{
				{ID: "seg-001"},
				{ID: "seg-002"},
			},
		},
		Artifacts: []recording.RecordingArtifact{
			{SegmentID: "seg-002", Role: "segment", Type: "video", Path: "segments/seg-002.mp4", DurationSeconds: 8},
			{SegmentID: "seg-001", Role: "raw", Type: "video", Path: "take0000/video.mp4"},
			{SegmentID: "seg-001", Role: "segment", Type: "video", Path: "segments/seg-001.mp4", DurationSeconds: 12},
		},
	}

	clips, warnings := SegmentClipsFromRecording(result)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(clips) != 2 {
		t.Fatalf("clips len = %d, want 2", len(clips))
	}
	if clips[0].SegmentID != "seg-001" || clips[1].SegmentID != "seg-002" {
		t.Fatalf("clips order = %s, %s", clips[0].SegmentID, clips[1].SegmentID)
	}
}

func TestSegmentClipsFromRecordingWarnsMissingSegment(t *testing.T) {
	result := recording.RecordingResult{
		Plan: recording.RecordingPlan{
			Segments: []recording.RecordingSegment{{ID: "seg-001"}},
		},
	}
	_, warnings := SegmentClipsFromRecording(result)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "seg-001 missing") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestConcatListEscapesPaths(t *testing.T) {
	got := ConcatList([]SegmentClip{{Path: `C:\tmp\clip's\seg-001.mp4`}})
	want := "file 'C:/tmp/clip'\\''s/seg-001.mp4'\n"
	if got != want {
		t.Fatalf("ConcatList = %q, want %q", got, want)
	}
}
