package recording

import (
	"strings"
	"testing"
)

func TestValidateRunResultAcceptsSuccessfulResult(t *testing.T) {
	err := ValidateRunResult(RecordingResult{})
	if err != nil {
		t.Fatalf("ValidateRunResult error = %v", err)
	}
}

func TestValidateRunResultRejectsFailedResult(t *testing.T) {
	err := ValidateRunResult(RecordingResult{Error: "recorder failed"})
	if err == nil {
		t.Fatal("ValidateRunResult error = nil, want recording result error")
	}
	if !strings.Contains(err.Error(), "recording result error: recorder failed") {
		t.Fatalf("error = %q, want recording result error", err.Error())
	}
}

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
