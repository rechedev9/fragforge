package captions

import (
	"context"
	"encoding/json"
	"errors"
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

const maxXAIFileBytes int64 = 500_000_000

var xaiCS2Keyterms = []string{
	"FragForge",
	"CS2",
	"Counter-Strike 2",
	"AWP",
	"AK-47",
	"M4A1-S",
	"M4A4",
	"Deagle",
	"Molotov",
	"flashbang",
	"clutch",
	"ace",
	"Heaven",
	"connector",
}

// defaultXAIHTTPTimeout bounds a single whole-file speech-to-text round trip.
// xAI takes the entire media file (up to 500MB) in one request, so the timeout
// is generous: a ~30MB clip can take tens of seconds to upload and transcribe.
const defaultXAIHTTPTimeout = 5 * time.Minute

// xaiErrorBodyMax bounds how much of a non-2xx response body is echoed back in
// an error, so an unexpected large/HTML error page cannot bloat the message.
const xaiErrorBodyMax = 512

// xaiSuccessBodyMax bounds the JSON transcript held in memory. The uploaded
// media may be large, but a word-timestamp response for a short stream clip
// should remain far below this defensive ceiling.
const xaiSuccessBodyMax int64 = 64 << 20

// xaiMaxTranscriptionAttempts lets the client recover when xAI returns a
// valid transcript whose word timestamps span only a small part of the media.
// This has been observed with bilingual stream clips: one call may omit the
// first language entirely, while a later call covers the whole clip. Complete
// first responses still use one API call; temporally sparse responses may use
// up to this many calls, separated by bounded backoff.
const xaiMaxTranscriptionAttempts = 3

const xaiPartialSpanThreshold = 0.5

const (
	xaiRetryInitialBackoff = 250 * time.Millisecond
	xaiRetryMaxBackoff     = time.Second
)

// XAITranscriber transcribes media through xAI's speech-to-text API (POST
// /v1/stt) to produce word-level cues for BuildASS. Each attempt uploads the
// whole media file and lets xAI return word-level timestamps directly, with no
// per-region language detection. A complete response takes one attempt; a
// temporally sparse response may take up to xaiMaxTranscriptionAttempts. It
// never logs or returns the API key.
type XAITranscriber struct {
	APIKey   string
	BaseURL  string // defaults to "https://api.x.ai/v1"
	Language string // ISO code (e.g. "es", "en"), or "auto"/"" to let xAI detect

	// HTTPClient performs the upload. A nil client falls back to one with
	// defaultXAIHTTPTimeout, generous enough for a large single-request upload.
	HTTPClient *http.Client
}

// Transcribe uploads mediaPath to xAI's /stt endpoint and returns the parsed
// word cues in media time. It normally makes one request, but may make up to
// xaiMaxTranscriptionAttempts when a response is temporally sparse. workDir is
// unused (no chunk files are produced); it is kept for parity with the other
// transcribers so XAITranscriber satisfies the same shape.
func (x XAITranscriber) Transcribe(ctx context.Context, mediaPath, workDir string) ([]WordCue, error) {
	if strings.TrimSpace(x.APIKey) == "" {
		return nil, fmt.Errorf("captions: xai api key not configured")
	}
	info, err := os.Stat(mediaPath)
	if err != nil {
		return nil, fmt.Errorf("captions: media file not found at %q: %w", mediaPath, err)
	}
	if info.Size() > maxXAIFileBytes {
		return nil, fmt.Errorf("captions: media file is %d bytes, exceeding xai's 500 MB limit", info.Size())
	}

	var best []WordCue
	var bestDuration float64
	var lastErr error
	for attempt := 0; attempt < xaiMaxTranscriptionAttempts; attempt++ {
		data, err := x.transcribe(ctx, mediaPath)
		if err != nil {
			return bestXAITranscriptAfterError(ctx, best, err)
		}
		cues, duration, err := parseXAITranscriptResponse(data)
		if err != nil {
			lastErr = err
			// An empty transcript is worth another attempt; a malformed response is
			// not going to parse any better next time.
			if !errors.Is(err, ErrUnusableTranscript) {
				return bestXAITranscriptAfterError(ctx, best, err)
			}
		} else {
			if betterXAITranscript(cues, duration, best, bestDuration) {
				best = cues
				bestDuration = duration
			}
			// Retry a transcript that covers only part of the clip, and one whose
			// word timings are implausible: both signal xAI returned something
			// other than a faithful reading of the audio.
			if !xaiTranscriptLooksTemporallyPartial(cues, duration) && ValidateTranscript(cues) == nil {
				return cues, nil
			}
		}
		if attempt+1 < xaiMaxTranscriptionAttempts {
			if err := waitForXAIRetry(ctx, attempt+1); err != nil {
				return nil, fmt.Errorf("captions: waiting to retry xai transcription: %w", err)
			}
		}
	}
	// A transcript that stayed implausible across every attempt is returned as-is:
	// ValidateTranscript is the caller's single gate (see
	// transcribeCaptionsWithFallback), which rejects it there and falls back to
	// another backend. Re-deciding that here would make xAI the only backend
	// enforcing the bar on itself.
	if len(best) > 0 {
		return best, nil
	}
	return nil, lastErr
}

func bestXAITranscriptAfterError(ctx context.Context, best []WordCue, err error) ([]WordCue, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("captions: xai transcription interrupted: %w", ctxErr)
	}
	// A usable earlier transcript survives a later attempt's failure. An unusable
	// one cannot rescue it, so report what actually went wrong upstream: that
	// keeps an outage (a rejected key, a 500) classified as an outage instead of
	// reappearing as "the audio had no usable speech".
	if len(best) > 0 && ValidateTranscript(best) == nil {
		return best, nil
	}
	return nil, err
}

func waitForXAIRetry(ctx context.Context, completedAttempts int) error {
	delay := xaiRetryInitialBackoff
	for attempt := 1; attempt < completedAttempts && delay < xaiRetryMaxBackoff; attempt++ {
		delay *= 2
		if delay > xaiRetryMaxBackoff {
			delay = xaiRetryMaxBackoff
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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
	defer pr.Close()
	writer := multipart.NewWriter(pw)
	go func() {
		// The xAI docs require the file field to be positioned last in the
		// payload, so the optional language field is written before it. Omit the
		// language field entirely for "auto"/empty so xAI detects it, rather than
		// sending a literal "auto".
		if lang := strings.TrimSpace(x.Language); lang != "" && !strings.EqualFold(lang, "auto") {
			if err := writer.WriteField("format", "true"); err != nil {
				_ = pw.CloseWithError(fmt.Errorf("captions: building xai request: %w", err))
				return
			}
			if err := writer.WriteField("language", lang); err != nil {
				_ = pw.CloseWithError(fmt.Errorf("captions: building xai request: %w", err))
				return
			}
		}
		for _, term := range xaiCS2Keyterms {
			if err := writer.WriteField("keyterm", term); err != nil {
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

	maxBody := xaiSuccessBodyMax
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		maxBody = xaiErrorBodyMax
	}
	respBody, exceeded, err := readLimitedXAIResponse(resp.Body, maxBody)
	if err != nil {
		return nil, fmt.Errorf("captions: reading xai response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if exceeded {
			respBody = respBody[:int(maxBody)]
		}
		return nil, xaiTranscribeError(resp.StatusCode, respBody)
	}
	if exceeded {
		return nil, fmt.Errorf("captions: xai response exceeds %d bytes", maxBody)
	}
	return respBody, nil
}

func readLimitedXAIResponse(r io.Reader, maxBytes int64) ([]byte, bool, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	return body, int64(len(body)) > maxBytes, nil
}

// xaiTranscribeError builds a lowercase, actionable error from a non-2xx xAI
// response. It never includes the API key: only the status code and xAI's own
// error message (if the body parses) or a bounded snippet of the raw body.
// xAI's live error envelope is {"code":"...","error":"message"} with a
// top-level string error (verified against the real API); the OpenAI-style
// {"error":{"message":"..."}} object is also accepted defensively.
func xaiTranscribeError(status int, body []byte) error {
	msg := strings.TrimSpace(string(body))
	var stringEnvelope struct {
		Error string `json:"error"`
	}
	var objectEnvelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &stringEnvelope); err == nil && stringEnvelope.Error != "" {
		msg = stringEnvelope.Error
	} else if err := json.Unmarshal(body, &objectEnvelope); err == nil && objectEnvelope.Error.Message != "" {
		msg = objectEnvelope.Error.Message
	}
	msg = strings.ToLower(strings.TrimSpace(msg))
	if len(msg) > xaiErrorBodyMax {
		msg = strings.ToValidUTF8(msg[:xaiErrorBodyMax], "")
	}

	switch {
	// A bad key comes back as a 401 or, observed live, as a 400 with an
	// "incorrect api key" message. Do not echo xAI's message here: it contains
	// a masked prefix/suffix of the rejected credential, which must not reach
	// logs or durable render state.
	case status == http.StatusUnauthorized || strings.Contains(msg, "api key"):
		return fmt.Errorf("captions: xai api key rejected (status %d); create a new key, configure it in FragForge Studio Settings or set XAI_API_KEY, and restart FragForge", status)
	case status == http.StatusRequestEntityTooLarge || strings.Contains(msg, "too large"):
		return fmt.Errorf("captions: media file too large for xai (status %d): %s; shorten the clip or check xai's per-request size limit", status, msg)
	default:
		return fmt.Errorf("captions: xai transcription request failed (status %d): %s", status, msg)
	}
}

// xaiTranscript mirrors the subset of xAI's /stt response that
// parseXAITranscript needs. The optional per-word "speaker" field is intentionally ignored.
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
// dropped, and a transcript with no usable words returns an error wrapping
// ErrUnusableTranscript, which the worker treats as a soft failure: try the
// next backend, and publish the clip uncaptioned rather than fail the render.
func parseXAITranscript(data []byte) ([]WordCue, error) {
	cues, _, err := parseXAITranscriptResponse(data)
	return cues, err
}

func parseXAITranscriptResponse(data []byte) ([]WordCue, float64, error) {
	var transcript xaiTranscript
	if err := json.Unmarshal(data, &transcript); err != nil {
		return nil, 0, fmt.Errorf("captions: invalid xai transcript json: %w", err)
	}
	cues := make([]WordCue, 0, len(transcript.Words))
	for _, w := range transcript.Words {
		text := strings.TrimSpace(w.Text)
		if !hasWordContent(text) || w.Start < 0 || w.End <= w.Start {
			continue
		}
		cues = append(cues, WordCue{
			Word:         text,
			StartSeconds: w.Start,
			EndSeconds:   w.End,
		})
	}
	// xAI returns words in order, but a stable sort by start time is cheap
	// insurance against an out-of-order response reaching BuildASS.
	sort.SliceStable(cues, func(i, j int) bool {
		return cues[i].StartSeconds < cues[j].StartSeconds
	})
	cues = normalizeXAICueTimings(cues)
	if len(cues) == 0 {
		return nil, transcript.Duration, fmt.Errorf("captions: xai transcript contains no words: %w", ErrUnusableTranscript)
	}
	return cues, transcript.Duration, nil
}

func xaiTranscriptLooksTemporallyPartial(cues []WordCue, duration float64) bool {
	if duration <= 0 || len(cues) == 0 {
		return false
	}
	return xaiTranscriptSpanRatio(cues, duration) < xaiPartialSpanThreshold
}

func betterXAITranscript(cues []WordCue, duration float64, best []WordCue, bestDuration float64) bool {
	if len(best) == 0 {
		return true
	}
	spanRatio := xaiTranscriptSpanRatio(cues, duration)
	bestSpanRatio := xaiTranscriptSpanRatio(best, bestDuration)
	if spanRatio != bestSpanRatio {
		return spanRatio > bestSpanRatio
	}
	return len(cues) > len(best)
}

// xaiTranscriptSpanRatio measures only the interval from the first usable word
// to the last usable word relative to xAI's reported media duration. It is a
// retry signal for transcripts concentrated in one temporal region, not proof
// that every spoken word or region was transcribed.
func xaiTranscriptSpanRatio(cues []WordCue, duration float64) float64 {
	if len(cues) == 0 {
		return 0
	}
	span := cues[len(cues)-1].EndSeconds - cues[0].StartSeconds
	if duration <= 0 {
		return span
	}
	return span / duration
}

// normalizeXAICueTimings removes small adjacent timestamp overlaps before the
// cues reach BuildASS, whose validation intentionally rejects overlaps. A cue
// fully swallowed by the preceding cue is dropped rather than given a fake
// duration.
func normalizeXAICueTimings(cues []WordCue) []WordCue {
	normalized := cues[:0]
	for _, cue := range cues {
		if n := len(normalized); n > 0 && cue.StartSeconds < normalized[n-1].EndSeconds {
			cue.StartSeconds = normalized[n-1].EndSeconds
			if cue.EndSeconds <= cue.StartSeconds {
				continue
			}
		}
		normalized = append(normalized, cue)
	}
	return normalized
}
