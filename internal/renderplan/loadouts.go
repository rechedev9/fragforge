package renderplan

import (
	"fmt"
	"strings"

	"github.com/rechedev9/fragforge/internal/editor"
)

type Loadout struct {
	Variant         string      `json:"variant"`
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
	UploadReadyDir  string      `json:"upload_ready_dir"`
}

type OutputShape struct {
	AspectRatio string `json:"aspect_ratio"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	FPS         int    `json:"fps"`
	Container   string `json:"container"`
	VideoCodec  string `json:"video_codec"`
	AudioCodec  string `json:"audio_codec"`
}

// LoadoutCatalog lists one loadout per registered render preset, in registry
// order. The first entry is the product default (viral-60-clean).
func LoadoutCatalog() []Loadout {
	names := editor.PresetNames()
	out := make([]Loadout, 0, len(names))
	for _, name := range names {
		loadout, err := LoadoutForVariant(name)
		if err != nil {
			continue
		}
		out = append(out, loadout)
	}
	return out
}

// LoadoutForVariant derives the loadout for a render variant from the editor
// preset registry, so adding a preset there is enough to expose it here.
func LoadoutForVariant(variant string) (Loadout, error) {
	preset, ok := editor.PresetByName(variant)
	if !ok {
		return Loadout{}, fmt.Errorf("unknown render variant %q (valid presets: %s)", variant, strings.Join(editor.PresetNames(), ", "))
	}
	return Loadout{
		Variant:         preset.Name,
		Preset:          preset.Name,
		EffectsPreset:   preset.EffectsPreset,
		Framing:         loadoutFraming(preset.FilterKind),
		VideoCRF:        preset.VideoCRF,
		VideoPreset:     preset.VideoPreset,
		HQFilters:       preset.HQFilters,
		AudioNormalize:  preset.AudioNormalize,
		QualityChecks:   preset.QualityChecks,
		CoverSheets:     preset.CoverSheets,
		CoversEnabled:   true,
		CaptionsEnabled: true,
		Output:          presetOutput(preset),
		UploadReadyDir:  "shortslistosparasubir",
	}, nil
}

func loadoutFraming(filterKind string) string {
	switch filterKind {
	case editor.FilterKindFullFrame:
		return "full-ui"
	default:
		return "center-crop"
	}
}

func presetOutput(preset editor.RenderPreset) OutputShape {
	return OutputShape{
		AspectRatio: "9:16",
		Width:       preset.Width,
		Height:      preset.Height,
		FPS:         preset.FPS,
		Container:   "mp4",
		VideoCodec:  "h264",
		AudioCodec:  "aac",
	}
}
