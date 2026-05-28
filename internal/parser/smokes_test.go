package parser

import (
	"errors"
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

func TestSegmentUtilityDoesNotExposeSharedAppendCapacity(t *testing.T) {
	got := SegmentUtility([]RawUtilityThrow{
		{Type: SmokeGrenadeType, Round: 5, ThrowTick: 10000, PopTick: 10200},
		{Type: FlashbangType, Round: 6, ThrowTick: 20000, PopTick: 20100},
	}, nil, defaultTestRules(), testTickrate)
	if len(got) != 2 {
		t.Fatalf("segments len = %d, want 2", len(got))
	}

	extended := append(got[0].Utility, killplan.UtilityThrow{ID: "extra"})
	if len(extended) != 2 {
		t.Fatalf("extended utility len = %d, want 2", len(extended))
	}
	if got[1].Utility[0].ID == "extra" {
		t.Fatalf("append to first segment utility mutated second segment")
	}
}

func TestSegmentUtilityAllFilteredKeepsEmptyNonNilResult(t *testing.T) {
	r := defaultTestRules()
	r.MinRound = 10
	r.MaxRound = 10

	got := SegmentUtility([]RawUtilityThrow{
		{Type: SmokeGrenadeType, Round: 5, ThrowTick: 10000, PopTick: 10200},
	}, nil, r, testTickrate)
	if got == nil {
		t.Fatalf("SegmentUtility all filtered = nil, want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Fatalf("segments len = %d, want 0", len(got))
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

func TestSmokeCollectorBuildFailsWhenTargetNeverSeen(t *testing.T) {
	c := NewSmokeCollector(targetID, defaultTestRules())
	_, err := c.Build(meta())
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("Build() error = %v, want errors.Is(ErrTargetNotFound)", err)
	}
}

func TestUtilityCollectorBuildFailsWhenTargetNeverSeen(t *testing.T) {
	c := NewUtilityCollector(targetID, defaultTestRules())
	_, err := c.Build(meta())
	if !errors.Is(err, ErrTargetNotFound) {
		t.Fatalf("Build() error = %v, want errors.Is(ErrTargetNotFound)", err)
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

func TestUtilityGeneratedIDsMatchLegacyFormatting(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "tick", got: utilityTickID("smoke", 123), want: "smoke-123"},
		{name: "negative tick", got: utilityTickID("smoke", -1), want: "smoke--1"},
		{name: "ordinal one", got: utilityOrdinalID("flash", 1), want: "flash-001"},
		{name: "ordinal wide", got: utilityOrdinalID("flash", 1000), want: "flash-1000"},
		{name: "ordinal zero", got: utilityOrdinalID("flash", 0), want: "flash-000"},
		{name: "ordinal negative", got: utilityOrdinalID("flash", -1), want: "flash--01"},
		{name: "ordinal negative wide", got: utilityOrdinalID("flash", -100), want: "flash--100"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("id = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestSortUtilityThrowsByThrowTickKeepsStableOrder(t *testing.T) {
	utility := []RawUtilityThrow{
		{ID: "late", ThrowTick: 20},
		{ID: "first", ThrowTick: 10},
		{ID: "second", ThrowTick: 10},
	}

	sortUtilityThrowsByThrowTick(utility)

	got := []string{utility[0].ID, utility[1].ID, utility[2].ID}
	want := []string{"first", "second", "late"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
