package renderplan

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

const PublishBoardSchemaVersion = "1.0"

type PublishBoard struct {
	SchemaVersion   string             `json:"schema_version"`
	JobID           uuid.UUID          `json:"job_id"`
	Variant         string             `json:"variant"`
	Status          string             `json:"status"`
	UploadReadyRoot string             `json:"upload_ready_root"`
	RenderReady     bool               `json:"render_ready"`
	RenderResultKey string             `json:"render_result_key,omitempty"`
	PackManifestKey string             `json:"pack_manifest_key,omitempty"`
	GalleryKey      string             `json:"gallery_key,omitempty"`
	PublishSummary  string             `json:"publish_summary_key,omitempty"`
	Uploaded        bool               `json:"uploaded"`
	Items           []PublishBoardItem `json:"items"`
	Warnings        []string           `json:"warnings,omitempty"`
	Error           string             `json:"error,omitempty"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type PublishBoardItem struct {
	SegmentID    string `json:"segment_id"`
	Status       string `json:"status"`
	VideoKey     string `json:"video_key,omitempty"`
	CoverKey     string `json:"cover_key,omitempty"`
	CaptionKey   string `json:"caption_key,omitempty"`
	VideoReady   bool   `json:"video_ready"`
	CoverReady   bool   `json:"cover_ready"`
	CaptionReady bool   `json:"caption_ready"`
}

type NewPublishBoardOptions struct {
	JobID           uuid.UUID
	Variant         string
	UploadReadyRoot string
	RenderResultKey string
	PackManifestKey string
	GalleryKey      string
	PublishSummary  string
	Uploaded        bool
	Items           []PublishBoardItem
	Warnings        []string
	Error           string
}

// ArtifactExistsFunc reports whether an artifact key currently exists.
type ArtifactExistsFunc func(key string) (bool, error)

// NewPublishBoardForVariantOptions carries the render result summary plus the
// storage readiness probe needed to build a publish board.
type NewPublishBoardForVariantOptions struct {
	JobID           uuid.UUID
	Variant         string
	UploadReadyRoot string
	SegmentIDs      []string
	Uploaded        bool
	Warnings        []string
	Error           string
	ArtifactExists  ArtifactExistsFunc
}

// NewPublishBoardForVariant derives the publish-board artifact keys for a
// render variant and probes each upload-ready item for storage readiness.
func NewPublishBoardForVariant(opts NewPublishBoardForVariantOptions) (PublishBoard, error) {
	refs, err := renderVariantArtifactsFor(opts.JobID, opts.Variant)
	if err != nil {
		return PublishBoard{}, err
	}
	exists := opts.ArtifactExists
	if exists == nil {
		exists = func(string) (bool, error) { return false, nil }
	}
	items := make([]PublishBoardItem, 0, len(opts.SegmentIDs))
	for _, segmentID := range opts.SegmentIDs {
		if segmentID == "" {
			continue
		}
		videoKey, err := renderVariantSegmentArtifactKey(opts.JobID, opts.Variant, RenderVariantArtifactVideo, segmentID)
		if err != nil {
			return PublishBoard{}, err
		}
		coverKey, err := renderVariantSegmentArtifactKey(opts.JobID, opts.Variant, RenderVariantArtifactCover, segmentID)
		if err != nil {
			return PublishBoard{}, err
		}
		captionKey, err := renderVariantSegmentArtifactKey(opts.JobID, opts.Variant, RenderVariantArtifactCaption, segmentID)
		if err != nil {
			return PublishBoard{}, err
		}
		videoReady, err := exists(videoKey)
		if err != nil {
			return PublishBoard{}, fmt.Errorf("check video artifact %s: %w", segmentID, err)
		}
		coverReady, err := exists(coverKey)
		if err != nil {
			return PublishBoard{}, fmt.Errorf("check cover artifact %s: %w", segmentID, err)
		}
		captionReady, err := exists(captionKey)
		if err != nil {
			return PublishBoard{}, fmt.Errorf("check caption artifact %s: %w", segmentID, err)
		}
		items = append(items, PublishBoardItem{
			SegmentID:    segmentID,
			VideoKey:     videoKey,
			CoverKey:     coverKey,
			CaptionKey:   captionKey,
			VideoReady:   videoReady,
			CoverReady:   coverReady,
			CaptionReady: captionReady,
		})
	}
	return NewPublishBoard(NewPublishBoardOptions{
		JobID:           opts.JobID,
		Variant:         opts.Variant,
		UploadReadyRoot: opts.UploadReadyRoot,
		RenderResultKey: refs.RenderResultKey,
		PackManifestKey: refs.PackManifestKey,
		GalleryKey:      refs.GalleryKey,
		PublishSummary:  refs.PublishSummaryKey,
		Uploaded:        opts.Uploaded,
		Items:           items,
		Warnings:        opts.Warnings,
		Error:           opts.Error,
	}), nil
}

func NewPublishBoard(opts NewPublishBoardOptions) PublishBoard {
	root := opts.UploadReadyRoot
	if root == "" {
		root = "shortslistosparasubir"
	}
	board := PublishBoard{
		SchemaVersion:   PublishBoardSchemaVersion,
		JobID:           opts.JobID,
		Variant:         opts.Variant,
		UploadReadyRoot: root,
		RenderResultKey: opts.RenderResultKey,
		PackManifestKey: opts.PackManifestKey,
		GalleryKey:      opts.GalleryKey,
		PublishSummary:  opts.PublishSummary,
		Uploaded:        opts.Uploaded,
		Items:           append([]PublishBoardItem(nil), opts.Items...),
		Warnings:        append([]string(nil), opts.Warnings...),
		Error:           opts.Error,
		UpdatedAt:       time.Now().UTC(),
	}
	board.RenderReady, board.Status = summarizePublishBoard(board.Items, board.Error, board.Uploaded)
	return board
}

func summarizePublishBoard(items []PublishBoardItem, resultError string, uploaded bool) (bool, string) {
	if resultError != "" {
		return false, "failed"
	}
	if len(items) == 0 {
		return false, "draft"
	}
	allReady := true
	needsCover := false
	needsCaption := false
	for i := range items {
		item := &items[i]
		switch {
		case !item.VideoReady:
			item.Status = "missing_video"
			allReady = false
		case !item.CoverReady:
			item.Status = "needs_cover"
			needsCover = true
			allReady = false
		case !item.CaptionReady:
			item.Status = "needs_caption"
			needsCaption = true
			allReady = false
		default:
			item.Status = "ready"
		}
	}
	switch {
	case uploaded && allReady:
		return true, "uploaded"
	case allReady:
		return true, "ready"
	case needsCover:
		return false, "needs_cover"
	case needsCaption:
		return false, "needs_caption"
	default:
		return false, "draft"
	}
}
