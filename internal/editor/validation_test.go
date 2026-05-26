package editor

import (
	"strings"
	"testing"

	"github.com/reche/zackvideo/internal/recording"
)

func TestValidateShortArtifactWarnsWhenTooLongForYouTubeShorts(t *testing.T) {
	warnings := ValidateShortArtifact(recording.RecordingArtifact{
		SegmentID:       "seg-long",
		Path:            "short.mp4",
		SizeBytes:       1,
		DurationSeconds: 181,
		Codec:           "h264",
		Width:           1080,
		Height:          1920,
		FrameRate:       "60/1",
	})
	if len(warnings) != 1 || !strings.Contains(warnings[0], "want <= 180s for YouTube Shorts") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestValidateShortArtifactAcceptsUploadReadyShort(t *testing.T) {
	warnings := ValidateShortArtifact(recording.RecordingArtifact{
		SegmentID:       "seg-ok",
		Path:            "short.mp4",
		SizeBytes:       1,
		DurationSeconds: 60,
		Codec:           "h264",
		Width:           1080,
		Height:          1920,
		FrameRate:       "60/1",
	})
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestValidateSourceArtifactWarnsWhenSourceFormatIsUnexpected(t *testing.T) {
	warnings := ValidateSourceArtifact(recording.RecordingArtifact{
		SegmentID: "seg-source",
		Path:      "source.mp4",
		Width:     1280,
		Height:    720,
		FrameRate: "30/1",
	})
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"want 1920x1080", "want 60fps"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings missing %q: %#v", want, warnings)
		}
	}
}

func TestQualityWarningsFromFFmpegLog(t *testing.T) {
	log := `
[blackdetect @ 000] black_start:0 black_end:0.5 black_duration:0.5
[freezedetect @ 000] freeze_start:1 freeze_duration:1.2 freeze_end:2.2
[Parsed_cropdetect_2 @ 000] crop=960:1728:60:96
`
	warnings := QualityWarningsFromFFmpegLog("seg-001", log)
	if len(warnings) != 3 {
		t.Fatalf("warnings len = %d, want 3: %#v", len(warnings), warnings)
	}
}

func TestQualityWarningsIgnoresFullFrameCrop(t *testing.T) {
	warnings := QualityWarningsFromFFmpegLog("seg-001", "[Parsed_cropdetect_2 @ 000] crop=1072:1904:4:8")
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", warnings)
	}
}
