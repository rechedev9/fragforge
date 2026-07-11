package trace

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rechedev9/fragforge/internal/parser"
)

// e2eSteamID is the known target for the repo's real-demo fixture, the same
// player id used by internal/parser and internal/workers real-demo tests
// (see internal/parser/roster_test.go).
const e2eSteamID = "76561198148986856" // maaryy

// loadRealDemoPath mirrors the parser/worker real-demo convention exactly:
// TEST_DEMO_PATH overrides the testdata fixture, and an unavailable demo skips
// the test rather than failing the gate (no .dem is committed to the repo).
func loadRealDemoPath(t *testing.T) string {
	t.Helper()
	path := os.Getenv("TEST_DEMO_PATH")
	if path == "" {
		path = filepath.Join("..", "..", "testdata", "lavked-vs-tnc-m2-nuke.dem")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no test demo at %s: %v", path, err)
	}
	return path
}

// TestRunAgainstRealDemoEndToEnd exercises the full demo->trace path (open,
// hash, demoinfocs parse, moments scoring, render argv) against a real .dem,
// the same fixture the parser/worker real-demo tests use. It is skipped when
// the fixture is absent, e.g. in CI or a fresh checkout.
func TestRunAgainstRealDemoEndToEnd(t *testing.T) {
	demoPath := loadRealDemoPath(t)

	doc, err := Run(context.Background(), Options{
		DemoPath:      demoPath,
		SteamID:       e2eSteamID,
		SegmentMode:   parser.SegmentModeKills,
		Deterministic: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if doc.Source.Kind != "demo" {
		t.Errorf("Source.Kind = %q, want %q", doc.Source.Kind, "demo")
	}
	if doc.Source.Path != demoPath {
		t.Errorf("Source.Path = %q, want %q", doc.Source.Path, demoPath)
	}
	if doc.Source.SHA256 == "" {
		t.Error("Source.SHA256 is empty, want the demo's hash")
	}

	if len(doc.KillPlan.Segments) == 0 {
		t.Fatal("kill plan has no segments, want at least one against a real demo (regression)")
	}
	if doc.KillPlan.Stats.SegmentsCreated == 0 {
		t.Error("KillPlan.Stats.SegmentsCreated = 0, want > 0")
	}
	if doc.KillPlan.Target.SteamID64 != e2eSteamID {
		t.Errorf("KillPlan.Target.SteamID64 = %q, want %q", doc.KillPlan.Target.SteamID64, e2eSteamID)
	}

	if len(doc.Moments.Moments) != len(doc.KillPlan.Segments) {
		t.Errorf("moments count = %d, want %d (one per segment)", len(doc.Moments.Moments), len(doc.KillPlan.Segments))
	}
	if len(doc.Selection.SegmentIDs) == 0 {
		t.Error("Selection.SegmentIDs is empty, want at least one selected segment")
	}

	if len(doc.Render.FFmpegArgv) == 0 {
		t.Fatal("Render.FFmpegArgv is empty, want the compiled short's argv")
	}
	argv := doc.Render.FFmpegArgv[0]
	if len(argv) == 0 || argv[0] != "ffmpeg" {
		t.Errorf("argv = %v, want it to start with the ffmpeg placeholder path", argv)
	}
}

// TestRunAgainstRealDemoIsDeterministic runs the deterministic demo->trace
// path twice over the same real .dem and requires byte-identical output, so a
// nondeterministic parse or scoring step cannot hide behind the fixture-only
// golden tests.
func TestRunAgainstRealDemoIsDeterministic(t *testing.T) {
	demoPath := loadRealDemoPath(t)
	opts := Options{
		DemoPath:      demoPath,
		SteamID:       e2eSteamID,
		SegmentMode:   parser.SegmentModeKills,
		Deterministic: true,
	}

	first, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	second, err := Run(context.Background(), opts)
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
		t.Errorf("two deterministic runs over the same demo produced different output:\nfirst:  %s\nsecond: %s", a, b)
	}
}
