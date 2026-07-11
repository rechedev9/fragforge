package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/killplan"
)

// samplePlan is a small, realistic kill plan: two kill segments (one a double
// kill) plus a utility throw, enough to exercise moments scoring and the
// compiled render argv.
func samplePlan() killplan.Plan {
	return killplan.Plan{
		SchemaVersion: killplan.SchemaVersion,
		Demo: killplan.Demo{
			Path:     "sample.dem",
			Map:      "de_mirage",
			Tickrate: 64,
		},
		Target: killplan.Target{
			SteamID64:  "76561198000000000",
			NameInDemo: "martinez",
		},
		Segments: []killplan.Segment{
			{
				ID:        "seg-001",
				Round:     3,
				TickStart: 1000,
				TickEnd:   1640,
				Kills: []killplan.Kill{
					{Tick: 1200, Weapon: "ak47", Headshot: true, Victim: killplan.Player{NameInDemo: "enemy1"}},
					{Tick: 1400, Weapon: "ak47", Victim: killplan.Player{NameInDemo: "enemy2"}},
				},
			},
			{
				ID:        "seg-002",
				Round:     7,
				TickStart: 5000,
				TickEnd:   5512,
				Kills: []killplan.Kill{
					{Tick: 5200, Weapon: "awp", Wallbang: true, Victim: killplan.Player{NameInDemo: "enemy3"}},
				},
				Utility: []killplan.UtilityThrow{
					{ID: "u1", Type: "smokegrenade", Round: 7, ThrowTick: 5050},
				},
			},
		},
	}
}

// writePlan marshals a plan to a temp file so Run exercises the real
// --from-plan load path.
func writePlan(t *testing.T, plan killplan.Plan) string {
	t.Helper()
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	path := filepath.Join(t.TempDir(), "killplan.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	return path
}

func TestRunSegmentsPropagateToMoments(t *testing.T) {
	plan := samplePlan()
	doc, err := Run(context.Background(), Options{FromPlan: writePlan(t, plan), Deterministic: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got, want := len(doc.Moments.Moments), len(plan.Segments); got != want {
		t.Fatalf("moment count: got %d, want %d", got, want)
	}
	for i, segment := range plan.Segments {
		if got := doc.Moments.Moments[i].SegmentID; got != segment.ID {
			t.Errorf("moment[%d] segment id: got %q, want %q", i, got, segment.ID)
		}
		if got := doc.Selection.SegmentIDs[i]; got != segment.ID {
			t.Errorf("selection[%d]: got %q, want %q", i, got, segment.ID)
		}
	}
	if got, want := len(doc.Selection.MomentIDs), len(plan.Segments); got != want {
		t.Errorf("selection moment count: got %d, want %d", got, want)
	}
}

func TestRunFFmpegArgvStartsWithPath(t *testing.T) {
	doc, err := Run(context.Background(), Options{FromPlan: writePlan(t, samplePlan()), Deterministic: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(doc.Render.FFmpegArgv) == 0 {
		t.Fatal("expected at least one ffmpeg argv")
	}
	argv := doc.Render.FFmpegArgv[0]
	if len(argv) == 0 {
		t.Fatal("ffmpeg argv is empty")
	}
	if argv[0] != "ffmpeg" {
		t.Errorf("argv[0]: got %q, want %q", argv[0], "ffmpeg")
	}
	// The compiled short carries one part per segment.
	if got, want := doc.Render.Summary.SegmentCount, len(samplePlan().Segments); got != want {
		t.Errorf("summary segment count: got %d, want %d", got, want)
	}
	if got, want := doc.Render.Preset, editor.DefaultPreset().Name; got != want {
		t.Errorf("preset: got %q, want %q", got, want)
	}
}

func TestRunDeterministicIsByteIdentical(t *testing.T) {
	path := writePlan(t, samplePlan())
	first, err := Run(context.Background(), Options{FromPlan: path, Deterministic: true})
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	second, err := Run(context.Background(), Options{FromPlan: path, Deterministic: true})
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}

	a, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	b, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("deterministic runs differ:\nfirst:  %s\nsecond: %s", a, b)
	}
	if !first.GeneratedAt.IsZero() {
		t.Errorf("deterministic GeneratedAt should be zero, got %v", first.GeneratedAt)
	}
}

func TestRunEmptyPlan(t *testing.T) {
	plan := samplePlan()
	plan.Segments = nil
	doc, err := Run(context.Background(), Options{FromPlan: writePlan(t, plan), Deterministic: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	found := false
	for _, warning := range doc.Warnings {
		if warning == "kill plan has no segments; render plan is empty" {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want the empty-plan warning", doc.Warnings)
	}
	if doc.Render.Summary.Compiled {
		t.Error("Summary.Compiled = true, want false for an empty plan")
	}
	if doc.Render.FFmpegArgv == nil {
		t.Error("FFmpegArgv is nil, want an empty slice so JSON emits [] instead of null")
	}
	if len(doc.Render.FFmpegArgv) != 0 {
		t.Errorf("FFmpegArgv = %v, want empty for an empty plan", doc.Render.FFmpegArgv)
	}
}

func TestRunRejectsWrongSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "killplan.json")
	// Written by hand: marshaling a killplan.Plan force-writes the supported
	// schema version, so a stale document can only be produced as raw JSON.
	raw := `{"schema_version":"0.9","demo":{"tickrate":64},"target":{},"segments":[],"stats":{}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	if _, err := Run(context.Background(), Options{FromPlan: path}); err == nil {
		t.Fatal("expected error for a plan with an unsupported schema version")
	}
}

func TestRunWarnsOnStructuralPlanProblems(t *testing.T) {
	plan := samplePlan()
	plan.Demo.Tickrate = 0
	plan.Segments[0].TickEnd = plan.Segments[0].TickStart
	plan.Segments[1].Kills[0].Tick = plan.Segments[1].TickEnd + 500

	doc, err := Run(context.Background(), Options{FromPlan: writePlan(t, plan), Deterministic: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	wantSubstrings := []string{
		"tickrate",
		"invalid tick range",
		"outside the segment tick window",
	}
	for _, want := range wantSubstrings {
		found := false
		for _, warning := range doc.Warnings {
			if strings.Contains(warning, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("warnings = %v, want one containing %q", doc.Warnings, want)
		}
	}
}

func TestRunRequiresSource(t *testing.T) {
	if _, err := Run(context.Background(), Options{Deterministic: true}); err == nil {
		t.Fatal("expected error when no source is given")
	}
}

func TestRunUnknownPreset(t *testing.T) {
	_, err := Run(context.Background(), Options{FromPlan: writePlan(t, samplePlan()), Preset: "nope"})
	if err == nil {
		t.Fatal("expected error for unknown preset")
	}
}
