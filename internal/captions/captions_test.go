package captions

import (
	"errors"
	"strings"
	"testing"
)

// Speech-to-text on gameplay audio hallucinates. ValidateTranscript is the one
// gate that keeps such a transcript from being burned in, whichever backend
// produced it.
func TestValidateTranscript(t *testing.T) {
	tests := []struct {
		name    string
		cues    []WordCue
		wantErr bool
	}{
		{
			name: "typical spoken words",
			cues: []WordCue{
				{Word: "gg", StartSeconds: 0, EndSeconds: 0.4},
				{Word: "wp", StartSeconds: 0.4, EndSeconds: 0.9},
			},
		},
		{
			name: "drawn out but plausible shout",
			cues: []WordCue{{Word: "noooo", StartSeconds: 0, EndSeconds: 2.4}},
		},
		{
			// The real hallucination that shipped: two words frozen over a 15s clip.
			name:    "words stretched across the clip",
			cues:    []WordCue{{Word: "Hola", StartSeconds: 0, EndSeconds: 3.66}, {Word: "Martínez", StartSeconds: 3.66, EndSeconds: 11.8}},
			wantErr: true,
		},
		{
			name:    "no cues",
			cues:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTranscript(tt.cues)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateTranscript(%+v) = nil, want an unusable-transcript error", tt.cues)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateTranscript(%+v) = %v, want nil", tt.cues, err)
			}
			if tt.wantErr && !errors.Is(err, ErrUnusableTranscript) {
				t.Fatalf("error = %v, want it to wrap ErrUnusableTranscript", err)
			}
		})
	}
}

// entranceTag is the once-per-line stretch-pop override that every displayed
// caption line must carry exactly once (on its first word).
const entranceTag = `\fscx160\fscy30\blur6\t(0,60,\fscx96\fscy106\blur0)\t(60,110,\fscx100\fscy100)`

func TestDefaultStyle(t *testing.T) {
	style := DefaultStyle()

	if style.PlayResX != 1080 || style.PlayResY != 1920 {
		t.Fatalf("got PlayRes %dx%d, want 1080x1920", style.PlayResX, style.PlayResY)
	}
	if style.WordsPerLine != 4 {
		t.Fatalf("got WordsPerLine %d, want 4", style.WordsPerLine)
	}
	if !style.Bold {
		t.Fatalf("got Bold false, want true")
	}
	if !style.Italic {
		t.Fatalf("got Italic false, want true")
	}
	if style.PrimaryColour != captionYellow || style.HighlightColour != captionYellow {
		t.Fatalf("got fills primary=%q highlight=%q, want both %q", style.PrimaryColour, style.HighlightColour, captionYellow)
	}
	if style.Alignment != 2 {
		t.Fatalf("got Alignment %d, want 2 (bottom-center fallback)", style.Alignment)
	}
	if style.MarginV != 460 {
		t.Fatalf("got MarginV %d, want 460", style.MarginV)
	}
	if style.PosX != 0 || style.PosY != 0 {
		t.Fatalf("got Pos (%d,%d), want (0,0): the layout-free default must not pin a position", style.PosX, style.PosY)
	}
}

func TestLayoutStyle(t *testing.T) {
	tests := []struct {
		name        string
		faceHeight  int
		outHeight   int
		wantPosX    int
		wantPosY    int
		wantResY    int
		wantDefault bool
	}{
		{
			// 40/60 default: facecam 768 over gameplay 1152 -> center 34-35%
			// into the gameplay band = 768 + round(0.35*1152) = 1171.
			name: "facecam 40/60", faceHeight: 768, outHeight: 1920,
			wantPosX: 540, wantPosY: 1171, wantResY: 1920,
		},
		{
			// Full frame: no facecam, so the band is the whole 1920px frame.
			name: "full frame", faceHeight: 0, outHeight: 1920,
			wantPosX: 540, wantPosY: 672, wantResY: 1920,
		},
		{
			name: "invalid output height falls back to default", faceHeight: 100, outHeight: 0,
			wantDefault: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LayoutStyle(tt.faceHeight, tt.outHeight)
			if tt.wantDefault {
				if got != DefaultStyle() {
					t.Fatalf("got %+v, want DefaultStyle() fallback", got)
				}
				return
			}
			if got.Alignment != 5 {
				t.Fatalf("got Alignment %d, want 5 (mid-center + \\pos)", got.Alignment)
			}
			if got.PosX != tt.wantPosX || got.PosY != tt.wantPosY {
				t.Fatalf("got Pos (%d,%d), want (%d,%d)", got.PosX, got.PosY, tt.wantPosX, tt.wantPosY)
			}
			if got.PlayResY != tt.wantResY {
				t.Fatalf("got PlayResY %d, want %d", got.PlayResY, tt.wantResY)
			}
			if !got.Italic || got.PrimaryColour != captionYellow {
				t.Fatalf("got Italic=%v primary=%q, want italic yellow like DefaultStyle", got.Italic, got.PrimaryColour)
			}
		})
	}
}

func TestBuildASS_ScriptInfoAndStyle(t *testing.T) {
	cues := []WordCue{
		{Word: "hola", StartSeconds: 0, EndSeconds: 0.3},
	}

	got, err := BuildASS(cues, DefaultStyle())
	if err != nil {
		t.Fatalf("BuildASS returned error: %v", err)
	}

	wantScriptInfo := "[Script Info]\nScriptType: v4.00+\nPlayResX: 1080\nPlayResY: 1920\n"
	if !strings.Contains(got, wantScriptInfo) {
		t.Fatalf("got ASS body %q, want it to contain script info %q", got, wantScriptInfo)
	}

	// All-yellow fill (primary + secondary), Bold -1, Italic -1, Outline 4,
	// Shadow 2, Alignment 2, MarginV 460.
	wantStyle := "Style: Karaoke,Montserrat ExtraBold,72,&H002FF4F9,&H002FF4F9,&H00000000,&H00000000,-1,-1,0,0,100,100,0,0,1,4,2,2,40,40,460,1"
	if !strings.Contains(got, wantStyle) {
		t.Fatalf("got ASS body %q, want it to contain style line %q", got, wantStyle)
	}
}

func TestBuildASS_LayoutStyleUsesPosAndMidCenter(t *testing.T) {
	cues := []WordCue{
		{Word: "hola", StartSeconds: 0, EndSeconds: 0.3},
	}

	got, err := BuildASS(cues, LayoutStyle(768, 1920))
	if err != nil {
		t.Fatalf("BuildASS returned error: %v", err)
	}

	// Mid-center alignment (5) and MarginV 0, since \pos drives placement.
	wantStyle := "Style: Karaoke,Montserrat ExtraBold,72,&H002FF4F9,&H002FF4F9,&H00000000,&H00000000,-1,-1,0,0,100,100,0,0,1,4,2,5,40,40,0,1"
	if !strings.Contains(got, wantStyle) {
		t.Fatalf("got ASS body %q, want it to contain style line %q", got, wantStyle)
	}

	wantDialogue := `Dialogue: 0,0:00:00.00,0:00:00.30,Karaoke,,0,0,0,,{\pos(540,1171)` + entranceTag + `\k30}hola`
	if !strings.Contains(got, wantDialogue) {
		t.Fatalf("got ASS body %q, want it to contain dialogue %q", got, wantDialogue)
	}
}

func TestBuildASS_LayoutStyleSupportsLandscapeOutput(t *testing.T) {
	cues := []WordCue{{Word: "hola", StartSeconds: 0, EndSeconds: 0.3}}
	got, err := BuildASS(cues, LayoutStyleForOutput(0, 1920, 1080))
	if err != nil {
		t.Fatalf("BuildASS returned error: %v", err)
	}
	for _, want := range []string{
		"PlayResX: 1920\nPlayResY: 1080",
		`\pos(960,378)`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got ASS body %q, want it to contain %q", got, want)
		}
	}
}

func TestBuildASS_EntranceTagOncePerLine(t *testing.T) {
	style := DefaultStyle()
	style.WordsPerLine = 2

	cues := []WordCue{
		{Word: "una", StartSeconds: 0, EndSeconds: 0.5},
		{Word: "kill", StartSeconds: 0.5, EndSeconds: 1.0},
		{Word: "limpia", StartSeconds: 1.0, EndSeconds: 1.5},
		{Word: "ya", StartSeconds: 1.5, EndSeconds: 2.0},
	}

	got, err := BuildASS(cues, style)
	if err != nil {
		t.Fatalf("BuildASS returned error: %v", err)
	}

	// Two windows -> the entrance pop must appear exactly twice, once per line.
	if gotCount := strings.Count(got, entranceTag); gotCount != 2 {
		t.Fatalf("got %d entrance tags, want 2 (one per displayed line)", gotCount)
	}
}

func TestBuildASS_TwoWindows(t *testing.T) {
	style := DefaultStyle()
	style.WordsPerLine = 2

	cues := []WordCue{
		{Word: "una", StartSeconds: 0, EndSeconds: 0.5},
		{Word: "kill", StartSeconds: 0.5, EndSeconds: 1.0},
		{Word: "limpia", StartSeconds: 1.0, EndSeconds: 1.5},
		{Word: "ya", StartSeconds: 1.5, EndSeconds: 2.0},
	}

	got, err := BuildASS(cues, style)
	if err != nil {
		t.Fatalf("BuildASS returned error: %v", err)
	}

	// The entrance pop rides on the window's first word; karaoke \k timings are
	// preserved for every word.
	wantFirst := `Dialogue: 0,0:00:00.00,0:00:01.00,Karaoke,,0,0,0,,{` + entranceTag + `\k50}una {\k50}kill`
	if !strings.Contains(got, wantFirst) {
		t.Fatalf("got ASS body %q, want it to contain first window %q", got, wantFirst)
	}

	wantSecond := `Dialogue: 0,0:00:01.00,0:00:02.00,Karaoke,,0,0,0,,{` + entranceTag + `\k50}limpia {\k50}ya`
	if !strings.Contains(got, wantSecond) {
		t.Fatalf("got ASS body %q, want it to contain second window %q", got, wantSecond)
	}
}

func TestBuildASS_GapSplitsWindow(t *testing.T) {
	style := DefaultStyle()
	style.WordsPerLine = 4

	cues := []WordCue{
		{Word: "espera", StartSeconds: 0, EndSeconds: 0.4},
		{Word: "ahi", StartSeconds: 0.4, EndSeconds: 0.8},
		// 2s gap here (> 1.2s), so this word must start a new window even
		// though the first window has not reached WordsPerLine.
		{Word: "va", StartSeconds: 2.8, EndSeconds: 3.2},
	}

	got, err := BuildASS(cues, style)
	if err != nil {
		t.Fatalf("BuildASS returned error: %v", err)
	}

	wantFirst := `Dialogue: 0,0:00:00.00,0:00:00.80,Karaoke,,0,0,0,,{` + entranceTag + `\k40}espera {\k40}ahi`
	if !strings.Contains(got, wantFirst) {
		t.Fatalf("got ASS body %q, want it to contain %q", got, wantFirst)
	}

	wantSecond := `Dialogue: 0,0:00:02.80,0:00:03.20,Karaoke,,0,0,0,,{` + entranceTag + `\k40}va`
	if !strings.Contains(got, wantSecond) {
		t.Fatalf("got ASS body %q, want it to contain %q", got, wantSecond)
	}
}

func TestBuildASS_SentencePunctuationSplitsWindow(t *testing.T) {
	style := DefaultStyle()
	style.WordsPerLine = 4

	cues := []WordCue{
		{Word: "¡Toma!", StartSeconds: 0, EndSeconds: 0.4},
		{Word: "¿Estás", StartSeconds: 0.5, EndSeconds: 0.8},
		{Word: "feliz?", StartSeconds: 0.8, EndSeconds: 1.1},
		{Word: "Vamos", StartSeconds: 1.2, EndSeconds: 1.6},
	}

	got, err := BuildASS(cues, style)
	if err != nil {
		t.Fatalf("BuildASS returned error: %v", err)
	}
	for _, want := range []string{
		`Dialogue: 0,0:00:00.00,0:00:00.40,Karaoke,,0,0,0,,{` + entranceTag + `\k40}¡Toma!`,
		`Dialogue: 0,0:00:00.50,0:00:01.10,Karaoke,,0,0,0,,{` + entranceTag + `\k30}¿Estás {\k30}feliz?`,
		`Dialogue: 0,0:00:01.20,0:00:01.60,Karaoke,,0,0,0,,{` + entranceTag + `\k40}Vamos`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("got ASS body %q, want it to contain %q", got, want)
		}
	}
}

func TestBuildASS_EscapesSpecialCharacters(t *testing.T) {
	cues := []WordCue{
		{Word: `wei{rd}\word`, StartSeconds: 0, EndSeconds: 0.3},
	}

	got, err := BuildASS(cues, DefaultStyle())
	if err != nil {
		t.Fatalf("BuildASS returned error: %v", err)
	}

	want := `\k30}wei\{rd\}\\word`
	if !strings.Contains(got, want) {
		t.Fatalf("got ASS body %q, want it to contain escaped word %q", got, want)
	}

	// The once-per-line entrance override block must open the line and precede
	// the first \k tag, so the stretch-pop rides the whole line rather than a
	// single word; escaping the word must not reorder it after the \k.
	entranceStart := strings.Index(got, `{\fscx`)
	firstK := strings.Index(got, `\k`)
	if entranceStart < 0 || firstK < 0 || entranceStart > firstK {
		t.Fatalf("entrance override block must precede the first \\k tag on the escaped-word line: %s", got)
	}
}

func TestBuildASS_Errors(t *testing.T) {
	tests := []struct {
		name string
		cues []WordCue
	}{
		{
			name: "empty cues",
			cues: nil,
		},
		{
			name: "non-positive duration",
			cues: []WordCue{{Word: "x", StartSeconds: 1, EndSeconds: 1}},
		},
		{
			name: "unsorted cues",
			cues: []WordCue{
				{Word: "b", StartSeconds: 1, EndSeconds: 2},
				{Word: "a", StartSeconds: 0, EndSeconds: 0.5},
			},
		},
		{
			name: "overlapping cues",
			cues: []WordCue{
				{Word: "a", StartSeconds: 0, EndSeconds: 1},
				{Word: "b", StartSeconds: 0.5, EndSeconds: 1.5},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildASS(tt.cues, DefaultStyle())
			if err == nil {
				t.Fatalf("BuildASS(%v) returned nil error, want an error", tt.cues)
			}
		})
	}
}

func TestFormatASSTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		seconds float64
		want    string
	}{
		{name: "zero", seconds: 0, want: "0:00:00.00"},
		{name: "sub-second", seconds: 0.5, want: "0:00:00.50"},
		{name: "over a minute", seconds: 75.25, want: "0:01:15.25"},
		{name: "over an hour", seconds: 3661.1, want: "1:01:01.10"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatASSTimestamp(tt.seconds)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBurnFilter(t *testing.T) {
	tests := []struct {
		name     string
		assPath  string
		fontsDir string
		want     string
	}{
		{
			name:     "windows path",
			assPath:  `C:\Users\reche\Documents\zackvideo\data\run\captions.ass`,
			fontsDir: `C:\Users\reche\AppData\Local\FragForge\fonts\v7.222`,
			want:     `ass='C\:/Users/reche/Documents/zackvideo/data/run/captions.ass':fontsdir='C\:/Users/reche/AppData/Local/FragForge/fonts/v7.222'`,
		},
		{
			name:     "unix-style path quoted",
			assPath:  "/tmp/run/captions.ass",
			fontsDir: "/tmp/run/fonts",
			want:     "ass='/tmp/run/captions.ass':fontsdir='/tmp/run/fonts'",
		},
		{
			name:     "embedded quote cannot break out",
			assPath:  `C:\run\o'clock.ass`,
			fontsDir: `C:\run\font's`,
			want:     `ass='C\:/run/o'\''clock.ass':fontsdir='C\:/run/font'\''s'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BurnFilter(tt.assPath, tt.fontsDir)
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
