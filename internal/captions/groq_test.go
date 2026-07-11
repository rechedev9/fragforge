package captions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleGroqJSON = `{
  "text": "Una kill limpísima en el pushback",
  "words": [
    {"word": "Una", "start": 0.0, "end": 0.4},
    {"word": "kill", "start": 0.4, "end": 0.9},
    {"word": "...", "start": 0.9, "end": 1.0},
    {"word": "limpísima", "start": 1.0, "end": 1.6},
    {"word": " ", "start": 1.6, "end": 1.6},
    {"word": "en", "start": 1.6, "end": 1.8}
  ]
}`

func TestParseGroqJSON(t *testing.T) {
	cues, err := ParseGroqJSON([]byte(sampleGroqJSON))
	if err != nil {
		t.Fatalf("ParseGroqJSON returned error: %v", err)
	}

	want := []WordCue{
		{Word: "Una", StartSeconds: 0, EndSeconds: 0.4},
		{Word: "kill", StartSeconds: 0.4, EndSeconds: 0.9},
		{Word: "limpísima", StartSeconds: 1.0, EndSeconds: 1.6},
		{Word: "en", StartSeconds: 1.6, EndSeconds: 1.8},
	}
	if len(cues) != len(want) {
		t.Fatalf("got %d cues, want %d: %+v", len(cues), len(want), cues)
	}
	for i, wantCue := range want {
		if cues[i] != wantCue {
			t.Fatalf("cue %d: got %+v, want %+v", i, cues[i], wantCue)
		}
	}
}

func TestParseGroqJSON_NormalizesOverlappingWordTimestamps(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		want     []WordCue
		buildASS bool
	}{
		{
			name: "mild overlap renders captions",
			data: `{"words":[
				{"word":"No","start":0.10,"end":0.35},
				{"word":"sabes","start":0.35,"end":0.72},
				{"word":"lo","start":0.72,"end":0.88},
				{"word":"que","start":0.88,"end":1.08},
				{"word":"haces.","start":1.08,"end":1.48},
				{"word":"¿Sabes","start":1.46,"end":1.82}
			]}`,
			want: []WordCue{
				{Word: "No", StartSeconds: 0.10, EndSeconds: 0.35},
				{Word: "sabes", StartSeconds: 0.35, EndSeconds: 0.72},
				{Word: "lo", StartSeconds: 0.72, EndSeconds: 0.88},
				{Word: "que", StartSeconds: 0.88, EndSeconds: 1.08},
				{Word: "haces.", StartSeconds: 1.08, EndSeconds: 1.48},
				{Word: "¿Sabes", StartSeconds: 1.48, EndSeconds: 1.82},
			},
			buildASS: true,
		},
		{
			name: "fully covered cue is dropped",
			data: `{"words":[
				{"word":"uno","start":0.50,"end":1.50},
				{"word":"cubierto","start":0.80,"end":1.20},
				{"word":"tres","start":1.10,"end":1.80}
			]}`,
			want: []WordCue{
				{Word: "uno", StartSeconds: 0.50, EndSeconds: 1.50},
				{Word: "tres", StartSeconds: 1.50, EndSeconds: 1.80},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGroqJSON([]byte(tt.data))
			if err != nil {
				t.Fatalf("ParseGroqJSON returned error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d cues (%+v), want %d (%+v)", len(got), got, len(tt.want), tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("cue %d: got %+v, want %+v", i, got[i], tt.want[i])
				}
			}
			if tt.buildASS {
				if _, err := BuildASS(got, DefaultStyle()); err != nil {
					t.Fatalf("BuildASS returned error for parsed Groq cues: %v", err)
				}
			}
		})
	}
}

func TestParseGroqJSON_Errors(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "invalid json", data: "{not json"},
		{name: "no word content", data: `{"words":[{"word":" ... ","start":0,"end":0.1}]}`},
		{name: "empty words", data: `{"text":"","words":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseGroqJSON([]byte(tt.data))
			if err == nil {
				t.Fatalf("ParseGroqJSON(%q) returned nil error, want an error", tt.data)
			}
		})
	}
}

// fakeExtractAudio writes fixed content to audioPath instead of shelling out
// to a real ffmpeg, so Transcribe tests never need ffmpeg on disk.
func fakeExtractAudio(_ context.Context, _, _, audioPath string) error {
	return os.WriteFile(audioPath, []byte("fake-flac-bytes"), 0o600)
}

func TestGroqTranscriber_Transcribe(t *testing.T) {
	var gotAuth, gotContentType string
	var gotFields = map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		for _, key := range []string{"model", "response_format", "temperature", "language", "prompt"} {
			gotFields[key] = r.FormValue(key)
		}
		gotFields["timestamp_granularities[]"] = r.FormValue("timestamp_granularities[]")
		if r.MultipartForm.File["file"] == nil {
			t.Fatal("request missing multipart file field")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleGroqJSON))
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatalf("writing fake media file: %v", err)
	}

	transcriber := GroqTranscriber{
		APIKey:       "secret-key",
		Language:     "es",
		BaseURL:      server.URL,
		extractAudio: fakeExtractAudio,
	}

	cues, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if len(cues) == 0 {
		t.Fatal("Transcribe returned no cues")
	}

	if gotAuth != "Bearer secret-key" {
		t.Fatalf("Authorization header = %q, want Bearer secret-key", gotAuth)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Fatalf("Content-Type = %q, want multipart/form-data", gotContentType)
	}
	// The default must stay the full multilingual model: the distilled turbo
	// variant garbles code-switched clips (Spanish stream audio with English
	// phrases), transcribing hallucinated text in the detected language.
	if gotFields["model"] != "whisper-large-v3" {
		t.Errorf("model field = %q, want whisper-large-v3", gotFields["model"])
	}
	if gotFields["response_format"] != "verbose_json" {
		t.Errorf("response_format field = %q, want verbose_json", gotFields["response_format"])
	}
	if gotFields["timestamp_granularities[]"] != "word" {
		t.Errorf("timestamp_granularities[] field = %q, want word", gotFields["timestamp_granularities[]"])
	}
	if gotFields["temperature"] != "0" {
		t.Errorf("temperature field = %q, want 0", gotFields["temperature"])
	}
	if gotFields["language"] != "es" {
		t.Errorf("language field = %q, want es", gotFields["language"])
	}
	if !strings.Contains(gotFields["prompt"], "ortografía") || !strings.Contains(gotFields["prompt"], "AWP") {
		t.Errorf("Spanish prompt = %q, want orthography guidance and CS2 vocabulary", gotFields["prompt"])
	}
}

func TestGroqTranscriber_Transcribe_OmitsAutoLanguage(t *testing.T) {
	var sawLanguageField bool
	var gotPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if _, ok := r.MultipartForm.Value["language"]; ok {
			sawLanguageField = true
		}
		gotPrompt = r.FormValue("prompt")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleGroqJSON))
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatalf("writing fake media file: %v", err)
	}

	transcriber := GroqTranscriber{
		APIKey:       "secret-key",
		Language:     "auto",
		BaseURL:      server.URL,
		extractAudio: fakeExtractAudio,
	}
	if _, err := transcriber.Transcribe(context.Background(), mediaPath, dir); err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if sawLanguageField {
		t.Fatal("request included a language field for \"auto\", want it omitted")
	}
	if !strings.Contains(gotPrompt, "CS2") || !strings.Contains(gotPrompt, "Spanish and English") || !strings.Contains(gotPrompt, "do not translate") {
		t.Fatalf("auto prompt = %q, want bilingual original-language guidance", gotPrompt)
	}
}

func TestGroqTranscriber_Transcribe_UsesEnglishPrompt(t *testing.T) {
	var gotPrompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		gotPrompt = r.FormValue("prompt")
		_, _ = w.Write([]byte(sampleGroqJSON))
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcriber := GroqTranscriber{APIKey: "key", Language: "en", BaseURL: server.URL, extractAudio: fakeExtractAudio}
	if _, err := transcriber.Transcribe(context.Background(), mediaPath, dir); err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if !strings.Contains(gotPrompt, "spelling") || !strings.Contains(gotPrompt, "Counter-Strike 2") {
		t.Fatalf("English prompt = %q, want spelling guidance and CS2 vocabulary", gotPrompt)
	}
}

func TestGroqTranscriber_Transcribe_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"message": "Invalid API Key"},
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatalf("writing fake media file: %v", err)
	}

	transcriber := GroqTranscriber{
		APIKey:       "bad-key",
		BaseURL:      server.URL,
		extractAudio: fakeExtractAudio,
	}
	_, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err == nil {
		t.Fatal("Transcribe returned nil error, want an error for a 401 response")
	}
	if !strings.Contains(err.Error(), "api key rejected") {
		t.Fatalf("got error %q, want it to mention the rejected api key", err.Error())
	}
	if strings.Contains(err.Error(), "bad-key") {
		t.Fatalf("error %q leaks the api key", err.Error())
	}
}

func TestGroqTranscriber_Transcribe_TooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		_, _ = w.Write([]byte(`{"error":{"message":"file too large"}}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatalf("writing fake media file: %v", err)
	}

	transcriber := GroqTranscriber{
		APIKey:       "secret-key",
		BaseURL:      server.URL,
		extractAudio: fakeExtractAudio,
	}
	_, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err == nil {
		t.Fatal("Transcribe returned nil error, want an error for a 413 response")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("got error %q, want it to mention the file being too large", err.Error())
	}
}

func TestGroqTranscriber_Transcribe_MissingAPIKey(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatalf("writing fake media file: %v", err)
	}

	transcriber := GroqTranscriber{extractAudio: fakeExtractAudio}
	_, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err == nil {
		t.Fatal("Transcribe returned nil error, want an error for a missing api key")
	}
	if !strings.Contains(err.Error(), "api key not configured") {
		t.Fatalf("got error %q, want it to mention the missing api key", err.Error())
	}
}

func TestGroqTranscriber_Transcribe_MissingMedia(t *testing.T) {
	dir := t.TempDir()
	transcriber := GroqTranscriber{APIKey: "secret-key", extractAudio: fakeExtractAudio}
	_, err := transcriber.Transcribe(context.Background(), filepath.Join(dir, "does-not-exist.mp4"), dir)
	if err == nil {
		t.Fatal("Transcribe returned nil error, want an error for missing media")
	}
	if !strings.Contains(err.Error(), "media file not found") {
		t.Fatalf("got error %q, want it to mention the missing media file", err.Error())
	}
}

func TestSpeechSpansFromSilences(t *testing.T) {
	tests := []struct {
		name     string
		duration float64
		silences []speechSpan
		want     []speechSpan
	}{
		{
			// The real failure case: one utterance up front, callouts mid-clip,
			// silence (game noise) between them.
			name:     "gaming clip with sparse speech",
			duration: 30,
			silences: []speechSpan{{Start: 0.9, End: 4.1}, {Start: 7.4, End: 9.4}, {Start: 11.3, End: 28.4}},
			want: []speechSpan{
				{Start: 0, End: 1.2},
				{Start: 3.8, End: 7.7},
				{Start: 9.1, End: 11.6},
				{Start: 28.1, End: 30},
			},
		},
		{
			name:     "fully silent clip yields no spans",
			duration: 20,
			silences: []speechSpan{{Start: 0, End: 20}},
			want:     nil,
		},
		{
			name:     "continuous speech falls back to the whole clip",
			duration: 30,
			silences: nil,
			want:     nil,
		},
		{
			name:     "blips shorter than the minimum are dropped",
			duration: 10,
			silences: []speechSpan{{Start: 0, End: 5}, {Start: 5.1, End: 10}},
			want:     nil,
		},
		{
			name:     "padded neighbours merge into one span",
			duration: 20,
			silences: []speechSpan{{Start: 2, End: 3}, {Start: 10, End: 18}},
			want:     []speechSpan{{Start: 0, End: 10.3}, {Start: 17.7, End: 20}},
		},
		{
			name:     "zero duration yields nothing",
			duration: 0,
			silences: nil,
			want:     nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := speechSpansFromSilences(tt.duration, tt.silences)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d spans (%+v), want %d (%+v)", len(got), got, len(tt.want), tt.want)
			}
			for i := range got {
				if diff := got[i].Start - tt.want[i].Start; diff > 0.001 || diff < -0.001 {
					t.Errorf("span %d start = %v, want %v", i, got[i].Start, tt.want[i].Start)
				}
				if diff := got[i].End - tt.want[i].End; diff > 0.001 || diff < -0.001 {
					t.Errorf("span %d end = %v, want %v", i, got[i].End, tt.want[i].End)
				}
			}
		})
	}
}

func TestParseSilenceDetect(t *testing.T) {
	out := `Input #0, flac, from 'audio.flac':
  Duration: 00:00:30.00, start: 0.000000, bitrate: 279 kb/s
[silencedetect @ 0x1] silence_start: 0.879375
[silencedetect @ 0x1] silence_end: 4.067063 | silence_duration: 3.187688
[silencedetect @ 0x1] silence_start: 27.291062
`
	duration, silences, err := parseSilenceDetect(out)
	if err != nil {
		t.Fatalf("parseSilenceDetect error = %v", err)
	}
	if duration != 30 {
		t.Fatalf("duration = %v, want 30", duration)
	}
	want := []speechSpan{{Start: 0.879375, End: 4.067063}, {Start: 27.291062, End: 30}}
	if len(silences) != len(want) {
		t.Fatalf("silences = %+v, want %+v", silences, want)
	}
	for i := range want {
		if silences[i] != want[i] {
			t.Errorf("silence %d = %+v, want %+v", i, silences[i], want[i])
		}
	}

	if _, _, err := parseSilenceDetect("no duration here"); err == nil {
		t.Fatal("parseSilenceDetect without a Duration line must error")
	}
}

// TestGroqTranscriber_TranscribeSpansOffsetsWords is the regression test for
// the missing-captions bug: a 30s gaming clip transcribed in one pass dropped
// every utterance after the first. With span detection, each speech region is
// transcribed separately and its words come back offset into clip time.
func TestGroqTranscriber_TranscribeSpansOffsetsWords(t *testing.T) {
	var requests, correctionRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/completions" {
			correctionRequests++
			writeGroqCorrectionResponse(t, w, []indexedCorrection{{Index: 0, Token: "Hay"}, {Index: 1, Token: "uno"}, {Index: 2, Token: "Smoke"}})
			return
		}
		requests++
		w.Header().Set("Content-Type", "application/json")
		switch requests {
		case 1:
			_, _ = w.Write([]byte(`{"words":[{"word":"Hay","start":0.1,"end":0.4},{"word":"uno","start":0.5,"end":0.8}]}`))
		case 2:
			// A noise-only span transcribing to nothing is normal, not an error.
			_, _ = w.Write([]byte(`{"words":[]}`))
		default:
			_, _ = w.Write([]byte(`{"words":[{"word":"smoke","start":1.0,"end":1.5}]}`))
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}

	var chunkStarts []float64
	transcriber := GroqTranscriber{
		APIKey:          "secret-key",
		BaseURL:         server.URL,
		CorrectionModel: "llama-test",
		extractAudio:    fakeExtractAudio,
		detectSpeech: func(_ context.Context, _, _ string) ([]speechSpan, error) {
			return []speechSpan{{Start: 0, End: 1.2}, {Start: 9.1, End: 11.6}, {Start: 22.4, End: 27.6}}, nil
		},
		extractChunk: func(_ context.Context, _, _, chunkPath string, start, _ float64) error {
			chunkStarts = append(chunkStarts, start)
			return os.WriteFile(chunkPath, []byte("fake-chunk"), 0o600)
		},
	}

	cues, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if requests != 3 {
		t.Fatalf("groq requests = %d, want 3 (one per span)", requests)
	}
	if correctionRequests != 1 {
		t.Fatalf("correction requests = %d, want 1 for the assembled clip", correctionRequests)
	}
	if len(chunkStarts) != 3 || chunkStarts[1] != 9.1 {
		t.Fatalf("chunk starts = %v, want the three span starts", chunkStarts)
	}
	want := []WordCue{
		{Word: "Hay", StartSeconds: 0.1, EndSeconds: 0.4},
		{Word: "uno", StartSeconds: 0.5, EndSeconds: 0.8},
		{Word: "Smoke", StartSeconds: 23.4, EndSeconds: 23.9},
	}
	if len(cues) != len(want) {
		t.Fatalf("cues = %+v, want %+v", cues, want)
	}
	for i := range want {
		if cues[i].Word != want[i].Word ||
			cues[i].StartSeconds-want[i].StartSeconds > 0.001 || want[i].StartSeconds-cues[i].StartSeconds > 0.001 {
			t.Errorf("cue %d = %+v, want %+v", i, cues[i], want[i])
		}
	}
}

func TestGroqTranscriber_TranscribeFallsBackWhenDetectionFails(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleGroqJSON))
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}

	transcriber := GroqTranscriber{
		APIKey:       "secret-key",
		BaseURL:      server.URL,
		extractAudio: fakeExtractAudio,
		detectSpeech: func(_ context.Context, _, _ string) ([]speechSpan, error) {
			return nil, context.DeadlineExceeded
		},
	}

	cues, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("groq requests = %d, want 1 whole-clip fallback", requests)
	}
	if len(cues) == 0 {
		t.Fatal("fallback returned no cues")
	}
}

// TestParseGroqWordsDropsHallucinatedSegments: words inside a segment Whisper
// itself scores as probably-noise (high no_speech_prob or very low logprob)
// must not become burned captions; the classic case is "아" or a phantom
// "Gracias." transcribed over pure game noise.
func TestParseGroqWordsDropsHallucinatedSegments(t *testing.T) {
	data := `{
	  "text": "hola 아 mundo",
	  "words": [
	    {"word": "hola", "start": 0.2, "end": 0.6},
	    {"word": "아", "start": 2.1, "end": 2.4},
	    {"word": "mundo", "start": 5.0, "end": 5.5}
	  ],
	  "segments": [
	    {"start": 0.0, "end": 1.0, "no_speech_prob": 0.02, "avg_logprob": -0.3},
	    {"start": 2.0, "end": 3.0, "no_speech_prob": 0.28, "avg_logprob": -1.37},
	    {"start": 4.5, "end": 6.0, "no_speech_prob": 0.7, "avg_logprob": -0.4}
	  ]
	}`
	cues, err := parseGroqWords([]byte(data))
	if err != nil {
		t.Fatalf("parseGroqWords error = %v", err)
	}
	if len(cues) != 1 || cues[0].Word != "hola" {
		t.Fatalf("cues = %+v, want only the confident word 'hola'", cues)
	}
}

// Regression for a real stream-clip render (background music mixed at 15%
// volume): genuine Spanish speech decoded at avg_logprob -0.781, well below
// the -0.7 logprob gate, but no_speech_prob was 0.005, meaning Whisper was
// near-certain it heard real speech. The old logprob-only gate discarded all
// three segments and the clip published with no captions at all.
func TestParseGroqWordsSpeechCertainSkipsLogprobGate(t *testing.T) {
	tests := []struct {
		name      string
		data      string
		wantWords []string
	}{
		{
			name: "music-suppressed genuine speech kept",
			data: `{
			  "words": [
			    {"word": "A", "start": 0.0, "end": 0.3},
			    {"word": "ver,", "start": 0.3, "end": 0.6},
			    {"word": "¿me", "start": 0.6, "end": 1.0},
			    {"word": "muero?", "start": 1.0, "end": 1.4},
			    {"word": "¿Qué?", "start": 1.4, "end": 2.0},
			    {"word": "Buen", "start": 15.0, "end": 15.5},
			    {"word": "round.", "start": 15.5, "end": 16.0},
			    {"word": "Ahora", "start": 17.0, "end": 17.3},
			    {"word": "vamos", "start": 17.3, "end": 17.6},
			    {"word": "a", "start": 17.6, "end": 17.9},
			    {"word": "hacer", "start": 17.9, "end": 18.2},
			    {"word": "comida,", "start": 18.2, "end": 18.6},
			    {"word": "¿vale?", "start": 18.6, "end": 19.0}
			  ],
			  "segments": [
			    {"start": 0.0, "end": 2.0, "text": " A ver, ¿me muero? ¿Qué?", "no_speech_prob": 0.005, "avg_logprob": -0.781},
			    {"start": 15.0, "end": 16.0, "text": " Buen round.", "no_speech_prob": 0.005, "avg_logprob": -0.781},
			    {"start": 17.0, "end": 19.0, "text": " Ahora vamos a hacer comida, ¿vale?", "no_speech_prob": 0.005, "avg_logprob": -0.781}
			  ]
			}`,
			wantWords: []string{
				"A", "ver,", "¿me", "muero?", "¿Qué?",
				"Buen", "round.",
				"Ahora", "vamos", "a", "hacer", "comida,", "¿vale?",
			},
		},
		{
			name: "actual hallucination still dropped despite low no_speech_prob margin",
			data: `{
			  "words": [{"word": "Gracias.", "start": 0.2, "end": 0.6}],
			  "segments": [{"start": 0.0, "end": 1.0, "text": " Gracias.", "no_speech_prob": 0.3, "avg_logprob": -0.85}]
			}`,
			wantWords: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cues, err := parseGroqWords([]byte(tt.data))
			if err != nil {
				t.Fatalf("parseGroqWords error = %v", err)
			}
			got := make([]string, 0, len(cues))
			for _, c := range cues {
				got = append(got, c.Word)
			}
			if len(got) != len(tt.wantWords) {
				t.Fatalf("got %d words %+v, want %d words %+v", len(got), got, len(tt.wantWords), tt.wantWords)
			}
			for i, w := range tt.wantWords {
				if got[i] != w {
					t.Fatalf("got word[%d] = %q, want %q (got=%+v)", i, got[i], w, got)
				}
			}
		})
	}
}

func TestParseGroqWordsKeepsAllWordsWithoutSegments(t *testing.T) {
	data := `{"words":[{"word":"hola","start":0.2,"end":0.6},{"word":"mundo","start":1.0,"end":1.4}]}`
	cues, err := parseGroqWords([]byte(data))
	if err != nil {
		t.Fatalf("parseGroqWords error = %v", err)
	}
	if len(cues) != 2 {
		t.Fatalf("cues = %+v, want both words kept when no segment metadata exists", cues)
	}
}

// A Whisper repetition loop over sustained noise emits the same short text in
// several segments whose confidence metrics hover just inside the thresholds;
// the verbatim repetition itself is the reliable signature.
func TestParseGroqWordsDropsRepetitionLoops(t *testing.T) {
	data := `{
	  "text": "vale 아 아 아",
	  "words": [
	    {"word": "vale", "start": 0.1, "end": 0.5},
	    {"word": "아", "start": 3.5, "end": 4.5},
	    {"word": "아", "start": 7.0, "end": 8.6},
	    {"word": "아", "start": 11.0, "end": 12.5}
	  ],
	  "segments": [
	    {"start": 0.0, "end": 1.0, "text": " vale", "no_speech_prob": 0.05, "avg_logprob": -0.3},
	    {"start": 3.5, "end": 5.5, "text": " 아", "no_speech_prob": 0.25, "avg_logprob": -0.6},
	    {"start": 7.0, "end": 9.0, "text": " 아", "no_speech_prob": 0.25, "avg_logprob": -0.6},
	    {"start": 11.0, "end": 13.0, "text": " 아", "no_speech_prob": 0.25, "avg_logprob": -0.6}
	  ]
	}`
	cues, err := parseGroqWords([]byte(data))
	if err != nil {
		t.Fatalf("parseGroqWords error = %v", err)
	}
	if len(cues) != 1 || cues[0].Word != "vale" {
		t.Fatalf("cues = %+v, want only 'vale' after dropping the repetition loop", cues)
	}
}

// Regression for the missing "apunta apunta": on noisy chunks auto-detection
// picks an absurd language (Korean) and decodes gibberish the hallucination
// filter drops, losing real speech. Chunks that end up empty are retried in
// both Spanish and English without forcing the rest of the clip.
func TestGroqTranscriber_TranscribeRetriesEmptiedSpanBilingually(t *testing.T) {
	var languages []string
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		languages = append(languages, r.FormValue("language"))
		w.Header().Set("Content-Type", "application/json")
		switch requests {
		case 1: // confident Spanish speech
			_, _ = w.Write([]byte(`{"language":"Spanish","words":[{"word":"joder","start":0.1,"end":0.5},{"word":"tío","start":0.5,"end":0.9}],"segments":[{"start":0,"end":1,"text":" joder tío","no_speech_prob":0.05,"avg_logprob":-0.3}]}`))
		case 2: // noisy chunk: Korean hallucination loop, filtered to nothing
			_, _ = w.Write([]byte(`{"language":"Korean","words":[{"word":"아","start":0.5,"end":1.0},{"word":"아","start":3.0,"end":3.5}],"segments":[{"start":0.5,"end":1.5,"text":" 아","no_speech_prob":0.3,"avg_logprob":-0.85},{"start":3.0,"end":4.0,"text":" 아","no_speech_prob":0.3,"avg_logprob":-0.85}]}`))
		case 3: // Spanish retry decodes real speech
			_, _ = w.Write([]byte(`{"language":"Spanish","words":[{"word":"apunta","start":0.4,"end":0.9},{"word":"apunta","start":1.0,"end":1.5}],"segments":[{"start":0.3,"end":1.6,"text":" apunta apunta","no_speech_prob":0.06,"avg_logprob":-0.46}]}`))
		default: // English retry remains empty
			_, _ = w.Write([]byte(`{"language":"English","words":[]}`))
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}

	transcriber := GroqTranscriber{
		APIKey:       "secret-key",
		BaseURL:      server.URL,
		extractAudio: fakeExtractAudio,
		detectSpeech: func(_ context.Context, _, _ string) ([]speechSpan, error) {
			return []speechSpan{{Start: 4.0, End: 7.4}, {Start: 17.5, End: 27.6}}, nil
		},
		extractChunk: func(_ context.Context, _, _, chunkPath string, _, _ float64) error {
			return os.WriteFile(chunkPath, []byte("fake-chunk"), 0o600)
		},
	}

	cues, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if requests != 4 {
		t.Fatalf("groq requests = %d, want 4 (two spans + Spanish and English retries)", requests)
	}
	if len(languages) != 4 || languages[0] != "" || languages[1] != "" || languages[2] != "es" || languages[3] != "en" {
		t.Fatalf("language fields = %v, want auto, auto, then forced es and en", languages)
	}
	var got []string
	for _, c := range cues {
		got = append(got, c.Word)
	}
	want := []string{"joder", "tío", "apunta", "apunta"}
	if len(got) != len(want) {
		t.Fatalf("words = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("word %d = %q, want %q (all: %v)", i, got[i], want[i], got)
		}
	}
	// The retried words must sit in clip time (span start 17.5 + 0.4).
	if diff := cues[2].StartSeconds - 17.9; diff > 0.001 || diff < -0.001 {
		t.Fatalf("retried word start = %v, want 17.9", cues[2].StartSeconds)
	}
}
