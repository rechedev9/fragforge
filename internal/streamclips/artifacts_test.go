package streamclips

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRenderRevisionKeysAreIsolatedByRevision(t *testing.T) {
	jobID := uuid.New()
	first := uuid.New()
	second := uuid.New()
	firstKey, err := RenderRevisionVideoKey(jobID, VariantStreamer4060, first, "clip-1")
	if err != nil {
		t.Fatal(err)
	}
	secondKey, err := RenderRevisionVideoKey(jobID, VariantStreamer4060, second, "clip-1")
	if err != nil {
		t.Fatal(err)
	}
	if firstKey == secondKey {
		t.Fatalf("revision video keys collide: %q", firstKey)
	}
	if !strings.Contains(firstKey, "/revisions/"+first.String()+"/") {
		t.Fatalf("revision video key = %q, want revision namespace", firstKey)
	}
	if _, err := RenderRevisionCaptionKey(jobID, VariantStreamer4060, uuid.Nil, "clip-1"); err == nil {
		t.Fatal("nil revision id error = nil")
	}
}

func TestValidateRenderStateArtifactsAcceptsOneRevisionAndRejectsEscapes(t *testing.T) {
	jobID := uuid.New()
	revisionID := uuid.New()
	variant := VariantStreamer4060
	prefix, err := RenderRevisionPrefix(jobID, variant, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	resultKey, _ := RenderRevisionResultKey(jobID, variant, revisionID)
	galleryKey, _ := RenderRevisionGalleryKey(jobID, variant, revisionID)
	videoKey, _ := RenderRevisionVideoKey(jobID, variant, revisionID, "clip-1")
	state := RenderState{
		JobID: jobID, Variant: variant, ArtifactDir: prefix,
		ResultKey: resultKey, GalleryKey: galleryKey,
		Videos: []VideoEntry{{ClipID: "clip-1", Key: videoKey}},
	}
	if err := ValidateRenderStateArtifacts(state); err != nil {
		t.Fatalf("valid revision state rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*RenderState)
	}{
		{name: "foreign gallery", mutate: func(s *RenderState) { s.GalleryKey = SourceKey(jobID) }},
		{name: "foreign video", mutate: func(s *RenderState) { s.Videos[0].Key = SourceKey(jobID) }},
		{name: "nested revision", mutate: func(s *RenderState) { s.ArtifactDir += "/extra" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := state
			got.Videos = append([]VideoEntry(nil), state.Videos...)
			test.mutate(&got)
			if err := ValidateRenderStateArtifacts(got); err == nil {
				t.Fatal("validation error = nil")
			}
		})
	}
}
