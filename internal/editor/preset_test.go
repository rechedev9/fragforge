package editor

import (
	"strings"
	"testing"
)

func TestPresetNamesAllResolve(t *testing.T) {
	names := PresetNames()
	if len(names) == 0 {
		t.Fatal("PresetNames returned no presets")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			preset, ok := PresetByName(name)
			if !ok {
				t.Fatalf("PresetByName(%q) ok = false, want true", name)
			}
			if preset.Name != name {
				t.Fatalf("preset name = %q, want %q", preset.Name, name)
			}
		})
	}
}

func TestAllPresetsRenderVertical1080x1920At60FPS(t *testing.T) {
	for _, name := range PresetNames() {
		t.Run(name, func(t *testing.T) {
			preset, _ := PresetByName(name)
			if preset.Width != 1080 || preset.Height != 1920 {
				t.Fatalf("resolution = %dx%d, want 1080x1920", preset.Width, preset.Height)
			}
			if preset.FPS != 60 {
				t.Fatalf("fps = %d, want 60", preset.FPS)
			}
		})
	}
}

func TestPresetByNameUnknown(t *testing.T) {
	for _, name := range []string{"", "nope", "viral-60", "viral-beatsync", "natural-hq2-full", "smoke-lineups"} {
		if _, ok := PresetByName(name); ok {
			t.Fatalf("PresetByName(%q) ok = true, want false", name)
		}
	}
}

func TestDefaultPresetIsViral60Clean(t *testing.T) {
	preset := DefaultPreset()
	if preset.Name != PresetViral60Clean {
		t.Fatalf("default preset = %q, want %q", preset.Name, PresetViral60Clean)
	}
	if preset.EffectsPreset != EffectsPresetViralUltraClean {
		t.Fatalf("default effects preset = %q, want %q", preset.EffectsPreset, EffectsPresetViralUltraClean)
	}
	if !presetUsesFullFrame(preset.Name) {
		t.Fatalf("default preset should use full-frame layout")
	}
	if got, want := preset.HUDMode, "deathnotices"; got != want {
		t.Fatalf("default hud mode = %q, want %q", got, want)
	}
}

func TestRegisteredPresets(t *testing.T) {
	want := []string{PresetViral60Clean, PresetCleanPOV60, PresetFullHUD60}
	names := PresetNames()
	if len(names) != len(want) {
		t.Fatalf("PresetNames = %v, want %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Fatalf("PresetNames[%d] = %q, want %q (registry order)", i, names[i], w)
		}
	}
	if got := DefaultPreset().Name; got != PresetViral60Clean {
		t.Fatalf("default preset = %q, want %q (first entry)", got, PresetViral60Clean)
	}
}

func TestEveryPresetHasLabel(t *testing.T) {
	for _, name := range PresetNames() {
		preset, _ := PresetByName(name)
		if preset.Label == "" {
			t.Errorf("preset %q has no Label (the UI picker shows it)", name)
		}
	}
}

func TestViral60CleanRecordsDeathnoticesHUD(t *testing.T) {
	preset, ok := PresetByName(PresetViral60Clean)
	if !ok {
		t.Fatalf("PresetByName(%q) ok = false, want true", PresetViral60Clean)
	}
	if got, want := preset.HUDMode, "deathnotices"; got != want {
		t.Fatalf("hud mode = %q, want %q", got, want)
	}
	if preset.EffectsPreset != EffectsPresetViralUltraClean {
		t.Fatalf("effects preset = %q, want %q", preset.EffectsPreset, EffectsPresetViralUltraClean)
	}
	if !presetUsesFullFrame(preset.Name) {
		t.Fatalf("viral-60-clean should use full-frame layout")
	}
}

func TestPresetHUDModes(t *testing.T) {
	// Each user-facing preset records with a specific HUD; that capture-time
	// difference is what the user picks ("Kill Feed" vs "Clean POV" vs "Full
	// HUD"). The render path is otherwise identical across presets.
	want := map[string]string{
		PresetViral60Clean: "deathnotices",
		PresetCleanPOV60:   "clean",
		PresetFullHUD60:    "gameplay",
	}
	valid := map[string]bool{"gameplay": true, "clean": true, "deathnotices": true}
	for _, name := range PresetNames() {
		t.Run(name, func(t *testing.T) {
			preset, _ := PresetByName(name)
			if !valid[preset.HUDMode] {
				t.Fatalf("hud mode = %q, want one of gameplay/clean/deathnotices", preset.HUDMode)
			}
			if w, ok := want[name]; ok && preset.HUDMode != w {
				t.Fatalf("preset %q hud mode = %q, want %q", name, preset.HUDMode, w)
			}
		})
	}
}

func TestUnknownPresetErrorListsValidNames(t *testing.T) {
	cfg := Config{
		RecordingResultPath: "recording-result.json",
		OutputDir:           "out",
		Preset:              "definitely-not-a-preset",
	}
	err := cfg.validate()
	if err == nil {
		t.Fatal("validate error = nil, want unknown preset error")
	}
	for _, want := range []string{"unknown preset", PresetViral60Clean} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
	for _, removed := range []string{"viral-beatsync", "natural-hq2-full", "smoke-lineups"} {
		if strings.Contains(err.Error(), removed) {
			t.Fatalf("error %q listed removed preset %q", err.Error(), removed)
		}
	}
}

func TestRetiredEffectsPresetsAreRejected(t *testing.T) {
	for _, preset := range []string{"builtin-clean", "awpgod", "viral-ultra", "smoke-lineups", "none"} {
		t.Run(preset, func(t *testing.T) {
			cfg := Config{
				RecordingResultPath: "recording-result.json",
				OutputDir:           "out",
				EffectsPreset:       preset,
			}
			err := cfg.validate()
			if err == nil {
				t.Fatal("validate error = nil, want unknown effects preset error")
			}
			if !strings.Contains(err.Error(), "unknown effects preset") {
				t.Fatalf("error = %q, want unknown effects preset", err.Error())
			}
		})
	}
}
