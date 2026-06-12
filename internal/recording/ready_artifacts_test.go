package recording

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewReadyArtifactsDerivesRequiredKeys(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	got, err := NewReadyArtifacts(id, RecordingResult{
		Artifacts: []RecordingArtifact{{
			SegmentID: "seg-001",
			Type:      "video",
			Role:      "segment",
		}, {
			SegmentID: "seg-001",
			Type:      "audio",
			Role:      "raw",
		}, {
			SegmentID: "seg-002",
			Type:      "video",
			Role:      "segment",
		}},
	})
	if err != nil {
		t.Fatalf("NewReadyArtifacts error = %v", err)
	}

	prefix := "jobs/11111111-1111-1111-1111-111111111111/recording"
	wantRequired := []string{
		prefix + "/recording.js",
		prefix + "/segments/seg-001.mp4",
		prefix + "/segments/seg-002.mp4",
	}
	if got.ResultKey != prefix+"/recording-result.json" {
		t.Fatalf("result key = %q", got.ResultKey)
	}
	if got.SegmentCount != 2 {
		t.Fatalf("segment count = %d, want 2", got.SegmentCount)
	}
	if len(got.RequiredKeys) != len(wantRequired) {
		t.Fatalf("required keys len = %d, want %d: %#v", len(got.RequiredKeys), len(wantRequired), got.RequiredKeys)
	}
	for i := range wantRequired {
		if got.RequiredKeys[i] != wantRequired[i] {
			t.Fatalf("required key[%d] = %q, want %q", i, got.RequiredKeys[i], wantRequired[i])
		}
	}
}

func TestNewReadyArtifactsSkipsRequirementsForFailedResult(t *testing.T) {
	got, err := NewReadyArtifacts(uuid.New(), RecordingResult{
		Error: "recorder failed",
		Artifacts: []RecordingArtifact{{
			SegmentID: "seg-001",
			Type:      "video",
			Role:      "segment",
		}},
	})
	if err != nil {
		t.Fatalf("NewReadyArtifacts error = %v", err)
	}
	if got.ResultKey == "" {
		t.Fatal("result key is empty")
	}
	if len(got.RequiredKeys) != 0 || got.SegmentCount != 0 {
		t.Fatalf("ready artifacts = %#v, want only result key for failed result", got)
	}
}
