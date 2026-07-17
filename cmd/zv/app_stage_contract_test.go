package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/killplan"
)

// TestStageCommandsEmitDryRunEnvelope pins the {ok, dry_run, executed} success
// envelope on the JSON stdout of the demo-side pipeline stage commands. Fase 1
// added the envelope to demo parse/moments/select; Fase 2 normalized record,
// compose final, and shorts render. This drives the real delegated binaries so
// the contract holds end to end, not just in the wrapper. The stream stages
// (plan/killfeed/captions/render) are covered media-free in
// internal/streamcli and at the binary level in the ffmpeg-gated
// TestStreamJourneyBinaryChainsPlanAndRender, so they are not re-driven here.
func TestStageCommandsEmitDryRunEnvelope(t *testing.T) {
	t.Parallel()
	exe := buildDelegatedBinaries(t)

	ws := t.TempDir()
	plan := writeStageContractPlan(t, ws)

	demo := filepath.Join(ws, "demo.dem")
	if err := os.WriteFile(demo, []byte("dummy demo"), 0o600); err != nil {
		t.Fatalf("write demo fixture: %v", err)
	}
	recordingResult := filepath.Join(ws, "rec", "recording-result.json")
	if err := os.MkdirAll(filepath.Dir(recordingResult), 0o750); err != nil {
		t.Fatalf("mkdir recording fixture dir: %v", err)
	}
	if err := os.WriteFile(recordingResult, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write recording-result fixture: %v", err)
	}

	cases := []struct {
		name string
		args []string
	}{
		{
			name: "demo parse",
			// zv-parser has no --format flag; its dry-run always emits the JSON envelope.
			args: []string{"demo", "parse", "--demo", demo, "--steamid", "76561198000000000", "--out", filepath.Join(ws, "parse", "plan.json"), "--dry-run"},
		},
		{
			name: "demo moments",
			args: []string{"demo", "moments", "--killplan", plan, "--dry-run", "--format", "json"},
		},
		{
			name: "demo select",
			args: []string{"demo", "select", "--killplan", plan, "--segments", "seg-001", "--out", filepath.Join(ws, "select", "selected.json"), "--dry-run", "--format", "json"},
		},
		{
			name: "record",
			args: []string{"record", "--killplan", plan, "--demo", demo, "--out", filepath.Join(ws, "recording"), "--dry-run", "--format", "json"},
		},
		{
			name: "compose final",
			args: []string{"compose", "final", "--recording-result", recordingResult, "--out", filepath.Join(ws, "compose", "final.mp4"), "--dry-run", "--format", "json"},
		},
		{
			name: "shorts render",
			// The editor accepts the empty-artifacts recording result at the
			// media-free dry-run level (no captured segments to render).
			args: []string{"shorts", "render", "--recording-result", recordingResult, "--killplan", plan, "--out", filepath.Join(ws, "shorts"), "--publish-dir", filepath.Join(ws, "shorts", "shortslistosparasubir"), "--dry-run", "--format", "json"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _ := runZVBinarySplit(t, exe, ws, tc.args...)
			assertDryRunEnvelope(t, tc.name, stdout)
		})
	}
}

func assertDryRunEnvelope(t *testing.T, source, stdout string) {
	t.Helper()
	var row map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &row); err != nil {
		t.Fatalf("%s: unmarshal stdout envelope: %v\n%s", source, err, stdout)
	}
	for _, key := range []string{"ok", "dry_run", "executed"} {
		if _, ok := row[key]; !ok {
			t.Fatalf("%s: stdout envelope missing %q key: %s", source, key, stdout)
		}
	}
	if got := boolField(t, source, row, "ok"); !got {
		t.Fatalf("%s: ok = false, want true: %s", source, stdout)
	}
	if got := boolField(t, source, row, "dry_run"); !got {
		t.Fatalf("%s: dry_run = false, want true: %s", source, stdout)
	}
	if got := boolField(t, source, row, "executed"); got {
		t.Fatalf("%s: executed = true, want false: %s", source, stdout)
	}
}

func boolField(t *testing.T, source string, row map[string]json.RawMessage, key string) bool {
	t.Helper()
	var value bool
	if err := json.Unmarshal(row[key], &value); err != nil {
		t.Fatalf("%s: decode %q as bool: %v", source, key, err)
	}
	return value
}

// writeStageContractPlan writes a kill plan whose segments have positive tick
// bounds so the recorder's plan validation accepts it (the shared
// writeDemoReviewPlan fixture starts seg-001 at tick 0, which the recorder
// rejects).
func writeStageContractPlan(t *testing.T, dir string) string {
	t.Helper()
	plan := killplan.NewPlan()
	plan.Demo = killplan.Demo{SHA256: strings.Repeat("a", 64), Tickrate: 128, Map: "de_nuke"}
	plan.Target = killplan.Target{SteamID64: "76561198377256168", NameInDemo: "Joey-"}
	plan.Segments = []killplan.Segment{
		{ID: "seg-001", Round: 1, TickStart: 128, TickEnd: 256, Kills: []killplan.Kill{{Tick: 192, Weapon: "ak47"}}},
		{ID: "seg-002", Round: 2, TickStart: 384, TickEnd: 640, Kills: []killplan.Kill{{Tick: 448, Weapon: "awp", Headshot: true}}},
	}
	plan.Stats = killplan.Stats{TotalKillsTarget: 2, KillsAfterFilters: 2, SegmentsCreated: 2, DurationSecondsTotal: 3}
	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "killplan.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestStageCommandsEmitJSONErrorEnvelope pins the failure-side contract of the
// stage commands' --format json mode: failures must land on stdout as
// {ok:false, error} JSON with a plain error message, never as timestamped log
// lines (the systematic dry-run probes caught compose emitting log.Fatal output
// and the recorder polluting the envelope with a log timestamp prefix).
func TestStageCommandsEmitJSONErrorEnvelope(t *testing.T) {
	t.Parallel()
	exe := buildDelegatedBinaries(t)

	ws := t.TempDir()
	badPlan := filepath.Join(ws, "bad-plan.json")
	if err := os.WriteFile(badPlan, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write malformed plan fixture: %v", err)
	}
	demo := filepath.Join(ws, "demo.dem")
	if err := os.WriteFile(demo, []byte("dummy demo"), 0o600); err != nil {
		t.Fatalf("write demo fixture: %v", err)
	}

	cases := []struct {
		name string
		args []string
	}{
		{
			name: "compose final missing recording result",
			args: []string{"compose", "final", "--recording-result", filepath.Join(ws, "missing.json"), "--out", filepath.Join(ws, "final.mp4"), "--dry-run", "--format", "json"},
		},
		{
			name: "record malformed killplan",
			args: []string{"record", "--killplan", badPlan, "--demo", demo, "--out", filepath.Join(ws, "recording"), "--dry-run", "--format", "json"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, code := runZVBinaryFailureSplit(t, exe, ws, tc.args...)
			if code == 0 {
				t.Fatalf("%s: exit code = 0, want failure", tc.name)
			}
			var row struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(stdout), &row); err != nil {
				t.Fatalf("%s: stdout is not a JSON error envelope: %v\n%s", tc.name, err, stdout)
			}
			if row.OK {
				t.Fatalf("%s: ok = true, want false: %s", tc.name, stdout)
			}
			if strings.TrimSpace(row.Error) == "" {
				t.Fatalf("%s: error message empty: %s", tc.name, stdout)
			}
			if timestamped := regexp.MustCompile(`\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}`); timestamped.MatchString(row.Error) {
				t.Fatalf("%s: error message carries a log timestamp: %q", tc.name, row.Error)
			}
		})
	}
}
