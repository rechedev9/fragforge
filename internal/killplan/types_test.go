package killplan

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSchemaVersionConstant(t *testing.T) {
	if SchemaVersion != "1.1" {
		t.Errorf("SchemaVersion = %q, want %q", SchemaVersion, "1.1")
	}
}

func TestPlanMarshalIncludesSchemaVersion(t *testing.T) {
	p := Plan{}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	if !strings.Contains(string(b), `"schema_version":"1.1"`) {
		t.Errorf("Marshaled plan missing schema_version=1.1: %s", string(b))
	}
}

func TestNewPlanSetsSchemaVersionAndTimestamp(t *testing.T) {
	before := time.Now().UTC()
	p := NewPlan()
	after := time.Now().UTC()

	if p.SchemaVersion != "1.1" {
		t.Errorf("SchemaVersion = %q, want %q", p.SchemaVersion, "1.1")
	}
	if p.GeneratedAt.Before(before) || p.GeneratedAt.After(after) {
		t.Errorf("GeneratedAt = %v, expected between %v and %v", p.GeneratedAt, before, after)
	}
}

func TestSteamIDSerializesAsString(t *testing.T) {
	// SteamID64 values exceed 2^53 — they must be JSON strings, not numbers,
	// or JS clients will silently truncate.
	p := Plan{
		Target: Target{SteamID64: "76561198000000000"},
		Segments: []Segment{
			{
				Kills: []Kill{
					{Victim: Player{SteamID64: "76561198000000001"}},
				},
			},
		},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}
	out := string(b)
	if !strings.Contains(out, `"steamid64":"76561198000000000"`) {
		t.Errorf("target steamid64 not serialized as string: %s", out)
	}
	if !strings.Contains(out, `"steamid64":"76561198000000001"`) {
		t.Errorf("victim steamid64 not serialized as string: %s", out)
	}
}

func TestPlanRoundtrip(t *testing.T) {
	original := Plan{
		SchemaVersion: "1.1",
		GeneratedAt:   time.Date(2026, 5, 14, 17, 42, 0, 0, time.UTC),
		Demo: Demo{
			Path:          "/tmp/demo.dem",
			SHA256:        "abc123",
			Map:           "de_inferno",
			Tickrate:      64,
			DurationTicks: 285000,
		},
		Target: Target{
			SteamID64:   "76561198000000000",
			NameInDemo:  "MARTINEZSA",
			TeamAtStart: "CT",
		},
		Segments: []Segment{
			{
				ID:        "seg-001",
				Round:     7,
				TickStart: 102340,
				TickEnd:   103200,
				Kills: []Kill{
					{
						Tick:     102450,
						Weapon:   "awp",
						Headshot: true,
						Wallbang: false,
						Victim: Player{
							SteamID64:  "76561198000000001",
							NameInDemo: "Player2",
							TeamAtKill: "T",
						},
						KillerPos: [3]float64{123.4, 456.7, 89.0},
						VictimPos: [3]float64{125.1, 470.2, 89.0},
					},
				},
			},
		},
		Stats: Stats{
			TotalKillsTarget:     24,
			KillsAfterFilters:    17,
			SegmentsCreated:      8,
			DurationSecondsTotal: 92.5,
		},
	}

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error = %v", err)
	}

	var got Plan
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal error = %v\nJSON was: %s", err, string(b))
	}

	if got.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion roundtrip = %q, want %q", got.SchemaVersion, original.SchemaVersion)
	}
	if !got.GeneratedAt.Equal(original.GeneratedAt) {
		t.Errorf("GeneratedAt roundtrip = %v, want %v", got.GeneratedAt, original.GeneratedAt)
	}
	if got.Demo != original.Demo {
		t.Errorf("Demo roundtrip = %+v, want %+v", got.Demo, original.Demo)
	}
	if got.Target != original.Target {
		t.Errorf("Target roundtrip = %+v, want %+v", got.Target, original.Target)
	}
	if len(got.Segments) != 1 {
		t.Fatalf("Segments length roundtrip = %d, want 1", len(got.Segments))
	}
	if got.Segments[0].Kills[0].KillerPos != original.Segments[0].Kills[0].KillerPos {
		t.Errorf("KillerPos roundtrip = %v, want %v",
			got.Segments[0].Kills[0].KillerPos, original.Segments[0].Kills[0].KillerPos)
	}
	if got.Stats != original.Stats {
		t.Errorf("Stats roundtrip = %+v, want %+v", got.Stats, original.Stats)
	}
}

func TestSegmentIDFormat(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{1, "seg-001"},
		{42, "seg-042"},
		{100, "seg-100"},
		{1000, "seg-1000"},
		{0, "seg-000"},
		{-1, "seg--01"},
		{-42, "seg--42"},
		{-100, "seg--100"},
	}
	for _, tt := range tests {
		got := FormatSegmentID(tt.n)
		if got != tt.want {
			t.Errorf("FormatSegmentID(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
