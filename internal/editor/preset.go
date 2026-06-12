package editor

import (
	"fmt"
	"strings"
)

// FilterKind values select how a preset builds its 9:16 filtergraph.
const (
	FilterKindCropCenter = "crop-center"
	FilterKindFullFrame  = "full-frame"
)

const (
	// PresetViral60Clean is the sole registered render preset.
	PresetViral60Clean = "viral-60-clean"
)

// RenderPreset is one declarative entry in the render preset registry.
// Adding a preset means adding one entry to renderPresets.
type RenderPreset struct {
	Name        string
	Description string

	// Output geometry. Every FragForge short renders at 1080x1920 / 60fps.
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

	// HUDMode is the recording-stage HUD hint passed to zv-recorder --hud.
	// Empty means the recorder default (full gameplay HUD). The render
	// stage never reads it; it only travels through `zv short`.
	HUDMode string
}

// renderPresets is the single source of preset knowledge: encoder defaults,
// filtergraph layout, default effects, feature flags, and grading. The first
// entry is the product default.
var renderPresets = []RenderPreset{
	{
		Name:           PresetViral60Clean,
		Description:    "default clean viral edit: HUD-less 60fps POV with kill notices, punch-ins, and kill counter overlays",
		FPS:            60,
		Width:          1080,
		Height:         1920,
		VideoCRF:       StandardVideoCRF,
		VideoPreset:    StandardVideoPreset,
		EffectsPreset:  EffectsPresetViralUltraClean,
		FilterKind:     FilterKindFullFrame,
		HQFilters:      true,
		AudioNormalize: true,
		QualityChecks:  true,
		CoverSheets:    true,
		HUDMode:        "deathnotices",
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

// DefaultPreset returns the product default render preset (viral-60-clean).
func DefaultPreset() RenderPreset {
	preset, _ := PresetByName(PresetViral60Clean)
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
