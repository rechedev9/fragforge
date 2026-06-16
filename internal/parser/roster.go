package parser

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

// PlayerStat is one player's tally from a roster scan of a demo.
type PlayerStat struct {
	SteamID64 string `json:"steamid64"`
	Name      string `json:"name"`
	Team      string `json:"team"` // "CT" | "T" | "" (last observed)
	Kills     int    `json:"kills"`
	Deaths    int    `json:"deaths"`
	Assists   int    `json:"assists"`
}

// Roster does one pass over the demo and returns every human player it saw,
// with kill/death/assist tallies, sorted by Kills desc then Name asc. Bots and
// players with a zero SteamID are skipped. An empty demo yields an empty slice,
// not an error; unlike Run, Roster needs no target and never reports one missing.
func Roster(p demoinfocs.Parser) ([]PlayerStat, error) {
	tally := map[uint64]*PlayerStat{}

	observe := func(pl *common.Player) *PlayerStat {
		if pl == nil || pl.SteamID64 == 0 {
			return nil
		}
		stat, ok := tally[pl.SteamID64]
		if !ok {
			stat = &PlayerStat{SteamID64: strconv.FormatUint(pl.SteamID64, 10)}
			tally[pl.SteamID64] = stat
		}
		stat.Name = pl.Name
		stat.Team = rosterTeam(pl.Team)
		return stat
	}

	p.RegisterEventHandler(func(e events.Kill) {
		if killer := observe(e.Killer); killer != nil {
			killer.Kills++
		}
		if victim := observe(e.Victim); victim != nil {
			victim.Deaths++
		}
		if assister := observe(e.Assister); assister != nil {
			assister.Assists++
		}
	})

	if err := parseToEnd(p); err != nil {
		return nil, fmt.Errorf("parsing demo: %w", err)
	}

	return sortRoster(tally), nil
}

// RosterWithContext drives Roster but aborts parsing when ctx is cancelled,
// returning the context error instead of a partial roster. It mirrors
// RunWithContext: a watcher goroutine calls p.Cancel() on ctx.Done() and is
// joined before return, so Close() never races a Cancel() in flight.
func RosterWithContext(ctx context.Context, p demoinfocs.Parser) ([]PlayerStat, error) {
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

	roster, err := Roster(p)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("scan roster: %w", ctxErr)
	}
	return roster, err
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
