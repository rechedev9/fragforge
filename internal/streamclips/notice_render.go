package streamclips

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"path"
	"sort"
	"strings"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// KillfeedNoticeHeight is the pixel height of every rendered kill notice.
const KillfeedNoticeHeight = 48

// Layout constants for the synthetic CS2 highlighted kill notice. They mirror
// the community killfeed generators: a translucent black plate with a solid red
// border, white weapon and flag icons, and team-colored player names.
const (
	noticeHPadding = 10 // horizontal padding inside the border, each side
	noticeGap      = 6  // gap between adjacent content elements
	noticeBorder   = 2  // border thickness in px
	weaponIconH    = 26 // weapon icon target height
	flagIconH      = 22 // flag icon target height
	nameFontSize   = 24 // Rajdhani-Bold size in points at 72 DPI
	nameFontDPI    = 72
)

var (
	plateFill   = color.RGBA{R: 0, G: 0, B: 0, A: 127}
	borderColor = color.RGBA{R: 0xB5, G: 0x00, B: 0x00, A: 0xFF}
	colorT      = color.RGBA{R: 0xEC, G: 0xCE, B: 0x51, A: 0xFF}
	colorCT     = color.RGBA{R: 0x71, G: 0xA6, B: 0xFF, A: 0xFF}
	colorWhite  = color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
)

//go:embed noticeassets/assets
var noticeAssets embed.FS

const (
	assetsRoot  = "noticeassets/assets"
	weaponsDir  = assetsRoot + "/weapons"
	flagsDir    = assetsRoot + "/flags"
	flashAssist = "flashbang_assist"
)

// weaponCatalog is the sorted list of valid weapon keys and its set form,
// derived once from the embedded weapons directory.
var weaponCatalog = sync.OnceValues(func() ([]string, map[string]bool) {
	entries, err := noticeAssets.ReadDir(weaponsDir)
	if err != nil {
		// The directory is embedded at build time; a read failure is a bug.
		panic(fmt.Sprintf("streamclips: reading embedded weapons dir: %v", err))
	}
	keys := make([]string, 0, len(entries))
	set := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".png") {
			continue
		}
		key := strings.TrimSuffix(name, ".png")
		if key == flashAssist {
			continue // an assist flag, not a selectable weapon
		}
		keys = append(keys, key)
		set[key] = true
	}
	sort.Strings(keys)
	return keys, set
})

type iconCacheKey struct {
	assetPath string
	targetH   int
}

type iconCacheEntry struct {
	once sync.Once
	img  image.Image
	err  error
}

// scaledIconCache avoids decoding and Catmull-Rom scaling immutable embedded
// icons for every rendered kill. Each asset/height pair initializes once even
// when several render workers request it concurrently.
var scaledIconCache sync.Map

// noticeFace is the parsed Rajdhani-Bold face used for player names.
var noticeFace = sync.OnceValues(func() (font.Face, error) {
	data, err := noticeAssets.ReadFile(assetsRoot + "/Rajdhani-Bold.ttf")
	if err != nil {
		return nil, fmt.Errorf("reading notice font: %w", err)
	}
	parsed, err := opentype.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing notice font: %w", err)
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    nameFontSize,
		DPI:     nameFontDPI,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, fmt.Errorf("building notice face: %w", err)
	}
	return face, nil
})

// WeaponKeys returns the sorted weapon icon keys (".png" stripped) that a kill
// notice may reference. The flashbang_assist icon is excluded because it is an
// assist flag, not a weapon.
func WeaponKeys() []string {
	keys, _ := weaponCatalog()
	out := make([]string, len(keys))
	copy(out, keys)
	return out
}

// ValidWeaponKey reports whether key names a renderable weapon icon.
func ValidWeaponKey(key string) bool {
	_, set := weaponCatalog()
	return set[key]
}

// element is one positioned piece of the notice: either drawn text or a scaled
// icon. Text uses baseline drawing; icons are vertically centered.
type element struct {
	img   image.Image
	text  string
	color color.Color
	width int
}

// RenderNotice draws a synthetic CS2 highlighted kill notice for the kill and
// returns it as an RGBA image of height KillfeedNoticeHeight. It errors on an
// unknown weapon key or an empty attacker or victim name.
func RenderNotice(kill KillfeedKill) (*image.RGBA, error) {
	attacker := strings.TrimSpace(kill.AttackerName)
	victim := strings.TrimSpace(kill.VictimName)
	if attacker == "" {
		return nil, fmt.Errorf("kill notice: attacker name is required")
	}
	if victim == "" {
		return nil, fmt.Errorf("kill notice: victim name is required")
	}
	if !ValidWeaponKey(kill.Weapon) {
		return nil, fmt.Errorf("kill notice: unknown weapon %q", kill.Weapon)
	}

	face, err := noticeFace()
	if err != nil {
		return nil, err
	}

	elements, err := buildElements(kill, attacker, victim, face)
	if err != nil {
		return nil, err
	}

	width := noticeHPadding*2 + elementsWidth(elements)
	canvas := image.NewRGBA(image.Rect(0, 0, width, KillfeedNoticeHeight))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(plateFill), image.Point{}, draw.Src)
	drawBorder(canvas)

	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()
	baseline := (KillfeedNoticeHeight-(ascent+descent))/2 + ascent

	x := noticeHPadding
	for i, el := range elements {
		if i > 0 {
			x += noticeGap
		}
		if el.img != nil {
			iconH := el.img.Bounds().Dy()
			y := (KillfeedNoticeHeight - iconH) / 2
			draw.Draw(canvas, image.Rect(x, y, x+el.width, y+iconH), el.img, el.img.Bounds().Min, draw.Over)
		} else {
			drawer := &font.Drawer{
				Dst:  canvas,
				Src:  image.NewUniform(el.color),
				Face: face,
				Dot:  fixed.P(x, baseline),
			}
			drawer.DrawString(el.text)
		}
		x += el.width
	}
	return canvas, nil
}

// EncodeNoticePNG renders the kill notice and writes it to w as a PNG.
func EncodeNoticePNG(kill KillfeedKill, w io.Writer) error {
	img, err := RenderNotice(kill)
	if err != nil {
		return err
	}
	return png.Encode(w, img)
}

// buildElements assembles the ordered content of the notice, left to right.
func buildElements(kill KillfeedKill, attacker, victim string, face font.Face) ([]element, error) {
	var elements []element

	if kill.Blind {
		flag, err := loadFlag("blind_kill", flagIconH)
		if err != nil {
			return nil, err
		}
		elements = append(elements, iconElement(flag))
	}

	elements = append(elements, textElement(attacker, sideColor(kill.AttackerSide), face))

	if strings.TrimSpace(kill.AssisterName) != "" {
		elements = append(elements, textElement("+", colorWhite, face))
		if kill.FlashAssist {
			flag, err := loadWeaponIcon(flashAssist, flagIconH)
			if err != nil {
				return nil, err
			}
			elements = append(elements, iconElement(flag))
		}
		elements = append(elements, textElement(strings.TrimSpace(kill.AssisterName), sideColor(kill.AssisterSide), face))
	}

	weapon, err := loadWeaponIcon(kill.Weapon, weaponIconH)
	if err != nil {
		return nil, err
	}
	elements = append(elements, iconElement(weapon))

	for _, flag := range killFlags(kill) {
		icon, err := loadFlag(flag, flagIconH)
		if err != nil {
			return nil, err
		}
		elements = append(elements, iconElement(icon))
	}

	elements = append(elements, textElement(victim, sideColor(kill.VictimSide), face))
	return elements, nil
}

// killFlags lists the modifier flag icons that follow the weapon, in order.
func killFlags(kill KillfeedKill) []string {
	var flags []string
	if kill.Noscope {
		flags = append(flags, "noscope")
	}
	if kill.Smoke {
		flags = append(flags, "smoke_kill")
	}
	if kill.Wallbang {
		flags = append(flags, "penetrate")
	}
	if kill.InAir {
		flags = append(flags, "inairkill")
	}
	if kill.Headshot {
		flags = append(flags, "icon_headshot")
	}
	return flags
}

func iconElement(img image.Image) element {
	return element{img: img, width: img.Bounds().Dx()}
}

func textElement(text string, col color.Color, face font.Face) element {
	return element{text: text, color: col, width: font.MeasureString(face, text).Ceil()}
}

func elementsWidth(elements []element) int {
	total := 0
	for i, el := range elements {
		if i > 0 {
			total += noticeGap
		}
		total += el.width
	}
	return total
}

func sideColor(side string) color.Color {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "T":
		return colorT
	case "CT":
		return colorCT
	default:
		return colorWhite
	}
}

// drawBorder strokes a solid noticeBorder-px frame in borderColor.
func drawBorder(img *image.RGBA) {
	b := img.Bounds()
	src := image.NewUniform(borderColor)
	// Top and bottom bands.
	draw.Draw(img, image.Rect(b.Min.X, b.Min.Y, b.Max.X, b.Min.Y+noticeBorder), src, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(b.Min.X, b.Max.Y-noticeBorder, b.Max.X, b.Max.Y), src, image.Point{}, draw.Src)
	// Left and right bands.
	draw.Draw(img, image.Rect(b.Min.X, b.Min.Y, b.Min.X+noticeBorder, b.Max.Y), src, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(b.Max.X-noticeBorder, b.Min.Y, b.Max.X, b.Max.Y), src, image.Point{}, draw.Src)
}

func loadWeaponIcon(key string, targetH int) (image.Image, error) {
	return loadIcon(path.Join(weaponsDir, key+".png"), targetH)
}

func loadFlag(name string, targetH int) (image.Image, error) {
	return loadIcon(path.Join(flagsDir, name+".png"), targetH)
}

// loadIcon decodes an embedded white-on-transparent PNG and scales it to
// targetH pixels tall, preserving aspect ratio with a quality scaler.
func loadIcon(assetPath string, targetH int) (image.Image, error) {
	key := iconCacheKey{assetPath: assetPath, targetH: targetH}
	entryValue, _ := scaledIconCache.LoadOrStore(key, &iconCacheEntry{})
	entry := entryValue.(*iconCacheEntry)
	entry.once.Do(func() {
		entry.img, entry.err = decodeAndScaleIcon(assetPath, targetH)
	})
	return entry.img, entry.err
}

func decodeAndScaleIcon(assetPath string, targetH int) (image.Image, error) {
	data, err := noticeAssets.ReadFile(assetPath)
	if err != nil {
		return nil, fmt.Errorf("reading icon %s: %w", assetPath, err)
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding icon %s: %w", assetPath, err)
	}
	sb := src.Bounds()
	if sb.Dy() == 0 {
		return nil, fmt.Errorf("icon %s has zero height", assetPath)
	}
	targetW := sb.Dx() * targetH / sb.Dy()
	if targetW < 1 {
		targetW = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, xdraw.Over, nil)
	return dst, nil
}
