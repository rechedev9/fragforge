package workers

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

type completionStreamRepo struct {
	job              streamclips.Job
	promotionFailure error
}

func (r *completionStreamRepo) Get(context.Context, uuid.UUID) (streamclips.Job, error) {
	return r.job, nil
}

func (r *completionStreamRepo) UpdateStatus(_ context.Context, _ uuid.UUID, status streamclips.Status, reason string) error {
	if status == streamclips.StatusRendered && r.promotionFailure != nil {
		return r.promotionFailure
	}
	r.job.Status = status
	r.job.FailureReason = reason
	return nil
}

type completionRenderRunner struct{}

func (completionRenderRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	out := args[len(args)-1]
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(out, []byte("rendered-video"), 0o600); err != nil {
		return nil, err
	}
	return nil, nil
}

func TestStreamRenderWorkerPreservesDurableCompletionWhenParentPromotionFails(t *testing.T) {
	id := uuid.New()
	variant := streamclips.DefaultVariant().Name
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 0, EndSeconds: 2}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal edit plan: %v", err)
	}

	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage.NewLocal: %v", err)
	}
	if err := store.Put(streamclips.SourceKey(id), strings.NewReader("source-video")); err != nil {
		t.Fatalf("write stream source: %v", err)
	}

	repo := &completionStreamRepo{
		job: streamclips.Job{
			ID:         id,
			Status:     streamclips.StatusReady,
			SourcePath: streamclips.SourceKey(id),
			EditPlan:   planJSON,
		},
		promotionFailure: context.Canceled,
	}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir:    t.TempDir(),
		FFmpegPath: "ffmpeg",
	})
	w.runner = completionRenderRunner{}
	task, err := tasks.NewRenderStreamClipTask(id, variant)
	if err != nil {
		t.Fatalf("NewRenderStreamClipTask: %v", err)
	}

	err = w.HandleRenderStreamClip(context.Background(), task)
	if !errors.Is(err, errStreamRenderParentPromotion) || !errors.Is(err, context.Canceled) {
		t.Fatalf("HandleRenderStreamClip error = %v, want promotion/context cancellation", err)
	}
	if repo.job.Status != streamclips.StatusRendering || repo.job.FailureReason != "" {
		t.Fatalf("parent after promotion failure = status %q reason %q, want rendering without failure", repo.job.Status, repo.job.FailureReason)
	}

	key, err := streamclips.RenderStateKey(id, variant)
	if err != nil {
		t.Fatalf("RenderStateKey: %v", err)
	}
	rc, err := store.Open(key)
	if err != nil {
		t.Fatalf("open render state: %v", err)
	}
	defer rc.Close()
	var state streamclips.RenderState
	if err := json.NewDecoder(rc).Decode(&state); err != nil {
		t.Fatalf("decode render state: %v", err)
	}
	if state.Status != streamclips.StatusRendered || state.Error != "" || len(state.Videos) != 1 {
		t.Fatalf("durable completion after promotion failure = %+v, want rendered with one video", state)
	}
}
