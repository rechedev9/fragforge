package composition

import "github.com/reche/zackvideo/internal/recording"

type SegmentClip struct {
	SegmentID       string                      `json:"segment_id"`
	Path            string                      `json:"path"`
	DurationSeconds float64                     `json:"duration_seconds,omitempty"`
	Artifact        recording.RecordingArtifact `json:"artifact,omitempty"`
}

type Result struct {
	RecordingResult string                      `json:"recording_result"`
	Output          string                      `json:"output"`
	OutputArtifact  recording.RecordingArtifact `json:"output_artifact,omitempty"`
	Clips           []SegmentClip               `json:"clips"`
	Warnings        []string                    `json:"warnings,omitempty"`
	Error           string                      `json:"error,omitempty"`
}
