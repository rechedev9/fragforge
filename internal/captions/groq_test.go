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
		for _, key := range []string{"model", "response_format", "temperature", "language"} {
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
}

func TestGroqTranscriber_Transcribe_OmitsAutoLanguage(t *testing.T) {
	var sawLanguageField bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		if _, ok := r.MultipartForm.Value["language"]; ok {
			sawLanguageField = true
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
