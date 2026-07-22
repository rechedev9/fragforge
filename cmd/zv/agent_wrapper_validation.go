package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func checkCodexPromptWrappers() (int, []skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return 0, nil, err
	}
	promptsDir := filepath.Join(root, ".codex", "prompts")
	entries, err := os.ReadDir(promptsDir)
	if err != nil {
		return 0, nil, fmt.Errorf("read codex prompts: %w", err)
	}
	prompts := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		prompts[filepath.ToSlash(filepath.Join(".codex", "prompts", entry.Name()))] = false
	}

	guidePath := filepath.Join(root, ".codex", "GUIDE.md")
	b, err := os.ReadFile(guidePath)
	if err != nil {
		return 0, nil, fmt.Errorf("read .codex/GUIDE.md: %w", err)
	}
	guideBody := string(b)
	var issues []skillIssue
	runnerPath := filepath.Join(root, "scripts", "codex-run.sh")
	relRunner := filepath.ToSlash(mustRel(root, runnerPath))
	if b, err := os.ReadFile(runnerPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			issues = append(issues, skillIssue{Path: relRunner, Message: "missing codex prompt runner"})
		} else {
			return 0, nil, fmt.Errorf("read %s: %w", relRunner, err)
		}
	} else {
		issues = append(issues, validateAgentShellScript(relRunner, string(b))...)
		if !strings.Contains(guideBody, relRunner) {
			issues = append(issues, skillIssue{Path: ".codex/GUIDE.md", Message: fmt.Sprintf("does not document runner %s", relRunner)})
		}
	}

	wrappers, err := filepath.Glob(filepath.Join(root, "scripts", "codex*.sh"))
	if err != nil {
		return 0, nil, fmt.Errorf("glob codex wrappers: %w", err)
	}
	var checked int
	for _, wrapper := range wrappers {
		if filepath.Base(wrapper) == "codex-run.sh" {
			continue
		}
		relWrapper := filepath.ToSlash(mustRel(root, wrapper))
		b, err := os.ReadFile(wrapper)
		if err != nil {
			return 0, nil, fmt.Errorf("read %s: %w", relWrapper, err)
		}
		body := string(b)
		issues = append(issues, validateAgentShellScript(relWrapper, body)...)
		prompt, ok := codexWrapperPromptPath(body)
		if !ok {
			issues = append(issues, skillIssue{Path: relWrapper, Message: "does not exec scripts/codex-run.sh with a prompt"})
			continue
		}
		if _, ok := prompts[prompt]; !ok {
			issues = append(issues, skillIssue{Path: relWrapper, Message: fmt.Sprintf("references missing prompt %s", prompt)})
			continue
		}
		prompts[prompt] = true
		checked++
		if !strings.Contains(guideBody, relWrapper) {
			issues = append(issues, skillIssue{Path: ".codex/GUIDE.md", Message: fmt.Sprintf("does not document wrapper %s", relWrapper)})
		}
	}
	if checked == 0 {
		issues = append(issues, skillIssue{Path: "scripts", Message: "no codex prompt wrappers found"})
	}
	for prompt, used := range prompts {
		if !used {
			issues = append(issues, skillIssue{Path: prompt, Message: "has no codex wrapper"})
		}
	}
	return checked, issues, nil
}

func validateAgentShellScript(path, body string) []skillIssue {
	var issues []skillIssue
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "#!/usr/bin/env bash" {
		issues = append(issues, skillIssue{Path: path, Message: "missing standard bash shebang"})
	}
	if !strings.Contains(body, "set -euo pipefail") {
		issues = append(issues, skillIssue{Path: path, Message: "missing strict shell mode set -euo pipefail"})
	}
	return issues
}

func codexWrapperPromptPath(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "scripts/codex-run.sh") {
			continue
		}
		for _, field := range strings.Fields(line) {
			field = strings.Trim(field, `"'`)
			if strings.HasPrefix(field, ".codex/prompts/") && strings.HasSuffix(field, ".md") {
				return filepath.ToSlash(field), true
			}
		}
	}
	return "", false
}
