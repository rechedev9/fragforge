package editor

import (
	"fmt"
	"strings"
)

const (
	// PresetViral60Clean is the product default render preset: a HUD-less POV
	// that keeps the in-game kill feed.
	PresetViral60Clean = "viral-60-clean"

	// PresetCleanPOV60 is a fully HUD-less first-person POV (no HUD, no kill
	// feed) for the most cinematic edit.
	PresetCleanPOV60 = "clean-pov-60"

	// PresetFullHUD60 keeps the full in-game CS2 HUD visible over the edit.
	PresetFullHUD60 = "full-hud-60"
)

// RenderPreset is one declarative entry in the render preset registry.
// Adding a preset means adding one entry to renderPresets.
type RenderPreset struct {
	Name string
	// Label is the short, user-facing name shown in the UI preset picker.
	Label       string
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

	HQFilters         bool
	AudioNormalize    bool
	QualityChecks     bool
	CoverSheets       bool
	TemporalSmoothing bool

	// KillfeedSource reports whether the preset's capture shows an in-game
	// killfeed, i.e. whether there is a death-notice region worth cropping and
	// re-overlaying into the 9:16 frame.
	KillfeedSource bool

	// HUDMode is the recording-stage HUD hint passed to zv-recorder --hud.
	// Empty means the recorder default (full gameplay HUD). The render
	// stage never reads it; it only travels through `zv short`.
	HUDMode string
}

// renderPresets is the single source of preset knowledge: encoder defaults,
// filtergraph layout, default effects, feature flags, and grading. The first
// entry is the product default.
// renderPresets share the same proven vertical render path (effects, encoder,
// feature flags, 1080x1920/60fps); they differ only in the recording-stage
// HUDMode, which is what the user actually sees as "Kill Feed" vs "Clean POV"
// vs "Full HUD". The render stage never reads HUDMode (see RenderPreset.HUDMode);
// it travels to zv-recorder --hud at record time.
var renderPresets = []RenderPreset{
	{
		Name:           PresetViral60Clean,
		Label:          "Kill Feed",
		Description:    "default clean viral edit: HUD-less 60fps POV that keeps the in-game kill feed, with punch-in kills",
		FPS:            60,
		Width:          1080,
		Height:         1920,
		VideoCRF:       StandardVideoCRF,
		VideoPreset:    StandardVideoPreset,
		EffectsPreset:  EffectsPresetViralUltraClean,
		HQFilters:      true,
		AudioNormalize: true,
		QualityChecks:  true,
		CoverSheets:    true,
		KillfeedSource: true,
		HUDMode:        "deathnotices",
	},
	{
		Name:           PresetCleanPOV60,
		Label:          "Clean POV",
		Description:    "fully HUD-less first-person POV: cinematic punch-in kills, no in-game HUD or kill feed",
		FPS:            60,
		Width:          1080,
		Height:         1920,
		VideoCRF:       StandardVideoCRF,
		VideoPreset:    StandardVideoPreset,
		EffectsPreset:  EffectsPresetViralUltraClean,
		HQFilters:      true,
		AudioNormalize: true,
		QualityChecks:  true,
		CoverSheets:    true,
		HUDMode:        "clean",
	},
	{
		Name:           PresetFullHUD60,
		Label:          "Full HUD",
		Description:    "full in-game HUD POV: keeps the CS2 HUD, health, ammo, and radar visible over the viral edit",
		FPS:            60,
		Width:          1080,
		Height:         1920,
		VideoCRF:       StandardVideoCRF,
		VideoPreset:    StandardVideoPreset,
		EffectsPreset:  EffectsPresetViralUltraClean,
		HQFilters:      true,
		AudioNormalize: true,
		QualityChecks:  true,
		CoverSheets:    true,
		KillfeedSource: true,
		HUDMode:        "gameplay",
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

// presetUsesFullFrame reports whether a preset uses the production full-frame
// vertical layout. Unknown or empty names keep the historical centered-crop
// layout so filter helpers stay usable on bare ShortEdit values.
func presetUsesFullFrame(name string) bool {
	_, ok := PresetByName(name)
	return ok
}

func unknownPresetError(name string) error {
	return fmt.Errorf("unknown preset %q (valid presets: %s)", name, strings.Join(PresetNames(), ", "))
}
