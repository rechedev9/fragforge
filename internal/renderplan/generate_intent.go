package renderplan

import (
	"time"

	"github.com/google/uuid"
)

// GenerateIntent is the normalized choice behind a one-click "generate a
// short" request: preset variant, optional music, and edit treatment. Each
// accepted record task carries an immutable copy, while the job-scoped artifact
// fences overlapping capture/render work and mirrors the current choice for
// workbench display. ActiveRunID is non-zero only while that accepted capture
// still owns the guided-flow handoff to a render task.
type GenerateIntent struct {
	Variant     string      `json:"variant"`
	MusicKey    string      `json:"music_key,omitempty"`
	Edit        EditRequest `json:"edit"`
	ActiveRunID uuid.UUID   `json:"active_run_id,omitzero"`
	AcceptedAt  time.Time   `json:"accepted_at,omitzero"`
}

// Normalize fills unset edit fields with their defaults and returns the result.
func (g GenerateIntent) Normalize() GenerateIntent {
	g.Edit = NormalizeEditRequest(g.Edit)
	return g
}

// Validate reports whether the intent names a known preset variant and carries
// a valid edit request. The music key is validated where the render task is
// built (it shares the render-variant token rules), so it is not checked here.
func (g GenerateIntent) Validate() error {
	if _, err := LoadoutForVariant(g.Variant); err != nil {
		return err
	}
	return g.Edit.Validate()
}
