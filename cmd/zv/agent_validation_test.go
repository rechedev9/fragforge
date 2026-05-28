package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWorkflowsCheckRejectsUnsafeClaudeReadOnlyCommand(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, ".claude", "commands", "zv-toolchain-diagnose.md"), strings.Join([]string{
		"Diagnose local toolchain.",
		"",
		"Run `scripts/go-tools-check.sh`.",
		"Install missing tools automatically.",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		`.claude/commands/zv-toolchain-diagnose.md: missing standard gate guidance "Read-only diagnosis. Do not install tools or edit files unless the user asks."`,
		`.claude/commands/zv-toolchain-diagnose.md: missing standard gate guidance "Do not run CS2/HLAE, Docker compose, migrations, or renders."`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}
