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
	"regexp"
	"strconv"
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
//
// Gaming audio defeats a single whole-clip pass: after the first utterance
// Whisper routinely "early-stops" on the gunfire/music that follows and drops
// every later line (observed identically on large-v3 and turbo). So the
// audio is first segmented into speech regions with an ffmpeg voice-band
// silencedetect pass, and each region is transcribed on its own with its
// word timestamps offset back into clip time. When detection fails (no
// ffmpeg, unparseable output) it falls back to the whole-clip request.
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

	// detectSpeech and extractChunk are the same kind of test seams for the
	// speech-region segmentation pass; nil falls back to the ffmpeg-backed
	// implementations.
	detectSpeech func(ctx context.Context, ffmpegPath, audioPath string) ([]speechSpan, error)
	extractChunk func(ctx context.Context, ffmpegPath, audioPath, chunkPath string, start, duration float64) error
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

	if cues, ok := g.transcribeBySpeechSpan(ctx, ffmpegPath, audioPath); ok {
		return cues, nil
	}
	// Whole-clip fallback: span detection unavailable, found nothing usable,
	// or a span request failed (in which case this surfaces the API error).
	data, err := g.transcribeAudio(ctx, audioPath, g.Language)
	if err != nil {
		return nil, err
	}
	return ParseGroqJSON(data)
}

// transcribeBySpeechSpan segments audioPath into speech regions and
// transcribes each on its own, offsetting word timestamps back into clip
// time. The bool reports whether the span pass produced a usable transcript;
// false sends the caller to the whole-clip fallback.
func (g GroqTranscriber) transcribeBySpeechSpan(ctx context.Context, ffmpegPath, audioPath string) ([]WordCue, bool) {
	detect := g.detectSpeech
	if detect == nil {
		detect = runFFmpegDetectSpeech
	}
	spans, err := detect(ctx, ffmpegPath, audioPath)
	if err != nil || len(spans) == 0 {
		return nil, false
	}
	extractChunk := g.extractChunk
	if extractChunk == nil {
		extractChunk = runFFmpegExtractChunk
	}

	// First pass: every span with the configured language (usually auto).
	results := make([]spanResult, 0, len(spans))
	for i, span := range spans {
		chunkPath := filepath.Join(filepath.Dir(audioPath), fmt.Sprintf("audio-span-%03d.flac", i))
		if err := extractChunk(ctx, ffmpegPath, audioPath, chunkPath, span.Start, span.End-span.Start); err != nil {
			return nil, false
		}
		data, err := g.transcribeAudio(ctx, chunkPath, g.Language)
		if err != nil {
			// The API itself failed (auth, size, outage): fall back so the
			// whole-clip request surfaces the same error to the caller.
			return nil, false
		}
		transcript, err := parseGroqTranscript(data)
		if err != nil {
			return nil, false
		}
		results = append(results, spanResult{
			span:       span,
			chunkPath:  chunkPath,
			transcript: transcript,
			words:      cuesFromTranscript(transcript),
		})
	}

	// Second pass: auto-detection picks absurd languages ("Korean") on noisy
	// chunks and decodes gibberish the hallucination filter then rightly
	// drops — losing real speech. Retry those emptied chunks forcing the
	// language the rest of the clip agreed on; a genuinely different-language
	// chunk that decoded confidently on its own is never retried, so
	// code-switched clips keep working.
	if lang := strings.TrimSpace(g.Language); lang == "" || strings.EqualFold(lang, "auto") {
		if majority := majorityLanguage(results); majority != "" {
			for i, r := range results {
				if len(r.words) > 0 || len(r.transcript.Words) == 0 {
					continue // kept something, or was true silence
				}
				if isoLanguageCode(r.transcript.Language) == majority {
					continue // same language; a retry would decode the same
				}
				data, err := g.transcribeAudio(ctx, r.chunkPath, majority)
				if err != nil {
					return nil, false
				}
				transcript, err := parseGroqTranscript(data)
				if err != nil {
					return nil, false
				}
				results[i].words = cuesFromTranscript(transcript)
			}
		}
	}

	var cues []WordCue
	for _, r := range results {
		for _, w := range r.words {
			w.StartSeconds += r.span.Start
			w.EndSeconds += r.span.Start
			cues = append(cues, w)
		}
	}
	if len(cues) == 0 {
		return nil, false
	}
	return cues, true
}

// spanResult is one speech region's first-pass transcription: the raw
// transcript (for its detected language) and the hallucination-filtered
// words in chunk-local time.
type spanResult struct {
	span       speechSpan
	chunkPath  string
	transcript groqTranscript
	words      []WordCue
}

// majorityLanguage returns the ISO-639-1 code most of the kept words were
// decoded in, or "" when nothing was kept or the name is unknown.
func majorityLanguage(results []spanResult) string {
	votes := map[string]int{}
	for _, r := range results {
		if len(r.words) == 0 {
			continue
		}
		if code := isoLanguageCode(r.transcript.Language); code != "" {
			votes[code] += len(r.words)
		}
	}
	majority, best := "", 0
	for code, n := range votes {
		if n > best {
			majority, best = code, n
		}
	}
	return majority
}

// isoLanguageCode maps the language name Groq's verbose_json reports
// ("Spanish", "english") to the ISO-639-1 code its language parameter
// expects. Unknown names return "" and simply skip the retry.
func isoLanguageCode(name string) string {
	codes := map[string]string{
		"english": "en", "spanish": "es", "portuguese": "pt", "french": "fr",
		"german": "de", "italian": "it", "dutch": "nl", "polish": "pl",
		"russian": "ru", "ukrainian": "uk", "turkish": "tr", "arabic": "ar",
		"japanese": "ja", "korean": "ko", "chinese": "zh", "hindi": "hi",
		"swedish": "sv", "norwegian": "no", "danish": "da", "finnish": "fi",
	}
	return codes[strings.ToLower(strings.TrimSpace(name))]
}

// speechSpan is one detected speech region of the extracted audio, in
// seconds from the start of the clip.
type speechSpan struct {
	Start float64
	End   float64
}

// Speech segmentation parameters. Detection runs on a voice-band filtered
// copy of the audio so constant game noise (gunfire lows, music highs) does
// not mask the silences between utterances.
const (
	// speechDetectFilter passes roughly the 300Hz-3.4kHz voice band before
	// silence detection.
	speechDetectFilter = "bandpass=f=1000:width_type=h:w=2400,silencedetect=noise=-28dB:d=0.8"
	speechSpanPad      = 0.3 // seconds added on both sides of a detected region
	speechSpanMergeGap = 0.5 // regions closer than this merge into one
	speechSpanMin      = 0.3 // regions shorter than this are noise blips
	// speechSpanMax caps how many regions are transcribed separately; beyond
	// it the audio is speech-dense enough that the whole-clip pass works.
	speechSpanMax = 16
)

// runFFmpegDetectSpeech runs silencedetect over the voice band of audioPath
// and returns the padded, merged speech regions. Errors are treated as
// "detection unavailable" by the caller, which falls back to the whole clip.
func runFFmpegDetectSpeech(ctx context.Context, ffmpegPath, audioPath string) ([]speechSpan, error) {
	// #nosec G204 -- ffmpegPath is a configured local binary, not user input.
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-hide_banner",
		"-i", audioPath,
		"-af", speechDetectFilter,
		"-f", "null", "-",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg silencedetect failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	duration, silences, err := parseSilenceDetect(stderr.String())
	if err != nil {
		return nil, err
	}
	return speechSpansFromSilences(duration, silences), nil
}

var (
	silenceStartRe = regexp.MustCompile(`silence_start:\s*(-?[0-9.]+)`)
	silenceEndRe   = regexp.MustCompile(`silence_end:\s*(-?[0-9.]+)`)
	durationRe     = regexp.MustCompile(`Duration:\s*(\d+):(\d+):(\d+(?:\.\d+)?)`)
)

// parseSilenceDetect pulls the input duration and the silencedetect regions
// out of ffmpeg's stderr.
func parseSilenceDetect(out string) (duration float64, silences []speechSpan, err error) {
	m := durationRe.FindStringSubmatch(out)
	if m == nil {
		return 0, nil, fmt.Errorf("captions: no duration in ffmpeg output")
	}
	hours, _ := strconv.ParseFloat(m[1], 64)
	minutes, _ := strconv.ParseFloat(m[2], 64)
	seconds, _ := strconv.ParseFloat(m[3], 64)
	duration = hours*3600 + minutes*60 + seconds

	starts := silenceStartRe.FindAllStringSubmatch(out, -1)
	ends := silenceEndRe.FindAllStringSubmatch(out, -1)
	for i, s := range starts {
		start, _ := strconv.ParseFloat(s[1], 64)
		end := duration // a trailing silence_start without an end runs to EOF
		if i < len(ends) {
			end, _ = strconv.ParseFloat(ends[i][1], 64)
		}
		silences = append(silences, speechSpan{Start: max(start, 0), End: min(end, duration)})
	}
	return duration, silences, nil
}

// speechSpansFromSilences inverts silence regions into speech regions, then
// pads, merges, and prunes them. It returns nil when the result would not
// improve on a whole-clip pass: no speech at all, more regions than
// speechSpanMax, or one region covering (almost) the entire clip.
func speechSpansFromSilences(duration float64, silences []speechSpan) []speechSpan {
	if duration <= 0 {
		return nil
	}
	// Invert: the gaps between silences are speech.
	var spans []speechSpan
	cursor := 0.0
	for _, s := range silences {
		if s.Start > cursor {
			spans = append(spans, speechSpan{Start: cursor, End: s.Start})
		}
		cursor = max(cursor, s.End)
	}
	if cursor < duration {
		spans = append(spans, speechSpan{Start: cursor, End: duration})
	}

	// Pad each region, then merge the overlaps/near-misses the padding creates.
	var padded []speechSpan
	for _, s := range spans {
		if s.End-s.Start < speechSpanMin {
			continue
		}
		padded = append(padded, speechSpan{Start: max(s.Start-speechSpanPad, 0), End: min(s.End+speechSpanPad, duration)})
	}
	var merged []speechSpan
	for _, s := range padded {
		if n := len(merged); n > 0 && s.Start-merged[n-1].End < speechSpanMergeGap {
			merged[n-1].End = max(merged[n-1].End, s.End)
			continue
		}
		merged = append(merged, s)
	}

	if len(merged) == 0 || len(merged) > speechSpanMax {
		return nil
	}
	if len(merged) == 1 && merged[0].End-merged[0].Start > duration*0.9 {
		return nil // effectively the whole clip; segmentation buys nothing
	}
	return merged
}

// runFFmpegExtractChunk copies one span of audioPath into chunkPath, the
// per-region extraction GroqTranscriber's span pass uses by default.
func runFFmpegExtractChunk(ctx context.Context, ffmpegPath, audioPath, chunkPath string, start, duration float64) error {
	// #nosec G204 -- ffmpegPath is a configured local binary, not user input.
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-y",
		"-ss", strconv.FormatFloat(start, 'f', 3, 64),
		"-t", strconv.FormatFloat(duration, 'f', 3, 64),
		"-i", audioPath,
		"-c:a", "flac",
		chunkPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg chunk extract failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
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
func (g GroqTranscriber) transcribeAudio(ctx context.Context, audioPath, language string) ([]byte, error) {
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
	// Segment granularity rides along for the per-segment confidence metrics
	// (no_speech_prob, avg_logprob) that filterHallucinatedWords needs.
	fields := [][2]string{
		{"model", model},
		{"response_format", "verbose_json"},
		{"timestamp_granularities[]", "word"},
		{"timestamp_granularities[]", "segment"},
		{"temperature", "0"},
	}
	// Omit the language field entirely for "auto"/empty so Groq auto-detects,
	// rather than sending a literal "auto" the API would reject.
	if lang := strings.TrimSpace(language); lang != "" && !strings.EqualFold(lang, "auto") {
		fields = append(fields, [2]string{"language", lang})
	}
	for _, field := range fields {
		if err := writer.WriteField(field[0], field[1]); err != nil {
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
	Text     string        `json:"text"`
	Language string        `json:"language"`
	Words    []groqWord    `json:"words"`
	Segments []groqSegment `json:"segments"`
}

type groqWord struct {
	Word  string  `json:"word"`
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// groqSegment carries the per-segment confidence metrics Whisper computes;
// together with its text they are the signals separating real speech from the
// text it hallucinates over game noise ("아", phantom thank-yous).
type groqSegment struct {
	Start        float64 `json:"start"`
	End          float64 `json:"end"`
	Text         string  `json:"text"`
	NoSpeechProb float64 `json:"no_speech_prob"`
	AvgLogprob   float64 `json:"avg_logprob"`
}

// Hallucination thresholds: a segment the model itself scores as
// probably-not-speech or decodes with low confidence is dropped rather than
// burned into the video. avg_logprob is the load-bearing gate, calibrated on
// a real gaming clip where it separates cleanly: genuine speech decoded at
// logprob >= -0.58 while noise hallucinations ("아" loops, phantom English)
// decoded at <= -0.87. no_speech_prob alone cannot separate them (shouted
// speech over gunfire and hallucinated noise both score ~0.37), so it keeps
// Whisper's own 0.6 default and only catches the obvious silence case.
// Burned captions favour precision: a dropped mumble is invisible, a
// hallucinated caption is not.
const (
	hallucinationNoSpeechProb = 0.6
	hallucinationAvgLogprob   = -0.7
)

// filterHallucinatedWords drops words that fall inside a hallucination-grade
// segment: one the model scored as low-confidence, or one whose short text
// repeats verbatim across segments of the same response (Whisper's
// repetition-loop signature over sustained noise, e.g. "아 ... 아 ... 아";
// the metrics alone are too flaky near the threshold to catch every loop
// iteration). Without segment metadata (older responses, tests) all words
// pass.
func filterHallucinatedWords(t groqTranscript, words []WordCue) []WordCue {
	if len(t.Segments) == 0 {
		return words
	}
	repeats := map[string]int{}
	for _, s := range t.Segments {
		repeats[strings.TrimSpace(s.Text)]++
	}
	accepted := make([]groqSegment, 0, len(t.Segments))
	for _, s := range t.Segments {
		text := strings.TrimSpace(s.Text)
		loop := text != "" && repeats[text] >= 2 && len(strings.Fields(text)) <= 2
		if !loop && s.NoSpeechProb <= hallucinationNoSpeechProb && s.AvgLogprob >= hallucinationAvgLogprob {
			accepted = append(accepted, s)
		}
	}
	if len(accepted) == len(t.Segments) {
		return words
	}
	// Word timestamps drift outside their segment's bounds (a word can start
	// half a second before its segment), so containment against the rejected
	// ranges misses them. Instead, keep only words near an accepted segment.
	const drift = 0.75 // seconds of timestamp slack around an accepted segment
	kept := words[:0]
	for _, w := range words {
		mid := (w.StartSeconds + w.EndSeconds) / 2
		for _, s := range accepted {
			if mid >= s.Start-drift && mid <= s.End+drift {
				kept = append(kept, w)
				break
			}
		}
	}
	return kept
}

// ParseGroqJSON parses Groq's verbose_json transcription response (with
// timestamp_granularities[]=word) into word cues. Entries with empty or
// punctuation-only text are skipped, mirroring ParseWhisperJSON's filtering.
func ParseGroqJSON(data []byte) ([]WordCue, error) {
	cues, err := parseGroqWords(data)
	if err != nil {
		return nil, err
	}
	if len(cues) == 0 {
		return nil, fmt.Errorf("captions: groq transcript contains no words")
	}
	return cues, nil
}

// parseGroqWords is ParseGroqJSON without the no-words check: a speech span
// that transcribes to nothing (a noise region) is normal, not an error. It
// also drops words from segments Whisper itself scored as hallucination-grade
// (see filterHallucinatedWords).
func parseGroqWords(data []byte) ([]WordCue, error) {
	transcript, err := parseGroqTranscript(data)
	if err != nil {
		return nil, err
	}
	return cuesFromTranscript(transcript), nil
}

func parseGroqTranscript(data []byte) (groqTranscript, error) {
	var transcript groqTranscript
	if err := json.Unmarshal(data, &transcript); err != nil {
		return groqTranscript{}, fmt.Errorf("captions: invalid groq transcript json: %w", err)
	}
	return transcript, nil
}

// cuesFromTranscript turns a transcript into word cues, applying the same
// word filtering ParseWhisperJSON does plus the hallucination filter.
func cuesFromTranscript(transcript groqTranscript) []WordCue {
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
	return filterHallucinatedWords(transcript, cues)
}
