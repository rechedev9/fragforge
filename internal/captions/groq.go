package captions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// defaultGroqBaseURL is the Groq OpenAI-compatible API base used when
// GroqTranscriber.BaseURL is empty.
const defaultGroqBaseURL = "https://api.groq.com/openai/v1"

// defaultGroqModel is the Groq transcription model used when
// GroqTranscriber.Model is empty. The full (non-turbo) large-v3 model is the
// default because stream clips routinely mix languages mid-sentence
// (Spanish commentary with English gaming terms); the distilled turbo
// variant is markedly worse at code-switched speech and tends to hallucinate
// plausible phrases in the detected language instead of transcribing the
// other one. Clips are seconds long, so the extra latency is negligible.
const defaultGroqModel = "whisper-large-v3"

// GroqTranscriber transcribes media through Groq's cloud Whisper API
// (OpenAI-compatible /audio/transcriptions endpoint) to produce word-level
// cues for BuildASS, mirroring Transcriber's local whisper.cpp shape. It
// first extracts a small mono FLAC audio track with FFmpeg (Groq's file size
// cap is 25MB on the free tier and 100MB on the dev tier; FLAC mono 16kHz
// keeps a typical clip comfortably under either), then uploads that audio
// for transcription.
type GroqTranscriber struct {
	APIKey   string
	Model    string // defaults to "whisper-large-v3"
	Language string // ISO-639-1, or "auto"/"" to let Groq auto-detect
	BaseURL  string // defaults to "https://api.groq.com/openai/v1"

	// FFmpegPath is the ffmpeg binary used for audio extraction. Defaults to
	// "ffmpeg" (resolved on PATH).
	FFmpegPath string

	// extractAudio isolates the ffmpeg audio-extraction step behind a seam so
	// tests can fake it without a real ffmpeg binary on disk. The zero value
	// (nil, used by every caller outside this package) falls back to
	// runFFmpegExtractAudio.
	extractAudio func(ctx context.Context, ffmpegPath, mediaPath, audioPath string) error
}

// Transcribe extracts a compact audio track from mediaPath into workDir and
// uploads it to Groq for word-level transcription, returning the parsed word
// cues. It never logs or returns the API key.
func (g GroqTranscriber) Transcribe(ctx context.Context, mediaPath, workDir string) ([]WordCue, error) {
	if strings.TrimSpace(g.APIKey) == "" {
		return nil, fmt.Errorf("captions: groq api key not configured")
	}
	if _, err := os.Stat(mediaPath); err != nil {
		return nil, fmt.Errorf("captions: media file not found at %q: %w", mediaPath, err)
	}

	ffmpegPath := g.FFmpegPath
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	extract := g.extractAudio
	if extract == nil {
		extract = runFFmpegExtractAudio
	}

	audioPath := filepath.Join(workDir, "audio.flac")
	if err := extract(ctx, ffmpegPath, mediaPath, audioPath); err != nil {
		return nil, fmt.Errorf("captions: extracting audio for groq transcription: %w", err)
	}

	data, err := g.transcribeAudio(ctx, audioPath)
	if err != nil {
		return nil, err
	}
	return ParseGroqJSON(data)
}

// runFFmpegExtractAudio extracts a mono 16kHz FLAC audio track from
// mediaPath, the ffmpeg invocation GroqTranscriber.Transcribe uses by
// default.
func runFFmpegExtractAudio(ctx context.Context, ffmpegPath, mediaPath, audioPath string) error {
	// #nosec G204 -- ffmpegPath is a configured local binary, not user input.
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-y",
		"-i", mediaPath,
		"-vn",
		"-ac", "1",
		"-ar", "16000",
		"-c:a", "flac",
		audioPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// transcribeAudio POSTs audioPath as multipart form data to Groq's
// audio/transcriptions endpoint and returns the raw verbose_json response
// body.
func (g GroqTranscriber) transcribeAudio(ctx context.Context, audioPath string) ([]byte, error) {
	f, err := os.Open(audioPath) // #nosec G304 -- audioPath is produced by extractAudio in the caller's work dir.
	if err != nil {
		return nil, fmt.Errorf("captions: opening extracted audio %q: %w", audioPath, err)
	}
	defer f.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return nil, fmt.Errorf("captions: building groq request: %w", err)
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, fmt.Errorf("captions: reading extracted audio: %w", err)
	}

	model := g.Model
	if model == "" {
		model = defaultGroqModel
	}
	fields := map[string]string{
		"model":                     model,
		"response_format":           "verbose_json",
		"timestamp_granularities[]": "word",
		"temperature":               "0",
	}
	// Omit the language field entirely for "auto"/empty so Groq auto-detects,
	// rather than sending a literal "auto" the API would reject.
	if lang := strings.TrimSpace(g.Language); lang != "" && !strings.EqualFold(lang, "auto") {
		fields["language"] = lang
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			return nil, fmt.Errorf("captions: building groq request: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("captions: building groq request: %w", err)
	}

	baseURL := strings.TrimSpace(g.BaseURL)
	if baseURL == "" {
		baseURL = defaultGroqBaseURL
	}
	url := strings.TrimRight(baseURL, "/") + "/audio/transcriptions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return nil, fmt.Errorf("captions: building groq request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+g.APIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("captions: groq transcription request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("captions: reading groq response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, groqTranscribeError(resp.StatusCode, respBody)
	}
	return respBody, nil
}

// groqErrorBody mirrors the subset of Groq/OpenAI's error envelope
// ({"error":{"message":"..."}}) that groqTranscribeError needs.
type groqErrorBody struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// groqTranscribeError builds a lowercase, actionable error from a non-2xx
// Groq response. It never includes the API key: only the status code and
// Groq's own error message (if the body parses) or the raw body otherwise.
func groqTranscribeError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var parsed groqErrorBody
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		msg = parsed.Error.Message
	}
	msg = strings.ToLower(msg)

	switch {
	case status == http.StatusUnauthorized:
		return fmt.Errorf("captions: groq api key rejected (401 unauthorized): %s", msg)
	case status == http.StatusRequestEntityTooLarge || strings.Contains(msg, "too large"):
		return fmt.Errorf("captions: audio file too large for groq (status %d): %s; shorten the clip or check groq's per-request size limit", status, msg)
	default:
		return fmt.Errorf("captions: groq transcription request failed (status %d): %s", status, msg)
	}
}

// groqTranscript mirrors the subset of Groq's verbose_json transcription
// response that ParseGroqJSON needs.
type groqTranscript struct {
	Text  string     `json:"text"`
	Words []groqWord `json:"words"`
}

type groqWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// ParseGroqJSON parses Groq's verbose_json transcription response (with
// timestamp_granularities[]=word) into word cues. Entries with empty or
// punctuation-only text are skipped, mirroring ParseWhisperJSON's filtering.
func ParseGroqJSON(data []byte) ([]WordCue, error) {
	var transcript groqTranscript
	if err := json.Unmarshal(data, &transcript); err != nil {
		return nil, fmt.Errorf("captions: invalid groq transcript json: %w", err)
	}

	cues := make([]WordCue, 0, len(transcript.Words))
	for _, w := range transcript.Words {
		word := strings.TrimSpace(w.Word)
		if !hasWordContent(word) {
			continue
		}
		cues = append(cues, WordCue{
			Word:         word,
			StartSeconds: w.Start,
			EndSeconds:   w.End,
		})
	}

	if len(cues) == 0 {
		return nil, fmt.Errorf("captions: groq transcript contains no words")
	}
	return cues, nil
}
