package utilityaudit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/lineups"
)

func TestBuildMarksAutoDestinationSeparately(t *testing.T) {
	plan := killplan.Plan{
		Demo: killplan.Demo{Map: "de_inferno", Tickrate: 64},
		Segments: []killplan.Segment{{
			ID:    "seg-001",
			Round: 2,
			Utility: []killplan.UtilityThrow{{
				ID:            "smoke-001",
				Type:          "smokegrenade",
				Round:         2,
				ThrowTick:     640,
				ThrowPlace:    "CTSpawn",
				ThrowAction:   "jumpthrow",
				LandingSource: "smoke_start",
				LandingPos:    [3]float64{145, 1012, 85},
				LineupMatch: &killplan.LineupMatch{
					ID:          "auto-smoke-t-ramp",
					Destination: "T ramp",
					FromArea:    "CTSpawn",
				},
			}},
		}},
	}

	rows := Build(plan, lineups.Catalog{})
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if rows[0].Destination != "T ramp" || rows[0].DestinationSource != "auto" {
		t.Fatalf("destination = %q source = %q, want auto T ramp", rows[0].Destination, rows[0].DestinationSource)
	}
	if rows[0].ThrowTimeSeconds != 10 {
		t.Fatalf("throw time = %f, want 10", rows[0].ThrowTimeSeconds)
	}
}

func TestBuildMarksPlanDestinationWhenNotAuto(t *testing.T) {
	plan := killplan.Plan{
		Demo: killplan.Demo{Map: "de_inferno", Tickrate: 64},
		Segments: []killplan.Segment{{
			ID:    "seg-001",
			Round: 2,
			Utility: []killplan.UtilityThrow{{
				ID:        "smoke-001",
				Type:      "smokegrenade",
				Round:     2,
				ThrowTick: 640,
				LineupMatch: &killplan.LineupMatch{
					ID:          "manual-t-ramp",
					Destination: "T ramp",
				},
			}},
		}},
	}

	rows := Build(plan, lineups.Catalog{})
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if rows[0].Destination != "T ramp" || rows[0].DestinationSource != "plan" {
		t.Fatalf("destination = %q source = %q, want plan T ramp", rows[0].Destination, rows[0].DestinationSource)
	}
}

func TestBuildLeavesDestinationUnknownWithoutMatch(t *testing.T) {
	plan := killplan.Plan{
		Demo: killplan.Demo{Map: "de_inferno", Tickrate: 64},
		Segments: []killplan.Segment{{
			ID:    "seg-001",
			Round: 2,
			Utility: []killplan.UtilityThrow{{
				ID:        "smoke-001",
				Type:      "smokegrenade",
				Round:     2,
				ThrowTick: 640,
			}},
		}},
	}

	rows := Build(plan, lineups.Catalog{})
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if rows[0].Destination != "" || rows[0].DestinationSource != "unknown" {
		t.Fatalf("destination = %q source = %q, want unknown empty", rows[0].Destination, rows[0].DestinationSource)
	}
}

func TestBuildCatalogMatchOverridesPlanLineup(t *testing.T) {
	dir := t.TempDir()
	body := `{"map":"de_inferno","smokes":[{"id":"cat-t-ramp","destination":"Catalog T ramp","from_area":"CTSpawn","side":"CT","landing":[145,1012,85],"landing_radius":100}]}`
	if err := os.WriteFile(filepath.Join(dir, "de_inferno.smokes.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog, err := lineups.LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error = %v", err)
	}

	plan := killplan.Plan{
		Demo: killplan.Demo{Map: "de_inferno", Tickrate: 64},
		Segments: []killplan.Segment{{
			ID:    "seg-001",
			Round: 2,
			Utility: []killplan.UtilityThrow{{
				ID:         "smoke-001",
				Type:       "smokegrenade",
				Round:      2,
				ThrowTick:  640,
				LandingPos: [3]float64{145, 1012, 85},
				LineupMatch: &killplan.LineupMatch{
					ID:          "manual-something",
					Destination: "Plan destination",
				},
			}},
		}},
	}

	rows := Build(plan, catalog)
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if rows[0].DestinationSource != "catalog" {
		t.Fatalf("source = %q, want catalog (catalog match takes priority over plan lineup)", rows[0].DestinationSource)
	}
	if rows[0].MatchID != "cat-t-ramp" || rows[0].Destination != "Catalog T ramp" {
		t.Fatalf("match = %q dest = %q, want catalog entry", rows[0].MatchID, rows[0].Destination)
	}
}

func TestWriteCSVIncludesDeterministicFields(t *testing.T) {
	var sb strings.Builder
	err := WriteCSV(&sb, []Row{{
		SegmentID:         "seg-001",
		Round:             2,
		Map:               "de_inferno",
		Player:            "iM",
		UtilityID:         "smoke-001",
		Type:              "smokegrenade",
		ThrowTick:         640,
		ThrowAction:       "jumpthrow",
		OnGround:          false,
		ThrowStateTick:    642,
		ThrowStateSource:  "projectile_throw",
		LandingSource:     "smoke_start",
		Destination:       "T ramp",
		DestinationSource: "catalog",
	}})
	if err != nil {
		t.Fatalf("WriteCSV: %v", err)
	}
	got := sb.String()
	for _, want := range []string{"landing_source", "destination_source", "smoke_start", "catalog", "projectile_throw"} {
		if !strings.Contains(got, want) {
			t.Fatalf("csv %q missing %q", got, want)
		}
	}
}
