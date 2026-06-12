package renderplan

import (
	"errors"
	"testing"

	"github.com/rechedev9/fragforge/internal/editor"
)

func TestRenderVariantFailureMessagePrefersResultError(t *testing.T) {
	got := RenderVariantFailureMessage(editor.Result{Error: "encoder failed"}, errors.New("process failed"))
	if got != "encoder failed" {
		t.Fatalf("RenderVariantFailureMessage = %q, want result error", got)
	}
}

func TestRenderVariantFailureMessageFallsBackToProcessError(t *testing.T) {
	got := RenderVariantFailureMessage(editor.Result{}, errors.New("process failed"))
	if got != "process failed" {
		t.Fatalf("RenderVariantFailureMessage = %q, want process error", got)
	}
}

func TestRenderVariantFailureMessageAllowsEmptyInput(t *testing.T) {
	got := RenderVariantFailureMessage(editor.Result{}, nil)
	if got != "" {
		t.Fatalf("RenderVariantFailureMessage = %q, want empty", got)
	}
}
