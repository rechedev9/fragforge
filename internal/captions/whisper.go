package captions

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"
)

// Transcriber runs whisper.cpp's CLI (whisper-cli) locally to produce
// word-level cues for BuildASS. Language defaults to "auto"; for FragForge's
// primarily Spanish content, callers typically set "es".
type Transcriber struct {
	BinaryPath string
	ModelPath  string
	Language   string
}

// Transcribe runs whisper-cli against mediaPath and returns word cues. It
// writes its JSON transcript into workDir (as transcript.json) and parses
// it with ParseWhisperJSON.
func (t Transcriber) Transcribe(ctx context.Context, mediaPath, workDir string) ([]WordCue, error) {
	if _, err := os.Stat(t.BinaryPath); err != nil {
		return nil, fmt.Errorf("captions: whisper binary not found at %q: %w", t.BinaryPath, err)
	}
	if _, err := os.Stat(t.ModelPath); err != nil {
		return nil, fmt.Errorf("captions: whisper model not found at %q: %w", t.ModelPath, err)
	}
	if _, err := os.Stat(mediaPath); err != nil {
		return nil, fmt.Errorf("captions: media file not found at %q: %w", mediaPath, err)
	}

	language := strings.TrimSpace(t.Language)
	if language == "" {
		language = "auto"
	}

	outputPrefix := filepath.Join(workDir, "transcript")
	args := []string{
		"-m", t.ModelPath,
		"-f", mediaPath,
		"-oj",
		"--max-len", "1",
		"-l", language,
		"-of", outputPrefix,
	}

	cmd := exec.CommandContext(ctx, t.BinaryPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("captions: whisper-cli failed: %w: %s", err, stderr.String())
	}

	jsonPath := outputPrefix + ".json"
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("captions: reading whisper transcript %q: %w", jsonPath, err)
	}

	cues, err := ParseWhisperJSON(data)
	if err != nil {
		return nil, fmt.Errorf("captions: parsing whisper transcript %q: %w", jsonPath, err)
	}
	return cues, nil
}

// whisperTranscript mirrors the subset of whisper.cpp's -oj JSON output that
// ParseWhisperJSON needs.
type whisperTranscript struct {
	Transcription []whisperSegment `json:"transcription"`
}

type whisperSegment struct {
	Offsets whisperOffsets `json:"offsets"`
	Text    string         `json:"text"`
}

type whisperOffsets struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

// ParseWhisperJSON parses whisper.cpp's -oj JSON transcript into word cues,
// using the millisecond "offsets" field for timing (more precise than the
// human-readable "timestamps" strings). Entries with empty or
// punctuation-only text are skipped.
func ParseWhisperJSON(data []byte) ([]WordCue, error) {
	var transcript whisperTranscript
	if err := json.Unmarshal(data, &transcript); err != nil {
		return nil, fmt.Errorf("captions: invalid whisper transcript json: %w", err)
	}

	cues := make([]WordCue, 0, len(transcript.Transcription))
	for _, segment := range transcript.Transcription {
		word := strings.TrimSpace(segment.Text)
		if !hasWordContent(word) {
			continue
		}
		cues = append(cues, WordCue{
			Word:         word,
			StartSeconds: float64(segment.Offsets.From) / 1000,
			EndSeconds:   float64(segment.Offsets.To) / 1000,
		})
	}

	if len(cues) == 0 {
		return nil, fmt.Errorf("captions: whisper transcript contains no words: %w", ErrUnusableTranscript)
	}
	return cues, nil
}

// hasWordContent reports whether s contains at least one letter or digit
// (including accented Spanish letters), so purely punctuation entries
// (e.g. "...", "¿") are skipped.
func hasWordContent(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
