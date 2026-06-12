package renderplan

import (
	"testing"
	"time"

	"github.com/google/uuid"
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
