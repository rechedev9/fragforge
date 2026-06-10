package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepoSkillsUseUnifiedCLI(t *testing.T) {
	root := repoRoot(t)
	skillsDir := filepath.Join(root, ".codex", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}

	foundSkill := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		foundSkill = true
		path := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(b)
		if !strings.Contains(body, `.\bin\zv.exe`) {
			t.Errorf("%s does not document the unified zv CLI", path)
		}
		if !strings.Contains(body, `.\bin\zv.exe workflows run`) {
			t.Errorf("%s does not document a cataloged workflow run command", path)
		}
		for _, legacy := range legacySkillBinaries() {
			if strings.Contains(body, legacy) {
				t.Errorf("%s documents legacy direct binary %s", path, legacy)
			}
		}
	}
	if !foundSkill {
		t.Fatalf("no repo skills found in %s", skillsDir)
	}
}

func TestGoGateRunsProjectCheck(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "scripts", "go-gate.sh")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(b)
	for _, want := range []string{
		"== zv check ==",
		"go run ./cmd/zv check",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s does not contain %q", path, want)
		}
	}
}

func TestGoBashScriptsBootstrapWindowsGoToolchain(t *testing.T) {
	root := repoRoot(t)
	tests := []string{
		filepath.Join(root, "scripts", "go-gate.sh"),
		filepath.Join(root, "scripts", "go-format-changed.sh"),
	}
	for _, path := range tests {
		t.Run(filepath.Base(path), func(t *testing.T) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			body := string(b)
			for _, want := range []string{
				"source scripts/go-env.sh",
				"ensure_go_toolchain",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s does not contain %q", path, want)
				}
			}
		})
	}

	path := filepath.Join(root, "scripts", "go-env.sh")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(b)
	for _, want := range []string{
		"ensure_go_toolchain()",
		"/c/Program Files/Go/bin",
		"/mnt/c/Program Files/Go/bin",
		"command -v go.exe",
		"command -v gofmt.exe",
		"go not found: install Go or add it to PATH",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s does not contain %q", path, want)
		}
	}
}

func TestCodexHarnessRunsProjectCheck(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "scripts", "check-codex-harness.sh")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(b)
	for _, want := range []string{
		"source scripts/go-env.sh",
		"ensure_go_toolchain",
		"mapfile -t shell_scripts",
		"find scripts -maxdepth 1 -type f -name '*.sh' | sort",
		`bash -n "${shell_scripts[@]}"`,
		"== FragForge workflow contract ==",
		"go run ./cmd/zv check",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s does not contain %q", path, want)
		}
	}
}

func TestCodexHarnessExecutesWorkflowContractEndToEnd(t *testing.T) {
	root := repoRoot(t)
	fakeBin := t.TempDir()
	fakeCodex := filepath.Join(fakeBin, "codex")
	writeFile(t, fakeCodex, strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`printf '%s\n' 'FragForge is a deterministic CS2 demo-to-video pipeline'`,
		`printf '%s\n' 'AGENTS.md'`,
	}, "\n"))
	if err := os.Chmod(fakeCodex, 0o755); err != nil {
		t.Fatalf("chmod fake codex: %v", err)
	}

	cmd := exec.Command("bash", "-c", "export PATH="+shellQuote(bashPath(fakeBin))+":\"$PATH\"; scripts/check-codex-harness.sh")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scripts/check-codex-harness.sh: %v\n%s", err, out)
	}
	body := string(out)
	for _, want := range []string{
		"== shell syntax ==",
		"== Codex sees AGENTS.md ==",
		"== FragForge workflow contract ==",
		"OK: 6 skills, 15 workflows, 15 workflow docs, and 19 agent prompt wrappers checked",
		"OK: Codex harness is wired",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("check-codex-harness output = %q, want %q", body, want)
		}
	}
}

func TestRootShellScriptsParseEndToEnd(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "scripts"))
	if err != nil {
		t.Fatalf("read scripts dir: %v", err)
	}
	args := []string{"-n"}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sh" {
			continue
		}
		args = append(args, filepath.ToSlash(filepath.Join("scripts", entry.Name())))
	}
	if len(args) == 1 {
		t.Fatalf("no root shell scripts found")
	}
	cmd := exec.Command("bash", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestRootPowerShellScriptsParseEndToEnd(t *testing.T) {
	root := repoRoot(t)
	powerShell, ok := findPowerShell()
	if !ok {
		t.Skip("powershell or pwsh not found")
	}
	entries, err := os.ReadDir(filepath.Join(root, "scripts"))
	if err != nil {
		t.Fatalf("read scripts dir: %v", err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".ps1" {
			continue
		}
		files = append(files, filepath.Join("scripts", entry.Name()))
	}
	if len(files) == 0 {
		t.Fatalf("no root PowerShell scripts found")
	}

	script := strings.Join([]string{
		"param([string[]]$Paths)",
		"$failed = $false",
		"foreach ($path in $Paths) {",
		"  $tokens = $null",
		"  $errors = $null",
		"  [System.Management.Automation.Language.Parser]::ParseFile((Resolve-Path -LiteralPath $path).Path, [ref]$tokens, [ref]$errors) | Out-Null",
		"  if ($errors.Count -gt 0) {",
		"    Write-Error (\"${path}: \" + (($errors | ForEach-Object { $_.Message }) -join '; '))",
		"    $failed = $true",
		"  }",
		"}",
		"if ($failed) { exit 1 }",
	}, "\n")
	scriptPath := filepath.Join(t.TempDir(), "parse-powershell-scripts.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write parse script: %v", err)
	}

	args := []string{"-NoProfile"}
	if strings.Contains(strings.ToLower(filepath.Base(powerShell)), "powershell") {
		args = append(args, "-ExecutionPolicy", "Bypass")
	}
	args = append(args, "-File", scriptPath)
	args = append(args, files...)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, powerShell, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s %s timed out\n%s", powerShell, strings.Join(args, " "), out)
	}
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", powerShell, strings.Join(args, " "), err, out)
	}
}

func TestCodexPromptWrappersHaveExistingPromptsAndDocs(t *testing.T) {
	root := repoRoot(t)
	scriptsDir := filepath.Join(root, "scripts")
	promptsDir := filepath.Join(root, ".codex", "prompts")

	promptEntries, err := os.ReadDir(promptsDir)
	if err != nil {
		t.Fatalf("read prompts dir: %v", err)
	}
	prompts := make(map[string]bool)
	for _, entry := range promptEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		prompts[filepath.ToSlash(filepath.Join(".codex", "prompts", entry.Name()))] = false
	}

	readme, err := os.ReadFile(filepath.Join(root, ".codex", "README.md"))
	if err != nil {
		t.Fatalf("read .codex/README.md: %v", err)
	}
	readmeBody := string(readme)
	runner := filepath.Join(scriptsDir, "codex-run.sh")
	if _, err := os.Stat(runner); err != nil {
		t.Fatalf("missing codex runner %s: %v", runner, err)
	}
	if !strings.Contains(readmeBody, "scripts/codex-run.sh") {
		t.Fatalf(".codex/README.md does not document runner scripts/codex-run.sh")
	}

	wrappers, err := filepath.Glob(filepath.Join(scriptsDir, "codex*.sh"))
	if err != nil {
		t.Fatalf("glob codex wrappers: %v", err)
	}
	var mappedWrappers int
	for _, wrapper := range wrappers {
		if filepath.Base(wrapper) == "codex-run.sh" {
			continue
		}
		b, err := os.ReadFile(wrapper)
		if err != nil {
			t.Fatalf("read %s: %v", wrapper, err)
		}
		prompt, ok := codexWrapperPromptPath(string(b))
		if !ok {
			t.Fatalf("%s does not exec scripts/codex-run.sh with a prompt", wrapper)
		}
		if _, ok := prompts[prompt]; !ok {
			t.Fatalf("%s references missing prompt %s", wrapper, prompt)
		}
		prompts[prompt] = true
		mappedWrappers++
		if name := filepath.ToSlash(filepath.Join("scripts", filepath.Base(wrapper))); !strings.Contains(readmeBody, name) {
			t.Fatalf(".codex/README.md does not document wrapper %s", name)
		}
	}
	if mappedWrappers == 0 {
		t.Fatalf("no codex prompt wrappers found")
	}
	for prompt, used := range prompts {
		if !used {
			t.Fatalf("prompt %s has no codex wrapper", prompt)
		}
	}
}

func TestClaudePromptWrappersHaveExistingCommandsAndDocs(t *testing.T) {
	root := repoRoot(t)
	scriptsDir := filepath.Join(root, "scripts")
	commandsDir := filepath.Join(root, ".claude", "commands")

	commandEntries, err := os.ReadDir(commandsDir)
	if err != nil {
		t.Fatalf("read commands dir: %v", err)
	}
	commands := make(map[string]bool)
	for _, entry := range commandEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		commands[filepath.ToSlash(filepath.Join(".claude", "commands", entry.Name()))] = false
	}

	readme, err := os.ReadFile(filepath.Join(root, ".claude", "README.md"))
	if err != nil {
		t.Fatalf("read .claude/README.md: %v", err)
	}
	readmeBody := string(readme)
	runner := filepath.Join(scriptsDir, "claude-run.sh")
	if _, err := os.Stat(runner); err != nil {
		t.Fatalf("missing claude runner %s: %v", runner, err)
	}
	if !strings.Contains(readmeBody, "scripts/claude-run.sh") {
		t.Fatalf(".claude/README.md does not document runner scripts/claude-run.sh")
	}

	wrappers, err := filepath.Glob(filepath.Join(scriptsDir, "claude-zv-*.sh"))
	if err != nil {
		t.Fatalf("glob claude wrappers: %v", err)
	}
	var mappedWrappers int
	for _, wrapper := range wrappers {
		b, err := os.ReadFile(wrapper)
		if err != nil {
			t.Fatalf("read %s: %v", wrapper, err)
		}
		command, ok := claudeWrapperCommandPath(string(b))
		if !ok {
			t.Fatalf("%s does not exec scripts/claude-run.sh with a command prompt", wrapper)
		}
		if _, ok := commands[command]; !ok {
			t.Fatalf("%s references missing claude command %s", wrapper, command)
		}
		commands[command] = true
		mappedWrappers++
		if name := filepath.ToSlash(filepath.Join("scripts", filepath.Base(wrapper))); !strings.Contains(readmeBody, name) {
			t.Fatalf(".claude/README.md does not document wrapper %s", name)
		}
	}
	if mappedWrappers == 0 {
		t.Fatalf("no claude prompt wrappers found")
	}
	for command, used := range commands {
		if !used {
			t.Fatalf("claude command %s has no wrapper", command)
		}
	}
}

func TestCodexPromptWrappersDryRunEndToEnd(t *testing.T) {
	root := repoRoot(t)
	for _, fixture := range currentCodexPromptWrappers(t, root) {
		t.Run(filepath.Base(fixture.wrapper), func(t *testing.T) {
			promptBody := readFileString(t, filepath.Join(root, filepath.FromSlash(fixture.prompt)))
			out := runAgentWrapperDryRun(t, root, fixture.wrapper, []string{"CODEX_DRY_RUN=1"}, "dry run task")
			for _, want := range []string{
				"repo:",
				"prompt:",
				filepath.ToSlash(fixture.prompt),
				"command: codex",
				"--- final prompt ---",
				"## User task",
				"dry run task",
			} {
				if !strings.Contains(filepath.ToSlash(out), filepath.ToSlash(want)) {
					t.Fatalf("output for %s = %q, want %q", fixture.wrapper, out, want)
				}
			}
			if !strings.Contains(normalizedText(out), strings.TrimRight(normalizedText(promptBody), "\n")) {
				t.Fatalf("output for %s did not include prompt body from %s", fixture.wrapper, fixture.prompt)
			}
		})
	}
}

func TestClaudePromptWrappersDryRunEndToEnd(t *testing.T) {
	root := repoRoot(t)
	for _, fixture := range currentClaudePromptWrappers(t, root) {
		t.Run(filepath.Base(fixture.wrapper), func(t *testing.T) {
			commandBody := readFileString(t, filepath.Join(root, filepath.FromSlash(fixture.command)))
			firstLine := strings.TrimSpace(strings.SplitN(commandBody, "\n", 2)[0])
			out := runAgentWrapperDryRun(t, root, fixture.wrapper, []string{"CLAUDE_DRY_RUN=1"}, "dry run task")
			for _, want := range []string{
				firstLine,
				"User task:",
				"dry run task",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("output for %s = %q, want %q", fixture.wrapper, out, want)
				}
			}
			if !strings.Contains(normalizedText(out), strings.TrimRight(normalizedText(commandBody), "\n")) {
				t.Fatalf("output for %s did not include command body from %s", fixture.wrapper, fixture.command)
			}
		})
	}
}

func TestCodexRunnerDryRunIncludesStdinContextEndToEnd(t *testing.T) {
	root := repoRoot(t)
	stdout, stderr := runAgentRunnerDryRunWithInput(t, root, "CODEX_DRY_RUN=1", "scripts/codex-run.sh", ".codex/prompts/go-plan.md", "argument task", "stdin task context\n")
	if stderr != "" {
		t.Fatalf("codex-run dry-run stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"--- final prompt ---",
		"## User task",
		"argument task",
		"## Stdin task/context",
		"stdin task context",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("codex-run dry-run stdout = %q, want %q", stdout, want)
		}
	}
}

func TestClaudeRunnerDryRunIncludesStdinContextEndToEnd(t *testing.T) {
	root := repoRoot(t)
	stdout, stderr := runAgentRunnerDryRunWithInput(t, root, "CLAUDE_DRY_RUN=1", "scripts/claude-run.sh", ".claude/commands/zv-plan.md", "argument task", "stdin task context\n")
	if stderr != "" {
		t.Fatalf("claude-run dry-run stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"User task:",
		"argument task",
		"## Stdin task/context",
		"stdin task context",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("claude-run dry-run stdout = %q, want %q", stdout, want)
		}
	}
}

func TestCodexPromptsUseProjectGate(t *testing.T) {
	root := repoRoot(t)
	tests := []struct {
		path string
		want []string
	}{
		{
			path: filepath.Join(root, ".codex", "prompts", "go-tdd.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".codex", "prompts", "go-bugfix.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".codex", "prompts", "go-pr-ready.md"),
			want: []string{
				"scripts/go-gate.sh",
				"scripts/go-gate.sh --no-format",
				"scripts/go-gate.sh --race",
				"scripts/go-gate.sh --security",
			},
		},
		{
			path: filepath.Join(root, ".codex", "prompts", "go-concurrency-review.md"),
			want: []string{
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".codex", "prompts", "go-security-review.md"),
			want: []string{
				"scripts/go-gate.sh --security",
			},
		},
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			b, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			body := string(b)
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Fatalf("%s does not contain %q", tt.path, want)
				}
			}
		})
	}
}

func TestClaudeCommandsUseProjectGate(t *testing.T) {
	root := repoRoot(t)
	tests := []struct {
		path string
		want []string
	}{
		{
			path: filepath.Join(root, ".claude", "commands", "zv-tdd.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".claude", "commands", "zv-bugfix.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".claude", "commands", "zv-parser-change.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
			},
		},
		{
			path: filepath.Join(root, ".claude", "commands", "zv-media-change.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
			},
		},
		{
			path: filepath.Join(root, ".claude", "commands", "zv-worker-api-change.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".claude", "commands", "zv-pr-ready.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race",
				"scripts/go-gate.sh --security",
			},
		},
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			b, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			body := string(b)
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Fatalf("%s does not contain %q", tt.path, want)
				}
			}
		})
	}
}

func TestFixLoopRunsProjectCheck(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "scripts", "fix-loop.ps1")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(b)
	for _, want := range []string{
		`Invoke-Step "zv check"`,
		"go run ./cmd/zv check",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s does not contain %q", path, want)
		}
	}
}

func TestMakefileRunsProjectCheck(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "Makefile")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(b)
	for _, want := range []string{
		"check:",
		"go run ./cmd/zv check",
		"workflows-check:",
		"go run ./cmd/zv workflows check",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s does not contain %q", path, want)
		}
	}
	if !strings.Contains(body, ".PHONY:") || !strings.Contains(body, "check") || !strings.Contains(body, "workflows-check") {
		t.Fatalf("%s does not mark check targets as phony", path)
	}
}

func TestCurrentBuildScriptsCoverCommandEntrypoints(t *testing.T) {
	root := repoRoot(t)
	commands, err := commandEntrypoints(root)
	if err != nil {
		t.Fatalf("command entrypoints: %v", err)
	}
	if len(commands) == 0 {
		t.Fatalf("no command entrypoints found")
	}

	makefileBody := readFileString(t, filepath.Join(root, "Makefile"))
	buildScriptBody := readFileString(t, filepath.Join(root, "scripts", "build.ps1"))
	known := make(map[string]struct{}, len(commands))
	for _, command := range commands {
		known[command] = struct{}{}
		makeTarget := fmt.Sprintf("go build -o bin/%s ./cmd/%s", command, command)
		if !strings.Contains(makefileBody, makeTarget) {
			t.Fatalf("Makefile does not build %s with %q", command, makeTarget)
		}
		buildEntry := fmt.Sprintf(`"%s"`, command)
		if !strings.Contains(buildScriptBody, buildEntry) {
			t.Fatalf("scripts/build.ps1 does not include command entry %s", buildEntry)
		}
	}
	for _, target := range makefileCommandBuildTargets(makefileBody) {
		if _, ok := known[target.Command]; !ok {
			t.Fatalf("Makefile builds stale command %q with %q", target.Command, target.Line)
		}
	}
	for _, command := range buildScriptCommandEntries(buildScriptBody) {
		if _, ok := known[command]; !ok {
			t.Fatalf("scripts/build.ps1 includes stale command entry %q", command)
		}
	}
}

func TestCurrentWorkflowDocsUseUnifiedCLI(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "docs", "README.md"),
		filepath.Join(root, "docs", "toolchain.md"),
		filepath.Join(root, "docs", "workflows", "catalog.md"),
	}
	legacyCommands := legacyWorkflowCommands()
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(b)
		for _, legacy := range legacyCommands {
			if strings.Contains(body, legacy) {
				t.Fatalf("%s contains legacy workflow command %q; use ./bin/zv instead", path, legacy)
			}
		}
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	for _, want := range []string{
		"./bin/zv demo parse",
		"./bin/zv demo players",
		"./bin/zv record",
		"./bin/zv compose final",
		"./bin/zv music analyze",
		"./bin/zv shorts render",
		"./bin/zv presets",
		"./bin/zv check",
		"./bin/zv serve",
	} {
		if !strings.Contains(string(readme), want) {
			t.Fatalf("README.md does not contain unified workflow command %q", want)
		}
	}
	catalog, err := os.ReadFile(filepath.Join(root, "docs", "workflows", "catalog.md"))
	if err != nil {
		t.Fatalf("read docs/workflows/catalog.md: %v", err)
	}
	body := string(catalog)
	for _, want := range []string{
		"./bin/zv demo parse",
		"./bin/zv demo players",
		"./bin/zv compose final",
		"./bin/zv shorts render",
		"./bin/zv analysis tactical-data",
		"./bin/zv analysis view",
		"./bin/zv serve",
		"./bin/zv pipeline",
		"./bin/zv check",
		"./bin/zv check --format json",
		"./bin/zv skills check",
		"./bin/zv skills list --format json",
		"./bin/zv skills show",
		"./bin/zv skills check --format json",
		"./bin/zv workflows list",
		"./bin/zv workflows list --format json",
		"./bin/zv workflows show",
		"./bin/zv workflows show demo-parse --format json",
		"./bin/zv workflows run demo-parse",
		"./bin/zv workflows run demo-players",
		"./bin/zv workflows run compose-final",
		"./bin/zv workflows run analysis-tactical-data",
		"./bin/zv workflows run analysis-viewer",
		"./bin/zv workflows run project-check",
		"./bin/zv workflows check",
		"./bin/zv workflows check --format json",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("docs/workflows/catalog.md does not contain unified workflow command %q", want)
		}
	}
	skills, err := loadSkills()
	if err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if len(skills) == 0 {
		t.Fatalf("no repo-local skills found")
	}
	for _, skill := range skills {
		if !strings.Contains(body, skill.Name) {
			t.Fatalf("docs/workflows/catalog.md does not document repo skill %q", skill.Name)
		}
	}
	toolchain, err := os.ReadFile(filepath.Join(root, "docs", "toolchain.md"))
	if err != nil {
		t.Fatalf("read docs/toolchain.md: %v", err)
	}
	if !strings.Contains(string(toolchain), `.\\bin\\zv.exe record`) && !strings.Contains(string(toolchain), `.\bin\zv.exe record`) {
		t.Fatalf("docs/toolchain.md does not document unified capture command")
	}
	if !strings.Contains(string(toolchain), `zv check`) {
		t.Fatalf("docs/toolchain.md does not document the project check command")
	}
}
