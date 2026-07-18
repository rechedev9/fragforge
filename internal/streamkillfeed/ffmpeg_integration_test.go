package streamkillfeed

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

func TestAnalyzerScanPreservesNativeNTSCPTS(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}

	sourcePath := filepath.Join(t.TempDir(), "killfeed-30000-1001.mp4")
	generateSyntheticKillfeed(t, ffmpeg, sourcePath, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	events, err := (Analyzer{FFmpegPath: ffmpeg}).Scan(
		ctx,
		sourcePath,
		streamclips.SourceProbe{
			Width:            1280,
			Height:           720,
			DurationSeconds:  2,
			FrameRate:        "30000/1001",
			VideoTimeBase:    "1/30000",
			StartTimeSeconds: 0,
		},
		streamclips.CropRect{X: 0.7, Y: 0, Width: 0.3, Height: 0.2},
		streamclips.ClipRange{ID: "clip-ntsc", StartSeconds: 0.5, EndSeconds: 1.8},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d: %+v", got, want, events)
	}
	event := events[0]
	if got, want := event.SourcePTS, int64(30030); got != want {
		t.Errorf("SourcePTS = %d, want frame 30 PTS %d", got, want)
	}
	if got, want := event.TimeBase, (TimeBase{Num: 1, Den: 30000}); got != want {
		t.Errorf("TimeBase = %+v, want %+v", got, want)
	}
	if got, want := event.OnsetStartPTS, int64(29029); got != want {
		t.Errorf("OnsetStartPTS = %d, want preceding frame PTS %d", got, want)
	}
	if got, want := event.Mode, ModeAlignedFrame; got != want {
		t.Errorf("Mode = %q, want %q", got, want)
	}
	if got, want := event.CueSeconds, 1.001; math.Abs(got-want) > 1e-12 {
		t.Errorf("CueSeconds = %.12f, want exact PTS-derived %.12f", got, want)
	}
	if event.SamplePTS < event.SourcePTS || event.SampleSeconds < event.CueSeconds+SampleDelaySeconds {
		t.Errorf(
			"sample (%d, %.9f) does not follow cue (%d, %.9f) by %.2fs",
			event.SamplePTS, event.SampleSeconds,
			event.SourcePTS, event.CueSeconds,
			SampleDelaySeconds,
		)
	}
}

func TestAnalyzerScanFindsNativePredecessorBeyondLookbackInVFRSource(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}

	sourcePath := filepath.Join(t.TempDir(), "killfeed-vfr-gap.mp4")
	generateSparseVFRKillfeed(t, ffmpeg, sourcePath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	probe, err := (streamclips.FFprobeProber{Path: ffprobe}).Probe(ctx, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	events, err := (Analyzer{FFmpegPath: ffmpeg}).Scan(
		ctx,
		sourcePath,
		probe,
		streamclips.CropRect{X: 0.7, Y: 0, Width: 0.3, Height: 0.2},
		streamclips.ClipRange{ID: "clip-vfr-gap", StartSeconds: 9, EndSeconds: 10.85},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d: %+v", got, want, events)
	}
	event := events[0]
	if got, want := event.CueSeconds, 10.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("CueSeconds = %.12f, want sparse native onset %.12f", got, want)
	}
	if got, want := event.TimeBase.Seconds(event.OnsetStartPTS)-probe.StartTimeSeconds, 0.0; math.Abs(got-want) > 1e-9 {
		t.Errorf("preceding native frame time = %.12f, want %.12f", got, want)
	}
	if got, want := event.Mode, ModeAlignedFrame; got != want {
		t.Errorf("Mode = %q, want %q", got, want)
	}
	if event.SourcePTS == event.OnsetStartPTS {
		t.Errorf("onset PTS %d has no distinct native predecessor", event.SourcePTS)
	}
}

func TestAnalyzerScanSubtractsNonZeroSourceStartTime(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}

	sourcePath := filepath.Join(t.TempDir(), "killfeed-offset.mp4")
	generateSyntheticKillfeed(t, ffmpeg, sourcePath, 5)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	probe, err := (streamclips.FFprobeProber{Path: ffprobe}).Probe(ctx, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := probe.VideoTimeBase, "1/30000"; got != want {
		t.Fatalf("probe.VideoTimeBase = %q, want %q", got, want)
	}
	if got, want := probe.StartTimeSeconds, 5.0; math.Abs(got-want) > 1e-12 {
		t.Fatalf("probe.StartTimeSeconds = %.12f, want %.12f", got, want)
	}
	if got, want := probe.VideoStartTimeSeconds, 5.0; math.Abs(got-want) > 1e-12 {
		t.Fatalf("probe.VideoStartTimeSeconds = %.12f, want %.12f", got, want)
	}
	events, err := (Analyzer{FFmpegPath: ffmpeg}).Scan(
		ctx,
		sourcePath,
		probe,
		streamclips.CropRect{X: 0.7, Y: 0, Width: 0.3, Height: 0.2},
		streamclips.ClipRange{ID: "clip-offset", StartSeconds: 0.5, EndSeconds: 1.8},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d: %+v", got, want, events)
	}
	event := events[0]
	if got, want := event.SourcePTS, int64(180030); got != want {
		t.Errorf("SourcePTS = %d, want offset frame PTS %d", got, want)
	}
	if got, want := event.CueSeconds, 1.001; math.Abs(got-want) > 1e-12 {
		t.Errorf("CueSeconds = %.12f, want source-relative %.12f", got, want)
	}
}

func TestAnalyzerScanUsesContainerOriginWhenVideoStartsLater(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}

	sourcePath := filepath.Join(t.TempDir(), "killfeed-delayed-video.mp4")
	generateSyntheticKillfeedWithDelayedVideo(t, ffmpeg, sourcePath)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	probe, err := (streamclips.FFprobeProber{Path: ffprobe}).Probe(ctx, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := probe.StartTimeSeconds, 0.0; math.Abs(got-want) > 1e-12 {
		t.Fatalf("probe.StartTimeSeconds = %.12f, want format start %.12f", got, want)
	}
	// MP4/AAC muxing may expose the delayed video stream one 30fps tick before
	// the nominal setpts offset (0.967 instead of 1.000). The test needs a
	// materially later video origin, not a fabricated exact mux timestamp.
	if got, want := probe.VideoStartTimeSeconds, 1.0; math.Abs(got-want) > 1.0/30+1e-6 {
		t.Fatalf("probe.VideoStartTimeSeconds = %.12f, want video start within one frame of %.12f", got, want)
	}

	events, err := (Analyzer{FFmpegPath: ffmpeg}).Scan(
		ctx,
		sourcePath,
		probe,
		streamclips.CropRect{X: 0.7, Y: 0, Width: 0.3, Height: 0.2},
		streamclips.ClipRange{ID: "clip-delayed-video", StartSeconds: 1.5, EndSeconds: 2.8},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d: %+v", got, want, events)
	}
	event := events[0]
	wantCue := event.TimeBase.Seconds(event.SourcePTS) - probe.StartTimeSeconds
	if got := event.CueSeconds; math.Abs(got-wantCue) > 1e-12 {
		t.Errorf("CueSeconds = %.12f, want format-relative PTS %.12f", got, wantCue)
	}
	wrongVideoRelativeCue := event.TimeBase.Seconds(event.SourcePTS) - probe.VideoStartTimeSeconds
	if math.Abs(event.CueSeconds-wrongVideoRelativeCue) < 0.9 {
		t.Errorf(
			"CueSeconds = %.12f was incorrectly based on video start; video-relative value is %.12f",
			event.CueSeconds,
			wrongVideoRelativeCue,
		)
	}
}

func generateSyntheticKillfeed(t *testing.T, ffmpeg, sourcePath string, timestampOffset float64) {
	t.Helper()
	filter := strings.Join([]string{
		"drawbox=x=960:y=50:w=280:h=45:color=0xFF0000:t=3:enable='gte(t,1.001)'",
		"drawbox=x=990:y=61:w=90:h=18:color=0xFFFFFF:t=fill:enable='gte(t,1.001)'",
		"drawbox=x=1100:y=61:w=105:h=18:color=0x00FFFF:t=fill:enable='gte(t,1.001)'",
	}, ",")
	args := []string{
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi",
		"-i", "color=c=0x202020:s=1280x720:r=30000/1001:d=2",
		"-vf", filter,
		"-an",
		"-c:v", "libx264",
		"-preset", "ultrafast",
		"-pix_fmt", "yuv420p",
		"-video_track_timescale", "30000",
	}
	if timestampOffset != 0 {
		args = append(args, "-output_ts_offset", formatFFmpegSeconds(timestampOffset))
	}
	args = append(args, sourcePath)
	generate := exec.Command(ffmpeg, args...)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate synthetic NTSC source: %v: %s", err, strings.TrimSpace(string(output)))
	}
}

func generateSyntheticKillfeedWithDelayedVideo(t *testing.T, ffmpeg, sourcePath string) {
	t.Helper()
	filter := strings.Join([]string{
		"drawbox=x=960:y=50:w=280:h=45:color=0xFF0000:t=3:enable='gte(t,1.001)'",
		"drawbox=x=990:y=61:w=90:h=18:color=0xFFFFFF:t=fill:enable='gte(t,1.001)'",
		"drawbox=x=1100:y=61:w=105:h=18:color=0x00FFFF:t=fill:enable='gte(t,1.001)'",
		"setpts=PTS+1/TB",
	}, ",")
	generate := exec.Command(
		ffmpeg,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=0x202020:s=1280x720:r=30:d=2",
		"-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo:d=3",
		"-filter_complex", "[0:v]"+filter+"[v]",
		"-map", "[v]", "-map", "1:a",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-video_track_timescale", "30000",
		"-c:a", "aac",
		sourcePath,
	)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate synthetic source with delayed video: %v: %s", err, strings.TrimSpace(string(output)))
	}
}

func generateSparseVFRKillfeed(t *testing.T, ffmpeg, sourcePath string) {
	t.Helper()
	filter := strings.Join([]string{
		"drawbox=x=960:y=50:w=280:h=45:color=0xFF0000:t=3:enable='gte(t,10)'",
		"drawbox=x=990:y=61:w=90:h=18:color=0xFFFFFF:t=fill:enable='gte(t,10)'",
		"drawbox=x=1100:y=61:w=105:h=18:color=0x00FFFF:t=fill:enable='gte(t,10)'",
		"select='eq(n,0)+eq(n,100)+eq(n,104)+eq(n,108)'",
	}, ",")
	generate := exec.Command(
		ffmpeg,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=0x202020:s=1280x720:r=10:d=11",
		"-vf", filter,
		"-an",
		"-fps_mode:v", "vfr",
		"-c:v", "libx264", "-bf", "2", "-g", "120",
		"-pix_fmt", "yuv420p",
		"-video_track_timescale", "1000",
		sourcePath,
	)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate sparse VFR source: %v: %s", err, strings.TrimSpace(string(output)))
	}
}
