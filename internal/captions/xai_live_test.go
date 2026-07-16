package captions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"
)

// TestXAITranscriberLive is an explicit smoke test, not part of the normal
// test gate. It runs only when both XAI_API_KEY and ZV_XAI_STT_MEDIA are set,
// calls the real xAI endpoint, and proves the returned word timings can be
// rendered into an ASS subtitle document. The API key and transcript text are
// never logged.
func TestXAITranscriberLive(t *testing.T) {
	apiKey := os.Getenv("XAI_API_KEY")
	mediaPath := os.Getenv("ZV_XAI_STT_MEDIA")
	if apiKey == "" || mediaPath == "" {
		t.Skip("set XAI_API_KEY and ZV_XAI_STT_MEDIA to run the live xAI STT smoke test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cues, err := (XAITranscriber{
		APIKey:   apiKey,
		Language: os.Getenv("ZV_XAI_STT_LANGUAGE"),
	}).Transcribe(ctx, mediaPath, t.TempDir())
	if err != nil {
		t.Fatalf("live xAI transcription failed: %v", err)
	}
	if len(cues) == 0 {
		t.Fatal("live xAI transcription returned no word cues")
	}
	if expected := os.Getenv("ZV_XAI_STT_EXPECTED"); expected != "" {
		words := make([]string, len(cues))
		for i, cue := range cues {
			words[i] = cue.Word
		}
		if got, want := normalizeSmokeText(strings.Join(words, " ")), normalizeSmokeText(expected); got != want {
			t.Fatalf("live xAI transcript did not match the expected fixture text (normalized lengths: got %d, want %d)", len(got), len(want))
		}
	}

	ass, err := BuildASS(cues, DefaultStyle())
	if err != nil {
		t.Fatalf("building ASS from live xAI cues: %v", err)
	}
	if !strings.Contains(ass, "Dialogue:") {
		t.Fatal("ASS generated from live xAI cues contains no dialogue events")
	}

	if outputPath := os.Getenv("ZV_XAI_STT_ASS"); outputPath != "" {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			t.Fatalf("creating ASS output directory: %v", err)
		}
		if err := os.WriteFile(outputPath, []byte(ass), 0o600); err != nil {
			t.Fatalf("writing live ASS output: %v", err)
		}
	}

	t.Logf("xAI STT smoke passed with %d timed words", len(cues))
}

// TestXAISpanishCaptionsLive exercises the complete paid caption path: xAI
// batch STT with automatic source-language recognition, followed by the Grok
// Spanish preservation/translation pass and ASS generation. It is opt-in and
// never logs either credential or transcript text.
func TestXAISpanishCaptionsLive(t *testing.T) {
	apiKey := os.Getenv("XAI_API_KEY")
	mediaPath := os.Getenv("ZV_XAI_SPANISH_MEDIA")
	if apiKey == "" || mediaPath == "" {
		t.Skip("set XAI_API_KEY and ZV_XAI_SPANISH_MEDIA to run the live Spanish caption smoke test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()

	source, err := (XAITranscriber{APIKey: apiKey}).Transcribe(ctx, mediaPath, t.TempDir())
	if err != nil {
		t.Fatalf("live xAI source transcription failed: %v", err)
	}
	if err := ValidateTranscript(source); err != nil {
		t.Fatalf("live xAI source transcript is unusable: %v", err)
	}

	spanish, err := (SpanishTranslator{APIKey: apiKey}).Translate(ctx, source)
	if err != nil {
		t.Fatalf("live Grok Spanish translation failed: %v", err)
	}
	if expected := os.Getenv("ZV_XAI_SPANISH_EXPECTED"); expected != "" {
		words := make([]string, len(spanish))
		for i, cue := range spanish {
			words[i] = cue.Word
		}
		if got, want := normalizeSmokeText(strings.Join(words, " ")), normalizeSmokeText(expected); got != want {
			t.Fatalf("live Spanish captions did not match the expected fixture text (normalized lengths: got %d, want %d)", len(got), len(want))
		}
	}

	ass, err := BuildASS(spanish, DefaultStyle())
	if err != nil {
		t.Fatalf("building ASS from live Spanish cues: %v", err)
	}
	if !strings.Contains(ass, "Dialogue:") {
		t.Fatal("ASS generated from live Spanish cues contains no dialogue events")
	}
	if outputPath := os.Getenv("ZV_XAI_SPANISH_ASS"); outputPath != "" {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			t.Fatalf("creating Spanish ASS output directory: %v", err)
		}
		if err := os.WriteFile(outputPath, []byte(ass), 0o600); err != nil {
			t.Fatalf("writing live Spanish ASS output: %v", err)
		}
	}

	t.Logf("xAI Spanish caption smoke passed with %d source words and %d Spanish words", len(source), len(spanish))
}

func normalizeSmokeText(value string) string {
	var normalized strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			normalized.WriteRune(r)
		}
	}
	return normalized.String()
}
