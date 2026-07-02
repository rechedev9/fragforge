package editor

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/parse"
)

var effectsEvaluationTimeout = 2 * time.Second

// viralUltraCleanEffectsScript keeps the reel clean: no overlay lettering and
// no per-kill effects (zoom, flash, or killfeed overlay). It applies only a
// subtle colour grade over the raw HUD-less gameplay capture.
const viralUltraCleanEffectsScript = `
on_segment(function(s)
  grade({
    contrast = 1.18,
    saturation = 1.28,
    gamma = 1.02
  })
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
	case EffectsPresetViralUltraClean:
		return effectsSource{Preset: preset, Script: viralUltraCleanEffectsScript}, nil
	default:
		return effectsSource{}, fmt.Errorf("unknown effects preset %q", preset)
	}
}

func normalizeEffectsPreset(preset string) string {
	preset = strings.TrimSpace(preset)
	if preset == "" {
		return EffectsPresetViralUltraClean
	}
	return preset
}

func applyEffectsToManifest(manifest *Manifest, source effectsSource, ffmpegPath string, killfeedProbe func(input string, atSeconds float64) (image.Image, error)) error {
	manifest.EffectsPath = source.Path
	manifest.EffectsPreset = source.Preset
	// Compile the effects script once and reuse the bytecode for every short.
	// Re-parsing the same source per clip dominates a multi-clip render.
	proto, err := compileEffectsScript(source)
	if err != nil {
		return fmt.Errorf("compile effects script: %w", err)
	}
	for i := range manifest.Shorts {
		short := &manifest.Shorts[i]
		effects, warnings, err := evaluateCompiledEffects(proto, *short)
		if err != nil {
			return fmt.Errorf("evaluate effects for %s: %w", short.SegmentID, err)
		}
		effects = append(effects, generatedEditEffects(*short)...)
		short.Effects = effects
		manifest.Warnings = append(manifest.Warnings, refineKillfeedEffects(short, killfeedProbe)...)
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

func generatedEditEffects(short ShortEdit) []Effect {
	var effects []Effect
	effects = append(effects, generatedKillEffects(short)...)
	effects = append(effects, generatedTransitionEffects(short)...)
	effects = append(effects, generatedHookEffect(short)...)
	effects = append(effects, generatedKillCounterEffects(short)...)
	effects = append(effects, generatedKillfeedEffects(short)...)
	effects = append(effects, generatedBookendEffects(short)...)
	return effects
}

// generatedHookEffect draws the generated headline over the first ~2 seconds
// as the viral hook: centered in the top safe zone, above the killfeed
// overlay band, box-less with a drop shadow so it reads over bright skyboxes.
func generatedHookEffect(short ShortEdit) []Effect {
	if !short.HookText {
		return nil
	}
	title := bookendTitle(short)
	if title == "" {
		return nil
	}
	size := 64
	if isLandscapeOutput(short) {
		size = 52
	}
	return []Effect{{
		Type:           EffectText,
		Value:          title,
		X:              "(w-text_w)/2",
		Y:              "150",
		Size:           size,
		FontColor:      "white@0.96",
		BoxColor:       "none",
		ShadowColor:    "black@0.85",
		ShadowX:        3,
		ShadowY:        3,
		StartSeconds:   0,
		EndSeconds:     transitionEnd(short, 1.8),
		FadeInSeconds:  0.12,
		FadeOutSeconds: 0.35,
		Source:         "edit-request",
	}}
}

// generatedKillCounterEffects pops a running kill count at each kill cue, with
// a milestone label (2K/3K/4K/ACE) replacing the plain number on the final
// kill of a multi-kill. Pops end at the next kill when kills land closer than
// the pop window, so labels never stack.
func generatedKillCounterEffects(short ShortEdit) []Effect {
	if !short.KillCounter || len(short.Kills) == 0 {
		return nil
	}
	effects := make([]Effect, 0, len(short.Kills))
	for i, kill := range short.Kills {
		value := fmt.Sprintf("%d", i+1)
		size := 60
		if i == len(short.Kills)-1 && len(short.Kills) >= 2 {
			value = killMilestoneLabel(len(short.Kills))
			size = 72
		}
		start := clampSeconds(kill.TimeSeconds+0.05, 0, short.DurationSeconds)
		end := kill.TimeSeconds + 0.95
		if i+1 < len(short.Kills) && short.Kills[i+1].TimeSeconds < end {
			end = short.Kills[i+1].TimeSeconds
		}
		end = clampSeconds(end, 0, short.DurationSeconds)
		if end <= start {
			continue
		}
		effects = append(effects, Effect{
			Type:               EffectText,
			Value:              value,
			X:                  "(w-text_w)/2",
			Y:                  "h*0.62",
			Size:               size,
			FontColor:          "white@0.95",
			BoxColor:           "none",
			ShadowColor:        "black@0.85",
			ShadowX:            3,
			ShadowY:            3,
			StartSeconds:       start,
			EndSeconds:         end,
			FadeOutSeconds:     0.25,
			Source:             "edit-request",
			SourceKillTick:     kill.Tick,
			SourceKillWeapon:   kill.Weapon,
			SourceKillVictim:   kill.Victim,
			SourceKillHeadshot: kill.Headshot,
		})
	}
	return effects
}

// killMilestoneLabel names the multi-kill milestone for the final kill pop.
func killMilestoneLabel(kills int) string {
	switch {
	case kills >= 5:
		return "ACE"
	case kills == 4:
		return "4K"
	case kills == 3:
		return "3K"
	case kills == 2:
		return "2K"
	default:
		return ""
	}
}

// generatedKillfeedEffects re-overlays the source's kill notice for each kill,
// so the killfeed survives the 9:16 center crop that discards the 16:9
// top-right corner. The static crop defaults mirror the proven Lua killfeed
// defaults; refineKillfeedEffects replaces them with a per-kill probe of the
// real highlight box.
func generatedKillfeedEffects(short ShortEdit) []Effect {
	if !short.KillfeedOverlay || len(short.Kills) == 0 {
		return nil
	}
	effects := make([]Effect, 0, len(short.Kills))
	for _, kill := range short.Kills {
		start := clampSeconds(kill.TimeSeconds-0.35, 0, short.DurationSeconds)
		end := clampSeconds(kill.TimeSeconds+2.80, 0, short.DurationSeconds)
		if end <= start {
			continue
		}
		effects = append(effects, Effect{
			Type:               EffectKillfeed,
			StartSeconds:       start,
			EndSeconds:         end,
			AtSeconds:          kill.TimeSeconds,
			X:                  "W-w-18",
			Y:                  "300",
			Width:              430,
			CropX:              1558,
			CropY:              64,
			CropWidth:          360,
			CropHeight:         110,
			FadeInSeconds:      0.12,
			FadeOutSeconds:     0.30,
			Source:             "edit-request",
			SourceKillTick:     kill.Tick,
			SourceKillWeapon:   kill.Weapon,
			SourceKillVictim:   kill.Victim,
			SourceKillHeadshot: kill.Headshot,
		})
	}
	return effects
}

func generatedKillEffects(short ShortEdit) []Effect {
	switch short.KillEffect {
	case "", KillEffectClean:
		return nil
	}
	effects := []Effect{}
	for _, kill := range short.Kills {
		switch short.KillEffect {
		case KillEffectPunchIn:
			effects = append(effects, Effect{
				Type:         EffectZoom,
				StartSeconds: clampSeconds(kill.TimeSeconds-0.16, 0, short.DurationSeconds),
				EndSeconds:   clampSeconds(kill.TimeSeconds+0.42, 0, short.DurationSeconds),
				AtSeconds:    kill.TimeSeconds,
				Scale:        1.08,
				Source:       "edit-request",
			})
		case KillEffectVelocity:
			effects = append(effects,
				Effect{
					Type:         EffectZoom,
					StartSeconds: clampSeconds(kill.TimeSeconds-0.22, 0, short.DurationSeconds),
					EndSeconds:   clampSeconds(kill.TimeSeconds+0.50, 0, short.DurationSeconds),
					AtSeconds:    kill.TimeSeconds,
					Scale:        1.16,
					Source:       "edit-request",
				},
				Effect{
					Type:         EffectGrade,
					StartSeconds: clampSeconds(kill.TimeSeconds-0.10, 0, short.DurationSeconds),
					EndSeconds:   clampSeconds(kill.TimeSeconds+0.34, 0, short.DurationSeconds),
					Contrast:     1.08,
					Saturation:   1.18,
					Gamma:        1.00,
					Source:       "edit-request",
				},
			)
		case KillEffectFreezeFlash:
			effects = append(effects,
				Effect{
					Type:         EffectZoom,
					StartSeconds: clampSeconds(kill.TimeSeconds-0.12, 0, short.DurationSeconds),
					EndSeconds:   clampSeconds(kill.TimeSeconds+0.34, 0, short.DurationSeconds),
					AtSeconds:    kill.TimeSeconds,
					Scale:        1.10,
					Source:       "edit-request",
				},
				Effect{
					Type:         EffectFlash,
					StartSeconds: clampSeconds(kill.TimeSeconds-0.02, 0, short.DurationSeconds),
					EndSeconds:   clampSeconds(kill.TimeSeconds+0.10, 0, short.DurationSeconds),
					Color:        "white",
					Opacity:      0.34,
					Source:       "edit-request",
				},
			)
		}
	}
	return effects
}

func generatedTransitionEffects(short ShortEdit) []Effect {
	switch short.Transition {
	case "", TransitionCut:
		return nil
	case TransitionFlash:
		return []Effect{
			{Type: EffectFlash, StartSeconds: 0, EndSeconds: transitionEnd(short, 0.16), Color: "white", Opacity: 0.20, Source: "edit-request"},
			{Type: EffectFlash, StartSeconds: transitionStart(short, 0.18), EndSeconds: short.DurationSeconds, Color: "white", Opacity: 0.16, Source: "edit-request"},
		}
	case TransitionWhip:
		return []Effect{
			{Type: EffectZoom, StartSeconds: 0, EndSeconds: transitionEnd(short, 0.22), AtSeconds: 0.08, Scale: 1.12, Source: "edit-request"},
			{Type: EffectFlash, StartSeconds: 0, EndSeconds: transitionEnd(short, 0.12), Color: "white", Opacity: 0.12, Source: "edit-request"},
		}
	case TransitionDip:
		return []Effect{
			{Type: EffectFlash, StartSeconds: 0, EndSeconds: transitionEnd(short, 0.18), Color: "black", Opacity: 0.32, Source: "edit-request"},
			{Type: EffectFlash, StartSeconds: transitionStart(short, 0.22), EndSeconds: short.DurationSeconds, Color: "black", Opacity: 0.34, Source: "edit-request"},
		}
	default:
		return nil
	}
}

func generatedBookendEffects(short ShortEdit) []Effect {
	var effects []Effect
	width, height := outputDimensions(short)
	// The hook text already opens the short with the same headline, so the
	// intro title only draws when the hook is off — never both.
	if short.Intro && !short.HookText {
		effects = append(effects, Effect{
			Type:           EffectText,
			Value:          bookendTitle(short),
			X:              "48",
			Y:              fmt.Sprintf("%d", maxInt(48, height/12)),
			Size:           bookendFontSize(width),
			FontColor:      "white@0.96",
			BoxColor:       "black@0.44",
			BoxBorder:      16,
			StartSeconds:   0,
			EndSeconds:     transitionEnd(short, 1.45),
			FadeInSeconds:  0.10,
			FadeOutSeconds: 0.18,
			Source:         "edit-request",
		})
	}
	if short.Outro {
		effects = append(effects, Effect{
			Type:           EffectText,
			Value:          "FragForge",
			X:              "48",
			Y:              fmt.Sprintf("%d", maxInt(48, height-160)),
			Size:           bookendFontSize(width),
			FontColor:      "white@0.94",
			BoxColor:       "black@0.42",
			BoxBorder:      16,
			StartSeconds:   transitionStart(short, 1.35),
			EndSeconds:     short.DurationSeconds,
			FadeInSeconds:  0.14,
			FadeOutSeconds: 0.10,
			Source:         "edit-request",
		})
	}
	return effects
}

func bookendTitle(short ShortEdit) string {
	if short.Headline != "" {
		return short.Headline
	}
	if short.Label != "" {
		return short.Label
	}
	return "CS2 Highlight"
}

func bookendFontSize(width int) int {
	if width >= 1920 {
		return 44
	}
	return 36
}

func transitionEnd(short ShortEdit, duration float64) float64 {
	return clampSeconds(duration, 0, short.DurationSeconds)
}

func transitionStart(short ShortEdit, duration float64) float64 {
	if short.DurationSeconds <= 0 {
		return 0
	}
	return clampSeconds(short.DurationSeconds-duration, 0, short.DurationSeconds)
}

func clampSeconds(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if max > min && value > max {
		return max
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// compileEffectsScript parses and compiles the Lua effects source into reusable
// bytecode. It returns a nil proto (and nil error) when the source is empty so
// callers can treat "no script" as "no effects".
func compileEffectsScript(source effectsSource) (*lua.FunctionProto, error) {
	if strings.TrimSpace(source.Script) == "" {
		return nil, nil
	}
	chunk, err := parse.Parse(strings.NewReader(source.Script), "effects")
	if err != nil {
		return nil, err
	}
	return lua.Compile(chunk, "effects")
}

func evaluateEffects(source effectsSource, short ShortEdit) ([]Effect, []string, error) {
	proto, err := compileEffectsScript(source)
	if err != nil {
		return nil, nil, err
	}
	return evaluateCompiledEffects(proto, short)
}

func evaluateCompiledEffects(proto *lua.FunctionProto, short ShortEdit) ([]Effect, []string, error) {
	if proto == nil {
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

	L.Push(L.NewFunctionFromProto(proto))
	if err := L.PCall(0, lua.MultRet, nil); err != nil {
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
		var err error
		if e.Color, err = tableColor(tb, "color", "white"); err != nil {
			return e, err
		}
		// The flash drawbox appends its own @opacity, so the color must be bare;
		// an embedded @opacity would render an invalid double-opacity color.
		if strings.Contains(e.Color, "@") {
			return e, fmt.Errorf("flash color %q must not include @opacity; use the opacity field instead", e.Color)
		}
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
		var err error
		if e.X, err = tablePositionValidated(tb, "x", "48"); err != nil {
			return e, err
		}
		if e.Y, err = tablePositionValidated(tb, "y", "72"); err != nil {
			return e, err
		}
		e.Size = tableInt(tb, "size", 32)
		if e.Size <= 0 || e.Size > 240 {
			return e, fmt.Errorf("size %d outside range 1..240", e.Size)
		}
		e.FontFile = tableString(tb, "fontfile", "")
		if err := validateEffectFontFile(e.FontFile); err != nil {
			return e, err
		}
		if e.FontColor, err = tableColor(tb, "color", "white@0.92"); err != nil {
			return e, err
		}
		if e.BoxColor, err = tableColor(tb, "box_color", "black@0.36"); err != nil {
			return e, err
		}
		e.BoxBorder = tableInt(tb, "box_border", 12)
		if e.BoxBorder < 0 || e.BoxBorder > 128 {
			return e, fmt.Errorf("box_border %d outside range 0..128", e.BoxBorder)
		}
		// shadow_color is optional; an absent or empty value means no shadow,
		// so only validate (and read offsets) when one is set.
		e.ShadowColor = strings.TrimSpace(tableString(tb, "shadow_color", ""))
		if e.ShadowColor != "" {
			if err := validateEffectColor("shadow_color", e.ShadowColor); err != nil {
				return e, err
			}
			e.ShadowX = tableInt(tb, "shadow_x", 2)
			if e.ShadowX < -32 || e.ShadowX > 32 {
				return e, fmt.Errorf("shadow_x %d outside range -32..32", e.ShadowX)
			}
			e.ShadowY = tableInt(tb, "shadow_y", 2)
			if e.ShadowY < -32 || e.ShadowY > 32 {
				return e, fmt.Errorf("shadow_y %d outside range -32..32", e.ShadowY)
			}
		}
		e.StartSeconds, e.EndSeconds, e.AtSeconds = effectWindow(tb, defaultEventTime(ctx), 0, 1, 1, ctx.short.DurationSeconds)
		if err := setEffectFades(tb, &e); err != nil {
			return e, err
		}
	case EffectImage:
		e.Path = tableString(tb, "path", "")
		if strings.TrimSpace(e.Path) == "" {
			return e, fmt.Errorf("path is required")
		}
		var err error
		if e.X, err = tablePositionValidated(tb, "x", "(W-w)/2"); err != nil {
			return e, err
		}
		if e.Y, err = tablePositionValidated(tb, "y", "72"); err != nil {
			return e, err
		}
		e.Width = tableInt(tb, "width", 0)
		e.Height = tableInt(tb, "height", 0)
		if e.Width < 0 || e.Width > 2160 {
			return e, fmt.Errorf("width %d outside range 0..2160", e.Width)
		}
		if e.Height < 0 || e.Height > 3840 {
			return e, fmt.Errorf("height %d outside range 0..3840", e.Height)
		}
		e.StartSeconds, e.EndSeconds, e.AtSeconds = effectWindow(tb, defaultEventTime(ctx), 0, ctx.short.DurationSeconds, ctx.short.DurationSeconds, ctx.short.DurationSeconds)
		if err := setEffectFades(tb, &e); err != nil {
			return e, err
		}
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
		var err error
		if e.X, err = tablePositionValidated(tb, "x", "W-w-18"); err != nil {
			return e, err
		}
		if e.Y, err = tablePositionValidated(tb, "y", "438"); err != nil {
			return e, err
		}
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
		if err := setEffectFades(tb, &e); err != nil {
			return e, err
		}
	default:
		return e, fmt.Errorf("unknown effect type %q", typ)
	}
	if e.EndSeconds <= e.StartSeconds {
		return e, fmt.Errorf("end %.3f must be greater than start %.3f", e.EndSeconds, e.StartSeconds)
	}
	return e, nil
}

func setEffectFades(tb *lua.LTable, e *Effect) error {
	var err error
	e.FadeInSeconds, err = tableFadeSeconds(tb, "fade_in")
	if err != nil {
		return err
	}
	e.FadeOutSeconds, err = tableFadeSeconds(tb, "fade_out")
	if err != nil {
		return err
	}
	return nil
}

func tableFadeSeconds(tb *lua.LTable, key string) (float64, error) {
	value, ok := tableOptionalFloat(tb, key)
	if !ok {
		return 0, nil
	}
	if value < 0 || value > 5 {
		return 0, fmt.Errorf("%s %.3f outside range 0..5", key, value)
	}
	return value, nil
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

// tableColor reads a colour from a Lua table, trims it, and validates it as a
// plain FFmpeg colour spec. It returns the exact string it validated, so the
// stored value always matches what was checked (no validate/use gap).
func tableColor(tb *lua.LTable, key, def string) (string, error) {
	value := strings.TrimSpace(tableString(tb, key, def))
	return value, validateEffectColor(key, value)
}

// tablePositionValidated reads a position from a Lua table and validates it as a
// safe FFmpeg position expression, since it is interpolated unescaped into the
// drawtext/overlay filtergraph.
func tablePositionValidated(tb *lua.LTable, key, def string) (string, error) {
	value := tablePosition(tb, key, def)
	return value, validateEffectPosition(key, value)
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
