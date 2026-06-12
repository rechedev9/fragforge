package renderplan

import (
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/editor"
)

func TestLoadoutForVariantDerivesFromPresetRegistry(t *testing.T) {
	for _, variant := range []string{editor.PresetViral60Clean} {
		t.Run(variant, func(t *testing.T) {
			got, err := LoadoutForVariant(variant)
			if err != nil {
				t.Fatalf("LoadoutForVariant error = %v", err)
			}
			if got.Variant != variant || got.Preset == "" {
				t.Fatalf("loadout = %#v", got)
			}
			if got.Framing != "full-ui" {
				t.Fatalf("framing = %q, want full-ui", got.Framing)
			}
			if got.UploadReadyDir != "shortslistosparasubir" {
				t.Fatalf("upload ready dir = %q", got.UploadReadyDir)
			}
			if got.VideoCRF == 0 || got.VideoPreset == "" || got.Output.Width != 1080 || got.Output.Height != 1920 || got.Output.FPS != 60 {
				t.Fatalf("quality/output loadout = %#v", got)
			}
		})
	}
}

func TestLoadoutForVariantRejectsUnknownVariant(t *testing.T) {
	_, err := LoadoutForVariant("custom")
	if err == nil {
		t.Fatal("LoadoutForVariant error = nil, want error")
	}
	if !strings.Contains(err.Error(), editor.PresetViral60Clean) {
		t.Fatalf("error %q should list valid presets", err)
	}
	for _, removed := range []string{"natural-hq2-full", "smoke-lineups"} {
		if strings.Contains(err.Error(), removed) {
			t.Fatalf("error %q listed removed preset %q", err, removed)
		}
	}
}

func TestLoadoutCatalogListsEveryPresetWithViral60CleanFirst(t *testing.T) {
	got := LoadoutCatalog()
	names := editor.PresetNames()
	if len(got) != len(names) {
		t.Fatalf("catalog has %d loadouts, want %d", len(got), len(names))
	}
	for i, name := range names {
		if got[i].Variant != name {
			t.Fatalf("catalog[%d].Variant = %q, want %q", i, got[i].Variant, name)
		}
	}
	if got[0].Variant != editor.PresetViral60Clean {
		t.Fatalf("catalog default = %q, want %q", got[0].Variant, editor.PresetViral60Clean)
	}
}
