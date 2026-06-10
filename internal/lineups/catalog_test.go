package lineups

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rechedev9/fragforge/internal/killplan"
)

func TestCatalogMatchesSmokeByLandingAndThrow(t *testing.T) {
	dir := t.TempDir()
	writeCatalog(t, dir, `{
  "map": "de_ancient",
  "smokes": [
    {
      "id": "ancient_t_spawn_to_ct",
      "destination": "CT",
      "from_area": "T Spawn",
      "side": "T",
      "landing": [100, 200, 0],
      "landing_radius": 96,
      "throw": [10, 20, 0],
      "throw_radius": 64
    }
  ]
}`)

	catalog, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error = %v", err)
	}
	match, ok := catalog.MatchSmoke("de_ancient", killplan.UtilityThrow{
		Type:       "smokegrenade",
		ThrowPos:   [3]float64{20, 25, 0},
		LandingPos: [3]float64{120, 210, 0},
	})
	if !ok {
		t.Fatal("MatchSmoke ok = false, want true")
	}
	if match.ID != "ancient_t_spawn_to_ct" || match.Destination != "CT" || match.FromArea != "T Spawn" || match.Side != "T" {
		t.Fatalf("match = %#v", match)
	}
	if match.Confidence <= 0 || match.DistanceUnits <= 0 {
		t.Fatalf("match confidence/distance = %#v", match)
	}
}

func TestCatalogDoesNotInventDestinationOutsideRadius(t *testing.T) {
	dir := t.TempDir()
	writeCatalog(t, dir, `{
  "map": "de_ancient",
  "smokes": [
    {
      "id": "ancient_a_main",
      "destination": "A Main",
      "landing": [0, 0, 0],
      "landing_radius": 32
    }
  ]
}`)

	catalog, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error = %v", err)
	}
	if _, ok := catalog.MatchSmoke("de_ancient", killplan.UtilityThrow{
		Type:       "smokegrenade",
		LandingPos: [3]float64{200, 0, 0},
	}); ok {
		t.Fatal("MatchSmoke ok = true, want false")
	}
}

func TestCatalogDoesNotMatchNonSmokeUtility(t *testing.T) {
	dir := t.TempDir()
	writeCatalog(t, dir, `{
  "map": "de_inferno",
  "smokes": [
    {
      "id": "inferno_t_ramp",
      "destination": "T ramp",
      "landing": [100, 200, 0],
      "landing_radius": 96
    }
  ]
}`)

	catalog, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir error = %v", err)
	}
	if _, ok := catalog.MatchSmoke("de_inferno", killplan.UtilityThrow{
		Type:       "flashbang",
		LandingPos: [3]float64{100, 200, 0},
	}); ok {
		t.Fatal("MatchSmoke ok = true for flashbang, want false")
	}
}

func writeCatalog(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "de_ancient.smokes.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
