package renderplan

import (
	"fmt"

	"github.com/rechedev9/fragforge/internal/editor"
)

// ValidateRenderVariantUploadResult returns an error when a successful render
// result lacks any Shorts to materialize.
func ValidateRenderVariantUploadResult(result editor.Result) error {
	if result.Error != "" || len(result.Shorts) > 0 {
		return nil
	}
	return fmt.Errorf("render result has no shorts")
}
