package captions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestGroqTranscriberCorrectCues(t *testing.T) {
	input := []WordCue{
		{Word: "que", StartSeconds: 0.125, EndSeconds: 0.375},
		{Word: "molo", StartSeconds: 0.5, EndSeconds: 0.875},
		{Word: "en", StartSeconds: 1.0, EndSeconds: 1.125},
		{Word: "conector", StartSeconds: 1.25, EndSeconds: 1.75},
		{Word: "underheaven", StartSeconds: 2.125, EndSeconds: 2.875},
	}
	original := append([]WordCue(nil), input...)

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("request path = %q, want /chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-key" {
			t.Fatalf("Authorization = %q, want Bearer secret-key", got)
		}
		var request struct {
			Model          string  `json:"model"`
			Temperature    float64 `json:"temperature"`
			ResponseFormat struct {
				Type string `json:"type"`
			} `json:"response_format"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode correction request: %v", err)
		}
		if request.Model != "llama-test" {
			t.Errorf("model = %q, want llama-test", request.Model)
		}
		if request.Temperature != 0 {
			t.Errorf("temperature = %v, want 0", request.Temperature)
		}
		if request.ResponseFormat.Type != "json_object" {
			t.Errorf("response_format.type = %q, want json_object", request.ResponseFormat.Type)
		}
		if len(request.Messages) != 2 || request.Messages[0].Role != "system" || request.Messages[1].Role != "user" {
			t.Fatalf("messages = %+v, want system and user messages", request.Messages)
		}
		if !strings.Contains(request.Messages[0].Content, "untrusted") {
			t.Errorf("system prompt does not identify tokens as untrusted data: %q", request.Messages[0].Content)
		}
		if !strings.Contains(request.Messages[0].Content, "cue-text") || !strings.Contains(request.Messages[0].Content, "insert spaces") {
			t.Errorf("system prompt does not describe per-index ASR word splitting: %q", request.Messages[0].Content)
		}
		var payload struct {
			Language string `json:"language"`
			Tokens   []struct {
				Index int    `json:"index"`
				Token string `json:"token"`
			} `json:"tokens"`
		}
		if err := json.Unmarshal([]byte(request.Messages[1].Content), &payload); err != nil {
			t.Fatalf("decode correction user payload: %v", err)
		}
		if payload.Language != "es" || len(payload.Tokens) != len(input) {
			t.Fatalf("payload = %+v, want language es and %d tokens", payload, len(input))
		}
		writeGroqCorrectionResponse(t, w, []indexedCorrection{
			{Index: 0, Token: "Qué,"},
			{Index: 1, Token: "Molotov"},
			{Index: 2, Token: "en"},
			{Index: 3, Token: "connector."},
			{Index: 4, Token: "under Heaven"},
		})
	}))
	defer server.Close()

	got := (GroqTranscriber{
		APIKey:          "secret-key",
		Language:        "es",
		BaseURL:         server.URL,
		CorrectionModel: "llama-test",
	}).correctCues(context.Background(), input)

	want := []WordCue{
		{Word: "Qué,", StartSeconds: 0.125, EndSeconds: 0.375},
		{Word: "Molotov", StartSeconds: 0.5, EndSeconds: 0.875},
		{Word: "en", StartSeconds: 1.0, EndSeconds: 1.125},
		{Word: "connector.", StartSeconds: 1.25, EndSeconds: 1.75},
		{Word: "under Heaven", StartSeconds: 2.125, EndSeconds: 2.875},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("correctCues = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatalf("correctCues mutated input: got %+v, want %+v", input, original)
	}
	if requests != 1 {
		t.Fatalf("correction requests = %d, want 1", requests)
	}
}

func TestGroqTranscriberCorrectCuesAcceptsReviewedAliases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeGroqCorrectionResponse(t, w, []indexedCorrection{
			{Index: 0, Token: "Molotov"},
			{Index: 1, Token: "AK-47"},
		})
	}))
	defer server.Close()

	input := []WordCue{{Word: "molo", StartSeconds: 1, EndSeconds: 2}, {Word: "ak", StartSeconds: 3, EndSeconds: 4}}
	got := (GroqTranscriber{APIKey: "key", BaseURL: server.URL, CorrectionModel: "model"}).correctCues(context.Background(), input)
	if got[0].Word != "Molotov" || got[1].Word != "AK-47" {
		t.Fatalf("correctCues aliases = %+v, want Molotov and AK-47", got)
	}
}

func TestGroqTranscriberCorrectCuesStructuralFailuresFallBack(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		correction []indexedCorrection
	}{
		{name: "http error", status: http.StatusInternalServerError, body: `{"error":{"message":"unavailable"}}`},
		{name: "malformed response", status: http.StatusOK, body: `{`},
		{name: "malformed content", status: http.StatusOK, body: `{"choices":[{"message":{"content":"not json"}}]}`},
		{name: "empty choices", status: http.StatusOK, body: `{"choices":[]}`},
		{name: "wrong count", correction: []indexedCorrection{{Index: 0, Token: "Hola"}}},
		{name: "wrong index", correction: []indexedCorrection{{Index: 0, Token: "Hola"}, {Index: 2, Token: "AK-47"}}},
		{name: "wrong order", correction: []indexedCorrection{{Index: 1, Token: "AK-47"}, {Index: 0, Token: "Hola"}}},
		{name: "oversized response", status: http.StatusOK, body: strings.Repeat("x", (1<<20)+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				status := tt.status
				if status == 0 {
					status = http.StatusOK
				}
				w.WriteHeader(status)
				if tt.body != "" {
					_, _ = w.Write([]byte(tt.body))
					return
				}
				writeGroqCorrectionResponse(t, w, tt.correction)
			}))
			defer server.Close()

			input := []WordCue{{Word: "hola", StartSeconds: 0.1, EndSeconds: 0.2}, {Word: "AK47", StartSeconds: 0.3, EndSeconds: 0.4}}
			original := append([]WordCue(nil), input...)
			got := (GroqTranscriber{APIKey: "key", BaseURL: server.URL, CorrectionModel: "model"}).correctCues(context.Background(), input)
			if !reflect.DeepEqual(got, original) {
				t.Fatalf("correctCues = %+v, want original %+v", got, original)
			}
			if !reflect.DeepEqual(input, original) {
				t.Fatalf("correctCues mutated input: got %+v, want %+v", input, original)
			}
		})
	}
}

func TestGroqTranscriberCorrectCuesKeepsOnlySemanticallyInvalidTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeGroqCorrectionResponse(t, w, []indexedCorrection{
			{Index: 0, Token: "Qué,"},
			{Index: 1, Token: "Adiós"},
			{Index: 2, Token: "Molotov"},
			{Index: 3, Token: "AK-47."},
		})
	}))
	defer server.Close()

	input := []WordCue{
		{Word: "que", StartSeconds: 0.1, EndSeconds: 0.2},
		{Word: "hola", StartSeconds: 0.3, EndSeconds: 0.4},
		{Word: "molo", StartSeconds: 0.5, EndSeconds: 0.6},
		{Word: "AK47", StartSeconds: 0.7, EndSeconds: 0.8},
	}
	original := append([]WordCue(nil), input...)
	got := (GroqTranscriber{APIKey: "key", BaseURL: server.URL, CorrectionModel: "model"}).correctCues(context.Background(), input)
	want := []WordCue{
		{Word: "Qué,", StartSeconds: 0.1, EndSeconds: 0.2},
		{Word: "hola", StartSeconds: 0.3, EndSeconds: 0.4},
		{Word: "Molotov", StartSeconds: 0.5, EndSeconds: 0.6},
		{Word: "AK-47.", StartSeconds: 0.7, EndSeconds: 0.8},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("correctCues = %+v, want safe per-token corrections %+v", got, want)
	}
	if !reflect.DeepEqual(input, original) {
		t.Fatalf("correctCues mutated input: got %+v, want %+v", input, original)
	}
}

func TestGroqTranscriberCorrectCuesKeepsOnlySyntacticallyInvalidTokens(t *testing.T) {
	tests := []struct {
		name  string
		token string
	}{
		{name: "tab", token: "under\tHeaven"},
		{name: "newline", token: "under\nHeaven"},
		{name: "non-ASCII whitespace", token: "under\u00a0Heaven"},
		{name: "leading space", token: " under Heaven"},
		{name: "trailing space", token: "under Heaven "},
		{name: "repeated space", token: "under  Heaven"},
		{name: "more than three words", token: "un der hea ven"},
		{name: "more than 96 bytes", token: strings.Repeat("u", 97)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeGroqCorrectionResponse(t, w, []indexedCorrection{
					{Index: 0, Token: "Qué,"},
					{Index: 1, Token: tt.token},
					{Index: 2, Token: "AK-47"},
				})
			}))
			defer server.Close()

			input := []WordCue{
				{Word: "que", StartSeconds: 0.1, EndSeconds: 0.2},
				{Word: "underheaven", StartSeconds: 0.3, EndSeconds: 0.4},
				{Word: "ak", StartSeconds: 0.5, EndSeconds: 0.6},
			}
			got := (GroqTranscriber{APIKey: "key", BaseURL: server.URL, CorrectionModel: "model"}).correctCues(context.Background(), input)
			want := []WordCue{
				{Word: "Qué,", StartSeconds: 0.1, EndSeconds: 0.2},
				{Word: "underheaven", StartSeconds: 0.3, EndSeconds: 0.4},
				{Word: "AK-47", StartSeconds: 0.5, EndSeconds: 0.6},
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("correctCues = %+v, want invalid index retained and safe corrections applied %+v", got, want)
			}
		})
	}
}

func TestGroqTranscriberCorrectCuesKeepsChangedNumberOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeGroqCorrectionResponse(t, w, []indexedCorrection{
			{Index: 0, Token: "Qué,"},
			{Index: 1, Token: "AK-74"},
		})
	}))
	defer server.Close()

	input := []WordCue{{Word: "que", StartSeconds: 0.1, EndSeconds: 0.2}, {Word: "AK47", StartSeconds: 0.3, EndSeconds: 0.4}}
	got := (GroqTranscriber{APIKey: "key", BaseURL: server.URL, CorrectionModel: "model"}).correctCues(context.Background(), input)
	want := []WordCue{{Word: "Qué,", StartSeconds: 0.1, EndSeconds: 0.2}, {Word: "AK47", StartSeconds: 0.3, EndSeconds: 0.4}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("correctCues = %+v, want changed number retained and safe correction applied %+v", got, want)
	}
}

func TestGroqTranscriberCorrectCuesDisabled(t *testing.T) {
	input := []WordCue{{Word: "hola", StartSeconds: 0.1, EndSeconds: 0.2}}
	got := (GroqTranscriber{APIKey: "key"}).correctCues(context.Background(), input)
	got[0].Word = "changed"
	if input[0].Word != "hola" {
		t.Fatalf("disabled correction returned the input slice instead of a copy: %+v", input)
	}
}

type indexedCorrection struct {
	Index int    `json:"index"`
	Token string `json:"token"`
}

func writeGroqCorrectionResponse(t *testing.T, w http.ResponseWriter, tokens []indexedCorrection) {
	t.Helper()
	content, err := json.Marshal(struct {
		Tokens []indexedCorrection `json:"tokens"`
	}{Tokens: tokens})
	if err != nil {
		t.Fatalf("marshal correction content: %v", err)
	}
	if err := json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]string{"content": string(content)}}},
	}); err != nil {
		t.Fatalf("write correction response: %v", err)
	}
}
