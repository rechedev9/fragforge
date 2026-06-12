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

func TestValidateEffectColor(t *testing.T) {
	valid := []string{
		"white", "red", "black",
		"white@0.92", "black@0.36", "red@0.5",
		"#ffffff", "#FF00FF",
		"0xff0000", "0x2a1190@0.92", "0xff0000ff",
	}
	for _, v := range valid {
		if err := validateEffectColor("color", v); err != nil {
			t.Errorf("validateEffectColor(%q) = %v, want nil", v, err)
		}
	}

	// All of these would inject extra filtergraph clauses or stream labels if
	// interpolated into a drawbox/drawtext color argument.
	invalid := []string{
		"",
		"white:drawbox=x=0:y=0",
		"red,sendcmd=c='reverse'",
		"white@0.5[tag]",
		"black;exec",
		"red 0.5",
		"white\nred",
		"0xff0000:t=fill",
	}
	for _, v := range invalid {
		if err := validateEffectColor("color", v); err == nil {
			t.Errorf("validateEffectColor(%q) = nil, want error", v)
		}
	}
}

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
		Preset:          PresetViral60Clean,
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

func TestEvaluateEffectsTextFadeOptions(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_kill(function(k)
  text({
    value = "HEADSHOT",
    at = k.time,
    pre = 0.1,
    post = 0.9,
    fade_in = 0.08,
    fade_out = 0.18
  })
end)
`,
	}
	short := ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
		DurationSeconds: 5,
		Kills:           []KillCue{{Tick: 100, TimeSeconds: 1, Weapon: "AK-47", Headshot: true}},
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
	if effect.Type != EffectText || effect.FadeInSeconds != 0.08 || effect.FadeOutSeconds != 0.18 {
		t.Fatalf("text fade effect = %#v", effect)
	}
}

func TestEvaluateEffectsTextFontFileOption(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_kill(function(k)
  text({
    value = "FAST TRADE",
    at = k.time,
    fontfile = "C:/fonts/BebasNeue-Regular.ttf"
  })
end)
`,
	}
	short := ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
		DurationSeconds: 5,
		Kills:           []KillCue{{Tick: 100, TimeSeconds: 1, Weapon: "AK-47"}},
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
	if got, want := effects[0].FontFile, "C:/fonts/BebasNeue-Regular.ttf"; got != want {
		t.Fatalf("fontfile = %q, want %q", got, want)
	}
}

func TestEvaluateEffectsTextShadowOptions(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_kill(function(k)
  text({
    value = "HEADSHOT",
    at = k.time,
    shadow_color = "black@0.55",
    shadow_x = 2,
    shadow_y = 3
  })
end)
`,
	}
	short := ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
		DurationSeconds: 5,
		Kills:           []KillCue{{Tick: 100, TimeSeconds: 1, Weapon: "AK-47", Headshot: true}},
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
	if effect.ShadowColor != "black@0.55" || effect.ShadowX != 2 || effect.ShadowY != 3 {
		t.Fatalf("text shadow effect = %#v", effect)
	}
}

func TestEvaluateEffectsTextShadowDefaultsOff(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_kill(function(k)
  text({ value = "HEADSHOT", at = k.time })
end)
`,
	}
	short := ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
		DurationSeconds: 5,
		Kills:           []KillCue{{Tick: 100, TimeSeconds: 1, Weapon: "AK-47"}},
	}

	effects, _, err := evaluateEffects(source, short)
	if err != nil {
		t.Fatalf("evaluateEffects error = %v", err)
	}
	if len(effects) != 1 {
		t.Fatalf("effects len = %d, want 1: %#v", len(effects), effects)
	}
	if effects[0].ShadowColor != "" {
		t.Fatalf("shadow color = %q, want empty (shadow off by default)", effects[0].ShadowColor)
	}
}

func TestEvaluateEffectsRejectsInvalidShadow(t *testing.T) {
	cases := []struct {
		name   string
		script string
		want   string
	}{
		{
			name: "bad color",
			script: `
on_segment(function(s)
  text({ value = "bad", start = 0, duration = 1, shadow_color = "black:enable=1" })
end)
`,
			want: "shadow_color",
		},
		{
			name: "offset out of range",
			script: `
on_segment(function(s)
  text({ value = "bad", start = 0, duration = 1, shadow_color = "black@0.5", shadow_x = 99 })
end)
`,
			want: "shadow_x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := evaluateEffects(effectsSource{
				Preset: EffectsPresetExternal,
				Script: tc.script,
			}, ShortEdit{
				SegmentID:       "seg-001",
				Preset:          PresetViral60Clean,
				DurationSeconds: 5,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("evaluateEffects error = %v, want %s validation", err, tc.want)
			}
		})
	}
}

func TestViralUltraCleanEmitsKillfeedPerKill(t *testing.T) {
	short := ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
		DurationSeconds: 12,
		Player:          "latto",
		Map:             "de_mirage",
		KillCount:       2,
		Kills: []KillCue{
			{Tick: 100, TimeSeconds: 1, Weapon: "AK-47"},
			{Tick: 500, TimeSeconds: 7, Weapon: "AK-47", Headshot: true},
		},
	}

	source, err := loadEffectsSource("", EffectsPresetViralUltraClean)
	if err != nil {
		t.Fatalf("loadEffectsSource error = %v", err)
	}
	effects, warnings, err := evaluateEffects(source, short)
	if err != nil {
		t.Fatalf("evaluateEffects error = %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	killfeeds := killfeedEffects(effects)
	if got, want := len(killfeeds), len(short.Kills); got != want {
		t.Fatalf("killfeed effects = %d, want %d (one per kill)", got, want)
	}
	for i, effect := range killfeeds {
		if effect.AtSeconds != short.Kills[i].TimeSeconds {
			t.Fatalf("killfeed[%d] at = %.3f, want %.3f", i, effect.AtSeconds, short.Kills[i].TimeSeconds)
		}
	}
}

func TestLoadEffectsSourceDefaultsToViralUltraClean(t *testing.T) {
	source, err := loadEffectsSource("", "")
	if err != nil {
		t.Fatalf("loadEffectsSource error = %v", err)
	}
	if source.Preset != EffectsPresetViralUltraClean {
		t.Fatalf("default effects preset = %q, want %q", source.Preset, EffectsPresetViralUltraClean)
	}
	if source.Script == "" {
		t.Fatal("default effects source is empty")
	}
}

func TestViralUltraCleanDropsPerKillTextBanners(t *testing.T) {
	short := ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
		DurationSeconds: 20,
		Player:          "latto",
		Map:             "de_mirage",
		KillCount:       5,
		Kills: []KillCue{
			{Tick: 100, TimeSeconds: 1, Weapon: "AK-47", Headshot: true},
			{Tick: 160, TimeSeconds: 2, Weapon: "AK-47", Wallbang: true},
			{Tick: 300, TimeSeconds: 6, Weapon: "AWP"},
			{Tick: 400, TimeSeconds: 10, Weapon: "AK-47"},
			{Tick: 500, TimeSeconds: 14, Weapon: "AK-47"},
		},
	}

	source, err := loadEffectsSource("", EffectsPresetViralUltraClean)
	if err != nil {
		t.Fatalf("loadEffectsSource error = %v", err)
	}
	effects, _, err := evaluateEffects(source, short)
	if err != nil {
		t.Fatalf("evaluateEffects error = %v", err)
	}
	// the killfeed overlay narrates the kills: no weapon labels, wallbang or
	// chain banners, and no milestone pills on the clean preset
	for _, effect := range effects {
		if effect.Type != EffectText {
			continue
		}
		for _, banned := range []string{"HEADSHOT", "THROUGH THE WALL", "FAST CHAIN", "KILLS", "AWP PICK"} {
			if strings.Contains(effect.Value, banned) {
				t.Fatalf("clean preset emits per-kill banner %q", effect.Value)
			}
		}
	}
	counters := 0
	for _, effect := range effects {
		if effect.Type == EffectText && strings.Contains(effect.Value, "/5") {
			counters++
		}
	}
	if counters != len(short.Kills) {
		t.Fatalf("kill counter texts = %d, want %d", counters, len(short.Kills))
	}
	if got, want := len(killfeedEffects(effects)), len(short.Kills); got != want {
		t.Fatalf("killfeed effects = %d, want %d", got, want)
	}
}

func TestEvaluateEffectsRejectsInvalidFade(t *testing.T) {
	_, _, err := evaluateEffects(effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_segment(function(s)
  text({ value = "bad", start = 0, duration = 1, fade_in = -0.1 })
end)
`,
	}, ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
		DurationSeconds: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "fade_in") {
		t.Fatalf("evaluateEffects error = %v, want fade validation", err)
	}
}

func TestValidateEffectPosition(t *testing.T) {
	valid := []string{"48", "72", "140", "(W-w)/2", "(w-text_w)/2", "W-w-18", "(h-text_h)/2", "-5"}
	for _, v := range valid {
		if err := validateEffectPosition("x", v); err != nil {
			t.Errorf("validateEffectPosition(%q) = %v, want nil", v, err)
		}
	}
	// All of these would close the drawtext/overlay option and inject a new
	// filtergraph clause if interpolated unescaped into an x=/y= argument.
	invalid := []string{"", "0:drawbox=w=iw", "10,20", "5;quit", "x[v]", "1\n2", "w=iw"}
	for _, v := range invalid {
		if err := validateEffectPosition("x", v); err == nil {
			t.Errorf("validateEffectPosition(%q) = nil, want error", v)
		}
	}
}

func TestEvaluateEffectsRejectsPositionInjection(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_segment(function(s)
  text({ value = "hi", start = 0, duration = 1, x = "0:drawbox=w=iw:h=ih:color=red", y = 34, size = 28 })
end)
`,
	}
	short := ShortEdit{SegmentID: "seg-001", Preset: PresetViral60Clean, Label: "x", DurationSeconds: 5}
	if _, _, err := evaluateEffects(source, short); err == nil {
		t.Fatal("evaluateEffects error = nil, want error for x position with filtergraph metacharacters")
	}
}

func TestEvaluateEffectsRejectsFlashColorWithOpacity(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_kill(function(k)
  flash({ at = k.time, duration = 0.1, color = "white@0.5" })
end)
`,
	}
	short := ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
		DurationSeconds: 5,
		Kills:           []KillCue{{Tick: 100, TimeSeconds: 1, Weapon: "AWP"}},
	}
	if _, _, err := evaluateEffects(source, short); err == nil {
		t.Fatal("evaluateEffects error = nil, want error for flash color with @opacity (double-opacity)")
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
		Preset:          PresetViral60Clean,
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

func TestEvaluateEffectsImageCallbackProducesOverlay(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_segment(function(s)
  image({
    path = "assets/title.png",
    start = 0,
    duration = s.duration,
    x = "(W-w)/2",
    y = 96,
    width = 720
  })
end)
`,
	}
	short := ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
		DurationSeconds: 6,
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
	if effect.Type != EffectImage || effect.Path != "assets/title.png" || effect.Width != 720 {
		t.Fatalf("image effect = %#v", effect)
	}
	if effect.StartSeconds != 0 || effect.EndSeconds != 6 {
		t.Fatalf("image window = %.3f-%.3f", effect.StartSeconds, effect.EndSeconds)
	}
}

func TestEvaluateEffectsKillfeedCallbackProducesSourceCropOverlay(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_kill(function(k)
  killfeed({
    at = k.time,
    pre = 0.25,
    post = 1.5,
    x = "W-w-18",
    y = 438,
    width = 430,
    crop_x = 1558,
    crop_y = 64,
    crop_width = 360,
    crop_height = 110
  })
end)
`,
	}
	short := ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
		DurationSeconds: 6,
		Kills:           []KillCue{{Tick: 100, TimeSeconds: 1, Weapon: "MP9", Headshot: true}},
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
	if effect.Type != EffectKillfeed || effect.Width != 430 || effect.CropX != 1558 || effect.CropWidth != 360 {
		t.Fatalf("killfeed effect = %#v", effect)
	}
	if effect.StartSeconds != 0.75 || effect.EndSeconds != 2.5 {
		t.Fatalf("killfeed window = %.3f-%.3f", effect.StartSeconds, effect.EndSeconds)
	}
	if effect.SourceKillWeapon != "MP9" || !effect.SourceKillHeadshot {
		t.Fatalf("killfeed source metadata = %#v", effect)
	}
}

func TestEvaluateEffectsRejectsInvalidScript(t *testing.T) {
	_, _, err := evaluateEffects(effectsSource{
		Preset: EffectsPresetExternal,
		Script: `on_kill(function(k) zoom({ at = k.time, scale = 9 }) end)`,
	}, ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
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
		Preset:          PresetViral60Clean,
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
		Preset:          PresetViral60Clean,
		DurationSeconds: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Fatalf("evaluateEffects error = %v, want context deadline", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("runaway Lua script took %s to stop", elapsed)
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
