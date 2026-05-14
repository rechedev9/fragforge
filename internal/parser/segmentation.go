package parser

import (
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
)

// RawKill is the normalized representation of a kill produced by the demo
// reader and consumed by the segmentation logic. It is intentionally
// independent of any demoinfocs types so the segmenter can be tested in
// isolation with synthetic data.
type RawKill struct {
	Tick      int
	Round     int
	Weapon    string
	Headshot  bool
	Wallbang  bool
	Killer    killplan.Player
	Victim    killplan.Player
	KillerPos [3]float64
	VictimPos [3]float64
}

// RoundEnd marks the tick at which a given round ended. Segmentation uses
// these to clip a segment's TickEnd if the post-roll would otherwise extend
// past the end of the round.
type RoundEnd struct {
	Round int
	Tick  int
}

// Segment groups a chronologically ordered list of kills into recording
// segments according to the supplied rules and tickrate. The input slice
// must be sorted by Tick ascending.
//
// Segments produced by this function are not yet attached to a kill plan;
// the demo metadata, target identity, and stats are filled in by the parser.
func Segment(kills []RawKill, roundEnds []RoundEnd, r rules.Rules, tickrate int) []killplan.Segment {
	if len(kills) == 0 || tickrate <= 0 {
		return nil
	}

	windowTicks := r.WindowSeconds * tickrate
	preRollTicks := r.PreRollSeconds * tickrate
	postRollTicks := r.PostRollSeconds * tickrate

	// Group consecutive kills whose gap stays within the window.
	var groups [][]RawKill
	current := []RawKill{kills[0]}
	for i := 1; i < len(kills); i++ {
		gap := kills[i].Tick - kills[i-1].Tick
		if gap <= windowTicks {
			current = append(current, kills[i])
			continue
		}
		groups = append(groups, current)
		current = []RawKill{kills[i]}
	}
	groups = append(groups, current)

	// Filter by min_kills_in_window and materialize segments.
	out := make([]killplan.Segment, 0, len(groups))
	for _, g := range groups {
		if len(g) < r.MinKillsInWindow {
			continue
		}
		first := g[0]
		last := g[len(g)-1]
		tickStart := first.Tick - preRollTicks
		if tickStart < 0 {
			tickStart = 0
		}
		tickEnd := last.Tick + postRollTicks
		if endTick, ok := roundEndForKill(roundEnds, first.Round); ok && endTick < tickEnd && endTick >= last.Tick {
			tickEnd = endTick
		}

		seg := killplan.Segment{
			ID:        killplan.FormatSegmentID(len(out) + 1),
			Round:     first.Round,
			TickStart: tickStart,
			TickEnd:   tickEnd,
			Kills:     buildKillPlanKills(g),
		}
		out = append(out, seg)
	}
	return out
}

func roundEndForKill(roundEnds []RoundEnd, round int) (int, bool) {
	for _, re := range roundEnds {
		if re.Round == round {
			return re.Tick, true
		}
	}
	return 0, false
}

func buildKillPlanKills(in []RawKill) []killplan.Kill {
	out := make([]killplan.Kill, len(in))
	for i, k := range in {
		out[i] = killplan.Kill{
			Tick:      k.Tick,
			Weapon:    k.Weapon,
			Headshot:  k.Headshot,
			Wallbang:  k.Wallbang,
			Victim:    k.Victim,
			KillerPos: k.KillerPos,
			VictimPos: k.VictimPos,
		}
	}
	return out
}
