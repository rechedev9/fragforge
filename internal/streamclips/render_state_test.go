package streamclips

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestNewRenderStateDerivesArtifactKeys(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	warnings := []string{"minor"}
	videos := []VideoEntry{{ClipID: "clip-001", Key: "video-key"}}

	got, err := NewRenderState(id, VariantStreamerVerticalStack, StatusRendering, warnings, "boom", videos)
	if err != nil {
		t.Fatalf("NewRenderState error = %v", err)
	}

	prefix := "stream-jobs/11111111-1111-1111-1111-111111111111/renders/streamer-vertical-stack"
	if got.JobID != id || got.Variant != VariantStreamerVerticalStack || got.Status != StatusRendering {
		t.Fatalf("identity fields = %#v", got)
	}
	if got.ResultKey != prefix+"/render-result.json" {
		t.Fatalf("result key = %q", got.ResultKey)
	}
	if got.GalleryKey != prefix+"/index.html" {
		t.Fatalf("gallery key = %q", got.GalleryKey)
	}
	if got.ArtifactDir != prefix {
		t.Fatalf("artifact dir = %q", got.ArtifactDir)
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != "minor" || got.Error != "boom" {
		t.Fatalf("status fields = %#v", got)
	}
	if len(got.Videos) != 1 || got.Videos[0].ClipID != "clip-001" {
		t.Fatalf("videos = %#v", got.Videos)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt is zero")
	}

	warnings[0] = "changed"
	videos[0].ClipID = "changed"
	if got.Warnings[0] != "minor" || got.Videos[0].ClipID != "clip-001" {
		t.Fatalf("NewRenderState did not copy slices: %#v", got)
	}
}

func TestNewRenderStateRejectsUnknownVariant(t *testing.T) {
	_, err := NewRenderState(uuid.New(), "other", StatusRendering, nil, "", nil)
	if err == nil {
		t.Fatal("NewRenderState error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unsupported stream render variant") {
		t.Fatalf("error = %q, want unsupported variant", err.Error())
	}
}
