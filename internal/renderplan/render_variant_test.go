package renderplan

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/editor"
)

func TestNewRenderVariantStatePreservesCreatedAt(t *testing.T) {
	created := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	updated := created.Add(time.Minute)
	previous := NewRenderVariantState(NewRenderVariantStateOptions{
		JobID:   uuid.New(),
		Variant: "viral-60-clean",
		Status:  RenderVariantStatusQueued,
		Now:     created,
	})

	got := NewRenderVariantState(NewRenderVariantStateOptions{
		JobID:    previous.JobID,
		Variant:  previous.Variant,
		Status:   RenderVariantStatusRendering,
		Now:      updated,
		Previous: &previous,
	})

	if !got.CreatedAt.Equal(created) {
		t.Fatalf("CreatedAt = %s, want %s", got.CreatedAt, created)
	}
	if !got.UpdatedAt.Equal(updated) {
		t.Fatalf("UpdatedAt = %s, want %s", got.UpdatedAt, updated)
	}
}

func TestNewRenderVariantStateForLoadoutDerivesArtifactKeys(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	loadout, err := LoadoutForVariant(editor.PresetViral60Clean)
	if err != nil {
		t.Fatal(err)
	}

	got, err := NewRenderVariantStateForLoadout(NewRenderVariantStateForLoadoutOptions{
		JobID:    id,
		Loadout:  loadout,
		Status:   RenderVariantStatusRendering,
		Warnings: []string{"minor"},
		Now:      now,
	})
	if err != nil {
		t.Fatalf("NewRenderVariantStateForLoadout error = %v", err)
	}

	if got.JobID != id || got.Variant != editor.PresetViral60Clean || got.Preset != editor.PresetViral60Clean {
		t.Fatalf("identity fields = %#v", got)
	}
	if got.Status != RenderVariantStatusRendering {
		t.Fatalf("status = %q, want %q", got.Status, RenderVariantStatusRendering)
	}
	wantPrefix := "jobs/11111111-1111-1111-1111-111111111111/renders/viral-60-clean"
	if got.ArtifactPrefix != wantPrefix {
		t.Fatalf("artifact prefix = %q, want %q", got.ArtifactPrefix, wantPrefix)
	}
	cases := map[string]string{
		got.RenderResultKey:   wantPrefix + "/render-result.json",
		got.EditDocumentKey:   wantPrefix + "/edit-document.json",
		got.EditManifestKey:   wantPrefix + "/edit-manifest.json",
		got.PackManifestKey:   wantPrefix + "/pack-manifest.json",
		got.GalleryKey:        wantPrefix + "/index.html",
		got.PublishSummaryKey: wantPrefix + "/publish-summary.md",
	}
	for gotKey, want := range cases {
		if gotKey != want {
			t.Fatalf("key = %q, want %q", gotKey, want)
		}
	}
	if len(got.Warnings) != 1 || got.Warnings[0] != "minor" {
		t.Fatalf("warnings = %#v", got.Warnings)
	}
}
