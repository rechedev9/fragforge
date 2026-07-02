package captions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleWhisperJSON = `{
  "transcription": [
    {
      "timestamps": {"from": "00:00:00,000", "to": "00:00:00,400"},
      "offsets": {"from": 0, "to": 400},
      "text": " Una"
    },
    {
      "timestamps": {"from": "00:00:00,400", "to": "00:00:00,900"},
      "offsets": {"from": 400, "to": 900},
      "text": " kill"
    },
    {
      "timestamps": {"from": "00:00:00,900", "to": "00:00:01,000"},
      "offsets": {"from": 900, "to": 1000},
      "text": " ..."
    },
    {
      "timestamps": {"from": "00:00:01,000", "to": "00:00:01,600"},
      "offsets": {"from": 1000, "to": 1600},
      "text": " limpísima"
    },
    {
      "timestamps": {"from": "00:00:01,600", "to": "00:00:02,000"},
      "offsets": {"from": 1600, "to": 2000},
      "text": " ¿en"
    },
    {
      "timestamps": {"from": "00:00:02,000", "to": "00:00:02,300"},
      "offsets": {"from": 2000, "to": 2300},
      "text": "  "
    }
  ]
}`

func TestParseWhisperJSON(t *testing.T) {
	cues, err := ParseWhisperJSON([]byte(sampleWhisperJSON))
	if err != nil {
		t.Fatalf("ParseWhisperJSON returned error: %v", err)
	}

	want := []WordCue{
		{Word: "Una", StartSeconds: 0, EndSeconds: 0.4},
		{Word: "kill", StartSeconds: 0.4, EndSeconds: 0.9},
		{Word: "limpísima", StartSeconds: 1.0, EndSeconds: 1.6},
		{Word: "¿en", StartSeconds: 1.6, EndSeconds: 2.0},
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

func TestParseWhisperJSON_Errors(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "invalid json", data: "{not json"},
		{name: "no word content", data: `{"transcription":[{"offsets":{"from":0,"to":100},"text":" ... "}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseWhisperJSON([]byte(tt.data))
			if err == nil {
				t.Fatalf("ParseWhisperJSON(%q) returned nil error, want an error", tt.data)
			}
		})
	}
}

func TestTranscribe_MissingBinary(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatalf("writing fake media file: %v", err)
	}

	transcriber := Transcriber{
		BinaryPath: filepath.Join(dir, "does-not-exist-whisper-cli.exe"),
		ModelPath:  filepath.Join(dir, "model.bin"),
	}

	_, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err == nil {
		t.Fatalf("Transcribe returned nil error, want an error for a missing binary")
	}
	if !strings.Contains(err.Error(), "whisper binary not found") {
		t.Fatalf("got error %q, want it to mention the missing binary", err.Error())
	}
}

func TestTranscribe_MissingModel(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "whisper-cli.exe")
	if err := os.WriteFile(binaryPath, []byte("fake binary"), 0o755); err != nil {
		t.Fatalf("writing fake binary file: %v", err)
	}
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatalf("writing fake media file: %v", err)
	}

	transcriber := Transcriber{
		BinaryPath: binaryPath,
		ModelPath:  filepath.Join(dir, "does-not-exist-model.bin"),
	}

	_, err := transcriber.Transcribe(context.Background(), mediaPath, dir)
	if err == nil {
		t.Fatalf("Transcribe returned nil error, want an error for a missing model")
	}
	if !strings.Contains(err.Error(), "whisper model not found") {
		t.Fatalf("got error %q, want it to mention the missing model", err.Error())
	}
}
