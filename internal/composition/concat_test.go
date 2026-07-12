package composition

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/recording"
)

func TestConcatListEscapesPaths(t *testing.T) {
	got := ConcatList([]recording.SegmentClip{{Path: `C:\tmp\clip's\seg-001.mp4`}})
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
