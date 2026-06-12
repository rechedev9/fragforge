package renderplan

import (
	"path/filepath"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/editor"
)

// RenderVariantUploadTarget maps one local render output file to its durable
// object-storage key.
type RenderVariantUploadTarget struct {
	Key      string
	Path     string
	Label    string
	Required bool
}

// RenderVariantReadyArtifacts names the durable artifacts that must already
// exist for a render variant to be considered reusable.
type RenderVariantReadyArtifacts struct {
	ResultKey    string
	RequiredKeys []string
}

// NewRenderVariantUploadTargetsOptions carries local render outputs and the
// rendered result metadata needed to plan durable uploads.
type NewRenderVariantUploadTargetsOptions struct {
	JobID      uuid.UUID
	Variant    string
	OutDir     string
	PublishDir string
	ResultPath string
	Result     editor.Result
}

// NewRenderVariantUploadTargets returns the ordered upload plan for one render
// variant. The first target is the required render result; later targets are
// optional artifacts uploaded when their local files exist.
func NewRenderVariantUploadTargets(opts NewRenderVariantUploadTargetsOptions) ([]RenderVariantUploadTarget, error) {
	refs, err := renderVariantArtifactsFor(opts.JobID, opts.Variant)
	if err != nil {
		return nil, err
	}
	targets := []RenderVariantUploadTarget{{
		Key:      refs.RenderResultKey,
		Path:     opts.ResultPath,
		Label:    "render result",
		Required: true,
	}, {
		Key:   refs.EditDocumentKey,
		Path:  filepath.Join(opts.OutDir, "edit-document.json"),
		Label: "edit document",
	}, {
		Key:   refs.EditManifestKey,
		Path:  filepath.Join(opts.OutDir, "edit-manifest.json"),
		Label: "edit manifest",
	}, {
		Key:   refs.PackManifestKey,
		Path:  filepath.Join(opts.PublishDir, "pack-manifest.json"),
		Label: "pack manifest",
	}, {
		Key:   refs.PublishSummaryKey,
		Path:  opts.Result.SummaryPath,
		Label: "publish summary",
	}, {
		Key:   refs.GalleryKey,
		Path:  opts.Result.GalleryPath,
		Label: "gallery",
	}}
	for _, short := range opts.Result.Shorts {
		if short.SegmentID == "" {
			continue
		}
		videoPath := short.PublishPath
		if videoPath == "" {
			videoPath = short.Output
		}
		if videoPath != "" {
			key, err := renderVariantSegmentArtifactKey(opts.JobID, opts.Variant, RenderVariantArtifactVideo, short.SegmentID)
			if err != nil {
				return nil, err
			}
			targets = append(targets, RenderVariantUploadTarget{
				Key:   key,
				Path:  videoPath,
				Label: "render video " + short.SegmentID,
			})
		}
		if short.CoverPath != "" {
			key, err := renderVariantSegmentArtifactKey(opts.JobID, opts.Variant, RenderVariantArtifactCover, short.SegmentID)
			if err != nil {
				return nil, err
			}
			targets = append(targets, RenderVariantUploadTarget{
				Key:   key,
				Path:  short.CoverPath,
				Label: "render cover " + short.SegmentID,
			})
		}
		if short.CaptionPath != "" {
			key, err := renderVariantSegmentArtifactKey(opts.JobID, opts.Variant, RenderVariantArtifactCaption, short.SegmentID)
			if err != nil {
				return nil, err
			}
			targets = append(targets, RenderVariantUploadTarget{
				Key:   key,
				Path:  short.CaptionPath,
				Label: "render caption " + short.SegmentID,
			})
		}
		if short.RenderLogPath != "" {
			key, err := renderVariantLogArtifactKey(opts.JobID, opts.Variant, short.SegmentID)
			if err != nil {
				return nil, err
			}
			targets = append(targets, RenderVariantUploadTarget{
				Key:   key,
				Path:  short.RenderLogPath,
				Label: "render log " + short.SegmentID,
			})
		}
	}
	return targets, nil
}

// NewRenderVariantReadyArtifacts returns the minimal durable artifacts that
// prove a render variant is already materialized enough to skip rerendering.
func NewRenderVariantReadyArtifacts(jobID uuid.UUID, variant string) (RenderVariantReadyArtifacts, error) {
	refs, err := renderVariantArtifactsFor(jobID, variant)
	if err != nil {
		return RenderVariantReadyArtifacts{}, err
	}
	return RenderVariantReadyArtifacts{
		ResultKey: refs.RenderResultKey,
		RequiredKeys: []string{
			refs.PackManifestKey,
			refs.GalleryKey,
		},
	}, nil
}
