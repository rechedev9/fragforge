package composition

import "fmt"

// ValidateUploadResult returns an error when a composition result is not a
// successful final output that can be materialized.
func ValidateUploadResult(result Result) error {
	if result.Error != "" {
		return fmt.Errorf("composition result error: %s", result.Error)
	}
	if result.Output == "" {
		return fmt.Errorf("composition result has no output")
	}
	return nil
}
