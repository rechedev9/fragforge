package parser

import (
	"errors"
	"fmt"
	"sort"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/rules"
)

// Collector accumulates the events the parser observes for the target player
// and assembles a kill plan when Build is called.
//
// A Collector is single-use: feed it events via RecordKill /
// RecordRoundEnd / RecordTargetIdentity in chronological order, then call
// Build exactly once.
type Collector struct {
	target string
	rules  rules.Rules

	kills     []RawKill
	roundEnds []RoundEnd

	totalKillsTarget  int
	killsAfterFilters int

	targetName        string
	targetTeamAtStart string
	targetSeen        bool
}

// PlanMeta carries the metadata about the source demo that the segmenter
// itself cannot derive (path, hash, map name, tickrate, demo duration).
type PlanMeta struct {
	DemoPath      string
	SHA256        string
	Map           string
	Tickrate      int
	DurationTicks int
}

// NewCollector returns a Collector configured for the given target SteamID64
// and rules.
func NewCollector(target string, r rules.Rules) *Collector {
	return &Collector{target: target, rules: r}
}

// RecordTargetIdentity captures the in-demo display name and starting team
// of the target player. Called once at the start of the demo (or whenever
// the target is first observed).
func (c *Collector) RecordTargetIdentity(name, teamAtStart string) {
	c.targetName = name
	c.targetTeamAtStart = teamAtStart
	c.targetSeen = true
}

// RecordKill processes one kill attributed to the target. The kill is
// always counted in TotalKillsTarget; it is appended to the segmenter input
// only if it passes the configured filters.
func (c *Collector) RecordKill(k RawKill) {
	c.totalKillsTarget++

	if !c.rules.AllowsWeapon(k.Weapon) {
		return
	}
	if c.rules.IncludeHeadshotOnly && !k.Headshot {
		return
	}
	if !c.rules.AllowsRound(k.Round) {
		return
	}

	c.kills = append(c.kills, k)
	c.killsAfterFilters++
}

// RecordRoundEnd remembers the tick at which a given round ended; used by
// segmentation to clip a segment's post-roll if the round ends inside it.
func (c *Collector) RecordRoundEnd(re RoundEnd) {
	c.roundEnds = append(c.roundEnds, re)
}

// TotalKillsTarget reports the number of kills attributed to the target
// observed in the demo, before filters.
func (c *Collector) TotalKillsTarget() int { return c.totalKillsTarget }

// KillsAfterFilters reports the number of kills that survived the weapon /
// headshot / round filters.
func (c *Collector) KillsAfterFilters() int { return c.killsAfterFilters }

// Build assembles the final kill plan. It returns an error if the target
// player was never observed in the demo.
func (c *Collector) Build(m PlanMeta) (killplan.Plan, error) {
	if !c.targetSeen {
		return killplan.Plan{}, fmt.Errorf("target steamid %q: %w", c.target, ErrTargetNotFound)
	}
	if m.Tickrate <= 0 {
		return killplan.Plan{}, errors.New("tickrate must be > 0")
	}

	sortRawKillsByTick(c.kills)

	segs := Segment(c.kills, c.roundEnds, c.rules, m.Tickrate)
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
		TotalKillsTarget:     c.totalKillsTarget,
		KillsAfterFilters:    c.killsAfterFilters,
		SegmentsCreated:      len(segs),
		DurationSecondsTotal: totalSegmentSeconds(segs, m.Tickrate),
	}
	return plan, nil
}

func totalSegmentSeconds(segs []killplan.Segment, tickrate int) float64 {
	total := 0.0
	for _, s := range segs {
		total += float64(s.TickEnd-s.TickStart) / float64(tickrate)
	}
	return total
}

func sortRawKillsByTick(kills []RawKill) {
	if rawKillsSortedByTick(kills) {
		return
	}
	sort.SliceStable(kills, func(i, j int) bool { return kills[i].Tick < kills[j].Tick })
}

func rawKillsSortedByTick(kills []RawKill) bool {
	for i := 1; i < len(kills); i++ {
		if kills[i].Tick < kills[i-1].Tick {
			return false
		}
	}
	return true
}
