package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

func TestSupersededVariantTaskClosesOnlyItsAttemptAfterWinnerCompletes(t *testing.T) {
	store := newFakeStorage()
	jobID, oldPlan := newReadyStreamJobWithCaptions(t, store, false)
	winningPlan := oldPlan
	winningPlan.Variant = streamclips.VariantStreamerLandscape16x9
	winningPlanJSON, err := json.Marshal(winningPlan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: jobID, Status: streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(jobID),
		Probe:      streamclips.SourceProbe{DurationSeconds: 2},
		EditPlan:   winningPlanJSON,
	})
	worker := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg", RequireAppliedKillfeedAnalysis: true,
	})
	worker.runner = &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("winning-video"), 0o600)
	}}
	oldAttemptID := uuid.New()
	winnerAttemptID := uuid.New()

	oldState, err := streamclips.NewRenderState(
		jobID, oldPlan.Variant, streamclips.StatusRendering, nil, "", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	oldState.AttemptID = oldAttemptID
	oldStateKey, err := streamclips.RenderStateKey(jobID, oldPlan.Variant)
	if err != nil {
		t.Fatal(err)
	}
	putJSON(t, store, oldStateKey, oldState)
	winnerState, err := streamclips.NewRenderState(
		jobID, winningPlan.Variant, streamclips.StatusRendering, nil, "", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	winnerState.AttemptID = winnerAttemptID
	winnerStateKey, err := streamclips.RenderStateKey(jobID, winningPlan.Variant)
	if err != nil {
		t.Fatal(err)
	}
	putJSON(t, store, winnerStateKey, winnerState)

	winningTask := boundStreamRenderAttemptForTest(t, jobID, winningPlan, winnerAttemptID)
	oldTask := boundStreamRenderAttemptForTest(t, jobID, oldPlan, oldAttemptID)
	if err := worker.HandleRenderStreamClip(context.Background(), winningTask); err != nil {
		t.Fatalf("winning render error = %v", err)
	}
	winnerBefore := append([]byte(nil), store.files[winnerStateKey]...)

	if err := worker.HandleRenderStreamClip(context.Background(), oldTask); err != nil {
		t.Fatalf("superseded render error = %v, want recoverable nil", err)
	}
	if got := store.files[winnerStateKey]; !bytes.Equal(got, winnerBefore) {
		t.Fatalf("superseded task overwrote winning state:\ngot  %s\nwant %s", got, winnerBefore)
	}
	parent, err := repo.Get(context.Background(), jobID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Status != streamclips.StatusRendered {
		t.Fatalf("parent status = %s, want rendered winner preserved", parent.Status)
	}
	var closed streamclips.RenderState
	if err := json.Unmarshal(store.files[oldStateKey], &closed); err != nil {
		t.Fatal(err)
	}
	if closed.Status == streamclips.StatusRendering {
		t.Fatalf("superseded attempt remains %s; it must close without touching the winner", closed.Status)
	}
	if closed.Status != streamclips.StatusFailed || closed.Error == "" {
		t.Fatalf("superseded attempt = %+v, want failed with an actionable error", closed)
	}
}

func boundStreamRenderAttemptForTest(
	t *testing.T,
	jobID uuid.UUID,
	plan streamclips.EditPlan,
	attemptID uuid.UUID,
) *asynq.Task {
	t.Helper()
	fingerprint, err := streamclips.EditPlanFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	task, err := tasks.NewBoundRenderStreamClipTask(jobID, plan.Variant, tasks.StreamRenderIntent{
		AttemptID: attemptID, EditPlanFingerprint: fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}
