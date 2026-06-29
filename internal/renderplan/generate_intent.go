package renderplan

// GenerateIntent is the durable, job-scoped capture of a one-click "generate a
// short" request: the preset variant the user picked (which selects both the
// recording HUD and the render variant), an optional music track, and the edit
// treatment. The record worker reads it after a successful capture to enqueue
// the matching render without a second user action, so the choice survives the
// orchestrator restarting mid-capture.
type GenerateIntent struct {
	Variant  string      `json:"variant"`
	MusicKey string      `json:"music_key,omitempty"`
	Edit     EditRequest `json:"edit"`
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
