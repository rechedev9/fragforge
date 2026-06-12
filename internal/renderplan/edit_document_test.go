package renderplan

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewEditDocumentSnapshotsStableRenderIntent(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	doc := NewEditDocument(NewEditDocumentOptions{
		JobID:              id,
		Variant:            "viral-60-clean",
		Preset:             "viral-60-clean",
		VideoCRF:           16,
		VideoPreset:        "slow",
		HQFilters:          true,
		AudioNormalize:     true,
		QualityChecks:      true,
		CoverSheets:        true,
		CoversEnabled:      true,
		CaptionsEnabled:    true,
		Output:             OutputShape{AspectRatio: "9:16", Width: 1080, Height: 1920, FPS: 60, Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"},
		RecordingResultKey: "jobs/111/recording/recording-result.json",
		KillPlanSource:     "job.kill_plan",
		OutputPrefix:       "jobs/111/renders/viral-60-clean",
		RenderResultKey:    "jobs/111/renders/viral-60-clean/render-result.json",
		EditManifestKey:    "jobs/111/renders/viral-60-clean/edit-manifest.json",
		PackManifestKey:    "jobs/111/renders/viral-60-clean/pack-manifest.json",
		GalleryKey:         "jobs/111/renders/viral-60-clean/index.html",
		PublishSummaryKey:  "jobs/111/renders/viral-60-clean/publish-summary.md",
		SegmentIDs:         []string{"seg-001"},
	})

	if doc.SchemaVersion != EditDocumentSchemaVersion {
		t.Fatalf("schema = %q, want %q", doc.SchemaVersion, EditDocumentSchemaVersion)
	}
	if doc.JobID != id || doc.Variant != "viral-60-clean" {
		t.Fatalf("identity = %#v", doc)
	}
	if doc.LoadoutSnapshot.Preset != "viral-60-clean" || doc.LoadoutSnapshot.Framing != "full-ui" {
		t.Fatalf("loadout = %#v", doc.LoadoutSnapshot)
	}
	if doc.LoadoutSnapshot.VideoCRF != 16 || doc.LoadoutSnapshot.VideoPreset != "slow" || !doc.LoadoutSnapshot.QualityChecks {
		t.Fatalf("loadout quality snapshot = %#v", doc.LoadoutSnapshot)
	}
	if doc.LoadoutSnapshot.Output.Width != 1080 || doc.LoadoutSnapshot.Output.Height != 1920 {
		t.Fatalf("output snapshot = %#v", doc.LoadoutSnapshot.Output)
	}
	if len(doc.Selection.SegmentIDs) != 1 || doc.Selection.SegmentIDs[0] != "seg-001" {
		t.Fatalf("selection = %#v", doc.Selection)
	}
	if doc.Outputs.UploadReadyRoot != "shortslistosparasubir" {
		t.Fatalf("upload ready root = %q", doc.Outputs.UploadReadyRoot)
	}
}
