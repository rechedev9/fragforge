package parser

import (
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/rules"
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
	roundEndByRound := indexRoundEnds(roundEnds)

	out := make([]killplan.Segment, 0, countKillSegmentGroups(kills, windowTicks, r.MinKillsInWindow))
	groupStart := 0
	for i := 1; i <= len(kills); i++ {
		if i < len(kills) && kills[i].Tick-kills[i-1].Tick <= windowTicks {
			continue
		}

		g := kills[groupStart:i]
		groupStart = i
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
		// Clip the post-roll at the end of the round where the segment actually
		// ends (last kill's round). For the common single-round group this is the
		// same as the first kill's round; for a group that spans a round boundary
		// it prevents the clip from bleeding into the following round.
		if endTick, ok := roundEndForRound(roundEndByRound, last.Round); ok && endTick < tickEnd && endTick >= last.Tick {
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

func countKillSegmentGroups(kills []RawKill, windowTicks, minKills int) int {
	count := 0
	groupStart := 0
	for i := 1; i <= len(kills); i++ {
		if i < len(kills) && kills[i].Tick-kills[i-1].Tick <= windowTicks {
			continue
		}
		if i-groupStart >= minKills {
			count++
		}
		groupStart = i
	}
	return count
}

func indexRoundEnds(roundEnds []RoundEnd) map[int]int {
	if len(roundEnds) == 0 {
		return nil
	}
	out := make(map[int]int, len(roundEnds))
	for _, re := range roundEnds {
		if _, ok := out[re.Round]; !ok {
			out[re.Round] = re.Tick
		}
	}
	return out
}

func roundEndForRound(roundEndByRound map[int]int, round int) (int, bool) {
	if len(roundEndByRound) == 0 {
		return 0, false
	}
	tick, ok := roundEndByRound[round]
	return tick, ok
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
