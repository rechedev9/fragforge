// Package captions renders word-pop karaoke subtitles for 1080x1920 vertical
// Shorts and 1920x1080 long-form videos. It turns timed word cues into an ASS
// (Advanced SubStation Alpha) subtitle track that libass/FFmpeg can burn in,
// with one word highlighted at a time as it is spoken.
package captions

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/rechedev9/fragforge/internal/mediafont"
)

// ErrUnusableTranscript marks a transcript a backend returned but that must not
// be burned in: it carries no words at all, or word timings so implausible the
// text cannot be trusted (see ValidateTranscript). It is a soft failure —
// callers publish the clip uncaptioned — which is why it is distinct from a
// transport or auth error, where failing the render is correct.
var ErrUnusableTranscript = errors.New("unusable transcript")

// MaxPlausibleWordSeconds bounds how long a single spoken word may take, even
// shouted or drawn out. Speech-to-text on gameplay audio hallucinates: on a
// real 15s CS2 clip a backend returned "Hola"/"Martínez" stamped at 3.66s and
// 8.14s, which burned in as one caption card frozen over most of the clip. No
// real word lasts that long, so the timings alone condemn the transcript
// without needing to know what was actually said.
const MaxPlausibleWordSeconds = 2.5

// ValidateTranscript reports whether cues can be burned in as karaoke captions,
// wrapping ErrUnusableTranscript when they cannot. It is the single gate the
// xAI output passes through before any words can be burned in.
// It also fronts BuildASS's own structural checks, so cues BuildASS would
// reject (unsorted, overlapping, zero-length) are reported as unusable — a soft
// failure that publishes the clip uncaptioned — rather than failing the render.
func ValidateTranscript(cues []WordCue) error {
	if len(cues) == 0 {
		return fmt.Errorf("transcript contains no words: %w", ErrUnusableTranscript)
	}
	for _, cue := range cues {
		if spoken := cue.EndSeconds - cue.StartSeconds; spoken > MaxPlausibleWordSeconds {
			return fmt.Errorf("transcript has implausible word timings (%q spans %.2fs): %w",
				cue.Word, spoken, ErrUnusableTranscript)
		}
	}
	if err := validateCues(cues); err != nil {
		return fmt.Errorf("%w: %w", err, ErrUnusableTranscript)
	}
	return nil
}

// WordCue is a single spoken word with its start and end time, in seconds
// from the start of the media.
type WordCue struct {
	Word         string
	StartSeconds float64
	EndSeconds   float64
}

// Style configures the look of the karaoke subtitle track. Colours use ASS
// hex notation, "&HAABBGGRR" (alpha, blue, green, red, each two hex digits,
// 00 = opaque), which is the reverse channel order from CSS/HTML hex.
type Style struct {
	FontName string
	FontSize int

	// PrimaryColour is the resting (not-yet-spoken and already-spoken) fill.
	// HighlightColour is the karaoke fill applied to the word being spoken.
	// OutlineColour is the text outline/border colour.
	PrimaryColour   string
	HighlightColour string
	OutlineColour   string

	Bold   bool
	Italic bool

	Outline int
	Shadow  int

	// Alignment is the ASS numpad alignment for the style. 2 (bottom-center)
	// places the block MarginV pixels above the frame bottom; 5 (mid-center),
	// paired with a per-line \pos, pins the block center at an absolute point.
	// Zero defaults to 2.
	Alignment int

	// MarginV is the vertical margin from the bottom of the frame, in
	// PlayRes pixels. It is ignored when PosX/PosY pin the block via \pos.
	MarginV int

	// PosX and PosY, when both positive, pin the caption block center at that
	// PlayRes point via a \pos tag on every Dialogue line, overriding
	// MarginV-based placement. LayoutStyle sets them so the caption tracks the
	// facecam split; DefaultStyle leaves them zero for bottom-margin placement.
	PosX int
	PosY int

	// WordsPerLine is the maximum number of words shown together in one
	// caption window.
	WordsPerLine int

	PlayResX int
	PlayResY int
}

// captionYellow is the reference viral-Short caption fill, #F9F42F in CSS,
// written in ASS &HAABBGGRR channel order.
const captionYellow = "&H002FF4F9"

// captionGameplayBandFraction is how far into the gameplay band the caption
// block center sits, measured from the reference Short (~34-35% below the
// facecam split).
const captionGameplayBandFraction = 0.35

// DefaultStyle returns the layout-free product default caption style: the
// reference viral-Short look — extra-bold italic, all-yellow fill with a black
// outline — sized for a 1080x1920 vertical Short with a bottom-margin fallback
// placement. Use LayoutStyle to place the caption relative to a specific
// facecam/gameplay split; DefaultStyle is the fallback when the layout is
// unknown.
func DefaultStyle() Style {
	return Style{
		FontName:        mediafont.FamilyName,
		FontSize:        72,
		PrimaryColour:   captionYellow, // all words yellow; no white->yellow karaoke flip
		HighlightColour: captionYellow,
		OutlineColour:   "&H00000000", // opaque black
		Bold:            true,
		Italic:          true,
		Outline:         4,
		Shadow:          2,
		Alignment:       2,
		MarginV:         460,
		WordsPerLine:    4,
		PlayResX:        1080,
		PlayResY:        1920,
	}
}

// LayoutStyle returns the caption style placed for a stacked layout: the same
// look as DefaultStyle, but with the caption block pinned a fixed fraction into
// the gameplay band instead of a hardcoded bottom margin, so it tracks the
// facecam split. It uses mid-center alignment plus a per-line \pos, which also
// anchors the entrance scale-pop at the block center. faceHeight is the facecam
// band height in output pixels (0 for a full-frame layout) and outputHeight is
// the full output height. Invalid dimensions fall back to DefaultStyle.
func LayoutStyle(faceHeight, outputHeight int) Style {
	return LayoutStyleForOutput(faceHeight, 1080, outputHeight)
}

// LayoutStyleForOutput places captions relative to arbitrary output geometry.
// Vertical stream variants use 1080x1920; landscape delivery uses 1920x1080.
// Keeping PlayRes equal to the encoded frame makes ASS placement and font size
// deterministic in both formats.
func LayoutStyleForOutput(faceHeight, outputWidth, outputHeight int) Style {
	style := DefaultStyle()
	if outputWidth <= 0 || outputHeight <= 0 {
		return style
	}
	if faceHeight < 0 {
		faceHeight = 0
	}
	if faceHeight > outputHeight {
		faceHeight = outputHeight
	}
	gameplayHeight := outputHeight - faceHeight
	captionCenterY := faceHeight + int(math.Round(captionGameplayBandFraction*float64(gameplayHeight)))

	style.PlayResX = outputWidth
	style.PlayResY = outputHeight
	style.Alignment = 5
	style.MarginV = 0
	style.PosX = outputWidth / 2
	style.PosY = captionCenterY
	return style
}

// maxWordGapSeconds is the pause between two consecutive words above which a
// caption window is forced to break early, even if it has not yet reached
// WordsPerLine words. This keeps captions from silently spanning long gaps
// (round transitions, silence) as if the words were spoken together.
const maxWordGapSeconds = 1.2

// BuildASS renders cues into a complete ASS subtitle document using style.
// Consecutive words are grouped into caption windows of up to
// style.WordsPerLine words; a window also breaks early when the gap to the
// next word exceeds 1.2s or the previous word ends a sentence. Each window
// becomes one Dialogue line spanning the window's start to end, with per-word
// \k karaoke tags so the active word's fill colour animates from PrimaryColour
// to HighlightColour as it is spoken.
//
// cues must be non-empty, already sorted by StartSeconds, non-overlapping,
// and have positive durations (EndSeconds > StartSeconds); BuildASS returns
// an error rather than silently reordering or clipping cues, since caption
// timing must match the source audio exactly.
func BuildASS(cues []WordCue, style Style) (string, error) {
	if len(cues) == 0 {
		return "", fmt.Errorf("captions: no word cues provided")
	}
	if err := validateCues(cues); err != nil {
		return "", err
	}

	var b strings.Builder
	writeScriptInfo(&b, style)
	writeStyles(&b, style)
	b.WriteString("\n[Events]\n")
	b.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

	for _, window := range windowCues(cues, style.WordsPerLine) {
		line, err := dialogueLine(window, style)
		if err != nil {
			return "", err
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String(), nil
}

func validateCues(cues []WordCue) error {
	sorted := sort.SliceIsSorted(cues, func(i, j int) bool {
		return cues[i].StartSeconds < cues[j].StartSeconds
	})
	if !sorted {
		return fmt.Errorf("captions: word cues must be sorted by start time")
	}
	for i, cue := range cues {
		if cue.EndSeconds <= cue.StartSeconds {
			return fmt.Errorf("captions: cue %d (%q) has non-positive duration", i, cue.Word)
		}
		if i > 0 && cue.StartSeconds < cues[i-1].EndSeconds {
			return fmt.Errorf("captions: cue %d (%q) overlaps the previous cue", i, cue.Word)
		}
	}
	return nil
}

// windowCues groups consecutive cues into caption windows of up to
// wordsPerLine words each, breaking a window early when the gap to the next
// word exceeds maxWordGapSeconds or the previous word ends a sentence. The
// punctuation boundary keeps a completed phrase from remaining on screen with
// words that have not been spoken yet.
func windowCues(cues []WordCue, wordsPerLine int) [][]WordCue {
	if wordsPerLine <= 0 {
		wordsPerLine = 1
	}

	var windows [][]WordCue
	current := []WordCue{cues[0]}
	for i := 1; i < len(cues); i++ {
		gap := cues[i].StartSeconds - cues[i-1].EndSeconds
		if len(current) >= wordsPerLine || gap > maxWordGapSeconds || endsSentence(cues[i-1].Word) {
			windows = append(windows, current)
			current = nil
		}
		current = append(current, cues[i])
	}
	windows = append(windows, current)
	return windows
}

func endsSentence(word string) bool {
	word = strings.TrimSpace(word)
	return strings.HasSuffix(word, ".") ||
		strings.HasSuffix(word, "!") ||
		strings.HasSuffix(word, "?") ||
		strings.HasSuffix(word, "…")
}

// dialogueLine renders one ASS Dialogue line for a caption window, with a
// \k karaoke tag per word timed in centiseconds. The window's first word also
// carries the once-per-line entrance override (and \pos when the style pins a
// position), so the stretch-pop fires once as the line appears rather than
// re-triggering per word.
func dialogueLine(window []WordCue, style Style) (string, error) {
	if len(window) == 0 {
		return "", fmt.Errorf("captions: empty caption window")
	}
	start := window[0].StartSeconds
	end := window[len(window)-1].EndSeconds

	lead := entranceOverride(style)
	var text strings.Builder
	for i, cue := range window {
		if i > 0 {
			text.WriteString(" ")
		}
		centiseconds := karaokeCentiseconds(cue)
		if i == 0 {
			fmt.Fprintf(&text, `{%s\k%d}%s`, lead, centiseconds, escapeASSText(cue.Word))
			continue
		}
		fmt.Fprintf(&text, `{\k%d}%s`, centiseconds, escapeASSText(cue.Word))
	}

	return fmt.Sprintf(
		"Dialogue: 0,%s,%s,Karaoke,,0,0,0,,%s",
		formatASSTimestamp(start),
		formatASSTimestamp(end),
		text.String(),
	), nil
}

// entranceOverride is the once-per-line "alive" pop measured from the reference
// Short at 60fps: the phrase enters wide and vertically squashed with blur
// (\fscx160\fscy30\blur6), snaps up past 100% with a small overshoot
// (\fscx96\fscy106 by 60ms), then settles to 100% by ~110ms. When the style
// pins a position it prepends \pos so the block sits at the caption center and
// the scale animates around that anchor.
func entranceOverride(style Style) string {
	var b strings.Builder
	if style.PosX > 0 && style.PosY > 0 {
		fmt.Fprintf(&b, `\pos(%d,%d)`, style.PosX, style.PosY)
	}
	b.WriteString(`\fscx160\fscy30\blur6\t(0,60,\fscx96\fscy106\blur0)\t(60,110,\fscx100\fscy100)`)
	return b.String()
}

// karaokeCentiseconds converts a cue's duration to ASS karaoke centiseconds,
// rounding to the nearest centisecond so the \k tag durations for a window
// sum to (approximately) the window's total duration.
func karaokeCentiseconds(cue WordCue) int {
	duration := cue.EndSeconds - cue.StartSeconds
	return max(int(duration*100+0.5), 1)
}

func writeScriptInfo(b *strings.Builder, style Style) {
	fmt.Fprintf(b, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nWrapStyle: 2\nScaledBorderAndShadow: yes\n",
		style.PlayResX, style.PlayResY)
}

func writeStyles(b *strings.Builder, style Style) {
	b.WriteString("\n[V4+ Styles]\n")
	b.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(b,
		"Style: Karaoke,%s,%d,%s,%s,%s,&H00000000,%d,%d,0,0,100,100,0,0,1,%d,%d,%d,40,40,%d,1\n",
		style.FontName,
		style.FontSize,
		style.PrimaryColour,
		style.HighlightColour, // SecondaryColour: \k sweeps from this to PrimaryColour; both yellow, so no visible flip
		style.OutlineColour,
		assBool(style.Bold),
		assBool(style.Italic),
		style.Outline,
		style.Shadow,
		alignmentOrDefault(style.Alignment),
		style.MarginV,
	)
}

// assBool renders a Go bool in the ASS style convention: -1 for true, 0 for
// false.
func assBool(v bool) int {
	if v {
		return -1
	}
	return 0
}

// alignmentOrDefault falls back to 2 (bottom-center) for a zero alignment so a
// Style that never set Alignment keeps the historical placement.
func alignmentOrDefault(a int) int {
	if a == 0 {
		return 2
	}
	return a
}

// formatASSTimestamp renders seconds as an ASS timestamp, h:mm:ss.cc
// (centiseconds, two digits).
func formatASSTimestamp(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	totalCentiseconds := int(seconds*100 + 0.5)
	hours := totalCentiseconds / 360000
	totalCentiseconds -= hours * 360000
	minutes := totalCentiseconds / 6000
	totalCentiseconds -= minutes * 6000
	secs := totalCentiseconds / 100
	centiseconds := totalCentiseconds % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", hours, minutes, secs, centiseconds)
}

// escapeASSText escapes ASS override-block special characters ({ and }),
// backslashes, and newlines in a word so it renders literally instead of
// being interpreted as an override tag or forced line break.
func escapeASSText(word string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`{`, `\{`,
		`}`, `\}`,
		"\n", `\N`,
		"\r", "",
	)
	return replacer.Replace(word)
}

// BurnFilter returns the FFmpeg video filter clause that burns in the ASS
// subtitle track and directs libass to the bundled font's directory.
func BurnFilter(assPath, fontsDir string) string {
	return fmt.Sprintf("ass='%s':fontsdir='%s'", escapeFilterPath(assPath), escapeFilterPath(fontsDir))
}

// escapeFilterPath escapes a filesystem path for use as a quoted FFmpeg
// filter option value. The path goes through two parsers: the filtergraph
// parser (which the surrounding single quotes neutralise, passing the
// content through verbatim) and the filter's own option parser, which
// splits positional options on ':' and therefore needs the drive-letter
// colon escaped as \: (the cause of "unable to parse original_size" when
// left bare). Separators become forward slashes to minimise escapes, and a
// literal single quote closes/reopens the quoted section so it cannot
// terminate it early.
func escapeFilterPath(path string) string {
	slashed := strings.ReplaceAll(path, `\`, `/`)
	replacer := strings.NewReplacer(
		`:`, `\:`,
		`'`, `'\''`,
	)
	return replacer.Replace(slashed)
}
