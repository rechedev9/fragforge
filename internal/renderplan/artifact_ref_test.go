package renderplan

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/editor"
)

func TestNewRenderVariantArtifactRefDerivesDocumentKeys(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	prefix := "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean"
	cases := []struct {
		kind RenderVariantArtifactKind
		want string
	}{
		{RenderVariantArtifactResult, prefix + "/render-result.json"},
		{RenderVariantArtifactPackManifest, prefix + "/pack-manifest.json"},
		{RenderVariantArtifactEditDocument, prefix + "/edit-document.json"},
		{RenderVariantArtifactGallery, prefix + "/index.html"},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			got, err := NewRenderVariantArtifactRef(id, editor.PresetViral60Clean, tc.kind, "")
			if err != nil {
				t.Fatalf("NewRenderVariantArtifactRef error = %v", err)
			}
			if got.Kind != tc.kind || got.Key != tc.want {
				t.Fatalf("artifact ref = %#v, want kind %q key %q", got, tc.kind, tc.want)
			}
		})
	}
}

func TestNewRenderVariantArtifactRefDerivesSegmentKeys(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	prefix := "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean"
	cases := []struct {
		kind RenderVariantArtifactKind
		want string
	}{
		{RenderVariantArtifactVideo, prefix + "/videos/seg-001.mp4"},
		{RenderVariantArtifactCover, prefix + "/covers/seg-001.jpg"},
		{RenderVariantArtifactCaption, prefix + "/captions/seg-001.caption.txt"},
	}

	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			got, err := NewRenderVariantArtifactRef(id, editor.PresetViral60Clean, tc.kind, "seg-001")
			if err != nil {
				t.Fatalf("NewRenderVariantArtifactRef error = %v", err)
			}
			if got.Kind != tc.kind || got.SegmentID != "seg-001" || got.Key != tc.want {
				t.Fatalf("artifact ref = %#v, want kind %q segment seg-001 key %q", got, tc.kind, tc.want)
			}
		})
	}
}

func TestNewRenderVariantArtifactRefRejectsUnknownKind(t *testing.T) {
	_, err := NewRenderVariantArtifactRef(uuid.New(), editor.PresetViral60Clean, "other", "")
	if err == nil {
		t.Fatal("NewRenderVariantArtifactRef error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unknown render artifact kind") {
		t.Fatalf("error = %q, want unknown kind", err.Error())
	}
}
