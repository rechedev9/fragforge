package editor

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/reche/zackvideo/internal/recording"
)

func ValidateShortArtifact(artifact recording.RecordingArtifact) []string {
	var warnings []string
	if artifact.ProbeError != "" {
		return []string{fmt.Sprintf("short %s probe failed: %s", artifact.SegmentID, artifact.ProbeError)}
	}
	if artifact.Path == "" || artifact.SizeBytes == 0 {
		warnings = append(warnings, fmt.Sprintf("short %s output is missing or empty", artifact.SegmentID))
	}
	if artifact.Codec != "" && artifact.Codec != "h264" {
		warnings = append(warnings, fmt.Sprintf("short %s codec = %q, want h264", artifact.SegmentID, artifact.Codec))
	}
	if artifact.Width != 0 && artifact.Height != 0 && (artifact.Width != 1080 || artifact.Height != 1920) {
		warnings = append(warnings, fmt.Sprintf("short %s resolution = %dx%d, want 1080x1920", artifact.SegmentID, artifact.Width, artifact.Height))
	}
	if artifact.FrameRate != "" && !frameRateMatches(artifact.FrameRate, 60) {
		warnings = append(warnings, fmt.Sprintf("short %s frame_rate = %q, want 60fps", artifact.SegmentID, artifact.FrameRate))
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
	if artifact.Width != 0 && artifact.Height != 0 && (artifact.Width != 1080 || artifact.Height != 1920) {
		warnings = append(warnings, fmt.Sprintf("cover %s resolution = %dx%d, want 1080x1920", artifact.SegmentID, artifact.Width, artifact.Height))
	}
	return warnings
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
