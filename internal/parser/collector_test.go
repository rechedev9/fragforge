package parser

import (
	"testing"

	"github.com/reche/zackvideo/internal/killplan"
)

const targetID = "76561198000000000"

func meta() PlanMeta {
	return PlanMeta{
		DemoPath:      "/tmp/demo.dem",
		SHA256:        "abc123",
		Map:           "de_inferno",
		Tickrate:      testTickrate,
		DurationTicks: 285000,
	}
}

func TestRecordKillAcceptedWeaponAdded(t *testing.T) {
	c := NewCollector(targetID, defaultTestRules())
	c.RecordTargetIdentity("MARTINEZSA", "CT")
	c.RecordKill(RawKill{Tick: 1000, Round: 3, Weapon: "awp"})

	if c.TotalKillsTarget() != 1 {
		t.Errorf("TotalKillsTarget = %d, want 1", c.TotalKillsTarget())
	}
	if c.KillsAfterFilters() != 1 {
		t.Errorf("KillsAfterFilters = %d, want 1", c.KillsAfterFilters())
	}
}

func TestRecordKillRejectedWeaponNotAdded(t *testing.T) {
	c := NewCollector(targetID, defaultTestRules())
	c.RecordKill(RawKill{Tick: 1000, Round: 3, Weapon: "knife"})

	if c.TotalKillsTarget() != 1 {
		t.Errorf("TotalKillsTarget = %d, want 1 (counted before filters)", c.TotalKillsTarget())
	}
	if c.KillsAfterFilters() != 0 {
		t.Errorf("KillsAfterFilters = %d, want 0 (filtered out)", c.KillsAfterFilters())
	}
}

func TestRecordKillHeadshotOnlyDropsNonHeadshots(t *testing.T) {
	r := defaultTestRules()
	r.IncludeHeadshotOnly = true
	c := NewCollector(targetID, r)
	c.RecordKill(RawKill{Tick: 1000, Round: 3, Weapon: "awp", Headshot: false})
	c.RecordKill(RawKill{Tick: 2000, Round: 3, Weapon: "awp", Headshot: true})

	if c.KillsAfterFilters() != 1 {
		t.Errorf("KillsAfterFilters = %d, want 1 (only headshot)", c.KillsAfterFilters())
	}
}

func TestRecordKillRoundFilter(t *testing.T) {
	r := defaultTestRules()
	r.MinRound = 5
	r.MaxRound = 10
	c := NewCollector(targetID, r)
	c.RecordKill(RawKill{Tick: 1000, Round: 4, Weapon: "awp"})  // below
	c.RecordKill(RawKill{Tick: 2000, Round: 5, Weapon: "awp"})  // ok
	c.RecordKill(RawKill{Tick: 3000, Round: 10, Weapon: "awp"}) // ok
	c.RecordKill(RawKill{Tick: 4000, Round: 11, Weapon: "awp"}) // above

	if c.KillsAfterFilters() != 2 {
		t.Errorf("KillsAfterFilters = %d, want 2", c.KillsAfterFilters())
	}
}

func TestBuildPlanFailsWhenTargetNeverSeen(t *testing.T) {
	c := NewCollector(targetID, defaultTestRules())
	// no RecordTargetIdentity, no kills

	_, err := c.Build(meta())
	if err == nil {
		t.Fatal("Build() error = nil, want error about target not seen")
	}
}

func TestBuildPlanWithNoKillsReturnsEmptySegments(t *testing.T) {
	c := NewCollector(targetID, defaultTestRules())
	c.RecordTargetIdentity("MARTINEZSA", "CT")

	plan, err := c.Build(meta())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.SchemaVersion != killplan.SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", plan.SchemaVersion, killplan.SchemaVersion)
	}
	if len(plan.Segments) != 0 {
		t.Errorf("Segments length = %d, want 0", len(plan.Segments))
	}
	if plan.Target.SteamID64 != targetID {
		t.Errorf("Target.SteamID64 = %q, want %q", plan.Target.SteamID64, targetID)
	}
	if plan.Demo.Map != "de_inferno" {
		t.Errorf("Demo.Map = %q, want de_inferno", plan.Demo.Map)
	}
	if plan.Stats.KillsAfterFilters != 0 {
		t.Errorf("Stats.KillsAfterFilters = %d, want 0", plan.Stats.KillsAfterFilters)
	}
}

func TestBuildPlanAssemblesSegments(t *testing.T) {
	c := NewCollector(targetID, defaultTestRules())
	c.RecordTargetIdentity("MARTINEZSA", "CT")
	c.RecordKill(RawKill{Tick: 10000, Round: 5, Weapon: "awp"})
	c.RecordKill(RawKill{Tick: 10000 + 2*testTickrate, Round: 5, Weapon: "awp"})

	plan, err := c.Build(meta())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(plan.Segments) != 1 {
		t.Fatalf("Segments length = %d, want 1", len(plan.Segments))
	}
	if len(plan.Segments[0].Kills) != 2 {
		t.Errorf("Kills in segment = %d, want 2", len(plan.Segments[0].Kills))
	}
	if plan.Stats.SegmentsCreated != 1 {
		t.Errorf("Stats.SegmentsCreated = %d, want 1", plan.Stats.SegmentsCreated)
	}
	if plan.Stats.KillsAfterFilters != 2 {
		t.Errorf("Stats.KillsAfterFilters = %d, want 2", plan.Stats.KillsAfterFilters)
	}
	if plan.Stats.DurationSecondsTotal <= 0 {
		t.Errorf("Stats.DurationSecondsTotal = %v, want > 0", plan.Stats.DurationSecondsTotal)
	}
}

func TestBuildPlanRoundEndClipping(t *testing.T) {
	c := NewCollector(targetID, defaultTestRules())
	c.RecordTargetIdentity("MARTINEZSA", "CT")
	c.RecordKill(RawKill{Tick: 10000, Round: 5, Weapon: "awp"})
	c.RecordRoundEnd(RoundEnd{Round: 5, Tick: 10100})

	plan, err := c.Build(meta())
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if plan.Segments[0].TickEnd != 10100 {
		t.Errorf("TickEnd = %d, want 10100 (clipped)", plan.Segments[0].TickEnd)
	}
}
