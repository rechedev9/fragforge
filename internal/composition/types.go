// Package composition contains the first post-recording composition step.
package composition

type SegmentClip struct {
	SegmentID       string  `json:"segment_id"`
	Path            string  `json:"path"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

type Result struct {
	RecordingResult string        `json:"recording_result"`
	Output          string        `json:"output"`
	Clips           []SegmentClip `json:"clips"`
	Warnings        []string      `json:"warnings,omitempty"`
	Error           string        `json:"error,omitempty"`
}
