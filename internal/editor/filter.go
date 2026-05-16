package editor

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func VideoFilter(short ShortEdit) string {
	scaleHeight := "1920"
	if expr := zoomHeightExpression(short.Effects); expr != "" {
		scaleHeight = "'" + expr + "'"
	}
	filters := []string{
		fmt.Sprintf("scale=w=-2:h=%s:eval=frame", scaleHeight),
		"crop=1080:1920:(iw-ow)/2:(ih-oh)/2",
		"fps=60",
	}
	filters = appendEffectFilters(filters, short.Effects)
	filters = append(filters, "format=yuv420p")
	return strings.Join(filters, ",")
}

func PremiumPlayerFilter(short ShortEdit) string {
	base := []string{
		"scale=w=-2:h=1920:eval=frame",
		"crop=1080:1920:(iw-ow)/2:(ih-oh)/2",
		"fps=60",
	}
	if expr := zoomHeightExpression(short.Effects); expr != "" {
		base[0] = fmt.Sprintf("scale=w=-2:h='%s':eval=frame", expr)
	}
	headline := short.Headline
	if headline == "" {
		headline = short.Label
	}
	if headline != "" {
		base = append(base, drawTextExpr(headline, "(w-text_w)/2", "82", 54, 0, 2.8, "black@0.96", "white@0.92", 22))
	}
	if short.PrimaryWeapon != "" {
		base = append(base, drawTextExpr(short.PrimaryWeapon, "(w-text_w)/2", "164", 32, 0, 2.8, "white@0.92", "black@0.32", 14))
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

func zoomHeightExpression(effects []Effect) string {
	var terms []string
	for _, effect := range effects {
		if effect.Type != EffectZoom || effect.Scale <= 1 {
			continue
		}
		height := int(math.Round(1920 * effect.Scale))
		terms = append(terms, fmt.Sprintf("if(%s\\,%d\\,1920)", betweenExpression(effect.StartSeconds, effect.EndSeconds), height))
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

func appendEffectFilters(filters []string, effects []Effect) []string {
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

func drawText(text string, x, y, size int, start, end float64, fontColor, boxColor string, boxBorder int) string {
	return drawTextExpr(text, fmt.Sprintf("%d", x), fmt.Sprintf("%d", y), size, start, end, fontColor, boxColor, boxBorder)
}

func drawTextExpr(text, x, y string, size int, start, end float64, fontColor, boxColor string, boxBorder int) string {
	fontOption := ""
	if fontFile := defaultDrawtextFontFile(); fontFile != "" {
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

func defaultDrawtextFontFile() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	windir := os.Getenv("WINDIR")
	if windir == "" {
		windir = `C:\Windows`
	}
	for _, candidate := range []string{
		filepath.Join(windir, "Fonts", "arial.ttf"),
		filepath.Join(windir, "Fonts", "segoeui.ttf"),
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
