package renderplan

import (
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/editor"
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
		Edit:               EditRequest{Format: FormatLandscape16x9, KillEffect: KillEffectVelocity, Transition: TransitionWhip, Intro: true, Outro: true},
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
	if doc.Edit.Format != FormatLandscape16x9 || doc.Edit.KillEffect != KillEffectVelocity || doc.Edit.Transition != TransitionWhip || !doc.Edit.Intro || !doc.Edit.Outro {
		t.Fatalf("edit request = %#v", doc.Edit)
	}
	if doc.Outputs.UploadReadyRoot != "shortslistosparasubir" {
		t.Fatalf("upload ready root = %q", doc.Outputs.UploadReadyRoot)
	}
}

func TestNewEditDocumentForLoadoutDerivesStandardArtifactKeys(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	loadout, err := LoadoutForVariant(editor.PresetViral60Clean)
	if err != nil {
		t.Fatal(err)
	}

	doc, err := NewEditDocumentForLoadout(NewEditDocumentForLoadoutOptions{
		JobID:      id,
		Loadout:    loadout,
		SegmentIDs: []string{"seg-001", "seg-002"},
		Edit:       EditRequest{Format: FormatLandscape16x9},
	})
	if err != nil {
		t.Fatalf("NewEditDocumentForLoadout error = %v", err)
	}

	if doc.JobID != id || doc.Variant != editor.PresetViral60Clean {
		t.Fatalf("identity = %#v", doc)
	}
	if doc.Source.RecordingResultKey != "jobs/11111111-1111-1111-1111-111111111111/recording/recording-result.json" {
		t.Fatalf("recording result key = %q", doc.Source.RecordingResultKey)
	}
	if doc.Source.KillPlanSource != "job.kill_plan" {
		t.Fatalf("kill plan source = %q", doc.Source.KillPlanSource)
	}
	if doc.Outputs.Prefix != "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean" {
		t.Fatalf("output prefix = %q", doc.Outputs.Prefix)
	}
	if doc.Outputs.RenderResult != doc.Outputs.Prefix+"/render-result.json" {
		t.Fatalf("render result key = %q", doc.Outputs.RenderResult)
	}
	if doc.Outputs.EditManifest != doc.Outputs.Prefix+"/edit-manifest.json" {
		t.Fatalf("edit manifest key = %q", doc.Outputs.EditManifest)
	}
	if doc.Outputs.PackManifest != doc.Outputs.Prefix+"/pack-manifest.json" {
		t.Fatalf("pack manifest key = %q", doc.Outputs.PackManifest)
	}
	if doc.Outputs.Gallery != doc.Outputs.Prefix+"/index.html" {
		t.Fatalf("gallery key = %q", doc.Outputs.Gallery)
	}
	if doc.Outputs.PublishSummary != doc.Outputs.Prefix+"/publish-summary.md" {
		t.Fatalf("publish summary key = %q", doc.Outputs.PublishSummary)
	}
	if got, want := doc.Selection.SegmentIDs, []string{"seg-001", "seg-002"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("segment ids = %#v, want %#v", got, want)
	}
	if doc.LoadoutSnapshot.Preset != editor.PresetViral60Clean || doc.LoadoutSnapshot.VideoCRF != loadout.VideoCRF {
		t.Fatalf("loadout snapshot = %#v", doc.LoadoutSnapshot)
	}
	if doc.LoadoutSnapshot.Output.AspectRatio != "16:9" || doc.LoadoutSnapshot.Output.Width != 1920 || doc.LoadoutSnapshot.Output.Height != 1080 {
		t.Fatalf("landscape output snapshot = %#v", doc.LoadoutSnapshot.Output)
	}
}
