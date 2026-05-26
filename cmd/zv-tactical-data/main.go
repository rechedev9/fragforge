package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

type frame struct {
	Tick    int      `json:"tick"`
	Players []player `json:"players"`
}

type player struct {
	SteamID64 string  `json:"steamid64"`
	Name      string  `json:"name"`
	Team      string  `json:"team"`
	Alive     bool    `json:"alive"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Z         float64 `json:"z"`
	Yaw       float64 `json:"yaw"`
	Health    int     `json:"health"`
}

type kill struct {
	Tick       int    `json:"tick"`
	KillerID   string `json:"killer_steamid64"`
	KillerName string `json:"killer_name"`
	VictimID   string `json:"victim_steamid64"`
	VictimName string `json:"victim_name"`
	Weapon     string `json:"weapon"`
	Headshot   bool   `json:"headshot"`
	KillerTeam string `json:"killer_team"`
	VictimTeam string `json:"victim_team"`
}

type output struct {
	Demo     string  `json:"demo"`
	Start    int     `json:"start_tick"`
	End      int     `json:"end_tick"`
	Sample   int     `json:"sample_ticks"`
	Tickrate float64 `json:"tickrate"`
	Frames   []frame `json:"frames"`
	Kills    []kill  `json:"kills"`
}

func main() {
	var demoPath string
	var outPath string
	var startTick int
	var endTick int
	var sampleTicks int
	flag.StringVar(&demoPath, "demo", "", "path to .dem file")
	flag.StringVar(&outPath, "out", "", "output JSON path")
	flag.IntVar(&startTick, "start", 0, "first tick to sample")
	flag.IntVar(&endTick, "end", 0, "last tick to sample")
	flag.IntVar(&sampleTicks, "sample", 4, "sample interval in ticks")
	flag.Parse()

	if demoPath == "" || outPath == "" || startTick <= 0 || endTick <= startTick {
		log.Fatal("--demo, --out, --start, and --end are required")
	}
	if sampleTicks <= 0 {
		sampleTicks = 1
	}

	// #nosec G304 -- demo path is an explicit local CLI input.
	f, err := os.Open(demoPath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	p := demoinfocs.NewParser(f)
	defer p.Close()

	result := output{Demo: demoPath, Start: startTick, End: endTick, Sample: sampleTicks}

	p.RegisterEventHandler(func(e events.Kill) {
		tick := p.GameState().IngameTick()
		if tick < startTick || tick > endTick || e.Killer == nil || e.Victim == nil {
			return
		}
		result.Kills = append(result.Kills, kill{
			Tick:       tick,
			KillerID:   strconv.FormatUint(e.Killer.SteamID64, 10),
			KillerName: e.Killer.Name,
			VictimID:   strconv.FormatUint(e.Victim.SteamID64, 10),
			VictimName: e.Victim.Name,
			Weapon:     weaponName(e.Weapon),
			Headshot:   e.IsHeadshot,
			KillerTeam: teamLabel(e.Killer.Team),
			VictimTeam: teamLabel(e.Victim.Team),
		})
	})

	p.RegisterEventHandler(func(events.FrameDone) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick < startTick || tick > endTick || (tick-startTick)%sampleTicks != 0 {
			return
		}
		fr := frame{Tick: tick}
		for _, pl := range gs.Participants().All() {
			if pl == nil || pl.SteamID64 == 0 || pl.Team == common.TeamSpectators {
				continue
			}
			pos := pl.Position()
			fr.Players = append(fr.Players, player{
				SteamID64: strconv.FormatUint(pl.SteamID64, 10),
				Name:      pl.Name,
				Team:      teamLabel(pl.Team),
				Alive:     pl.IsAlive(),
				X:         pos.X,
				Y:         pos.Y,
				Z:         pos.Z,
				Yaw:       float64(pl.ViewDirectionX()),
				Health:    pl.Health(),
			})
		}
		result.Frames = append(result.Frames, fr)
	})

	if err := p.ParseToEnd(); err != nil {
		log.Fatal(err)
	}
	result.Tickrate = p.TickRate()

	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(outPath, append(b, '\n'), 0o600); err != nil {
		log.Fatal(err)
	}
}

func weaponName(w *common.Equipment) string {
	if w == nil {
		return ""
	}
	if w.OriginalString != "" {
		return w.OriginalString
	}
	return fmt.Sprint(w.Type)
}

func teamLabel(t common.Team) string {
	switch t {
	case common.TeamCounterTerrorists:
		return "CT"
	case common.TeamTerrorists:
		return "T"
	default:
		return ""
	}
}
