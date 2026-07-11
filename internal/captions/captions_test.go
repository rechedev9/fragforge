package captions

import (
	"reflect"
	"strings"
	"testing"
)

func TestBoundCuesToDuration(t *testing.T) {
	tests := []struct {
		name        string
		cues        []WordCue
		duration    float64
		want        []WordCue
		wantDropped int
		wantClipped int
	}{
		{
			name: "keeps in-range and clips crossing eof",
			cues: []WordCue{
				{Word: "keep", StartSeconds: 1, EndSeconds: 2},
				{Word: "clip", StartSeconds: 19.4, EndSeconds: 19.8},
				{Word: "drop", StartSeconds: 20.02, EndSeconds: 20.10},
			},
			duration:    19.55,
			want:        []WordCue{{Word: "keep", StartSeconds: 1, EndSeconds: 2}, {Word: "clip", StartSeconds: 19.4, EndSeconds: 19.55}},
			wantDropped: 1,
			wantClipped: 1,
		},
		{
			name:        "unknown duration drops all",
			cues:        []WordCue{{Word: "word", StartSeconds: 0, EndSeconds: 1}},
			duration:    0,
			wantDropped: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, dropped, clipped := BoundCuesToDuration(tt.cues, tt.duration)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("BoundCuesToDuration() = %+v, want %+v", got, tt.want)
			}
			if dropped != tt.wantDropped || clipped != tt.wantClipped {
				t.Fatalf("counts = dropped %d clipped %d, want dropped %d clipped %d", dropped, clipped, tt.wantDropped, tt.wantClipped)
			}
		})
	}
}

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
	if style.MarginV != 460 {
		t.Fatalf("got MarginV %d, want 460", style.MarginV)
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

	wantStyle := "Style: Karaoke,Montserrat ExtraBold,72,&H00FFFFFF,&H00FFFFFF,&H00000000,&H00000000,-1,0,0,0,100,100,0,0,1,4,2,2,40,40,460,1"
	if !strings.Contains(got, wantStyle) {
		t.Fatalf("got ASS body %q, want it to contain style line %q", got, wantStyle)
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

	wantFirst := `Dialogue: 0,0:00:00.00,0:00:01.00,Karaoke,,0,0,0,,{\k50}una {\k50}kill`
	if !strings.Contains(got, wantFirst) {
		t.Fatalf("got ASS body %q, want it to contain first window %q", got, wantFirst)
	}

	wantSecond := `Dialogue: 0,0:00:01.00,0:00:02.00,Karaoke,,0,0,0,,{\k50}limpia {\k50}ya`
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

	wantFirst := `Dialogue: 0,0:00:00.00,0:00:00.80,Karaoke,,0,0,0,,{\k40}espera {\k40}ahi`
	if !strings.Contains(got, wantFirst) {
		t.Fatalf("got ASS body %q, want it to contain %q", got, wantFirst)
	}

	wantSecond := `Dialogue: 0,0:00:02.80,0:00:03.20,Karaoke,,0,0,0,,{\k40}va`
	if !strings.Contains(got, wantSecond) {
		t.Fatalf("got ASS body %q, want it to contain %q", got, wantSecond)
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

	want := `{\k30}wei\{rd\}\\word`
	if !strings.Contains(got, want) {
		t.Fatalf("got ASS body %q, want it to contain escaped word %q", got, want)
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
