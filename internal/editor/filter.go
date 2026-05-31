package editor

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// effectColorPattern matches the FFmpeg colour forms accepted from Lua presets:
// a named colour, #RRGGBB, or 0xRRGGBB[AA], each optionally followed by
// @opacity. Anything else — notably ':' ',' '[' ']' ';' or whitespace — is
// rejected so a preset cannot smuggle extra filtergraph clauses or stream
// labels into a drawbox/drawtext colour argument.
var effectColorPattern = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9]*|#[0-9A-Fa-f]{6}|0x[0-9A-Fa-f]{6}(?:[0-9A-Fa-f]{2})?)(?:@[0-9]+(?:\.[0-9]+)?)?$`)

const (
	naturalHQ2FullSaturation     = 1.12
	naturalHQ2FullPlusContrast   = 1.02
	naturalHQ2FullPlusSaturation = 1.16
	naturalHQ2FullPlusGamma      = 1.00
)

// validateEffectColor rejects colour values that are not a plain FFmpeg colour
// spec. It validates the value exactly as given (callers trim before storing),
// so the validated form is the form that reaches the filtergraph. field is used
// only for the error message.
func validateEffectColor(field, value string) error {
	if !effectColorPattern.MatchString(value) {
		return fmt.Errorf("%s %q is not a valid color", field, value)
	}
	return nil
}

// effectPositionPattern matches the FFmpeg position expressions accepted from
// Lua presets: digits, identifiers (W, w, h, text_w, ...), arithmetic,
// parentheses, dots and spaces. It rejects ':' ',' ';' '[' ']' '=' quotes and
// newlines so a preset cannot smuggle extra filtergraph clauses through an x=/y=
// argument, which is interpolated unescaped into drawtext/overlay filters.
var effectPositionPattern = regexp.MustCompile(`^[A-Za-z0-9_.()+\-*/ ]+$`)

// validateEffectPosition rejects position values that are not a plain numeric or
// FFmpeg expression. field is used only for the error message.
func validateEffectPosition(field, value string) error {
	if !effectPositionPattern.MatchString(value) {
		return fmt.Errorf("%s %q is not a valid position expression", field, value)
	}
	return nil
}

func VideoFilter(short ShortEdit) string {
	if short.Preset == PresetShortNaturalHQ2Full || short.Preset == PresetShortNaturalHQ2FullPlus {
		return FullFrameVideoFilter(short)
	}
	scaleHeight := "1920"
	if expr := zoomHeightExpression(short.Effects); expr != "" {
		scaleHeight = "'" + expr + "'"
	}
	filters := []string{
		scaleFilter(scaleHeight, short),
		"crop=1080:1920:(iw-ow)/2:(ih-oh)/2",
		"setsar=1",
		"fps=60",
	}
	filters = appendTemporalSmoothingFilter(filters, short)
	filters = appendEffectFilters(filters, short.Effects)
	filters = append(filters, "format=yuv420p")
	return strings.Join(filters, ",")
}

func FullFrameVideoFilter(short ShortEdit) string {
	filters := []string{
		fullFrameScaleFilter(short),
		fullFrameGradeFilter(short),
	}
	if short.Preset == PresetShortNaturalHQ2FullPlus {
		filters = append(filters, "unsharp=5:5:0.35:3:3:0.15")
	}
	filters = append(filters,
		"pad=1080:1920:(ow-iw)/2:(oh-ih)/2:black",
		"setsar=1",
		"fps=60",
	)
	filters = appendTemporalSmoothingFilter(filters, short)
	filters = appendEffectFilters(filters, short.Effects)
	filters = append(filters, "format=yuv420p")
	return strings.Join(filters, ",")
}

func fullFrameGradeFilter(short ShortEdit) string {
	if short.Preset == PresetShortNaturalHQ2FullPlus {
		return fmt.Sprintf(
			"eq=contrast=%.3f:saturation=%.3f:gamma=%.3f",
			naturalHQ2FullPlusContrast,
			naturalHQ2FullPlusSaturation,
			naturalHQ2FullPlusGamma,
		)
	}
	return fmt.Sprintf("eq=saturation=%.3f", naturalHQ2FullSaturation)
}

func ViralSquareFilter(short ShortEdit) string {
	background := []string{
		viralSquareBackgroundScaleFilter(short),
		"crop=1080:1920:(iw-ow)/2:(ih-oh)/2",
		"boxblur=24:1",
		"setsar=1",
	}
	foregroundHeight := "1080"
	if expr := zoomHeightExpressionForBase(short.Effects, 1080); expr != "" {
		foregroundHeight = "'" + expr + "'"
	}
	foreground := []string{
		scaleFilter(foregroundHeight, short),
		"crop=1080:1080:(iw-ow)/2:(ih-oh)/2",
		"setsar=1",
	}
	foreground = appendTemporalSmoothingFilter(foreground, short)
	final := []string{"fps=60"}
	final = appendEffectFilters(final, short.Effects)
	images := imageEffects(short.Effects)
	killfeeds := killfeedEffects(short.Effects)
	final = append(final, "format=rgba")
	clauses := []string{
		fmt.Sprintf("[0:v]%s[bg]", strings.Join(background, ",")),
		fmt.Sprintf("[0:v]%s[fg]", strings.Join(foreground, ",")),
		"[bg][fg]overlay=x=0:y=420:format=auto[base]",
		fmt.Sprintf("[base]%s[vbase]", strings.Join(final, ",")),
	}
	current := "vbase"
	for i, effect := range killfeeds {
		killfeedLabel := fmt.Sprintf("kf%d", i)
		next := fmt.Sprintf("vkf%d", i)
		clauses = append(clauses,
			fmt.Sprintf("[0:v]%s[%s]", sourceCropFilter(effect, short), killfeedLabel),
			fmt.Sprintf("[%s][%s]overlay=x=%s:y=%s:format=auto:enable='%s'[%s]",
				current,
				killfeedLabel,
				effectPosition(effect.X, "W-w-18"),
				effectPosition(effect.Y, "438"),
				betweenExpression(effect.StartSeconds, effect.EndSeconds),
				next,
			),
		)
		current = next
	}
	for i, effect := range images {
		imageInput := i + 1
		imageLabel := fmt.Sprintf("img%d", i)
		next := fmt.Sprintf("vimg%d", i)
		if i == len(images)-1 {
			next = "vimages"
		}
		clauses = append(clauses,
			fmt.Sprintf("[%d:v]format=rgba,%s[%s]", imageInput, imageScaleFilter(effect), imageLabel),
			fmt.Sprintf("[%s][%s]overlay=x=%s:y=%s:format=auto:enable='%s'[%s]",
				current,
				imageLabel,
				effectPosition(effect.X, "(W-w)/2"),
				effectPosition(effect.Y, "72"),
				betweenExpression(effect.StartSeconds, effect.EndSeconds),
				next,
			),
		)
		current = next
	}
	clauses = append(clauses, fmt.Sprintf("[%s]format=yuv420p[v]", current))
	return strings.Join(clauses, ";")
}

func imageEffects(effects []Effect) []Effect {
	out := []Effect{}
	for _, effect := range effects {
		if effect.Type == EffectImage {
			out = append(out, effect)
		}
	}
	return out
}

func killfeedEffects(effects []Effect) []Effect {
	out := []Effect{}
	for _, effect := range effects {
		if effect.Type == EffectKillfeed {
			out = append(out, effect)
		}
	}
	return out
}

func imageScaleFilter(effect Effect) string {
	switch {
	case effect.Width > 0 && effect.Height > 0:
		return fmt.Sprintf("scale=w=%d:h=%d:flags=lanczos", effect.Width, effect.Height)
	case effect.Width > 0:
		return fmt.Sprintf("scale=w=%d:h=-1:flags=lanczos", effect.Width)
	case effect.Height > 0:
		return fmt.Sprintf("scale=w=-1:h=%d:flags=lanczos", effect.Height)
	default:
		return "scale=w=760:h=-1:flags=lanczos"
	}
}

func sourceCropFilter(effect Effect, short ShortEdit) string {
	cropWidth := effect.CropWidth
	if cropWidth == 0 {
		cropWidth = 360
	}
	cropHeight := effect.CropHeight
	if cropHeight == 0 {
		cropHeight = 110
	}
	filters := []string{
		scaleFilter("1080", short),
		fmt.Sprintf("crop=%d:%d:%d:%d", cropWidth, cropHeight, effect.CropX, effect.CropY),
		sourceCropScaleFilter(effect),
	}
	filters = append(filters, gradeFilters(short.Effects)...)
	filters = append(filters, "format=rgba")
	return strings.Join(filters, ",")
}

func sourceCropScaleFilter(effect Effect) string {
	switch {
	case effect.Width > 0 && effect.Height > 0:
		return fmt.Sprintf("scale=w=%d:h=%d:flags=lanczos", effect.Width, effect.Height)
	case effect.Width > 0:
		return fmt.Sprintf("scale=w=%d:h=-1:flags=lanczos", effect.Width)
	case effect.Height > 0:
		return fmt.Sprintf("scale=w=-1:h=%d:flags=lanczos", effect.Height)
	default:
		return "scale=w=430:h=-1:flags=lanczos"
	}
}

func effectPosition(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func viralSquareBackgroundScaleFilter(short ShortEdit) string {
	filter := "scale=1080:1920:force_original_aspect_ratio=increase"
	if short.HQFilters {
		filter += ":flags=" + hqScaleFlags(short)
	}
	return filter
}

func SmokeLineupSlowMotionFilter(short ShortEdit) string {
	window := smokeLineupSlowMotionWindow(short)
	base := VideoFilter(short)
	videoParts := []string{}
	audioParts := []string{}
	clauses := []string{
		fmt.Sprintf("[0:v]%s,split=%d%s", base, len(window.parts), splitLabels("vsrc", len(window.parts))),
		fmt.Sprintf("[0:a]asplit=%d%s", len(window.parts), splitLabels("asrc", len(window.parts))),
	}
	for i, part := range window.parts {
		videoLabel := fmt.Sprintf("v%d", i)
		audioLabel := fmt.Sprintf("a%d", i)
		videoParts = append(videoParts, "["+videoLabel+"]")
		audioParts = append(audioParts, "["+audioLabel+"]")
		videoFilters := []string{trimFilter(part.start, part.end), "setpts=PTS-STARTPTS"}
		audioFilters := []string{atrimFilter(part.start, part.end), "asetpts=PTS-STARTPTS"}
		if part.slow {
			videoFilters[1] = fmt.Sprintf("setpts=(PTS-STARTPTS)*%.3f", window.factor)
			audioFilters = append(audioFilters, atempoChain(1/window.factor)...)
		}
		clauses = append(clauses,
			fmt.Sprintf("[vsrc%d]%s[%s]", i, strings.Join(videoFilters, ","), videoLabel),
			fmt.Sprintf("[asrc%d]%s[%s]", i, strings.Join(audioFilters, ","), audioLabel),
		)
	}
	clauses = append(clauses,
		fmt.Sprintf("%sconcat=n=%d:v=1:a=0,fps=60,format=yuv420p[v]", strings.Join(videoParts, ""), len(videoParts)),
		fmt.Sprintf("%sconcat=n=%d:v=0:a=1%s", strings.Join(audioParts, ""), len(audioParts), smokeLineupAudioOutput(short)),
	)
	return strings.Join(clauses, ";")
}

type smokeLineupSlowMotionPart struct {
	start float64
	end   float64
	slow  bool
}

type smokeLineupSlowMotionPlan struct {
	factor float64
	parts  []smokeLineupSlowMotionPart
}

func smokeLineupSlowMotionWindowForTest(short ShortEdit) smokeLineupSlowMotionPlan {
	return smokeLineupSlowMotionWindow(short)
}

func smokeLineupSlowMotionWindow(short ShortEdit) smokeLineupSlowMotionPlan {
	const (
		preSeconds  = 1.15
		postSeconds = 0.95
		factor      = 2.5
	)
	smoke := short.Smokes[0]
	start := smoke.TimeSeconds - preSeconds
	if start < 0 {
		start = 0
	}
	end := smoke.TimeSeconds + postSeconds
	if short.DurationSeconds > 0 && end > short.DurationSeconds {
		end = short.DurationSeconds
	}
	if end <= start {
		end = start + 0.25
	}
	parts := make([]smokeLineupSlowMotionPart, 0, 3)
	if start > 0.001 {
		parts = append(parts, smokeLineupSlowMotionPart{start: 0, end: start})
	}
	parts = append(parts, smokeLineupSlowMotionPart{start: start, end: end, slow: true})
	if short.DurationSeconds <= 0 || end < short.DurationSeconds-0.001 {
		parts = append(parts, smokeLineupSlowMotionPart{start: end})
	}
	return smokeLineupSlowMotionPlan{factor: factor, parts: parts}
}

func splitLabels(prefix string, n int) string {
	labels := make([]string, 0, n)
	for i := 0; i < n; i++ {
		labels = append(labels, fmt.Sprintf("[%s%d]", prefix, i))
	}
	return strings.Join(labels, "")
}

func trimFilter(start, end float64) string {
	if end > 0 {
		return fmt.Sprintf("trim=start=%.3f:end=%.3f", start, end)
	}
	return fmt.Sprintf("trim=start=%.3f", start)
}

func atrimFilter(start, end float64) string {
	if end > 0 {
		return fmt.Sprintf("atrim=start=%.3f:end=%.3f", start, end)
	}
	return fmt.Sprintf("atrim=start=%.3f", start)
}

func atempoChain(speed float64) []string {
	if speed <= 0 {
		return nil
	}
	out := []string{}
	for speed < 0.5 {
		out = append(out, "atempo=0.5")
		speed /= 0.5
	}
	for speed > 2.0 {
		out = append(out, "atempo=2.0")
		speed /= 2.0
	}
	out = append(out, fmt.Sprintf("atempo=%.3f", speed))
	return out
}

func smokeLineupAudioOutput(short ShortEdit) string {
	if short.AudioNormalize {
		return ",loudnorm=I=-16:TP=-1.5:LRA=11[a]"
	}
	return "[a]"
}

func PremiumPlayerFilter(short ShortEdit) string {
	base := []string{
		scaleFilter("1920", short),
		"crop=1080:1920:(iw-ow)/2:(ih-oh)/2",
		"setsar=1",
		"fps=60",
	}
	base = appendTemporalSmoothingFilter(base, short)
	if expr := zoomHeightExpression(short.Effects); expr != "" {
		base[0] = scaleFilter("'"+expr+"'", short)
	}
	headline := short.Headline
	if headline == "" {
		headline = short.Label
	}
	if headline != "" {
		base = append(base, drawTextExpr(headline, "(w-text_w)/2", "82", 54, 0, 2.8, "black@0.96", "white@0.92", 22))
	}
	if short.Player != "" {
		base = append(base, drawTextExpr(short.Player, "(w-text_w)/2", "164", 32, 0, 2.8, "white@0.92", "black@0.32", 14))
	}
	base = appendEffectFilters(base, short.Effects)
	base = append(base, "format=rgba")

	player := "format=rgba"
	if key := ffmpegColor(short.PlayerKeyColor); key != "" {
		player += fmt.Sprintf(",chromakey=%s:0.09:0.03", key)
	}
	player += ",scale=-1:640"

	return fmt.Sprintf(
		"[0:v]%s[base];[1:v]%s[player];[base][player]overlay=x=(W-w)/2:y=H-h+36:format=auto,format=yuv420p[v]",
		strings.Join(base, ","),
		player,
	)
}

func scaleFilter(height string, short ShortEdit) string {
	filter := fmt.Sprintf("scale=w=-2:h=%s:eval=frame", height)
	if short.HQFilters {
		filter += ":flags=" + hqScaleFlags(short)
	}
	return filter
}

func fullFrameScaleFilter(short ShortEdit) string {
	filter := "scale=w=1080:h=1920:force_original_aspect_ratio=decrease:eval=frame"
	if short.HQFilters {
		filter += ":flags=" + hqScaleFlags(short)
	}
	return filter
}

func hqScaleFlags(short ShortEdit) string {
	if short.Preset == PresetShortNaturalHQ2FullPlus || short.Preset == PresetShortNaturalHQ3 || short.Preset == PresetShortNaturalHQ3Smooth {
		return "lanczos+accurate_rnd"
	}
	return "lanczos"
}

func appendTemporalSmoothingFilter(filters []string, short ShortEdit) []string {
	if !short.TemporalSmoothing {
		return filters
	}
	return append(filters, "tmix=frames=2:weights='1 2'")
}

func zoomHeightExpression(effects []Effect) string {
	return zoomHeightExpressionForBase(effects, 1920)
}

func zoomHeightExpressionForBase(effects []Effect, baseHeight float64) string {
	var terms []string
	for _, effect := range effects {
		if effect.Type != EffectZoom || effect.Scale <= 1 {
			continue
		}
		terms = append(terms, smoothZoomHeightExpressionForBase(effect, baseHeight))
	}
	if len(terms) == 0 {
		return ""
	}
	combined := terms[0]
	for _, term := range terms[1:] {
		combined = fmt.Sprintf("max(%s\\,%s)", combined, term)
	}
	return combined
}

func smoothZoomHeightExpression(effect Effect) string {
	return smoothZoomHeightExpressionForBase(effect, 1920)
}

func smoothZoomHeightExpressionForBase(effect Effect, baseHeight float64) string {
	start := effect.StartSeconds
	end := effect.EndSeconds
	at := effect.AtSeconds
	if at <= start || at >= end {
		at = start + (end-start)/2
	}
	if at <= start || end <= at {
		height := int(math.Round(baseHeight * effect.Scale))
		return fmt.Sprintf("if(%s\\,%d\\,%d)", betweenExpression(start, end), height, int(math.Round(baseHeight)))
	}
	peak := baseHeight * effect.Scale
	rise := smoothZoomRampExpression(start, at, baseHeight, peak)
	fall := smoothZoomRampExpression(at, end, peak, baseHeight)
	return fmt.Sprintf(
		"if(%s\\,%s\\,if(%s\\,%s\\,%d))",
		betweenExpression(start, at),
		rise,
		betweenExpression(at, end),
		fall,
		int(math.Round(baseHeight)),
	)
}

func smoothZoomRampExpression(start, end, from, to float64) string {
	duration := end - start
	if duration <= 0 {
		return fmt.Sprintf("%.3f", to)
	}
	// Smoothstep avoids a visible scale step at the beginning and end of a
	// scripted zoom while keeping the Lua API compact.
	t := fmt.Sprintf("((t-%.3f)/%.3f)", start, duration)
	return fmt.Sprintf("(%.3f+(%.3f-%.3f)*(%s*%s*(3-2*%s)))", from, to, from, t, t, t)
}

func appendEffectFilters(filters []string, effects []Effect) []string {
	filters = append(filters, gradeFilters(effects)...)
	for _, effect := range effects {
		if effect.Type != EffectFlash {
			continue
		}
		color := effect.Color
		if color == "" {
			color = "white"
		}
		if converted := ffmpegColor(color); converted != "" {
			color = converted
		}
		opacity := effect.Opacity
		if opacity == 0 {
			opacity = 0.18
		}
		filters = append(filters, fmt.Sprintf(
			"drawbox=x=0:y=0:w=iw:h=ih:color=%s@%.3f:t=fill:enable='%s'",
			color,
			opacity,
			betweenExpression(effect.StartSeconds, effect.EndSeconds),
		))
	}
	for _, effect := range effects {
		if effect.Type != EffectText {
			continue
		}
		x := effect.X
		if x == "" {
			x = "48"
		}
		y := effect.Y
		if y == "" {
			y = "72"
		}
		size := effect.Size
		if size == 0 {
			size = 32
		}
		fontColor := effect.FontColor
		if fontColor == "" {
			fontColor = "white@0.92"
		}
		boxColor := effect.BoxColor
		if boxColor == "" {
			boxColor = "black@0.36"
		}
		boxBorder := effect.BoxBorder
		if boxBorder == 0 {
			boxBorder = 12
		}
		filters = append(filters, drawTextExpr(effect.Value, x, y, size, effect.StartSeconds, effect.EndSeconds, fontColor, boxColor, boxBorder))
	}
	return filters
}

func gradeFilters(effects []Effect) []string {
	filters := []string{}
	for _, effect := range effects {
		if effect.Type != EffectGrade {
			continue
		}
		contrast := effect.Contrast
		if contrast == 0 {
			contrast = 1
		}
		saturation := effect.Saturation
		if saturation == 0 {
			saturation = 1
		}
		gamma := effect.Gamma
		if gamma == 0 {
			gamma = 1
		}
		filters = append(filters, fmt.Sprintf("eq=contrast=%.3f:saturation=%.3f:gamma=%.3f", contrast, saturation, gamma))
	}
	return filters
}

func drawText(text string, x, y, size int, start, end float64, fontColor, boxColor string, boxBorder int) string {
	return drawTextExpr(text, fmt.Sprintf("%d", x), fmt.Sprintf("%d", y), size, start, end, fontColor, boxColor, boxBorder)
}

func drawTextExpr(text, x, y string, size int, start, end float64, fontColor, boxColor string, boxBorder int) string {
	fontOption := ""
	if fontFile := drawtextFontFile(); fontFile != "" {
		fontOption = fmt.Sprintf(":fontfile='%s'", escapeDrawtextOption(filepath.ToSlash(fontFile)))
	}
	return fmt.Sprintf(
		"drawtext=text='%s'%s:x=%s:y=%s:fontsize=%d:fontcolor=%s:box=1:boxcolor=%s:boxborderw=%d:enable='%s'",
		escapeDrawtextText(text),
		fontOption,
		x,
		y,
		size,
		fontColor,
		boxColor,
		boxBorder,
		betweenExpression(start, end),
	)
}

func ffmpegColor(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "#") && len(raw) == 7 {
		return "0x" + raw[1:]
	}
	if strings.HasPrefix(raw, "0x") && len(raw) == 8 {
		return raw
	}
	switch strings.ToLower(raw) {
	case "black":
		return "0x000000"
	case "white":
		return "0xffffff"
	case "green":
		return "0x00ff00"
	case "magenta":
		return "0xff00ff"
	default:
		return raw
	}
}

// drawtextFontFile resolves the drawtext font path once per process. The font
// location is invariant for a run, so the filesystem probe in
// defaultDrawtextFontFile must not repeat for every drawtext clause and clip.
var drawtextFontFile = sync.OnceValue(defaultDrawtextFontFile)

func defaultDrawtextFontFile() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	for _, candidate := range []string{
		filepath.Join(`C:\Windows`, "Fonts", "arial.ttf"),
		filepath.Join(`C:\Windows`, "Fonts", "segoeui.ttf"),
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func betweenExpression(start, end float64) string {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	return fmt.Sprintf("between(t\\,%.3f\\,%.3f)", start, end)
}

func escapeDrawtextText(text string) string {
	return escapeDrawtextOption(text)
}

func escapeDrawtextOption(text string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		`:`, `\:`,
		`,`, `\,`,
		`[`, `\[`,
		`]`, `\]`,
		`%`, `\%`,
	)
	return replacer.Replace(text)
}
