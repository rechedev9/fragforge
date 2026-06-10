package renderplan

import (
	"testing"

	"github.com/google/uuid"

	"github.com/reche/zackvideo/internal/editor"
	"github.com/reche/zackvideo/internal/recording"
)

func TestNewQualityReportMarksReadyForUploadReadyArtifact(t *testing.T) {
	report := NewQualityReport(uuid.New(), "natural-hq2-full", editor.Result{
		Shorts: []editor.ShortResult{{
			SegmentID: "seg-001",
			PublishArtifact: recording.RecordingArtifact{
				SizeBytes:       10,
				Width:           1080,
				Height:          1920,
				DurationSeconds: 30,
				Codec:           "h264",
			},
		}},
	})

	if report.Status != "ready" {
		t.Fatalf("status = %q, want ready", report.Status)
	}
	if report.Items[0].Status != "ready" {
		t.Fatalf("item status = %q, want ready", report.Items[0].Status)
	}
}

func TestNewQualityReportWarnsForBadArtifactShape(t *testing.T) {
	report := NewQualityReport(uuid.New(), "natural-hq2-full", editor.Result{
		Shorts: []editor.ShortResult{{
			SegmentID: "seg-001",
			PublishArtifact: recording.RecordingArtifact{
				SizeBytes:       10,
				Width:           1920,
				Height:          1080,
				DurationSeconds: 75,
			},
		}},
	})

	if report.Status != "warning" {
		t.Fatalf("status = %q, want warning", report.Status)
	}
	for _, want := range []string{"unexpected_vertical_resolution", "too_long_for_shorts"} {
		if !containsString(report.Items[0].Warnings, want) {
			t.Fatalf("warnings = %v, missing %q", report.Items[0].Warnings, want)
		}
	}
}

func TestNewQualityReportMarksFailedFromRenderError(t *testing.T) {
	report := NewQualityReport(uuid.New(), "natural-hq2-full", editor.Result{
		Error: "render failed",
	})

	if report.Status != "failed" {
		t.Fatalf("status = %q, want failed", report.Status)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
