package captions

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleXAIJSON exercises the response mapping and the drop rules: a
// whitespace-only word, a negative start, a zero-length span, and a reversed
// span must all be discarded, leaving only the two valid words. The optional
// per-word "speaker" field must be ignored.
const sampleXAIJSON = `{
  "text": "Hola mundo",
  "duration": 2.1,
  "language": "Spanish",
  "words": [
    {"text": "Hola", "start": 0.2, "end": 0.6, "speaker": "A"},
    {"text": "   ", "start": 0.6, "end": 0.9},
    {"text": "mundo", "start": 0.9, "end": 1.3},
    {"text": "neg", "start": -0.1, "end": 0.2},
    {"text": "zero", "start": 1.5, "end": 1.5},
    {"text": "rev", "start": 2.0, "end": 1.9}
  ]
}`

func TestParseXAITranscript(t *testing.T) {
	cues, err := parseXAITranscript([]byte(sampleXAIJSON))
	if err != nil {
		t.Fatalf("parseXAITranscript returned error: %v", err)
	}
	want := []WordCue{
		{Word: "Hola", StartSeconds: 0.2, EndSeconds: 0.6},
		{Word: "mundo", StartSeconds: 0.9, EndSeconds: 1.3},
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

func TestParseXAITranscript_Errors(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "invalid json", data: "{not json"},
		{name: "empty words", data: `{"text":"","words":[]}`},
		{name: "only invalid words", data: `{"words":[{"text":"  ","start":0,"end":0.1},{"text":"x","start":1,"end":1}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseXAITranscript([]byte(tt.data))
			if err == nil {
				t.Fatalf("parseXAITranscript(%q) returned nil error, want an error", tt.data)
			}
		})
	}
}

func TestXAITranscriber_Transcribe(t *testing.T) {
	var gotMethod, gotAuth, gotFormat, gotLanguage string
	var gotFieldOrder, gotKeyterms []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		if !strings.HasSuffix(r.URL.Path, "/stt") {
			t.Errorf("request path = %q, want it to end in /stt", r.URL.Path)
		}
		mr, err := r.MultipartReader()
		if err != nil {
			t.Fatalf("MultipartReader: %v", err)
		}
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("NextPart: %v", err)
			}
			gotFieldOrder = append(gotFieldOrder, part.FormName())
			if part.FormName() == "format" {
				b, _ := io.ReadAll(part)
				gotFormat = string(b)
			}
			if part.FormName() == "language" {
				b, _ := io.ReadAll(part)
				gotLanguage = string(b)
			}
			if part.FormName() == "keyterm" {
				b, _ := io.ReadAll(part)
				gotKeyterms = append(gotKeyterms, string(b))
			}
			_ = part.Close()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleXAIJSON))
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatalf("writing fake media file: %v", err)
	}

	transcriber := XAITranscriber{APIKey: "secret-key", Language: "es", BaseURL: server.URL}
	cues, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("Authorization header = %q, want Bearer secret-key", gotAuth)
	}
	if gotLanguage != "es" {
		t.Errorf("language field = %q, want es", gotLanguage)
	}
	if gotFormat != "true" {
		t.Errorf("format field = %q, want true", gotFormat)
	}
	if got, want := strings.Join(gotKeyterms, "|"), strings.Join(xaiCS2Keyterms, "|"); got != want {
		t.Errorf("keyterm fields = %q, want %q", got, want)
	}
	if len(gotFieldOrder) == 0 || gotFieldOrder[len(gotFieldOrder)-1] != "file" {
		t.Errorf("field order = %v, want the file field positioned last", gotFieldOrder)
	}
	sawLanguage := false
	for _, name := range gotFieldOrder {
		if name == "language" {
			sawLanguage = true
		}
	}
	if !sawLanguage {
		t.Errorf("field order = %v, want a language field present", gotFieldOrder)
	}

	want := []WordCue{
		{Word: "Hola", StartSeconds: 0.2, EndSeconds: 0.6},
		{Word: "mundo", StartSeconds: 0.9, EndSeconds: 1.3},
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

func TestParseXAITranscript_NormalizesOverlapsAndDropsPunctuation(t *testing.T) {
	data := []byte(`{
  "words": [
    {"text":"second","start":0.8,"end":1.2},
    {"text":"first","start":0.0,"end":1.0},
    {"text":"hidden","start":0.9,"end":0.95},
    {"text":"...","start":1.2,"end":1.3}
  ]
}`)
	cues, err := parseXAITranscript(data)
	if err != nil {
		t.Fatalf("parseXAITranscript returned error: %v", err)
	}
	want := []WordCue{
		{Word: "first", StartSeconds: 0, EndSeconds: 1},
		{Word: "second", StartSeconds: 1, EndSeconds: 1.2},
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

func TestXAITranscriber_Transcribe_OmitsAutoOrEmptyLanguage(t *testing.T) {
	for _, lang := range []string{"auto", "AUTO", "", "  "} {
		t.Run("language="+lang, func(t *testing.T) {
			var sawFormat, sawLanguage bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mr, err := r.MultipartReader()
				if err != nil {
					t.Fatalf("MultipartReader: %v", err)
				}
				for {
					part, err := mr.NextPart()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatalf("NextPart: %v", err)
					}
					if part.FormName() == "language" {
						sawLanguage = true
					}
					if part.FormName() == "format" {
						sawFormat = true
					}
					_ = part.Close()
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(sampleXAIJSON))
			}))
			defer server.Close()

			dir := t.TempDir()
			mediaPath := filepath.Join(dir, "clip.mp4")
			if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
				t.Fatal(err)
			}
			transcriber := XAITranscriber{APIKey: "secret-key", Language: lang, BaseURL: server.URL}
			if _, err := transcriber.Transcribe(context.Background(), mediaPath, dir); err != nil {
				t.Fatalf("Transcribe returned error: %v", err)
			}
			if sawLanguage {
				t.Fatalf("request included a language field for %q, want it omitted", lang)
			}
			if sawFormat {
				t.Fatalf("request included a format field for %q without an explicit language, want it omitted", lang)
			}
		})
	}
}

func TestXAITranscriber_Transcribe_NonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"internal boom"}}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcriber := XAITranscriber{APIKey: "secret-key", BaseURL: server.URL}
	_, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err == nil {
		t.Fatal("Transcribe returned nil error, want an error for a 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("got error %q, want it to mention the 500 status", err.Error())
	}
}

func TestReadLimitedXAIResponse(t *testing.T) {
	body, exceeded, err := readLimitedXAIResponse(strings.NewReader("12345"), 4)
	if err != nil {
		t.Fatalf("readLimitedXAIResponse returned error: %v", err)
	}
	if !exceeded {
		t.Fatal("readLimitedXAIResponse exceeded = false, want true")
	}
	if got, want := string(body), "12345"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestXAITranscriber_Transcribe_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Invalid API Key"}}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcriber := XAITranscriber{APIKey: "bad-key", BaseURL: server.URL}
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

// The exact error envelope the live xAI API returns for a bad key (verified
// 2026-07-11): HTTP 400 with a top-level string "error", not the OpenAI-style
// nested {"error":{"message":...}} object. The response must be recognized as
// a key rejection without surfacing the masked credential fragment.
func TestXAITranscriber_Transcribe_LiveErrorEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"Client specified an invalid argument","error":"Incorrect API key provided: xa***QW. You can obtain an API key from https://console.x.ai."}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcriber := XAITranscriber{APIKey: "bad-key", BaseURL: server.URL}
	_, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err == nil {
		t.Fatal("Transcribe returned nil error, want an error for a 400 response")
	}
	if !strings.Contains(err.Error(), "api key rejected") {
		t.Fatalf("got error %q, want it to recognize the rejected api key", err.Error())
	}
	if strings.Contains(err.Error(), `"code"`) {
		t.Fatalf("got error %q, want the parsed message, not the raw json envelope", err.Error())
	}
	if strings.Contains(err.Error(), "xa***QW") {
		t.Fatalf("got error %q, want the masked credential fragment removed", err.Error())
	}
}

func TestXAITranscriber_Transcribe_EmptyWords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"","duration":0.0,"words":[]}`))
	}))
	defer server.Close()

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}
	transcriber := XAITranscriber{APIKey: "secret-key", BaseURL: server.URL}
	_, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err == nil {
		t.Fatal("Transcribe returned nil error, want an error for an empty transcript")
	}
	if !strings.Contains(err.Error(), "no words") {
		t.Fatalf("got error %q, want it to contain \"no words\" (the worker keys on that substring)", err.Error())
	}
}

func TestXAITranscriber_Transcribe_MissingAPIKey(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := XAITranscriber{}.Transcribe(context.Background(), mediaPath, dir)
	if err == nil {
		t.Fatal("Transcribe returned nil error, want an error for a missing api key")
	}
	if !strings.Contains(err.Error(), "api key not configured") {
		t.Fatalf("got error %q, want it to mention the missing api key", err.Error())
	}
}

func TestXAITranscriber_Transcribe_MissingMedia(t *testing.T) {
	dir := t.TempDir()
	_, err := XAITranscriber{APIKey: "secret-key"}.Transcribe(context.Background(), filepath.Join(dir, "nope.mp4"), dir)
	if err == nil {
		t.Fatal("Transcribe returned nil error, want an error for missing media")
	}
	if !strings.Contains(err.Error(), "media file not found") {
		t.Fatalf("got error %q, want it to mention the missing media file", err.Error())
	}
}
