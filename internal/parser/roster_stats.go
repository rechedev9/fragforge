package parser

import (
	"math"
	"strconv"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

// tradeWindowSeconds is how long after a death a teammate kill still counts as a
// trade for KAST. Five seconds is the common convention (HLTV/Leetify use ~5s).
const tradeWindowSeconds = 5.0

// HLTV 1.0 rating constants: the league-average kills-per-round, survival-per-
// round, and weighted-multi-kill-per-round used to normalise each component so a
// rating of ~1.0 is average. These are the published HLTV 1.0 baselines.
const (
	avgKillsPerRound       = 0.679
	avgSurvivedPerRound    = 0.317
	avgMultiKillWeightPerR = 1.277
)

// roundKill is one kill within a round, kept for KAST and trade detection. Teams
// are the "CT"/"T" labels at the moment of the kill.
type roundKill struct {
	killer, victim, assister uint64
	killerTeam, victimTeam   string
	tick                     int
}

// rosterAccumulator gathers everything a scoreboard line needs in a single demo
// pass: raw K/D/A plus per-player damage, MVPs, headshot kills, and the per-round
// data (kills, deaths, participants) that KAST and the HLTV rating are derived
// from. Counters reset on MatchStart so warmup never counts; a demo with no
// MatchStart simply accumulates from the first event.
type rosterAccumulator struct {
	players           map[uint64]*PlayerStat
	damage            map[uint64]int
	mvps              map[uint64]int
	kastRounds        map[uint64]int
	roundsByKillCount map[uint64]map[int]int // player -> kills-in-a-round -> count of such rounds
	rounds            int

	// mapName comes from the demo header (CSVCMsg_ServerInfo) and, like the map
	// name callers elsewhere in this package read (see demo_smokes.go), is not
	// cleared on MatchStart: it is set once near the start of the demo and the
	// map never changes mid-match.
	mapName string

	// Current-round scratch, reset each RoundStart.
	curParticipants map[uint64]string // steamid -> team, snapshot at round start
	curKills        []roundKill
	curKillsByID    map[uint64]int
}

func newRosterAccumulator() *rosterAccumulator {
	a := &rosterAccumulator{}
	a.reset()
	return a
}

// reset clears all tallies. Called once at construction and again on MatchStart
// so kills/damage during warmup or knife rounds are discarded.
func (a *rosterAccumulator) reset() {
	a.players = map[uint64]*PlayerStat{}
	a.damage = map[uint64]int{}
	a.mvps = map[uint64]int{}
	a.kastRounds = map[uint64]int{}
	a.roundsByKillCount = map[uint64]map[int]int{}
	a.rounds = 0
	a.curParticipants = map[uint64]string{}
	a.curKills = nil
	a.curKillsByID = map[uint64]int{}
}

func (a *rosterAccumulator) observe(pl *common.Player) *PlayerStat {
	if pl == nil || pl.SteamID64 == 0 {
		return nil
	}
	stat, ok := a.players[pl.SteamID64]
	if !ok {
		stat = &PlayerStat{SteamID64: strconv.FormatUint(pl.SteamID64, 10)}
		a.players[pl.SteamID64] = stat
	}
	stat.Name = pl.Name
	stat.Team = rosterTeam(pl.Team)
	return stat
}

// register wires the demo event handlers onto the accumulator. p is captured so
// handlers can read game state (tick, tickrate, current participants).
func (a *rosterAccumulator) register(p demoinfocs.Parser) {
	p.RegisterNetMessageHandler(func(info *msg.CSVCMsg_ServerInfo) {
		if name := info.GetMapName(); name != "" {
			a.mapName = name
		}
	})

	p.RegisterEventHandler(func(events.MatchStart) { a.reset() })

	p.RegisterEventHandler(func(events.RoundStart) {
		a.curParticipants = map[uint64]string{}
		for _, pl := range p.GameState().Participants().Playing() {
			if pl != nil && pl.SteamID64 != 0 {
				a.curParticipants[pl.SteamID64] = rosterTeam(pl.Team)
			}
		}
		a.curKills = a.curKills[:0]
		a.curKillsByID = map[uint64]int{}
	})

	p.RegisterEventHandler(func(e events.Kill) {
		if killer := a.observe(e.Killer); killer != nil {
			killer.Kills++
			if e.IsHeadshot {
				killer.Headshots++
			}
			a.curKillsByID[e.Killer.SteamID64]++
		}
		if victim := a.observe(e.Victim); victim != nil {
			victim.Deaths++
		}
		if assister := a.observe(e.Assister); assister != nil {
			assister.Assists++
		}
		if e.Killer != nil && e.Victim != nil && e.Killer.SteamID64 != 0 && e.Victim.SteamID64 != 0 {
			rk := roundKill{
				killer:     e.Killer.SteamID64,
				victim:     e.Victim.SteamID64,
				killerTeam: teamLabel(e.Killer.Team),
				victimTeam: teamLabel(e.Victim.Team),
				tick:       p.GameState().IngameTick(),
			}
			if e.Assister != nil {
				rk.assister = e.Assister.SteamID64
			}
			a.curKills = append(a.curKills, rk)
		}
	})

	p.RegisterEventHandler(func(e events.PlayerHurt) {
		if e.Attacker == nil || e.Player == nil || e.Attacker.SteamID64 == 0 {
			return
		}
		if e.Attacker.SteamID64 == e.Player.SteamID64 || e.Attacker.Team == e.Player.Team {
			return // self-damage and team damage do not count toward ADR
		}
		a.damage[e.Attacker.SteamID64] += e.HealthDamageTaken
	})

	p.RegisterEventHandler(func(e events.RoundMVPAnnouncement) {
		if e.Player != nil && e.Player.SteamID64 != 0 {
			a.mvps[e.Player.SteamID64]++
		}
	})

	p.RegisterEventHandler(func(events.RoundEnd) {
		a.rounds++
		if len(a.curParticipants) == 0 {
			// No RoundStart snapshot (POV/odd demo): fall back to who is playing now.
			for _, pl := range p.GameState().Participants().Playing() {
				if pl != nil && pl.SteamID64 != 0 {
					a.curParticipants[pl.SteamID64] = rosterTeam(pl.Team)
				}
			}
		}
		tradeWindowTicks := int(p.TickRate() * tradeWindowSeconds)
		for id := range kastCreditedPlayers(a.curParticipants, a.curKills, tradeWindowTicks) {
			a.kastRounds[id]++
		}
		for id, n := range a.curKillsByID {
			if a.roundsByKillCount[id] == nil {
				a.roundsByKillCount[id] = map[int]int{}
			}
			a.roundsByKillCount[id][n]++
		}
	})
}

// finalize computes the derived columns (HS%, ADR, KAST, rating) onto every
// player and returns the scoreboard sorted by kills desc then name asc.
func (a *rosterAccumulator) finalize() []PlayerStat {
	for id, ps := range a.players {
		ps.MVPs = a.mvps[id]
		ps.Rounds = a.rounds
		if ps.Kills > 0 {
			ps.HSPct = round1(100 * float64(ps.Headshots) / float64(ps.Kills))
		}
		if a.rounds > 0 {
			ps.ADR = round1(float64(a.damage[id]) / float64(a.rounds))
			ps.KAST = round1(100 * float64(a.kastRounds[id]) / float64(a.rounds))
		}
		ps.Rating = round2(hltv1Rating(ps.Kills, ps.Deaths, a.rounds, a.roundsByKillCount[id]))
		for n, count := range a.roundsByKillCount[id] {
			switch {
			case n == 2:
				ps.Rounds2K = count
			case n == 3:
				ps.Rounds3K = count
			case n == 4:
				ps.Rounds4K = count
			case n >= 5:
				ps.Rounds5K += count
			}
		}
	}
	return sortRoster(a.players)
}

// kastCreditedPlayers returns the set of steamids that earned KAST in a round: a
// kill, an assist, survival (a known participant who did not die), or a traded
// death (the player's killer was killed by a teammate within the trade window).
func kastCreditedPlayers(participants map[uint64]string, kills []roundKill, tradeWindowTicks int) map[uint64]bool {
	killers := map[uint64]bool{}
	assisters := map[uint64]bool{}
	victims := map[uint64]bool{}
	for _, k := range kills {
		killers[k.killer] = true
		victims[k.victim] = true
		if k.assister != 0 {
			assisters[k.assister] = true
		}
	}

	traded := map[uint64]bool{}
	for _, dead := range kills {
		for _, avenge := range kills {
			if avenge.victim != dead.killer {
				continue
			}
			if avenge.tick < dead.tick || avenge.tick-dead.tick > tradeWindowTicks {
				continue
			}
			if avenge.killerTeam != "" && avenge.killerTeam == dead.victimTeam {
				traded[dead.victim] = true
			}
		}
	}

	credited := map[uint64]bool{}
	for id := range killers {
		credited[id] = true
	}
	for id := range assisters {
		credited[id] = true
	}
	for id := range traded {
		credited[id] = true
	}
	for id := range participants {
		if !victims[id] { // survived the round
			credited[id] = true
		}
	}
	return credited
}

// hltv1Rating is the published HLTV 1.0 rating: kill rating, survival rating, and
// a multi-kill-round rating, each normalised to a league average and combined.
// A player who never appeared in a round (rounds == 0) gets 0.
func hltv1Rating(kills, deaths, rounds int, roundsByKillCount map[int]int) float64 {
	if rounds <= 0 {
		return 0
	}
	r := float64(rounds)

	killRating := (float64(kills) / r) / avgKillsPerRound

	survived := r - float64(deaths)
	if survived < 0 {
		survived = 0
	}
	survivalRating := (survived / r) / avgSurvivedPerRound

	// Rounds with n kills are weighted n^2 (1, 4, 9, 16, 25) per HLTV 1.0.
	var weighted float64
	for n, count := range roundsByKillCount {
		if n >= 1 {
			weighted += float64(n*n) * float64(count)
		}
	}
	multiKillRating := (weighted / r) / avgMultiKillWeightPerR

	return (killRating + 0.7*survivalRating + multiKillRating) / 2.7
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }
func round2(x float64) float64 { return math.Round(x*100) / 100 }
