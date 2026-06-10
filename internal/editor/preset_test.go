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
	for _, name := range []string{"", "nope", "natural-hq9"} {
		if _, ok := PresetByName(name); ok {
			t.Fatalf("PresetByName(%q) ok = true, want false", name)
		}
	}
}

func TestDefaultPresetIsViral60(t *testing.T) {
	preset := DefaultPreset()
	if preset.Name != PresetViral60 {
		t.Fatalf("default preset = %q, want %q", preset.Name, PresetViral60)
	}
	if preset.EffectsPreset != EffectsPresetViralUltra {
		t.Fatalf("default effects preset = %q, want %q", preset.EffectsPreset, EffectsPresetViralUltra)
	}
	if preset.FilterKind != FilterKindFullFrame {
		t.Fatalf("default filter kind = %q, want %q", preset.FilterKind, FilterKindFullFrame)
	}
}

func TestLegacyPresetsKeepHistoricalDefaults(t *testing.T) {
	type flags struct {
		hq, audio, quality, covers, smoothing bool
	}
	tests := []struct {
		name            string
		videoCRF        int
		videoPreset     string
		effectsPreset   string
		filterKind      string
		flags           flags
		accurateScaling bool
		masteringBT709  bool
		grade           presetGrade
	}{
		{
			name:          PresetShortClean,
			videoCRF:      18,
			videoPreset:   "fast",
			effectsPreset: EffectsPresetBuiltinClean,
			filterKind:    FilterKindCropCenter,
		},
		{
			name:          PresetShortPremiumPlayer,
			videoCRF:      18,
			videoPreset:   "fast",
			effectsPreset: EffectsPresetBuiltinClean,
			filterKind:    FilterKindCropCenter,
		},
		{
			name:          PresetShortViralSquare,
			videoCRF:      16,
			videoPreset:   "slow",
			effectsPreset: EffectsPresetBuiltinClean,
			filterKind:    FilterKindViralSquare,
			flags:         flags{hq: true, audio: true, quality: true, covers: true},
		},
		{
			name:          PresetShortNaturalHQ,
			videoCRF:      16,
			videoPreset:   "slow",
			effectsPreset: EffectsPresetNone,
			filterKind:    FilterKindCropCenter,
		},
		{
			name:          PresetShortNaturalHQ2,
			videoCRF:      16,
			videoPreset:   "slow",
			effectsPreset: EffectsPresetNone,
			filterKind:    FilterKindCropCenter,
			flags:         flags{hq: true, audio: true, quality: true, covers: true},
		},
		{
			name:          PresetShortNaturalHQ2Full,
			videoCRF:      16,
			videoPreset:   "slow",
			effectsPreset: EffectsPresetNone,
			filterKind:    FilterKindFullFrame,
			flags:         flags{hq: true, audio: true, quality: true, covers: true},
			grade:         presetGrade{Saturation: 1.12},
		},
		{
			name:            PresetShortNaturalHQ2FullPlus,
			videoCRF:        15,
			videoPreset:     "slower",
			effectsPreset:   EffectsPresetNone,
			filterKind:      FilterKindFullFrame,
			flags:           flags{hq: true, audio: true, quality: true, covers: true},
			accurateScaling: true,
			masteringBT709:  true,
			grade:           presetGrade{Saturation: 1.16, Contrast: 1.02, Gamma: 1.00, Unsharp: true},
		},
		{
			name:            PresetShortNaturalHQ3,
			videoCRF:        15,
			videoPreset:     "slower",
			effectsPreset:   EffectsPresetNone,
			filterKind:      FilterKindCropCenter,
			flags:           flags{hq: true, audio: true, quality: true, covers: true},
			accurateScaling: true,
			masteringBT709:  true,
		},
		{
			name:            PresetShortNaturalHQ3Smooth,
			videoCRF:        15,
			videoPreset:     "slower",
			effectsPreset:   EffectsPresetNone,
			filterKind:      FilterKindCropCenter,
			flags:           flags{hq: true, audio: true, quality: true, covers: true, smoothing: true},
			accurateScaling: true,
			masteringBT709:  true,
		},
		{
			name:          PresetSmokeLineups,
			videoCRF:      16,
			videoPreset:   "slow",
			effectsPreset: EffectsPresetSmokeLineups,
			filterKind:    FilterKindSmokeLineups,
			flags:         flags{hq: true, audio: true, quality: true, covers: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preset, ok := PresetByName(tt.name)
			if !ok {
				t.Fatalf("PresetByName(%q) ok = false, want true", tt.name)
			}
			if preset.VideoCRF != tt.videoCRF || preset.VideoPreset != tt.videoPreset {
				t.Fatalf("encode defaults = crf %d / %q, want crf %d / %q", preset.VideoCRF, preset.VideoPreset, tt.videoCRF, tt.videoPreset)
			}
			if preset.EffectsPreset != tt.effectsPreset {
				t.Fatalf("effects preset = %q, want %q", preset.EffectsPreset, tt.effectsPreset)
			}
			if preset.FilterKind != tt.filterKind {
				t.Fatalf("filter kind = %q, want %q", preset.FilterKind, tt.filterKind)
			}
			got := flags{
				hq:        preset.HQFilters,
				audio:     preset.AudioNormalize,
				quality:   preset.QualityChecks,
				covers:    preset.CoverSheets,
				smoothing: preset.TemporalSmoothing,
			}
			if got != tt.flags {
				t.Fatalf("feature flags = %+v, want %+v", got, tt.flags)
			}
			if preset.AccurateScaling != tt.accurateScaling {
				t.Fatalf("accurate scaling = %v, want %v", preset.AccurateScaling, tt.accurateScaling)
			}
			if preset.MasteringBT709 != tt.masteringBT709 {
				t.Fatalf("bt709 mastering = %v, want %v", preset.MasteringBT709, tt.masteringBT709)
			}
			if preset.Grade != tt.grade {
				t.Fatalf("grade = %+v, want %+v", preset.Grade, tt.grade)
			}
			if preset.RhythmSync {
				t.Fatalf("rhythm sync = true, want false for legacy preset %q", tt.name)
			}
		})
	}
}

func TestViralBeatsyncRequiresRhythmInputs(t *testing.T) {
	preset, ok := PresetByName(PresetViralBeatsync)
	if !ok {
		t.Fatalf("PresetByName(%q) ok = false, want true", PresetViralBeatsync)
	}
	if !preset.RhythmSync {
		t.Fatal("viral-beatsync rhythm sync = false, want true")
	}
	cfg := Config{
		RecordingResultPath: "recording-result.json",
		OutputDir:           "out",
		Preset:              PresetViralBeatsync,
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("validate error = nil, want music path requirement")
	}
	cfg.MusicPath = "music.wav"
	if err := cfg.validate(); err == nil {
		t.Fatal("validate error = nil, want rhythm path requirement")
	}
	cfg.RhythmPath = "rhythm.json"
	cfg.CompileSegments = true
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate error = %v, want nil with rhythm inputs", err)
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
	for _, want := range []string{"unknown preset", PresetViral60, PresetShortClean, PresetSmokeLineups} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}
