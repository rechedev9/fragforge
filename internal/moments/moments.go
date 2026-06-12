// Package moments derives reviewable, scored clip candidates from kill plans.
package moments

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/killplan"
)

const SchemaVersion = "1.0"

func ArtifactKey(jobID uuid.UUID) string {
	return artifacts.MomentsKey(jobID)
}

type Document struct {
	SchemaVersion string    `json:"schema_version"`
	JobID         uuid.UUID `json:"job_id"`
	GeneratedAt   time.Time `json:"generated_at"`
	Moments       []Moment  `json:"moments"`
}

type Moment struct {
	ID              string         `json:"id"`
	Source          string         `json:"source"`
	SegmentID       string         `json:"segment_id"`
	Player          string         `json:"player,omitempty"`
	Map             string         `json:"map,omitempty"`
	Round           int            `json:"round"`
	TickStart       int            `json:"tick_start"`
	TickEnd         int            `json:"tick_end"`
	TimeStart       float64        `json:"time_start_seconds,omitempty"`
	TimeEnd         float64        `json:"time_end_seconds,omitempty"`
	DurationSeconds float64        `json:"duration_seconds"`
	Score           float64        `json:"score"`
	ReasonCodes     []string       `json:"reason_codes"`
	Events          Events         `json:"events"`
	Weapons         []string       `json:"weapons,omitempty"`
	Victims         []string       `json:"victims,omitempty"`
	Utility         []UtilityEvent `json:"utility,omitempty"`
	Warnings        []string       `json:"warnings,omitempty"`
	DefaultVariant  string         `json:"default_variant"`
}

type Events struct {
	Kills          int      `json:"kills"`
	Headshots      int      `json:"headshots,omitempty"`
	Wallbangs      int      `json:"wallbangs,omitempty"`
	Weapons        []string `json:"weapons,omitempty"`
	Utility        int      `json:"utility,omitempty"`
	KnownLineups   int      `json:"known_lineups,omitempty"`
	UnknownLineups int      `json:"unknown_lineups,omitempty"`
}

type UtilityEvent struct {
	ID          string  `json:"id,omitempty"`
	Type        string  `json:"type,omitempty"`
	Destination string  `json:"destination,omitempty"`
	FromArea    string  `json:"from_area,omitempty"`
	Side        string  `json:"side,omitempty"`
	Action      string  `json:"action,omitempty"`
	ThrowPlace  string  `json:"throw_place,omitempty"`
	ThrowTick   int     `json:"throw_tick,omitempty"`
	PopTick     int     `json:"pop_tick,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	KnownLineup bool    `json:"known_lineup"`
}

func Build(jobID uuid.UUID, plan killplan.Plan) Document {
	doc := Document{
		SchemaVersion: SchemaVersion,
		JobID:         jobID,
		GeneratedAt:   time.Now().UTC(),
		Moments:       make([]Moment, 0, len(plan.Segments)),
	}
	for i, segment := range plan.Segments {
		doc.Moments = append(doc.Moments, buildMoment(i+1, segment, plan))
	}
	return doc
}

func buildMoment(index int, segment killplan.Segment, plan killplan.Plan) Moment {
	tickrate := plan.Demo.Tickrate
	events := segmentEvents(segment)
	reasons := reasonCodes(segment, events)
	warnings := warnings(segment, tickrate)
	duration := durationSeconds(segment, tickrate)
	timeStart, timeEnd := timeRangeSeconds(segment, tickrate)
	return Moment{
		ID:              fmt.Sprintf("mom-%03d", index),
		Source:          "killplan",
		SegmentID:       segment.ID,
		Player:          plan.Target.NameInDemo,
		Map:             plan.Demo.Map,
		Round:           segment.Round,
		TickStart:       segment.TickStart,
		TickEnd:         segment.TickEnd,
		TimeStart:       timeStart,
		TimeEnd:         timeEnd,
		DurationSeconds: duration,
		Score:           score(events, duration),
		ReasonCodes:     reasons,
		Events:          events,
		Weapons:         append([]string(nil), events.Weapons...),
		Victims:         victims(segment),
		Utility:         utilityEvents(segment),
		Warnings:        warnings,
		DefaultVariant:  editor.PresetViral60Clean,
	}
}

func segmentEvents(segment killplan.Segment) Events {
	weaponSet := map[string]bool{}
	events := Events{
		Kills:   len(segment.Kills),
		Utility: len(segment.Utility),
	}
	for _, kill := range segment.Kills {
		weapon := strings.TrimSpace(strings.ToLower(kill.Weapon))
		if weapon != "" {
			weaponSet[weapon] = true
		}
		if kill.Headshot {
			events.Headshots++
		}
		if kill.Wallbang {
			events.Wallbangs++
		}
	}
	for _, u := range segment.Utility {
		if u.LineupMatch != nil && u.LineupMatch.ID != "" {
			events.KnownLineups++
		} else {
			events.UnknownLineups++
		}
	}
	for weapon := range weaponSet {
		events.Weapons = append(events.Weapons, weapon)
	}
	sort.Strings(events.Weapons)
	return events
}

func victims(segment killplan.Segment) []string {
	set := map[string]bool{}
	for _, kill := range segment.Kills {
		name := strings.TrimSpace(kill.Victim.NameInDemo)
		if name != "" {
			set[name] = true
		}
	}
	out := make([]string, 0, len(set))
	for victim := range set {
		out = append(out, victim)
	}
	sort.Strings(out)
	return out
}

func utilityEvents(segment killplan.Segment) []UtilityEvent {
	out := make([]UtilityEvent, 0, len(segment.Utility))
	for _, u := range segment.Utility {
		event := UtilityEvent{
			ID:         u.ID,
			Type:       u.Type,
			Action:     u.ThrowAction,
			ThrowPlace: u.ThrowPlace,
			ThrowTick:  u.ThrowTick,
			PopTick:    u.PopTick,
		}
		if u.LineupMatch != nil {
			event.Destination = u.LineupMatch.Destination
			event.FromArea = u.LineupMatch.FromArea
			event.Side = u.LineupMatch.Side
			event.Confidence = u.LineupMatch.Confidence
			event.KnownLineup = u.LineupMatch.ID != ""
		}
		out = append(out, event)
	}
	return out
}

func reasonCodes(segment killplan.Segment, events Events) []string {
	reasons := map[string]bool{}
	if events.Kills >= 2 {
		reasons["multi_kill"] = true
	}
	if events.Headshots > 0 {
		reasons["headshot"] = true
	}
	if events.Wallbangs > 0 {
		reasons["wallbang"] = true
	}
	for _, weapon := range events.Weapons {
		switch weapon {
		case "awp", "weapon_awp":
			reasons["awp"] = true
		case "ak47", "weapon_ak47", "m4a1", "m4a1_silencer", "weapon_m4a1", "weapon_m4a1_silencer":
			reasons["rifle"] = true
		case "deagle", "weapon_deagle", "usp_silencer", "glock", "weapon_usp_silencer", "weapon_glock":
			reasons["pistol"] = true
		}
	}
	if len(segment.Utility) > 0 {
		reasons["utility_lineup"] = true
	}
	if events.KnownLineups > 0 {
		reasons["known_lineup"] = true
	}
	if events.UnknownLineups > 0 && len(segment.Utility) > 0 {
		reasons["unmatched_lineup"] = true
	}
	out := make([]string, 0, len(reasons))
	for reason := range reasons {
		out = append(out, reason)
	}
	sort.Strings(out)
	return out
}

func warnings(segment killplan.Segment, tickrate int) []string {
	var out []string
	if segment.ID == "" {
		out = append(out, "missing_segment_id")
	}
	if tickrate <= 0 {
		out = append(out, "missing_tickrate")
	}
	if segment.TickEnd <= segment.TickStart {
		out = append(out, "invalid_tick_range")
	}
	if len(segment.Kills) == 0 && len(segment.Utility) == 0 {
		out = append(out, "empty_segment")
	}
	return out
}

func durationSeconds(segment killplan.Segment, tickrate int) float64 {
	if tickrate <= 0 || segment.TickEnd <= segment.TickStart {
		return 0
	}
	return math.Round((float64(segment.TickEnd-segment.TickStart)/float64(tickrate))*1000) / 1000
}

func timeRangeSeconds(segment killplan.Segment, tickrate int) (float64, float64) {
	if tickrate <= 0 {
		return 0, 0
	}
	start := math.Round((float64(segment.TickStart)/float64(tickrate))*1000) / 1000
	end := math.Round((float64(segment.TickEnd)/float64(tickrate))*1000) / 1000
	return start, end
}

func score(events Events, duration float64) float64 {
	value := 0.2
	value += float64(events.Kills) * 0.22
	value += float64(events.Headshots) * 0.06
	value += float64(events.Wallbangs) * 0.08
	value += float64(events.KnownLineups) * 0.18
	value += float64(events.UnknownLineups) * 0.08
	if duration > 0 && duration <= 18 {
		value += 0.08
	}
	if value > 1 {
		value = 1
	}
	return math.Round(value*100) / 100
}
