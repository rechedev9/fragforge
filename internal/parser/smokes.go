package parser

import (
	"fmt"
	"sort"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
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

	out := make([]killplan.Segment, 0, len(smokes))
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
		if endTick, ok := roundEndForKill(roundEnds, smoke.Round); ok && endTick < tickEnd && endTick >= smoke.ThrowTick {
			tickEnd = endTick
		}
		if tickEnd <= tickStart {
			tickEnd = tickStart + max(1, tickrate)
		}

		out = append(out, killplan.Segment{
			ID:        killplan.FormatSegmentID(len(out) + 1),
			Round:     smoke.Round,
			TickStart: tickStart,
			TickEnd:   tickEnd,
			Utility:   []killplan.UtilityThrow{buildUtilityThrow(smoke)},
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
	out := make([]killplan.Segment, 0, len(utility))
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
		if endTick, ok := roundEndForKill(roundEnds, u.Round); ok && endTick < tickEnd && endTick >= u.ThrowTick {
			tickEnd = endTick
		}
		if tickEnd <= tickStart {
			tickEnd = tickStart + max(1, tickrate)
		}

		out = append(out, killplan.Segment{
			ID:        killplan.FormatSegmentID(len(out) + 1),
			Round:     u.Round,
			TickStart: tickStart,
			TickEnd:   tickEnd,
			Utility:   []killplan.UtilityThrow{buildUtilityThrow(u)},
		})
	}
	return out
}

func buildUtilityThrow(in RawUtilityThrow) killplan.UtilityThrow {
	id := in.ID
	if id == "" {
		id = fmt.Sprintf("%s-%d", utilityIDPrefix(in.Type), in.ThrowTick)
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

type SmokeCollector struct {
	target string
	rules  rules.Rules

	smokes    []RawUtilityThrow
	roundEnds []RoundEnd

	totalSmokesTarget  int
	smokesAfterFilters int

	targetName        string
	targetTeamAtStart string
	targetSeen        bool
}

type UtilityCollector struct {
	target string
	rules  rules.Rules

	utility   []RawUtilityThrow
	roundEnds []RoundEnd

	totalUtilityTarget  int
	utilityAfterFilters int
	totalSmokesTarget   int
	smokesAfterFilters  int

	targetName        string
	targetTeamAtStart string
	targetSeen        bool
}

func NewSmokeCollector(target string, r rules.Rules) *SmokeCollector {
	return &SmokeCollector{target: target, rules: r}
}

func NewUtilityCollector(target string, r rules.Rules) *UtilityCollector {
	return &UtilityCollector{target: target, rules: r}
}

func (c *SmokeCollector) RecordTargetIdentity(name, teamAtStart string) {
	c.targetName = name
	c.targetTeamAtStart = teamAtStart
	c.targetSeen = true
}

func (c *UtilityCollector) RecordTargetIdentity(name, teamAtStart string) {
	c.targetName = name
	c.targetTeamAtStart = teamAtStart
	c.targetSeen = true
}

func (c *SmokeCollector) RecordSmoke(s RawUtilityThrow) {
	c.totalSmokesTarget++
	if s.Type == "" {
		s.Type = SmokeGrenadeType
	}
	if !c.rules.AllowsRound(s.Round) {
		return
	}
	c.smokes = append(c.smokes, s)
	c.smokesAfterFilters++
}

func (c *UtilityCollector) RecordUtility(u RawUtilityThrow) {
	if !isTrackedUtilityType(u.Type) {
		return
	}
	c.totalUtilityTarget++
	if u.Type == SmokeGrenadeType {
		c.totalSmokesTarget++
	}
	if !c.rules.AllowsRound(u.Round) {
		return
	}
	c.utility = append(c.utility, u)
	c.utilityAfterFilters++
	if u.Type == SmokeGrenadeType {
		c.smokesAfterFilters++
	}
}

func (c *SmokeCollector) RecordRoundEnd(re RoundEnd) {
	c.roundEnds = append(c.roundEnds, re)
}

func (c *UtilityCollector) RecordRoundEnd(re RoundEnd) {
	c.roundEnds = append(c.roundEnds, re)
}

func (c *SmokeCollector) Build(m PlanMeta) (killplan.Plan, error) {
	if !c.targetSeen {
		return killplan.Plan{}, fmt.Errorf("target steamid %q not found in demo", c.target)
	}
	if m.Tickrate <= 0 {
		return killplan.Plan{}, fmt.Errorf("tickrate must be > 0")
	}

	sort.SliceStable(c.smokes, func(i, j int) bool {
		return c.smokes[i].ThrowTick < c.smokes[j].ThrowTick
	})
	for i := range c.smokes {
		if c.smokes[i].ID == "" {
			c.smokes[i].ID = fmt.Sprintf("smoke-%03d", i+1)
		}
	}

	segs := SegmentSmokes(c.smokes, c.roundEnds, c.rules, m.Tickrate)
	if segs == nil {
		segs = []killplan.Segment{}
	}

	plan := killplan.NewPlan()
	plan.Demo = killplan.Demo{
		Path:          m.DemoPath,
		SHA256:        m.SHA256,
		Map:           m.Map,
		Tickrate:      m.Tickrate,
		DurationTicks: m.DurationTicks,
	}
	plan.Target = killplan.Target{
		SteamID64:   c.target,
		NameInDemo:  c.targetName,
		TeamAtStart: c.targetTeamAtStart,
	}
	plan.Rules = c.rules
	plan.Segments = segs
	plan.Stats = killplan.Stats{
		TotalSmokesTarget:    c.totalSmokesTarget,
		SmokesAfterFilters:   c.smokesAfterFilters,
		SegmentsCreated:      len(segs),
		DurationSecondsTotal: totalSegmentSeconds(segs, m.Tickrate),
	}
	return plan, nil
}

func (c *UtilityCollector) Build(m PlanMeta) (killplan.Plan, error) {
	if !c.targetSeen {
		return killplan.Plan{}, fmt.Errorf("target steamid %q not found in demo", c.target)
	}
	if m.Tickrate <= 0 {
		return killplan.Plan{}, fmt.Errorf("tickrate must be > 0")
	}

	sort.SliceStable(c.utility, func(i, j int) bool {
		return c.utility[i].ThrowTick < c.utility[j].ThrowTick
	})
	for i := range c.utility {
		if c.utility[i].ID == "" {
			c.utility[i].ID = fmt.Sprintf("%s-%03d", utilityIDPrefix(c.utility[i].Type), i+1)
		}
	}

	segs := SegmentUtility(c.utility, c.roundEnds, c.rules, m.Tickrate)
	if segs == nil {
		segs = []killplan.Segment{}
	}

	plan := killplan.NewPlan()
	plan.Demo = killplan.Demo{
		Path:          m.DemoPath,
		SHA256:        m.SHA256,
		Map:           m.Map,
		Tickrate:      m.Tickrate,
		DurationTicks: m.DurationTicks,
	}
	plan.Target = killplan.Target{
		SteamID64:   c.target,
		NameInDemo:  c.targetName,
		TeamAtStart: c.targetTeamAtStart,
	}
	plan.Rules = c.rules
	plan.Segments = segs
	plan.Stats = killplan.Stats{
		TotalUtilityTarget:   c.totalUtilityTarget,
		UtilityAfterFilters:  c.utilityAfterFilters,
		TotalSmokesTarget:    c.totalSmokesTarget,
		SmokesAfterFilters:   c.smokesAfterFilters,
		SegmentsCreated:      len(segs),
		DurationSecondsTotal: totalSegmentSeconds(segs, m.Tickrate),
	}
	return plan, nil
}
