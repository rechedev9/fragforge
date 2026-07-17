package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/killplan"
)

func TestRunDemoMomentsRanksCandidatesAndWritesArtifact(t *testing.T) {
	dir := t.TempDir()
	planPath := writeDemoReviewPlan(t, dir)
	outPath := filepath.Join(dir, "review", "moments.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"zv", "demo", "moments", "--killplan", planPath,
		"--top", "2", "--out", outPath, "--format", "json",
	}, &stdout, &stderr, nil, &fakeRunner{})
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result demoMomentsResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Count != 2 || len(result.Document.Moments) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if got, want := result.Document.Moments[0].SegmentID, "seg-002"; got != want {
		t.Fatalf("highest-ranked segment = %q, want %q", got, want)
	}
	if result.Document.JobID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("moments job id is nil; want stable source-derived identity")
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("stat moments artifact: %v", err)
	}
}

func TestRunDemoReviewHelpIsReachable(t *testing.T) {
	tests := []struct {
		subcommand string
		want       string
	}{
		{subcommand: "moments", want: demoMomentsUsage},
		{subcommand: "select", want: demoSelectUsage},
	}
	for _, tt := range tests {
		t.Run(tt.subcommand, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"zv", "demo", tt.subcommand, "--help"}, &stdout, &stderr, nil, &fakeRunner{})
			if code != exitSuccess || stderr.Len() != 0 {
				t.Fatalf("code = %d, stderr = %q", code, stderr.String())
			}
			if got := stdout.String(); got != tt.want {
				t.Fatalf("stdout = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunDemoSelectPreservesRequestedOrderAndRecalculatesStats(t *testing.T) {
	dir := t.TempDir()
	planPath := writeDemoReviewPlan(t, dir)
	outPath := filepath.Join(dir, "selected-plan.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"zv", "demo", "select", "--killplan", planPath,
		"--segments", "seg-003,seg-001", "--out", outPath, "--format", "json",
	}, &stdout, &stderr, nil, &fakeRunner{})
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result demoSelectionResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.DryRun || !result.Executed {
		t.Fatalf("result = %#v", result)
	}
	var gotIDs []string
	for _, segment := range result.Plan.Segments {
		gotIDs = append(gotIDs, segment.ID)
	}
	if want := []string{"seg-003", "seg-001"}; !reflect.DeepEqual(gotIDs, want) {
		t.Fatalf("segment order = %v, want %v", gotIDs, want)
	}
	if got, want := result.Plan.Stats.KillsAfterFilters, 2; got != want {
		t.Fatalf("kills after filters = %d, want %d", got, want)
	}
	if got, want := result.Plan.Stats.SegmentsCreated, 2; got != want {
		t.Fatalf("segments created = %d, want %d", got, want)
	}
	if got, want := result.Plan.Stats.DurationSecondsTotal, 2.0; got != want {
		t.Fatalf("duration = %v, want %v", got, want)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("stat selected plan: %v", err)
	}
}

func TestRunDemoSelectDryRunRejectsUnknownSegmentWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	planPath := writeDemoReviewPlan(t, dir)
	outPath := filepath.Join(dir, "selected-plan.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"zv", "demo", "select", "--killplan", planPath,
		"--segments", "seg-999", "--out", outPath, "--dry-run", "--format", "json",
	}, &stdout, &stderr, nil, &fakeRunner{})
	if code != exitInvalidArgs || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("output stat error = %v, want not exist", err)
	}
	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || !strings.Contains(result.Error, "seg-999") {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunDemoReviewCommandsRefuseToOverwriteKillPlan(t *testing.T) {
	tests := []struct {
		name string
		args func(string) []string
	}{
		{
			name: "moments",
			args: func(path string) []string {
				return []string{"zv", "demo", "moments", "--killplan", path, "--out", path, "--format", "json"}
			},
		},
		{
			name: "select",
			args: func(path string) []string {
				return []string{"zv", "demo", "select", "--killplan", path, "--segments", "seg-001", "--out", path, "--format", "json"}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			planPath := writeDemoReviewPlan(t, dir)
			before, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := Run(tt.args(planPath), &stdout, &stderr, nil, &fakeRunner{})
			if code != exitInvalidArgs || stderr.Len() != 0 {
				t.Fatalf("code = %d, stderr = %q", code, stderr.String())
			}
			var result struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(result.Error, "must not overwrite --killplan") {
				t.Fatalf("result = %#v", result)
			}
			after, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatal("kill plan changed despite rejected output alias")
			}
		})
	}
}

func TestRunDemoReviewValidationErrorsStayMachineReadable(t *testing.T) {
	for _, command := range []string{"moments", "select"} {
		t.Run(command, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"zv", "demo", command, "--format", "json"}, &stdout, &stderr, nil, &fakeRunner{})
			if code != exitInvalidArgs || stderr.Len() != 0 {
				t.Fatalf("code = %d, stderr = %q", code, stderr.String())
			}
			var result struct {
				OK    bool   `json:"ok"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.OK || result.Error == "" {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func writeDemoReviewPlan(t *testing.T, dir string) string {
	t.Helper()
	plan := killplan.NewPlan()
	plan.Demo = killplan.Demo{SHA256: strings.Repeat("a", 64), Tickrate: 128, Map: "de_nuke"}
	plan.Target = killplan.Target{SteamID64: "76561198377256168", NameInDemo: "Joey-"}
	plan.Segments = []killplan.Segment{
		{ID: "seg-001", Round: 1, TickStart: 0, TickEnd: 128, Kills: []killplan.Kill{{Tick: 64, Weapon: "ak47"}}},
		{ID: "seg-002", Round: 2, TickStart: 128, TickEnd: 384, Kills: []killplan.Kill{
			{Tick: 192, Weapon: "awp", Headshot: true},
			{Tick: 256, Weapon: "awp", Wallbang: true},
		}},
		{ID: "seg-003", Round: 3, TickStart: 384, TickEnd: 512, Kills: []killplan.Kill{{Tick: 448, Weapon: "deagle"}}},
	}
	plan.Stats = killplan.Stats{TotalKillsTarget: 4, KillsAfterFilters: 4, SegmentsCreated: 3, DurationSecondsTotal: 4}
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
