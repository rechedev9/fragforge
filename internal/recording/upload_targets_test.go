package recording

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestNewUploadTargetsDerivesKeysAndPaths(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	outDir := filepath.Join("stage", "recording")
	resultPath := filepath.Join(outDir, "recording-result.json")

	got, err := NewUploadTargets(NewUploadTargetsOptions{
		JobID:      id,
		OutDir:     outDir,
		ResultPath: resultPath,
		Result: RecordingResult{
			Artifacts: []RecordingArtifact{{
				SegmentID: "seg-001",
				Type:      "video",
				Role:      "segment",
				Path:      filepath.Join(outDir, "segments", "seg-001.mp4"),
				SizeBytes: 1,
			}, {
				SegmentID: "seg-001",
				Type:      "audio",
				Role:      "raw",
				Path:      filepath.Join(outDir, "take0000", "audio.wav"),
			}},
		},
	})
	if err != nil {
		t.Fatalf("NewUploadTargets error = %v", err)
	}

	prefix := "jobs/11111111-1111-1111-1111-111111111111/recording"
	want := []UploadTarget{{
		Key:      prefix + "/recording-result.json",
		Path:     resultPath,
		Label:    "recording result",
		Required: true,
	}, {
		Key:            prefix + "/recording.js",
		Path:           filepath.Join(outDir, "recording.js"),
		Label:          "recording script",
		Required:       true,
		MissingMessage: "recording script not found at " + filepath.Join(outDir, "recording.js"),
	}, {
		Key:       prefix + "/segments/seg-001.mp4",
		Path:      filepath.Join(outDir, "segments", "seg-001.mp4"),
		Label:     "segment seg-001",
		Required:  true,
		SegmentID: "seg-001",
	}}
	if len(got) != len(want) {
		t.Fatalf("targets len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("target[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestNewUploadTargetsSkipsFailedSegmentAndKeepsLaterValidClip(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got, err := NewUploadTargets(NewUploadTargetsOptions{
		JobID:      id,
		OutDir:     "recording",
		ResultPath: filepath.Join("recording", "recording-result.json"),
		Result: RecordingResult{
			Error: "capture failed",
			Artifacts: []RecordingArtifact{
				{SegmentID: "seg-001", Type: "video", Role: "segment", Path: "missing.mp4", ProbeError: "ffmpeg mux failed"},
				{SegmentID: "seg-002", Type: "video", Role: "segment", Path: "seg-002.mp4", SizeBytes: 42},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewUploadTargets error = %v", err)
	}
	if gotCount, wantCount := len(got), 3; gotCount != wantCount {
		t.Fatalf("targets len = %d, want %d: %#v", gotCount, wantCount, got)
	}
	if got[2].SegmentID != "seg-002" || got[2].Path != "seg-002.mp4" {
		t.Fatalf("segment target = %#v, want valid seg-002", got[2])
	}
}

func TestNewUploadTargetsKeepsFailedScriptOptional(t *testing.T) {
	got, err := NewUploadTargets(NewUploadTargetsOptions{
		JobID:      uuid.New(),
		OutDir:     "recording",
		ResultPath: filepath.Join("recording", "recording-result.json"),
		Result: RecordingResult{
			Script: filepath.Join("custom", "recording.js"),
			Error:  "recorder failed",
		},
	})
	if err != nil {
		t.Fatalf("NewUploadTargets error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("targets len = %d, want 2: %#v", len(got), got)
	}
	if got[1].Path != filepath.Join("custom", "recording.js") || got[1].Required {
		t.Fatalf("script target = %#v, want custom optional script", got[1])
	}
}
