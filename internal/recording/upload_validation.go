package recording

import "fmt"

// ValidateUploadResult returns an error when a successful recorder result has
// no segment clips to materialize.
func ValidateUploadResult(result RecordingResult) error {
	if result.Error != "" {
		return nil
	}
	for _, artifact := range result.Artifacts {
		if artifact.Role == "segment" && artifact.Type == "video" && artifact.SegmentID != "" {
			return nil
		}
	}
	return fmt.Errorf("recording result has no segment clips")
}
