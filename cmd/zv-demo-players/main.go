package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"

	"github.com/rechedev9/fragforge/internal/pathguard"
	"github.com/rechedev9/fragforge/internal/storage"
)

const usage = `usage: zv-demo-players --demo <match.dem> [--contains <name>] [--out <players.json>] [--format text|json]
`

type playerStats struct {
	SteamID64 uint64 `json:"steamid64,string"`
	Name      string `json:"name"`
	Team      string `json:"team"`
	Kills     int    `json:"kills"`
	Deaths    int    `json:"deaths"`
}

type rosterResult struct {
	SchemaVersion   string        `json:"schema_version"`
	Demo            string        `json:"demo"`
	Map             string        `json:"map,omitempty"`
	Tickrate        int           `json:"tickrate,omitempty"`
	DurationSeconds float64       `json:"duration_seconds,omitempty"`
	Players         []playerStats `json:"players"`
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		fmt.Fprint(stdout, usage)
		return 0
	}
	fs := flag.NewFlagSet("zv-demo-players", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	demoPath := fs.String("demo", "", "path to .dem file")
	contains := fs.String("contains", "", "case-insensitive name filter")
	outPath := fs.String("out", "", "optional JSON roster artifact")
	format := fs.String("format", "text", "text or json")
	if err := fs.Parse(args); err != nil {
		return writeCommandError(args, stdout, stderr, err, true)
	}
	if fs.NArg() != 0 {
		return writeCommandError(args, stdout, stderr, fmt.Errorf("unexpected positional arg %q", fs.Arg(0)), true)
	}
	if strings.TrimSpace(*demoPath) == "" {
		return writeCommandError(args, stdout, stderr, fmt.Errorf("--demo is required"), true)
	}
	if *format != "text" && *format != "json" {
		return writeCommandError(args, stdout, stderr, fmt.Errorf("unsupported format %q", *format), true)
	}
	if strings.TrimSpace(*outPath) != "" {
		if err := pathguard.RejectOutputAliases(*outPath, pathguard.Input{Flag: "--demo", Path: *demoPath}); err != nil {
			return writeCommandError(args, stdout, stderr, err, true)
		}
	}
	result, err := scanDemo(*demoPath, *contains)
	if err != nil {
		return writeCommandError(args, stdout, stderr, err, false)
	}
	if strings.TrimSpace(*outPath) != "" {
		if err := writeRosterJSON(*outPath, result); err != nil {
			return writeCommandError(args, stdout, stderr, fmt.Errorf("write roster: %w", err), false)
		}
	}
	if *format == "json" {
		if err := writeRosterJSONOutput(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: write roster JSON: %v\n", err)
			return 1
		}
		return 0
	}
	writeRosterText(stdout, result)
	if strings.TrimSpace(*outPath) != "" {
		absOut, _ := filepath.Abs(*outPath)
		fmt.Fprintf(stdout, "players_json\t%s\n", absOut)
	}
	return 0
}

func scanDemo(demoPath, contains string) (rosterResult, error) {
	// #nosec G304 -- demo path is an explicit local CLI input.
	f, err := os.Open(demoPath)
	if err != nil {
		return rosterResult{}, fmt.Errorf("open demo: %w", err)
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
			ensurePlayer(players, e.Killer).Kills++
		}
		if e.Victim != nil {
			ensurePlayer(players, e.Victim).Deaths++
		}
	})
	if err := p.ParseToEnd(); err != nil && !errors.Is(err, demoinfocs.ErrUnexpectedEndOfDemo) {
		return rosterResult{}, fmt.Errorf("parse demo: %w", err)
	}
	for _, player := range p.GameState().Participants().All() {
		ensurePlayer(players, player)
	}

	rows := make([]playerStats, 0, len(players))
	filter := strings.ToLower(contains)
	for _, stats := range players {
		if stats.SteamID64 == 0 || (filter != "" && !strings.Contains(strings.ToLower(stats.Name), filter)) {
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
	tickrate := int(p.TickRate())
	result := rosterResult{
		SchemaVersion: "1.0",
		Demo:          demoPath,
		Map:           mapName,
		Tickrate:      tickrate,
		Players:       rows,
	}
	if tickrate > 0 {
		result.DurationSeconds = float64(maxTick) / float64(tickrate)
	}
	return result, nil
}

func writeRosterText(w io.Writer, result rosterResult) {
	fmt.Fprintf(w, "demo\t%s\n", result.Demo)
	if result.Map != "" {
		fmt.Fprintf(w, "map\t%s\n", result.Map)
	}
	if result.Tickrate > 0 {
		fmt.Fprintf(w, "tickrate\t%d\n", result.Tickrate)
		fmt.Fprintf(w, "duration_seconds\t%.2f\n", result.DurationSeconds)
	}
	fmt.Fprintln(w, "steamid64\tname\tteam\tkills\tdeaths")
	for _, row := range result.Players {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\n",
			strconv.FormatUint(row.SteamID64, 10), row.Name, row.Team, row.Kills, row.Deaths)
	}
}

func writeRosterJSON(path string, result rosterResult) error {
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	store, err := storage.NewLocal(filepath.Dir(abs))
	if err != nil {
		return err
	}
	return store.Put(filepath.Base(abs), bytes.NewReader(body))
}

func writeRosterJSONOutput(w io.Writer, result any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(result)
}

func writeCommandError(args []string, stdout, stderr io.Writer, err error, showUsage bool) int {
	code := 1
	if showUsage {
		code = 2
	}
	if jsonRequested(args) {
		if writeErr := writeRosterJSONOutput(stdout, map[string]any{"ok": false, "error": err.Error()}); writeErr != nil {
			fmt.Fprintf(stderr, "error: write JSON error: %v\n", writeErr)
			return 1
		}
		return code
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	if showUsage {
		fmt.Fprint(stderr, usage)
	}
	return code
}

func jsonRequested(args []string) bool {
	for i, arg := range args {
		if arg == "--format=json" || (arg == "--format" && i+1 < len(args) && args[i+1] == "json") {
			return true
		}
	}
	return false
}

func ensurePlayer(players map[uint64]*playerStats, player *common.Player) *playerStats {
	stats, ok := players[player.SteamID64]
	if !ok {
		stats = &playerStats{SteamID64: player.SteamID64}
		players[player.SteamID64] = stats
	}
	if player.Name != "" {
		stats.Name = player.Name
	}
	if team := teamLabel(player.Team); team != "" {
		stats.Team = team
	}
	return stats
}

func teamLabel(team common.Team) string {
	switch team {
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
