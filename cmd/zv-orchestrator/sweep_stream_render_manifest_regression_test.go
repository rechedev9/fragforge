package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

func TestSweepRejectsPublishedStreamRenderManifestDivergence(t *testing.T) {
	for _, tc := range []struct {
		name              string
		stateVideos       []streamclips.VideoEntry
		resultVideos      []streamclips.VideoEntry
		materializeResult bool
	}{
		{
			name:              "state and result name different videos",
			stateVideos:       []streamclips.VideoEntry{{ClipID: "state-clip"}},
			resultVideos:      []streamclips.VideoEntry{{ClipID: "result-clip"}},
			materializeResult: true,
		},
		{
			name:         "result-only video is missing",
			resultVideos: []streamclips.VideoEntry{{ClipID: "result-clip"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemoryStreamJobRepository()
			store, err := storage.NewLocal(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			job := seedStreamJob(t, repo, streamclips.StatusRendering)
			variant := streamclips.DefaultVariant().Name
			revisionID := uuid.New()
			prefix, err := streamclips.RenderRevisionPrefix(job.ID, variant, revisionID)
			if err != nil {
				t.Fatal(err)
			}
			resultKey, _ := streamclips.RenderRevisionResultKey(job.ID, variant, revisionID)
			galleryKey, _ := streamclips.RenderRevisionGalleryKey(job.ID, variant, revisionID)
			stateVideos := revisionManifestVideos(t, job.ID, variant, revisionID, tc.stateVideos)
			resultVideos := revisionManifestVideos(t, job.ID, variant, revisionID, tc.resultVideos)
			state, err := streamclips.NewRenderState(
				job.ID, variant, streamclips.StatusRendered, nil, "", stateVideos,
			)
			if err != nil {
				t.Fatal(err)
			}
			state.ArtifactDir = prefix
			state.ResultKey = resultKey
			state.GalleryKey = galleryKey
			result, err := streamclips.NewRenderResult(job.ID, variant, resultVideos, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			putSweepFixture(t, store, resultKey, result)
			if err := store.Put(galleryKey, strings.NewReader("gallery")); err != nil {
				t.Fatal(err)
			}
			for _, video := range stateVideos {
				if err := store.Put(video.Key, strings.NewReader("state-video")); err != nil {
					t.Fatal(err)
				}
			}
			if tc.materializeResult {
				for _, video := range resultVideos {
					if err := store.Put(video.Key, strings.NewReader("result-video")); err != nil {
						t.Fatal(err)
					}
				}
			}
			stateKey, err := streamclips.RenderStateKey(job.ID, variant)
			if err != nil {
				t.Fatal(err)
			}
			putSweepFixture(t, store, stateKey, state)

			if _, err := sweepInterruptedStreamRenderStates(context.Background(), repo, store, nil); err != nil {
				t.Fatal(err)
			}
			parent, err := repo.Get(context.Background(), job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if parent.Status == streamclips.StatusRendered {
				t.Fatal("parent promoted from divergent published manifests")
			}
			var repaired streamclips.RenderState
			readSweepFixture(t, store, stateKey, &repaired)
			if repaired.Status != streamclips.StatusFailed || repaired.HasPublishedRender() {
				t.Fatalf("repaired state = %+v, want failed without published revision", repaired)
			}
		})
	}
}

func revisionManifestVideos(
	t *testing.T,
	jobID uuid.UUID,
	variant string,
	revisionID uuid.UUID,
	videos []streamclips.VideoEntry,
) []streamclips.VideoEntry {
	t.Helper()
	result := make([]streamclips.VideoEntry, len(videos))
	for i, video := range videos {
		key, err := streamclips.RenderRevisionVideoKey(jobID, variant, revisionID, video.ClipID)
		if err != nil {
			t.Fatal(err)
		}
		video.Key = key
		result[i] = video
	}
	return result
}
