// Package editor builds local 9:16 short edits from recorder segment clips.
package editor

import (
	"image"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/recording"
)

const (
	// EffectsPresetViralUltraClean is the effects pack bundled into the sole
	// public render preset, viral-60-clean.
	EffectsPresetViralUltraClean = "viral-ultra-clean"

	// EffectsPresetExternal marks manifests rendered from a user script path.
	EffectsPresetExternal = "external"
)

const (
	// DefaultVideoCRF keeps the historical editor quality/speed tradeoff.
	DefaultVideoCRF = 18

	// DefaultVideoPreset keeps the historical x264 speed setting.
	DefaultVideoPreset = "fast"

	// StandardVideoCRF is the quality setting for viral-60-clean.
	StandardVideoCRF = 16

	// StandardVideoPreset is the x264 speed/quality setting for viral-60-clean.
	StandardVideoPreset = "slow"
)

const (
	OutputFormatShort9x16     = "short-9x16"
	OutputFormatLandscape16x9 = "landscape-16x9"

	KillEffectClean       = "clean"
	KillEffectPunchIn     = "punch-in"
	KillEffectVelocity    = "velocity"
	KillEffectFreezeFlash = "freeze-flash"

	TransitionCut   = "cut"
	TransitionFlash = "flash"
	TransitionWhip  = "whip"
	TransitionDip   = "dip"
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
	OutputFormat        string
	KillEffect          string
	Transition          string
	Intro               bool
	Outro               bool
	// IntroText and OutroText customize the intro/outro overlay card text;
	// empty falls back to the generated headline (intro) or "FragForge"
	// (outro). Neither auto-enables its bookend.
	IntroText string
	OutroText string
	// HookText draws the generated headline over the first ~2s of each short.
	HookText bool
	// KillCounter pops a running kill count (with a 2K/3K/4K/ACE milestone on
	// the final kill) at each kill cue.
	KillCounter bool
	// KillfeedOverlay re-overlays the source's kill notices near the top of
	// the frame, so the killfeed survives the 9:16 center crop. Only applies
	// to presets whose capture actually shows a killfeed.
	KillfeedOverlay bool
	// TailTrimSeconds ends each kill clip this many seconds after its final
	// kill, cutting the recorded quit-tick dead air. Zero disables trimming.
	TailTrimSeconds   float64
	OutputFPS         int
	CompileSegments   bool
	LineupCatalogPath string
	SegmentIDs        []string
	Limit             int
	VideoCRF          int
	VideoPreset       string
	HQFilters         bool
	AudioNormalize    bool
	QualityChecks     bool
	CoverSheets       bool
	TemporalSmoothing bool
	FFmpegPath        string
	FFprobePath       string
	DisableCovers     bool
	SkipExisting      bool
	DryRun            bool
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
	OutputFormat        string
	KillEffect          string
	Transition          string
	Intro               bool
	Outro               bool
	IntroText           string
	OutroText           string
	HookText            bool
	KillCounter         bool
	KillfeedOverlay     bool
	TailTrimSeconds     float64
	OutputFPS           int
	CompileSegments     bool
	LineupCatalogPath   string
	SegmentIDs          []string
	Limit               int
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
	OutputFormat      string      `json:"output_format,omitempty"`
	KillEffect        string      `json:"kill_effect,omitempty"`
	Transition        string      `json:"transition,omitempty"`
	Intro             bool        `json:"intro,omitempty"`
	Outro             bool        `json:"outro,omitempty"`
	IntroText         string      `json:"intro_text,omitempty"`
	OutroText         string      `json:"outro_text,omitempty"`
	HookText          bool        `json:"hook_text,omitempty"`
	KillCounter       bool        `json:"kill_counter,omitempty"`
	KillfeedOverlay   bool        `json:"killfeed_overlay,omitempty"`
	TailTrimSeconds   float64     `json:"tail_trim_seconds,omitempty"`
	OutputFPS         int         `json:"output_fps,omitempty"`
	CompileSegments   bool        `json:"compile_segments,omitempty"`
	LineupCatalogPath string      `json:"lineup_catalog_path,omitempty"`
	UnmatchedSmokes   string      `json:"unmatched_smokes,omitempty"`
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
	MusicPath         string                      `json:"music_path,omitempty"`
	RhythmPath        string                      `json:"rhythm_path,omitempty"`
	OutputFormat      string                      `json:"output_format,omitempty"`
	KillEffect        string                      `json:"kill_effect,omitempty"`
	Transition        string                      `json:"transition,omitempty"`
	Intro             bool                        `json:"intro,omitempty"`
	Outro             bool                        `json:"outro,omitempty"`
	IntroText         string                      `json:"intro_text,omitempty"`
	OutroText         string                      `json:"outro_text,omitempty"`
	HookText          bool                        `json:"hook_text,omitempty"`
	KillCounter       bool                        `json:"kill_counter,omitempty"`
	KillfeedOverlay   bool                        `json:"killfeed_overlay,omitempty"`
	TailTrimSeconds   float64                     `json:"tail_trim_seconds,omitempty"`
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
	OutputFormat      string        `json:"output_format,omitempty"`
	KillEffect        string        `json:"kill_effect,omitempty"`
	Transition        string        `json:"transition,omitempty"`
	Intro             bool          `json:"intro,omitempty"`
	Outro             bool          `json:"outro,omitempty"`
	IntroText         string        `json:"intro_text,omitempty"`
	OutroText         string        `json:"outro_text,omitempty"`
	HookText          bool          `json:"hook_text,omitempty"`
	KillCounter       bool          `json:"kill_counter,omitempty"`
	KillfeedOverlay   bool          `json:"killfeed_overlay,omitempty"`
	TailTrimSeconds   float64       `json:"tail_trim_seconds,omitempty"`
	OutputFPS         int           `json:"output_fps,omitempty"`
	CompileSegments   bool          `json:"compile_segments,omitempty"`
	LineupCatalogPath string        `json:"lineup_catalog_path,omitempty"`
	UnmatchedSmokes   string        `json:"unmatched_smokes,omitempty"`
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
	MusicPath          string                      `json:"music_path,omitempty"`
	RhythmPath         string                      `json:"rhythm_path,omitempty"`
	OutputFormat       string                      `json:"output_format,omitempty"`
	KillEffect         string                      `json:"kill_effect,omitempty"`
	Transition         string                      `json:"transition,omitempty"`
	Intro              bool                        `json:"intro,omitempty"`
	Outro              bool                        `json:"outro,omitempty"`
	IntroText          string                      `json:"intro_text,omitempty"`
	OutroText          string                      `json:"outro_text,omitempty"`
	HookText           bool                        `json:"hook_text,omitempty"`
	KillCounter        bool                        `json:"kill_counter,omitempty"`
	KillfeedOverlay    bool                        `json:"killfeed_overlay,omitempty"`
	TailTrimSeconds    float64                     `json:"tail_trim_seconds,omitempty"`
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
	OutputFormat      string        `json:"output_format,omitempty"`
	KillEffect        string        `json:"kill_effect,omitempty"`
	Transition        string        `json:"transition,omitempty"`
	Intro             bool          `json:"intro,omitempty"`
	Outro             bool          `json:"outro,omitempty"`
	OutputFPS         int           `json:"output_fps,omitempty"`
	CompileSegments   bool          `json:"compile_segments,omitempty"`
	LineupCatalogPath string        `json:"lineup_catalog_path,omitempty"`
	UnmatchedSmokes   string        `json:"unmatched_smokes,omitempty"`
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
	MusicPath          string                      `json:"music_path,omitempty"`
	RhythmPath         string                      `json:"rhythm_path,omitempty"`
	OutputFormat       string                      `json:"output_format,omitempty"`
	KillEffect         string                      `json:"kill_effect,omitempty"`
	Transition         string                      `json:"transition,omitempty"`
	Intro              bool                        `json:"intro,omitempty"`
	Outro              bool                        `json:"outro,omitempty"`
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
	Bold               bool       `json:"bold,omitempty"`
	BorderWidth        int        `json:"border_width,omitempty"`
	BorderColor        string     `json:"border_color,omitempty"`
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
