package composition

import "github.com/rechedev9/fragforge/internal/recording"

type Result struct {
	RecordingResult string                      `json:"recording_result"`
	Output          string                      `json:"output"`
	OutputArtifact  recording.RecordingArtifact `json:"output_artifact,omitempty"`
	Clips           []recording.SegmentClip     `json:"clips"`
	Warnings        []string                    `json:"warnings,omitempty"`
	Error           string                      `json:"error,omitempty"`
}
