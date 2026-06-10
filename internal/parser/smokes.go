package parser

import (
	"strconv"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/rules"
)

const SmokeGrenadeType = "smokegrenade"
const FlashbangType = "flashbang"
const MolotovType = "molotov"
const IncendiaryGrenadeType = "incgrenade"

// RawUtilityThrow is the normalized representation of a target-player utility
// throw produced by the demo reader and consumed by the utility segmenter.
type RawUtilityThrow struct {
	ID               string
	Type             string
	Round            int
	ThrowTick        int
	PopTick          int
	ExpireTick       int
	Thrower          killplan.Player
	ThrowPos         [3]float64
	LandingPos       [3]float64
	LandingSource    string
	ThrowPlace       string
	ThrowStateTick   int
	ThrowStateSource string
	ThrowAction      string
	Stance           string
	Movement         string
	Speed2D          float64
	OnGround         bool
	Walking          bool
	Ducking          bool
	LineupMatch      *killplan.LineupMatch
}

// SegmentSmokes creates one teaching clip per target smoke. Unlike kill
// segmentation, smokes are intentionally not grouped: each lineup gets a
// standalone timeline from throw setup through pop/fade context.
func SegmentSmokes(smokes []RawUtilityThrow, roundEnds []RoundEnd, r rules.Rules, tickrate int) []killplan.Segment {
	if len(smokes) == 0 || tickrate <= 0 {
		return nil
	}

	preRollTicks := r.PreRollSeconds * tickrate
	postRollTicks := r.PostRollSeconds * tickrate
	fallbackTicks := 8 * tickrate
	roundEndByRound := indexRoundEnds(roundEnds)
	segmentCount := len(smokes)
	if roundFilterActive(r) {
		segmentCount = countSmokeSegments(smokes, r)
	}

	out := make([]killplan.Segment, 0, segmentCount)
	utilityThrows := make([]killplan.UtilityThrow, 0, segmentCount)
	for _, smoke := range smokes {
		if smoke.Type != SmokeGrenadeType {
			continue
		}
		if !r.AllowsRound(smoke.Round) {
			continue
		}
		tickStart := smoke.ThrowTick - preRollTicks
		if tickStart < 0 {
			tickStart = 0
		}
		tickEnd := smoke.ThrowTick + fallbackTicks
		if smoke.PopTick > 0 {
			tickEnd = smoke.PopTick + postRollTicks
		}
		if endTick, ok := roundEndForRound(roundEndByRound, smoke.Round); ok && endTick < tickEnd && endTick >= smoke.ThrowTick {
			tickEnd = endTick
		}
		if tickEnd <= tickStart {
			tickEnd = tickStart + max(1, tickrate)
		}
		utilityStart := len(utilityThrows)
		utilityThrows = append(utilityThrows, buildUtilityThrow(smoke))

		out = append(out, killplan.Segment{
			ID:        killplan.FormatSegmentID(len(out) + 1),
			Round:     smoke.Round,
			TickStart: tickStart,
			TickEnd:   tickEnd,
			Utility:   utilityThrows[utilityStart : utilityStart+1 : utilityStart+1],
		})
	}
	return out
}

// SegmentUtility creates one teaching clip per target smoke, flash, molotov,
// or incendiary. Each throw stays standalone so the editor can label its
// destination cleanly in a vertical short.
func SegmentUtility(utility []RawUtilityThrow, roundEnds []RoundEnd, r rules.Rules, tickrate int) []killplan.Segment {
	if len(utility) == 0 || tickrate <= 0 {
		return nil
	}

	preRollTicks := r.PreRollSeconds * tickrate
	postRollTicks := r.PostRollSeconds * tickrate
	roundEndByRound := indexRoundEnds(roundEnds)
	segmentCount := len(utility)
	if roundFilterActive(r) {
		segmentCount = countUtilitySegments(utility, r)
	}
	out := make([]killplan.Segment, 0, segmentCount)
	utilityThrows := make([]killplan.UtilityThrow, 0, segmentCount)
	for _, u := range utility {
		if !isTrackedUtilityType(u.Type) || !r.AllowsRound(u.Round) {
			continue
		}
		tickStart := u.ThrowTick - preRollTicks
		if tickStart < 0 {
			tickStart = 0
		}
		tickEnd := u.ThrowTick + utilityFallbackTicks(u.Type, tickrate)
		if u.PopTick > 0 {
			tickEnd = u.PopTick + postRollTicks
			if u.Type == FlashbangType {
				tickEnd = u.PopTick + 2*tickrate
			}
		}
		if endTick, ok := roundEndForRound(roundEndByRound, u.Round); ok && endTick < tickEnd && endTick >= u.ThrowTick {
			tickEnd = endTick
		}
		if tickEnd <= tickStart {
			tickEnd = tickStart + max(1, tickrate)
		}
		utilityStart := len(utilityThrows)
		utilityThrows = append(utilityThrows, buildUtilityThrow(u))

		out = append(out, killplan.Segment{
			ID:        killplan.FormatSegmentID(len(out) + 1),
			Round:     u.Round,
			TickStart: tickStart,
			TickEnd:   tickEnd,
			Utility:   utilityThrows[utilityStart : utilityStart+1 : utilityStart+1],
		})
	}
	return out
}

func roundFilterActive(r rules.Rules) bool {
	return r.MinRound > 1 || r.MaxRound != 0
}

func countSmokeSegments(smokes []RawUtilityThrow, r rules.Rules) int {
	count := 0
	for _, smoke := range smokes {
		if smoke.Type == SmokeGrenadeType && r.AllowsRound(smoke.Round) {
			count++
		}
	}
	return count
}

func countUtilitySegments(utility []RawUtilityThrow, r rules.Rules) int {
	count := 0
	for _, u := range utility {
		if isTrackedUtilityType(u.Type) && r.AllowsRound(u.Round) {
			count++
		}
	}
	return count
}

func buildUtilityThrow(in RawUtilityThrow) killplan.UtilityThrow {
	id := in.ID
	if id == "" {
		id = utilityTickID(utilityIDPrefix(in.Type), in.ThrowTick)
	}
	typ := in.Type
	if typ == "" {
		typ = SmokeGrenadeType
	}
	return killplan.UtilityThrow{
		ID:               id,
		Type:             typ,
		Round:            in.Round,
		ThrowTick:        in.ThrowTick,
		PopTick:          in.PopTick,
		ExpireTick:       in.ExpireTick,
		Thrower:          in.Thrower,
		ThrowPos:         in.ThrowPos,
		LandingPos:       in.LandingPos,
		LandingSource:    in.LandingSource,
		ThrowPlace:       in.ThrowPlace,
		ThrowStateTick:   in.ThrowStateTick,
		ThrowStateSource: in.ThrowStateSource,
		ThrowAction:      in.ThrowAction,
		Stance:           in.Stance,
		Movement:         in.Movement,
		Speed2D:          in.Speed2D,
		OnGround:         in.OnGround,
		Walking:          in.Walking,
		Ducking:          in.Ducking,
		LineupMatch:      in.LineupMatch,
	}
}

func isTrackedUtilityType(typ string) bool {
	switch typ {
	case SmokeGrenadeType, FlashbangType, MolotovType, IncendiaryGrenadeType:
		return true
	default:
		return false
	}
}

func utilityFallbackTicks(typ string, tickrate int) int {
	switch typ {
	case FlashbangType:
		return 4 * tickrate
	case MolotovType, IncendiaryGrenadeType:
		return 9 * tickrate
	default:
		return 8 * tickrate
	}
}

func utilityIDPrefix(typ string) string {
	switch typ {
	case FlashbangType:
		return "flash"
	case MolotovType:
		return "molotov"
	case IncendiaryGrenadeType:
		return "incgrenade"
	default:
		return "smoke"
	}
}

func utilityTickID(prefix string, tick int) string {
	var b [32]byte
	if len(prefix)+1+20 <= len(b) {
		out := b[:0]
		out = append(out, prefix...)
		out = append(out, '-')
		out = strconv.AppendInt(out, int64(tick), 10)
		return string(out)
	}
	return prefix + "-" + strconv.Itoa(tick)
}

func utilityOrdinalID(prefix string, n int) string {
	var b [32]byte
	if len(prefix)+1+20 <= len(b) {
		out := b[:0]
		out = append(out, prefix...)
		out = append(out, '-')
		out = appendZeroPadded3(out, n)
		return string(out)
	}
	return prefix + "-" + zeroPadded3(n)
}

func zeroPadded3(n int) string {
	var b [20]byte
	out := appendZeroPadded3(b[:0], n)
	return string(out)
}

func appendZeroPadded3(out []byte, n int) []byte {
	if n >= 0 && n < 1000 {
		return append(out,
			byte('0'+n/100),
			byte('0'+n/10%10),
			byte('0'+n%10),
		)
	}
	if n > -100 && n < 0 {
		abs := -n
		out = append(out, '-')
		if abs >= 10 {
			out = append(out, byte('0'+abs/10))
		} else {
			out = append(out, '0')
		}
		return append(out, byte('0'+abs%10))
	}
	return strconv.AppendInt(out, int64(n), 10)
}
