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
	width, height := outputDimensions(short)
	if presetUsesFullFrame(short.Preset) {
		return FullFrameVideoFilter(short)
	}
	scaleHeight := fmt.Sprintf("%d", height)
	if expr := zoomHeightExpression(short.Effects, height); expr != "" {
		scaleHeight = "'" + expr + "'"
	}
	filters := []string{
		scaleFilter(scaleHeight, short),
		fmt.Sprintf("crop=%d:%d:(iw-ow)/2:(ih-oh)/2", width, height),
		"setsar=1",
		fpsFilter(short),
	}
	filters = appendTemporalSmoothingFilter(filters, short)
	filters = appendEffectFilters(filters, short.Effects)
	filters = append(filters, "format=yuv420p")
	return strings.Join(filters, ",")
}

func FullFrameVideoFilter(short ShortEdit) string {
	width, height := outputDimensions(short)
	filters := []string{
		fullFrameBackgroundScaleFilter(short),
		fmt.Sprintf("crop=%d:%d:(iw-ow)/2:(ih-oh)/2", width, height),
	}
	filters = append(filters, "setsar=1")
	filters = append(filters,
		fpsFilter(short),
	)
	filters = appendTemporalSmoothingFilter(filters, short)
	filters = appendEffectFilters(filters, short.Effects)
	filters = append(filters, "format=yuv420p")
	return strings.Join(filters, ",")
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

func appendImageOverlayClauses(clauses []string, current string, imageInputStart int, images []Effect, short ShortEdit, outputLabel string) []string {
	for i, effect := range images {
		imageInput := imageInputStart + i
		imageLabel := fmt.Sprintf("img%d", i)
		next := fmt.Sprintf("vimg%d", i)
		if i == len(images)-1 {
			next = outputLabel
		}
		clauses = append(clauses,
			fmt.Sprintf("[%d:v]%s[%s]", imageInput, imageOverlayFilter(effect, short), imageLabel),
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
	return clauses
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

func imageOverlayFilter(effect Effect, short ShortEdit) string {
	filters := []string{
		"format=rgba",
		imageScaleFilter(effect),
	}
	if hasEffectFade(effect) {
		duration := short.DurationSeconds
		if duration <= 0 {
			duration = effect.EndSeconds
		}
		filters = append(filters,
			"loop=loop=-1:size=1:start=0",
			fmt.Sprintf("setpts=N/%d/TB", outputFPS(short)),
		)
		if duration > 0 {
			filters = append(filters, fmt.Sprintf("trim=duration=%.3f", duration))
		}
		filters = append(filters, overlayFadeFilters(effect)...)
	}
	return strings.Join(filters, ",")
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

func overlayFadeFilters(effect Effect) []string {
	fadeIn, fadeOut := normalizedFadeDurations(effect)
	filters := []string{}
	if fadeIn > 0 {
		filters = append(filters, fmt.Sprintf("fade=t=in:st=%.3f:d=%.3f:alpha=1", effect.StartSeconds, fadeIn))
	}
	if fadeOut > 0 {
		filters = append(filters, fmt.Sprintf("fade=t=out:st=%.3f:d=%.3f:alpha=1", effect.EndSeconds-fadeOut, fadeOut))
	}
	return filters
}

func hasEffectFade(effect Effect) bool {
	return effect.FadeInSeconds > 0 || effect.FadeOutSeconds > 0
}

func normalizedFadeDurations(effect Effect) (float64, float64) {
	fadeIn := effect.FadeInSeconds
	fadeOut := effect.FadeOutSeconds
	if fadeIn < 0 {
		fadeIn = 0
	}
	if fadeOut < 0 {
		fadeOut = 0
	}
	duration := effect.EndSeconds - effect.StartSeconds
	if duration <= 0 || fadeIn+fadeOut <= duration {
		return fadeIn, fadeOut
	}
	scale := duration / (fadeIn + fadeOut)
	return fadeIn * scale, fadeOut * scale
}

func effectPosition(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func scaleFilter(height string, short ShortEdit) string {
	filter := fmt.Sprintf("scale=w=-2:h=%s:eval=frame", height)
	if short.HQFilters {
		filter += ":flags=" + hqScaleFlags(short)
	}
	return filter
}

func fpsFilter(short ShortEdit) string {
	return fmt.Sprintf("fps=%d", outputFPS(short))
}

func outputFPS(short ShortEdit) int {
	if short.OutputFPS > 0 {
		return short.OutputFPS
	}
	return 60
}

func fullFrameBackgroundScaleFilter(short ShortEdit) string {
	width, height := outputDimensions(short)
	heightExpr := fmt.Sprintf("%d", height)
	if expr := zoomHeightExpression(short.Effects, height); expr != "" {
		heightExpr = "'" + expr + "'"
	}
	filter := fmt.Sprintf("scale=w=%d:h=%s:force_original_aspect_ratio=increase:eval=frame", width, heightExpr)
	if short.HQFilters {
		filter += ":flags=" + hqScaleFlags(short)
	}
	return filter
}

func hqScaleFlags(short ShortEdit) string {
	return "lanczos"
}

func appendTemporalSmoothingFilter(filters []string, short ShortEdit) []string {
	if !short.TemporalSmoothing {
		return filters
	}
	return append(filters, "tmix=frames=2:weights='1 2'")
}

func zoomHeightExpression(effects []Effect, baseHeight int) string {
	return zoomHeightExpressionForBase(effects, float64(baseHeight))
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
		styled := effect
		styled.X = x
		styled.Y = y
		styled.Size = size
		styled.FontColor = fontColor
		styled.BoxColor = boxColor
		styled.BoxBorder = boxBorder
		filters = append(filters, drawTextEffect(styled))
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
	return drawTextExprWithFade(text, x, y, size, start, end, fontColor, boxColor, boxBorder, "", 0, 0)
}

func drawTextExprWithFade(text, x, y string, size int, start, end float64, fontColor, boxColor string, boxBorder int, fontFile string, fadeIn, fadeOut float64) string {
	return drawTextEffect(Effect{
		Value:          text,
		X:              x,
		Y:              y,
		Size:           size,
		StartSeconds:   start,
		EndSeconds:     end,
		FontColor:      fontColor,
		BoxColor:       boxColor,
		BoxBorder:      boxBorder,
		FontFile:       fontFile,
		FadeInSeconds:  fadeIn,
		FadeOutSeconds: fadeOut,
	})
}

// drawTextEffect renders a text effect as a drawtext filter. BoxColor "none"
// disables the backing box entirely; a non-empty ShadowColor adds a drop
// shadow at the ShadowX/ShadowY offsets.
func drawTextEffect(effect Effect) string {
	fontOption := ""
	fontFile := strings.TrimSpace(effect.FontFile)
	if fontFile == "" {
		if effect.Bold {
			fontFile = boldDrawtextFontFile()
		} else {
			fontFile = drawtextFontFile()
		}
	}
	if fontFile != "" {
		fontOption = fmt.Sprintf(":fontfile='%s'", escapeDrawtextOption(filepath.ToSlash(fontFile)))
	}
	boxOption := "box=0"
	if effect.BoxColor != "none" {
		boxOption = fmt.Sprintf("box=1:boxcolor=%s:boxborderw=%d", effect.BoxColor, effect.BoxBorder)
	}
	shadowOption := ""
	if effect.ShadowColor != "" {
		shadowOption = fmt.Sprintf(":shadowcolor=%s:shadowx=%d:shadowy=%d", effect.ShadowColor, effect.ShadowX, effect.ShadowY)
	}
	borderOption := ""
	if effect.BorderWidth > 0 {
		borderColor := strings.TrimSpace(effect.BorderColor)
		if borderColor == "" {
			borderColor = "black@0.9"
		}
		borderOption = fmt.Sprintf(":borderw=%d:bordercolor=%s", effect.BorderWidth, borderColor)
	}
	alphaOption := ""
	if alpha := textAlphaExpression(effect.StartSeconds, effect.EndSeconds, effect.FadeInSeconds, effect.FadeOutSeconds); alpha != "" {
		alphaOption = fmt.Sprintf(":alpha='%s'", alpha)
	}
	return fmt.Sprintf(
		"drawtext=text='%s'%s:x=%s:y=%s:fontsize=%d:fontcolor=%s:%s%s%s%s:enable='%s'",
		escapeDrawtextText(effect.Value),
		fontOption,
		effect.X,
		effect.Y,
		effect.Size,
		effect.FontColor,
		boxOption,
		shadowOption,
		borderOption,
		alphaOption,
		betweenExpression(effect.StartSeconds, effect.EndSeconds),
	)
}

func textAlphaExpression(start, end, fadeIn, fadeOut float64) string {
	effect := Effect{
		StartSeconds:   start,
		EndSeconds:     end,
		FadeInSeconds:  fadeIn,
		FadeOutSeconds: fadeOut,
	}
	fadeIn, fadeOut = normalizedFadeDurations(effect)
	if fadeIn <= 0 && fadeOut <= 0 {
		return ""
	}
	expr := "1"
	if fadeIn > 0 {
		expr = fmt.Sprintf("min(%s\\,((t-%.3f)/%.3f))", expr, start, fadeIn)
	}
	if fadeOut > 0 {
		expr = fmt.Sprintf("min(%s\\,((%.3f-t)/%.3f))", expr, end, fadeOut)
	}
	return expr
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

// boldDrawtextFontFile resolves a bold/heavy font for viral-style titles,
// once per process like drawtextFontFile. It degrades gracefully: if none of
// the bold candidates exist it falls back to the regular drawtext font
// rather than failing the render.
var boldDrawtextFontFile = sync.OnceValue(defaultBoldDrawtextFontFile)

func defaultBoldDrawtextFontFile() string {
	if runtime.GOOS != "windows" {
		return drawtextFontFile()
	}
	for _, candidate := range []string{
		filepath.Join(`C:\Windows`, "Fonts", "ariblk.ttf"),  // Arial Black
		filepath.Join(`C:\Windows`, "Fonts", "arialbd.ttf"), // Arial Bold
		filepath.Join(`C:\Windows`, "Fonts", "seguisb.ttf"), // Segoe UI Semibold
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return drawtextFontFile()
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

func outputDimensions(short ShortEdit) (int, int) {
	if isLandscapeOutput(short) {
		return 1920, 1080
	}
	return 1080, 1920
}

func isLandscapeOutput(short ShortEdit) bool {
	return short.OutputFormat == OutputFormatLandscape16x9
}

func validateEffectFontFile(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.ContainsAny(value, "\r\n;") {
		return fmt.Errorf("fontfile contains unsupported characters")
	}
	return nil
}
