package youtubeinsights

import (
	"reflect"
	"testing"
	"time"
)

func TestRecommendDailyReturnsBaselineOrder(t *testing.T) {
	location := mustMadridLocation(t)
	from := time.Date(2026, time.July, 13, 10, 30, 0, 0, location) // Monday.

	got, err := RecommendDaily(from, 2, DefaultScheduleConfig())
	if err != nil {
		t.Fatalf("RecommendDaily() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("day count = %d, want 2", len(got))
	}
	if got[0].Date != "2026-07-13" || got[0].Weekday != time.Monday || got[0].TimeZone != MadridTimeZone {
		t.Fatalf("first day = %#v", got[0])
	}
	if hours := slotHours(got[0].Slots); !reflect.DeepEqual(hours, []int{20, 17, 18}) {
		t.Fatalf("Monday hours = %v, want [20 17 18]", hours)
	}
	if hours := slotHours(got[1].Slots); !reflect.DeepEqual(hours, []int{20, 21, 19}) {
		t.Fatalf("Tuesday hours = %v, want [20 21 19]", hours)
	}
	for _, day := range got {
		for _, slot := range day.Slots {
			if slot.Confidence != 0.4 || slot.Rationale == "" {
				t.Errorf("slot = %#v, want fixed confidence and rationale", slot)
			}
		}
	}
}

func TestRecommendDailyUsesMadridDST(t *testing.T) {
	location := mustMadridLocation(t)
	tests := []struct {
		name       string
		from       time.Time
		wantOffset int
	}{
		{name: "winter CET", from: time.Date(2026, time.January, 10, 8, 0, 0, 0, time.UTC), wantOffset: 60 * 60},
		{name: "summer CEST", from: time.Date(2026, time.July, 10, 8, 0, 0, 0, time.UTC), wantOffset: 2 * 60 * 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RecommendDaily(tt.from, 1, DefaultScheduleConfig())
			if err != nil {
				t.Fatalf("RecommendDaily() error = %v", err)
			}
			_, offset := got[0].Slots[0].PublishAt.Zone()
			if offset != tt.wantOffset {
				t.Fatalf("offset = %d, want %d", offset, tt.wantOffset)
			}
			if got[0].Slots[0].PublishAt.Location().String() != location.String() {
				t.Fatalf("location = %s, want %s", got[0].Slots[0].PublishAt.Location(), location)
			}
		})
	}
}

func TestRecommendDailyHonorsConfiguredCount(t *testing.T) {
	got, err := RecommendDaily(time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC), 1, ScheduleConfig{RecommendationsPerDay: 2})
	if err != nil {
		t.Fatalf("RecommendDaily() error = %v", err)
	}
	if len(got[0].Slots) != 2 {
		t.Fatalf("slot count = %d, want 2", len(got[0].Slots))
	}
}

func TestRecommendDailySkipsPastSlotsAndEmptyCurrentDay(t *testing.T) {
	location := mustMadridLocation(t)

	lateMonday := time.Date(2026, time.July, 13, 21, 30, 0, 0, location)
	afterHours, err := RecommendDaily(lateMonday, 1, DefaultScheduleConfig())
	if err != nil {
		t.Fatalf("RecommendDaily() error = %v", err)
	}
	if got, want := afterHours[0].Date, "2026-07-14"; got != want {
		t.Fatalf("first date = %s, want %s", got, want)
	}

	earlyMonday := time.Date(2026, time.July, 13, 17, 30, 0, 0, location)
	remaining, err := RecommendDaily(earlyMonday, 1, DefaultScheduleConfig())
	if err != nil {
		t.Fatalf("RecommendDaily() error = %v", err)
	}
	for _, slot := range remaining[0].Slots {
		if slot.PublishAt.Before(earlyMonday) {
			t.Fatalf("past slot returned: %s before %s", slot.PublishAt, earlyMonday)
		}
	}
}

func TestRecommendDailyValidation(t *testing.T) {
	tests := []struct {
		name string
		from time.Time
		days int
		cfg  ScheduleConfig
	}{
		{name: "zero start", days: 1, cfg: DefaultScheduleConfig()},
		{name: "zero days", from: time.Now(), cfg: DefaultScheduleConfig()},
		{name: "zero slots", from: time.Now(), days: 1, cfg: ScheduleConfig{}},
		{name: "too many slots", from: time.Now(), days: 1, cfg: ScheduleConfig{RecommendationsPerDay: 4}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := RecommendDaily(tt.from, tt.days, tt.cfg); err == nil {
				t.Fatal("RecommendDaily() error = nil, want validation error")
			}
		})
	}
}

func slotHours(slots []SlotRecommendation) []int {
	hours := make([]int, 0, len(slots))
	for _, slot := range slots {
		hours = append(hours, slot.PublishAt.Hour())
	}
	return hours
}

func mustMadridLocation(t *testing.T) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(MadridTimeZone)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", MadridTimeZone, err)
	}
	return location
}
