package utilityaudit

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/lineups"
)

type Row struct {
	SegmentID          string     `json:"segment_id"`
	Round              int        `json:"round"`
	Map                string     `json:"map"`
	Player             string     `json:"player"`
	UtilityID          string     `json:"utility_id"`
	Type               string     `json:"type"`
	ThrowTick          int        `json:"throw_tick"`
	PopTick            int        `json:"pop_tick,omitempty"`
	ExpireTick         int        `json:"expire_tick,omitempty"`
	ThrowTimeSeconds   float64    `json:"throw_time_seconds"`
	ThrowPlace         string     `json:"throw_place,omitempty"`
	ThrowAction        string     `json:"throw_action,omitempty"`
	Stance             string     `json:"stance,omitempty"`
	Movement           string     `json:"movement,omitempty"`
	Speed2D            float64    `json:"speed_2d"`
	OnGround           bool       `json:"on_ground"`
	Walking            bool       `json:"walking"`
	Ducking            bool       `json:"ducking"`
	ThrowStateTick     int        `json:"throw_state_tick,omitempty"`
	ThrowStateSource   string     `json:"throw_state_source,omitempty"`
	ThrowPos           [3]float64 `json:"throw_pos"`
	LandingPos         [3]float64 `json:"landing_pos"`
	LandingSource      string     `json:"landing_source,omitempty"`
	Destination        string     `json:"destination,omitempty"`
	DestinationSource  string     `json:"destination_source"`
	MatchID            string     `json:"match_id,omitempty"`
	MatchConfidence    float64    `json:"match_confidence,omitempty"`
	MatchDistanceUnits float64    `json:"match_distance_units,omitempty"`
	FromArea           string     `json:"from_area,omitempty"`
	Side               string     `json:"side,omitempty"`
}

func Build(plan killplan.Plan, catalog lineups.Catalog) []Row {
	var out []Row
	for _, segment := range plan.Segments {
		for _, utility := range segment.Utility {
			row := Row{
				SegmentID:         segment.ID,
				Round:             utility.Round,
				Map:               plan.Demo.Map,
				Player:            utility.Thrower.NameInDemo,
				UtilityID:         utility.ID,
				Type:              utility.Type,
				ThrowTick:         utility.ThrowTick,
				PopTick:           utility.PopTick,
				ExpireTick:        utility.ExpireTick,
				ThrowTimeSeconds:  seconds(utility.ThrowTick, plan.Demo.Tickrate),
				ThrowPlace:        utility.ThrowPlace,
				ThrowAction:       utility.ThrowAction,
				Stance:            utility.Stance,
				Movement:          utility.Movement,
				Speed2D:           utility.Speed2D,
				OnGround:          utility.OnGround,
				Walking:           utility.Walking,
				Ducking:           utility.Ducking,
				ThrowStateTick:    utility.ThrowStateTick,
				ThrowStateSource:  utility.ThrowStateSource,
				ThrowPos:          utility.ThrowPos,
				LandingPos:        utility.LandingPos,
				LandingSource:     utility.LandingSource,
				DestinationSource: "unknown",
				FromArea:          utility.ThrowPlace,
			}
			applyDestination(&row, plan.Demo.Map, utility, catalog)
			out = append(out, row)
		}
	}
	return out
}

func WriteCSV(w io.Writer, rows []Row) error {
	cw := csv.NewWriter(w)
	header := []string{
		"segment", "round", "map", "player", "utility_id", "type",
		"throw_tick", "pop_tick", "expire_tick", "throw_time_seconds",
		"throw_place", "throw_action", "stance", "movement", "speed_2d",
		"on_ground", "walking", "ducking", "throw_state_tick", "throw_state_source",
		"throw_x", "throw_y", "throw_z",
		"landing_x", "landing_y", "landing_z", "landing_source",
		"destination", "destination_source", "match_id", "match_confidence",
		"match_distance_units", "from_area", "side",
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		record := []string{
			row.SegmentID,
			strconv.Itoa(row.Round),
			row.Map,
			row.Player,
			row.UtilityID,
			row.Type,
			strconv.Itoa(row.ThrowTick),
			optionalInt(row.PopTick),
			optionalInt(row.ExpireTick),
			float(row.ThrowTimeSeconds),
			row.ThrowPlace,
			row.ThrowAction,
			row.Stance,
			row.Movement,
			float(row.Speed2D),
			strconv.FormatBool(row.OnGround),
			strconv.FormatBool(row.Walking),
			strconv.FormatBool(row.Ducking),
			optionalInt(row.ThrowStateTick),
			row.ThrowStateSource,
			float(row.ThrowPos[0]),
			float(row.ThrowPos[1]),
			float(row.ThrowPos[2]),
			float(row.LandingPos[0]),
			float(row.LandingPos[1]),
			float(row.LandingPos[2]),
			row.LandingSource,
			row.Destination,
			row.DestinationSource,
			row.MatchID,
			float(row.MatchConfidence),
			float(row.MatchDistanceUnits),
			row.FromArea,
			row.Side,
		}
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func applyDestination(row *Row, mapName string, utility killplan.UtilityThrow, catalog lineups.Catalog) {
	if !catalog.Empty() {
		if match, ok := catalog.MatchSmoke(mapName, utility); ok {
			row.Destination = match.Destination
			row.DestinationSource = "catalog"
			row.MatchID = match.ID
			row.MatchConfidence = match.Confidence
			row.MatchDistanceUnits = match.DistanceUnits
			row.FromArea = firstNonEmpty(match.FromArea, utility.ThrowPlace)
			row.Side = match.Side
			return
		}
	}
	if utility.LineupMatch == nil || utility.LineupMatch.Destination == "" {
		return
	}
	match := utility.LineupMatch
	row.Destination = match.Destination
	row.MatchID = match.ID
	row.MatchConfidence = match.Confidence
	row.MatchDistanceUnits = match.DistanceUnits
	row.FromArea = firstNonEmpty(match.FromArea, utility.ThrowPlace)
	row.Side = match.Side
	if strings.HasPrefix(match.ID, "auto-") {
		row.DestinationSource = "auto"
		return
	}
	row.DestinationSource = "plan"
}

func seconds(tick, tickrate int) float64 {
	if tick <= 0 || tickrate <= 0 {
		return 0
	}
	return math.Round((float64(tick)/float64(tickrate))*1000) / 1000
}

func float(v float64) string {
	if v == 0 {
		return "0"
	}
	return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
}

func optionalInt(v int) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(v)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func ValidateFormat(format string) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "csv", "json":
		return nil
	default:
		return fmt.Errorf("format must be csv or json")
	}
}
