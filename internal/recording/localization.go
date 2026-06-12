package recording

import (
	"path/filepath"

	"github.com/google/uuid"
)

// SegmentClipLocalization maps one stored segment clip to its local worker path.
type SegmentClipLocalization struct {
	ArtifactIndex int
	SegmentID     string
	Key           string
	LocalPath     string
}

// NewSegmentClipLocalizations returns the local copy plan for recorded segment
// clips referenced by result.
func NewSegmentClipLocalizations(jobID uuid.UUID, workDir string, result RecordingResult) ([]SegmentClipLocalization, error) {
	var localizations []SegmentClipLocalization
	for i, artifact := range result.Artifacts {
		if artifact.Role != "segment" || artifact.Type != "video" || artifact.SegmentID == "" {
			continue
		}
		key, err := SegmentClipArtifactKey(jobID, artifact.SegmentID)
		if err != nil {
			return nil, err
		}
		localizations = append(localizations, SegmentClipLocalization{
			ArtifactIndex: i,
			SegmentID:     artifact.SegmentID,
			Key:           key,
			LocalPath:     filepath.Join(workDir, "segments", artifact.SegmentID+".mp4"),
		})
	}
	return localizations, nil
}
