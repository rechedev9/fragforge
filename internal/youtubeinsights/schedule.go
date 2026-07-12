// Package youtubeinsights provides deterministic recommendations for publishing
// YouTube Shorts. It intentionally contains no network or storage code.
package youtubeinsights

import (
	"errors"
	"fmt"
	"time"
	_ "time/tzdata"
)

const MadridTimeZone = "Europe/Madrid"

// ScheduleConfig controls how many ordered baseline slots are returned per day.
type ScheduleConfig struct {
	RecommendationsPerDay int
}

// DefaultScheduleConfig returns the standard three daily publication options.
func DefaultScheduleConfig() ScheduleConfig {
	return ScheduleConfig{RecommendationsPerDay: 3}
}

// SlotRecommendation is an exact, timezone-aware baseline publication instant.
// Confidence and Score are normalized to the range [0, 1].
type SlotRecommendation struct {
	PublishAt  time.Time
	Confidence float64
	Score      float64
	Rationale  string
}

// DailyRecommendation groups the recommended slots for a Madrid calendar day.
type DailyRecommendation struct {
	Date     string
	Weekday  time.Weekday
	TimeZone string
	Slots    []SlotRecommendation
}

// BaselineHours returns a copy of the ordered local hours for a weekday.
func BaselineHours(weekday time.Weekday) []int {
	switch weekday {
	case time.Sunday:
		return []int{19, 20, 17}
	case time.Monday:
		return []int{20, 17, 18}
	case time.Tuesday:
		return []int{20, 21, 19}
	case time.Wednesday:
		return []int{19, 20, 21}
	case time.Thursday:
		return []int{19, 20, 21}
	case time.Friday:
		return []int{16, 18, 19}
	case time.Saturday:
		return []int{19, 11, 18}
	default:
		return nil
	}
}

// RecommendDaily returns deterministic baseline recommendations beginning on
// the Madrid calendar date containing from. Each PublishAt retains the correct
// CET or CEST offset.
func RecommendDaily(from time.Time, days int, cfg ScheduleConfig) ([]DailyRecommendation, error) {
	if from.IsZero() {
		return nil, errors.New("start time is required")
	}
	if days <= 0 {
		return nil, errors.New("days must be positive")
	}
	if cfg.RecommendationsPerDay < 1 || cfg.RecommendationsPerDay > 3 {
		return nil, errors.New("recommendations per day must be between 1 and 3")
	}
	location, err := time.LoadLocation(MadridTimeZone)
	if err != nil {
		return nil, fmt.Errorf("load Madrid timezone: %w", err)
	}

	localStart := from.In(location)
	startDate := time.Date(localStart.Year(), localStart.Month(), localStart.Day(), 0, 0, 0, 0, location)
	result := make([]DailyRecommendation, 0, days)
	for dayOffset := 0; len(result) < days; dayOffset++ {
		date := startDate.AddDate(0, 0, dayOffset)
		hours := BaselineHours(date.Weekday())
		slots := make([]SlotRecommendation, 0, cfg.RecommendationsPerDay)
		for rank, hour := range hours[:cfg.RecommendationsPerDay] {
			publishAt := time.Date(date.Year(), date.Month(), date.Day(), hour, 0, 0, 0, location)
			if dayOffset == 0 && publishAt.Before(from) {
				continue
			}
			slots = append(slots, SlotRecommendation{
				PublishAt:  publishAt,
				Confidence: 0.4,
				Score:      baselineScore(rank),
				Rationale:  "deterministic baseline for Shorts in Spain local time",
			})
		}
		if len(slots) == 0 {
			continue
		}
		result = append(result, DailyRecommendation{
			Date:     date.Format(time.DateOnly),
			Weekday:  date.Weekday(),
			TimeZone: MadridTimeZone,
			Slots:    slots,
		})
	}
	return result, nil
}

func baselineScore(rank int) float64 {
	switch rank {
	case 0:
		return 0.7
	case 1:
		return 0.65
	default:
		return 0.6
	}
}
