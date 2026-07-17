package streamcli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestStreamArtifactCommandsRejectOutputAliasingBeforeIO(t *testing.T) {
	tests := []struct {
		name string
		args []string
		flag string
	}{
		{
			name: "killfeed overwrites plan",
			args: []string{"killfeed", "--plan", "plan.json", "--events", "events.json", "--out", "plan.json"},
			flag: "--plan",
		},
		{
			name: "captions overwrite words",
			args: []string{"captions", "--plan", "plan.json", "--words", "words.json", "--out", "words.json"},
			flag: "--words",
		},
		{
			name: "transcript overwrites model",
			args: []string{"transcribe", "--input", "stream.mp4", "--plan", "plan.json", "--model", "whisper.bin", "--vad-model", "vad.bin", "--out", "whisper.bin"},
			flag: "--model",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeStreamService{}
			var stdout, stderr bytes.Buffer
			code := runStreamWithService(tt.args, &stdout, &stderr, service)
			if code != exitInvalidArgs || !strings.Contains(stderr.String(), "must not overwrite "+tt.flag) {
				t.Fatalf("code = %d, stderr = %q", code, stderr.String())
			}
			if service.probeCalls != 0 || service.transcribeCalls != 0 || service.renderCalls != 0 {
				t.Fatalf("service calls = probe %d transcribe %d render %d, want none", service.probeCalls, service.transcribeCalls, service.renderCalls)
			}
		})
	}
}

func TestLocalStreamServiceRejectsMissingFFmpeg(t *testing.T) {
	err := (localStreamService{}).ValidateFFmpeg(context.Background(), "fragforge-ffmpeg-that-does-not-exist", false)
	if err == nil || !strings.Contains(err.Error(), "not accessible") {
		t.Fatalf("ValidateFFmpeg error = %v, want inaccessible executable", err)
	}
}
