package renderplan

import (
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
)

const (
	RenderVariantStatusQueued    = "queued"
	RenderVariantStatusRendering = "rendering"
	RenderVariantStatusReady     = "ready"
	RenderVariantStatusFailed    = "failed"
)

// RenderVariantState is the durable product-level state for one materialized
// output variant. It intentionally stores artifact keys, not local paths.
type RenderVariantState struct {
	SchemaVersion     string    `json:"schema_version"`
	JobID             uuid.UUID `json:"job_id"`
	Variant           string    `json:"variant"`
	Status            string    `json:"status"`
	Preset            string    `json:"preset,omitempty"`
	EditDocumentKey   string    `json:"edit_document_key,omitempty"`
	EditManifestKey   string    `json:"edit_manifest_key,omitempty"`
	RenderResultKey   string    `json:"render_result_key,omitempty"`
	PackManifestKey   string    `json:"pack_manifest_key,omitempty"`
	GalleryKey        string    `json:"gallery_key,omitempty"`
	PublishSummaryKey string    `json:"publish_summary_key,omitempty"`
	ArtifactPrefix    string    `json:"artifact_prefix,omitempty"`
	Warnings          []string  `json:"warnings,omitempty"`
	Error             string    `json:"error,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type NewRenderVariantStateOptions struct {
	JobID             uuid.UUID
	Variant           string
	Status            string
	Preset            string
	EditDocumentKey   string
	EditManifestKey   string
	RenderResultKey   string
	PackManifestKey   string
	GalleryKey        string
	PublishSummaryKey string
	ArtifactPrefix    string
	Warnings          []string
	Error             string
	Now               time.Time
	Previous          *RenderVariantState
}

// NewRenderVariantStateForLoadoutOptions carries the product loadout and
// mutable status fields needed to materialize a durable render state.
type NewRenderVariantStateForLoadoutOptions struct {
	JobID    uuid.UUID
	Loadout  Loadout
	Status   string
	Warnings []string
	Error    string
	Now      time.Time
	Previous *RenderVariantState
}

// NewRenderVariantStateForLoadout derives artifact keys from the loadout's
// variant and returns the durable render state document for API and worker
// boundaries.
func NewRenderVariantStateForLoadout(opts NewRenderVariantStateForLoadoutOptions) (RenderVariantState, error) {
	prefix, err := artifacts.RenderVariantPrefix(opts.JobID, opts.Loadout.Variant)
	if err != nil {
		return RenderVariantState{}, err
	}
	resultKey, err := artifacts.RenderVariantResultKey(opts.JobID, opts.Loadout.Variant)
	if err != nil {
		return RenderVariantState{}, err
	}
	editDocumentKey, err := artifacts.RenderVariantEditDocumentKey(opts.JobID, opts.Loadout.Variant)
	if err != nil {
		return RenderVariantState{}, err
	}
	editManifestKey, err := artifacts.RenderVariantEditManifestKey(opts.JobID, opts.Loadout.Variant)
	if err != nil {
		return RenderVariantState{}, err
	}
	packKey, err := artifacts.RenderVariantPackManifestKey(opts.JobID, opts.Loadout.Variant)
	if err != nil {
		return RenderVariantState{}, err
	}
	galleryKey, err := artifacts.RenderVariantGalleryKey(opts.JobID, opts.Loadout.Variant)
	if err != nil {
		return RenderVariantState{}, err
	}
	summaryKey, err := artifacts.RenderVariantPublishSummaryKey(opts.JobID, opts.Loadout.Variant)
	if err != nil {
		return RenderVariantState{}, err
	}
	return NewRenderVariantState(NewRenderVariantStateOptions{
		JobID:             opts.JobID,
		Variant:           opts.Loadout.Variant,
		Status:            opts.Status,
		Preset:            opts.Loadout.Preset,
		EditDocumentKey:   editDocumentKey,
		EditManifestKey:   editManifestKey,
		RenderResultKey:   resultKey,
		PackManifestKey:   packKey,
		GalleryKey:        galleryKey,
		PublishSummaryKey: summaryKey,
		ArtifactPrefix:    prefix,
		Warnings:          opts.Warnings,
		Error:             opts.Error,
		Now:               opts.Now,
		Previous:          opts.Previous,
	}), nil
}

func NewRenderVariantState(opts NewRenderVariantStateOptions) RenderVariantState {
	now := opts.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	createdAt := now
	if opts.Previous != nil && !opts.Previous.CreatedAt.IsZero() {
		createdAt = opts.Previous.CreatedAt
	}
	warnings := append([]string(nil), opts.Warnings...)
	return RenderVariantState{
		SchemaVersion:     "1.0",
		JobID:             opts.JobID,
		Variant:           opts.Variant,
		Status:            opts.Status,
		Preset:            opts.Preset,
		EditDocumentKey:   opts.EditDocumentKey,
		EditManifestKey:   opts.EditManifestKey,
		RenderResultKey:   opts.RenderResultKey,
		PackManifestKey:   opts.PackManifestKey,
		GalleryKey:        opts.GalleryKey,
		PublishSummaryKey: opts.PublishSummaryKey,
		ArtifactPrefix:    opts.ArtifactPrefix,
		Warnings:          warnings,
		Error:             opts.Error,
		CreatedAt:         createdAt,
		UpdatedAt:         now,
	}
}
