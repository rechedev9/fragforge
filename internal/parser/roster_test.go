package parser

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
)

// loadRosterDemo reads the demo the roster scan runs against, mirroring the
// parser/worker convention: TEST_DEMO_PATH overrides the testdata fixture, and
// an unavailable demo skips the test rather than failing the gate.
func loadRosterDemo(t *testing.T) []byte {
	t.Helper()
	path := os.Getenv("TEST_DEMO_PATH")
	if path == "" {
		path = filepath.Join("..", "..", "testdata", "lavked-vs-tnc-m2-nuke.dem")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no test demo at %s: %v", path, err)
	}
	return b
}

func TestRosterScansRealDemo(t *testing.T) {
	demo := loadRosterDemo(t)

	p := demoinfocs.NewParser(bytes.NewReader(demo))
	defer p.Close()

	roster, err := Roster(p)
	if err != nil {
		t.Fatalf("Roster error = %v", err)
	}
	if len(roster) == 0 {
		t.Fatal("roster is empty, want at least one player")
	}

	const maaryySteamID = "76561198148986856"
	tests := []struct {
		name  string
		check func(t *testing.T)
	}{
		{
			name: "sorted by kills desc then name asc",
			check: func(t *testing.T) {
				for i := 1; i < len(roster); i++ {
					prev, cur := roster[i-1], roster[i]
					if prev.Kills < cur.Kills {
						t.Fatalf("roster[%d].Kills=%d < roster[%d].Kills=%d, want desc", i-1, prev.Kills, i, cur.Kills)
					}
					if prev.Kills == cur.Kills && prev.Name > cur.Name {
						t.Fatalf("roster[%d].Name=%q > roster[%d].Name=%q at equal kills, want asc", i-1, prev.Name, i, cur.Name)
					}
				}
			},
		},
		{
			name: "contains the known target steamid",
			check: func(t *testing.T) {
				if findRosterPlayer(roster, maaryySteamID) == nil {
					t.Fatalf("roster missing steamid %s", maaryySteamID)
				}
			},
		},
		{
			name: "skips bots and zero steamids",
			check: func(t *testing.T) {
				for _, pl := range roster {
					if pl.SteamID64 == "" || pl.SteamID64 == "0" {
						t.Fatalf("roster includes a bot / zero steamid: %#v", pl)
					}
				}
			},
		},
		{
			name: "tallies are non-negative and at least one kill exists",
			check: func(t *testing.T) {
				totalKills := 0
				for _, pl := range roster {
					if pl.Kills < 0 || pl.Deaths < 0 || pl.Assists < 0 {
						t.Fatalf("negative tally: %#v", pl)
					}
					totalKills += pl.Kills
				}
				if totalKills == 0 {
					t.Fatal("roster has zero total kills, expected > 0 (regression)")
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, tc.check)
	}
}

func TestSortRosterOrdersByKillsDescThenNameAsc(t *testing.T) {
	tally := map[uint64]*PlayerStat{
		1: {SteamID64: "1", Name: "charlie", Kills: 10},
		2: {SteamID64: "2", Name: "alice", Kills: 20},
		3: {SteamID64: "3", Name: "bob", Kills: 20},
		4: {SteamID64: "4", Name: "dave", Kills: 5},
	}

	got := sortRoster(tally)

	wantNames := []string{"alice", "bob", "charlie", "dave"}
	if len(got) != len(wantNames) {
		t.Fatalf("len = %d, want %d", len(got), len(wantNames))
	}
	for i, want := range wantNames {
		if got[i].Name != want {
			t.Errorf("got[%d].Name = %q, want %q (full order: %+v)", i, got[i].Name, want, got)
		}
	}
}

func TestRosterTeamKeepsOnlyPlayingTeams(t *testing.T) {
	tests := []struct {
		name string
		team common.Team
		want string
	}{
		{name: "counter-terrorists", team: common.TeamCounterTerrorists, want: "CT"},
		{name: "terrorists", team: common.TeamTerrorists, want: "T"},
		{name: "spectators collapse to empty", team: common.TeamSpectators, want: ""},
		{name: "unassigned collapse to empty", team: common.TeamUnassigned, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rosterTeam(tc.team); got != tc.want {
				t.Errorf("rosterTeam(%v) = %q, want %q", tc.team, got, tc.want)
			}
		})
	}
}

func findRosterPlayer(roster []PlayerStat, steamID string) *PlayerStat {
	for i := range roster {
		if roster[i].SteamID64 == steamID {
			return &roster[i]
		}
	}
	return nil
}
