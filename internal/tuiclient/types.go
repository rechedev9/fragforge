package tuiclient

import (
	"encoding/json"
	"time"
)

// The DTOs below mirror the JSON emitted by internal/httpapi. Field tags match
// the server structs exactly (internal/job, internal/killplan, internal/moments,
// internal/parser roster, internal/renderplan, internal/streamclips). Statuses
// are kept as plain strings here (the server marshals its typed enums to the
// same lowercase names).

// ---- demo -> reel ----------------------------------------------------------

// Job is a demo-to-reel job (internal/job.Job).
type Job struct {
	ID            string          `json:"id"`
	Status        string          `json:"status"`
	FailureReason string          `json:"failure_reason,omitempty"`
	DemoPath      string          `json:"demo_path"`
	DemoSHA256    string          `json:"demo_sha256"`
	TargetSteamID string          `json:"target_steamid"`
	Rules         json.RawMessage `json:"rules,omitempty"`
	KillPlan      *Plan           `json:"kill_plan,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// Demo->reel job statuses (internal/job/job.go).
const (
	StatusQueued    = "queued"
	StatusScanning  = "scanning"
	StatusScanned   = "scanned"
	StatusParsing   = "parsing"
	StatusParsed    = "parsed"
	StatusRecording = "recording"
	StatusRecorded  = "recorded"
	StatusComposing = "composing"
	StatusComposed  = "composed"
	StatusDone      = "done"
	StatusFailed    = "failed"
)

// Plan is the kill plan (internal/killplan.Plan).
type Plan struct {
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	Demo          PlanDemo  `json:"demo"`
	Target        Target    `json:"target"`
	Segments      []Segment `json:"segments"`
	Stats         PlanStats `json:"stats"`
}

type PlanDemo struct {
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	Map           string `json:"map"`
	Tickrate      int    `json:"tickrate"`
	DurationTicks int    `json:"duration_ticks"`
}

type Target struct {
	SteamID64   string `json:"steamid64"`
	NameInDemo  string `json:"name_in_demo"`
	TeamAtStart string `json:"team_at_start"`
}

type Segment struct {
	ID        string `json:"id"`
	Round     int    `json:"round"`
	TickStart int    `json:"tick_start"`
	TickEnd   int    `json:"tick_end"`
	Kills     []Kill `json:"kills,omitempty"`
	Utility   []any  `json:"utility,omitempty"`
}

type Kill struct {
	Tick     int        `json:"tick"`
	Weapon   string     `json:"weapon"`
	Headshot bool       `json:"headshot"`
	Wallbang bool       `json:"wallbang"`
	Victim   KillVictim `json:"victim"`
}

type KillVictim struct {
	SteamID64  string `json:"steamid64"`
	NameInDemo string `json:"name_in_demo"`
	TeamAtKill string `json:"team_at_kill"`
}

type PlanStats struct {
	TotalKillsTarget     int     `json:"total_kills_target"`
	KillsAfterFilters    int     `json:"kills_after_filters"`
	SegmentsCreated      int     `json:"segments_created"`
	DurationSecondsTotal float64 `json:"duration_seconds_total"`
}

// ---- roster (scanned jobs) -------------------------------------------------

// RosterResult is the roster scan artifact (internal/parser.RosterResult),
// streamed by GET /api/jobs/{id}/roster.
type RosterResult struct {
	Players []PlayerStat `json:"players"`
	Match   MatchInfo    `json:"match"`
}

type PlayerStat struct {
	SteamID64 string  `json:"steamid64"`
	Name      string  `json:"name"`
	Team      string  `json:"team"`
	Kills     int     `json:"kills"`
	Deaths    int     `json:"deaths"`
	Assists   int     `json:"assists"`
	Headshots int     `json:"headshots"`
	MVPs      int     `json:"mvps"`
	Rounds    int     `json:"rounds"`
	ADR       float64 `json:"adr"`
	HSPct     float64 `json:"hs_pct"`
	KAST      float64 `json:"kast"`
	Rating    float64 `json:"rating"`
}

type MatchInfo struct {
	Map     string `json:"map"`
	ScoreCT int    `json:"score_ct"`
	ScoreT  int    `json:"score_t"`
	Rounds  int    `json:"rounds"`
}

// ---- moments ---------------------------------------------------------------

// MomentsDocument is the scored-moments artifact (internal/moments.Document).
type MomentsDocument struct {
	SchemaVersion string    `json:"schema_version"`
	JobID         string    `json:"job_id"`
	GeneratedAt   time.Time `json:"generated_at"`
	Moments       []Moment  `json:"moments"`
}

type Moment struct {
	ID              string       `json:"id"`
	SegmentID       string       `json:"segment_id"`
	Player          string       `json:"player,omitempty"`
	Map             string       `json:"map,omitempty"`
	Round           int          `json:"round"`
	DurationSeconds float64      `json:"duration_seconds"`
	Score           float64      `json:"score"`
	ReasonCodes     []string     `json:"reason_codes"`
	Events          MomentEvents `json:"events"`
	Weapons         []string     `json:"weapons,omitempty"`
	Victims         []string     `json:"victims,omitempty"`
	DefaultVariant  string       `json:"default_variant"`
}

type MomentEvents struct {
	Kills     int `json:"kills"`
	Headshots int `json:"headshots,omitempty"`
	Wallbangs int `json:"wallbangs,omitempty"`
}

// ---- render variants -------------------------------------------------------

// RenderVariantState is the durable state of one rendered reel variant
// (internal/renderplan.RenderVariantState).
type RenderVariantState struct {
	JobID     string    `json:"job_id"`
	Variant   string    `json:"variant"`
	Status    string    `json:"status"`
	Preset    string    `json:"preset,omitempty"`
	Warnings  []string  `json:"warnings,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Render variant statuses (internal/renderplan/render_variant.go).
const (
	RenderQueued    = "queued"
	RenderRendering = "rendering"
	RenderReady     = "ready"
	RenderFailed    = "failed"
)

// PublishBoard mirrors GET /api/jobs/{id}/renders/{variant}/publish
// (internal/renderplan.PublishBoard): the upload-readiness of a rendered
// variant's artifacts, plus whether it has been marked uploaded.
type PublishBoard struct {
	JobID           string             `json:"job_id"`
	Variant         string             `json:"variant"`
	Status          string             `json:"status"`
	UploadReadyRoot string             `json:"upload_ready_root"`
	RenderReady     bool               `json:"render_ready"`
	Uploaded        bool               `json:"uploaded"`
	Items           []PublishBoardItem `json:"items"`
	Warnings        []string           `json:"warnings,omitempty"`
	Error           string             `json:"error,omitempty"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

type PublishBoardItem struct {
	SegmentID    string `json:"segment_id"`
	Status       string `json:"status"`
	VideoReady   bool   `json:"video_ready"`
	CoverReady   bool   `json:"cover_ready"`
	CaptionReady bool   `json:"caption_ready"`
}

// ---- capabilities / presets ------------------------------------------------

// Capabilities mirrors GET /api/capabilities (internal/httpapi.GetCapabilities).
type Capabilities struct {
	Record  CapabilityGroup `json:"record"`
	Render  CapabilityGroup `json:"render"`
	Compose struct {
		Enabled bool `json:"enabled"`
	} `json:"compose"`
	Stream StreamCapabilities `json:"stream"`
}

type CapabilityGroup struct {
	Enabled bool          `json:"enabled"`
	Tools   []CaptureTool `json:"tools"`
}

type StreamCapabilities struct {
	YtdlpEnabled   bool          `json:"ytdlp_enabled"`
	WhisperEnabled bool          `json:"whisper_enabled"`
	GroqEnabled    bool          `json:"groq_enabled"`
	Tools          []CaptureTool `json:"tools"`
}

type CaptureTool struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Source     string `json:"source"`
	Configured bool   `json:"configured"`
	Accessible bool   `json:"accessible"`
}

// PresetList mirrors GET /api/presets.
type PresetList struct {
	Default string   `json:"default"`
	Presets []Preset `json:"presets"`
}

type Preset struct {
	Name          string `json:"name"`
	Label         string `json:"label"`
	Description   string `json:"description"`
	Default       bool   `json:"default"`
	FPS           int    `json:"fps"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	EffectsPreset string `json:"effects_preset,omitempty"`
}

// ---- stream clips ----------------------------------------------------------

// StreamJob is a streamer-MP4 clip job (internal/streamclips.Job).
type StreamJob struct {
	ID            string          `json:"id"`
	Status        string          `json:"status"`
	FailureReason string          `json:"failure_reason,omitempty"`
	SourcePath    string          `json:"source_path"`
	SourceSHA256  string          `json:"source_sha256"`
	SourceURL     string          `json:"source_url,omitempty"`
	Title         string          `json:"title,omitempty"`
	Probe         SourceProbe     `json:"probe"`
	EditPlan      json.RawMessage `json:"edit_plan,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// Stream job statuses (internal/streamclips/types.go).
const (
	StreamAcquiring = "acquiring"
	StreamUploaded  = "uploaded"
	StreamReady     = "ready"
	StreamRendering = "rendering"
	StreamRendered  = "rendered"
	StreamFailed    = "failed"

	// StreamDefaultVariant is the product-default stream render variant
	// (internal/streamclips.DefaultVariant): a 40% facecam / 60% gameplay
	// vertical stack. "streamer-vertical-stack" (no suffix) is a legacy 35/65
	// layout, so the suffix here is deliberate.
	StreamDefaultVariant = "streamer-vertical-stack-40-60"
)

type SourceProbe struct {
	Width           int      `json:"width,omitempty"`
	Height          int      `json:"height,omitempty"`
	DurationSeconds float64  `json:"duration_seconds,omitempty"`
	VideoCodec      string   `json:"video_codec,omitempty"`
	AudioCodec      string   `json:"audio_codec,omitempty"`
	FrameRate       string   `json:"frame_rate,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

// StreamEditPlan is the render plan for a stream job (internal/streamclips.EditPlan).
type StreamEditPlan struct {
	SchemaVersion string          `json:"schema_version"`
	Variant       string          `json:"variant"`
	FaceCrop      CropRect        `json:"face_crop"`
	GameplayCrop  CropRect        `json:"gameplay_crop"`
	Clips         []ClipRange     `json:"clips"`
	Captions      json.RawMessage `json:"captions,omitempty"`
	Music         json.RawMessage `json:"music,omitempty"`
	Effects       json.RawMessage `json:"effects,omitempty"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

type CropRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type ClipRange struct {
	ID           string  `json:"id"`
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
	Title        string  `json:"title,omitempty"`
}

// StreamRenderState is the render state of a stream job (internal/streamclips.RenderState).
type StreamRenderState struct {
	JobID     string             `json:"job_id"`
	Variant   string             `json:"variant"`
	Status    string             `json:"status"`
	Warnings  []string           `json:"warnings,omitempty"`
	Error     string             `json:"error,omitempty"`
	UpdatedAt time.Time          `json:"updated_at"`
	Videos    []StreamVideoEntry `json:"videos,omitempty"`
}

type StreamVideoEntry struct {
	ClipID          string  `json:"clip_id"`
	Title           string  `json:"title,omitempty"`
	Key             string  `json:"key"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}
