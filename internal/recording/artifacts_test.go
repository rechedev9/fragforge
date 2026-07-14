package recording

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

var benchmarkArtifacts []RecordingArtifact

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

func TestCollectArtifactsCollapsesImageSequencePerTake(t *testing.T) {
	dir := t.TempDir()
	// A TGA-sequence take holds one file per frame; collection must yield a
	// single sequence artifact, not one artifact (and one ffprobe) per frame.
	writeTestFile(t, filepath.Join(dir, "take0000", "0000.tga"), 3)
	writeTestFile(t, filepath.Join(dir, "take0000", "0001.tga"), 4)
	writeTestFile(t, filepath.Join(dir, "take0000", "0002.tga"), 5)

	plan := testPlan()
	plan.OutputDir = dir

	got := CollectArtifacts(context.Background(), plan, "")
	if len(got) != 1 {
		t.Fatalf("CollectArtifacts returned %d artifacts, want 1 collapsed sequence: %#v", len(got), got)
	}
	a := got[0]
	if a.SegmentID != "seg-001" || a.TakeID != "take0000" || a.Type != "video" || a.Role != "raw" {
		t.Errorf("artifact = %+v, want seg-001/take0000/video/raw", a)
	}
	if a.FrameCount != 3 {
		t.Errorf("FrameCount = %d, want 3", a.FrameCount)
	}
	if a.SizeBytes != 12 {
		t.Errorf("SizeBytes = %d, want 12 (sum of frame sizes)", a.SizeBytes)
	}
	if filepath.Base(a.Path) != "0000.tga" {
		t.Errorf("Path = %q, want the lexically-first frame 0000.tga", a.Path)
	}
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

func TestProbeArtifactsUsesBoundedConcurrencyAndSkipsImageSequences(t *testing.T) {
	files := make([]RecordingArtifact, maxConcurrentArtifactProbes+2)
	for i := range files[:len(files)-1] {
		files[i].Path = filepath.Join("take0000", "clip-"+string(rune('a'+i))+".mp4")
	}
	files[len(files)-1].Path = filepath.Join("take0000", "frames.tga")

	started := make(chan struct{}, len(files))
	release := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int32
	go func() {
		probeArtifacts(context.Background(), "ffprobe", files, func(_ context.Context, path string, artifact *RecordingArtifact) {
			if path != "ffprobe" {
				t.Errorf("probe path = %q, want ffprobe", path)
			}
			calls.Add(1)
			started <- struct{}{}
			<-release
			artifact.Codec = "probed"
		})
		close(done)
	}()

	for range maxConcurrentArtifactProbes {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			t.Fatal("probe workers did not start concurrently")
		}
	}
	select {
	case <-started:
		close(release)
		t.Fatalf("more than %d probes ran concurrently", maxConcurrentArtifactProbes)
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("probe workers did not finish")
	}

	if got, want := calls.Load(), int32(len(files)-1); got != want {
		t.Fatalf("probe calls = %d, want %d", got, want)
	}
	for i, artifact := range files[:len(files)-1] {
		if artifact.Codec != "probed" {
			t.Errorf("artifact %d codec = %q, want probed", i, artifact.Codec)
		}
	}
	if files[len(files)-1].Codec != "" {
		t.Fatalf("image sequence codec = %q, want unprobed", files[len(files)-1].Codec)
	}
}

func TestArtifactKeysDeriveRecordingStorageKeys(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	if got, want := ResultArtifactKey(id), "jobs/11111111-1111-1111-1111-111111111111/recording/recording-result.json"; got != want {
		t.Fatalf("result artifact key = %q, want %q", got, want)
	}
	if got, want := ScriptArtifactKey(id), "jobs/11111111-1111-1111-1111-111111111111/recording/recording.js"; got != want {
		t.Fatalf("script artifact key = %q, want %q", got, want)
	}
	if got, want := mustSegmentClipArtifactKey(t, id, "seg-001"), "jobs/11111111-1111-1111-1111-111111111111/recording/segments/seg-001.mp4"; got != want {
		t.Fatalf("segment clip artifact key = %q, want %q", got, want)
	}
}

func mustSegmentClipArtifactKey(t *testing.T, id uuid.UUID, segmentID string) string {
	t.Helper()
	key, err := SegmentClipArtifactKey(id, segmentID)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestApplyProbeOutputVideo(t *testing.T) {
	artifact := RecordingArtifact{Type: "video"}
	err := ApplyProbeOutput(&artifact, []byte(`{
		"streams": [{
			"codec_type": "video",
			"codec_name": "h264",
			"width": 1920,
			"height": 1080,
			"duration": "8.016667",
			"nb_frames": "481",
			"r_frame_rate": "60/1"
		}],
		"format": {"duration": "8.016667", "size": "12345"}
	}`))
	if err != nil {
		t.Fatalf("ApplyProbeOutput error = %v", err)
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
	if artifact.FrameRate != "60/1" {
		t.Fatalf("FrameRate = %q, want r_frame_rate fallback", artifact.FrameRate)
	}
	if artifact.DurationSeconds != 8.016667 {
		t.Fatalf("DurationSeconds = %f, want 8.016667", artifact.DurationSeconds)
	}
	if artifact.SizeBytes != 12345 {
		t.Fatalf("SizeBytes = %d, want 12345", artifact.SizeBytes)
	}
}

func TestApplyProbeOutputAudio(t *testing.T) {
	artifact := RecordingArtifact{Type: "audio"}
	err := ApplyProbeOutput(&artifact, []byte(`{
		"streams": [{
			"codec_type": "audio",
			"codec_name": "pcm_s16le",
			"sample_rate": "44100",
			"channels": 2,
			"duration": "5.015510"
		}]
	}`))
	if err != nil {
		t.Fatalf("ApplyProbeOutput error = %v", err)
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

func BenchmarkProbeArtifacts(b *testing.B) {
	base := make([]RecordingArtifact, 9)
	for i := range base[:8] {
		base[i].Path = filepath.Join("take0000", "clip-"+string(rune('a'+i))+".mp4")
	}
	base[8].Path = filepath.Join("take0000", "frames.tga")
	probe := func(context.Context, string, *RecordingArtifact) {
		time.Sleep(time.Millisecond)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		files := append([]RecordingArtifact(nil), base...)
		probeArtifacts(context.Background(), "ffprobe", files, probe)
		benchmarkArtifacts = files
	}
}
