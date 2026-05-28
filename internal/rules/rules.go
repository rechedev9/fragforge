// Package rules defines and loads the segmentation rules used by zv-parser
// to decide which kills are relevant and how to group them into clip segments.
package rules

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Rules controls how the parser filters kills and groups them into segments.
// Zero values are not meaningful; use Default or Load to construct a Rules.
type Rules struct {
	Weapons             []string `json:"weapons"`
	MinKillsInWindow    int      `json:"min_kills_in_window"`
	WindowSeconds       int      `json:"window_seconds"`
	PreRollSeconds      int      `json:"pre_roll_seconds"`
	PostRollSeconds     int      `json:"post_roll_seconds"`
	IncludeHeadshotOnly bool     `json:"include_headshot_only"`
	ExcludeTeamKills    bool     `json:"exclude_team_kills"`
	MinRound            int      `json:"min_round"`
	// MaxRound is the inclusive last round to include. 0 means no upper bound.
	MaxRound int `json:"max_round"`
}

// Default returns the canonical default rules.
func Default() Rules {
	return Rules{
		Weapons: []string{
			"awp", "deagle", "ak47", "m4a1",
			"m4a1_silencer", "usp_silencer", "glock", "hkp2000",
		},
		MinKillsInWindow:    1,
		WindowSeconds:       8,
		PreRollSeconds:      3,
		PostRollSeconds:     5,
		IncludeHeadshotOnly: false,
		ExcludeTeamKills:    true,
		MinRound:            1,
		MaxRound:            0,
	}
}

// Load reads a JSON rules document from r and returns Rules with any
// unspecified fields filled in from Default. An empty document yields the
// defaults. The returned Rules is validated; invalid combinations return an error.
func Load(r io.Reader) (Rules, error) {
	// Use a parallel struct of pointers so we can detect which fields were
	// actually present in the JSON and only override the corresponding defaults.
	var raw struct {
		Weapons             *[]string `json:"weapons"`
		MinKillsInWindow    *int      `json:"min_kills_in_window"`
		WindowSeconds       *int      `json:"window_seconds"`
		PreRollSeconds      *int      `json:"pre_roll_seconds"`
		PostRollSeconds     *int      `json:"post_roll_seconds"`
		IncludeHeadshotOnly *bool     `json:"include_headshot_only"`
		ExcludeTeamKills    *bool     `json:"exclude_team_kills"`
		MinRound            *int      `json:"min_round"`
		MaxRound            *int      `json:"max_round"`
	}
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		// An empty (or whitespace-only) document yields the defaults.
		if errors.Is(err, io.EOF) {
			return Default(), nil
		}
		return Rules{}, fmt.Errorf("decoding rules: %w", err)
	}
	// Reject a second JSON value or trailing content so a concatenated or
	// truncated file is not silently parsed as just its first document. Use a
	// clean-EOF check rather than dec.More(), which returns false for a stray
	// trailing '}' or ']' and would let corrupted input through.
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Rules{}, errors.New("rules: unexpected content after rules document")
	}

	rules := Default()
	if raw.Weapons != nil {
		rules.Weapons = *raw.Weapons
	}
	if raw.MinKillsInWindow != nil {
		rules.MinKillsInWindow = *raw.MinKillsInWindow
	}
	if raw.WindowSeconds != nil {
		rules.WindowSeconds = *raw.WindowSeconds
	}
	if raw.PreRollSeconds != nil {
		rules.PreRollSeconds = *raw.PreRollSeconds
	}
	if raw.PostRollSeconds != nil {
		rules.PostRollSeconds = *raw.PostRollSeconds
	}
	if raw.IncludeHeadshotOnly != nil {
		rules.IncludeHeadshotOnly = *raw.IncludeHeadshotOnly
	}
	if raw.ExcludeTeamKills != nil {
		rules.ExcludeTeamKills = *raw.ExcludeTeamKills
	}
	if raw.MinRound != nil {
		rules.MinRound = *raw.MinRound
	}
	if raw.MaxRound != nil {
		rules.MaxRound = *raw.MaxRound
	}

	if err := rules.Validate(); err != nil {
		return Rules{}, err
	}
	return rules, nil
}

// Validate checks rules for self-consistency.
func (r Rules) Validate() error {
	if len(r.Weapons) == 0 {
		return errors.New("rules: weapons must contain at least one entry")
	}
	if r.WindowSeconds < 0 {
		return fmt.Errorf("rules: window_seconds must be >= 0, got %d", r.WindowSeconds)
	}
	if r.PreRollSeconds < 0 {
		return fmt.Errorf("rules: pre_roll_seconds must be >= 0, got %d", r.PreRollSeconds)
	}
	if r.PostRollSeconds < 0 {
		return fmt.Errorf("rules: post_roll_seconds must be >= 0, got %d", r.PostRollSeconds)
	}
	if r.MinKillsInWindow < 1 {
		return fmt.Errorf("rules: min_kills_in_window must be >= 1, got %d", r.MinKillsInWindow)
	}
	if r.MinRound < 1 {
		return fmt.Errorf("rules: min_round must be >= 1, got %d", r.MinRound)
	}
	if r.MaxRound != 0 && r.MaxRound < r.MinRound {
		return fmt.Errorf("rules: max_round (%d) must be 0 or >= min_round (%d)", r.MaxRound, r.MinRound)
	}
	return nil
}

// AllowsWeapon reports whether weapon is in the allowed list.
func (r Rules) AllowsWeapon(weapon string) bool {
	for _, w := range r.Weapons {
		if w == weapon {
			return true
		}
	}
	return false
}

// AllowsRound reports whether round is within the configured range.
// A MaxRound of 0 means no upper bound.
func (r Rules) AllowsRound(round int) bool {
	if round < r.MinRound {
		return false
	}
	if r.MaxRound != 0 && round > r.MaxRound {
		return false
	}
	return true
}
