package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"
)

type playerStats struct {
	SteamID64 uint64
	Name      string
	Team      string
	Kills     int
	Deaths    int
}

func main() {
	var demoPath string
	var contains string
	flag.StringVar(&demoPath, "demo", "", "path to .dem file")
	flag.StringVar(&contains, "contains", "", "case-insensitive name filter")
	flag.Parse()

	if demoPath == "" {
		log.Fatal("--demo is required")
	}

	if err := run(demoPath, contains); err != nil {
		log.Fatal(err)
	}
}

func run(demoPath, contains string) error {
	// #nosec G304 -- demo path is an explicit local CLI input.
	f, err := os.Open(demoPath)
	if err != nil {
		return fmt.Errorf("open demo: %w", err)
	}
	defer f.Close()

	p := demoinfocs.NewParser(f)
	defer p.Close()

	players := map[uint64]*playerStats{}
	var mapName string
	var maxTick int

	p.RegisterNetMessageHandler(func(info *msg.CSVCMsg_ServerInfo) {
		if name := info.GetMapName(); name != "" {
			mapName = name
		}
	})

	p.RegisterEventHandler(func(e events.Kill) {
		tick := p.GameState().IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		if e.Killer != nil {
			stats := ensurePlayer(players, e.Killer)
			stats.Kills++
		}
		if e.Victim != nil {
			stats := ensurePlayer(players, e.Victim)
			stats.Deaths++
		}
	})

	if err := p.ParseToEnd(); err != nil {
		return fmt.Errorf("parse demo: %w", err)
	}

	for _, pl := range p.GameState().Participants().All() {
		ensurePlayer(players, pl)
	}

	rows := make([]playerStats, 0, len(players))
	filter := strings.ToLower(contains)
	for _, stats := range players {
		if stats.SteamID64 == 0 {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(stats.Name), filter) {
			continue
		}
		rows = append(rows, *stats)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kills != rows[j].Kills {
			return rows[i].Kills > rows[j].Kills
		}
		return rows[i].Name < rows[j].Name
	})

	fmt.Printf("demo\t%s\n", demoPath)
	if mapName != "" {
		fmt.Printf("map\t%s\n", mapName)
	}
	if tickrate := int(p.TickRate()); tickrate > 0 {
		fmt.Printf("tickrate\t%d\n", tickrate)
		fmt.Printf("duration_seconds\t%.2f\n", float64(maxTick)/float64(tickrate))
	}
	fmt.Println("steamid64\tname\tteam\tkills\tdeaths")
	for _, row := range rows {
		fmt.Printf("%s\t%s\t%s\t%d\t%d\n",
			strconv.FormatUint(row.SteamID64, 10),
			row.Name,
			row.Team,
			row.Kills,
			row.Deaths,
		)
	}
	return nil
}

func ensurePlayer(players map[uint64]*playerStats, pl *common.Player) *playerStats {
	stats, ok := players[pl.SteamID64]
	if !ok {
		stats = &playerStats{SteamID64: pl.SteamID64}
		players[pl.SteamID64] = stats
	}
	if pl.Name != "" {
		stats.Name = pl.Name
	}
	if team := teamLabel(pl.Team); team != "" {
		stats.Team = team
	}
	return stats
}

func teamLabel(t common.Team) string {
	switch t {
	case common.TeamCounterTerrorists:
		return "CT"
	case common.TeamTerrorists:
		return "T"
	case common.TeamSpectators:
		return "SPEC"
	default:
		return ""
	}
}
