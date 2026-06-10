package renderplan

import (
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/recording"
)

const QualityReportSchemaVersion = "1.0"

type QualityReport struct {
	SchemaVersion string        `json:"schema_version"`
	JobID         uuid.UUID     `json:"job_id"`
	Variant       string        `json:"variant"`
	Status        string        `json:"status"`
	Items         []QualityItem `json:"items"`
	Warnings      []string      `json:"warnings,omitempty"`
	Error         string        `json:"error,omitempty"`
	GeneratedAt   time.Time     `json:"generated_at"`
}

type QualityItem struct {
	SegmentID       string   `json:"segment_id"`
	Status          string   `json:"status"`
	VideoWidth      int      `json:"video_width,omitempty"`
	VideoHeight     int      `json:"video_height,omitempty"`
	DurationSeconds float64  `json:"duration_seconds,omitempty"`
	VideoCodec      string   `json:"video_codec,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
}

func NewQualityReport(jobID uuid.UUID, variant string, result editor.Result) QualityReport {
	report := QualityReport{
		SchemaVersion: QualityReportSchemaVersion,
		JobID:         jobID,
		Variant:       variant,
		Items:         make([]QualityItem, 0, len(result.Shorts)),
		Warnings:      append([]string(nil), result.Warnings...),
		Error:         result.Error,
		GeneratedAt:   time.Now().UTC(),
	}
	for _, short := range result.Shorts {
		report.Items = append(report.Items, qualityItem(short))
	}
	report.Status = summarizeQuality(report.Items, result.Error)
	return report
}

func qualityItem(short editor.ShortResult) QualityItem {
	artifact := short.PublishArtifact
	if isEmptyArtifact(artifact) {
		artifact = short.OutputArtifact
	}
	item := QualityItem{
		SegmentID:       short.SegmentID,
		VideoWidth:      artifact.Width,
		VideoHeight:     artifact.Height,
		DurationSeconds: artifact.DurationSeconds,
		VideoCodec:      artifact.Codec,
		Warnings:        artifactWarnings(artifact),
	}
	if len(item.Warnings) > 0 {
		item.Status = "warning"
	} else {
		item.Status = "ready"
	}
	return item
}

func isEmptyArtifact(artifact recording.RecordingArtifact) bool {
	return artifact.Path == "" &&
		artifact.SizeBytes == 0 &&
		artifact.Width == 0 &&
		artifact.Height == 0 &&
		artifact.DurationSeconds == 0 &&
		artifact.ProbeError == ""
}

func artifactWarnings(artifact recording.RecordingArtifact) []string {
	var warnings []string
	if artifact.ProbeError != "" {
		warnings = append(warnings, "probe_error")
	}
	if artifact.SizeBytes == 0 {
		warnings = append(warnings, "missing_or_empty_video")
	}
	if artifact.Width > 0 && artifact.Height > 0 && (artifact.Width != 1080 || artifact.Height != 1920) {
		warnings = append(warnings, "unexpected_vertical_resolution")
	}
	if artifact.DurationSeconds > 60 {
		warnings = append(warnings, "too_long_for_shorts")
	}
	return warnings
}

func summarizeQuality(items []QualityItem, resultError string) string {
	if resultError != "" {
		return "failed"
	}
	if len(items) == 0 {
		return "unknown"
	}
	for _, item := range items {
		if item.Status != "ready" {
			return "warning"
		}
	}
	return "ready"
}
