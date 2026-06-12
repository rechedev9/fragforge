package recording

import (
	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
)

// ResultArtifactKey returns the durable recorder result JSON key for a job.
func ResultArtifactKey(jobID uuid.UUID) string {
	return artifacts.RecordingResultKey(jobID)
}

// ScriptArtifactKey returns the durable HLAE recording script key for a job.
func ScriptArtifactKey(jobID uuid.UUID) string {
	return artifacts.RecordingScriptKey(jobID)
}

// SegmentClipArtifactKey returns the durable MP4 key for one recorded segment.
func SegmentClipArtifactKey(jobID uuid.UUID, segmentID string) (string, error) {
	return artifacts.SegmentClipKey(jobID, segmentID)
}
