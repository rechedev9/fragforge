package editor

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/reche/zackvideo/internal/recording"
)

const youtubeShortMaxSeconds = 180.0

func ValidateShortArtifact(artifact recording.RecordingArtifact) []string {
	return ValidateShortArtifactForFPS(artifact, DefaultPreset().FPS)
}

func ValidateShortArtifactForFPS(artifact recording.RecordingArtifact, fps int) []string {
	// Every registered preset shares the same vertical output contract, so
	// the resolution gate comes from the registry default entry.
	preset := DefaultPreset()
	if fps <= 0 {
		fps = preset.FPS
	}
	var warnings []string
	if artifact.ProbeError != "" {
		return []string{fmt.Sprintf("short %s probe failed: %s", artifact.SegmentID, artifact.ProbeError)}
	}
	if artifact.Path == "" || artifact.SizeBytes == 0 {
		warnings = append(warnings, fmt.Sprintf("short %s output is missing or empty", artifact.SegmentID))
	}
	if artifact.DurationSeconds > youtubeShortMaxSeconds {
		warnings = append(warnings, fmt.Sprintf("short %s duration = %.3fs, want <= %.0fs for YouTube Shorts", artifact.SegmentID, artifact.DurationSeconds, youtubeShortMaxSeconds))
	}
	if artifact.Codec != "" && artifact.Codec != "h264" {
		warnings = append(warnings, fmt.Sprintf("short %s codec = %q, want h264", artifact.SegmentID, artifact.Codec))
	}
	if artifact.Width != 0 && artifact.Height != 0 && (artifact.Width != preset.Width || artifact.Height != preset.Height) {
		warnings = append(warnings, fmt.Sprintf("short %s resolution = %dx%d, want %dx%d", artifact.SegmentID, artifact.Width, artifact.Height, preset.Width, preset.Height))
	}
	if artifact.FrameRate != "" && !frameRateMatches(artifact.FrameRate, float64(fps)) {
		warnings = append(warnings, fmt.Sprintf("short %s frame_rate = %q, want %dfps", artifact.SegmentID, artifact.FrameRate, fps))
	}
	return warnings
}

func ValidateCoverArtifact(artifact recording.RecordingArtifact) []string {
	var warnings []string
	if artifact.ProbeError != "" {
		return []string{fmt.Sprintf("cover %s probe failed: %s", artifact.SegmentID, artifact.ProbeError)}
	}
	if artifact.Path == "" || artifact.SizeBytes == 0 {
		warnings = append(warnings, fmt.Sprintf("cover %s output is missing or empty", artifact.SegmentID))
	}
	preset := DefaultPreset()
	if artifact.Width != 0 && artifact.Height != 0 && (artifact.Width != preset.Width || artifact.Height != preset.Height) {
		warnings = append(warnings, fmt.Sprintf("cover %s resolution = %dx%d, want %dx%d", artifact.SegmentID, artifact.Width, artifact.Height, preset.Width, preset.Height))
	}
	return warnings
}

func ValidateSourceArtifact(artifact recording.RecordingArtifact) []string {
	var warnings []string
	if artifact.ProbeError != "" {
		return []string{fmt.Sprintf("source %s probe failed: %s", artifact.SegmentID, artifact.ProbeError)}
	}
	if artifact.Width != 0 && artifact.Height != 0 && (artifact.Width != 1920 || artifact.Height != 1080) {
		warnings = append(warnings, fmt.Sprintf("source %s resolution = %dx%d, want 1920x1080", artifact.SegmentID, artifact.Width, artifact.Height))
	}
	if artifact.FrameRate != "" && !frameRateMatches(artifact.FrameRate, 60) {
		warnings = append(warnings, fmt.Sprintf("source %s frame_rate = %q, want 60fps", artifact.SegmentID, artifact.FrameRate))
	}
	return warnings
}

var cropDetectPattern = regexp.MustCompile(`crop=([0-9]+):([0-9]+):([0-9]+):([0-9]+)`)

func QualityWarningsFromFFmpegLog(segmentID, log string) []string {
	var warnings []string
	if strings.Contains(log, "black_start:") {
		warnings = append(warnings, fmt.Sprintf("quality %s detected black frames", segmentID))
	}
	if strings.Contains(log, "freeze_start:") {
		warnings = append(warnings, fmt.Sprintf("quality %s detected frozen frames", segmentID))
	}
	if crop := tightestDetectedCrop(log); crop != "" {
		warnings = append(warnings, fmt.Sprintf("quality %s cropdetect suggested %s, possible border/letterbox", segmentID, crop))
	}
	return warnings
}

func tightestDetectedCrop(log string) string {
	matches := cropDetectPattern.FindAllStringSubmatch(log, -1)
	if len(matches) == 0 {
		return ""
	}
	best := ""
	bestArea := 1080 * 1920
	for _, match := range matches {
		w, werr := strconv.Atoi(match[1])
		h, herr := strconv.Atoi(match[2])
		if werr != nil || herr != nil || w <= 0 || h <= 0 {
			continue
		}
		area := w * h
		if area < bestArea {
			bestArea = area
			best = match[0]
		}
	}
	if best == "" {
		return ""
	}
	if bestArea >= 1000*1840 {
		return ""
	}
	return best
}

func frameRateMatches(raw string, want float64) bool {
	parts := strings.Split(raw, "/")
	if len(parts) == 2 {
		n, nerr := strconv.ParseFloat(parts[0], 64)
		d, derr := strconv.ParseFloat(parts[1], 64)
		if nerr == nil && derr == nil && d != 0 {
			return math.Abs(n/d-want) < 0.01
		}
	}
	v, err := strconv.ParseFloat(raw, 64)
	return err == nil && math.Abs(v-want) < 0.01
}
