package editor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
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

func TestViralUltraCleanEmitsOnlyColorGrade(t *testing.T) {
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
	// The clean preset adds no overlay lettering and no per-kill effects
	// (zoom/flash/killfeed): it keeps only a colour grade over raw gameplay.
	if len(effects) == 0 {
		t.Fatal("clean preset emitted no effects; expected a colour grade")
	}
	for _, effect := range effects {
		if effect.Type != EffectGrade {
			t.Fatalf("clean preset emits a %q effect; want only colour grade", effect.Type)
		}
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
	dir := t.TempDir()
	assetsDir := filepath.Join(dir, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll assets: %v", err)
	}
	imagePath := filepath.Join(assetsDir, "title.png")
	if err := os.WriteFile(imagePath, []byte("test image"), 0o600); err != nil {
		t.Fatalf("WriteFile image: %v", err)
	}
	source := effectsSource{
		Path:   filepath.Join(dir, "effects.lua"),
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
	if effect.Type != EffectImage || effect.Path != imagePath || effect.Width != 720 {
		t.Fatalf("image effect = %#v", effect)
	}
	if effect.StartSeconds != 0 || effect.EndSeconds != 6 {
		t.Fatalf("image window = %.3f-%.3f", effect.StartSeconds, effect.EndSeconds)
	}
}

func TestLoadEffectsSourceRejectsOversizedScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effects.lua")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxEffectsSourceBytes+1)), 0o600); err != nil {
		t.Fatalf("WriteFile effects script: %v", err)
	}

	_, err := loadEffectsSource(path, "")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("loadEffectsSource error = %v, want size limit", err)
	}
}

func TestEvaluateEffectsRejectsExcessiveCallbacks(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: strings.Repeat("on_segment(function() end)\n", maxEffectCallbacks+1),
	}

	_, _, err := evaluateEffects(source, ShortEdit{SegmentID: "seg-001", DurationSeconds: 6})
	if err == nil || !strings.Contains(err.Error(), "callback limit") {
		t.Fatalf("evaluateEffects error = %v, want callback limit", err)
	}
}

func TestEvaluateEffectsRejectsExcessiveCallbackExecutions(t *testing.T) {
	kills := make([]KillCue, maxCallbackExecutions+1)
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `on_kill(function() end)`,
	}

	_, _, err := evaluateEffects(source, ShortEdit{
		SegmentID:       "seg-001",
		DurationSeconds: 6,
		Kills:           kills,
	})
	if err == nil || !strings.Contains(err.Error(), "callback execution limit") {
		t.Fatalf("evaluateEffects error = %v, want callback execution limit", err)
	}
}

func TestEvaluateEffectsRejectsOversizedStringRep(t *testing.T) {
	source := effectsSource{
		Preset: EffectsPresetExternal,
		Script: `
on_segment(function()
  text({ value = string.rep("x", 4097), duration = 1 })
end)
`,
	}

	_, _, err := evaluateEffects(source, ShortEdit{SegmentID: "seg-001", DurationSeconds: 6})
	if err == nil || !strings.Contains(err.Error(), "string.rep result exceeds") {
		t.Fatalf("evaluateEffects error = %v, want string size limit", err)
	}
}

func TestEvaluateEffectsRejectsImagePathOutsideScriptDirectory(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(filepath.Dir(dir), "outside.png")
	if err := os.WriteFile(outside, []byte("test image"), 0o600); err != nil {
		t.Fatalf("WriteFile outside image: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(outside) })
	source := effectsSource{
		Path:   filepath.Join(dir, "effects.lua"),
		Preset: EffectsPresetExternal,
		Script: `
on_segment(function()
  image({ path = "../outside.png", duration = 1 })
end)
`,
	}

	_, _, err := evaluateEffects(source, ShortEdit{SegmentID: "seg-001", DurationSeconds: 6})
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("evaluateEffects error = %v, want path escape rejection", err)
	}
}

func TestResolveEffectImagePathRejectsNonLocalInputs(t *testing.T) {
	root := t.TempDir()
	for _, raw := range []string{
		"https://example.invalid/title.png",
		`\\server\share\title.png`,
		`\\?\C:\Windows\title.png`,
		`C:\Windows\title.png`,
	} {
		if _, err := resolveEffectImagePath(root, raw); err == nil {
			t.Fatalf("resolveEffectImagePath(%q) error = nil, want rejection", raw)
		}
	}
}

func TestResolveEffectImagePathRejectsSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "effects")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("Mkdir effects root: %v", err)
	}
	outside := filepath.Join(parent, "outside.png")
	if err := os.WriteFile(outside, []byte("test image"), 0o600); err != nil {
		t.Fatalf("WriteFile outside image: %v", err)
	}
	link := filepath.Join(root, "linked.png")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := resolveEffectImagePath(root, "linked.png"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("resolveEffectImagePath symlink error = %v, want escape rejection", err)
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
if dofile ~= nil then error("dofile should be disabled") end
if loadfile ~= nil then error("loadfile should be disabled") end
if load ~= nil then error("load should be disabled") end
if loadstring ~= nil then error("loadstring should be disabled") end
if require ~= nil then error("require should be disabled") end
if collectgarbage ~= nil then error("collectgarbage should be disabled") end
if print ~= nil then error("print should be disabled") end
if os ~= nil then error("os should be disabled") end
if io ~= nil then error("io should be disabled") end
if package ~= nil then error("package should be disabled") end
if debug ~= nil then error("debug should be disabled") end
if table ~= nil then error("table should be disabled") end
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

func TestEvaluateEffectsCannotLoadUncheckedLua(t *testing.T) {
	for _, loader := range []string{"load", "loadstring"} {
		_, _, err := evaluateEffects(effectsSource{
			Preset: EffectsPresetExternal,
			Script: fmt.Sprintf(`%s("while true do end")()`, loader),
		}, ShortEdit{
			SegmentID:       "seg-001",
			DurationSeconds: 5,
		})
		if err == nil || !strings.Contains(err.Error(), "attempt to call a non-function object") {
			t.Fatalf("%s bypass error = %v, want disabled loader", loader, err)
		}
	}
}

func TestEvaluateEffectsRejectsRunawayControlFlow(t *testing.T) {
	start := time.Now()
	_, _, err := evaluateEffects(effectsSource{
		Preset: EffectsPresetExternal,
		Script: `while true do end`,
	}, ShortEdit{
		SegmentID:       "seg-001",
		Preset:          PresetViral60Clean,
		DurationSeconds: 5,
	})
	if err == nil || !strings.Contains(err.Error(), "loops are disabled") {
		t.Fatalf("evaluateEffects error = %v, want loop rejection", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("runaway Lua script took %s to reject", elapsed)
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

func TestGeneratedHookEffect(t *testing.T) {
	short := ShortEdit{HookText: true, Headline: "2K con AK-47 en de_ancient", DurationSeconds: 8}
	effects := generatedHookEffect(short)
	if len(effects) != 1 {
		t.Fatalf("effects = %#v, want exactly one hook", effects)
	}
	hook := effects[0]
	if hook.Type != EffectText || hook.Value != strings.ToUpper(short.Headline) {
		t.Fatalf("hook = %#v, want upper-cased text drawing the headline", hook)
	}
	if hook.X != "(w-text_w)/2" || hook.Y != "150" || hook.BoxColor != "none" || hook.ShadowColor == "" {
		t.Fatalf("hook styling = %#v, want centered box-less shadowed text", hook)
	}
	if !hook.Bold || hook.BorderWidth <= 0 || hook.BorderColor == "" {
		t.Fatalf("hook styling = %#v, want a bold outlined title", hook)
	}
	if hook.StartSeconds != 0 || hook.EndSeconds != 1.8 {
		t.Fatalf("hook window = %.3f..%.3f, want 0..1.8", hook.StartSeconds, hook.EndSeconds)
	}

	short.DurationSeconds = 1.0
	if got := generatedHookEffect(short)[0].EndSeconds; got != 1.0 {
		t.Fatalf("hook end on short clip = %.3f, want clamped to 1.0", got)
	}

	short.HookText = false
	if got := generatedHookEffect(short); got != nil {
		t.Fatalf("effects with hook disabled = %#v, want none", got)
	}
}

// TestIntroSuppressesHook verifies the intro card and the plain hook never
// draw at once: showing the same headline twice over the opening seconds
// would read as a broken duplicate, so the intro card takes over from the
// hook whenever Intro is on.
func TestIntroSuppressesHook(t *testing.T) {
	short := ShortEdit{Intro: true, HookText: true, Headline: "Highlight", DurationSeconds: 8}
	if got := generatedHookEffect(short); got != nil {
		t.Fatalf("hook effects with intro on = %#v, want none", got)
	}
	bookends := generatedBookendEffects(short)
	if len(bookends) != 1 || bookends[0].Value != "HIGHLIGHT" {
		t.Fatalf("bookend effects = %#v, want exactly one upper-cased intro card", bookends)
	}

	short.Intro = false
	if got := generatedHookEffect(short); len(got) != 1 {
		t.Fatalf("hook effects with intro off = %#v, want exactly one", got)
	}
}

func TestGeneratedBookendEffectsDefaultText(t *testing.T) {
	short := ShortEdit{Intro: true, Outro: true, Headline: "2K Highlight", DurationSeconds: 10}
	effects := generatedBookendEffects(short)
	if len(effects) != 2 {
		t.Fatalf("effects = %#v, want intro and outro", effects)
	}
	intro, outro := effects[0], effects[1]
	if intro.Value != "2K HIGHLIGHT" {
		t.Fatalf("intro value = %q, want upper-cased generated headline", intro.Value)
	}
	if outro.Value != "FragForge" {
		t.Fatalf("outro value = %q, want the FragForge default", outro.Value)
	}
	if !intro.Bold || intro.BorderWidth <= 0 || !outro.Bold || outro.BorderWidth <= 0 {
		t.Fatalf("intro/outro styling = %#v / %#v, want bold outlined text", intro, outro)
	}
}

func TestGeneratedBookendEffectsCustomTextIsNotUppercased(t *testing.T) {
	short := ShortEdit{
		Intro:           true,
		Outro:           true,
		Headline:        "2K Highlight",
		IntroText:       "Watch this ace",
		OutroText:       "follow for more",
		DurationSeconds: 10,
	}
	effects := generatedBookendEffects(short)
	if len(effects) != 2 {
		t.Fatalf("effects = %#v, want intro and outro", effects)
	}
	if got := effects[0].Value; got != "Watch this ace" {
		t.Fatalf("intro value = %q, want the custom text verbatim", got)
	}
	if got := effects[1].Value; got != "follow for more" {
		t.Fatalf("outro value = %q, want the custom text verbatim", got)
	}
}

func TestGeneratedBookendEffectsIntroWindow(t *testing.T) {
	short := ShortEdit{Intro: true, Headline: "Highlight", DurationSeconds: 10}
	intro := generatedBookendEffects(short)[0]
	if intro.StartSeconds != 0 || intro.EndSeconds != 1.3 {
		t.Fatalf("intro window = %.3f..%.3f, want 0..1.3", intro.StartSeconds, intro.EndSeconds)
	}

	short.DurationSeconds = 1.0
	intro = generatedBookendEffects(short)[0]
	if intro.EndSeconds != 1.0 {
		t.Fatalf("intro end on short clip = %.3f, want clamped to 1.0", intro.EndSeconds)
	}
}

func TestGeneratedBookendEffectsOutroWindowNeverExtendsDuration(t *testing.T) {
	short := ShortEdit{Outro: true, DurationSeconds: 10}
	outro := generatedBookendEffects(short)[0]
	if outro.EndSeconds != short.DurationSeconds {
		t.Fatalf("outro end = %.3f, want the clip duration %.3f (never extended)", outro.EndSeconds, short.DurationSeconds)
	}
	if got := outro.EndSeconds - outro.StartSeconds; math.Abs(got-1.6) > 1e-9 {
		t.Fatalf("outro window = %.3fs, want 1.6s on a clip long enough for it", got)
	}

	// A clip shorter than the outro window must clamp the start to 0 rather
	// than run negative or extend past the clip.
	short.DurationSeconds = 2
	outro = generatedBookendEffects(short)[0]
	if outro.StartSeconds < 0 || outro.EndSeconds != short.DurationSeconds {
		t.Fatalf("outro window on a 2s clip = %.3f..%.3f, want start clamped to >=0 and end at the clip duration", outro.StartSeconds, outro.EndSeconds)
	}
}

func TestGeneratedBookendEffectsEscapeSpecialCharacters(t *testing.T) {
	short := ShortEdit{
		Intro:           true,
		Outro:           true,
		IntroText:       "Zack's: 100% Ace",
		OutroText:       "follow @zack, now!",
		DurationSeconds: 10,
	}
	short.Effects = generatedBookendEffects(short)
	filter := VideoFilter(short)
	for _, want := range []string{`Zack\'s\: 100\% Ace`, `follow @zack\, now!`} {
		if !strings.Contains(filter, want) {
			t.Fatalf("filter missing escaped text %q:\n%s", want, filter)
		}
	}
}

func TestGeneratedKillCounterEffects(t *testing.T) {
	short := ShortEdit{
		KillCounter:     true,
		DurationSeconds: 8,
		Kills: []KillCue{
			{TimeSeconds: 1.0, Tick: 100},
			{TimeSeconds: 1.5, Tick: 132},
			{TimeSeconds: 5.0, Tick: 356},
		},
	}
	effects := generatedKillCounterEffects(short)
	if len(effects) != 3 {
		t.Fatalf("effects = %#v, want 3 counter pops", effects)
	}
	if effects[0].Value != "1" || effects[1].Value != "2" || effects[2].Value != "3K" {
		t.Fatalf("values = %q %q %q, want 1, 2, 3K", effects[0].Value, effects[1].Value, effects[2].Value)
	}
	if effects[2].Size <= effects[0].Size {
		t.Fatalf("milestone size = %d, want larger than %d", effects[2].Size, effects[0].Size)
	}
	// The first pop would run past the second kill; it must end when the next
	// kill lands so pops never stack.
	if effects[0].EndSeconds != 1.5 {
		t.Fatalf("first pop end = %.3f, want clamped to next kill at 1.5", effects[0].EndSeconds)
	}
	if effects[1].EndSeconds != 2.45 {
		t.Fatalf("second pop end = %.3f, want 2.45", effects[1].EndSeconds)
	}

	single := ShortEdit{KillCounter: true, DurationSeconds: 8, Kills: []KillCue{{TimeSeconds: 2}}}
	got := generatedKillCounterEffects(single)
	if len(got) != 1 || got[0].Value != "1" {
		t.Fatalf("single-kill effects = %#v, want one plain 1 pop", got)
	}

	short.KillCounter = false
	if got := generatedKillCounterEffects(short); got != nil {
		t.Fatalf("effects with counter disabled = %#v, want none", got)
	}
}

func TestKillMilestoneLabel(t *testing.T) {
	tests := []struct {
		kills int
		want  string
	}{
		{kills: 1, want: ""},
		{kills: 2, want: "2K"},
		{kills: 3, want: "3K"},
		{kills: 4, want: "4K"},
		{kills: 5, want: "ACE"},
		{kills: 7, want: "ACE"},
	}
	for _, tt := range tests {
		if got := killMilestoneLabel(tt.kills); got != tt.want {
			t.Errorf("killMilestoneLabel(%d) = %q, want %q", tt.kills, got, tt.want)
		}
	}
}

func TestGeneratedKillfeedEffects(t *testing.T) {
	short := ShortEdit{
		KillfeedOverlay: true,
		DurationSeconds: 8,
		Kills:           []KillCue{{TimeSeconds: 1.0, Tick: 100}, {TimeSeconds: 4.578, Tick: 329}},
	}
	effects := generatedKillfeedEffects(short)
	if len(effects) != 2 {
		t.Fatalf("effects = %#v, want one overlay per kill", effects)
	}
	first := effects[0]
	if first.Type != EffectKillfeed || first.X != "W-w-18" || first.Y != "300" {
		t.Fatalf("overlay = %#v, want killfeed near the top of the frame", first)
	}
	if first.CropX != 1558 || first.CropY != 64 || first.CropWidth != 360 || first.CropHeight != 110 || first.Width != 430 {
		t.Fatalf("overlay crop = %#v, want the static killfeed defaults", first)
	}
	if first.StartSeconds != 0.65 || first.EndSeconds != 3.8 || first.AtSeconds != 1.0 {
		t.Fatalf("overlay window = %.3f..%.3f at %.3f, want 0.65..3.80 at 1.0", first.StartSeconds, first.EndSeconds, first.AtSeconds)
	}
	if second := effects[1]; second.EndSeconds != 7.378 {
		t.Fatalf("second overlay end = %.3f, want clamped 7.378", second.EndSeconds)
	}

	short.KillfeedOverlay = false
	if got := generatedKillfeedEffects(short); got != nil {
		t.Fatalf("effects with overlay disabled = %#v, want none", got)
	}
	if got := generatedKillfeedEffects(ShortEdit{KillfeedOverlay: true}); got != nil {
		t.Fatalf("effects without kills = %#v, want none", got)
	}
}
