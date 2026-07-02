package parser

import (
	"context"
	"fmt"
	"sort"
	"sync"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// PlayerStat is one player's scoreboard line from a roster scan of a demo.
// Kills/Deaths/Assists are raw tallies; ADR, HSPct, KAST, and Rating are derived
// once the full match is known (see roster_stats.go). Counters are measured from
// MatchStart so warmup is excluded; if a demo has no MatchStart, everything counts.
type PlayerStat struct {
	SteamID64 string  `json:"steamid64"`
	Name      string  `json:"name"`
	Team      string  `json:"team"` // "CT" | "T" | "" (last observed)
	Kills     int     `json:"kills"`
	Deaths    int     `json:"deaths"`
	Assists   int     `json:"assists"`
	Headshots int     `json:"headshots"` // headshot kills
	MVPs      int     `json:"mvps"`      // RoundMVPAnnouncement count
	Rounds    int     `json:"rounds"`    // rounds played (denominator for ADR/KAST)
	ADR       float64 `json:"adr"`       // average damage per round (excl. team damage)
	HSPct     float64 `json:"hs_pct"`    // headshot kills / kills, 0..100
	KAST      float64 `json:"kast"`      // % of rounds with kill/assist/survive/trade, 0..100
	Rating    float64 `json:"rating"`    // HLTV 1.0 rating, ~1.0-centered

	// Multi-kill round counts, measured the same way as Rounds (from MatchStart,
	// warmup excluded). Rounds5K includes 5+ (aces and beyond, which CS2 rounds
	// cap at 5 kills anyway since a round has 5 opponents at most).
	Rounds2K int `json:"rounds_2k,omitempty"`
	Rounds3K int `json:"rounds_3k,omitempty"`
	Rounds4K int `json:"rounds_4k,omitempty"`
	Rounds5K int `json:"rounds_5k,omitempty"`
}

// MatchInfo is match-level metadata gathered in the same roster scan pass: the
// map name from the demo header and the final scoreline read the way a
// scoreboard reads it - the score of whichever team ended the match on each
// side, not tied to a specific team identity across a side swap.
type MatchInfo struct {
	Map     string `json:"map"`
	ScoreCT int    `json:"score_ct"`
	ScoreT  int    `json:"score_t"`
	Rounds  int    `json:"rounds"`
}

// RosterResult is the result of a roster scan: every player's scoreboard line
// plus the match metadata gathered in the same pass.
type RosterResult struct {
	Players []PlayerStat `json:"players"`
	Match   MatchInfo    `json:"match"`
}

// Roster does one pass over the demo and returns every human player it saw, as a
// full scoreboard line (K/D/A plus HS%, ADR, KAST, MVPs, and an HLTV 1.0 rating),
// sorted by Kills desc then Name asc. Bots and players with a zero SteamID are
// skipped. An empty demo yields an empty slice, not an error; unlike Run, Roster
// needs no target and never reports one missing.
func Roster(p demoinfocs.Parser) ([]PlayerStat, error) {
	result, err := RosterScan(p)
	if err != nil {
		return nil, err
	}
	return result.Players, nil
}

// RosterWithContext drives Roster but aborts parsing when ctx is cancelled,
// returning the context error instead of a partial roster. It mirrors
// RunWithContext: a watcher goroutine calls p.Cancel() on ctx.Done() and is
// joined before return, so Close() never races a Cancel() in flight.
func RosterWithContext(ctx context.Context, p demoinfocs.Parser) ([]PlayerStat, error) {
	result, err := RosterScanWithContext(ctx, p)
	if err != nil {
		return nil, err
	}
	return result.Players, nil
}

// RosterScan does one pass over the demo and returns both the player roster
// (see Roster) and match-level metadata (map, final scoreline, rounds
// played), gathered in the same pass.
func RosterScan(p demoinfocs.Parser) (RosterResult, error) {
	acc := newRosterAccumulator()
	acc.register(p)

	if err := parseToEnd(p); err != nil {
		return RosterResult{}, fmt.Errorf("parsing demo: %w", err)
	}

	gs := p.GameState()
	return RosterResult{
		Players: acc.finalize(),
		Match: MatchInfo{
			Map:     acc.mapName,
			ScoreCT: gs.TeamCounterTerrorists().Score(),
			ScoreT:  gs.TeamTerrorists().Score(),
			Rounds:  acc.rounds,
		},
	}, nil
}

// RosterScanWithContext drives RosterScan but aborts parsing when ctx is
// cancelled, mirroring RosterWithContext.
func RosterScanWithContext(ctx context.Context, p demoinfocs.Parser) (RosterResult, error) {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		select {
		case <-ctx.Done():
			p.Cancel()
		case <-stop:
		}
	})
	defer func() {
		close(stop)
		wg.Wait()
	}()

	result, err := RosterScan(p)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return RosterResult{}, fmt.Errorf("scan roster: %w", ctxErr)
	}
	return result, err
}

// rosterTeam keeps only the playing teams so PlayerStat.Team matches the web
// DemoPlayer union ("CT" | "T" | ""). teamLabel can return "SPEC" for
// spectators, which has no place in the roster, so it collapses to "".
func rosterTeam(t common.Team) string {
	switch label := teamLabel(t); label {
	case "CT", "T":
		return label
	default:
		return ""
	}
}

func sortRoster(tally map[uint64]*PlayerStat) []PlayerStat {
	roster := make([]PlayerStat, 0, len(tally))
	for _, stat := range tally {
		roster = append(roster, *stat)
	}
	sort.Slice(roster, func(i, j int) bool {
		if roster[i].Kills != roster[j].Kills {
			return roster[i].Kills > roster[j].Kills
		}
		return roster[i].Name < roster[j].Name
	})
	return roster
}
