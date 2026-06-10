package editor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNormalizeRenderJobs(t *testing.T) {
	tests := []struct {
		name string
		jobs int
		want func(got int) bool
	}{
		{"explicit value wins", 3, func(got int) bool { return got == 3 }},
		{"one stays sequential", 1, func(got int) bool { return got == 1 }},
		{"zero selects automatic bounded limit", 0, func(got int) bool { return got >= 1 && got <= 4 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeRenderJobs(tt.jobs); !tt.want(got) {
				t.Errorf("normalizeRenderJobs(%d) = %d, out of expected range", tt.jobs, got)
			}
		})
	}
}

func TestRunRendersShortsConcurrently(t *testing.T) {
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)
	ffmpegPath := fakeFFmpeg(t, dir)

	sequentialOut := filepath.Join(dir, "shorts-seq")
	sequential, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           sequentialOut,
		FFmpegPath:          ffmpegPath,
		RenderJobs:          1,
	})
	if err != nil {
		t.Fatalf("sequential Run error = %v", err)
	}

	parallelOut := filepath.Join(dir, "shorts-par")
	parallel, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           parallelOut,
		FFmpegPath:          ffmpegPath,
		RenderJobs:          4,
	})
	if err != nil {
		t.Fatalf("parallel Run error = %v", err)
	}

	if got, want := len(parallel.Shorts), len(sequential.Shorts); got != want {
		t.Fatalf("parallel shorts len = %d, want %d", got, want)
	}
	for i, short := range parallel.Shorts {
		if short.SegmentID != sequential.Shorts[i].SegmentID {
			t.Errorf("shorts[%d].SegmentID = %q, want %q", i, short.SegmentID, sequential.Shorts[i].SegmentID)
		}
		if short.OutputArtifact.SizeBytes == 0 {
			t.Errorf("shorts[%d] output artifact missing size: %#v", i, short.OutputArtifact)
		}
		if _, err := os.Stat(short.OutputArtifact.Path); err != nil {
			t.Errorf("shorts[%d] output missing: %v", i, err)
		}
		if short.PublishArtifact.SizeBytes == 0 {
			t.Errorf("shorts[%d] publish artifact missing size: %#v", i, short.PublishArtifact)
		}
	}
	if !reflect.DeepEqual(parallel.Warnings, sequential.Warnings) {
		t.Errorf("parallel warnings = %#v, want sequential order %#v", parallel.Warnings, sequential.Warnings)
	}
}

func TestRunParallelFailingShortReturnsError(t *testing.T) {
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)
	outDir := filepath.Join(dir, "shorts")
	ffmpegPath := fakeFFmpegFailingShorts(t, dir)

	result, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           outDir,
		FFmpegPath:          ffmpegPath,
		RenderJobs:          4,
	})
	if err == nil {
		t.Fatal("Run error = nil, want render failure")
	}
	if result.Error == "" {
		t.Fatalf("result.Error empty, want render failure recorded: %#v", result)
	}
}

func TestRunRejectsNegativeRenderJobs(t *testing.T) {
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)

	_, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           filepath.Join(dir, "shorts"),
		RenderJobs:          -1,
	})
	if err == nil || err.Error() != "render jobs must be >= 0" {
		t.Fatalf("Run error = %v, want render jobs validation error", err)
	}
}
