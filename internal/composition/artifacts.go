package composition

import (
	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
)

// ResultArtifactKey returns the durable composition result JSON key for a job.
func ResultArtifactKey(jobID uuid.UUID) string {
	return artifacts.CompositionResultKey(jobID)
}

// FinalArtifactKey returns the durable final MP4 key for a job.
func FinalArtifactKey(jobID uuid.UUID) string {
	return artifacts.FinalMP4Key(jobID)
}
