package editor

import (
	"fmt"
	"strings"
)

// FilterKind values select how a preset builds its 9:16 filtergraph.
const (
	FilterKindCropCenter   = "crop-center"
	FilterKindFullFrame    = "full-frame"
	FilterKindViralSquare  = "viral-square"
	FilterKindSmokeLineups = "smoke-lineups"
)

const (
	// PresetViral60 is the default render preset: the natural-hq2-full
	// full-UI 60fps base plus the aggressive viral-ultra overlay pack
	// (cold-open hook text, kill punch-ins, kill counter, milestone labels).
	PresetViral60 = "viral-60"

	// PresetViralBeatsync is viral-60 for beat-synced montages: it requires
	// music, a rhythm analysis json, and compile-segments so cuts land on
	// the detected beat grid.
	PresetViralBeatsync = "viral-beatsync"

	// PresetViral60Clean is viral-60 recorded without the gameplay HUD: a
	// clean POV where only kill notices appear, plus the viral overlay pack.
	PresetViral60Clean = "viral-60-clean"
)

// RenderPreset is one declarative entry in the render preset registry.
// Adding a preset means adding one entry to renderPresets.
type RenderPreset struct {
	Name        string
	Description string

	// Output geometry. Every ZackVideo short renders at 1080x1920 / 60fps.
	FPS    int
	Width  int
	Height int

	// Encoder defaults used when the caller does not override them.
	VideoCRF    int
	VideoPreset string

	// EffectsPreset names the default scripted-effects preset; EffectsPath
	// optionally points at an external Lua script instead. Both apply only
	// when the caller supplies neither an effects path nor an effects preset.
	EffectsPreset string
	EffectsPath   string

	// FilterKind is one of the FilterKind* constants.
	FilterKind string

	HQFilters         bool
	AudioNormalize    bool
	QualityChecks     bool
	CoverSheets       bool
	TemporalSmoothing bool

	// AccurateScaling upgrades lanczos scaling with accurate rounding.
	AccurateScaling bool
	// MasteringBT709 adds high-profile encode arguments and BT.709 color
	// metadata to render commands.
	MasteringBT709 bool
	// RhythmSync marks presets that require beat-synced compilation inputs
	// (music path, rhythm json, compile-segments).
	RhythmSync bool

	// HUDMode is the recording-stage HUD hint passed to zv-recorder --hud.
	// Empty means the recorder default (full gameplay HUD). The render
	// stage never reads it; it only travels through `zv short`.
	HUDMode string

	// Grade is the FFmpeg-only base color grade applied by full-frame
	// presets. The zero value means no grading.
	Grade struct {
		Saturation float64
		Contrast   float64
		Gamma      float64
		Unsharp    bool
	}
}

// presetGrade aliases the anonymous RenderPreset.Grade type so registry
// literals and grade helpers stay readable.
type presetGrade = struct {
	Saturation float64
	Contrast   float64
	Gamma      float64
	Unsharp    bool
}

// renderPresets is the single source of preset knowledge: encoder defaults,
// filtergraph layout, default effects, feature flags, and grading.
var renderPresets = []RenderPreset{
	{
		Name:           PresetViral60,
		Description:    "default viral edit: full-UI 60fps gameplay with hook text, punch-ins, and kill counter overlays",
		FPS:            60,
		Width:          1080,
		Height:         1920,
		VideoCRF:       NaturalHQVideoCRF,
		VideoPreset:    NaturalHQVideoPreset,
		EffectsPreset:  EffectsPresetViralUltra,
		FilterKind:     FilterKindFullFrame,
		HQFilters:      true,
		AudioNormalize: true,
		QualityChecks:  true,
		CoverSheets:    true,
	},
	{
		Name:           PresetViralBeatsync,
		Description:    "viral-60 for montages with cuts on the detected beat grid; requires music, rhythm json, and compile-segments",
		FPS:            60,
		Width:          1080,
		Height:         1920,
		VideoCRF:       NaturalHQVideoCRF,
		VideoPreset:    NaturalHQVideoPreset,
		EffectsPreset:  EffectsPresetViralUltra,
		FilterKind:     FilterKindFullFrame,
		HQFilters:      true,
		AudioNormalize: true,
		QualityChecks:  true,
		CoverSheets:    true,
		RhythmSync:     true,
	},
	{
		Name:           PresetViral60Clean,
		Description:    "viral-60 on a clean HUD-less POV; only kill notices appear when kills happen",
		FPS:            60,
		Width:          1080,
		Height:         1920,
		VideoCRF:       NaturalHQVideoCRF,
		VideoPreset:    NaturalHQVideoPreset,
		EffectsPreset:  EffectsPresetViralUltraClean,
		FilterKind:     FilterKindFullFrame,
		HQFilters:      true,
		AudioNormalize: true,
		QualityChecks:  true,
		CoverSheets:    true,
		HUDMode:        "deathnotices",
	},
	{
		Name:          PresetShortClean,
		Description:   "restrained labels, vertical POV crop, and subtle kill punch-ins",
		FPS:           60,
		Width:         1080,
		Height:        1920,
		VideoCRF:      DefaultVideoCRF,
		VideoPreset:   DefaultVideoPreset,
		EffectsPreset: EffectsPresetBuiltinClean,
		FilterKind:    FilterKindCropCenter,
	},
	{
		Name:          PresetShortPremiumPlayer,
		Description:   "short-clean base plus a player cutout overlay and larger headline",
		FPS:           60,
		Width:         1080,
		Height:        1920,
		VideoCRF:      DefaultVideoCRF,
		VideoPreset:   DefaultVideoPreset,
		EffectsPreset: EffectsPresetBuiltinClean,
		FilterKind:    FilterKindCropCenter,
	},
	{
		Name:           PresetShortViralSquare,
		Description:    "blurred vertical background with centered square gameplay for top/bottom copy",
		FPS:            60,
		Width:          1080,
		Height:         1920,
		VideoCRF:       NaturalHQVideoCRF,
		VideoPreset:    NaturalHQVideoPreset,
		EffectsPreset:  EffectsPresetBuiltinClean,
		FilterKind:     FilterKindViralSquare,
		HQFilters:      true,
		AudioNormalize: true,
		QualityChecks:  true,
		CoverSheets:    true,
	},
	{
		Name:          PresetShortNaturalHQ,
		Description:   "unmodified gameplay at higher encode quality for clean local masters",
		FPS:           60,
		Width:         1080,
		Height:        1920,
		VideoCRF:      NaturalHQVideoCRF,
		VideoPreset:   NaturalHQVideoPreset,
		EffectsPreset: EffectsPresetNone,
		FilterKind:    FilterKindCropCenter,
	},
	{
		Name:           PresetShortNaturalHQ2,
		Description:    "natural-hq plus FFmpeg quality-of-life checks and contact sheets",
		FPS:            60,
		Width:          1080,
		Height:         1920,
		VideoCRF:       NaturalHQVideoCRF,
		VideoPreset:    NaturalHQVideoPreset,
		EffectsPreset:  EffectsPresetNone,
		FilterKind:     FilterKindCropCenter,
		HQFilters:      true,
		AudioNormalize: true,
		QualityChecks:  true,
		CoverSheets:    true,
	},
	{
		Name:           PresetShortNaturalHQ2Full,
		Description:    "continuous full-UI 9:16 gameplay crop with a mild saturation lift and no scripted effects",
		FPS:            60,
		Width:          1080,
		Height:         1920,
		VideoCRF:       NaturalHQVideoCRF,
		VideoPreset:    NaturalHQVideoPreset,
		EffectsPreset:  EffectsPresetNone,
		FilterKind:     FilterKindFullFrame,
		HQFilters:      true,
		AudioNormalize: true,
		QualityChecks:  true,
		CoverSheets:    true,
		Grade:          presetGrade{Saturation: 1.12},
	},
	{
		Name:            PresetShortNaturalHQ2FullPlus,
		Description:     "experimental full-frame variant with stronger FFmpeg-only color, sharpening, and mastering settings",
		FPS:             60,
		Width:           1080,
		Height:          1920,
		VideoCRF:        NaturalHQ2FullPlusVideoCRF,
		VideoPreset:     NaturalHQ2FullPlusVideoPreset,
		EffectsPreset:   EffectsPresetNone,
		FilterKind:      FilterKindFullFrame,
		HQFilters:       true,
		AudioNormalize:  true,
		QualityChecks:   true,
		CoverSheets:     true,
		AccurateScaling: true,
		MasteringBT709:  true,
		Grade:           presetGrade{Saturation: 1.16, Contrast: 1.02, Gamma: 1.00, Unsharp: true},
	},
	{
		Name:            PresetShortNaturalHQ3,
		Description:     "experimental hq2 variant with higher encode settings and strict playback/color metadata",
		FPS:             60,
		Width:           1080,
		Height:          1920,
		VideoCRF:        NaturalHQ3VideoCRF,
		VideoPreset:     NaturalHQ3VideoPreset,
		EffectsPreset:   EffectsPresetNone,
		FilterKind:      FilterKindCropCenter,
		HQFilters:       true,
		AudioNormalize:  true,
		QualityChecks:   true,
		CoverSheets:     true,
		AccurateScaling: true,
		MasteringBT709:  true,
	},
	{
		Name:              PresetShortNaturalHQ3Smooth,
		Description:       "natural-hq3 comparison with subtle temporal blending at a 60fps upload target",
		FPS:               60,
		Width:             1080,
		Height:            1920,
		VideoCRF:          NaturalHQ3VideoCRF,
		VideoPreset:       NaturalHQ3VideoPreset,
		EffectsPreset:     EffectsPresetNone,
		FilterKind:        FilterKindCropCenter,
		HQFilters:         true,
		AudioNormalize:    true,
		QualityChecks:     true,
		CoverSheets:       true,
		TemporalSmoothing: true,
		AccurateScaling:   true,
		MasteringBT709:    true,
	},
	{
		Name:           PresetSmokeLineups,
		Description:    "natural-hq2 visual baseline plus educational overlays and slow motion for utility throws",
		FPS:            60,
		Width:          1080,
		Height:         1920,
		VideoCRF:       NaturalHQVideoCRF,
		VideoPreset:    NaturalHQVideoPreset,
		EffectsPreset:  EffectsPresetSmokeLineups,
		FilterKind:     FilterKindSmokeLineups,
		HQFilters:      true,
		AudioNormalize: true,
		QualityChecks:  true,
		CoverSheets:    true,
	},
}

var renderPresetByName = buildRenderPresetIndex()

func buildRenderPresetIndex() map[string]RenderPreset {
	index := make(map[string]RenderPreset, len(renderPresets))
	for _, preset := range renderPresets {
		index[preset.Name] = preset
	}
	return index
}

// PresetByName returns the registry entry for name.
func PresetByName(name string) (RenderPreset, bool) {
	preset, ok := renderPresetByName[name]
	return preset, ok
}

// PresetNames returns all registered preset names in stable registry order.
func PresetNames() []string {
	names := make([]string, 0, len(renderPresets))
	for _, preset := range renderPresets {
		names = append(names, preset.Name)
	}
	return names
}

// DefaultPreset returns the product default render preset (viral-60).
func DefaultPreset() RenderPreset {
	preset, _ := PresetByName(PresetViral60)
	return preset
}

// presetFilterKind resolves the filtergraph layout for a preset name. Unknown
// or empty names keep the historical centered-crop layout so filter helpers
// stay usable on bare ShortEdit values.
func presetFilterKind(name string) string {
	if preset, ok := PresetByName(name); ok {
		return preset.FilterKind
	}
	return FilterKindCropCenter
}

func unknownPresetError(name string) error {
	return fmt.Errorf("unknown preset %q (valid presets: %s)", name, strings.Join(PresetNames(), ", "))
}
