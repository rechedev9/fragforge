package parser

import (
	"testing"

	"github.com/reche/zackvideo/internal/killplan"
)

func mkSmoke(throwTick, popTick, round int) RawUtilityThrow {
	return RawUtilityThrow{
		Type:      SmokeGrenadeType,
		Round:     round,
		ThrowTick: throwTick,
		PopTick:   popTick,
		Thrower:   killplan.Player{SteamID64: targetID, NameInDemo: "MARTINEZSA", TeamAtKill: "T"},
		ThrowPos:  [3]float64{1, 2, 3},
	}
}

func TestSegmentSmokesCreatesOneSegmentPerSmoke(t *testing.T) {
	smokes := []RawUtilityThrow{
		mkSmoke(10000, 10200, 5),
		mkSmoke(20000, 20200, 6),
	}
	got := SegmentSmokes(smokes, nil, defaultTestRules(), testTickrate)
	if len(got) != 2 {
		t.Fatalf("segments len = %d, want 2", len(got))
	}
	if got[0].ID != "seg-001" || got[1].ID != "seg-002" {
		t.Fatalf("ids = %q, %q", got[0].ID, got[1].ID)
	}
	if len(got[0].Utility) != 1 || len(got[0].Kills) != 0 {
		t.Fatalf("segment utility/kills = %#v", got[0])
	}
	if got[0].TickStart != 10000-3*testTickrate {
		t.Fatalf("TickStart = %d", got[0].TickStart)
	}
	if got[0].TickEnd != 10200+5*testTickrate {
		t.Fatalf("TickEnd = %d", got[0].TickEnd)
	}
}

func TestSegmentSmokesFallbackWhenPopTickMissing(t *testing.T) {
	got := SegmentSmokes([]RawUtilityThrow{mkSmoke(10000, 0, 5)}, nil, defaultTestRules(), testTickrate)
	if len(got) != 1 {
		t.Fatalf("segments len = %d, want 1", len(got))
	}
	wantEnd := 10000 + 8*testTickrate
	if got[0].TickEnd != wantEnd {
		t.Fatalf("TickEnd = %d, want fallback %d", got[0].TickEnd, wantEnd)
	}
}

func TestSmokeCollectorBuildsUtilityPlan(t *testing.T) {
	c := NewSmokeCollector(targetID, defaultTestRules())
	c.RecordTargetIdentity("MARTINEZSA", "T")
	c.RecordSmoke(mkSmoke(10000, 10200, 5))

	plan, err := c.Build(meta())
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if plan.SchemaVersion != killplan.SchemaVersion {
		t.Fatalf("schema = %q, want %q", plan.SchemaVersion, killplan.SchemaVersion)
	}
	if len(plan.Segments) != 1 || len(plan.Segments[0].Utility) != 1 {
		t.Fatalf("segments = %#v", plan.Segments)
	}
	if plan.Stats.TotalSmokesTarget != 1 || plan.Stats.SmokesAfterFilters != 1 {
		t.Fatalf("smoke stats = %#v", plan.Stats)
	}
}

func TestUtilityCollectorCountsSmokesBeforeRoundFilters(t *testing.T) {
	r := defaultTestRules()
	r.MinRound = 10
	r.MaxRound = 10
	c := NewUtilityCollector(targetID, r)
	c.RecordTargetIdentity("MARTINEZSA", "T")
	c.RecordUtility(RawUtilityThrow{Type: SmokeGrenadeType, Round: 9, ThrowTick: 9000})
	c.RecordUtility(RawUtilityThrow{Type: SmokeGrenadeType, Round: 10, ThrowTick: 10000})
	c.RecordUtility(RawUtilityThrow{Type: FlashbangType, Round: 10, ThrowTick: 10100})

	plan, err := c.Build(meta())
	if err != nil {
		t.Fatalf("Build error = %v", err)
	}
	if got, want := plan.Stats.TotalUtilityTarget, 3; got != want {
		t.Fatalf("TotalUtilityTarget = %d, want %d", got, want)
	}
	if got, want := plan.Stats.UtilityAfterFilters, 2; got != want {
		t.Fatalf("UtilityAfterFilters = %d, want %d", got, want)
	}
	if got, want := plan.Stats.TotalSmokesTarget, 2; got != want {
		t.Fatalf("TotalSmokesTarget = %d, want %d", got, want)
	}
	if got, want := plan.Stats.SmokesAfterFilters, 1; got != want {
		t.Fatalf("SmokesAfterFilters = %d, want %d", got, want)
	}
}
