package moments

import (
	"testing"

	"github.com/google/uuid"

	"github.com/reche/zackvideo/internal/editor"
	"github.com/reche/zackvideo/internal/killplan"
)

func TestBuildDerivesScoredKillMoments(t *testing.T) {
	plan := killplan.NewPlan()
	plan.Demo.Tickrate = 64
	plan.Demo.Map = "de_ancient"
	plan.Target.NameInDemo = "MartinezSa"
	plan.Segments = []killplan.Segment{{
		ID:        "seg-001",
		Round:     7,
		TickStart: 640,
		TickEnd:   1280,
		Kills: []killplan.Kill{
			{Tick: 700, Weapon: "weapon_awp", Headshot: true, Victim: killplan.Player{NameInDemo: "alex"}},
			{Tick: 900, Weapon: "weapon_awp", Victim: killplan.Player{NameInDemo: "b1t"}},
		},
	}}

	doc := Build(uuid.MustParse("11111111-1111-1111-1111-111111111111"), plan)

	if doc.SchemaVersion != SchemaVersion {
		t.Fatalf("schema = %q, want %q", doc.SchemaVersion, SchemaVersion)
	}
	if len(doc.Moments) != 1 {
		t.Fatalf("moments len = %d, want 1", len(doc.Moments))
	}
	got := doc.Moments[0]
	if got.ID != "mom-001" || got.SegmentID != "seg-001" || got.Round != 7 {
		t.Fatalf("moment identity = %#v", got)
	}
	if got.DurationSeconds != 10 {
		t.Fatalf("duration = %v, want 10", got.DurationSeconds)
	}
	if got.Player != "MartinezSa" || got.Map != "de_ancient" {
		t.Fatalf("player/map = %q/%q", got.Player, got.Map)
	}
	if got.TimeStart != 10 || got.TimeEnd != 20 {
		t.Fatalf("time range = %v/%v, want 10/20", got.TimeStart, got.TimeEnd)
	}
	if len(got.Victims) != 2 || got.Victims[0] != "alex" || got.Victims[1] != "b1t" {
		t.Fatalf("victims = %#v", got.Victims)
	}
	if got.Events.Kills != 2 || got.Events.Headshots != 1 {
		t.Fatalf("events = %#v", got.Events)
	}
	for _, want := range []string{"awp", "headshot", "multi_kill"} {
		if !hasReason(got.ReasonCodes, want) {
			t.Fatalf("reasons = %v, missing %q", got.ReasonCodes, want)
		}
	}
	if got.Score <= 0.5 {
		t.Fatalf("score = %v, want > 0.5", got.Score)
	}
	if got.DefaultVariant != editor.PresetViral60 {
		t.Fatalf("default variant = %q, want %q", got.DefaultVariant, editor.PresetViral60)
	}
}

func TestBuildDerivesUtilityLineupReasons(t *testing.T) {
	plan := killplan.NewPlan()
	plan.Demo.Tickrate = 64
	plan.Segments = []killplan.Segment{{
		ID:        "seg-001",
		Round:     3,
		TickStart: 100,
		TickEnd:   200,
		Utility: []killplan.UtilityThrow{{
			ID:          "smoke-001",
			Type:        "smokegrenade",
			ThrowAction: "jumpthrow",
			ThrowPlace:  "t-spawn",
			LineupMatch: &killplan.LineupMatch{
				ID:          "ancient-ct-smoke",
				Destination: "ct",
				FromArea:    "t-spawn",
				Side:        "T",
				Confidence:  0.97,
			},
		}},
	}}

	got := Build(uuid.New(), plan).Moments[0]

	if got.Events.Utility != 1 || got.Events.KnownLineups != 1 {
		t.Fatalf("events = %#v", got.Events)
	}
	if len(got.Utility) != 1 || got.Utility[0].Destination != "ct" || !got.Utility[0].KnownLineup {
		t.Fatalf("utility = %#v", got.Utility)
	}
	for _, want := range []string{"known_lineup", "utility_lineup"} {
		if !hasReason(got.ReasonCodes, want) {
			t.Fatalf("reasons = %v, missing %q", got.ReasonCodes, want)
		}
	}
}

func TestBuildWarnsOnInvalidSegments(t *testing.T) {
	plan := killplan.NewPlan()
	plan.Segments = []killplan.Segment{{}}

	got := Build(uuid.New(), plan).Moments[0]

	for _, want := range []string{"missing_segment_id", "missing_tickrate", "invalid_tick_range", "empty_segment"} {
		if !hasReason(got.Warnings, want) {
			t.Fatalf("warnings = %v, missing %q", got.Warnings, want)
		}
	}
}

func hasReason(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
