// Package editor builds local 9:16 short edits from recorder segment clips.
package editor

import (
	"image"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/recording"
)

const (
	// Legacy render preset names are retained only so old manifests and focused
	// filter tests can still refer to their historical values. They are not
	// registered or accepted as product presets.
	PresetShortClean              = "short-clean"
	PresetShortPremiumPlayer      = "short-premium-player"
	PresetShortViralSquare        = "viral-square"
	PresetShortNaturalHQ          = "natural-hq"
	PresetShortNaturalHQ2         = "natural-hq2"
	PresetShortNaturalHQ2Full     = "natural-hq2-full"
	PresetShortNaturalHQ2FullPlus = "natural-hq2-full-plus"
	PresetShortNaturalHQ3         = "natural-hq3"
	PresetShortNaturalHQ3Smooth   = "natural-hq3-smooth"
	PresetSmokeLineups            = "smoke-lineups"
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

	// EffectsPresetSmokeLineups annotates utility clips with restrained
	// tutorial labels.
	EffectsPresetSmokeLineups = "smoke-lineups"

	// EffectsPresetViralUltra is the aggressive viral overlay pack: cold-open
	// hook text, kill punch-ins with flashes, a running kill counter, and
	// milestone labels.
	EffectsPresetViralUltra = "viral-ultra"

	// EffectsPresetViralUltraClean is viral-ultra plus a killfeed overlay on
	// every kill, for HUD-less recordings where the vertical crop would
	// otherwise lose the death notices.
	EffectsPresetViralUltraClean = "viral-ultra-clean"

	// EffectsPresetExternal marks manifests rendered from a user script path.
	EffectsPresetExternal = "external"
)

const (
	// DefaultVideoCRF keeps the historical editor quality/speed tradeoff.
	DefaultVideoCRF = 18

	// DefaultVideoPreset keeps the historical x264 speed setting.
	DefaultVideoPreset = "fast"

	// NaturalHQVideoCRF is the quality setting for the natural-hq preset.
	NaturalHQVideoCRF = 16

	// NaturalHQVideoPreset is the x264 speed/quality setting for natural-hq.
	NaturalHQVideoPreset = "slow"

	// NaturalHQ3VideoCRF raises quality one step beyond natural-hq2.
	NaturalHQ3VideoCRF = 15

	// NaturalHQ3VideoPreset is intentionally slower for cleaner local masters.
	NaturalHQ3VideoPreset = "slower"

	// NaturalHQ2FullPlusVideoCRF raises full-frame comparison renders one step
	// beyond natural-hq2-full.
	NaturalHQ2FullPlusVideoCRF = 15

	// NaturalHQ2FullPlusVideoPreset spends more encode time on cleaner masters.
	NaturalHQ2FullPlusVideoPreset = "slower"
)

type Config struct {
	RecordingResultPath string
	KillPlanPath        string
	OutputDir           string
	PublishDir          string
	Preset              string
	EffectsPath         string
	EffectsPreset       string
	MusicPath           string
	RhythmPath          string
	OutputFPS           int
	CompileSegments     bool
	LineupCatalogPath   string
	SegmentIDs          []string
	Limit               int
	PlayerImagePath     string
	PlayerKeyColor      string
	VideoCRF            int
	VideoPreset         string
	HQFilters           bool
	AudioNormalize      bool
	QualityChecks       bool
	CoverSheets         bool
	TemporalSmoothing   bool
	FFmpegPath          string
	FFprobePath         string
	DisableCovers       bool
	SkipExisting        bool
	DryRun              bool
	// RenderJobs caps how many shorts render concurrently; 0 selects an
	// automatic limit based on available CPUs.
	RenderJobs int
}

type ManifestOptions struct {
	RecordingResultPath string
	KillPlanPath        string
	OutputDir           string
	PublishDir          string
	Preset              string
	EffectsPath         string
	EffectsPreset       string
	MusicPath           string
	RhythmPath          string
	OutputFPS           int
	CompileSegments     bool
	LineupCatalogPath   string
	SegmentIDs          []string
	Limit               int
	PlayerImagePath     string
	PlayerKeyColor      string
	VideoCRF            int
	VideoPreset         string
	HQFilters           bool
	AudioNormalize      bool
	QualityChecks       bool
	CoverSheets         bool
	TemporalSmoothing   bool
	FFmpegPath          string
	CoversEnabled       bool
	SkipExisting        bool
	KillPlan            *killplan.Plan
	// KillfeedFrameProbe loads a source frame for per-kill killfeed crop
	// measurement; nil keeps the static crop defaults.
	KillfeedFrameProbe func(input string, atSeconds float64) (image.Image, error)
}

type Manifest struct {
	Preset            string      `json:"preset"`
	RecordingResult   string      `json:"recording_result"`
	KillPlan          string      `json:"killplan,omitempty"`
	OutputDir         string      `json:"output_dir"`
	PublishDir        string      `json:"publish_dir"`
	GalleryPath       string      `json:"gallery_path"`
	SummaryPath       string      `json:"summary_path"`
	SegmentFilter     []string    `json:"segment_filter,omitempty"`
	Limit             int         `json:"limit,omitempty"`
	SkipExisting      bool        `json:"skip_existing,omitempty"`
	EffectsPath       string      `json:"effects_path,omitempty"`
	EffectsPreset     string      `json:"effects_preset,omitempty"`
	MusicPath         string      `json:"music_path,omitempty"`
	RhythmPath        string      `json:"rhythm_path,omitempty"`
	OutputFPS         int         `json:"output_fps,omitempty"`
	CompileSegments   bool        `json:"compile_segments,omitempty"`
	LineupCatalogPath string      `json:"lineup_catalog_path,omitempty"`
	UnmatchedSmokes   string      `json:"unmatched_smokes,omitempty"`
	PlayerImage       string      `json:"player_image,omitempty"`
	PlayerKeyColor    string      `json:"player_key_color,omitempty"`
	VideoCRF          int         `json:"video_crf,omitempty"`
	VideoPreset       string      `json:"video_preset,omitempty"`
	HQFilters         bool        `json:"hq_filters,omitempty"`
	AudioNormalize    bool        `json:"audio_normalize,omitempty"`
	QualityChecks     bool        `json:"quality_checks,omitempty"`
	CoverSheets       bool        `json:"cover_sheets,omitempty"`
	TemporalSmoothing bool        `json:"temporal_smoothing,omitempty"`
	CoversEnabled     bool        `json:"covers_enabled"`
	Shorts            []ShortEdit `json:"shorts"`
	Warnings          []string    `json:"warnings,omitempty"`
}

type ShortEdit struct {
	Index             int                         `json:"index"`
	SegmentID         string                      `json:"segment_id"`
	Preset            string                      `json:"preset,omitempty"`
	Player            string                      `json:"player"`
	Map               string                      `json:"map,omitempty"`
	KillCount         int                         `json:"kill_count"`
	PrimaryWeapon     string                      `json:"primary_weapon,omitempty"`
	SmokeCount        int                         `json:"smoke_count,omitempty"`
	PrimarySmoke      string                      `json:"primary_smoke,omitempty"`
	Input             string                      `json:"input"`
	Output            string                      `json:"output"`
	SourceArtifact    recording.RecordingArtifact `json:"source_artifact,omitempty"`
	OutputArtifact    recording.RecordingArtifact `json:"output_artifact,omitempty"`
	PublishArtifact   recording.RecordingArtifact `json:"publish_artifact,omitempty"`
	PromptPath        string                      `json:"prompt_path"`
	PublishPath       string                      `json:"publish_path"`
	PlayerImage       string                      `json:"player_image,omitempty"`
	PlayerKeyColor    string                      `json:"player_key_color,omitempty"`
	MusicPath         string                      `json:"music_path,omitempty"`
	RhythmPath        string                      `json:"rhythm_path,omitempty"`
	OutputFPS         int                         `json:"output_fps,omitempty"`
	VideoCRF          int                         `json:"video_crf,omitempty"`
	VideoPreset       string                      `json:"video_preset,omitempty"`
	HQFilters         bool                        `json:"hq_filters,omitempty"`
	AudioNormalize    bool                        `json:"audio_normalize,omitempty"`
	TemporalSmoothing bool                        `json:"temporal_smoothing,omitempty"`
	CaptionPath       string                      `json:"caption_path"`
	CoverPath         string                      `json:"cover_path,omitempty"`
	CoverSheetPath    string                      `json:"cover_sheet_path,omitempty"`
	CoverTimeSeconds  float64                     `json:"cover_time_seconds"`
	DurationSeconds   float64                     `json:"duration_seconds,omitempty"`
	Label             string                      `json:"label"`
	Title             string                      `json:"title"`
	Headline          string                      `json:"headline"`
	Caption           string                      `json:"caption"`
	Hashtags          []string                    `json:"hashtags,omitempty"`
	Kills             []KillCue                   `json:"kills,omitempty"`
	Smokes            []SmokeCue                  `json:"smokes,omitempty"`
	Parts             []ShortPart                 `json:"parts,omitempty"`
	Effects           []Effect                    `json:"effects,omitempty"`
	FFmpegCommand     []string                    `json:"ffmpeg_command"`
	CoverCommand      []string                    `json:"cover_command,omitempty"`
	CoverSheetCommand []string                    `json:"cover_sheet_command,omitempty"`
	QualityCommand    []string                    `json:"quality_command,omitempty"`
	RenderLogPath     string                      `json:"render_log_path,omitempty"`
	QualityLogPath    string                      `json:"quality_log_path,omitempty"`
}

type ShortPart struct {
	SegmentID            string                      `json:"segment_id"`
	Input                string                      `json:"input"`
	SourceArtifact       recording.RecordingArtifact `json:"source_artifact,omitempty"`
	DurationSeconds      float64                     `json:"duration_seconds,omitempty"`
	TimelineStartSeconds float64                     `json:"timeline_start_seconds,omitempty"`
	GapBeforeSeconds     float64                     `json:"gap_before_seconds,omitempty"`
	Kills                []KillCue                   `json:"kills,omitempty"`
}

type KillCue struct {
	Tick        int     `json:"tick"`
	TimeSeconds float64 `json:"time_seconds"`
	Weapon      string  `json:"weapon,omitempty"`
	Victim      string  `json:"victim,omitempty"`
	Headshot    bool    `json:"headshot,omitempty"`
	Wallbang    bool    `json:"wallbang,omitempty"`
}

type SmokeCue struct {
	ID              string     `json:"id,omitempty"`
	Type            string     `json:"type,omitempty"`
	Round           int        `json:"round,omitempty"`
	ThrowTick       int        `json:"throw_tick,omitempty"`
	PopTick         int        `json:"pop_tick,omitempty"`
	ExpireTick      int        `json:"expire_tick,omitempty"`
	TimeSeconds     float64    `json:"time_seconds"`
	PopTimeSeconds  float64    `json:"pop_time_seconds,omitempty"`
	ThrowPlace      string     `json:"throw_place,omitempty"`
	ThrowAction     string     `json:"throw_action,omitempty"`
	Stance          string     `json:"stance,omitempty"`
	Movement        string     `json:"movement,omitempty"`
	Speed2D         float64    `json:"speed_2d,omitempty"`
	OnGround        bool       `json:"on_ground,omitempty"`
	Walking         bool       `json:"walking,omitempty"`
	Ducking         bool       `json:"ducking,omitempty"`
	Destination     string     `json:"destination,omitempty"`
	FromArea        string     `json:"from_area,omitempty"`
	Side            string     `json:"side,omitempty"`
	MatchID         string     `json:"match_id,omitempty"`
	Confidence      float64    `json:"confidence,omitempty"`
	DistanceUnits   float64    `json:"distance_units,omitempty"`
	ThrowPos        [3]float64 `json:"throw_pos,omitempty"`
	LandingPos      [3]float64 `json:"landing_pos,omitempty"`
	Matched         bool       `json:"matched,omitempty"`
	UnmatchedReason string     `json:"unmatched_reason,omitempty"`
}

type Result struct {
	Preset            string        `json:"preset"`
	RecordingResult   string        `json:"recording_result"`
	KillPlan          string        `json:"killplan,omitempty"`
	OutputDir         string        `json:"output_dir"`
	PublishDir        string        `json:"publish_dir"`
	GalleryPath       string        `json:"gallery_path"`
	SummaryPath       string        `json:"summary_path"`
	SegmentFilter     []string      `json:"segment_filter,omitempty"`
	Limit             int           `json:"limit,omitempty"`
	SkipExisting      bool          `json:"skip_existing,omitempty"`
	EffectsPath       string        `json:"effects_path,omitempty"`
	EffectsPreset     string        `json:"effects_preset,omitempty"`
	MusicPath         string        `json:"music_path,omitempty"`
	RhythmPath        string        `json:"rhythm_path,omitempty"`
	OutputFPS         int           `json:"output_fps,omitempty"`
	CompileSegments   bool          `json:"compile_segments,omitempty"`
	LineupCatalogPath string        `json:"lineup_catalog_path,omitempty"`
	UnmatchedSmokes   string        `json:"unmatched_smokes,omitempty"`
	PlayerImage       string        `json:"player_image,omitempty"`
	PlayerKeyColor    string        `json:"player_key_color,omitempty"`
	VideoCRF          int           `json:"video_crf,omitempty"`
	VideoPreset       string        `json:"video_preset,omitempty"`
	HQFilters         bool          `json:"hq_filters,omitempty"`
	AudioNormalize    bool          `json:"audio_normalize,omitempty"`
	QualityChecks     bool          `json:"quality_checks,omitempty"`
	CoverSheets       bool          `json:"cover_sheets,omitempty"`
	TemporalSmoothing bool          `json:"temporal_smoothing,omitempty"`
	CoversEnabled     bool          `json:"covers_enabled"`
	DryRun            bool          `json:"dry_run,omitempty"`
	Shorts            []ShortResult `json:"shorts"`
	Warnings          []string      `json:"warnings,omitempty"`
	Error             string        `json:"error,omitempty"`
}

type ShortResult struct {
	Index              int                         `json:"index"`
	SegmentID          string                      `json:"segment_id"`
	Preset             string                      `json:"preset,omitempty"`
	Input              string                      `json:"input"`
	Output             string                      `json:"output"`
	SourceArtifact     recording.RecordingArtifact `json:"source_artifact,omitempty"`
	PromptPath         string                      `json:"prompt_path"`
	PublishPath        string                      `json:"publish_path"`
	PlayerImage        string                      `json:"player_image,omitempty"`
	PlayerKeyColor     string                      `json:"player_key_color,omitempty"`
	MusicPath          string                      `json:"music_path,omitempty"`
	RhythmPath         string                      `json:"rhythm_path,omitempty"`
	OutputFPS          int                         `json:"output_fps,omitempty"`
	VideoCRF           int                         `json:"video_crf,omitempty"`
	VideoPreset        string                      `json:"video_preset,omitempty"`
	HQFilters          bool                        `json:"hq_filters,omitempty"`
	AudioNormalize     bool                        `json:"audio_normalize,omitempty"`
	TemporalSmoothing  bool                        `json:"temporal_smoothing,omitempty"`
	CaptionPath        string                      `json:"caption_path"`
	CoverPath          string                      `json:"cover_path,omitempty"`
	CoverSheetPath     string                      `json:"cover_sheet_path,omitempty"`
	CoverTimeSeconds   float64                     `json:"cover_time_seconds"`
	DurationSeconds    float64                     `json:"duration_seconds,omitempty"`
	Title              string                      `json:"title"`
	Headline           string                      `json:"headline"`
	Caption            string                      `json:"caption"`
	Hashtags           []string                    `json:"hashtags,omitempty"`
	SmokeCount         int                         `json:"smoke_count,omitempty"`
	PrimarySmoke       string                      `json:"primary_smoke,omitempty"`
	Smokes             []SmokeCue                  `json:"smokes,omitempty"`
	Parts              []ShortPart                 `json:"parts,omitempty"`
	Effects            []Effect                    `json:"effects,omitempty"`
	OutputArtifact     recording.RecordingArtifact `json:"output_artifact,omitempty"`
	PublishArtifact    recording.RecordingArtifact `json:"publish_artifact,omitempty"`
	CoverArtifact      recording.RecordingArtifact `json:"cover_artifact,omitempty"`
	CoverSheetArtifact recording.RecordingArtifact `json:"cover_sheet_artifact,omitempty"`
	RenderSkipped      bool                        `json:"render_skipped,omitempty"`
	CoverSkipped       bool                        `json:"cover_skipped,omitempty"`
	CoverSheetSkipped  bool                        `json:"cover_sheet_skipped,omitempty"`
	FFmpegCommand      []string                    `json:"ffmpeg_command"`
	CoverCommand       []string                    `json:"cover_command,omitempty"`
	CoverSheetCommand  []string                    `json:"cover_sheet_command,omitempty"`
	QualityCommand     []string                    `json:"quality_command,omitempty"`
	RenderLogPath      string                      `json:"render_log_path,omitempty"`
	QualityLogPath     string                      `json:"quality_log_path,omitempty"`
}

type PackManifest struct {
	Preset            string        `json:"preset"`
	RecordingResult   string        `json:"recording_result"`
	KillPlan          string        `json:"killplan,omitempty"`
	PublishDir        string        `json:"publish_dir"`
	GalleryPath       string        `json:"gallery_path"`
	SummaryPath       string        `json:"summary_path"`
	SegmentFilter     []string      `json:"segment_filter,omitempty"`
	Limit             int           `json:"limit,omitempty"`
	SkipExisting      bool          `json:"skip_existing,omitempty"`
	EffectsPath       string        `json:"effects_path,omitempty"`
	EffectsPreset     string        `json:"effects_preset,omitempty"`
	MusicPath         string        `json:"music_path,omitempty"`
	RhythmPath        string        `json:"rhythm_path,omitempty"`
	OutputFPS         int           `json:"output_fps,omitempty"`
	CompileSegments   bool          `json:"compile_segments,omitempty"`
	LineupCatalogPath string        `json:"lineup_catalog_path,omitempty"`
	UnmatchedSmokes   string        `json:"unmatched_smokes,omitempty"`
	PlayerImage       string        `json:"player_image,omitempty"`
	PlayerKeyColor    string        `json:"player_key_color,omitempty"`
	VideoCRF          int           `json:"video_crf,omitempty"`
	VideoPreset       string        `json:"video_preset,omitempty"`
	HQFilters         bool          `json:"hq_filters,omitempty"`
	AudioNormalize    bool          `json:"audio_normalize,omitempty"`
	QualityChecks     bool          `json:"quality_checks,omitempty"`
	CoverSheets       bool          `json:"cover_sheets,omitempty"`
	TemporalSmoothing bool          `json:"temporal_smoothing,omitempty"`
	CoversEnabled     bool          `json:"covers_enabled"`
	Items             []PublishItem `json:"items"`
	Warnings          []string      `json:"warnings,omitempty"`
}

type PublishItem struct {
	Index              int                         `json:"index"`
	SegmentID          string                      `json:"segment_id"`
	Preset             string                      `json:"preset,omitempty"`
	Player             string                      `json:"player"`
	Map                string                      `json:"map,omitempty"`
	KillCount          int                         `json:"kill_count"`
	PrimaryWeapon      string                      `json:"primary_weapon,omitempty"`
	SmokeCount         int                         `json:"smoke_count,omitempty"`
	PrimarySmoke       string                      `json:"primary_smoke,omitempty"`
	Source             string                      `json:"source"`
	Video              string                      `json:"video"`
	SourceArtifact     recording.RecordingArtifact `json:"source_artifact,omitempty"`
	PlayerImage        string                      `json:"player_image,omitempty"`
	MusicPath          string                      `json:"music_path,omitempty"`
	RhythmPath         string                      `json:"rhythm_path,omitempty"`
	OutputFPS          int                         `json:"output_fps,omitempty"`
	VideoCRF           int                         `json:"video_crf,omitempty"`
	VideoPreset        string                      `json:"video_preset,omitempty"`
	HQFilters          bool                        `json:"hq_filters,omitempty"`
	AudioNormalize     bool                        `json:"audio_normalize,omitempty"`
	TemporalSmoothing  bool                        `json:"temporal_smoothing,omitempty"`
	CaptionPath        string                      `json:"caption_path"`
	CoverPath          string                      `json:"cover_path,omitempty"`
	CoverSheetPath     string                      `json:"cover_sheet_path,omitempty"`
	CoverTimeSeconds   float64                     `json:"cover_time_seconds"`
	Title              string                      `json:"title"`
	Headline           string                      `json:"headline"`
	Caption            string                      `json:"caption"`
	Hashtags           []string                    `json:"hashtags,omitempty"`
	Effects            []Effect                    `json:"effects,omitempty"`
	Smokes             []SmokeCue                  `json:"smokes,omitempty"`
	Parts              []ShortPart                 `json:"parts,omitempty"`
	DurationSeconds    float64                     `json:"duration_seconds,omitempty"`
	Artifact           recording.RecordingArtifact `json:"artifact,omitempty"`
	CoverArtifact      recording.RecordingArtifact `json:"cover_artifact,omitempty"`
	CoverSheetArtifact recording.RecordingArtifact `json:"cover_sheet_artifact,omitempty"`
}

type EffectType string

const (
	EffectZoom     EffectType = "zoom"
	EffectFlash    EffectType = "flash"
	EffectText     EffectType = "text"
	EffectGrade    EffectType = "grade"
	EffectImage    EffectType = "image"
	EffectKillfeed EffectType = "killfeed"
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
	Path               string     `json:"path,omitempty"`
	X                  string     `json:"x,omitempty"`
	Y                  string     `json:"y,omitempty"`
	Width              int        `json:"width,omitempty"`
	Height             int        `json:"height,omitempty"`
	CropX              int        `json:"crop_x,omitempty"`
	CropY              int        `json:"crop_y,omitempty"`
	CropWidth          int        `json:"crop_width,omitempty"`
	CropHeight         int        `json:"crop_height,omitempty"`
	Size               int        `json:"size,omitempty"`
	FontFile           string     `json:"fontfile,omitempty"`
	FontColor          string     `json:"font_color,omitempty"`
	BoxColor           string     `json:"box_color,omitempty"`
	BoxBorder          int        `json:"box_border,omitempty"`
	ShadowColor        string     `json:"shadow_color,omitempty"`
	ShadowX            int        `json:"shadow_x,omitempty"`
	ShadowY            int        `json:"shadow_y,omitempty"`
	FadeInSeconds      float64    `json:"fade_in_seconds,omitempty"`
	FadeOutSeconds     float64    `json:"fade_out_seconds,omitempty"`
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
	SourceSmokeID      string     `json:"source_smoke_id,omitempty"`
	SourceSmokeType    string     `json:"source_smoke_type,omitempty"`
	SourceSmokeTarget  string     `json:"source_smoke_target,omitempty"`
}
