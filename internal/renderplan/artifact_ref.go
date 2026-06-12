package renderplan

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
)

type RenderVariantArtifactKind string

const (
	RenderVariantArtifactResult       RenderVariantArtifactKind = "result"
	RenderVariantArtifactPackManifest RenderVariantArtifactKind = "pack-manifest"
	RenderVariantArtifactEditDocument RenderVariantArtifactKind = "edit-document"
	RenderVariantArtifactGallery      RenderVariantArtifactKind = "gallery"
	RenderVariantArtifactVideo        RenderVariantArtifactKind = "video"
	RenderVariantArtifactCover        RenderVariantArtifactKind = "cover"
	RenderVariantArtifactCaption      RenderVariantArtifactKind = "caption"
)

// RenderVariantArtifactRef identifies one durable render-variant artifact.
type RenderVariantArtifactRef struct {
	Kind      RenderVariantArtifactKind
	Key       string
	SegmentID string
}

// NewRenderVariantArtifactRef derives the durable storage key for one
// render-variant artifact. Segment artifacts require a non-empty segment ID.
func NewRenderVariantArtifactRef(jobID uuid.UUID, variant string, kind RenderVariantArtifactKind, segmentID string) (RenderVariantArtifactRef, error) {
	var (
		key string
		err error
	)
	switch kind {
	case RenderVariantArtifactResult:
		refs, err := renderVariantArtifactsFor(jobID, variant)
		if err == nil {
			key = refs.RenderResultKey
		}
	case RenderVariantArtifactPackManifest:
		refs, err := renderVariantArtifactsFor(jobID, variant)
		if err == nil {
			key = refs.PackManifestKey
		}
	case RenderVariantArtifactEditDocument:
		refs, err := renderVariantArtifactsFor(jobID, variant)
		if err == nil {
			key = refs.EditDocumentKey
		}
	case RenderVariantArtifactGallery:
		refs, err := renderVariantArtifactsFor(jobID, variant)
		if err == nil {
			key = refs.GalleryKey
		}
	case RenderVariantArtifactVideo, RenderVariantArtifactCover, RenderVariantArtifactCaption:
		key, err = renderVariantSegmentArtifactKey(jobID, variant, kind, segmentID)
	default:
		err = fmt.Errorf("unknown render artifact kind %q", kind)
	}
	if err != nil {
		return RenderVariantArtifactRef{}, err
	}
	return RenderVariantArtifactRef{
		Kind:      kind,
		Key:       key,
		SegmentID: segmentID,
	}, nil
}

func renderVariantSegmentArtifactKey(jobID uuid.UUID, variant string, kind RenderVariantArtifactKind, segmentID string) (string, error) {
	switch kind {
	case RenderVariantArtifactVideo:
		return artifacts.RenderVariantVideoKey(jobID, variant, segmentID)
	case RenderVariantArtifactCover:
		return artifacts.RenderVariantCoverKey(jobID, variant, segmentID)
	case RenderVariantArtifactCaption:
		return artifacts.RenderVariantCaptionKey(jobID, variant, segmentID)
	default:
		return "", fmt.Errorf("unknown render segment artifact kind %q", kind)
	}
}

func renderVariantLogArtifactKey(jobID uuid.UUID, variant, segmentID string) (string, error) {
	return artifacts.RenderVariantLogKey(jobID, variant, segmentID+"-render")
}
