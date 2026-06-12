package renderplan

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/editor"
)

func TestNewRenderVariantUploadTargetsDerivesKeysAndPaths(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	outDir := filepath.Join("stage", "out")
	publishDir := filepath.Join(outDir, "shortslistosparasubir")
	resultPath := filepath.Join(outDir, "shorts-result.json")
	result := editor.Result{
		SummaryPath: filepath.Join(publishDir, "publish-summary.md"),
		GalleryPath: filepath.Join(publishDir, "index.html"),
		Shorts: []editor.ShortResult{{
			SegmentID:     "seg-001",
			Output:        filepath.Join(outDir, "seg-001.mp4"),
			PublishPath:   filepath.Join(publishDir, "seg-001.mp4"),
			CoverPath:     filepath.Join(publishDir, "seg-001.jpg"),
			CaptionPath:   filepath.Join(publishDir, "seg-001.caption.txt"),
			RenderLogPath: filepath.Join(outDir, "logs", "seg-001-render.log"),
		}, {
			SegmentID: "seg-002",
			Output:    filepath.Join(outDir, "seg-002.mp4"),
		}, {
			Output: filepath.Join(outDir, "missing-segment.mp4"),
		}},
	}

	got, err := NewRenderVariantUploadTargets(NewRenderVariantUploadTargetsOptions{
		JobID:      id,
		Variant:    editor.PresetViral60Clean,
		OutDir:     outDir,
		PublishDir: publishDir,
		ResultPath: resultPath,
		Result:     result,
	})
	if err != nil {
		t.Fatalf("NewRenderVariantUploadTargets error = %v", err)
	}

	prefix := "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean"
	want := []RenderVariantUploadTarget{{
		Key:      prefix + "/render-result.json",
		Path:     resultPath,
		Label:    "render result",
		Required: true,
	}, {
		Key:   prefix + "/edit-document.json",
		Path:  filepath.Join(outDir, "edit-document.json"),
		Label: "edit document",
	}, {
		Key:   prefix + "/edit-manifest.json",
		Path:  filepath.Join(outDir, "edit-manifest.json"),
		Label: "edit manifest",
	}, {
		Key:   prefix + "/pack-manifest.json",
		Path:  filepath.Join(publishDir, "pack-manifest.json"),
		Label: "pack manifest",
	}, {
		Key:   prefix + "/publish-summary.md",
		Path:  result.SummaryPath,
		Label: "publish summary",
	}, {
		Key:   prefix + "/index.html",
		Path:  result.GalleryPath,
		Label: "gallery",
	}, {
		Key:   prefix + "/videos/seg-001.mp4",
		Path:  result.Shorts[0].PublishPath,
		Label: "render video seg-001",
	}, {
		Key:   prefix + "/covers/seg-001.jpg",
		Path:  result.Shorts[0].CoverPath,
		Label: "render cover seg-001",
	}, {
		Key:   prefix + "/captions/seg-001.caption.txt",
		Path:  result.Shorts[0].CaptionPath,
		Label: "render caption seg-001",
	}, {
		Key:   prefix + "/logs/seg-001-render.log",
		Path:  result.Shorts[0].RenderLogPath,
		Label: "render log seg-001",
	}, {
		Key:   prefix + "/videos/seg-002.mp4",
		Path:  result.Shorts[1].Output,
		Label: "render video seg-002",
	}}
	if len(got) != len(want) {
		t.Fatalf("targets len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("target[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestNewRenderVariantReadyArtifactsReturnsSkipSentinels(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	got, err := NewRenderVariantReadyArtifacts(id, editor.PresetViral60Clean)
	if err != nil {
		t.Fatalf("NewRenderVariantReadyArtifacts error = %v", err)
	}

	want := RenderVariantReadyArtifacts{
		ResultKey: "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/render-result.json",
		RequiredKeys: []string{
			"jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/pack-manifest.json",
			"jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/index.html",
		},
	}
	if got.ResultKey != want.ResultKey {
		t.Fatalf("result key = %q, want %q", got.ResultKey, want.ResultKey)
	}
	if len(got.RequiredKeys) != len(want.RequiredKeys) {
		t.Fatalf("required keys len = %d, want %d: %#v", len(got.RequiredKeys), len(want.RequiredKeys), got.RequiredKeys)
	}
	for i := range want.RequiredKeys {
		if got.RequiredKeys[i] != want.RequiredKeys[i] {
			t.Fatalf("required key[%d] = %q, want %q", i, got.RequiredKeys[i], want.RequiredKeys[i])
		}
	}
}
