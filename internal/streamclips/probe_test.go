package streamclips

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseFFprobeOutputKeepsContainerAndVideoOriginsSeparate(t *testing.T) {
	probe, err := parseFFprobeOutput([]byte(`{
		"streams": [
			{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"r_frame_rate":"30/1","time_base":"1/15360","start_time":"1.000000","duration":"2.000000"},
			{"codec_type":"audio","codec_name":"aac","start_time":"0.000000","duration":"3.000000"}
		],
		"format": {"start_time":"0.000000","duration":"3.000000"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := probe.StartTimeSeconds, 0.0; got != want {
		t.Errorf("StartTimeSeconds = %g, want format start %g", got, want)
	}
	if got, want := probe.VideoStartTimeSeconds, 1.0; got != want {
		t.Errorf("VideoStartTimeSeconds = %g, want video stream start %g", got, want)
	}
	if got, want := probe.VideoTimeBase, "1/15360"; got != want {
		t.Errorf("VideoTimeBase = %q, want %q", got, want)
	}
}

func TestFFprobeProberKeepsContainerAndVideoOriginsSeparate(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}

	sourcePath := filepath.Join(t.TempDir(), "audio-before-video.mp4")
	generate := exec.Command(
		ffmpeg,
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=black:s=320x180:r=30:d=2",
		"-f", "lavfi", "-i", "anullsrc=r=48000:cl=stereo:d=3",
		"-filter_complex", "[0:v]setpts=PTS+1/TB[v]",
		"-map", "[v]", "-map", "1:a",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac",
		sourcePath,
	)
	if output, err := generate.CombinedOutput(); err != nil {
		t.Fatalf("generate source with delayed video: %v: %s", err, strings.TrimSpace(string(output)))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe, err := (FFprobeProber{Path: ffprobe}).Probe(ctx, sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := probe.StartTimeSeconds, 0.0; math.Abs(got-want) > 1e-12 {
		t.Errorf("StartTimeSeconds = %.12f, want format start %.12f", got, want)
	}
	if got, want := probe.VideoStartTimeSeconds, 1.0; math.Abs(got-want) > 1e-12 {
		t.Errorf("VideoStartTimeSeconds = %.12f, want video stream start %.12f", got, want)
	}
}
