package parser

import (
	"testing"

	"github.com/reche/zackvideo/internal/killplan"
)

var (
	benchmarkPlan           killplan.Plan
	benchmarkPendingUtility *RawUtilityThrow
	benchmarkSegments       []killplan.Segment
)

func BenchmarkSegment(b *testing.B) {
	kills, roundEnds := benchmarkKills(240)
	r := defaultTestRules()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkSegments = Segment(kills, roundEnds, r, testTickrate)
	}
}

func BenchmarkSegmentUtility(b *testing.B) {
	utility, roundEnds := benchmarkUtility(240)
	r := defaultTestRules()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkSegments = SegmentUtility(utility, roundEnds, r, testTickrate)
	}
}

func BenchmarkSegmentUtilityWithRoundFilter(b *testing.B) {
	utility, roundEnds := benchmarkUtility(240)
	r := defaultTestRules()
	r.MinRound = 10
	r.MaxRound = 20

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkSegments = SegmentUtility(utility, roundEnds, r, testTickrate)
	}
}

func BenchmarkCollectorBuildSorted(b *testing.B) {
	kills, roundEnds := benchmarkKills(240)
	r := defaultTestRules()
	m := meta()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c := NewCollector(targetID, r)
		c.RecordTargetIdentity("MARTINEZSA", "CT")
		for _, kill := range kills {
			c.RecordKill(kill)
		}
		for _, roundEnd := range roundEnds {
			c.RecordRoundEnd(roundEnd)
		}

		plan, err := c.Build(m)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkPlan = plan
	}
}

func BenchmarkUtilityCollectorBuildSorted(b *testing.B) {
	utility, roundEnds := benchmarkUtility(240)
	r := defaultTestRules()
	m := meta()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c := NewUtilityCollector(targetID, r)
		c.RecordTargetIdentity("MARTINEZSA", "CT")
		for _, throw := range utility {
			c.RecordUtility(throw)
		}
		for _, roundEnd := range roundEnds {
			c.RecordRoundEnd(roundEnd)
		}

		plan, err := c.Build(m)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkPlan = plan
	}
}

func BenchmarkFindRecentPendingUtility(b *testing.B) {
	thrower := mkPlayer(killerID, "MARTINEZSA", 0)
	throwerSteamID := playerIdentity(thrower).SteamID64
	pending := make([]*RawUtilityThrow, 240)
	for i := range pending {
		pending[i] = &RawUtilityThrow{
			Type:      FlashbangType,
			Thrower:   playerIdentity(thrower),
			ThrowTick: 1000 + i*testTickrate,
		}
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		benchmarkPendingUtility = findRecentPendingUtility(pending, thrower, throwerSteamID, FlashbangType, 1000+239*testTickrate, testTickrate)
	}
}

func benchmarkKills(count int) ([]RawKill, []RoundEnd) {
	kills := make([]RawKill, count)
	roundEnds := make([]RoundEnd, 0, count/8+1)
	for i := range kills {
		round := i/8 + 1
		kills[i] = mkKill(1000+i*testTickrate*3, round, "awp")
		if i%8 == 7 {
			roundEnds = append(roundEnds, RoundEnd{
				Round: round,
				Tick:  kills[i].Tick + testTickrate,
			})
		}
	}
	return kills, roundEnds
}

func benchmarkUtility(count int) ([]RawUtilityThrow, []RoundEnd) {
	utility := make([]RawUtilityThrow, count)
	roundEnds := make([]RoundEnd, 0, count/8+1)
	types := []string{SmokeGrenadeType, FlashbangType, MolotovType, IncendiaryGrenadeType}
	for i := range utility {
		round := i/8 + 1
		utility[i] = RawUtilityThrow{
			Type:      types[i%len(types)],
			Round:     round,
			ThrowTick: 1000 + i*testTickrate*3,
			PopTick:   1100 + i*testTickrate*3,
		}
		if i%8 == 7 {
			roundEnds = append(roundEnds, RoundEnd{
				Round: round,
				Tick:  utility[i].PopTick + testTickrate,
			})
		}
	}
	return utility, roundEnds
}
