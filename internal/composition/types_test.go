package composition

import (
	"encoding/json"
	"testing"

	"github.com/rechedev9/fragforge/internal/recording"
)

func TestResultPreservesSegmentClipJSONShape(t *testing.T) {
	result := Result{
		Clips: []recording.SegmentClip{{
			SegmentID:       "seg-001",
			Path:            "segments/seg-001.mp4",
			DurationSeconds: 12,
			Artifact: recording.RecordingArtifact{
				SegmentID:       "seg-001",
				Role:            "segment",
				Type:            "video",
				Path:            "segments/seg-001.mp4",
				DurationSeconds: 12,
			},
		}},
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var wire struct {
		Clips []struct {
			SegmentID       string                      `json:"segment_id"`
			Path            string                      `json:"path"`
			DurationSeconds float64                     `json:"duration_seconds"`
			Artifact        recording.RecordingArtifact `json:"artifact"`
		} `json:"clips"`
	}
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(wire.Clips) != 1 {
		t.Fatalf("clips len = %d, want 1", len(wire.Clips))
	}
	clip := wire.Clips[0]
	if clip.SegmentID != "seg-001" || clip.Path != "segments/seg-001.mp4" || clip.DurationSeconds != 12 {
		t.Fatalf("clip JSON = %#v, want stable segment/path/duration fields", clip)
	}
	if clip.Artifact.SegmentID != clip.SegmentID || clip.Artifact.Path != clip.Path {
		t.Fatalf("clip artifact JSON = %#v, want selected recording artifact", clip.Artifact)
	}
}
