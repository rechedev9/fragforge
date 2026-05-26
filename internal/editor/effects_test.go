package editor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvaluateEffectsCallbacksProduceTimeline(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_segment(function(s)
  text({ value = s.label, start = 0, duration = 1.5, x = 12, y = 34, size = 28 })
end)

on_kill(function(k)
  zoom({ at = k.time, pre = 0.2, post = 0.4, scale = 1.2 })
  if k.headshot then
    flash({ at = k.time, duration = 0.1, opacity = 0.25, color = "#ffffff" })
  end
end)
`,
	}
	short := ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetShortClean,
		Label:           "MartinezSa | de_ancient | 1K",
		DurationSeconds: 5,
		Kills: []KillCue{
			{Tick: 100, TimeSeconds: 1, Weapon: "AWP", Headshot: true},
		},
	}

	effects, warnings, err := evaluateEffects(source, short)
	if err != nil {
		t.Fatalf("evaluateEffects error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(effects) != 3 {
		t.Fatalf("effects len = %d, want 3: %#v", len(effects), effects)
	}
	if effects[0].Type != EffectText || effects[0].Value != short.Label || effects[0].X != "12" || effects[0].Y != "34" {
		t.Fatalf("segment text effect = %#v", effects[0])
	}
	if effects[1].Type != EffectZoom || effects[1].StartSeconds != 0.8 || effects[1].EndSeconds != 1.4 || effects[1].Scale != 1.2 {
		t.Fatalf("zoom effect = %#v", effects[1])
	}
	if effects[2].Type != EffectFlash || effects[2].StartSeconds != 1 || effects[2].EndSeconds != 1.1 || effects[2].Opacity != 0.25 {
		t.Fatalf("flash effect = %#v", effects[2])
	}
}

func TestEvaluateEffectsSmokeCallbacksProduceText(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_smoke(function(smoke)
  text({
    value = smoke.from_area .. " -> " .. smoke.destination,
    at = smoke.pop_time,
    pre = 0.1,
    post = 0.4,
    x = "(w-text_w)/2",
    y = 140,
    size = 42
  })
end)
`,
	}
	short := ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetSmokeLineups,
		DurationSeconds: 6,
		Smokes: []SmokeCue{
			{
				ID:             "smoke-001",
				Type:           "smokegrenade",
				TimeSeconds:    1,
				PopTimeSeconds: 2,
				FromArea:       "T Spawn",
				Destination:    "CT",
			},
		},
	}

	effects, warnings, err := evaluateEffects(source, short)
	if err != nil {
		t.Fatalf("evaluateEffects error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if len(effects) != 1 {
		t.Fatalf("effects len = %d, want 1: %#v", len(effects), effects)
	}
	effect := effects[0]
	if effect.Type != EffectText || effect.Value != "T Spawn -> CT" {
		t.Fatalf("smoke text effect = %#v", effect)
	}
	if effect.StartSeconds != 1.9 || effect.EndSeconds != 2.4 {
		t.Fatalf("smoke text window = %.3f-%.3f", effect.StartSeconds, effect.EndSeconds)
	}
	if effect.SourceSmokeID != "smoke-001" || effect.SourceSmokeTarget != "CT" {
		t.Fatalf("smoke source metadata = %#v", effect)
	}
}

func TestEvaluateEffectsRejectsInvalidScript(t *testing.T) {
	_, _, err := evaluateEffects(effectsSource{
		Preset: EffectsPresetExternal,
		Script: `on_kill(function(k) zoom({ at = k.time, scale = 9 }) end)`,
	}, ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetShortClean,
		DurationSeconds: 5,
		Kills:           []KillCue{{TimeSeconds: 1}},
	})
	if err == nil || !strings.Contains(err.Error(), "scale") {
		t.Fatalf("evaluateEffects error = %v, want scale validation", err)
	}
}

func TestEvaluateEffectsDisablesUnsafeLuaLibraries(t *testing.T) {
	_, _, err := evaluateEffects(effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
for _, name in ipairs({"dofile", "loadfile", "require", "collectgarbage", "print", "os", "io", "package", "debug"}) do
  if _G[name] ~= nil then
    error(name .. " should be disabled")
  end
end
on_segment(function(s)
  text({ value = s.label, start = 0, duration = 1 })
end)
`,
	}, ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetShortClean,
		Label:           "safe",
		DurationSeconds: 5,
	})
	if err != nil {
		t.Fatalf("evaluateEffects error = %v", err)
	}
}

func TestEvaluateEffectsTimesOutRunawayScript(t *testing.T) {
	oldTimeout := effectsEvaluationTimeout
	effectsEvaluationTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		effectsEvaluationTimeout = oldTimeout
	})

	start := time.Now()
	_, _, err := evaluateEffects(effectsSource{
		Preset: EffectsPresetExternal,
		Script: `while true do end`,
	}, ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetShortClean,
		DurationSeconds: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("evaluateEffects error = %v, want context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("runaway Lua script took %s to stop", elapsed)
	}
}

func TestBuildManifestEffectsPresetNoneLeavesBaseFilter(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.EffectsPreset = EffectsPresetNone

	manifest := BuildManifest(result, opts)
	if manifest.EffectsPreset != EffectsPresetNone {
		t.Fatalf("effects preset = %q", manifest.EffectsPreset)
	}
	if len(manifest.Shorts) != 2 {
		t.Fatalf("shorts len = %d", len(manifest.Shorts))
	}
	if len(manifest.Shorts[0].Effects) != 0 {
		t.Fatalf("effects = %#v, want none", manifest.Shorts[0].Effects)
	}
	filter := argAfter(manifest.Shorts[0].FFmpegCommand, "-vf")
	if strings.Contains(filter, "drawtext") || strings.Contains(filter, "if(between") {
		t.Fatalf("base filter should not contain scripted effects:\n%s", filter)
	}
}

func TestBuildManifestAWPGodPresetAddsGradeAndAWPFlash(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.EffectsPreset = EffectsPresetAWPGod

	manifest := BuildManifest(result, opts)
	if manifest.EffectsPreset != EffectsPresetAWPGod {
		t.Fatalf("effects preset = %q", manifest.EffectsPreset)
	}
	if len(manifest.Shorts) != 2 {
		t.Fatalf("shorts len = %d", len(manifest.Shorts))
	}
	if !hasEffect(manifest.Shorts[0].Effects, EffectGrade) || !hasEffect(manifest.Shorts[0].Effects, EffectZoom) {
		t.Fatalf("first short missing grade/zoom effects: %#v", manifest.Shorts[0].Effects)
	}
	if !hasEffect(manifest.Shorts[1].Effects, EffectFlash) {
		t.Fatalf("AWP short missing flash effect: %#v", manifest.Shorts[1].Effects)
	}
	filter := argAfter(manifest.Shorts[1].FFmpegCommand, "-vf")
	if !strings.Contains(filter, "drawbox") || !strings.Contains(filter, "eq=contrast=1.080") {
		t.Fatalf("awpgod filter missing flash/grade:\n%s", filter)
	}
}

func TestRunDryRunExternalEffectsWritesMetadata(t *testing.T) {
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)
	effectsPath := filepath.Join(dir, "effects.lua")
	if err := os.WriteFile(effectsPath, []byte(`
on_kill(function(k)
  if k.weapon == "AWP" then
    flash({ at = k.time, duration = 0.1, opacity = 0.2 })
  end
end)
`), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(dir, "shorts")

	result, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           outDir,
		EffectsPath:         effectsPath,
		FFmpegPath:          filepath.Join(dir, "missing-ffmpeg"),
		DryRun:              true,
	})
	if err != nil {
		t.Fatalf("Run dry-run error = %v", err)
	}
	if result.EffectsPreset != EffectsPresetExternal || result.EffectsPath == "" {
		t.Fatalf("effects metadata missing: %#v", result)
	}
	if len(result.Shorts) != 2 || len(result.Shorts[0].Effects) != 0 || len(result.Shorts[1].Effects) != 1 {
		t.Fatalf("short effects = %#v", result.Shorts)
	}

	var manifest Manifest
	b, err := os.ReadFile(filepath.Join(outDir, "edit-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.EffectsPreset != EffectsPresetExternal || manifest.EffectsPath == "" {
		t.Fatalf("manifest effects metadata missing: %#v", manifest)
	}
}

func hasEffect(effects []Effect, typ EffectType) bool {
	for _, effect := range effects {
		if effect.Type == typ {
			return true
		}
	}
	return false
}
