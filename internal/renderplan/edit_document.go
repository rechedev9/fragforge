// Package renderplan defines stable render intent documents.
package renderplan

import (
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/recording"
)

const EditDocumentSchemaVersion = "1.0"

type EditDocument struct {
	SchemaVersion   string          `json:"schema_version"`
	JobID           uuid.UUID       `json:"job_id"`
	Variant         string          `json:"variant"`
	CreatedAt       time.Time       `json:"created_at"`
	Source          Source          `json:"source"`
	Selection       Selection       `json:"selection"`
	Edit            EditRequest     `json:"edit"`
	LoadoutSnapshot LoadoutSnapshot `json:"loadout_snapshot"`
	Layers          Layers          `json:"layers"`
	Publish         Publish         `json:"publish"`
	Outputs         Outputs         `json:"outputs"`
}

type Source struct {
	RecordingResultKey string `json:"recording_result_key"`
	KillPlanSource     string `json:"killplan_source"`
}

type Selection struct {
	SegmentIDs []string `json:"segment_ids,omitempty"`
	MomentIDs  []string `json:"moment_ids,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

type LoadoutSnapshot struct {
	Preset          string      `json:"preset"`
	EffectsPreset   string      `json:"effects_preset"`
	Framing         string      `json:"framing"`
	VideoCRF        int         `json:"video_crf"`
	VideoPreset     string      `json:"video_preset"`
	HQFilters       bool        `json:"hq_filters"`
	AudioNormalize  bool        `json:"audio_normalize"`
	QualityChecks   bool        `json:"quality_checks"`
	CoverSheets     bool        `json:"cover_sheets"`
	CoversEnabled   bool        `json:"covers_enabled"`
	CaptionsEnabled bool        `json:"captions_enabled"`
	Output          OutputShape `json:"output"`
}

type Layers struct {
	Subtitles bool `json:"subtitles"`
	Hook      bool `json:"hook"`
	Sticker   bool `json:"sticker"`
	Watermark bool `json:"watermark"`
}

type Publish struct {
	Status string `json:"status"`
}

type Outputs struct {
	Prefix          string `json:"prefix"`
	RenderResult    string `json:"render_result"`
	EditManifest    string `json:"edit_manifest"`
	PackManifest    string `json:"pack_manifest"`
	Gallery         string `json:"gallery"`
	PublishSummary  string `json:"publish_summary"`
	UploadReadyRoot string `json:"upload_ready_root"`
}

type NewEditDocumentOptions struct {
	JobID              uuid.UUID
	Variant            string
	Preset             string
	EffectsPreset      string
	Framing            string
	VideoCRF           int
	VideoPreset        string
	HQFilters          bool
	AudioNormalize     bool
	QualityChecks      bool
	CoverSheets        bool
	CoversEnabled      bool
	CaptionsEnabled    bool
	Output             OutputShape
	UploadReadyRoot    string
	RecordingResultKey string
	KillPlanSource     string
	OutputPrefix       string
	RenderResultKey    string
	EditManifestKey    string
	PackManifestKey    string
	GalleryKey         string
	PublishSummaryKey  string
	SegmentIDs         []string
	Edit               EditRequest
}

// NewEditDocumentForLoadoutOptions carries the render loadout plus the current
// source selection needed to write a stable edit intent document.
type NewEditDocumentForLoadoutOptions struct {
	JobID          uuid.UUID
	Loadout        Loadout
	KillPlanSource string
	SegmentIDs     []string
	Edit           EditRequest
}

// NewEditDocumentForLoadout derives storage keys from the loadout's variant
// and snapshots the render intent used by the worker.
func NewEditDocumentForLoadout(opts NewEditDocumentForLoadoutOptions) (EditDocument, error) {
	refs, err := renderVariantArtifactsFor(opts.JobID, opts.Loadout.Variant)
	if err != nil {
		return EditDocument{}, err
	}
	killPlanSource := opts.KillPlanSource
	if killPlanSource == "" {
		killPlanSource = "job.kill_plan"
	}
	edit := NormalizeEditRequest(opts.Edit)
	return NewEditDocument(NewEditDocumentOptions{
		JobID:              opts.JobID,
		Variant:            opts.Loadout.Variant,
		Preset:             opts.Loadout.Preset,
		EffectsPreset:      opts.Loadout.EffectsPreset,
		Framing:            opts.Loadout.Framing,
		VideoCRF:           opts.Loadout.VideoCRF,
		VideoPreset:        opts.Loadout.VideoPreset,
		HQFilters:          opts.Loadout.HQFilters,
		AudioNormalize:     opts.Loadout.AudioNormalize,
		QualityChecks:      opts.Loadout.QualityChecks,
		CoverSheets:        opts.Loadout.CoverSheets,
		CoversEnabled:      opts.Loadout.CoversEnabled,
		CaptionsEnabled:    opts.Loadout.CaptionsEnabled,
		Output:             outputShapeForEdit(opts.Loadout.Output, edit),
		UploadReadyRoot:    opts.Loadout.UploadReadyDir,
		RecordingResultKey: recording.ResultArtifactKey(opts.JobID),
		KillPlanSource:     killPlanSource,
		OutputPrefix:       refs.Prefix,
		RenderResultKey:    refs.RenderResultKey,
		EditManifestKey:    refs.EditManifestKey,
		PackManifestKey:    refs.PackManifestKey,
		GalleryKey:         refs.GalleryKey,
		PublishSummaryKey:  refs.PublishSummaryKey,
		SegmentIDs:         opts.SegmentIDs,
		Edit:               edit,
	}), nil
}

func outputShapeForEdit(base OutputShape, edit EditRequest) OutputShape {
	if edit.Format != FormatLandscape16x9 {
		return base
	}
	base.AspectRatio = "16:9"
	base.Width = 1920
	base.Height = 1080
	return base
}

func NewEditDocument(opts NewEditDocumentOptions) EditDocument {
	effectsPreset := opts.EffectsPreset
	if effectsPreset == "" {
		effectsPreset = "none"
	}
	framing := opts.Framing
	if framing == "" {
		framing = "full-ui"
	}
	uploadReadyRoot := opts.UploadReadyRoot
	if uploadReadyRoot == "" {
		uploadReadyRoot = "shortslistosparasubir"
	}
	return EditDocument{
		SchemaVersion: EditDocumentSchemaVersion,
		JobID:         opts.JobID,
		Variant:       opts.Variant,
		CreatedAt:     time.Now().UTC(),
		Source: Source{
			RecordingResultKey: opts.RecordingResultKey,
			KillPlanSource:     opts.KillPlanSource,
		},
		Selection: Selection{
			SegmentIDs: append([]string(nil), opts.SegmentIDs...),
		},
		Edit: NormalizeEditRequest(opts.Edit),
		LoadoutSnapshot: LoadoutSnapshot{
			Preset:          opts.Preset,
			EffectsPreset:   effectsPreset,
			Framing:         framing,
			VideoCRF:        opts.VideoCRF,
			VideoPreset:     opts.VideoPreset,
			HQFilters:       opts.HQFilters,
			AudioNormalize:  opts.AudioNormalize,
			QualityChecks:   opts.QualityChecks,
			CoverSheets:     opts.CoverSheets,
			CoversEnabled:   opts.CoversEnabled,
			CaptionsEnabled: opts.CaptionsEnabled,
			Output:          opts.Output,
		},
		Layers: Layers{},
		Publish: Publish{
			Status: "draft",
		},
		Outputs: Outputs{
			Prefix:          opts.OutputPrefix,
			RenderResult:    opts.RenderResultKey,
			EditManifest:    opts.EditManifestKey,
			PackManifest:    opts.PackManifestKey,
			Gallery:         opts.GalleryKey,
			PublishSummary:  opts.PublishSummaryKey,
			UploadReadyRoot: uploadReadyRoot,
		},
	}
}
