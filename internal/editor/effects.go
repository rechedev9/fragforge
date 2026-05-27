package editor

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

var effectsEvaluationTimeout = 2 * time.Second

const builtinCleanEffectsScript = `
on_segment(function(s)
  if s.preset == "short-clean" then
    text({
      value = s.label,
      start = 0,
      duration = 2.6,
      x = 48,
      y = 176,
      size = 34,
      color = "white@0.92",
      box_color = "black@0.42",
      box_border = 14
    })

    local footer = "CS2 HIGHLIGHT"
    if s.kill_count > 0 and s.primary_weapon ~= "" then
      footer = tostring(s.kill_count) .. "K " .. s.primary_weapon
    elseif s.primary_weapon ~= "" then
      footer = s.primary_weapon
    end
    text({
      value = footer,
      start = 0,
      duration = 2.6,
      x = "(w-text_w)/2",
      y = 1588,
      size = 46,
      color = "white@0.94",
      box_color = "black@0.40",
      box_border = 18
    })
  end
end)

on_kill(function(k)
  zoom({
    at = k.time,
    pre = 0.28,
    post = 0.72,
    scale = 1.040625
  })

  if k.preset == "short-clean" and k.weapon ~= "" then
    local label = k.weapon
    if k.headshot then label = label .. " HS" end
    text({
      value = label,
      at = k.time,
      pre = 0.15,
      post = 1.10,
      x = 48,
      y = 254,
      size = 30,
      color = "white@0.90",
      box_color = "black@0.30",
      box_border = 14
    })
  end
end)
`

const awpgodEffectsScript = `
on_segment(function(s)
  if s.preset == "short-clean" then
    text({
      value = s.label,
      start = 0,
      duration = 2.4,
      x = 48,
      y = 176,
      size = 34,
      color = "white@0.94",
      box_color = "black@0.42",
      box_border = 14
    })

    local footer = "CS2 HIGHLIGHT"
    if s.kill_count > 0 and s.primary_weapon ~= "" then
      footer = tostring(s.kill_count) .. "K " .. s.primary_weapon
    elseif s.primary_weapon ~= "" then
      footer = s.primary_weapon
    end
    text({
      value = footer,
      start = 0,
      duration = 2.4,
      x = "(w-text_w)/2",
      y = 1588,
      size = 52,
      color = "white@0.96",
      box_color = "black@0.44",
      box_border = 20
    })
  end

  grade({
    contrast = 1.08,
    saturation = 1.12,
    gamma = 1.02
  })
end)

on_kill(function(k)
  local scale = 1.045
  local post = 0.68

  if k.weapon == "AWP" then
    scale = 1.085
    post = 0.84
    flash({
      at = k.time,
      duration = 0.075,
      opacity = 0.16,
      color = "white"
    })
  elseif k.headshot then
    scale = 1.065
  end

  zoom({
    at = k.time,
    pre = 0.24,
    post = post,
    scale = scale
  })

  if k.preset == "short-clean" and k.weapon ~= "" then
    local label = k.weapon
    if k.headshot then label = label .. " HS" end
    if k.wallbang then label = label .. " WB" end

    text({
      value = label,
      at = k.time,
      pre = 0.12,
      post = 0.95,
      x = 48,
      y = 254,
      size = 30,
      color = "white@0.92",
      box_color = "black@0.32",
      box_border = 14
    })
  end
end)
`

const smokeLineupsEffectsScript = `
on_segment(function(segment)
  grade({
    contrast = 1.03,
    saturation = 1.24,
    gamma = 1.00
  })
end)

on_smoke(function(smoke)
  local duration = smoke.duration or 0
  local kind = "UTILITY"
  if smoke.type == "smokegrenade" then kind = "SMOKE" end
  if smoke.type == "flashbang" then kind = "FLASH" end
  if smoke.type == "molotov" or smoke.type == "incgrenade" then kind = "MOLLY" end

  local destination = smoke.destination
  if destination == "" then
    destination = "LINEUP"
  end

  local title = string.upper(destination .. " " .. kind)
  local subtitle = "UTILITY THROW"
  if smoke.from_area ~= "" then
    subtitle = "FROM " .. string.upper(smoke.from_area)
  end
  local action = smoke.throw_action
  if action ~= "" then
    local action_label = string.upper(string.gsub(action, "_", " "))
    if action == "jumpthrow" then
      if smoke.stance == "crouching" or smoke.stance == "crouching_in_progress" then
        action_label = "CROUCH JUMPTHROW"
      elseif smoke.stance == "standing" then
        action_label = "STANDING JUMPTHROW"
      end
    end
    subtitle = subtitle .. " · " .. action_label
  elseif smoke.movement ~= "" then
    subtitle = subtitle .. " · " .. string.upper(smoke.movement)
  end

  if duration == 0 or smoke.time < duration - 0.1 then
    text({
      value = title,
      start = smoke.time,
      duration = 2.75,
      x = 58,
      y = 1368,
      size = 58,
      color = "white@0.97",
      box_color = "0x2a1190@0.92",
      box_border = 22
    })

    text({
      value = subtitle,
      start = smoke.time,
      duration = 2.75,
      x = 58,
      y = 1450,
      size = 34,
      color = "white@0.96",
      box_color = "black@0.84",
      box_border = 14
    })
  end
end)
`

type effectsSource struct {
	Path   string
	Preset string
	Script string
}

type effectEvalContext struct {
	short            ShortEdit
	effects          []Effect
	warnings         []string
	sourceName       string
	sourceIndex      int
	sourceKill       *KillCue
	sourceSmoke      *SmokeCue
	segmentCallbacks []lua.LValue
	killCallbacks    []lua.LValue
	smokeCallbacks   []lua.LValue
}

func loadEffectsSource(path, preset string) (effectsSource, error) {
	path = strings.TrimSpace(path)
	if path != "" {
		abs, err := filepath.Abs(path)
		if err != nil {
			return effectsSource{}, fmt.Errorf("resolve effects script path: %w", err)
		}
		// #nosec G304 -- effects script path is an explicit local CLI/config input.
		b, err := os.ReadFile(abs)
		if err != nil {
			return effectsSource{}, fmt.Errorf("read effects script: %w", err)
		}
		return effectsSource{Path: abs, Preset: EffectsPresetExternal, Script: string(b)}, nil
	}
	preset = normalizeEffectsPreset(preset)
	switch preset {
	case EffectsPresetBuiltinClean:
		return effectsSource{Preset: preset, Script: builtinCleanEffectsScript}, nil
	case EffectsPresetAWPGod:
		return effectsSource{Preset: preset, Script: awpgodEffectsScript}, nil
	case EffectsPresetSmokeLineups:
		return effectsSource{Preset: preset, Script: smokeLineupsEffectsScript}, nil
	case EffectsPresetNone:
		return effectsSource{Preset: preset}, nil
	default:
		return effectsSource{}, fmt.Errorf("unknown effects preset %q", preset)
	}
}

func normalizeEffectsPreset(preset string) string {
	preset = strings.TrimSpace(preset)
	if preset == "" {
		return EffectsPresetBuiltinClean
	}
	return preset
}

func applyEffectsToManifest(manifest *Manifest, source effectsSource, ffmpegPath string) error {
	manifest.EffectsPath = source.Path
	manifest.EffectsPreset = source.Preset
	for i := range manifest.Shorts {
		short := &manifest.Shorts[i]
		effects, warnings, err := evaluateEffects(source, *short)
		if err != nil {
			return fmt.Errorf("evaluate effects for %s: %w", short.SegmentID, err)
		}
		short.Effects = effects
		short.FFmpegCommand = BuildFFmpegCommand(ffmpegPath, *short)
		if short.CoverPath != "" {
			short.CoverCommand = BuildCoverFFmpegCommand(ffmpegPath, *short)
		}
		if short.CoverSheetPath != "" {
			short.CoverSheetCommand = BuildCoverSheetFFmpegCommand(ffmpegPath, *short)
		}
		if short.QualityLogPath != "" {
			short.QualityCommand = BuildQualityCheckFFmpegCommand(ffmpegPath, *short)
		}
		manifest.Warnings = append(manifest.Warnings, warnings...)
	}
	return nil
}

func evaluateEffects(source effectsSource, short ShortEdit) ([]Effect, []string, error) {
	if strings.TrimSpace(source.Script) == "" {
		return nil, nil, nil
	}
	ctx := &effectEvalContext{
		short: short,
	}
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer L.Close()
	evalCtx, cancel := context.WithTimeout(context.Background(), effectsEvaluationTimeout)
	defer cancel()
	L.SetContext(evalCtx)
	openEffectsLuaLibs(L)
	registerEffectsAPI(L, ctx)

	if err := L.DoString(source.Script); err != nil {
		return nil, nil, err
	}
	if err := callCallbacks(L, ctx.segmentCallbacks, short.segmentLuaTable(L), "segment", ctx); err != nil {
		return nil, nil, err
	}
	for i, kill := range short.Kills {
		ctx.sourceName = "kill"
		ctx.sourceIndex = i + 1
		ctx.sourceKill = &kill
		ctx.sourceSmoke = nil
		if err := callCallbacks(L, ctx.killCallbacks, killLuaTable(L, short, kill), "kill", ctx); err != nil {
			return nil, nil, err
		}
	}
	for i, smoke := range short.Smokes {
		ctx.sourceName = "smoke"
		ctx.sourceIndex = i + 1
		ctx.sourceKill = nil
		ctx.sourceSmoke = &smoke
		if err := callCallbacks(L, ctx.smokeCallbacks, smokeLuaTable(L, short, smoke), "smoke", ctx); err != nil {
			return nil, nil, err
		}
	}
	return ctx.effects, ctx.warnings, nil
}

func openEffectsLuaLibs(L *lua.LState) {
	lua.OpenBase(L)
	lua.OpenTable(L)
	lua.OpenString(L)
	lua.OpenMath(L)
	for _, name := range []string{"dofile", "loadfile", "require", "collectgarbage", "print"} {
		L.SetGlobal(name, lua.LNil)
	}
}

func registerEffectsAPI(L *lua.LState, ctx *effectEvalContext) {
	L.SetGlobal("on_segment", L.NewFunction(func(L *lua.LState) int {
		ctx.segmentCallbacks = append(ctx.segmentCallbacks, L.CheckFunction(1))
		return 0
	}))
	L.SetGlobal("on_kill", L.NewFunction(func(L *lua.LState) int {
		ctx.killCallbacks = append(ctx.killCallbacks, L.CheckFunction(1))
		return 0
	}))
	L.SetGlobal("on_smoke", L.NewFunction(func(L *lua.LState) int {
		ctx.smokeCallbacks = append(ctx.smokeCallbacks, L.CheckFunction(1))
		return 0
	}))
	L.SetGlobal("zoom", L.NewFunction(func(L *lua.LState) int {
		ctx.addEffectFromTable(L, EffectZoom)
		return 0
	}))
	L.SetGlobal("flash", L.NewFunction(func(L *lua.LState) int {
		ctx.addEffectFromTable(L, EffectFlash)
		return 0
	}))
	L.SetGlobal("text", L.NewFunction(func(L *lua.LState) int {
		ctx.addEffectFromTable(L, EffectText)
		return 0
	}))
	L.SetGlobal("image", L.NewFunction(func(L *lua.LState) int {
		ctx.addEffectFromTable(L, EffectImage)
		return 0
	}))
	L.SetGlobal("killfeed", L.NewFunction(func(L *lua.LState) int {
		ctx.addEffectFromTable(L, EffectKillfeed)
		return 0
	}))
	L.SetGlobal("grade", L.NewFunction(func(L *lua.LState) int {
		ctx.addEffectFromTable(L, EffectGrade)
		return 0
	}))
}

func callCallbacks(L *lua.LState, callbacks []lua.LValue, arg lua.LValue, label string, ctx *effectEvalContext) error {
	ctx.sourceName = label
	for i, fn := range callbacks {
		ctx.sourceIndex = i + 1
		if err := L.CallByParam(lua.P{Fn: fn, NRet: 0, Protect: true}, arg); err != nil {
			return fmt.Errorf("%s callback %d: %w", label, i+1, err)
		}
	}
	return nil
}

func (ctx *effectEvalContext) addEffectFromTable(L *lua.LState, typ EffectType) {
	tb := L.CheckTable(1)
	effect, err := ctx.effectFromTable(tb, typ)
	if err != nil {
		L.RaiseError("%s effect: %s", typ, err)
		return
	}
	ctx.effects = append(ctx.effects, effect)
}

func (ctx *effectEvalContext) effectFromTable(tb *lua.LTable, typ EffectType) (Effect, error) {
	e := Effect{
		Type:            typ,
		Source:          ctx.sourceName,
		SourceIndex:     ctx.sourceIndex,
		SourceSegmentID: ctx.short.SegmentID,
	}
	if ctx.sourceKill != nil {
		e.SourceKillTick = ctx.sourceKill.Tick
		e.SourceKillWeapon = ctx.sourceKill.Weapon
		e.SourceKillVictim = ctx.sourceKill.Victim
		e.SourceKillHeadshot = ctx.sourceKill.Headshot
	}
	if ctx.sourceSmoke != nil {
		e.SourceSmokeID = ctx.sourceSmoke.ID
		e.SourceSmokeType = ctx.sourceSmoke.Type
		e.SourceSmokeTarget = ctx.sourceSmoke.Destination
	}

	switch typ {
	case EffectZoom:
		e.Scale = tableFloat(tb, "scale", 1.04)
		if e.Scale < 1 || e.Scale > 2.5 {
			return e, fmt.Errorf("scale %.3f outside range 1.0..2.5", e.Scale)
		}
		e.StartSeconds, e.EndSeconds, e.AtSeconds = effectWindow(tb, defaultEventTime(ctx), 0.28, 0.72, 1, ctx.short.DurationSeconds)
	case EffectFlash:
		e.Color = tableString(tb, "color", "white")
		e.Opacity = tableFloat(tb, "opacity", 0.18)
		if e.Opacity < 0 || e.Opacity > 1 {
			return e, fmt.Errorf("opacity %.3f outside range 0..1", e.Opacity)
		}
		e.StartSeconds, e.EndSeconds, e.AtSeconds = effectWindow(tb, defaultEventTime(ctx), 0, 0.08, 0.08, ctx.short.DurationSeconds)
	case EffectText:
		e.Value = tableString(tb, "value", "")
		if strings.TrimSpace(e.Value) == "" {
			return e, fmt.Errorf("value is required")
		}
		e.X = tablePosition(tb, "x", "48")
		e.Y = tablePosition(tb, "y", "72")
		e.Size = tableInt(tb, "size", 32)
		if e.Size <= 0 || e.Size > 240 {
			return e, fmt.Errorf("size %d outside range 1..240", e.Size)
		}
		e.FontColor = tableString(tb, "color", "white@0.92")
		e.BoxColor = tableString(tb, "box_color", "black@0.36")
		e.BoxBorder = tableInt(tb, "box_border", 12)
		if e.BoxBorder < 0 || e.BoxBorder > 128 {
			return e, fmt.Errorf("box_border %d outside range 0..128", e.BoxBorder)
		}
		e.StartSeconds, e.EndSeconds, e.AtSeconds = effectWindow(tb, defaultEventTime(ctx), 0, 1, 1, ctx.short.DurationSeconds)
	case EffectImage:
		e.Path = tableString(tb, "path", "")
		if strings.TrimSpace(e.Path) == "" {
			return e, fmt.Errorf("path is required")
		}
		e.X = tablePosition(tb, "x", "(W-w)/2")
		e.Y = tablePosition(tb, "y", "72")
		e.Width = tableInt(tb, "width", 0)
		e.Height = tableInt(tb, "height", 0)
		if e.Width < 0 || e.Width > 2160 {
			return e, fmt.Errorf("width %d outside range 0..2160", e.Width)
		}
		if e.Height < 0 || e.Height > 3840 {
			return e, fmt.Errorf("height %d outside range 0..3840", e.Height)
		}
		e.StartSeconds, e.EndSeconds, e.AtSeconds = effectWindow(tb, defaultEventTime(ctx), 0, ctx.short.DurationSeconds, ctx.short.DurationSeconds, ctx.short.DurationSeconds)
	case EffectGrade:
		e.Contrast = tableFloat(tb, "contrast", 1)
		e.Saturation = tableFloat(tb, "saturation", 1)
		e.Gamma = tableFloat(tb, "gamma", 1)
		if e.Contrast <= 0 || e.Contrast > 4 {
			return e, fmt.Errorf("contrast %.3f outside range 0..4", e.Contrast)
		}
		if e.Saturation < 0 || e.Saturation > 4 {
			return e, fmt.Errorf("saturation %.3f outside range 0..4", e.Saturation)
		}
		if e.Gamma <= 0 || e.Gamma > 4 {
			return e, fmt.Errorf("gamma %.3f outside range 0..4", e.Gamma)
		}
		e.StartSeconds, e.EndSeconds, e.AtSeconds = effectWindow(tb, 0, 0, ctx.short.DurationSeconds, ctx.short.DurationSeconds, ctx.short.DurationSeconds)
	case EffectKillfeed:
		e.X = tablePosition(tb, "x", "W-w-18")
		e.Y = tablePosition(tb, "y", "438")
		e.Width = tableInt(tb, "width", 430)
		e.Height = tableInt(tb, "height", 0)
		e.CropX = tableInt(tb, "crop_x", 1558)
		e.CropY = tableInt(tb, "crop_y", 64)
		e.CropWidth = tableInt(tb, "crop_width", 360)
		e.CropHeight = tableInt(tb, "crop_height", 110)
		if e.Width < 0 || e.Width > 2160 {
			return e, fmt.Errorf("width %d outside range 0..2160", e.Width)
		}
		if e.Height < 0 || e.Height > 3840 {
			return e, fmt.Errorf("height %d outside range 0..3840", e.Height)
		}
		if e.CropX < 0 || e.CropX > 3840 {
			return e, fmt.Errorf("crop_x %d outside range 0..3840", e.CropX)
		}
		if e.CropY < 0 || e.CropY > 2160 {
			return e, fmt.Errorf("crop_y %d outside range 0..2160", e.CropY)
		}
		if e.CropWidth <= 0 || e.CropWidth > 3840 {
			return e, fmt.Errorf("crop_width %d outside range 1..3840", e.CropWidth)
		}
		if e.CropHeight <= 0 || e.CropHeight > 2160 {
			return e, fmt.Errorf("crop_height %d outside range 1..2160", e.CropHeight)
		}
		e.StartSeconds, e.EndSeconds, e.AtSeconds = effectWindow(tb, defaultEventTime(ctx), 0.35, 2.80, 3.15, ctx.short.DurationSeconds)
	default:
		return e, fmt.Errorf("unknown effect type %q", typ)
	}
	if e.EndSeconds <= e.StartSeconds {
		return e, fmt.Errorf("end %.3f must be greater than start %.3f", e.EndSeconds, e.StartSeconds)
	}
	return e, nil
}

func defaultEventTime(ctx *effectEvalContext) float64 {
	if ctx.sourceKill != nil {
		return ctx.sourceKill.TimeSeconds
	}
	if ctx.sourceSmoke != nil {
		return ctx.sourceSmoke.TimeSeconds
	}
	return 0
}

func effectWindow(tb *lua.LTable, defaultAt, defaultPre, defaultPost, defaultDuration, clipDuration float64) (float64, float64, float64) {
	start, hasStart := tableOptionalFloat(tb, "start")
	end, hasEnd := tableOptionalFloat(tb, "end")
	at, hasAt := tableOptionalFloat(tb, "at")
	duration, hasDuration := tableOptionalFloat(tb, "duration")
	pre, hasPre := tableOptionalFloat(tb, "pre")
	post, hasPost := tableOptionalFloat(tb, "post")

	if !hasAt {
		at = defaultAt
	}
	if hasStart {
		at = start
		if hasDuration {
			end = start + duration
		} else if !hasEnd {
			end = start + defaultDuration
		}
	} else {
		if hasDuration && !hasPre && !hasPost {
			start = at
			end = at + duration
		} else {
			if !hasPre {
				pre = defaultPre
			}
			if !hasPost {
				post = defaultPost
			}
			start = at - pre
			end = at + post
		}
	}
	if start < 0 {
		start = 0
	}
	if clipDuration > 0 && end > clipDuration {
		end = clipDuration
	}
	return roundMillis(start), roundMillis(end), roundMillis(at)
}

func roundMillis(v float64) float64 {
	return math.Round(v*1000) / 1000
}

func tableString(tb *lua.LTable, key, def string) string {
	value := tb.RawGetString(key)
	if value == lua.LNil {
		return def
	}
	return lua.LVAsString(value)
}

func tablePosition(tb *lua.LTable, key, def string) string {
	value := tb.RawGetString(key)
	switch v := value.(type) {
	case lua.LNumber:
		return fmt.Sprintf("%.0f", float64(v))
	case lua.LString:
		return string(v)
	default:
		return def
	}
}

func tableInt(tb *lua.LTable, key string, def int) int {
	value := tb.RawGetString(key)
	if n, ok := value.(lua.LNumber); ok {
		return int(math.Round(float64(n)))
	}
	return def
}

func tableFloat(tb *lua.LTable, key string, def float64) float64 {
	if value, ok := tableOptionalFloat(tb, key); ok {
		return value
	}
	return def
}

func tableOptionalFloat(tb *lua.LTable, key string) (float64, bool) {
	value := tb.RawGetString(key)
	if n, ok := value.(lua.LNumber); ok {
		return float64(n), true
	}
	return 0, false
}

func (short ShortEdit) segmentLuaTable(L *lua.LState) *lua.LTable {
	tb := L.NewTable()
	tb.RawSetString("id", lua.LString(short.SegmentID))
	tb.RawSetString("preset", lua.LString(short.Preset))
	tb.RawSetString("player", lua.LString(short.Player))
	tb.RawSetString("map", lua.LString(short.Map))
	tb.RawSetString("kill_count", lua.LNumber(short.KillCount))
	tb.RawSetString("smoke_count", lua.LNumber(short.SmokeCount))
	tb.RawSetString("utility_count", lua.LNumber(short.SmokeCount))
	tb.RawSetString("primary_weapon", lua.LString(short.PrimaryWeapon))
	tb.RawSetString("primary_smoke", lua.LString(short.PrimarySmoke))
	tb.RawSetString("label", lua.LString(short.Label))
	tb.RawSetString("headline", lua.LString(short.Headline))
	tb.RawSetString("duration", lua.LNumber(short.DurationSeconds))
	return tb
}

func smokeLuaTable(L *lua.LState, short ShortEdit, smoke SmokeCue) *lua.LTable {
	tb := L.NewTable()
	tb.RawSetString("segment_id", lua.LString(short.SegmentID))
	tb.RawSetString("preset", lua.LString(short.Preset))
	tb.RawSetString("duration", lua.LNumber(short.DurationSeconds))
	tb.RawSetString("id", lua.LString(smoke.ID))
	tb.RawSetString("type", lua.LString(smoke.Type))
	tb.RawSetString("round", lua.LNumber(smoke.Round))
	tb.RawSetString("throw_tick", lua.LNumber(smoke.ThrowTick))
	tb.RawSetString("pop_tick", lua.LNumber(smoke.PopTick))
	tb.RawSetString("expire_tick", lua.LNumber(smoke.ExpireTick))
	tb.RawSetString("time", lua.LNumber(smoke.TimeSeconds))
	tb.RawSetString("pop_time", lua.LNumber(smoke.PopTimeSeconds))
	tb.RawSetString("throw_place", lua.LString(smoke.ThrowPlace))
	tb.RawSetString("throw_action", lua.LString(smoke.ThrowAction))
	tb.RawSetString("stance", lua.LString(smoke.Stance))
	tb.RawSetString("movement", lua.LString(smoke.Movement))
	tb.RawSetString("speed_2d", lua.LNumber(smoke.Speed2D))
	tb.RawSetString("on_ground", lua.LBool(smoke.OnGround))
	tb.RawSetString("walking", lua.LBool(smoke.Walking))
	tb.RawSetString("ducking", lua.LBool(smoke.Ducking))
	tb.RawSetString("destination", lua.LString(smoke.Destination))
	tb.RawSetString("from_area", lua.LString(smoke.FromArea))
	tb.RawSetString("side", lua.LString(smoke.Side))
	tb.RawSetString("match_id", lua.LString(smoke.MatchID))
	tb.RawSetString("confidence", lua.LNumber(smoke.Confidence))
	tb.RawSetString("distance_units", lua.LNumber(smoke.DistanceUnits))
	tb.RawSetString("landing_x", lua.LNumber(smoke.LandingPos[0]))
	tb.RawSetString("landing_y", lua.LNumber(smoke.LandingPos[1]))
	tb.RawSetString("landing_z", lua.LNumber(smoke.LandingPos[2]))
	tb.RawSetString("throw_x", lua.LNumber(smoke.ThrowPos[0]))
	tb.RawSetString("throw_y", lua.LNumber(smoke.ThrowPos[1]))
	tb.RawSetString("throw_z", lua.LNumber(smoke.ThrowPos[2]))
	tb.RawSetString("matched", lua.LBool(smoke.Matched))
	return tb
}

func killLuaTable(L *lua.LState, short ShortEdit, kill KillCue) *lua.LTable {
	tb := L.NewTable()
	tb.RawSetString("segment_id", lua.LString(short.SegmentID))
	tb.RawSetString("preset", lua.LString(short.Preset))
	tb.RawSetString("tick", lua.LNumber(kill.Tick))
	tb.RawSetString("time", lua.LNumber(kill.TimeSeconds))
	tb.RawSetString("weapon", lua.LString(kill.Weapon))
	tb.RawSetString("victim", lua.LString(kill.Victim))
	tb.RawSetString("headshot", lua.LBool(kill.Headshot))
	tb.RawSetString("wallbang", lua.LBool(kill.Wallbang))
	return tb
}
