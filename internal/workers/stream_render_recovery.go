package workers

import (
	"errors"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

// writeRecoverableStreamRenderState persists the stable machine-readable code
// used by Studio to keep the editor open when exact killfeed row captures need
// regeneration. Other superseded-render failures retain their existing
// message-only contract.
func (w *StreamRenderWorker) writeRecoverableStreamRenderState(
	id uuid.UUID,
	variant string,
	intent tasks.StreamRenderIntent,
	hasIntent bool,
	cause error,
	message string,
) (bool, error) {
	errorCode := streamclips.RenderErrorCodeSuperseded
	if errors.Is(cause, errStreamKillfeedArtifactsStale) {
		errorCode = streamclips.RenderErrorCodeKillfeedArtifactsStale
	}
	return w.writeOwnedStreamRenderAttempt(
		id, variant, intent, hasIntent,
		streamclips.StatusFailed, nil, message, errorCode,
	)
}
