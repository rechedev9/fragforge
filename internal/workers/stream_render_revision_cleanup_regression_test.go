package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

func TestSuccessfulStreamRerenderDeletesPreviousRevisionAndKeepsWinner(t *testing.T) {
	jobID := uuid.New()
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 2, Title: "rerender",
	}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(streamclips.SourceKey(jobID), bytes.NewReader([]byte("source-video"))); err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: jobID, Status: streamclips.StatusRendered,
		SourcePath: streamclips.SourceKey(jobID),
		Probe:      streamclips.SourceProbe{DurationSeconds: 2},
		EditPlan:   planJSON,
	})

	oldRevisionID := uuid.New()
	oldPrefix, err := streamclips.RenderRevisionPrefix(jobID, plan.Variant, oldRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	oldResultKey, _ := streamclips.RenderRevisionResultKey(jobID, plan.Variant, oldRevisionID)
	oldGalleryKey, _ := streamclips.RenderRevisionGalleryKey(jobID, plan.Variant, oldRevisionID)
	oldVideoKey, _ := streamclips.RenderRevisionVideoKey(jobID, plan.Variant, oldRevisionID, plan.Clips[0].ID)
	oldVideos := []streamclips.VideoEntry{{ClipID: plan.Clips[0].ID, Key: oldVideoKey}}
	oldResult, err := streamclips.NewRenderResult(jobID, plan.Variant, oldVideos, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	putRevisionCleanupJSON(t, store, oldResultKey, oldResult)
	for key, body := range map[string]string{
		oldGalleryKey: "old-gallery",
		oldVideoKey:   "old-video",
	} {
		if err := store.Put(key, bytes.NewReader([]byte(body))); err != nil {
			t.Fatal(err)
		}
	}

	task, err := tasks.NewBoundRenderStreamClipTask(jobID, plan.Variant, tasks.StreamRenderIntent{
		AttemptID:           uuid.New(),
		EditPlanFingerprint: mustEditPlanFingerprintForRevisionCleanup(t, plan),
	})
	if err != nil {
		t.Fatal(err)
	}
	intent, ok, err := tasks.StreamRenderIntentFromTask(task)
	if err != nil || !ok {
		t.Fatalf("StreamRenderIntentFromTask = (%+v, %v, %v)", intent, ok, err)
	}
	admitted, err := streamclips.NewRenderState(
		jobID, plan.Variant, streamclips.StatusRendering, nil, "", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	admitted.AttemptID = intent.AttemptID
	previous, err := streamclips.NewRenderState(
		jobID, plan.Variant, streamclips.StatusRendered, nil, "", oldVideos,
	)
	if err != nil {
		t.Fatal(err)
	}
	previous.ArtifactDir = oldPrefix
	previous.ResultKey = oldResultKey
	previous.GalleryKey = oldGalleryKey
	admitted.PreservePublishedRender(previous)
	stateKey, err := streamclips.RenderStateKey(jobID, plan.Variant)
	if err != nil {
		t.Fatal(err)
	}
	putRevisionCleanupJSON(t, store, stateKey, admitted)

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
	if err := worker.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	var winner streamclips.RenderState
	readRevisionCleanupJSON(t, store, stateKey, &winner)
	if !winner.HasPublishedRender() || winner.ArtifactDir == oldPrefix || len(winner.Videos) != 1 {
		t.Fatalf("winner state = %+v", winner)
	}
	for _, key := range []string{winner.ResultKey, winner.GalleryKey, winner.Videos[0].Key} {
		exists, err := store.Exists(key)
		if err != nil || !exists {
			t.Fatalf("winning artifact %s exists = %v, error = %v", key, exists, err)
		}
	}
	for _, key := range []string{oldResultKey, oldGalleryKey, oldVideoKey} {
		exists, err := store.Exists(key)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("superseded revision artifact remains: %s", key)
		}
	}
}

func mustEditPlanFingerprintForRevisionCleanup(t *testing.T, plan streamclips.EditPlan) string {
	t.Helper()
	fingerprint, err := streamclips.EditPlanFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func putRevisionCleanupJSON(t *testing.T, store storage.Storage, key string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(key, bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
}

func readRevisionCleanupJSON(t *testing.T, store storage.Storage, key string, value any) {
	t.Helper()
	r, err := store.Open(key)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := json.NewDecoder(r).Decode(value); err != nil {
		t.Fatal(err)
	}
}
