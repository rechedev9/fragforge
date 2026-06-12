package recording

import (
	"strings"
	"testing"
)

func TestValidateUploadResultAcceptsSegmentClip(t *testing.T) {
	err := ValidateUploadResult(RecordingResult{
		Artifacts: []RecordingArtifact{{
			SegmentID: "seg-001",
			Type:      "video",
			Role:      "segment",
		}},
	})
	if err != nil {
		t.Fatalf("ValidateUploadResult error = %v", err)
	}
}

func TestValidateUploadResultRejectsSuccessfulResultWithoutSegmentClips(t *testing.T) {
	err := ValidateUploadResult(RecordingResult{
		Artifacts: []RecordingArtifact{{
			SegmentID: "seg-001",
			Type:      "audio",
			Role:      "raw",
		}},
	})
	if err == nil {
		t.Fatal("ValidateUploadResult error = nil, want missing segment clips")
	}
	if !strings.Contains(err.Error(), "recording result has no segment clips") {
		t.Fatalf("error = %q, want no segment clips", err.Error())
	}
}

func TestValidateUploadResultAcceptsFailedResult(t *testing.T) {
	err := ValidateUploadResult(RecordingResult{Error: "recorder failed"})
	if err != nil {
		t.Fatalf("ValidateUploadResult error = %v", err)
	}
}
