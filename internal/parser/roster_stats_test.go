package parser

import (
	"math"
	"testing"
)

func TestKastCreditedPlayers(t *testing.T) {
	// Six participants on a round. Kills (tick order):
	//   A: 1(T)  kills 3(CT), assisted by 2(T)
	//   B: 4(CT) kills 1(T)
	//   C: 2(T)  kills 4(CT)        -> avenges 1's death (2 is 1's teammate)  => 1 traded
	//   D: 1(T)  kills 5(CT)        -> 5's killer (1) is never re-killed after => 5 not traded
	participants := map[uint64]string{1: "T", 2: "T", 3: "CT", 4: "CT", 5: "CT", 6: "T"}
	kills := []roundKill{
		{killer: 1, victim: 3, assister: 2, killerTeam: "T", victimTeam: "CT", tick: 100},
		{killer: 4, victim: 1, killerTeam: "CT", victimTeam: "T", tick: 200},
		{killer: 2, victim: 4, killerTeam: "T", victimTeam: "CT", tick: 210},
		{killer: 1, victim: 5, killerTeam: "T", victimTeam: "CT", tick: 300},
	}

	got := kastCreditedPlayers(participants, kills, 200)

	// 1 killed+traded, 2 killed+assisted, 3 traded, 4 killed, 6 survived. 5 died untraded.
	wantCredited := []uint64{1, 2, 3, 4, 6}
	for _, id := range wantCredited {
		if !got[id] {
			t.Errorf("player %d should earn KAST, got not credited", id)
		}
	}
	if got[5] {
		t.Errorf("player 5 died untraded with no kill/assist, should not earn KAST")
	}
}

func TestKastTradeWindowIsRespected(t *testing.T) {
	// 1 dies to 2; teammate 3 avenges, but outside the trade window.
	participants := map[uint64]string{1: "T", 2: "CT", 3: "T"}
	kills := []roundKill{
		{killer: 2, victim: 1, killerTeam: "CT", victimTeam: "T", tick: 100},
		{killer: 3, victim: 2, killerTeam: "T", victimTeam: "CT", tick: 100 + 500}, // 500 ticks later
	}

	if got := kastCreditedPlayers(participants, kills, 320); got[1] {
		t.Errorf("death avenged after the trade window must not count as a trade")
	}
	if got := kastCreditedPlayers(participants, kills, 600); !got[1] {
		t.Errorf("death avenged inside the trade window should count as a trade")
	}
}

func TestHLTV1Rating(t *testing.T) {
	t.Run("zero rounds is zero", func(t *testing.T) {
		if got := hltv1Rating(10, 5, 0, nil); got != 0 {
			t.Errorf("hltv1Rating with 0 rounds = %v, want 0", got)
		}
	})

	t.Run("known value", func(t *testing.T) {
		// 20 kills / 10 deaths over 20 rounds, all single-kill rounds.
		got := hltv1Rating(20, 10, 20, map[int]int{1: 20})
		const want = 1.2444
		if math.Abs(got-want) > 0.001 {
			t.Errorf("hltv1Rating = %.4f, want ~%.4f", got, want)
		}
	})

	t.Run("more frags rates higher", func(t *testing.T) {
		high := hltv1Rating(25, 8, 20, map[int]int{1: 15, 2: 5})
		low := hltv1Rating(8, 18, 20, map[int]int{1: 8})
		if !(high > low) {
			t.Errorf("high-frag rating %.3f should exceed low-frag rating %.3f", high, low)
		}
	})
}
