package recording

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSegmentMediaPairsRequiresVideoAndAudio(t *testing.T) {
	artifacts := []RecordingArtifact{
		{SegmentID: "seg-002", TakeID: "take0001", Type: "video", Path: `C:\out\take0001\video.mp4`},
		{SegmentID: "seg-001", TakeID: "take0000", Type: "audio", Path: `C:\out\take0000\audio.wav`},
		{SegmentID: "seg-001", TakeID: "take0000", Type: "video", Path: `C:\out\take0000\video.mp4`},
		{SegmentID: "seg-003", TakeID: "take0002", Type: "audio", Path: `C:\out\take0002\audio.wav`},
	}

	got := segmentMediaPairs(artifacts)
	if len(got) != 1 {
		t.Fatalf("segmentMediaPairs returned %d pairs, want 1", len(got))
	}
	if got[0].segmentID != "seg-001" || got[0].takeID != "take0000" {
		t.Fatalf("pair = %s/%s, want seg-001/take0000", got[0].segmentID, got[0].takeID)
	}
	if got[0].video.Path == "" || got[0].audio.Path == "" {
		t.Fatalf("pair missing video/audio: %#v", got[0])
	}
}

func TestMuxSegmentClipsCreatesSegmentOutputs(t *testing.T) {
	dir := t.TempDir()
	ffmpegPath := fakeFFmpeg(t, dir)
	video := filepath.Join(dir, "take0000", "video.mp4")
	audio := filepath.Join(dir, "take0000", "audio.wav")
	writeTestFile(t, video, 2)
	writeTestFile(t, audio, 3)

	plan := testPlan()
	plan.OutputDir = dir
	artifacts := []RecordingArtifact{
		{SegmentID: "seg-001", TakeID: "take0000", Type: "video", Path: video, SizeBytes: 2},
		{SegmentID: "seg-001", TakeID: "take0000", Type: "audio", Path: audio, SizeBytes: 3},
	}

	got := MuxSegmentClips(context.Background(), plan, artifacts, ffmpegPath, "")
	if len(got) != 1 {
		t.Fatalf("MuxSegmentClips returned %d artifacts, want 1", len(got))
	}
	if got[0].SegmentID != "seg-001" || got[0].TakeID != "take0000" {
		t.Fatalf("artifact = %s/%s, want seg-001/take0000", got[0].SegmentID, got[0].TakeID)
	}
	if got[0].Role != "segment" || got[0].Type != "video" {
		t.Fatalf("role/type = %s/%s, want segment/video", got[0].Role, got[0].Type)
	}
	if got[0].ProbeError != "" {
		t.Fatalf("ProbeError = %q", got[0].ProbeError)
	}
	if got[0].SizeBytes == 0 {
		t.Fatalf("SizeBytes = 0, want fake ffmpeg output size")
	}
	if _, err := os.Stat(filepath.Join(dir, "segments", "seg-001.mp4")); err != nil {
		t.Fatalf("mux output missing: %v", err)
	}
}

func fakeFFmpeg(t *testing.T, dir string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, "ffmpeg.cmd")
		body := "@echo off\r\nset last=\r\n:loop\r\nif \"%~1\"==\"\" goto done\r\nset last=%~1\r\nshift\r\ngoto loop\r\n:done\r\necho fake>\"%last%\"\r\n"
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	path := filepath.Join(dir, "ffmpeg")
	body := "#!/bin/sh\nlast=\nfor arg in \"$@\"; do last=\"$arg\"; done\nprintf fake > \"$last\"\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
