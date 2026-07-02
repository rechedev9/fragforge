package streamclips

import (
	"fmt"
	"strings"
)

const (
	// VariantStreamer4060 is the product default layout variant: a 40%
	// facecam band over a 60% gameplay band, both at full 1080 width.
	VariantStreamer4060 = "streamer-vertical-stack-40-60"

	// VariantStreamerFullframeNoCam drops the facecam band entirely and
	// fills the whole 1080x1920 frame with the gameplay crop.
	VariantStreamerFullframeNoCam = "streamer-fullframe-nocam"
)

// LayoutVariant is one declarative entry in the layout variant registry: the
// facecam/gameplay split geometry, default crops, and whether the layout
// uses a facecam band at all.
type LayoutVariant struct {
	Name string
	// Label is the short, user-facing name shown in a layout picker.
	Label       string
	Description string

	// FullFrame reports whether this variant has no facecam band: the
	// gameplay crop fills the entire 1080x1920 output on its own and
	// FaceCrop is ignored (Validate does not require it).
	FullFrame bool

	// Output geometry. OutputWidth is shared by both bands. For a
	// FullFrame variant only OutputWidth/GameOutputHeight are used, and
	// GameOutputHeight is the full frame height (1920).
	OutputWidth      int
	FaceOutputHeight int
	GameOutputHeight int

	// DefaultFaceCrop and DefaultGameplayCrop seed DefaultEditPlan when
	// this variant is the registry default. DefaultFaceCrop is unused for
	// FullFrame variants.
	DefaultFaceCrop     CropRect
	DefaultGameplayCrop CropRect
}

// layoutVariants is the single source of layout variant knowledge: split
// geometry, default crops, and the facecam/no-facecam distinction. The first
// entry is the product default.
var layoutVariants = []LayoutVariant{
	{
		Name:                VariantStreamer4060,
		Label:               "Facecam 40 / Gameplay 60",
		Description:         "default vertical stack: a 40% facecam band over a 60% gameplay band, both full width",
		OutputWidth:         1080,
		FaceOutputHeight:    768,
		GameOutputHeight:    1152,
		DefaultFaceCrop:     CropRect{X: 0, Y: 0, Width: 0.25, Height: 0.30},
		DefaultGameplayCrop: CropRect{X: 0, Y: 0, Width: 1, Height: 1},
	},
	{
		Name:                VariantStreamerVerticalStack,
		Label:               "Facecam 35 / Gameplay 65 (legacy)",
		Description:         "legacy vertical stack: a 35% facecam band over a 65% gameplay band, both full width",
		OutputWidth:         1080,
		FaceOutputHeight:    520,
		GameOutputHeight:    1400,
		DefaultFaceCrop:     CropRect{X: 0, Y: 0, Width: 1, Height: 0.35},
		DefaultGameplayCrop: CropRect{X: 0, Y: 0.35, Width: 1, Height: 0.65},
	},
	{
		Name:                VariantStreamerFullframeNoCam,
		Label:               "Full Frame (no facecam)",
		Description:         "no facecam band: the gameplay crop fills the whole 1080x1920 frame",
		FullFrame:           true,
		OutputWidth:         1080,
		GameOutputHeight:    1920,
		DefaultGameplayCrop: CropRect{X: 0, Y: 0, Width: 1, Height: 1},
	},
}

var layoutVariantByName = buildLayoutVariantIndex()

func buildLayoutVariantIndex() map[string]LayoutVariant {
	index := make(map[string]LayoutVariant, len(layoutVariants))
	for _, variant := range layoutVariants {
		index[variant.Name] = variant
	}
	return index
}

// VariantByName returns the registry entry for name.
func VariantByName(name string) (LayoutVariant, bool) {
	variant, ok := layoutVariantByName[name]
	return variant, ok
}

// VariantNames returns all registered variant names in stable registry order.
func VariantNames() []string {
	names := make([]string, 0, len(layoutVariants))
	for _, variant := range layoutVariants {
		names = append(names, variant.Name)
	}
	return names
}

// DefaultVariant returns the product default layout variant
// (streamer-vertical-stack-40-60).
func DefaultVariant() LayoutVariant {
	variant, _ := VariantByName(VariantStreamer4060)
	return variant
}

func unknownVariantError(name string) error {
	return fmt.Errorf("unsupported stream render variant %q (valid variants: %s)", name, strings.Join(VariantNames(), ", "))
}
