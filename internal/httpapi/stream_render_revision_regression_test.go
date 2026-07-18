package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/workers"
)

func TestFailedStreamRerenderKeepsServingLastCommittedRevision(t *testing.T) {
	repo := newFakeStreamRepo()
	store := newFakeStorage()
	locks := streamclips.NewJobLocks()
	jobID := uuid.New()
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 2, Title: "published clip",
	}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo.jobs[jobID] = streamclips.Job{
		ID: jobID, Status: streamclips.StatusRendered,
		SourcePath: streamclips.SourceKey(jobID),
		Probe:      streamclips.SourceProbe{DurationSeconds: 2},
		EditPlan:   planJSON,
	}
	if err := store.Put(streamclips.SourceKey(jobID), strings.NewReader("source-video")); err != nil {
		t.Fatal(err)
	}

	revisionID := uuid.New()
	revisionPrefix, err := streamclips.RenderRevisionPrefix(jobID, plan.Variant, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	videoKey, err := streamclips.RenderRevisionVideoKey(jobID, plan.Variant, revisionID, "clip-001")
	if err != nil {
		t.Fatal(err)
	}
	galleryKey, err := streamclips.RenderRevisionGalleryKey(jobID, plan.Variant, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	resultKey, err := streamclips.RenderRevisionResultKey(jobID, plan.Variant, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	for key, body := range map[string]string{
		videoKey:   "previous-video",
		galleryKey: "previous-gallery",
	} {
		if err := store.Put(key, strings.NewReader(body)); err != nil {
			t.Fatal(err)
		}
	}
	result, err := streamclips.NewRenderResult(jobID, plan.Variant, []streamclips.VideoEntry{{
		ClipID: "clip-001", Key: videoKey, DurationSeconds: 2,
	}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	resultBody, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(resultKey, bytes.NewReader(resultBody)); err != nil {
		t.Fatal(err)
	}
	state, err := streamclips.NewRenderState(jobID, plan.Variant, streamclips.StatusRendered, nil, "", result.Clips)
	if err != nil {
		t.Fatal(err)
	}
	state.ResultKey = resultKey
	state.GalleryKey = galleryKey
	state.ArtifactDir = revisionPrefix

	queue := &fakeQueue{}
	h := NewHandlers(
		newFakeRepo(), store, queue,
		WithStreamRepository(repo), WithStreamJobLocks(locks),
	)
	if err := h.writeStreamRenderState(state); err != nil {
		t.Fatal(err)
	}
	router := Routes(h)
	start := httptest.NewRequest(
		http.MethodPost,
		"/api/stream-jobs/"+jobID.String()+"/renders/"+plan.Variant,
		nil,
	)
	startResponse := httptest.NewRecorder()
	router.ServeHTTP(startResponse, start)
	if startResponse.Code != http.StatusAccepted || len(queue.enqueued) != 1 {
		t.Fatalf(
			"start response = %d %s, queued=%d; want accepted one-task rerender",
			startResponse.Code, startResponse.Body.String(), len(queue.enqueued),
		)
	}

	worker := workers.NewStreamRenderWorker(repo, store, workers.StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg-that-does-not-exist",
		JobLocks: locks, RequireAppliedKillfeedAnalysis: true,
	})
	if err := worker.HandleRenderStreamClip(context.Background(), queue.enqueued[0]); err == nil {
		t.Fatal("rerender error = nil, want injected FFmpeg launch failure")
	}

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{
			name: "video",
			path: "/api/stream-jobs/" + jobID.String() + "/renders/" + plan.Variant + "/videos/clip-001",
			want: "previous-video",
		},
		{
			name: "gallery",
			path: "/api/stream-jobs/" + jobID.String() + "/renders/" + plan.Variant + "/gallery",
			want: "previous-gallery",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rw := httptest.NewRecorder()
			router.ServeHTTP(rw, req)
			if rw.Code != http.StatusOK || rw.Body.String() != tc.want {
				t.Fatalf("response = %d %q, want 200 %q", rw.Code, rw.Body.String(), tc.want)
			}
		})
	}
}
