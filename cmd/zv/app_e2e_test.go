package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZVBinaryCanonicalGroupHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "demo", args: []string{"demo", "--help"}, want: demoUsage},
		{name: "utility", args: []string{"utility", "--help"}, want: utilityUsage},
		{name: "compose", args: []string{"compose", "--help"}, want: composeUsage},
		{name: "shorts", args: []string{"shorts", "--help"}, want: shortsUsage},
		{name: "analysis", args: []string{"analysis", "--help"}, want: analysisUsage},
		{name: "gallery", args: []string{"gallery", "--help"}, want: galleryUsage},
		{name: "check", args: []string{"check", "--help"}, want: checkUsage},
		{name: "skills", args: []string{"skills", "--help"}, want: skillsUsage},
		{name: "workflows", args: []string{"workflows", "--help"}, want: workflowsUsage},
		{name: "serve", args: []string{"serve", "--help"}, want: serveUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runZVBinary(t, exe, tempDir, tt.args...)
			if got, want := out, tt.want; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryCanonicalHelpAliasesEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "root short", args: []string{"-h"}, want: usage},
		{name: "root long", args: []string{"--help"}, want: usage},
		{name: "root word", args: []string{"help"}, want: usage},
		{name: "demo short", args: []string{"demo", "-h"}, want: demoUsage},
		{name: "demo word", args: []string{"demo", "help"}, want: demoUsage},
		{name: "skills list short", args: []string{"skills", "list", "-h"}, want: skillsListUsage},
		{name: "skills list word", args: []string{"skills", "list", "help"}, want: skillsListUsage},
		{name: "workflows show short", args: []string{"workflows", "show", "-h"}, want: workflowsShowUsage},
		{name: "workflows show word", args: []string{"workflows", "show", "help"}, want: workflowsShowUsage},
		{name: "gallery open short", args: []string{"gallery", "open", "-h"}, want: galleryUsage},
		{name: "gallery open word", args: []string{"gallery", "open", "help"}, want: galleryUsage},
		{name: "workflows run project short", args: []string{"workflows", "run", "project-check", "--", "-h"}, want: checkUsage},
		{name: "workflows run project word", args: []string{"workflows", "run", "project-check", "--", "help"}, want: checkUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := runZVBinarySplit(t, exe, tempDir, tt.args...)

			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if got, want := stdout, tt.want; got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryHelpCoversWorkflowCatalogEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	stdout, stderr := runZVBinarySplit(t, exe, tempDir, "--help")
	if stderr != "" {
		t.Fatalf("root help stderr = %q, want empty", stderr)
	}
	rootStems := usageCommandStems(stdout)
	groupStems := make(map[string]map[string]struct{})
	for _, workflow := range workflowCatalog() {
		stem := helpCommandStem(documentedWorkflowCommand(workflow.Command))
		if stem == "" {
			continue
		}
		if _, ok := rootStems[stem]; !ok {
			t.Fatalf("root help does not cover workflow %q command %q; saw %#v", workflow.Name, stem, rootStems)
		}
		fields, ok := splitCommandFields(stem)
		if !ok || len(fields) < 2 || fields[0] != "zv" {
			t.Fatalf("workflow %q command stem %q is not a zv command", workflow.Name, stem)
		}
		if fields[1] == "record" || fields[1] == "pipeline" {
			continue
		}
		if groupStems[fields[1]] == nil {
			groupStdout, groupStderr := runZVBinarySplit(t, exe, tempDir, fields[1], "--help")
			if groupStderr != "" {
				t.Fatalf("%s help stderr = %q, want empty", fields[1], groupStderr)
			}
			groupStems[fields[1]] = usageCommandStems(groupStdout)
		}
		if _, ok := groupStems[fields[1]][stem]; !ok {
			t.Fatalf("%s help does not cover workflow %q command %q; saw %#v", fields[1], workflow.Name, stem, groupStems[fields[1]])
		}
	}
}

func TestZVBinaryCanonicalSubcommandHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "skills list", args: []string{"skills", "list", "--help"}, want: skillsListUsage},
		{name: "skills show", args: []string{"skills", "show", "--help"}, want: skillsShowUsage},
		{name: "skills check", args: []string{"skills", "check", "--help"}, want: skillsCheckUsage},
		{name: "gallery open", args: []string{"gallery", "open", "--help"}, want: galleryUsage},
		{name: "workflows list", args: []string{"workflows", "list", "--help"}, want: workflowsListUsage},
		{name: "workflows show", args: []string{"workflows", "show", "--help"}, want: workflowsShowUsage},
		{name: "workflows check", args: []string{"workflows", "check", "--help"}, want: workflowsCheckUsage},
		{name: "run skills check", args: []string{"workflows", "run", "skills-check", "--", "--help"}, want: skillsCheckUsage},
		{name: "run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--help"}, want: workflowsCheckUsage},
		{name: "run project check", args: []string{"workflows", "run", "project-check", "--", "--help"}, want: checkUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runZVBinary(t, exe, tempDir, tt.args...)
			if got, want := out, tt.want; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryCanonicalSubcommandHelpUsesStdoutOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "skills list", args: []string{"skills", "list", "--help"}, want: skillsListUsage},
		{name: "skills show", args: []string{"skills", "show", "--help"}, want: skillsShowUsage},
		{name: "skills check", args: []string{"skills", "check", "--help"}, want: skillsCheckUsage},
		{name: "gallery open", args: []string{"gallery", "open", "--help"}, want: galleryUsage},
		{name: "workflows list", args: []string{"workflows", "list", "--help"}, want: workflowsListUsage},
		{name: "workflows show", args: []string{"workflows", "show", "--help"}, want: workflowsShowUsage},
		{name: "workflows check", args: []string{"workflows", "check", "--help"}, want: workflowsCheckUsage},
		{name: "run skills check", args: []string{"workflows", "run", "skills-check", "--", "--help"}, want: skillsCheckUsage},
		{name: "run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--help"}, want: workflowsCheckUsage},
		{name: "run project check", args: []string{"workflows", "run", "project-check", "--", "--help"}, want: checkUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := runZVBinarySplit(t, exe, tempDir, tt.args...)
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if got, want := stdout, tt.want; got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryWorkflowRunForwardsDelegatedHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	logPath := filepath.Join(tempDir, "calls.jsonl")
	runZVBinaryWithEnv(t, exe, tempDir, []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + logPath,
	}, "workflows", "run", "demo-parse", "--", "--help")

	calls := readFakeSubcommandCalls(t, logPath)
	if got, want := len(calls), 1; got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, calls)
	}
	if got, want := calls[0].Executable, executableName("zv-parser"); got != want {
		t.Fatalf("executable = %q, want %q", got, want)
	}
	if got, want := strings.Join(calls[0].Args, "\x00"), strings.Join([]string{"parse", "--help"}, "\x00"); got != want {
		t.Fatalf("args = %#v, want parse --help", calls[0].Args)
	}
}

func TestZVBinaryEveryWorkflowHelpEndToEnd(t *testing.T) {
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
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	logPath := filepath.Join(tempDir, "calls.jsonl")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + logPath,
	}
	wantDelegated := 0
	for _, workflow := range workflowCatalog() {
		t.Run(workflow.Name, func(t *testing.T) {
			args := workflowRunCommandArgs(t, workflow)
			args = append(args, "--", "--help")
			out := runZVBinaryWithEnv(t, exe, tempDir, env, args...)
			if workflowHelpDelegatesExternally(workflow) {
				wantDelegated++
				return
			}
			if strings.TrimSpace(out) == "" {
				t.Fatalf("help output is empty")
			}
		})
	}
	if got, want := len(readFakeSubcommandCalls(t, logPath)), wantDelegated; got != want {
		t.Fatalf("delegated help calls = %d, want %d", got, want)
	}
}

func TestZVBinaryEveryDirectWorkflowHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	logPath := filepath.Join(tempDir, "calls.jsonl")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + logPath,
	}
	wantDelegated := 0
	for _, workflow := range workflowCatalog() {
		t.Run(workflow.Name, func(t *testing.T) {
			args := append([]string(nil), workflow.RunArgs...)
			args = append(args, "--help")
			out := runZVBinaryWithEnv(t, exe, tempDir, env, args...)
			if workflowHelpDelegatesExternally(workflow) {
				wantDelegated++
				return
			}
			if strings.TrimSpace(out) == "" {
				t.Fatalf("help output is empty")
			}
		})
	}
	if got, want := len(readFakeSubcommandCalls(t, logPath)), wantDelegated; got != want {
		t.Fatalf("delegated direct help calls = %d, want %d", got, want)
	}
}

func TestZVBinaryCanonicalSubcommandUsageErrorsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "skills list", args: []string{"skills", "list", "extra"}, want: "error: unexpected extra args for \"skills list\"\n" + skillsListUsage},
		{name: "skills show", args: []string{"skills", "show"}, want: "error: missing skill name for \"skills show\"\n" + skillsShowUsage},
		{name: "skills check", args: []string{"skills", "check", "extra"}, want: "error: unexpected extra args for \"skills check\"\n" + skillsCheckUsage},
		{name: "workflows list", args: []string{"workflows", "list", "extra"}, want: "error: unexpected extra args for \"workflows list\"\n" + workflowsListUsage},
		{name: "workflows show", args: []string{"workflows", "show"}, want: "error: missing workflow name for \"workflows show\"\n" + workflowsShowUsage},
		{name: "workflows run", args: []string{"workflows", "run"}, want: workflowsRunUsage},
		{name: "workflows check", args: []string{"workflows", "check", "extra"}, want: "error: unexpected extra args for \"workflows check\"\n" + workflowsCheckUsage},
		{name: "serve", args: []string{"serve", "extra"}, want: "error: unexpected extra args for \"serve\"\n" + serveUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, code := runZVBinaryFailure(t, exe, tempDir, tt.args...)
			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\n%s", got, want, out)
			}
			if got, want := out, tt.want; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryIncompleteCommandsUseStderrOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "root", args: nil, want: usage},
		{name: "demo", args: []string{"demo"}, want: demoUsage},
		{name: "utility", args: []string{"utility"}, want: utilityUsage},
		{name: "compose", args: []string{"compose"}, want: composeUsage},
		{name: "shorts", args: []string{"shorts"}, want: shortsUsage},
		{name: "analysis", args: []string{"analysis"}, want: analysisUsage},
		{name: "gallery", args: []string{"gallery"}, want: galleryUsage},
		{name: "skills", args: []string{"skills"}, want: skillsUsage},
		{name: "workflows", args: []string{"workflows"}, want: workflowsUsage},
		{name: "workflows run", args: []string{"workflows", "run"}, want: workflowsRunUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.want; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryServeRejectsExtraArgsBeforeDelegationEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	tests := []struct {
		name string
		args []string
	}{
		{name: "direct", args: []string{"serve", "extra"}},
		{name: "workflow run", args: []string{"workflows", "run", "serve", "--", "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, tt.name+"-serve-extra.jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `unexpected extra args for "serve"`) {
				t.Fatalf("stderr = %q, want serve extra args error", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
		})
	}
}

func TestZVBinaryGalleryOpenRejectsEmptyPathBeforeOpenEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{name: "direct", args: []string{"gallery", "open", "--path", ""}, wantStderr: "error: missing required flag --path for \"gallery open\"\n"},
		{name: "workflow run", args: []string{"workflows", "run", "gallery-open", "--", "--path", ""}, wantStderr: "error: missing required flag --path for \"gallery open\"\n" + workflowsRunUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openPathLogPath := filepath.Join(tempDir, tt.name+"-gallery-open-empty-path.txt")
			env := []string{"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantStderr; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryGalleryOpenSuccessUsesStdoutOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	tests := []struct {
		name string
		args []string
	}{
		{name: "direct", args: []string{"gallery", "open", "--path", galleryPath}},
		{name: "workflow run", args: []string{"workflows", "run", "gallery-open", "--", "--path", galleryPath}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openPathLogPath := filepath.Join(tempDir, tt.name+"-gallery-open-success.txt")
			env := []string{"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath}

			stdout, stderr := runZVBinarySplitWithEnv(t, exe, tempDir, env, tt.args...)

			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if got, want := stdout, fmt.Sprintf("opened: %s\n", galleryPath); got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
			if got, want := strings.Join(readLines(t, openPathLogPath), "\n"), galleryPath; got != want {
				t.Fatalf("open path log = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryUnknownGroupCommandsShowUsageEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "demo", args: []string{"demo", "wat"}, want: "unknown demo command \"wat\"\n" + demoUsage},
		{name: "utility", args: []string{"utility", "wat"}, want: "unknown utility command \"wat\"\n" + utilityUsage},
		{name: "compose", args: []string{"compose", "wat"}, want: "unknown compose command \"wat\"\n" + composeUsage},
		{name: "shorts", args: []string{"shorts", "wat"}, want: "unknown shorts command \"wat\"\n" + shortsUsage},
		{name: "analysis", args: []string{"analysis", "wat"}, want: "unknown analysis command \"wat\"\n" + analysisUsage},
		{name: "gallery", args: []string{"gallery", "wat"}, want: "unknown gallery command \"wat\"\n" + galleryUsage},
		{name: "skills", args: []string{"skills", "wat"}, want: "unknown skills command \"wat\"\n" + skillsUsage},
		{name: "workflows", args: []string{"workflows", "wat"}, want: "unknown workflows command \"wat\"\n" + workflowsUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, code := runZVBinaryFailure(t, exe, tempDir, tt.args...)
			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\n%s", got, want, out)
			}
			if got, want := out, tt.want; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryUnknownRootCommandUsesStderrOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "wat")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if got, want := stderr, "unknown command \"wat\"\n"+usage; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestZVBinaryJSONCheckFailuresEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	t.Run("skills check", func(t *testing.T) {
		root := filepath.Join(tempDir, "skills-failure")
		writeSkillBody(t, root, "alpha", strings.Join([]string{
			"---",
			"name: alpha",
			`description: "Alpha workflow"`,
			"---",
			"",
			"```powershell",
			`.\bin\zv-parser.exe parse --demo demo.dem --steamid 76561198000000000`,
			"```",
			"",
		}, "\n"))

		tests := []struct {
			name string
			args []string
		}{
			{name: "direct", args: []string{"skills", "check", "--format", "json"}},
			{name: "workflow run", args: []string{"workflows", "run", "skills-check", "--", "--format", "json"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stdout, stderr, code := runZVBinaryFailureSplit(t, exe, root, tt.args...)

				if got, want := code, exitInvalidArgs; got != want {
					t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
				}
				if stderr != "" {
					t.Fatalf("stderr = %q, want empty", stderr)
				}
				var result skillCheckResult
				if err := json.Unmarshal([]byte(stdout), &result); err != nil {
					t.Fatalf("unmarshal skills check failure json: %v\n%s", err, stdout)
				}
				if result.OK {
					t.Fatalf("result.OK = true, want false")
				}
				if got := len(result.Issues); got == 0 {
					t.Fatalf("issues len = 0, want issues")
				}
				var row map[string]json.RawMessage
				if err := json.Unmarshal([]byte(stdout), &row); err != nil {
					t.Fatalf("unmarshal skills check failure json schema: %v\n%s", err, stdout)
				}
				assertJSONKeys(t, "skills check failure json", row, "ok", "skills_checked", "issues")
				assertIssueJSONKeys(t, "skills check failure json issues", row["issues"])
			})
		}
	})

	t.Run("workflow checks", func(t *testing.T) {
		root := filepath.Join(tempDir, "workflow-failure")
		writeSkillBody(t, root, "alpha", strings.Join([]string{
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
		writeWorkflowDocs(t, root)
		appendFile(t, filepath.Join(root, "README.md"), "\n./bin/zv-parser parse --demo demo.dem --steamid 76561198000000000\n")

		tests := []struct {
			name string
			args []string
		}{
			{name: "workflows check", args: []string{"workflows", "check", "--format", "json"}},
			{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--format", "json"}},
			{name: "project check", args: []string{"check", "--format", "json"}},
			{name: "workflow run project check", args: []string{"workflows", "run", "project-check", "--", "--format", "json"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stdout, stderr, code := runZVBinaryFailureSplit(t, exe, root, tt.args...)

				if got, want := code, exitInvalidArgs; got != want {
					t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
				}
				if stderr != "" {
					t.Fatalf("stderr = %q, want empty", stderr)
				}
				var result workflowCheckResult
				if err := json.Unmarshal([]byte(stdout), &result); err != nil {
					t.Fatalf("unmarshal %s failure json: %v\n%s", tt.name, err, stdout)
				}
				if result.OK {
					t.Fatalf("result.OK = true, want false")
				}
				if got := len(result.Issues); got == 0 {
					t.Fatalf("issues len = 0, want issues")
				}
				var row map[string]json.RawMessage
				if err := json.Unmarshal([]byte(stdout), &row); err != nil {
					t.Fatalf("unmarshal %s failure json schema: %v\n%s", tt.name, err, stdout)
				}
				assertJSONKeys(t, tt.name+" failure json", row, "ok", "skills_checked", "workflows_checked", "workflow_docs_checked", "agent_prompt_wrappers_checked", "issues")
				assertIssueJSONKeys(t, tt.name+" failure json issues", row["issues"])
			})
		}
	})
}

func TestZVBinaryJSONSuccessUsesStdoutOnlyEndToEnd(t *testing.T) {
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
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name string
		args []string
	}{
		{name: "skills list", args: []string{"skills", "list", "--format", "json"}},
		{name: "skills check", args: []string{"skills", "check", "--format", "json"}},
		{name: "workflows list", args: []string{"workflows", "list", "--format", "json"}},
		{name: "workflows check", args: []string{"workflows", "check", "--format", "json"}},
		{name: "project check", args: []string{"check", "--format", "json"}},
		{name: "workflow run skills check", args: []string{"workflows", "run", "skills-check", "--", "--format", "json"}},
		{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--format", "json"}},
		{name: "workflow run project check", args: []string{"workflows", "run", "project-check", "--", "--format", "json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := runZVBinarySplit(t, exe, tempDir, tt.args...)

			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if !json.Valid([]byte(stdout)) {
				t.Fatalf("stdout is not valid json: %q", stdout)
			}
		})
	}
}

func TestZVBinaryJSONEqualsFormatUsesStdoutOnlyEndToEnd(t *testing.T) {
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
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name string
		args []string
	}{
		{name: "skills list", args: []string{"skills", "list", "--format=json"}},
		{name: "skills show", args: []string{"skills", "show", "alpha", "--format=json"}},
		{name: "skills check", args: []string{"skills", "check", "--format=json"}},
		{name: "workflows list", args: []string{"workflows", "list", "--format=json"}},
		{name: "workflows show", args: []string{"workflows", "show", "demo-parse", "--format=json"}},
		{name: "workflows check", args: []string{"workflows", "check", "--format=json"}},
		{name: "project check", args: []string{"check", "--format=json"}},
		{name: "workflow run skills check", args: []string{"workflows", "run", "skills-check", "--", "--format=json"}},
		{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--format=json"}},
		{name: "workflow run project check", args: []string{"workflows", "run", "project-check", "--", "--format=json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := runZVBinarySplit(t, exe, tempDir, tt.args...)

			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if !json.Valid([]byte(stdout)) {
				t.Fatalf("stdout is not valid json: %q", stdout)
			}
		})
	}
}

func TestZVBinaryFormattedCommandsRejectExtraArgsEndToEnd(t *testing.T) {
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
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name       string
		args       []string
		wantStderr string
		wantUsage  string
	}{
		{name: "skills list", args: []string{"skills", "list", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "skills list"` + "\n", wantUsage: skillsListUsage},
		{name: "skills show", args: []string{"skills", "show", "alpha", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "skills show"` + "\n", wantUsage: skillsShowUsage},
		{name: "skills check", args: []string{"skills", "check", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "skills check"` + "\n", wantUsage: skillsCheckUsage},
		{name: "workflows list", args: []string{"workflows", "list", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "workflows list"` + "\n", wantUsage: workflowsListUsage},
		{name: "workflows show", args: []string{"workflows", "show", "demo-parse", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "workflows show"` + "\n", wantUsage: workflowsShowUsage},
		{name: "workflows check", args: []string{"workflows", "check", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "workflows check"` + "\n", wantUsage: workflowsCheckUsage},
		{name: "project check", args: []string{"check", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "check"` + "\n", wantUsage: checkUsage},
		{name: "workflow run skills check", args: []string{"workflows", "run", "skills-check", "--", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "skills check"` + "\n", wantUsage: workflowsRunUsage},
		{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "workflows check"` + "\n", wantUsage: workflowsRunUsage},
		{name: "workflow run project check", args: []string{"workflows", "run", "project-check", "--", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "check"` + "\n", wantUsage: workflowsRunUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantStderr+tt.wantUsage; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryFormattedCommandsRejectInvalidFormatEndToEnd(t *testing.T) {
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
	exe := buildZVBinary(t, tempDir)

	commands := []struct {
		name      string
		args      []string
		wantUsage string
	}{
		{name: "skills list", args: []string{"skills", "list"}},
		{name: "skills show", args: []string{"skills", "show", "alpha"}},
		{name: "skills check", args: []string{"skills", "check"}},
		{name: "workflows list", args: []string{"workflows", "list"}},
		{name: "workflows show", args: []string{"workflows", "show", "demo-parse"}},
		{name: "workflows check", args: []string{"workflows", "check"}},
		{name: "project check", args: []string{"check"}},
		{name: "workflow run skills check", args: []string{"workflows", "run", "skills-check", "--"}, wantUsage: workflowsRunUsage},
		{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check", "--"}, wantUsage: workflowsRunUsage},
		{name: "workflow run project check", args: []string{"workflows", "run", "project-check", "--"}, wantUsage: workflowsRunUsage},
	}
	invalidFormats := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{name: "unsupported", args: []string{"--format=yaml"}, wantStderr: `unsupported format "yaml"`},
		{name: "missing value", args: []string{"--format"}, wantStderr: `--format requires a value`},
		{name: "duplicate", args: []string{"--format=json", "--format", "text"}, wantStderr: `duplicate flag --format`},
	}
	for _, command := range commands {
		for _, invalid := range invalidFormats {
			t.Run(command.name+"/"+invalid.name, func(t *testing.T) {
				args := append([]string(nil), command.args...)
				args = append(args, invalid.args...)
				stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, args...)

				if got, want := code, exitInvalidArgs; got != want {
					t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
				}
				if stdout != "" {
					t.Fatalf("stdout = %q, want empty", stdout)
				}
				if got, want := stderr, "error: "+invalid.wantStderr+"\n"+command.wantUsage; got != want {
					t.Fatalf("stderr = %q, want %q", got, want)
				}
			})
		}
	}
}

func TestZVBinaryDiscoveryCommandsRejectUnknownNamesEndToEnd(t *testing.T) {
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
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name            string
		args            []string
		wantExactStderr string
	}{
		{name: "skills show text", args: []string{"skills", "show", "missing"}, wantExactStderr: "error: skill not found: missing\n"},
		{name: "skills show json", args: []string{"skills", "show", "missing", "--format=json"}, wantExactStderr: "error: skill not found: missing\n"},
		{name: "workflows show text", args: []string{"workflows", "show", "missing"}, wantExactStderr: "error: workflow not found: missing\n"},
		{name: "workflows show json", args: []string{"workflows", "show", "missing", "--format=json"}, wantExactStderr: "error: workflow not found: missing\n"},
		{name: "workflows run", args: []string{"workflows", "run", "missing"}, wantExactStderr: "error: workflow not found: missing\n"},
		{name: "workflows run with separator", args: []string{"workflows", "run", "missing", "--"}, wantExactStderr: "error: workflow not found: missing\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantExactStderr; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryTextSuccessUsesStdoutOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"# alpha",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name string
		args []string
	}{
		{name: "root help", args: []string{"--help"}},
		{name: "skills list", args: []string{"skills", "list"}},
		{name: "skills show", args: []string{"skills", "show", "alpha"}},
		{name: "skills check", args: []string{"skills", "check"}},
		{name: "workflows list", args: []string{"workflows", "list"}},
		{name: "workflows show", args: []string{"workflows", "show", "demo-parse"}},
		{name: "workflows check", args: []string{"workflows", "check"}},
		{name: "project check", args: []string{"check"}},
		{name: "workflow run skills check", args: []string{"workflows", "run", "skills-check"}},
		{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check"}},
		{name: "workflow run project check", args: []string{"workflows", "run", "project-check"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := runZVBinarySplit(t, exe, tempDir, tt.args...)

			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if strings.TrimSpace(stdout) == "" {
				t.Fatalf("stdout is empty")
			}
		})
	}
}

func TestZVBinaryTextFailuresUseStderrOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv-parser.exe parse --demo demo.dem --steamid 76561198000000000`,
		"```",
		"",
	}, "\n"))
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name string
		args []string
	}{
		{name: "root unknown", args: []string{"wat"}},
		{name: "group unknown", args: []string{"skills", "wat"}},
		{name: "usage error", args: []string{"skills", "show"}},
		{name: "missing workflow", args: []string{"workflows", "show", "missing"}},
		{name: "missing workflow run flags", args: []string{"workflows", "run", "demo-parse"}},
		{name: "skills check issue", args: []string{"skills", "check"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Fatalf("stderr is empty")
			}
		})
	}
}

func TestZVBinarySkillsCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"# alpha",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run skills-check`,
		`.\bin\zv.exe skills show alpha --format json`,
		`.\bin\zv.exe workflows run workflows-check -- --format json`,
		"```",
		"",
	}, "\n"))

	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name       string
		args       []string
		wantStdout []string
	}{
		{
			name:       "list",
			args:       []string{"skills", "list"},
			wantStdout: []string{"alpha\tAlpha workflow"},
		},
		{
			name:       "show",
			args:       []string{"skills", "show", "alpha"},
			wantStdout: []string{"name: alpha", "# alpha"},
		},
		{
			name:       "show json",
			args:       []string{"skills", "show", "alpha", "--format", "json"},
			wantStdout: []string{`"name": "alpha"`, `"body":`, `# alpha`},
		},
		{
			name:       "check",
			args:       []string{"skills", "check"},
			wantStdout: []string{"OK: 1 skills checked"},
		},
		{
			name:       "list json",
			args:       []string{"skills", "list", "--format", "json"},
			wantStdout: []string{`"name": "alpha"`, `"description": "Alpha workflow"`},
		},
		{
			name:       "check json",
			args:       []string{"skills", "check", "--format", "json"},
			wantStdout: []string{`"ok": true`, `"skills_checked": 1`, `"issues": []`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runZVBinary(t, exe, tempDir, tt.args...)
			for _, want := range tt.wantStdout {
				if !strings.Contains(out, want) {
					t.Fatalf("output = %q, want %q", out, want)
				}
			}
		})
	}
}

func TestZVBinaryWorkflowsCheckEndToEnd(t *testing.T) {
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

	exe := buildZVBinary(t, tempDir)
	out := runZVBinary(t, exe, tempDir, "workflows", "check")
	want := fmt.Sprintf("OK: 1 skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked", len(workflowCatalog()), len(workflowDocs()), len(agentPromptWrapperFixtures()))
	if !strings.Contains(out, want) {
		t.Fatalf("output = %q, want workflow OK count", out)
	}
	checkOut := runZVBinary(t, exe, tempDir, "check")
	if !strings.Contains(checkOut, want) {
		t.Fatalf("output = %q, want project OK count", checkOut)
	}

	jsonOut := runZVBinary(t, exe, tempDir, "workflows", "check", "--format", "json")
	var result workflowCheckResult
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("unmarshal workflows check json: %v\n%s", err, jsonOut)
	}
	if !result.OK {
		t.Fatalf("result.OK = false, want true: %#v", result)
	}
	if got, want := result.WorkflowsChecked, len(workflowCatalog()); got != want {
		t.Fatalf("result.WorkflowsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowDocsChecked, len(workflowDocs()); got != want {
		t.Fatalf("result.WorkflowDocsChecked = %d, want %d", got, want)
	}
	if got, want := result.AgentPromptWrappersChecked, len(agentPromptWrapperFixtures()); got != want {
		t.Fatalf("result.AgentPromptWrappersChecked = %d, want %d", got, want)
	}

	runJSONOut := runZVBinary(t, exe, tempDir, "workflows", "run", "workflows-check", "--", "--format", "json")
	var runResult workflowCheckResult
	if err := json.Unmarshal([]byte(runJSONOut), &runResult); err != nil {
		t.Fatalf("unmarshal workflows run check json: %v\n%s", err, runJSONOut)
	}
	if !runResult.OK {
		t.Fatalf("runResult.OK = false, want true: %#v", runResult)
	}
	if got, want := runResult.WorkflowsChecked, len(workflowCatalog()); got != want {
		t.Fatalf("runResult.WorkflowsChecked = %d, want %d", got, want)
	}

	projectJSONOut := runZVBinary(t, exe, tempDir, "workflows", "run", "project-check", "--", "--format", "json")
	var projectResult workflowCheckResult
	if err := json.Unmarshal([]byte(projectJSONOut), &projectResult); err != nil {
		t.Fatalf("unmarshal project check json: %v\n%s", err, projectJSONOut)
	}
	if !projectResult.OK {
		t.Fatalf("projectResult.OK = false, want true: %#v", projectResult)
	}
}

func TestZVBinaryCurrentRepoCheckEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	root := repoRoot(t)
	wantSkills := currentRepoSkills(t, root)

	jsonOut := runZVBinary(t, exe, root, "check", "--format", "json")
	var result workflowCheckResult
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("unmarshal current repo check json: %v\n%s", err, jsonOut)
	}
	if !result.OK {
		t.Fatalf("current repo check failed: %#v", result)
	}
	if got, want := result.SkillsChecked, len(wantSkills); got != want {
		t.Fatalf("SkillsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowsChecked, len(workflowCatalog()); got != want {
		t.Fatalf("WorkflowsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowDocsChecked, len(workflowDocs()); got != want {
		t.Fatalf("WorkflowDocsChecked = %d, want %d", got, want)
	}
	if got, want := result.AgentPromptWrappersChecked, len(currentAgentPromptWrappers(t, root)); got != want {
		t.Fatalf("AgentPromptWrappersChecked = %d, want %d", got, want)
	}
	if got := len(result.Issues); got != 0 {
		t.Fatalf("issues len = %d, want 0: %#v", got, result.Issues)
	}
}

func TestZVBinaryCurrentRepoWorkflowChecksEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	root := repoRoot(t)
	wantSkills := currentRepoSkills(t, root)
	wantWrappers := currentAgentPromptWrappers(t, root)
	wantText := fmt.Sprintf("OK: %d skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked\n", len(wantSkills), len(workflowCatalog()), len(workflowDocs()), len(wantWrappers))

	workflowText, workflowTextStderr := runZVBinarySplit(t, exe, root, "workflows", "check")
	if workflowTextStderr != "" {
		t.Fatalf("workflows check stderr = %q, want empty", workflowTextStderr)
	}
	if got, want := workflowText, wantText; got != want {
		t.Fatalf("workflows check text = %q, want %q", got, want)
	}

	projectText, projectTextStderr := runZVBinarySplit(t, exe, root, "check")
	if projectTextStderr != "" {
		t.Fatalf("check stderr = %q, want empty", projectTextStderr)
	}
	if got, want := projectText, workflowText; got != want {
		t.Fatalf("check text = %q, want workflows check text %q", got, want)
	}

	runWorkflowText, runWorkflowTextStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "workflows-check")
	if runWorkflowTextStderr != "" {
		t.Fatalf("workflows run workflows-check stderr = %q, want empty", runWorkflowTextStderr)
	}
	if got, want := runWorkflowText, workflowText; got != want {
		t.Fatalf("workflow run workflows-check text = %q, want direct output %q", got, want)
	}

	runProjectText, runProjectTextStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "project-check")
	if runProjectTextStderr != "" {
		t.Fatalf("workflows run project-check stderr = %q, want empty", runProjectTextStderr)
	}
	if got, want := runProjectText, projectText; got != want {
		t.Fatalf("workflow run project-check text = %q, want direct output %q", got, want)
	}

	workflowJSON, workflowJSONStderr := runZVBinarySplit(t, exe, root, "workflows", "check", "--format", "json")
	if workflowJSONStderr != "" {
		t.Fatalf("workflows check json stderr = %q, want empty", workflowJSONStderr)
	}
	result := decodeWorkflowCheckResult(t, workflowJSON)
	if !result.OK {
		t.Fatalf("workflows check json ok = false: %#v", result)
	}
	if got, want := result.SkillsChecked, len(wantSkills); got != want {
		t.Fatalf("SkillsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowsChecked, len(workflowCatalog()); got != want {
		t.Fatalf("WorkflowsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowDocsChecked, len(workflowDocs()); got != want {
		t.Fatalf("WorkflowDocsChecked = %d, want %d", got, want)
	}
	if got, want := result.AgentPromptWrappersChecked, len(wantWrappers); got != want {
		t.Fatalf("AgentPromptWrappersChecked = %d, want %d", got, want)
	}
	if got := len(result.Issues); got != 0 {
		t.Fatalf("issues len = %d, want 0: %#v", got, result.Issues)
	}

	projectJSON, projectJSONStderr := runZVBinarySplit(t, exe, root, "check", "--format", "json")
	if projectJSONStderr != "" {
		t.Fatalf("check json stderr = %q, want empty", projectJSONStderr)
	}
	if got, want := projectJSON, workflowJSON; got != want {
		t.Fatalf("check json = %q, want workflows check json %q", got, want)
	}

	runWorkflowJSON, runWorkflowJSONStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "workflows-check", "--", "--format", "json")
	if runWorkflowJSONStderr != "" {
		t.Fatalf("workflows run workflows-check json stderr = %q, want empty", runWorkflowJSONStderr)
	}
	if got, want := runWorkflowJSON, workflowJSON; got != want {
		t.Fatalf("workflow run workflows-check json = %q, want direct output %q", got, want)
	}

	runProjectJSON, runProjectJSONStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "project-check", "--", "--format", "json")
	if runProjectJSONStderr != "" {
		t.Fatalf("workflows run project-check json stderr = %q, want empty", runProjectJSONStderr)
	}
	if got, want := runProjectJSON, projectJSON; got != want {
		t.Fatalf("workflow run project-check json = %q, want direct output %q", got, want)
	}
}

func TestZVBinaryCurrentRepoSkillsDiscoveryEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	wantSkills := currentRepoSkills(t, root)

	listText := runZVBinary(t, exe, root, "skills", "list")
	for _, skill := range wantSkills {
		if !strings.Contains(listText, skill.Name) {
			t.Fatalf("skills list output = %q, want skill %q", listText, skill.Name)
		}
		if skill.Description != "" && !strings.Contains(listText, skill.Description) {
			t.Fatalf("skills list output = %q, want description %q", listText, skill.Description)
		}
	}
	if got, want := listText, skillListText(wantSkills); got != want {
		t.Fatalf("skills list text = %q, want %q", got, want)
	}

	listJSON := runZVBinary(t, exe, root, "skills", "list", "--format", "json")
	var gotSkills []skillInfo
	if err := json.Unmarshal([]byte(listJSON), &gotSkills); err != nil {
		t.Fatalf("unmarshal skills list json: %v\n%s", err, listJSON)
	}
	if got, want := len(gotSkills), len(wantSkills); got != want {
		t.Fatalf("skills list len = %d, want %d: %#v", got, want, gotSkills)
	}
	for i, want := range wantSkills {
		got := gotSkills[i]
		if got.Name != want.Name || got.Description != want.Description {
			t.Fatalf("skill %d = %#v, want %#v", i, got, want)
		}
		if strings.Contains(listJSON, want.Path) {
			t.Fatalf("skills list json leaked local path %q", want.Path)
		}
	}
	if got, want := skillNames(gotSkills), skillNames(wantSkills); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("skills list json order = %#v, want %#v", got, want)
	}

	for _, skill := range wantSkills {
		showJSON := runZVBinary(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		wantBody := readFileString(t, skill.Path)
		if detail.Name != skill.Name || detail.Description != skill.Description || detail.Body != wantBody {
			t.Fatalf("skills show %s = %#v, want name=%q description=%q body from %s", skill.Name, detail, skill.Name, skill.Description, skill.Path)
		}
		showText := runZVBinary(t, exe, root, "skills", "show", skill.Name)
		if strings.TrimRight(showText, "\n") != strings.TrimRight(wantBody, "\n") {
			t.Fatalf("skills show text for %s did not match %s", skill.Name, skill.Path)
		}
	}
}

func TestZVBinaryCurrentRepoPublicJSONSchemasEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	wantSkills := currentRepoSkills(t, root)

	skillsListJSON, skillsListStderr := runZVBinarySplit(t, exe, root, "skills", "list", "--format", "json")
	if skillsListStderr != "" {
		t.Fatalf("skills list json stderr = %q, want empty", skillsListStderr)
	}
	var skillListRows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(skillsListJSON), &skillListRows); err != nil {
		t.Fatalf("unmarshal skills list json schema: %v\n%s", err, skillsListJSON)
	}
	if got, want := len(skillListRows), len(wantSkills); got != want {
		t.Fatalf("skills list json rows = %d, want %d", got, want)
	}
	for i, row := range skillListRows {
		assertJSONKeys(t, fmt.Sprintf("skills list json row %d", i), row, "name", "description")
	}

	for _, skill := range wantSkills {
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("skills show json stderr for %s = %q, want empty", skill.Name, showStderr)
		}
		var showRow map[string]json.RawMessage
		if err := json.Unmarshal([]byte(showJSON), &showRow); err != nil {
			t.Fatalf("unmarshal skills show json schema for %s: %v\n%s", skill.Name, err, showJSON)
		}
		assertJSONKeys(t, "skills show json "+skill.Name, showRow, "name", "description", "body")
		if strings.Contains(showJSON, skill.Path) {
			t.Fatalf("skills show json for %s leaked local path %q", skill.Name, skill.Path)
		}
	}

	workflowsListJSON, workflowsListStderr := runZVBinarySplit(t, exe, root, "workflows", "list", "--format", "json")
	if workflowsListStderr != "" {
		t.Fatalf("workflows list json stderr = %q, want empty", workflowsListStderr)
	}
	var workflowListRows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(workflowsListJSON), &workflowListRows); err != nil {
		t.Fatalf("unmarshal workflows list json schema: %v\n%s", err, workflowsListJSON)
	}
	if got, want := len(workflowListRows), len(workflowCatalog()); got != want {
		t.Fatalf("workflows list json rows = %d, want %d", got, want)
	}
	for i, row := range workflowListRows {
		assertJSONKeys(t, fmt.Sprintf("workflows list json row %d", i), row, "name", "description", "command", "run_command")
	}

	for _, workflow := range workflowCatalog() {
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "workflows", "show", workflow.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("workflows show json stderr for %s = %q, want empty", workflow.Name, showStderr)
		}
		var showRow map[string]json.RawMessage
		if err := json.Unmarshal([]byte(showJSON), &showRow); err != nil {
			t.Fatalf("unmarshal workflows show json schema for %s: %v\n%s", workflow.Name, err, showJSON)
		}
		assertJSONKeys(t, "workflows show json "+workflow.Name, showRow, "name", "description", "command", "run_command")
	}

	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "skills check json", args: []string{"skills", "check", "--format", "json"}},
		{name: "workflow run skills check json", args: []string{"workflows", "run", "skills-check", "--", "--format", "json"}},
	} {
		stdout, stderr := runZVBinarySplit(t, exe, root, tt.args...)
		if stderr != "" {
			t.Fatalf("%s stderr = %q, want empty", tt.name, stderr)
		}
		var row map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stdout), &row); err != nil {
			t.Fatalf("unmarshal %s schema: %v\n%s", tt.name, err, stdout)
		}
		assertJSONKeys(t, tt.name, row, "ok", "skills_checked", "issues")
	}

	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "workflows check json", args: []string{"workflows", "check", "--format", "json"}},
		{name: "project check json", args: []string{"check", "--format", "json"}},
		{name: "workflow run workflows check json", args: []string{"workflows", "run", "workflows-check", "--", "--format", "json"}},
		{name: "workflow run project check json", args: []string{"workflows", "run", "project-check", "--", "--format", "json"}},
	} {
		stdout, stderr := runZVBinarySplit(t, exe, root, tt.args...)
		if stderr != "" {
			t.Fatalf("%s stderr = %q, want empty", tt.name, stderr)
		}
		var row map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stdout), &row); err != nil {
			t.Fatalf("unmarshal %s schema: %v\n%s", tt.name, err, stdout)
		}
		assertJSONKeys(t, tt.name, row, "ok", "skills_checked", "workflows_checked", "workflow_docs_checked", "agent_prompt_wrappers_checked", "issues")
	}
}

func TestZVBinaryCurrentRepoDiscoveryTextUsesStdoutOnlyEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	wantSkills := currentRepoSkills(t, root)

	skillsList, skillsListStderr := runZVBinarySplit(t, exe, root, "skills", "list")
	if skillsListStderr != "" {
		t.Fatalf("skills list stderr = %q, want empty", skillsListStderr)
	}
	if got, want := skillsList, skillListText(wantSkills); got != want {
		t.Fatalf("skills list stdout = %q, want %q", got, want)
	}

	for _, skill := range wantSkills {
		showText, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name)
		if showStderr != "" {
			t.Fatalf("skills show %s stderr = %q, want empty", skill.Name, showStderr)
		}
		wantBody := readFileString(t, skill.Path)
		if strings.TrimRight(showText, "\n") != strings.TrimRight(wantBody, "\n") {
			t.Fatalf("skills show %s stdout did not match %s", skill.Name, skill.Path)
		}
	}

	workflowsList, workflowsListStderr := runZVBinarySplit(t, exe, root, "workflows", "list")
	if workflowsListStderr != "" {
		t.Fatalf("workflows list stderr = %q, want empty", workflowsListStderr)
	}
	if got, want := workflowsList, workflowListText(workflowCatalog()); got != want {
		t.Fatalf("workflows list stdout = %q, want %q", got, want)
	}

	for _, workflow := range workflowCatalog() {
		showText, showStderr := runZVBinarySplit(t, exe, root, "workflows", "show", workflow.Name)
		if showStderr != "" {
			t.Fatalf("workflows show %s stderr = %q, want empty", workflow.Name, showStderr)
		}
		if got, want := showText, workflowShowText(workflow); got != want {
			t.Fatalf("workflows show %s stdout = %q, want %q", workflow.Name, got, want)
		}
	}
}

func TestZVBinaryCurrentRepoSkillsCheckEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	wantSkills := currentRepoSkills(t, root)

	wantText := fmt.Sprintf("OK: %d skills checked\n", len(wantSkills))
	directText, directTextStderr := runZVBinarySplit(t, exe, root, "skills", "check")
	if directTextStderr != "" {
		t.Fatalf("skills check stderr = %q, want empty", directTextStderr)
	}
	if got, want := directText, wantText; got != want {
		t.Fatalf("skills check text = %q, want %q", got, want)
	}

	runText, runTextStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "skills-check")
	if runTextStderr != "" {
		t.Fatalf("workflows run skills-check stderr = %q, want empty", runTextStderr)
	}
	if got, want := runText, directText; got != want {
		t.Fatalf("workflow run skills-check text = %q, want direct output %q", got, want)
	}

	directJSON, directJSONStderr := runZVBinarySplit(t, exe, root, "skills", "check", "--format", "json")
	if directJSONStderr != "" {
		t.Fatalf("skills check json stderr = %q, want empty", directJSONStderr)
	}
	var directResult skillCheckResult
	if err := json.Unmarshal([]byte(directJSON), &directResult); err != nil {
		t.Fatalf("unmarshal skills check json: %v\n%s", err, directJSON)
	}
	if !directResult.OK {
		t.Fatalf("skills check json ok = false: %#v", directResult)
	}
	if got, want := directResult.SkillsChecked, len(wantSkills); got != want {
		t.Fatalf("skills checked = %d, want %d", got, want)
	}
	if got := len(directResult.Issues); got != 0 {
		t.Fatalf("skills check issues len = %d, want 0: %#v", got, directResult.Issues)
	}

	runJSON, runJSONStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "skills-check", "--", "--format", "json")
	if runJSONStderr != "" {
		t.Fatalf("workflows run skills-check json stderr = %q, want empty", runJSONStderr)
	}
	if got, want := runJSON, directJSON; got != want {
		t.Fatalf("workflow run skills-check json = %q, want direct output %q", got, want)
	}
}

func TestZVBinaryCurrentRepoDiscoveryJSONEqualsFormatMatchesSpaceFormatEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name      string
		spaceArgs []string
		eqArgs    []string
	}{
		{
			name:      "skills list",
			spaceArgs: []string{"skills", "list", "--format", "json"},
			eqArgs:    []string{"skills", "list", "--format=json"},
		},
		{
			name:      "skills show",
			spaceArgs: []string{"skills", "show", "zackvideo-cs2-utility-shorts", "--format", "json"},
			eqArgs:    []string{"skills", "show", "zackvideo-cs2-utility-shorts", "--format=json"},
		},
		{
			name:      "workflows list",
			spaceArgs: []string{"workflows", "list", "--format", "json"},
			eqArgs:    []string{"workflows", "list", "--format=json"},
		},
		{
			name:      "workflows show",
			spaceArgs: []string{"workflows", "show", "demo-parse", "--format", "json"},
			eqArgs:    []string{"workflows", "show", "demo-parse", "--format=json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spaceStdout, spaceStderr := runZVBinarySplit(t, exe, root, tt.spaceArgs...)
			if spaceStderr != "" {
				t.Fatalf("space format stderr = %q, want empty", spaceStderr)
			}
			eqStdout, eqStderr := runZVBinarySplit(t, exe, root, tt.eqArgs...)
			if eqStderr != "" {
				t.Fatalf("equals format stderr = %q, want empty", eqStderr)
			}
			if got, want := eqStdout, spaceStdout; got != want {
				t.Fatalf("equals format stdout = %q, want space format stdout %q", got, want)
			}
			if !json.Valid([]byte(eqStdout)) {
				t.Fatalf("equals format stdout is not valid json: %q", eqStdout)
			}
		})
	}
}

func TestZVBinaryCurrentRepoCheckJSONEqualsFormatMatchesSpaceFormatEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name      string
		spaceArgs []string
		eqArgs    []string
	}{
		{
			name:      "skills check",
			spaceArgs: []string{"skills", "check", "--format", "json"},
			eqArgs:    []string{"skills", "check", "--format=json"},
		},
		{
			name:      "workflows check",
			spaceArgs: []string{"workflows", "check", "--format", "json"},
			eqArgs:    []string{"workflows", "check", "--format=json"},
		},
		{
			name:      "project check",
			spaceArgs: []string{"check", "--format", "json"},
			eqArgs:    []string{"check", "--format=json"},
		},
		{
			name:      "workflow run skills check",
			spaceArgs: []string{"workflows", "run", "skills-check", "--", "--format", "json"},
			eqArgs:    []string{"workflows", "run", "skills-check", "--", "--format=json"},
		},
		{
			name:      "workflow run workflows check",
			spaceArgs: []string{"workflows", "run", "workflows-check", "--", "--format", "json"},
			eqArgs:    []string{"workflows", "run", "workflows-check", "--", "--format=json"},
		},
		{
			name:      "workflow run project check",
			spaceArgs: []string{"workflows", "run", "project-check", "--", "--format", "json"},
			eqArgs:    []string{"workflows", "run", "project-check", "--", "--format=json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spaceStdout, spaceStderr := runZVBinarySplit(t, exe, root, tt.spaceArgs...)
			if spaceStderr != "" {
				t.Fatalf("space format stderr = %q, want empty", spaceStderr)
			}
			eqStdout, eqStderr := runZVBinarySplit(t, exe, root, tt.eqArgs...)
			if eqStderr != "" {
				t.Fatalf("equals format stderr = %q, want empty", eqStderr)
			}
			if got, want := eqStdout, spaceStdout; got != want {
				t.Fatalf("equals format stdout = %q, want space format stdout %q", got, want)
			}
			if !json.Valid([]byte(eqStdout)) {
				t.Fatalf("equals format stdout is not valid json: %q", eqStdout)
			}
		})
	}
}

func TestZVBinaryWorkflowRunsMatchInternalCommandsEndToEnd(t *testing.T) {
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
	exe := buildZVBinary(t, tempDir)

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	tests := []struct {
		name       string
		directArgs []string
		runArgs    []string
	}{
		{
			name:       "gallery-open",
			directArgs: []string{"gallery", "open", "--path", galleryPath},
			runArgs:    []string{"workflows", "run", "gallery-open", "--", "--path", galleryPath},
		},
		{
			name:       "skills-check",
			directArgs: []string{"skills", "check", "--format", "json"},
			runArgs:    []string{"workflows", "run", "skills-check", "--", "--format", "json"},
		},
		{
			name:       "workflows-check",
			directArgs: []string{"workflows", "check", "--format", "json"},
			runArgs:    []string{"workflows", "run", "workflows-check", "--", "--format", "json"},
		},
		{
			name:       "project-check",
			directArgs: []string{"check", "--format", "json"},
			runArgs:    []string{"workflows", "run", "project-check", "--", "--format", "json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directOpenLog := filepath.Join(tempDir, tt.name+"-direct-open.txt")
			runOpenLog := filepath.Join(tempDir, tt.name+"-run-open.txt")

			directOut := runZVBinaryWithEnv(t, exe, tempDir, []string{
				"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
			}, tt.directArgs...)
			runOut := runZVBinaryWithEnv(t, exe, tempDir, []string{
				"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
			}, tt.runArgs...)

			if got, want := runOut, directOut; got != want {
				t.Fatalf("workflow run output = %q, want direct output %q", got, want)
			}
			if tt.name != "gallery-open" {
				return
			}
			if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
				t.Fatalf("workflow run open path log = %q, want direct log %q", got, want)
			}
		})
	}
}

func TestRunCanonicalSkillWorkflowDelegatesThroughLocalBinEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	installFakeDelegatedSubcommands(t, binDir)

	logPath := filepath.Join(tempDir, "calls.jsonl")
	t.Setenv("ZV_FAKE_SUBCOMMAND", "1")
	t.Setenv("ZV_FAKE_SUBCOMMAND_LOG", logPath)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	tests := []struct {
		name           string
		argv           []string
		wantExecutable string
		wantArgs       []string
	}{
		{
			name:           "demo parse",
			argv:           []string{"zv", "demo", "parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--segment-mode", "utility", "--out", "run/plan.json"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--segment-mode", "utility", "--out", "run/plan.json"},
		},
		{
			name:           "utility audit",
			argv:           []string{"zv", "utility", "audit", "--plan", "run/plan.json", "--lineup-catalog", "data/lineups", "--out", "run/utility-audit.csv"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"utility-audit", "--plan", "run/plan.json", "--lineup-catalog", "data/lineups", "--out", "run/utility-audit.csv"},
		},
		{
			name:           "record",
			argv:           []string{"zv", "record", "--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/recording", "--dry-run"},
			wantExecutable: executableName("zv-recorder"),
			wantArgs:       []string{"--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/recording", "--dry-run"},
		},
		{
			name:           "shorts render",
			argv:           []string{"zv", "shorts", "render", "--recording-result", "run/recording/recording-result.json", "--killplan", "run/plan.json", "--out", "run/shorts", "--preset", "smoke-lineups"},
			wantExecutable: executableName("zv-editor"),
			wantArgs:       []string{"--recording-result", "run/recording/recording-result.json", "--killplan", "run/plan.json", "--out", "run/shorts", "--preset", "smoke-lineups"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			code := Run(tt.argv, &stdout, &stderr, nil, osCommandRunner{})
			if got, want := code, exitSuccess; got != want {
				t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
			}
		})
	}

	calls := readFakeSubcommandCalls(t, logPath)
	if got, want := len(calls), len(tests); got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, calls)
	}
	for i, tt := range tests {
		call := calls[i]
		if got, want := call.Executable, tt.wantExecutable; got != want {
			t.Fatalf("call %d executable = %q, want %q", i, got, want)
		}
		if got, want := strings.Join(call.Args, "\x00"), strings.Join(tt.wantArgs, "\x00"); got != want {
			t.Fatalf("call %d args = %#v, want %#v", i, call.Args, tt.wantArgs)
		}
	}
}
