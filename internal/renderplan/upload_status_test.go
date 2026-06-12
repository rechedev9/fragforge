package renderplan

import (
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/editor"
)

func TestRenderVariantUploadStatusKeyDerivesUploadedKey(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	got, err := RenderVariantUploadStatusKey(id, editor.PresetViral60Clean)
	if err != nil {
		t.Fatalf("RenderVariantUploadStatusKey error = %v", err)
	}

	want := "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean/uploaded.json"
	if got != want {
		t.Fatalf("upload status key = %q, want %q", got, want)
	}
}

func TestNewRenderVariantUploadStatus(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	got := NewRenderVariantUploadStatus(id, editor.PresetViral60Clean, true)

	if got.SchemaVersion != "1.0" || got.JobID != id || got.Variant != editor.PresetViral60Clean || !got.Uploaded {
		t.Fatalf("upload status = %#v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt is zero")
	}
}
