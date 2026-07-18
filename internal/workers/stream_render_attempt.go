package workers

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

// ownedStreamRenderState loads the mutable status document and verifies that
// a bound task still owns it. Attempt identity is independent of the parent
// job status: two variants may have queued states while only one owns the
// job-wide render lease.
func (w *StreamRenderWorker) ownedStreamRenderState(
	id uuid.UUID,
	variant string,
	intent tasks.StreamRenderIntent,
	hasIntent bool,
) (streamclips.RenderState, bool, error) {
	key, err := streamclips.RenderStateKey(id, variant)
	if err != nil {
		return streamclips.RenderState{}, false, err
	}
	rc, err := w.storage.Open(key)
	if err != nil {
		if storage.IsNotExist(err) {
			if hasIntent {
				return streamclips.RenderState{}, false, nil
			}
			state, stateErr := streamclips.NewRenderState(id, variant, streamclips.StatusRendering, nil, "", nil)
			return state, stateErr == nil, stateErr
		}
		return streamclips.RenderState{}, false, err
	}
	defer rc.Close()
	var state streamclips.RenderState
	if err := json.NewDecoder(rc).Decode(&state); err != nil {
		return streamclips.RenderState{}, false, fmt.Errorf("decode stream render state: %w", err)
	}
	if state.JobID != id || state.Variant != variant {
		return streamclips.RenderState{}, false, fmt.Errorf("stream render state identity does not match task")
	}
	if err := streamclips.ValidateRenderStateArtifacts(state); err != nil {
		return streamclips.RenderState{}, false, fmt.Errorf("validate stream render state: %w", err)
	}
	if hasIntent && state.AttemptID != intent.AttemptID {
		return state, false, nil
	}
	return state, true, nil
}

func (w *StreamRenderWorker) writeOwnedStreamRenderAttempt(
	id uuid.UUID,
	variant string,
	intent tasks.StreamRenderIntent,
	hasIntent bool,
	status streamclips.Status,
	warnings []string,
	errMsg string,
	errorCode string,
) (bool, error) {
	state, owned, err := w.ownedStreamRenderState(id, variant, intent, hasIntent)
	if err != nil || !owned {
		return owned, err
	}
	if hasIntent {
		state.AttemptID = intent.AttemptID
	}
	state.Status = status
	state.Warnings = append([]string(nil), warnings...)
	state.Error = errMsg
	state.ErrorCode = errorCode
	state.UpdatedAt = time.Now().UTC()
	return true, w.writeStreamRenderState(state)
}

func (w *StreamRenderWorker) ensureStreamRenderAttemptCurrent(
	id uuid.UUID,
	variant string,
	intent tasks.StreamRenderIntent,
	hasIntent bool,
) error {
	if !hasIntent {
		return nil
	}
	_, owned, err := w.ownedStreamRenderState(id, variant, intent, true)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("%w: render attempt no longer owns variant state", errStreamRenderSuperseded)
	}
	return nil
}
