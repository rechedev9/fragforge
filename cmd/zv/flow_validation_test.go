package main

import (
	"strings"
	"testing"
)

func TestValidateProductionFlowsIsClean(t *testing.T) {
	if issues := validateProductionFlows(productionFlows()); len(issues) != 0 {
		t.Fatalf("productionFlows() are not canonical: %#v", issues)
	}
}

func TestResolveFlowPlaceholders(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  string
	}{
		{"demo path", "<match.dem>", "match.dem"},
		{"steamid", "<SteamID64>", "76561198000000000"},
		{"segments", "<seg-ids>", "seg-1"},
		{"path with run prefix", "<run>/killplan.json", "run/killplan.json"},
		{"nested run path", "<run>/recording/recording-result.json", "run/recording/recording-result.json"},
		{"alternatives take first", "<gameplay|clean|deathnotices>", "gameplay"},
		{"inline boolean alternatives", "--portrait-safe-killfeed=<true|false>", "--portrait-safe-killfeed=true"},
		{"literal without placeholder", "--format", "--format"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveFlowPlaceholders(tc.token); got != tc.want {
				t.Fatalf("resolveFlowPlaceholders(%q) = %q, want %q", tc.token, got, tc.want)
			}
		})
	}
}

func TestResolveFlowCommandArgsDropsOptionalGroups(t *testing.T) {
	raw := []string{"shorts", "render", "[--intro-text", "<text>]", "[--music", "<track>]", "--covers=<true|false>"}
	got, issue := resolveFlowCommandArgs(raw)
	if issue != "" {
		t.Fatalf("unexpected issue: %s", issue)
	}
	want := []string{"shorts", "render", "--covers=true"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("resolveFlowCommandArgs = %q, want %q", got, want)
	}
}

func TestValidateProductionFlowsFlagsUnresolvedPlaceholder(t *testing.T) {
	flows := []productionFlow{{
		Name: "synthetic",
		Phases: []flowPhase{
			{ID: "bad", Command: "zv demo players --demo <mystery> --format json", ReadOnly: true},
		},
	}}
	issues := validateProductionFlows(flows)
	if len(issues) == 0 {
		t.Fatal("expected an unresolved placeholder issue")
	}
	if !strings.Contains(issues[0].Message, "unresolved placeholder") {
		t.Fatalf("issue = %#v, want unresolved placeholder", issues[0])
	}
}

func TestValidateProductionFlowsFlagsReadOnlyMutatingWithoutDryRun(t *testing.T) {
	flows := []productionFlow{{
		Name: "synthetic",
		Phases: []flowPhase{
			{ID: "leaky", Command: "zv demo select --killplan <run>/killplan.json --segments <seg-ids> --out <run>/selected-plan.json --format json", ReadOnly: true},
		},
	}}
	issues := validateProductionFlows(flows)
	if len(issues) == 0 {
		t.Fatal("expected a read-only/dry-run issue")
	}
	if !strings.Contains(issues[0].Message, "--dry-run") {
		t.Fatalf("issue = %#v, want missing --dry-run", issues[0])
	}
}

// TestValidateProductionFlowsFlagsReadOnlyRunningExpensiveCommands pins the
// rewritten drift guard: a read-only phase running a genuinely expensive or
// mutating command (resolved through the catalog by RunArgs prefix) must fail
// without --dry-run, even when the command exposes no --dry-run flag in the old
// commandBoolFlags table (music analyze) or carries a positional the display name
// missed (short match.dem).
func TestValidateProductionFlowsFlagsReadOnlyRunningExpensiveCommands(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"short with positional demo", "zv short match.dem --prompt allkills"},
		{"music analyze without dry-run", "zv music analyze --input track.mp4 --out rhythm.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			flows := []productionFlow{{
				Name: "synthetic",
				Phases: []flowPhase{
					{ID: "leaky", Command: tc.command, ReadOnly: true},
				},
			}}
			issues := validateProductionFlows(flows)
			if len(issues) == 0 {
				t.Fatalf("expected a read-only/dry-run issue for %q", tc.command)
			}
			if !strings.Contains(issues[0].Message, "--dry-run") {
				t.Fatalf("issue = %#v, want missing --dry-run", issues[0])
			}
		})
	}
}

// TestValidateProductionFlowsAllowsReadOnlyInspectionCommands guards against
// over-eager flagging: a read-only phase running a lightweight inspection command
// (demo players lists a roster; it is non-ReadOnly because --out can persist, yet
// this invocation does not) must stay clean without a --dry-run it does not offer.
func TestValidateProductionFlowsAllowsReadOnlyInspectionCommands(t *testing.T) {
	flows := []productionFlow{{
		Name: "synthetic",
		Phases: []flowPhase{
			{ID: "roster", Command: "zv demo players --demo match.dem --format json", ReadOnly: true},
		},
	}}
	if issues := validateProductionFlows(flows); len(issues) != 0 {
		t.Fatalf("expected no issues for a read-only inspection command, got %#v", issues)
	}
}

func TestValidateProductionFlowsFlagsNonCanonicalCommand(t *testing.T) {
	flows := []productionFlow{{
		Name: "synthetic",
		Phases: []flowPhase{
			{ID: "typo", Command: "zv demo parse --demo <match.dem> --steamid <SteamID64> --bogus <run>/killplan.json"},
		},
	}}
	issues := validateProductionFlows(flows)
	if len(issues) == 0 {
		t.Fatal("expected a non-canonical command issue")
	}
	if !strings.Contains(issues[0].Message, "not canonical") {
		t.Fatalf("issue = %#v, want non-canonical", issues[0])
	}
}
