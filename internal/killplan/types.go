// Package killplan defines the JSON document emitted by zv-parser:
// the structured "kill plan" that downstream workers (recorder, composer)
// consume to drive the rest of the pipeline.
package killplan

import (
	"encoding/json"
	"fmt"
	"time"
)

// SchemaVersion identifies the kill plan document format. Downstream
// consumers should reject versions they do not understand.
const SchemaVersion = "1.0"

// Plan is the top-level kill plan document.
type Plan struct {
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	Demo          Demo      `json:"demo"`
	Target        Target    `json:"target"`
	Rules         any       `json:"rules,omitempty"`
	Segments      []Segment `json:"segments"`
	Stats         Stats     `json:"stats"`
}

// Demo holds metadata about the source demo file.
type Demo struct {
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	Map           string `json:"map"`
	Tickrate      int    `json:"tickrate"`
	DurationTicks int    `json:"duration_ticks"`
}

// Target identifies the player whose kills the plan is built around.
type Target struct {
	SteamID64   string `json:"steamid64"`
	NameInDemo  string `json:"name_in_demo"`
	TeamAtStart string `json:"team_at_start"`
}

// Segment is one contiguous recording range covering one or more kills.
type Segment struct {
	ID        string `json:"id"`
	Round     int    `json:"round"`
	TickStart int    `json:"tick_start"`
	TickEnd   int    `json:"tick_end"`
	Kills     []Kill `json:"kills"`
}

// Kill captures the metadata downstream stages need to choose effects
// (zoom, flash, slow-mo) and frame the cinematography.
type Kill struct {
	Tick      int        `json:"tick"`
	Weapon    string     `json:"weapon"`
	Headshot  bool       `json:"headshot"`
	Wallbang  bool       `json:"wallbang"`
	Victim    Player     `json:"victim"`
	KillerPos [3]float64 `json:"killer_pos"`
	VictimPos [3]float64 `json:"victim_pos"`
}

// Player is the victim's identity at the moment of the kill.
type Player struct {
	SteamID64  string `json:"steamid64"`
	NameInDemo string `json:"name_in_demo"`
	TeamAtKill string `json:"team_at_kill"`
}

// Stats summarises what the parser observed; useful to display in the UI.
type Stats struct {
	TotalKillsTarget     int     `json:"total_kills_target"`
	KillsAfterFilters    int     `json:"kills_after_filters"`
	SegmentsCreated      int     `json:"segments_created"`
	DurationSecondsTotal float64 `json:"duration_seconds_total"`
}

// MarshalJSON guarantees that every emitted plan carries the current
// SchemaVersion, even when callers constructed the Plan as a zero value.
func (p Plan) MarshalJSON() ([]byte, error) {
	type alias Plan
	p.SchemaVersion = SchemaVersion
	return json.Marshal(alias(p))
}

// NewPlan returns a Plan pre-populated with the current schema version,
// a UTC timestamp, and an empty segments slice.
func NewPlan() Plan {
	return Plan{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC(),
		Segments:      []Segment{},
	}
}

// FormatSegmentID renders a 1-based segment number as "seg-001", zero-padded
// to three digits (or as many as fit).
func FormatSegmentID(n int) string {
	return fmt.Sprintf("seg-%03d", n)
}
