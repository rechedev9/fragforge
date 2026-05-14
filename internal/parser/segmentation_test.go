package parser

import (
	"testing"

	"github.com/reche/zackvideo/internal/rules"
)

func defaultTestRules() rules.Rules {
	r := rules.Default()
	// keep the test deterministic regardless of future default changes
	r.WindowSeconds = 8
	r.PreRollSeconds = 3
	r.PostRollSeconds = 5
	r.MinKillsInWindow = 1
	return r
}

const testTickrate = 64

func mkKill(tick, round int, weapon string) RawKill {
	return RawKill{
		Tick:   tick,
		Round:  round,
		Weapon: weapon,
	}
}

func TestSegmentEmptyKillsReturnsNoSegments(t *testing.T) {
	got := Segment(nil, nil, defaultTestRules(), testTickrate)
	if len(got) != 0 {
		t.Errorf("Segment(nil) = %d segments, want 0", len(got))
	}
}

func TestSegmentSingleKillProducesOneSegment(t *testing.T) {
	kills := []RawKill{mkKill(10000, 5, "awp")}
	got := Segment(kills, nil, defaultTestRules(), testTickrate)
	if len(got) != 1 {
		t.Fatalf("got %d segments, want 1", len(got))
	}
	s := got[0]
	if s.Round != 5 {
		t.Errorf("Round = %d, want 5", s.Round)
	}
	if len(s.Kills) != 1 {
		t.Errorf("Kills length = %d, want 1", len(s.Kills))
	}
	// pre_roll_seconds=3 at 64 tickrate = 192 ticks before
	if s.TickStart != 10000-3*testTickrate {
		t.Errorf("TickStart = %d, want %d", s.TickStart, 10000-3*testTickrate)
	}
	// post_roll_seconds=5 = 320 ticks after
	if s.TickEnd != 10000+5*testTickrate {
		t.Errorf("TickEnd = %d, want %d", s.TickEnd, 10000+5*testTickrate)
	}
	if s.ID != "seg-001" {
		t.Errorf("ID = %q, want seg-001", s.ID)
	}
}

func TestSegmentTwoKillsWithinWindowMergeIntoOneSegment(t *testing.T) {
	// 2 ticks_per_sec * 7 sec window = 7 sec apart. window is 8 sec → same segment.
	kills := []RawKill{
		mkKill(10000, 5, "awp"),
		mkKill(10000+7*testTickrate, 5, "awp"),
	}
	got := Segment(kills, nil, defaultTestRules(), testTickrate)
	if len(got) != 1 {
		t.Fatalf("got %d segments, want 1", len(got))
	}
	if len(got[0].Kills) != 2 {
		t.Errorf("Kills length = %d, want 2", len(got[0].Kills))
	}
	if got[0].TickStart != 10000-3*testTickrate {
		t.Errorf("TickStart = %d, want %d", got[0].TickStart, 10000-3*testTickrate)
	}
	// TickEnd uses the last kill in the segment
	want := 10000 + 7*testTickrate + 5*testTickrate
	if got[0].TickEnd != want {
		t.Errorf("TickEnd = %d, want %d", got[0].TickEnd, want)
	}
}

func TestSegmentKillsOutsideWindowSplitIntoSeparateSegments(t *testing.T) {
	// 9 seconds apart > 8 second window
	kills := []RawKill{
		mkKill(10000, 5, "awp"),
		mkKill(10000+9*testTickrate, 5, "awp"),
	}
	got := Segment(kills, nil, defaultTestRules(), testTickrate)
	if len(got) != 2 {
		t.Fatalf("got %d segments, want 2", len(got))
	}
	if got[0].ID != "seg-001" || got[1].ID != "seg-002" {
		t.Errorf("IDs = %q, %q; want seg-001, seg-002", got[0].ID, got[1].ID)
	}
}

func TestSegmentTransitiveChainingAcrossKills(t *testing.T) {
	// k1 at t=0, k2 at t=7s (within 8s of k1), k3 at t=14s (within 8s of k2).
	// All three should land in one segment.
	kills := []RawKill{
		mkKill(10000, 5, "awp"),
		mkKill(10000+7*testTickrate, 5, "awp"),
		mkKill(10000+14*testTickrate, 5, "awp"),
	}
	got := Segment(kills, nil, defaultTestRules(), testTickrate)
	if len(got) != 1 {
		t.Fatalf("got %d segments, want 1", len(got))
	}
	if len(got[0].Kills) != 3 {
		t.Errorf("Kills length = %d, want 3", len(got[0].Kills))
	}
}

func TestSegmentMinKillsInWindowDropsSingleKillSegments(t *testing.T) {
	r := defaultTestRules()
	r.MinKillsInWindow = 2

	kills := []RawKill{
		mkKill(10000, 5, "awp"),                       // alone
		mkKill(20000, 6, "awp"),                       // start of a pair...
		mkKill(20000+2*testTickrate, 6, "awp"),        // ...with this one
		mkKill(40000, 7, "awp"),                       // alone
	}
	got := Segment(kills, nil, r, testTickrate)
	if len(got) != 1 {
		t.Fatalf("got %d segments, want 1 (only the pair survives)", len(got))
	}
	if len(got[0].Kills) != 2 {
		t.Errorf("surviving segment kills = %d, want 2", len(got[0].Kills))
	}
	if got[0].ID != "seg-001" {
		t.Errorf("ID = %q, want seg-001 (renumbered after filtering)", got[0].ID)
	}
}

func TestSegmentPreRollClampedToZero(t *testing.T) {
	// kill very early — pre-roll would underflow past tick 0
	kills := []RawKill{mkKill(100, 1, "awp")}
	got := Segment(kills, nil, defaultTestRules(), testTickrate)
	if len(got) != 1 {
		t.Fatalf("got %d segments, want 1", len(got))
	}
	if got[0].TickStart != 0 {
		t.Errorf("TickStart = %d, want 0 (clamped)", got[0].TickStart)
	}
}

func TestSegmentClippedAtRoundEnd(t *testing.T) {
	// kill at tick 10000, post-roll would extend to 10000 + 320 = 10320.
	// Round 5 ends at tick 10100 → segment should clip to 10100.
	kills := []RawKill{mkKill(10000, 5, "awp")}
	roundEnds := []RoundEnd{{Round: 5, Tick: 10100}}
	got := Segment(kills, roundEnds, defaultTestRules(), testTickrate)
	if len(got) != 1 {
		t.Fatalf("got %d segments, want 1", len(got))
	}
	if got[0].TickEnd != 10100 {
		t.Errorf("TickEnd = %d, want 10100 (clipped at round end)", got[0].TickEnd)
	}
}

func TestSegmentNotClippedWhenRoundEndIsAfterPostRoll(t *testing.T) {
	// Round 5 ends way after post-roll; no clipping should happen.
	kills := []RawKill{mkKill(10000, 5, "awp")}
	roundEnds := []RoundEnd{{Round: 5, Tick: 999999}}
	got := Segment(kills, roundEnds, defaultTestRules(), testTickrate)
	if len(got) != 1 {
		t.Fatalf("got %d segments, want 1", len(got))
	}
	want := 10000 + 5*testTickrate
	if got[0].TickEnd != want {
		t.Errorf("TickEnd = %d, want %d (not clipped)", got[0].TickEnd, want)
	}
}

func TestSegmentRoundIsFirstKillsRound(t *testing.T) {
	// Edge case: two kills span a round boundary (unusual but possible if a
	// kill counted at the end of one round and the next sits at the very start).
	// The segment's Round should follow the first kill.
	kills := []RawKill{
		mkKill(10000, 5, "awp"),
		mkKill(10000+4*testTickrate, 6, "awp"),
	}
	got := Segment(kills, nil, defaultTestRules(), testTickrate)
	if len(got) != 1 {
		t.Fatalf("got %d segments, want 1", len(got))
	}
	if got[0].Round != 5 {
		t.Errorf("Round = %d, want 5 (first kill's round)", got[0].Round)
	}
}
