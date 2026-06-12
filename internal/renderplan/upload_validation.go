package renderplan

import (
	"fmt"

	"github.com/rechedev9/fragforge/internal/editor"
)

// ValidateRenderVariantRunResult returns an error when the editor wrote a
// structured failure result after the process completed.
func ValidateRenderVariantRunResult(result editor.Result) error {
	if result.Error != "" {
		return fmt.Errorf("render result error: %s", result.Error)
	}
	return nil
}

// ValidateRenderVariantUploadResult returns an error when a successful render
// result lacks any Shorts to materialize.
func ValidateRenderVariantUploadResult(result editor.Result) error {
	if result.Error != "" || len(result.Shorts) > 0 {
		return nil
	}
	return fmt.Errorf("render result has no shorts")
}
