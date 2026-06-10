// Package renderplan defines stable render intent documents.
package renderplan

import (
	"time"

	"github.com/google/uuid"
)

const EditDocumentSchemaVersion = "1.0"

type EditDocument struct {
	SchemaVersion   string          `json:"schema_version"`
	JobID           uuid.UUID       `json:"job_id"`
	Variant         string          `json:"variant"`
	CreatedAt       time.Time       `json:"created_at"`
	Source          Source          `json:"source"`
	Selection       Selection       `json:"selection"`
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
