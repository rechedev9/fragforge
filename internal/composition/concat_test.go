package composition

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/recording"
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

	clips, warnings, err := SegmentClipsFromRecording(result)
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
}

func TestSegmentClipsFromRecordingMissingSegmentIsFatal(t *testing.T) {
	result := recording.RecordingResult{
		Plan: recording.RecordingPlan{
			Segments: []recording.RecordingSegment{{ID: "seg-001"}},
		},
	}
	clips, warnings, err := SegmentClipsFromRecording(result)
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

func TestSegmentClipsFromRecordingDuplicateIsNonFatal(t *testing.T) {
	result := recording.RecordingResult{
		Plan: recording.RecordingPlan{
			Segments: []recording.RecordingSegment{{ID: "seg-001"}},
		},
		Artifacts: []recording.RecordingArtifact{
			{SegmentID: "seg-001", Role: "segment", Type: "video", Path: "segments/seg-001-b.mp4", DurationSeconds: 5},
			{SegmentID: "seg-001", Role: "segment", Type: "video", Path: "segments/seg-001-a.mp4", DurationSeconds: 6},
		},
	}
	clips, warnings, err := SegmentClipsFromRecording(result)
	if err != nil {
		t.Fatalf("err = %v, want nil (duplicate clips are resolved deterministically, not fatal)", err)
	}
	if len(clips) != 1 {
		t.Fatalf("clips len = %d, want 1", len(clips))
	}
	if clips[0].Path != "segments/seg-001-a.mp4" {
		t.Fatalf("chosen clip = %s, want lexicographically first segments/seg-001-a.mp4", clips[0].Path)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "seg-001") {
		t.Fatalf("warnings = %v, want one duplicate-clip warning", warnings)
	}
}

func TestConcatListEscapesPaths(t *testing.T) {
	got := ConcatList([]SegmentClip{{Path: `C:\tmp\clip's\seg-001.mp4`}})
	want := "file 'C:/tmp/clip'\\''s/seg-001.mp4'\n"
	if got != want {
		t.Fatalf("ConcatList = %q, want %q", got, want)
	}
}

func TestValidateFinalArtifactAcceptsExpectedShape(t *testing.T) {
	warnings := ValidateFinalArtifact(recording.RecordingArtifact{
		Path:            "final.mp4",
		SizeBytes:       10,
		Codec:           "h264",
		Width:           1920,
		Height:          1080,
		FrameRate:       "60/1",
		DurationSeconds: 10,
	}, 1920, 1080, 60, 10.1)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestValidateFinalArtifactReportsBadShape(t *testing.T) {
	warnings := ValidateFinalArtifact(recording.RecordingArtifact{
		Path:            "final.mp4",
		Codec:           "mpeg4",
		Width:           1280,
		Height:          720,
		FrameRate:       "30000/1001",
		DurationSeconds: 4,
	}, 1920, 1080, 60, 10)
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"missing or empty", "codec", "resolution", "frame_rate", "duration"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings missing %q:\n%s", want, joined)
		}
	}
}

func TestArtifactKeys(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	if got, want := ResultArtifactKey(id), "jobs/11111111-1111-1111-1111-111111111111/composition/composition-result.json"; got != want {
		t.Fatalf("result artifact key = %q, want %q", got, want)
	}
	if got, want := FinalArtifactKey(id), "jobs/11111111-1111-1111-1111-111111111111/composition/final.mp4"; got != want {
		t.Fatalf("final artifact key = %q, want %q", got, want)
	}
}
