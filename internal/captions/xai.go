package captions

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// defaultXAIBaseURL is the xAI API base used when XAITranscriber.BaseURL is
// empty.
const defaultXAIBaseURL = "https://api.x.ai/v1"

// defaultXAIHTTPTimeout bounds a single whole-file speech-to-text round trip.
// xAI takes the entire media file (up to 500MB) in one request, so the timeout
// is generous: a ~30MB clip can take tens of seconds to upload and transcribe.
const defaultXAIHTTPTimeout = 5 * time.Minute

// xaiErrorBodyMax bounds how much of a non-2xx response body is echoed back in
// an error, so an unexpected large/HTML error page cannot bloat the message.
const xaiErrorBodyMax = 512

// XAITranscriber transcribes media through xAI's speech-to-text API (POST
// /v1/stt) to produce word-level cues for BuildASS, mirroring GroqTranscriber's
// shape so it drops into the same worker seam. Unlike the Groq path it uploads
// the whole media file in a single request and lets xAI return word-level
// timestamps directly: no ffmpeg preprocessing, no per-speech-region
// segmentation, and no per-chunk language detection (the source of the Groq
// path's cross-language hallucinations). It never logs or returns the API key.
type XAITranscriber struct {
	APIKey   string
	BaseURL  string // defaults to "https://api.x.ai/v1"
	Language string // ISO code (e.g. "es", "en"), or "auto"/"" to let xAI detect

	// HTTPClient performs the upload. A nil client falls back to one with
	// defaultXAIHTTPTimeout, generous enough for a large single-request upload.
	HTTPClient *http.Client
}

// Transcribe uploads mediaPath to xAI's /stt endpoint in a single request and
// returns the parsed word cues in media time. workDir is unused (no chunk
// files are produced); it is kept for parity with the other transcribers so
// XAITranscriber satisfies the same shape.
func (x XAITranscriber) Transcribe(ctx context.Context, mediaPath, workDir string) ([]WordCue, error) {
	if strings.TrimSpace(x.APIKey) == "" {
		return nil, fmt.Errorf("captions: xai api key not configured")
	}
	if _, err := os.Stat(mediaPath); err != nil {
		return nil, fmt.Errorf("captions: media file not found at %q: %w", mediaPath, err)
	}

	data, err := x.transcribe(ctx, mediaPath)
	if err != nil {
		return nil, err
	}
	return parseXAITranscript(data)
}

// transcribe streams mediaPath to xAI's /stt endpoint as multipart form data
// and returns the raw response body. The media file (up to 500MB) is never
// buffered whole in memory: the multipart payload is produced on a goroutine
// writing into a pipe the HTTP client consumes as it uploads.
func (x XAITranscriber) transcribe(ctx context.Context, mediaPath string) ([]byte, error) {
	f, err := os.Open(mediaPath) // #nosec G304 -- mediaPath is the worker's materialized clip, not user input.
	if err != nil {
		return nil, fmt.Errorf("captions: opening media %q: %w", mediaPath, err)
	}
	defer f.Close()

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	go func() {
		// The xAI docs require the file field to be positioned last in the
		// payload, so the optional language field is written before it. Omit the
		// language field entirely for "auto"/empty so xAI detects it, rather than
		// sending a literal "auto".
		if lang := strings.TrimSpace(x.Language); lang != "" && !strings.EqualFold(lang, "auto") {
			if err := writer.WriteField("language", lang); err != nil {
				_ = pw.CloseWithError(fmt.Errorf("captions: building xai request: %w", err))
				return
			}
		}
		part, err := writer.CreateFormFile("file", filepath.Base(mediaPath))
		if err != nil {
			_ = pw.CloseWithError(fmt.Errorf("captions: building xai request: %w", err))
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("captions: reading media: %w", err))
			return
		}
		if err := writer.Close(); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("captions: building xai request: %w", err))
			return
		}
		_ = pw.Close()
	}()

	baseURL := strings.TrimSpace(x.BaseURL)
	if baseURL == "" {
		baseURL = defaultXAIBaseURL
	}
	url := strings.TrimRight(baseURL, "/") + "/stt"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		return nil, fmt.Errorf("captions: building xai request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+x.APIKey)

	client := x.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultXAIHTTPTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("captions: xai transcription request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("captions: reading xai response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, xaiTranscribeError(resp.StatusCode, respBody)
	}
	return respBody, nil
}

// xaiTranscribeError builds a lowercase, actionable error from a non-2xx xAI
// response. It never includes the API key: only the status code and xAI's own
// error message (if the body parses) or a bounded snippet of the raw body.
func xaiTranscribeError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Error.Message != "" {
		msg = parsed.Error.Message
	}
	msg = strings.ToLower(strings.TrimSpace(msg))
	if len(msg) > xaiErrorBodyMax {
		msg = strings.ToValidUTF8(msg[:xaiErrorBodyMax], "")
	}

	switch {
	case status == http.StatusUnauthorized:
		return fmt.Errorf("captions: xai api key rejected (401 unauthorized): %s", msg)
	case status == http.StatusRequestEntityTooLarge || strings.Contains(msg, "too large"):
		return fmt.Errorf("captions: media file too large for xai (status %d): %s; shorten the clip or check xai's per-request size limit", status, msg)
	default:
		return fmt.Errorf("captions: xai transcription request failed (status %d): %s", status, msg)
	}
}

// xaiTranscript mirrors the subset of xAI's /stt response that ParseXAIJSON
// needs. The optional per-word "speaker" field is intentionally ignored.
type xaiTranscript struct {
	Text     string    `json:"text"`
	Duration float64   `json:"duration"`
	Language string    `json:"language"`
	Words    []xaiWord `json:"words"`
}

type xaiWord struct {
	Text  string  `json:"text"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// parseXAITranscript parses xAI's /stt response into word cues. Entries with
// empty trimmed text or invalid timings (start < 0 or end <= start) are
// dropped, and a transcript with no usable words returns an error whose
// message contains "no words" (the worker relies on that substring to publish
// the clip uncaptioned instead of failing the render).
func parseXAITranscript(data []byte) ([]WordCue, error) {
	var transcript xaiTranscript
	if err := json.Unmarshal(data, &transcript); err != nil {
		return nil, fmt.Errorf("captions: invalid xai transcript json: %w", err)
	}
	cues := make([]WordCue, 0, len(transcript.Words))
	for _, w := range transcript.Words {
		text := strings.TrimSpace(w.Text)
		if text == "" || w.Start < 0 || w.End <= w.Start {
			continue
		}
		cues = append(cues, WordCue{
			Word:         text,
			StartSeconds: w.Start,
			EndSeconds:   w.End,
		})
	}
	if len(cues) == 0 {
		return nil, fmt.Errorf("captions: xai transcript contains no words")
	}
	// xAI returns words in order, but a stable sort by start time is cheap
	// insurance against an out-of-order response reaching BuildASS.
	sort.SliceStable(cues, func(i, j int) bool {
		return cues[i].StartSeconds < cues[j].StartSeconds
	})
	return cues, nil
}
