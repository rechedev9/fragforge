package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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

// TestWriteBytesCreatesNestedOutputDir is a regression test: --out pointing at
// a nested, not-yet-existing directory must succeed by creating the parents,
// while "-" still streams to stdout.
func TestWriteBytesCreatesNestedOutputDir(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte(`{"schema_version":"1.1","segments":[]}`), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	tests := []struct {
		name       string
		out        string
		wantStdout bool
		wantFile   string
	}{
		{
			name:     "nested directory is created",
			out:      filepath.Join(dir, "nested", "deeper", "audit.json"),
			wantFile: filepath.Join(dir, "nested", "deeper", "audit.json"),
		},
		{
			name:       "dash streams to stdout",
			out:        "-",
			wantStdout: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runApp(t, "utility-audit", "--plan", planPath, "--format", "json", "--out", tt.out)
			if code != 0 {
				t.Fatalf("exit code = %d, want 0, stderr=%s", code, stderr)
			}
			if tt.wantStdout {
				if stdout == "" {
					t.Fatal("stdout empty, want audit output")
				}
				return
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty when writing to file", stdout)
			}
			if _, err := os.Stat(tt.wantFile); err != nil {
				t.Fatalf("stat output file: %v", err)
			}
		})
	}
}

// TestRunParseDryRunValidatesWithoutParsing is a table-driven check that
// "parse --dry-run" validates inputs and the output path, emits the
// {ok, dry_run, executed} envelope, and never writes the plan.
func TestRunParseDryRunValidatesWithoutParsing(t *testing.T) {
	dir := t.TempDir()
	demoPath := filepath.Join(dir, "match.dem")
	if err := os.WriteFile(demoPath, []byte("not a real demo"), 0o644); err != nil {
		t.Fatalf("write demo: %v", err)
	}

	tests := []struct {
		name string
		out  string
	}{
		{name: "stdout target", out: "-"},
		{name: "nested output path", out: filepath.Join(dir, "runs", "plan.json")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runApp(t, "parse", "--demo", demoPath, "--steamid", "76561198000000000", "--out", tt.out, "--dry-run")
			if code != 0 {
				t.Fatalf("exit code = %d, want 0, stderr=%s", code, stderr)
			}
			var got parseDryRunResult
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("unmarshal stdout %q: %v", stdout, err)
			}
			if !got.OK || !got.DryRun || got.Executed {
				t.Fatalf("envelope = %#v, want ok+dry_run without executed", got)
			}
			if tt.out != "-" {
				if _, err := os.Stat(tt.out); !os.IsNotExist(err) {
					t.Fatalf("output stat error = %v, want not exist (dry-run must not write)", err)
				}
			}
		})
	}
}

// TestOutputCreatableRejectsNonexistentRoot guards the ancestor walk: a path
// whose walk exhausts at a filesystem root that itself does not exist (a
// nonexistent Windows drive, or a regular file used as a directory ancestor
// elsewhere) is not creatable and must report an error rather than a false
// positive.
func TestOutputCreatableRejectsNonexistentRoot(t *testing.T) {
	var out string
	if runtime.GOOS == "windows" {
		out = `Q:\does-not-exist\plan.json`
	} else {
		file := filepath.Join(t.TempDir(), "not-a-dir")
		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatalf("write file: %v", err)
		}
		out = filepath.Join(file, "child", "plan.json")
	}
	if err := outputCreatable(out); err == nil {
		t.Fatalf("outputCreatable(%q) = nil, want error", out)
	}
}

func TestOutputCreatableAcceptsNestedUnderExistingRoot(t *testing.T) {
	out := filepath.Join(t.TempDir(), "runs", "deeper", "plan.json")
	if err := outputCreatable(out); err != nil {
		t.Fatalf("outputCreatable(%q) = %v, want nil", out, err)
	}
}

func TestRunParseDryRunMissingDemoExits3(t *testing.T) {
	code, _, _ := runApp(t, "parse", "--demo", "/tmp/does-not-exist.dem", "--steamid", "76561198000000000", "--dry-run")
	if code != 3 {
		t.Errorf("exit code = %d, want 3 (missing demo even in dry-run)", code)
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
