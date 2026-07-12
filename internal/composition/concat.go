package composition

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rechedev9/fragforge/internal/recording"
)

func ComposeConcat(ctx context.Context, ffmpegPath string, clips []recording.SegmentClip, outputPath, workDir string) error {
	if ffmpegPath == "" {
		return fmt.Errorf("ffmpeg path is required")
	}
	if len(clips) == 0 {
		return fmt.Errorf("at least one clip is required")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return err
	}
	if workDir == "" {
		workDir = filepath.Dir(outputPath)
	}
	if err := os.MkdirAll(workDir, 0o750); err != nil {
		return err
	}
	listPath := filepath.Join(workDir, "concat-list.txt")
	if err := os.WriteFile(listPath, []byte(ConcatList(clips)), 0o600); err != nil {
		return err
	}
	// #nosec G204 -- ffmpegPath is configured locally and args are not shell-interpolated.
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-y",
		"-v", "error",
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-vf", "fps=60,format=yuv420p",
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "18",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		outputPath,
	)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("ffmpeg concat: %w: %s", err, msg)
		}
		return fmt.Errorf("ffmpeg concat: %w", err)
	}
	return nil
}

func ConcatList(clips []recording.SegmentClip) string {
	var sb strings.Builder
	for _, clip := range clips {
		sb.WriteString("file '")
		// FFmpeg concat lists always want forward slashes, so normalize
		// backslashes unconditionally. filepath.ToSlash only rewrites on
		// Windows, which left Windows-style paths unconverted (and this test
		// failing) when the pipeline or its tests run on Linux/WSL.
		sb.WriteString(escapeConcatPath(strings.ReplaceAll(clip.Path, "\\", "/")))
		sb.WriteString("'\n")
	}
	return sb.String()
}

func escapeConcatPath(path string) string {
	return strings.ReplaceAll(path, "'", "'\\''")
}

func ValidateFinalArtifact(artifact recording.RecordingArtifact, width, height, fps int, expectedDuration float64) []string {
	var warnings []string
	if artifact.ProbeError != "" {
		warnings = append(warnings, fmt.Sprintf("final output probe failed: %s", artifact.ProbeError))
		return warnings
	}
	if artifact.Path == "" || artifact.SizeBytes == 0 {
		warnings = append(warnings, "final output is missing or empty")
	}
	if artifact.Codec != "h264" {
		warnings = append(warnings, fmt.Sprintf("final output codec = %q, want h264", artifact.Codec))
	}
	if artifact.Width != width || artifact.Height != height {
		warnings = append(warnings, fmt.Sprintf("final output resolution = %dx%d, want %dx%d", artifact.Width, artifact.Height, width, height))
	}
	wantFPS := fmt.Sprintf("%d/1", fps)
	if artifact.FrameRate != "" && artifact.FrameRate != wantFPS {
		warnings = append(warnings, fmt.Sprintf("final output frame_rate = %q, want %s", artifact.FrameRate, wantFPS))
	}
	if expectedDuration > 0 && artifact.DurationSeconds > 0 && math.Abs(artifact.DurationSeconds-expectedDuration) > 0.5 {
		warnings = append(warnings, fmt.Sprintf("final output duration %.3fs differs from segment sum %.3fs", artifact.DurationSeconds, expectedDuration))
	}
	return warnings
}

func ClipDurationSum(clips []recording.SegmentClip) float64 {
	var total float64
	for _, clip := range clips {
		total += clip.DurationSeconds
	}
	return total
}
