package recording

import (
	"fmt"
	"strings"
)

// ValidateRunResult returns an error when the recorder wrote a structured
// failure result after the process completed.
func ValidateRunResult(result RecordingResult) error {
	if result.Error != "" {
		return fmt.Errorf("recording result error: %s", result.Error)
	}
	return nil
}

// ValidateUploadResult returns an error when a successful recorder result does
// not contain every planned segment clip.
func ValidateUploadResult(result RecordingResult) error {
	if result.Error != "" {
		return nil
	}
	clips := map[string]bool{}
	for _, artifact := range result.Artifacts {
		if isUsableSegmentClip(artifact) {
			clips[artifact.SegmentID] = true
		}
	}
	if len(clips) == 0 {
		return fmt.Errorf("recording result has no segment clips")
	}

	missing := make([]string, 0, len(result.Plan.Segments))
	for _, segment := range result.Plan.Segments {
		if !clips[segment.ID] {
			missing = append(missing, segment.ID)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("recording result missing segment clips: %s", strings.Join(missing, ", "))
	}
	return nil
}

func isUsableSegmentClip(artifact RecordingArtifact) bool {
	return artifact.Role == "segment" && artifact.Type == "video" && artifact.SegmentID != "" &&
		artifact.Path != "" && artifact.SizeBytes > 0 && artifact.ProbeError == ""
}
