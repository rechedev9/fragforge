// Package captions renders TikTok-style word-pop karaoke subtitles for
// 1080x1920 vertical Shorts. It turns timed word cues into an ASS (Advanced
// SubStation Alpha) subtitle track that libass/FFmpeg can burn in, with one
// word highlighted at a time as it is spoken.
package captions

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/rechedev9/fragforge/internal/mediafont"
)

// ErrUnusableTranscript marks a transcript a backend returned but that must not
// be burned in: it carries no words at all, or word timings so implausible the
// text cannot be trusted (see ValidateTranscript). It is a soft failure —
// callers try the next backend, and publish the clip uncaptioned if none
// succeed — which is why it is distinct from a transport or auth error, where
// failing the render is correct.
var ErrUnusableTranscript = errors.New("unusable transcript")

// MaxPlausibleWordSeconds bounds how long a single spoken word may take, even
// shouted or drawn out. Speech-to-text on gameplay audio hallucinates: on a
// real 15s CS2 clip both xAI and Groq returned "Hola"/"Martínez" stamped at
// 3.66s and 8.14s, which burned in as one caption card frozen over most of the
// clip. No real word lasts that long, so the timings alone condemn the
// transcript without needing to know what was actually said.
const MaxPlausibleWordSeconds = 2.5

// ValidateTranscript reports whether cues can be burned in as karaoke captions,
// wrapping ErrUnusableTranscript when they cannot. It is the single gate every
// backend's output passes through, because a garbled transcript is just as
// unusable when it comes from the fallback backend as from the preferred one.
// It also fronts BuildASS's own structural checks, so cues BuildASS would
// reject (unsorted, overlapping, zero-length) are reported as unusable — a soft
// failure that falls back to another backend — rather than failing the render.
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

	Bold    bool
	Outline int
	Shadow  int

	// MarginV is the vertical margin from the bottom of the frame, in
	// PlayRes pixels.
	MarginV int

	// WordsPerLine is the maximum number of words shown together in one
	// caption window.
	WordsPerLine int

	PlayResX int
	PlayResY int
}

// DefaultStyle returns the product default caption style: a bold,
// high-contrast look sized for a 1080x1920 vertical Short, with the caption
// block sitting in the lower-middle "gameplay band" of a 40/60 stacked
// layout, clear of platform UI (share/like buttons, captions safe area).
func DefaultStyle() Style {
	return Style{
		FontName:        mediafont.FamilyName,
		FontSize:        72,
		PrimaryColour:   "&H00FFFFFF", // opaque white
		HighlightColour: "&H0000FFFF", // opaque yellow (BGR: 00 FF FF)
		OutlineColour:   "&H00000000", // opaque black
		Bold:            true,
		Outline:         4,
		Shadow:          2,
		MarginV:         460,
		WordsPerLine:    4,
		PlayResX:        1080,
		PlayResY:        1920,
	}
}

// maxWordGapSeconds is the pause between two consecutive words above which a
// caption window is forced to break early, even if it has not yet reached
// WordsPerLine words. This keeps captions from silently spanning long gaps
// (round transitions, silence) as if the words were spoken together.
const maxWordGapSeconds = 1.2

// BuildASS renders cues into a complete ASS subtitle document using style.
// Consecutive words are grouped into caption windows of up to
// style.WordsPerLine words; a window also breaks early when the gap to the
// next word exceeds 1.2s. Each window becomes one Dialogue line spanning the
// window's start to end, with per-word \k karaoke tags so the active word's
// fill colour animates from PrimaryColour to HighlightColour as it is
// spoken.
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
		line, err := dialogueLine(window)
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
// word exceeds maxWordGapSeconds.
func windowCues(cues []WordCue, wordsPerLine int) [][]WordCue {
	if wordsPerLine <= 0 {
		wordsPerLine = 1
	}

	var windows [][]WordCue
	current := []WordCue{cues[0]}
	for i := 1; i < len(cues); i++ {
		gap := cues[i].StartSeconds - cues[i-1].EndSeconds
		if len(current) >= wordsPerLine || gap > maxWordGapSeconds {
			windows = append(windows, current)
			current = nil
		}
		current = append(current, cues[i])
	}
	windows = append(windows, current)
	return windows
}

// dialogueLine renders one ASS Dialogue line for a caption window, with a
// \k karaoke tag per word timed in centiseconds.
func dialogueLine(window []WordCue) (string, error) {
	if len(window) == 0 {
		return "", fmt.Errorf("captions: empty caption window")
	}
	start := window[0].StartSeconds
	end := window[len(window)-1].EndSeconds

	var text strings.Builder
	for i, cue := range window {
		if i > 0 {
			text.WriteString(" ")
		}
		centiseconds := karaokeCentiseconds(cue)
		fmt.Fprintf(&text, `{\k%d}%s`, centiseconds, escapeASSText(cue.Word))
	}

	return fmt.Sprintf(
		"Dialogue: 0,%s,%s,Karaoke,,0,0,0,,%s",
		formatASSTimestamp(start),
		formatASSTimestamp(end),
		text.String(),
	), nil
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
	boldFlag := 0
	if style.Bold {
		boldFlag = -1 // ASS boolean convention: -1 = true, 0 = false
	}
	b.WriteString("\n[V4+ Styles]\n")
	b.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(b,
		"Style: Karaoke,%s,%d,%s,%s,%s,&H00000000,%d,0,0,0,100,100,0,0,1,%d,%d,2,40,40,%d,1\n",
		style.FontName,
		style.FontSize,
		style.PrimaryColour,
		style.PrimaryColour, // SecondaryColour: the karaoke fill sweeps to HighlightColour per-word below
		style.OutlineColour,
		boldFlag,
		style.Outline,
		style.Shadow,
		style.MarginV,
	)
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
