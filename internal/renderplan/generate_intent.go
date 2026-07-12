package renderplan

// GenerateIntent is the normalized choice behind a one-click "generate a
// short" request: preset variant, optional music, and edit treatment. Each
// accepted record task carries an immutable copy so concurrent captures cannot
// change one another's render; a job-scoped artifact mirrors the latest
// accepted choice for workbench display.
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
