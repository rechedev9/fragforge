package recording

import (
	"fmt"
	"strings"
)

// SegmentClip is the selected video artifact for one planned recording segment.
type SegmentClip struct {
	SegmentID       string            `json:"segment_id"`
	Path            string            `json:"path"`
	DurationSeconds float64           `json:"duration_seconds,omitempty"`
	Artifact        RecordingArtifact `json:"artifact,omitempty"`
}

// ResolveSegmentClips selects one video artifact for each planned segment in
// plan order. A missing artifact is fatal; duplicate artifacts are resolved to
// the lexicographically first path and reported as warnings. On a missing
// segment it returns every clip it could resolve, plus accumulated warnings,
// alongside the error so callers may choose a best-effort policy.
func ResolveSegmentClips(result RecordingResult) ([]SegmentClip, []string, error) {
	bySegment := map[string][]RecordingArtifact{}
	for _, artifact := range result.Artifacts {
		if artifact.Role == "segment" && artifact.Type == "video" && artifact.SegmentID != "" {
			bySegment[artifact.SegmentID] = append(bySegment[artifact.SegmentID], artifact)
		}
	}

	var warnings []string
	var missing []string
	clips := make([]SegmentClip, 0, len(result.Plan.Segments))
	for _, segment := range result.Plan.Segments {
		artifacts := bySegment[segment.ID]
		if len(artifacts) == 0 {
			missing = append(missing, segment.ID)
			continue
		}
		// Only the lexicographically-first clip is used, so scan for the minimum
		// instead of sorting the whole slice.
		chosen := artifacts[0]
		for _, artifact := range artifacts[1:] {
			if artifact.Path < chosen.Path {
				chosen = artifact
			}
		}
		if len(artifacts) > 1 {
			warnings = append(warnings, fmt.Sprintf(
				"segment %s has %d composed input clips; using %s",
				segment.ID,
				len(artifacts),
				chosen.Path,
			))
		}
		clips = append(clips, SegmentClip{
			SegmentID:       segment.ID,
			Path:            chosen.Path,
			DurationSeconds: chosen.DurationSeconds,
			Artifact:        chosen,
		})
	}
	if len(missing) > 0 {
		return clips, warnings, fmt.Errorf(
			"recording result missing composed clips for segments: %s",
			strings.Join(missing, ", "),
		)
	}
	return clips, warnings, nil
}
