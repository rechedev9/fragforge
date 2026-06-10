package lineups

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/rechedev9/fragforge/internal/killplan"
)

const smokeGrenadeType = "smokegrenade"

type Catalog struct {
	byMap map[string][]SmokeLineup
}

type catalogFile struct {
	Map    string        `json:"map"`
	Smokes []SmokeLineup `json:"smokes"`
}

type SmokeLineup struct {
	ID            string     `json:"id"`
	Map           string     `json:"map,omitempty"`
	Destination   string     `json:"destination"`
	FromArea      string     `json:"from_area,omitempty"`
	Side          string     `json:"side,omitempty"`
	Landing       [3]float64 `json:"landing"`
	LandingRadius float64    `json:"landing_radius"`
	Throw         [3]float64 `json:"throw,omitempty"`
	ThrowRadius   float64    `json:"throw_radius,omitempty"`
}

func LoadDir(dir string) (Catalog, error) {
	catalog := Catalog{byMap: map[string][]SmokeLineup{}}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return catalog, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Catalog{}, fmt.Errorf("read lineup catalog: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		if err := catalog.loadFile(filepath.Join(dir, entry.Name())); err != nil {
			return Catalog{}, err
		}
	}
	return catalog, nil
}

func (c *Catalog) loadFile(path string) error {
	// #nosec G304 -- lineup catalog files are loaded from a configured local catalog directory.
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read lineup file %s: %w", path, err)
	}
	var file catalogFile
	if err := json.Unmarshal(b, &file); err != nil {
		return fmt.Errorf("parse lineup file %s: %w", path, err)
	}
	mapName := normalizeMap(file.Map)
	for i, smoke := range file.Smokes {
		if smoke.ID == "" {
			return fmt.Errorf("lineup file %s smoke[%d]: id is required", path, i)
		}
		if smoke.Destination == "" {
			return fmt.Errorf("lineup file %s smoke[%d]: destination is required", path, i)
		}
		if smoke.LandingRadius <= 0 {
			return fmt.Errorf("lineup file %s smoke[%d]: landing_radius must be > 0", path, i)
		}
		if smoke.Map == "" {
			smoke.Map = mapName
		}
		smoke.Map = normalizeMap(smoke.Map)
		if smoke.Map == "" {
			return fmt.Errorf("lineup file %s smoke[%d]: map is required", path, i)
		}
		c.byMap[smoke.Map] = append(c.byMap[smoke.Map], smoke)
	}
	return nil
}

func (c Catalog) MatchSmoke(mapName string, smoke killplan.UtilityThrow) (killplan.LineupMatch, bool) {
	if smoke.Type != smokeGrenadeType {
		return killplan.LineupMatch{}, false
	}
	lineups := c.byMap[normalizeMap(mapName)]
	if len(lineups) == 0 {
		return killplan.LineupMatch{}, false
	}
	var best SmokeLineup
	bestLanding := math.MaxFloat64
	bestThrow := math.MaxFloat64
	for _, candidate := range lineups {
		landingDistance := distance3(smoke.LandingPos, candidate.Landing)
		if landingDistance > candidate.LandingRadius {
			continue
		}
		throwDistance := 0.0
		if candidate.ThrowRadius > 0 {
			throwDistance = distance3(smoke.ThrowPos, candidate.Throw)
			if throwDistance > candidate.ThrowRadius {
				continue
			}
		}
		if landingDistance < bestLanding {
			best = candidate
			bestLanding = landingDistance
			bestThrow = throwDistance
		}
	}
	if best.ID == "" {
		return killplan.LineupMatch{}, false
	}
	confidence := 1 - bestLanding/best.LandingRadius
	if confidence < 0 {
		confidence = 0
	}
	if best.ThrowRadius > 0 {
		throwConfidence := 1 - bestThrow/best.ThrowRadius
		if throwConfidence < 0 {
			throwConfidence = 0
		}
		confidence = (confidence + throwConfidence) / 2
	}
	return killplan.LineupMatch{
		ID:            best.ID,
		Destination:   best.Destination,
		FromArea:      best.FromArea,
		Side:          best.Side,
		Confidence:    math.Round(confidence*1000) / 1000,
		DistanceUnits: math.Round(bestLanding*1000) / 1000,
	}, true
}

func (c Catalog) Empty() bool {
	return len(c.byMap) == 0
}

func normalizeMap(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func distance3(a, b [3]float64) float64 {
	dx := a[0] - b[0]
	dy := a[1] - b[1]
	dz := a[2] - b[2]
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
