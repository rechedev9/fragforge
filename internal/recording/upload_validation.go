package recording

import "fmt"

// ValidateRunResult returns an error when the recorder wrote a structured
// failure result after the process completed.
func ValidateRunResult(result RecordingResult) error {
	if result.Error != "" {
		return fmt.Errorf("recording result error: %s", result.Error)
	}
	return nil
}

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
