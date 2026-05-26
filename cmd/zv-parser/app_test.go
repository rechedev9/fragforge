package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runApp(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"zv-parser"}, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRunNoArgsExits2(t *testing.T) {
	code, _, stderr := runApp(t)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if stderr == "" {
		t.Error("stderr empty, want usage message")
	}
}

func TestRunMissingDemoFlagExits2(t *testing.T) {
	code, _, stderr := runApp(t, "parse", "--steamid", "76561198000000000")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "demo") {
		t.Errorf("stderr %q should mention --demo", stderr)
	}
}

func TestRunMissingSteamIDExits2(t *testing.T) {
	code, _, stderr := runApp(t, "parse", "--demo", "/tmp/x.dem")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "steamid") {
		t.Errorf("stderr %q should mention --steamid", stderr)
	}
}

func TestRunInvalidSteamIDExits2(t *testing.T) {
	code, _, _ := runApp(t, "parse", "--demo", "/tmp/x.dem", "--steamid", "not-a-number")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRunInvalidSegmentModeExits2(t *testing.T) {
	code, _, stderr := runApp(t, "parse", "--demo", "/tmp/x.dem", "--steamid", "76561198000000000", "--segment-mode", "utilityx")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "segment-mode") {
		t.Errorf("stderr %q should mention --segment-mode", stderr)
	}
}

func TestRunNonexistentDemoExits3(t *testing.T) {
	code, _, stderr := runApp(t,
		"parse",
		"--demo", "/tmp/does-not-exist.dem",
		"--steamid", "76561198000000000",
	)
	if code != 3 {
		t.Errorf("exit code = %d, want 3, stderr=%s", code, stderr)
	}
}

func TestRunInvalidRulesFileExits2(t *testing.T) {
	dir := t.TempDir()
	rulesPath := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(rulesPath, []byte(`{not-json}`), 0o644); err != nil {
		t.Fatalf("write rules: %v", err)
	}
	code, _, _ := runApp(t,
		"parse",
		"--demo", "/tmp/x.dem",
		"--steamid", "76561198000000000",
		"--rules", rulesPath,
	)
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (invalid rules JSON)", code)
	}
}

func TestRunRulesFileNotFoundExits3(t *testing.T) {
	code, _, _ := runApp(t,
		"parse",
		"--demo", "/tmp/x.dem",
		"--steamid", "76561198000000000",
		"--rules", "/tmp/no-such-rules.json",
	)
	if code != 3 {
		t.Errorf("exit code = %d, want 3 (missing rules file)", code)
	}
}

func TestRunUnknownSubcommandExits2(t *testing.T) {
	code, _, _ := runApp(t, "frobnicate")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRunUtilityAuditMissingPlanExits2(t *testing.T) {
	code, _, stderr := runApp(t, "utility-audit")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "plan") {
		t.Errorf("stderr %q should mention --plan", stderr)
	}
}

func TestRunUtilityAuditInvalidFormatExits2(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(`{"schema_version":"1.1","segments":[]}`), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}
	code, _, stderr := runApp(t, "utility-audit", "--plan", planPath, "--format", "xml")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "format") {
		t.Errorf("stderr %q should mention format", stderr)
	}
}
