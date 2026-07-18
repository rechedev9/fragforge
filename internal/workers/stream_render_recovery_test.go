package workers

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

func TestWriteRecoverableStreamRenderStateCodesOnlyStaleKillfeedArtifacts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cause    error
		wantCode string
	}{
		{
			name:     "stale exact artifacts",
			cause:    errors.Join(errStreamKillfeedArtifactsStale, errors.New("row missing")),
			wantCode: streamclips.RenderErrorCodeKillfeedArtifactsStale,
		},
		{
			name:     "superseded plan",
			cause:    errStreamRenderSuperseded,
			wantCode: streamclips.RenderErrorCodeSuperseded,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeStorage()
			worker := NewStreamRenderWorker(newFakeStreamRepo(), store, StreamRenderWorkerConfig{})
			jobID := uuid.New()
			intent := tasks.StreamRenderIntent{AttemptID: uuid.New()}
			initial, err := streamclips.NewRenderState(jobID, streamclips.VariantStreamer4060, streamclips.StatusRendering, nil, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			initial.AttemptID = intent.AttemptID
			if err := worker.writeStreamRenderState(initial); err != nil {
				t.Fatal(err)
			}
			owned, err := worker.writeRecoverableStreamRenderState(
				jobID, streamclips.VariantStreamer4060, intent, true, tc.cause, "recoverable",
			)
			if err != nil {
				t.Fatal(err)
			}
			if !owned {
				t.Fatal("recoverable state was not owned")
			}
			key, err := streamclips.RenderStateKey(jobID, streamclips.VariantStreamer4060)
			if err != nil {
				t.Fatal(err)
			}
			var state streamclips.RenderState
			if err := json.Unmarshal(store.files[key], &state); err != nil {
				t.Fatal(err)
			}
			if state.Status != streamclips.StatusFailed || state.Error != "recoverable" || state.ErrorCode != tc.wantCode {
				t.Fatalf("state = %#v, want failed/error code %q", state, tc.wantCode)
			}
		})
	}
}
