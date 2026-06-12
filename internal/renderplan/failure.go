package renderplan

import "github.com/rechedev9/fragforge/internal/editor"

// RenderVariantFailureMessage returns the durable failure message for a render
// variant state, preferring the editor's structured result error when present.
func RenderVariantFailureMessage(result editor.Result, err error) string {
	if result.Error != "" {
		return result.Error
	}
	if err != nil {
		return err.Error()
	}
	return ""
}
