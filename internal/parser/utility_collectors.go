package parser

import (
	"fmt"
	"sort"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
)

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
		return killplan.Plan{}, fmt.Errorf("target steamid %q: %w", c.target, ErrTargetNotFound)
	}
	if m.Tickrate <= 0 {
		return killplan.Plan{}, fmt.Errorf("tickrate must be > 0")
	}

	sortUtilityThrowsByThrowTick(c.smokes)
	for i := range c.smokes {
		if c.smokes[i].ID == "" {
			c.smokes[i].ID = utilityOrdinalID("smoke", i+1)
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
		return killplan.Plan{}, fmt.Errorf("target steamid %q: %w", c.target, ErrTargetNotFound)
	}
	if m.Tickrate <= 0 {
		return killplan.Plan{}, fmt.Errorf("tickrate must be > 0")
	}

	sortUtilityThrowsByThrowTick(c.utility)
	for i := range c.utility {
		if c.utility[i].ID == "" {
			c.utility[i].ID = utilityOrdinalID(utilityIDPrefix(c.utility[i].Type), i+1)
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

func sortUtilityThrowsByThrowTick(utility []RawUtilityThrow) {
	if utilityThrowsSortedByThrowTick(utility) {
		return
	}
	sort.SliceStable(utility, func(i, j int) bool {
		return utility[i].ThrowTick < utility[j].ThrowTick
	})
}

func utilityThrowsSortedByThrowTick(utility []RawUtilityThrow) bool {
	for i := 1; i < len(utility); i++ {
		if utility[i].ThrowTick < utility[i-1].ThrowTick {
			return false
		}
	}
	return true
}
