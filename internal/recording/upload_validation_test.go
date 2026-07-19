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
			Path:      "seg-001.mp4",
			SizeBytes: 1,
		}},
	})
	if err != nil {
		t.Fatalf("ValidateUploadResult error = %v", err)
	}
}

func TestValidateUploadResultAcceptsAllPlannedSegmentClips(t *testing.T) {
	err := ValidateUploadResult(RecordingResult{
		Plan: RecordingPlan{Segments: []RecordingSegment{
			{ID: "seg-001"},
			{ID: "seg-002"},
		}},
		Artifacts: []RecordingArtifact{
			{SegmentID: "seg-001", Type: "video", Role: "segment", Path: "seg-001.mp4", SizeBytes: 1},
			{SegmentID: "seg-002", Type: "video", Role: "segment", Path: "seg-002.mp4", SizeBytes: 1},
		},
	})
	if err != nil {
		t.Fatalf("ValidateUploadResult error = %v", err)
	}
}

func TestValidateUploadResultRejectsMissingPlannedSegmentClips(t *testing.T) {
	err := ValidateUploadResult(RecordingResult{
		Plan: RecordingPlan{Segments: []RecordingSegment{
			{ID: "seg-001"},
			{ID: "seg-002"},
			{ID: "seg-003"},
		}},
		Artifacts: []RecordingArtifact{
			{SegmentID: "seg-001", Type: "video", Role: "segment", Path: "seg-001.mp4", SizeBytes: 1},
		},
	})
	if err == nil {
		t.Fatal("ValidateUploadResult error = nil, want missing planned segments")
	}
	if want := "recording result missing segment clips: seg-002, seg-003"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestValidateUploadResultRejectsFailedSegmentArtifacts(t *testing.T) {
	for _, tt := range []struct {
		name     string
		artifact RecordingArtifact
	}{
		{
			name:     "mux error",
			artifact: RecordingArtifact{SegmentID: "seg-001", Type: "video", Role: "segment", Path: "seg-001.mp4", ProbeError: "ffmpeg mux failed"},
		},
		{
			name:     "empty clip",
			artifact: RecordingArtifact{SegmentID: "seg-001", Type: "video", Role: "segment", Path: "seg-001.mp4"},
		},
		{
			name:     "missing path",
			artifact: RecordingArtifact{SegmentID: "seg-001", Type: "video", Role: "segment", SizeBytes: 1},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUploadResult(RecordingResult{
				Plan:      RecordingPlan{Segments: []RecordingSegment{{ID: "seg-001"}}},
				Artifacts: []RecordingArtifact{tt.artifact},
			})
			if err == nil {
				t.Fatal("ValidateUploadResult error = nil, want invalid segment clip")
			}
		})
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
