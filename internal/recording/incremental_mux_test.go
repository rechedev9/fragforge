package recording

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// writeTakeFiles creates a takeNNNN dir under root with the given file names.
func writeTakeFiles(t *testing.T, root, take string, files ...string) {
	t.Helper()
	dir := filepath.Join(root, take)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func incrementalTestPlan(dir string, segmentIDs ...string) RecordingPlan {
	plan := RecordingPlan{OutputDir: dir}
	for _, id := range segmentIDs {
		plan.Segments = append(plan.Segments, RecordingSegment{ID: id})
	}
	return plan
}

// TestFinishedTakePairs drives a recorder output dir through the states it
// passes during a live HLAE run: takes appear one by one, and a take counts as
// finished only once a strictly later take exists and it holds both media
// files. The newest take is never returned - HLAE may still be writing it.
func TestFinishedTakePairs(t *testing.T) {
	tests := []struct {
		name     string
		takes    map[string][]string // take dir -> files inside
		segments []string
		want     []string // finished segment ids, in order
	}{
		{
			name:     "no takes yet",
			takes:    map[string][]string{},
			segments: []string{"s1", "s2"},
			want:     nil,
		},
		{
			name:     "only the first take exists (still recording it)",
			takes:    map[string][]string{"take0000": {"video.mp4", "audio.wav"}},
			segments: []string{"s1", "s2"},
			want:     nil,
		},
		{
			name: "second take started, so the first is finished",
			takes: map[string][]string{
				"take0000": {"video.mp4", "audio.wav"},
				"take0001": {"video.mp4"},
			},
			segments: []string{"s1", "s2"},
			want:     []string{"s1"},
		},
		{
			name: "three takes finish the first two",
			takes: map[string][]string{
				"take0000": {"video.mp4", "audio.wav"},
				"take0001": {"video.mp4", "audio.wav"},
				"take0002": {"video.mp4"},
			},
			segments: []string{"s1", "s2", "s3"},
			want:     []string{"s1", "s2"},
		},
		{
			name: "a take missing audio is skipped, not counted finished",
			takes: map[string][]string{
				"take0000": {"video.mp4"},
				"take0001": {"video.mp4", "audio.wav"},
			},
			segments: []string{"s1", "s2"},
			want:     nil,
		},
		{
			name: "more takes than segments never over-maps",
			takes: map[string][]string{
				"take0000": {"video.mp4", "audio.wav"},
				"take0001": {"video.mp4", "audio.wav"},
				"take0002": {"video.mp4", "audio.wav"},
			},
			segments: []string{"s1"},
			want:     []string{"s1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for take, files := range tt.takes {
				writeTakeFiles(t, dir, take, files...)
			}
			plan := incrementalTestPlan(dir, tt.segments...)

			got := finishedTakePairs(plan)
			ids := make([]string, 0, len(got))
			for _, pair := range got {
				ids = append(ids, pair.segmentID)
			}
			if len(ids) != len(tt.want) {
				t.Fatalf("finished segments = %v, want %v", ids, tt.want)
			}
			for i := range ids {
				if ids[i] != tt.want[i] {
					t.Fatalf("finished segments = %v, want %v", ids, tt.want)
				}
			}
		})
	}
}

// TestIncrementalMuxerPublishesOnceAndAtomically covers the muxer's contract:
// a finished take publishes segments/<id>.mp4 exactly once (a second call does
// not re-mux), and no half-written .part file is left behind.
func TestIncrementalMuxerPublishesOnce(t *testing.T) {
	dir := t.TempDir()
	ffmpeg := fakeFFmpeg(t, dir)
	writeTakeFiles(t, dir, "take0000", "video.mp4", "audio.wav")
	writeTakeFiles(t, dir, "take0001", "video.mp4", "audio.wav")
	plan := incrementalTestPlan(dir, "seg-001", "seg-002")

	muxer := NewIncrementalMuxer(plan, ffmpeg)

	got := muxer.MuxFinished(context.Background())
	if len(got) != 1 || got[0] != "seg-001" {
		t.Fatalf("MuxFinished = %v, want [seg-001]", got)
	}
	out := filepath.Join(dir, "segments", "seg-001.mp4")
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("published clip missing: %v", err)
	}
	if _, err := os.Stat(out + ".part"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}

	// Idempotent: the already-published segment is not re-muxed. Deleting the
	// output proves a second call does not recreate it.
	if err := os.Remove(out); err != nil {
		t.Fatal(err)
	}
	if got := muxer.MuxFinished(context.Background()); got != nil {
		t.Fatalf("second MuxFinished = %v, want nil", got)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("second call re-muxed the segment: %v", err)
	}
}

func TestIncrementalMuxerWithoutFFmpegIsNoOp(t *testing.T) {
	dir := t.TempDir()
	writeTakeFiles(t, dir, "take0000", "video.mp4", "audio.wav")
	writeTakeFiles(t, dir, "take0001", "video.mp4", "audio.wav")
	muxer := NewIncrementalMuxer(incrementalTestPlan(dir, "s1", "s2"), "")
	if got := muxer.MuxFinished(context.Background()); got != nil {
		t.Fatalf("MuxFinished = %v, want nil without ffmpeg", got)
	}
}

// TestMuxSegmentClipsKeepsIncrementallyPublishedClips: the end-of-run pass must
// not rewrite a clip the incremental muxer already published mid-run (an
// observer may be uploading it), only collect its metadata.
func TestMuxSegmentClipsKeepsIncrementallyPublishedClips(t *testing.T) {
	dir := t.TempDir()
	ffmpeg := fakeFFmpeg(t, dir)
	video := filepath.Join(dir, "take0000", "video.mp4")
	audio := filepath.Join(dir, "take0000", "audio.wav")
	writeTakeFiles(t, dir, "take0000", "video.mp4", "audio.wav")

	published := filepath.Join(dir, "segments", "seg-001.mp4")
	if err := os.MkdirAll(filepath.Dir(published), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(published, []byte("incremental"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := incrementalTestPlan(dir, "seg-001")
	artifacts := []RecordingArtifact{
		{SegmentID: "seg-001", TakeID: "take0000", Type: "video", Path: video, SizeBytes: 1},
		{SegmentID: "seg-001", TakeID: "take0000", Type: "audio", Path: audio, SizeBytes: 1},
	}

	got := MuxSegmentClips(context.Background(), plan, artifacts, ffmpeg, "")
	if len(got) != 1 {
		t.Fatalf("MuxSegmentClips returned %d artifacts, want 1", len(got))
	}
	if got[0].SizeBytes != int64(len("incremental")) {
		t.Fatalf("SizeBytes = %d, want the existing clip's size %d", got[0].SizeBytes, len("incremental"))
	}
	b, err := os.ReadFile(published)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "incremental" {
		t.Fatalf("clip content = %q, want the incrementally published bytes kept", b)
	}
}
