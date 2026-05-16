// Package editor builds local 9:16 short edits from recorder segment clips.
package editor

import (
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/recording"
)

const (
	// PresetShortClean is the first local editing preset: restrained labels,
	// vertical POV crop, and subtle kill punch-ins.
	PresetShortClean = "short-clean"

	// PresetShortPremiumPlayer adds a player cutout and larger headline while
	// keeping the same vertical gameplay base.
	PresetShortPremiumPlayer = "short-premium-player"
)

const (
	// EffectsPresetBuiltinClean reproduces the original local editor look
	// through the Lua effects layer.
	EffectsPresetBuiltinClean = "builtin-clean"

	// EffectsPresetNone disables scripted effects and leaves only the base
	// vertical layout.
	EffectsPresetNone = "none"

	// EffectsPresetAWPGod is a more aggressive local iteration preset with
	// grade, AWP flashes, and stronger punch-ins.
	EffectsPresetAWPGod = "awpgod"

	// EffectsPresetExternal marks manifests rendered from a user script path.
	EffectsPresetExternal = "external"
)

type Config struct {
	RecordingResultPath string
	KillPlanPath        string
	OutputDir           string
	PublishDir          string
	Preset              string
	EffectsPath         string
	EffectsPreset       string
	SegmentIDs          []string
	Limit               int
	PlayerImagePath     string
	PlayerKeyColor      string
	FFmpegPath          string
	FFprobePath         string
	DisableCovers       bool
	SkipExisting        bool
	DryRun              bool
}

type ManifestOptions struct {
	RecordingResultPath string
	KillPlanPath        string
	OutputDir           string
	PublishDir          string
	Preset              string
	EffectsPath         string
	EffectsPreset       string
	SegmentIDs          []string
	Limit               int
	PlayerImagePath     string
	PlayerKeyColor      string
	FFmpegPath          string
	CoversEnabled       bool
	SkipExisting        bool
	KillPlan            *killplan.Plan
}

type Manifest struct {
	Preset          string      `json:"preset"`
	RecordingResult string      `json:"recording_result"`
	KillPlan        string      `json:"killplan,omitempty"`
	OutputDir       string      `json:"output_dir"`
	PublishDir      string      `json:"publish_dir"`
	GalleryPath     string      `json:"gallery_path"`
	SummaryPath     string      `json:"summary_path"`
	SegmentFilter   []string    `json:"segment_filter,omitempty"`
	Limit           int         `json:"limit,omitempty"`
	SkipExisting    bool        `json:"skip_existing,omitempty"`
	EffectsPath     string      `json:"effects_path,omitempty"`
	EffectsPreset   string      `json:"effects_preset,omitempty"`
	PlayerImage     string      `json:"player_image,omitempty"`
	PlayerKeyColor  string      `json:"player_key_color,omitempty"`
	CoversEnabled   bool        `json:"covers_enabled"`
	Shorts          []ShortEdit `json:"shorts"`
	Warnings        []string    `json:"warnings,omitempty"`
}

type ShortEdit struct {
	Index            int       `json:"index"`
	SegmentID        string    `json:"segment_id"`
	Preset           string    `json:"preset,omitempty"`
	Player           string    `json:"player"`
	Map              string    `json:"map,omitempty"`
	KillCount        int       `json:"kill_count"`
	PrimaryWeapon    string    `json:"primary_weapon,omitempty"`
	Input            string    `json:"input"`
	Output           string    `json:"output"`
	PromptPath       string    `json:"prompt_path"`
	PublishPath      string    `json:"publish_path"`
	PlayerImage      string    `json:"player_image,omitempty"`
	PlayerKeyColor   string    `json:"player_key_color,omitempty"`
	CaptionPath      string    `json:"caption_path"`
	CoverPath        string    `json:"cover_path,omitempty"`
	CoverTimeSeconds float64   `json:"cover_time_seconds"`
	DurationSeconds  float64   `json:"duration_seconds,omitempty"`
	Label            string    `json:"label"`
	Title            string    `json:"title"`
	Headline         string    `json:"headline"`
	Caption          string    `json:"caption"`
	Hashtags         []string  `json:"hashtags,omitempty"`
	Kills            []KillCue `json:"kills,omitempty"`
	Effects          []Effect  `json:"effects,omitempty"`
	FFmpegCommand    []string  `json:"ffmpeg_command"`
	CoverCommand     []string  `json:"cover_command,omitempty"`
}

type KillCue struct {
	Tick        int     `json:"tick"`
	TimeSeconds float64 `json:"time_seconds"`
	Weapon      string  `json:"weapon,omitempty"`
	Victim      string  `json:"victim,omitempty"`
	Headshot    bool    `json:"headshot,omitempty"`
	Wallbang    bool    `json:"wallbang,omitempty"`
}

type Result struct {
	Preset          string        `json:"preset"`
	RecordingResult string        `json:"recording_result"`
	KillPlan        string        `json:"killplan,omitempty"`
	OutputDir       string        `json:"output_dir"`
	PublishDir      string        `json:"publish_dir"`
	GalleryPath     string        `json:"gallery_path"`
	SummaryPath     string        `json:"summary_path"`
	SegmentFilter   []string      `json:"segment_filter,omitempty"`
	Limit           int           `json:"limit,omitempty"`
	SkipExisting    bool          `json:"skip_existing,omitempty"`
	EffectsPath     string        `json:"effects_path,omitempty"`
	EffectsPreset   string        `json:"effects_preset,omitempty"`
	PlayerImage     string        `json:"player_image,omitempty"`
	PlayerKeyColor  string        `json:"player_key_color,omitempty"`
	CoversEnabled   bool          `json:"covers_enabled"`
	DryRun          bool          `json:"dry_run,omitempty"`
	Shorts          []ShortResult `json:"shorts"`
	Warnings        []string      `json:"warnings,omitempty"`
	Error           string        `json:"error,omitempty"`
}

type ShortResult struct {
	Index            int                         `json:"index"`
	SegmentID        string                      `json:"segment_id"`
	Preset           string                      `json:"preset,omitempty"`
	Input            string                      `json:"input"`
	Output           string                      `json:"output"`
	PromptPath       string                      `json:"prompt_path"`
	PublishPath      string                      `json:"publish_path"`
	PlayerImage      string                      `json:"player_image,omitempty"`
	PlayerKeyColor   string                      `json:"player_key_color,omitempty"`
	CaptionPath      string                      `json:"caption_path"`
	CoverPath        string                      `json:"cover_path,omitempty"`
	CoverTimeSeconds float64                     `json:"cover_time_seconds"`
	DurationSeconds  float64                     `json:"duration_seconds,omitempty"`
	Title            string                      `json:"title"`
	Headline         string                      `json:"headline"`
	Caption          string                      `json:"caption"`
	Hashtags         []string                    `json:"hashtags,omitempty"`
	Effects          []Effect                    `json:"effects,omitempty"`
	OutputArtifact   recording.RecordingArtifact `json:"output_artifact,omitempty"`
	PublishArtifact  recording.RecordingArtifact `json:"publish_artifact,omitempty"`
	CoverArtifact    recording.RecordingArtifact `json:"cover_artifact,omitempty"`
	RenderSkipped    bool                        `json:"render_skipped,omitempty"`
	CoverSkipped     bool                        `json:"cover_skipped,omitempty"`
	FFmpegCommand    []string                    `json:"ffmpeg_command"`
	CoverCommand     []string                    `json:"cover_command,omitempty"`
}

type PackManifest struct {
	Preset          string        `json:"preset"`
	RecordingResult string        `json:"recording_result"`
	KillPlan        string        `json:"killplan,omitempty"`
	PublishDir      string        `json:"publish_dir"`
	GalleryPath     string        `json:"gallery_path"`
	SummaryPath     string        `json:"summary_path"`
	SegmentFilter   []string      `json:"segment_filter,omitempty"`
	Limit           int           `json:"limit,omitempty"`
	SkipExisting    bool          `json:"skip_existing,omitempty"`
	EffectsPath     string        `json:"effects_path,omitempty"`
	EffectsPreset   string        `json:"effects_preset,omitempty"`
	PlayerImage     string        `json:"player_image,omitempty"`
	PlayerKeyColor  string        `json:"player_key_color,omitempty"`
	CoversEnabled   bool          `json:"covers_enabled"`
	Items           []PublishItem `json:"items"`
	Warnings        []string      `json:"warnings,omitempty"`
}

type PublishItem struct {
	Index            int                         `json:"index"`
	SegmentID        string                      `json:"segment_id"`
	Preset           string                      `json:"preset,omitempty"`
	Player           string                      `json:"player"`
	Map              string                      `json:"map,omitempty"`
	KillCount        int                         `json:"kill_count"`
	PrimaryWeapon    string                      `json:"primary_weapon,omitempty"`
	Source           string                      `json:"source"`
	Video            string                      `json:"video"`
	PlayerImage      string                      `json:"player_image,omitempty"`
	CaptionPath      string                      `json:"caption_path"`
	CoverPath        string                      `json:"cover_path,omitempty"`
	CoverTimeSeconds float64                     `json:"cover_time_seconds"`
	Title            string                      `json:"title"`
	Headline         string                      `json:"headline"`
	Caption          string                      `json:"caption"`
	Hashtags         []string                    `json:"hashtags,omitempty"`
	Effects          []Effect                    `json:"effects,omitempty"`
	DurationSeconds  float64                     `json:"duration_seconds,omitempty"`
	Artifact         recording.RecordingArtifact `json:"artifact,omitempty"`
	CoverArtifact    recording.RecordingArtifact `json:"cover_artifact,omitempty"`
}

type EffectType string

const (
	EffectZoom  EffectType = "zoom"
	EffectFlash EffectType = "flash"
	EffectText  EffectType = "text"
	EffectGrade EffectType = "grade"
)

type Effect struct {
	Type               EffectType `json:"type"`
	StartSeconds       float64    `json:"start_seconds"`
	EndSeconds         float64    `json:"end_seconds"`
	AtSeconds          float64    `json:"at_seconds,omitempty"`
	Scale              float64    `json:"scale,omitempty"`
	Opacity            float64    `json:"opacity,omitempty"`
	Color              string     `json:"color,omitempty"`
	Value              string     `json:"value,omitempty"`
	X                  string     `json:"x,omitempty"`
	Y                  string     `json:"y,omitempty"`
	Size               int        `json:"size,omitempty"`
	FontColor          string     `json:"font_color,omitempty"`
	BoxColor           string     `json:"box_color,omitempty"`
	BoxBorder          int        `json:"box_border,omitempty"`
	Contrast           float64    `json:"contrast,omitempty"`
	Saturation         float64    `json:"saturation,omitempty"`
	Gamma              float64    `json:"gamma,omitempty"`
	Source             string     `json:"source,omitempty"`
	SourceIndex        int        `json:"source_index,omitempty"`
	SourceSegmentID    string     `json:"source_segment_id,omitempty"`
	SourceKillTick     int        `json:"source_kill_tick,omitempty"`
	SourceKillWeapon   string     `json:"source_kill_weapon,omitempty"`
	SourceKillVictim   string     `json:"source_kill_victim,omitempty"`
	SourceKillHeadshot bool       `json:"source_kill_headshot,omitempty"`
}
