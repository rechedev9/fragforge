package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rechedev9/fragforge/internal/faceit"
)

func TestParseFaceitIndexOptionsDefaultsToCurrentYearToDate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 22, 14, 30, 0, 0, time.FixedZone("CEST", 2*60*60))
	opts, err := parseFaceitIndexOptions([]string{
		"--profile", "https://www.faceit.com/es/players/m0NESY",
		"--out", "data/faceit/m0nesy-2026.json",
		"--format", "json",
	}, now)
	if err != nil {
		t.Fatalf("parse options: %v", err)
	}
	if got, want := opts.From, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("from = %s, want %s", got, want)
	}
	if got, want := opts.To, time.Date(2026, time.July, 22, 23, 59, 59, int(time.Second-time.Millisecond), time.UTC); !got.Equal(want) {
		t.Errorf("to = %s, want %s", got, want)
	}
	if opts.Format != "json" {
		t.Errorf("format = %q, want json", opts.Format)
	}
}

func TestExecuteFaceitIndexPersistsManifestAndReturnsJSON(t *testing.T) {
	t.Parallel()
	outPath := filepath.Join(t.TempDir(), "m0nesy-2026.json")
	index := faceit.Index{
		SchemaVersion: faceit.SchemaVersion,
		GeneratedAt:   time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC),
		Player:        faceit.Player{ID: "player-1", Nickname: "m0NESY", ProfileURL: "https://www.faceit.com/en/players/m0NESY"},
		Range: faceit.DateRange{
			From: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2026, time.July, 22, 23, 59, 59, 0, time.UTC),
		},
		Summary:           faceit.Summary{Matches: 1, DemoAvailable: 1, StatsEnriched: 1},
		HighlightMatchIDs: []string{"match-1"},
		Matches: []faceit.Match{{
			ID:               "match-1",
			RoomURL:          "https://www.faceit.com/en/cs2/room/match-1",
			FinishedAt:       time.Date(2026, time.July, 20, 12, 0, 0, 0, time.UTC),
			Stats:            &faceit.MatchStats{Map: "de_mirage", Kills: 30, Deaths: 15, ADR: 110},
			DemoLookupStatus: "available",
			DemoAvailable:    true,
			DemoResourceURLs: []string{"https://demos.faceit.example/match-1.dem.zst"},
		}},
	}
	indexer := faceitIndexerFunc(func(_ context.Context, request faceit.IndexRequest) (faceit.Index, error) {
		if request.Profile != "m0NESY" {
			t.Errorf("profile = %q, want m0NESY", request.Profile)
		}
		return index, nil
	})
	opts := faceitIndexOptions{
		Profile: "m0NESY",
		OutPath: outPath,
		From:    index.Range.From,
		To:      index.Range.To,
		Format:  "json",
	}
	var stdout, stderr strings.Builder
	code := executeFaceitIndex(context.Background(), opts, indexer, &stdout, &stderr)
	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	var result faceitIndexResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout.String())
	}
	if !result.OK || !result.Executed || result.Index.Summary.Matches != 1 {
		t.Fatalf("result = %#v, want successful complete index", result)
	}
	body, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var persisted faceit.Index
	if err := json.Unmarshal(body, &persisted); err != nil {
		t.Fatalf("decode persisted index: %v", err)
	}
	if persisted.SchemaVersion != faceit.SchemaVersion || len(persisted.Matches) != 1 {
		t.Fatalf("persisted = %#v, want complete index", persisted)
	}
}

func TestRunFaceitValidatesArgumentsBeforeAPIConfiguration(t *testing.T) {
	t.Setenv("FACEIT_API_KEY", "")
	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "faceit", "index", "--profile", "m0NESY", "--format", "json"}, &stdout, &stderr, nil, &fakeRunner{})
	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("decode stdout: %v; stdout=%q stderr=%q", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(result["error"].(string), "missing required flag --out") {
		t.Fatalf("result error = %q, want missing --out", result["error"])
	}
}

func TestRunFaceitHelp(t *testing.T) {
	t.Parallel()
	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "faceit", "index", "--help"}, &stdout, &stderr, nil, &fakeRunner{})
	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if !strings.Contains(stdout.String(), "--profile") || !strings.Contains(stdout.String(), "FACEIT_API_KEY") {
		t.Fatalf("stdout = %q, want FACEIT index help", stdout.String())
	}
}

type faceitIndexerFunc func(context.Context, faceit.IndexRequest) (faceit.Index, error)

func (fn faceitIndexerFunc) Index(ctx context.Context, request faceit.IndexRequest) (faceit.Index, error) {
	return fn(ctx, request)
}
