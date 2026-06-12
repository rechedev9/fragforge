package renderplan

import (
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
)

// RenderVariantUploadStatus records whether the upload-ready render pack has
// already been published outside FragForge.
type RenderVariantUploadStatus struct {
	SchemaVersion string    `json:"schema_version"`
	JobID         uuid.UUID `json:"job_id"`
	Variant       string    `json:"variant"`
	Uploaded      bool      `json:"uploaded"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// RenderVariantUploadStatusKey returns the durable storage key for the
// render-variant upload marker.
func RenderVariantUploadStatusKey(jobID uuid.UUID, variant string) (string, error) {
	return artifacts.RenderVariantUploadStatusKey(jobID, variant)
}

// NewRenderVariantUploadStatus returns a fresh upload marker document.
func NewRenderVariantUploadStatus(jobID uuid.UUID, variant string, uploaded bool) RenderVariantUploadStatus {
	return RenderVariantUploadStatus{
		SchemaVersion: "1.0",
		JobID:         jobID,
		Variant:       variant,
		Uploaded:      uploaded,
		UpdatedAt:     time.Now().UTC(),
	}
}
