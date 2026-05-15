package recording

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectArtifactsMapsTakeFoldersToSegments(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "take0001", "video.mp4"), 4)
	writeTestFile(t, filepath.Join(dir, "take0000", "audio.wav"), 3)
	writeTestFile(t, filepath.Join(dir, "take0000", "video.mp4"), 2)
	writeTestFile(t, filepath.Join(dir, "segments", "seg-001.mp4"), 5)
	writeTestFile(t, filepath.Join(dir, "recording.js"), 1)

	plan := testPlan()
	plan.OutputDir = dir

	got := CollectArtifacts(context.Background(), plan, "")
	if len(got) != 3 {
		t.Fatalf("CollectArtifacts returned %d artifacts, want 3: %#v", len(got), got)
	}

	assertArtifact(t, got[0], "seg-001", "take0000", "video", "raw", "video.mp4", 2)
	assertArtifact(t, got[1], "seg-001", "take0000", "audio", "raw", "audio.wav", 3)
	assertArtifact(t, got[2], "seg-002", "take0001", "video", "raw", "video.mp4", 4)
}

func TestCollectArtifactsLeavesUnmappedExtraTakes(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "take0009", "video.mp4"), 1)

	plan := testPlan()
	plan.OutputDir = dir
	plan.Segments = plan.Segments[:0]

	got := CollectArtifacts(context.Background(), plan, "")
	if len(got) != 1 {
		t.Fatalf("CollectArtifacts returned %d artifacts, want 1", len(got))
	}
	if got[0].SegmentID != "" {
		t.Fatalf("SegmentID = %q, want empty", got[0].SegmentID)
	}
}

func TestApplyProbeOutputVideo(t *testing.T) {
	artifact := RecordingArtifact{Type: "video"}
	err := applyProbeOutput(&artifact, []byte(`{
		"streams": [{
			"codec_type": "video",
			"codec_name": "h264",
			"width": 1920,
			"height": 1080,
			"duration": "8.016667",
			"nb_frames": "481"
		}],
		"format": {"duration": "8.016667"}
	}`))
	if err != nil {
		t.Fatalf("applyProbeOutput error = %v", err)
	}
	if artifact.Codec != "h264" || artifact.Type != "video" {
		t.Fatalf("artifact codec/type = %q/%q", artifact.Codec, artifact.Type)
	}
	if artifact.Width != 1920 || artifact.Height != 1080 {
		t.Fatalf("artifact size = %dx%d", artifact.Width, artifact.Height)
	}
	if artifact.FrameCount != 481 {
		t.Fatalf("FrameCount = %d, want 481", artifact.FrameCount)
	}
	if artifact.FrameRate != "" {
		t.Fatalf("FrameRate = %q, want empty because fixture omits avg_frame_rate", artifact.FrameRate)
	}
	if artifact.DurationSeconds != 8.016667 {
		t.Fatalf("DurationSeconds = %f, want 8.016667", artifact.DurationSeconds)
	}
}

func TestApplyProbeOutputAudio(t *testing.T) {
	artifact := RecordingArtifact{Type: "audio"}
	err := applyProbeOutput(&artifact, []byte(`{
		"streams": [{
			"codec_type": "audio",
			"codec_name": "pcm_s16le",
			"sample_rate": "44100",
			"channels": 2,
			"duration": "5.015510"
		}]
	}`))
	if err != nil {
		t.Fatalf("applyProbeOutput error = %v", err)
	}
	if artifact.Codec != "pcm_s16le" || artifact.Type != "audio" {
		t.Fatalf("artifact codec/type = %q/%q", artifact.Codec, artifact.Type)
	}
	if artifact.SampleRate != 44100 || artifact.Channels != 2 {
		t.Fatalf("audio shape = %d/%d", artifact.SampleRate, artifact.Channels)
	}
	if artifact.DurationSeconds != 5.015510 {
		t.Fatalf("DurationSeconds = %f, want 5.015510", artifact.DurationSeconds)
	}
}

func writeTestFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertArtifact(t *testing.T, got RecordingArtifact, segmentID, takeID, typ, role, base string, size int64) {
	t.Helper()
	if got.SegmentID != segmentID {
		t.Errorf("SegmentID = %q, want %q", got.SegmentID, segmentID)
	}
	if got.TakeID != takeID {
		t.Errorf("TakeID = %q, want %q", got.TakeID, takeID)
	}
	if got.Type != typ {
		t.Errorf("Type = %q, want %q", got.Type, typ)
	}
	if got.Role != role {
		t.Errorf("Role = %q, want %q", got.Role, role)
	}
	if filepath.Base(got.Path) != base {
		t.Errorf("Path base = %q, want %q", filepath.Base(got.Path), base)
	}
	if got.SizeBytes != size {
		t.Errorf("SizeBytes = %d, want %d", got.SizeBytes, size)
	}
}
