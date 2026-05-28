package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func checkClaudeReviewerAgents() ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	agentsDir := filepath.Join(root, ".claude", "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read claude agents: %w", err)
	}
	readmeBody, err := readWorkflowDocBody(root, ".claude/README.md")
	if err != nil {
		return nil, err
	}
	claudeBody, err := readWorkflowDocBody(root, "CLAUDE.md")
	if err != nil {
		return nil, err
	}
	var issues []skillIssue
	var checked int
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		checked++
		relPath := filepath.ToSlash(filepath.Join(".claude", "agents", entry.Name()))
		path := filepath.Join(agentsDir, entry.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", relPath, err)
		}
		body := string(b)
		name, ok := markdownFrontMatterValue(body, "name")
		wantName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if !ok {
			issues = append(issues, skillIssue{Path: relPath, Message: "missing agent front matter name"})
		} else if name != wantName {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("agent name %q does not match file name %q", name, wantName)})
		}
		for _, doc := range []struct {
			path string
			body string
		}{
			{path: ".claude/README.md", body: readmeBody},
			{path: "CLAUDE.md", body: claudeBody},
		} {
			if !strings.Contains(doc.body, "@"+wantName) {
				issues = append(issues, skillIssue{Path: doc.path, Message: fmt.Sprintf("does not document reviewer agent @%s", wantName)})
			}
		}
		for _, required := range claudeReviewerAgentRequiredText(wantName) {
			if !strings.Contains(body, required) {
				issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("missing reviewer guidance %q", required)})
			}
		}
	}
	if checked == 0 {
		issues = append(issues, skillIssue{Path: ".claude/agents", Message: "no claude reviewer agents found"})
	}
	return issues, nil
}

func readWorkflowDocBody(root, relPath string) (string, error) {
	path := filepath.Join(root, filepath.FromSlash(relPath))
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", relPath, err)
	}
	return string(b), nil
}

func markdownFrontMatterValue(body, key string) (string, bool) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return "", false
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			break
		}
		k, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		return trimMetadataValue(value), true
	}
	return "", false
}

func claudeReviewerAgentRequiredText(name string) []string {
	required := []string{
		"BLOCKER",
		"WARNING",
		"NIT",
		"Every finding",
		"file/path",
		"why",
		"practical fix",
		"No blocking",
		"issues found.",
	}
	switch name {
	case "go-concurrency-reviewer":
		required = append(required, "scripts/go-gate.sh --race")
	case "go-security-reviewer":
		required = append(required, "Do not read `.env`")
	case "zv-media-pipeline-reviewer":
		required = append(required, "HLAE/CS2/large media")
	}
	return required
}

func checkClaudeRuleDocs() ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	rulesDir := filepath.Join(root, ".claude", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return nil, fmt.Errorf("read claude rules: %w", err)
	}
	readmeBody, err := readWorkflowDocBody(root, ".claude/README.md")
	if err != nil {
		return nil, err
	}
	claudeBody, err := readWorkflowDocBody(root, "CLAUDE.md")
	if err != nil {
		return nil, err
	}
	var issues []skillIssue
	var checked int
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		checked++
		relPath := filepath.ToSlash(filepath.Join(".claude", "rules", entry.Name()))
		path := filepath.Join(rulesDir, entry.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", relPath, err)
		}
		body := string(b)
		for _, doc := range []struct {
			path string
			body string
		}{
			{path: ".claude/README.md", body: readmeBody},
			{path: "CLAUDE.md", body: claudeBody},
		} {
			if !strings.Contains(doc.body, relPath) {
				issues = append(issues, skillIssue{Path: doc.path, Message: fmt.Sprintf("does not document claude rule %s", relPath)})
			}
		}
		for _, required := range claudeRuleRequiredText(relPath) {
			if !strings.Contains(body, required) {
				issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("missing claude rule guidance %q", required)})
			}
		}
	}
	if checked == 0 {
		issues = append(issues, skillIssue{Path: ".claude/rules", Message: "no claude rule docs found"})
	}
	return issues, nil
}

func claudeRuleRequiredText(path string) []string {
	switch path {
	case ".claude/rules/go-style.md":
		return []string{
			"clarity, simplicity, concision, maintainability",
			"Avoid `util`, `common`, `helper`, `manager`",
			"Return errors with context",
			"Respect context cancellation",
			"Every goroutine needs an owner",
		}
	case ".claude/rules/zackvideo-operations.md":
		return []string{
			"scripts/go-gate.sh --no-format",
			"HLAE/CS2 launch or real capture",
			"Docker compose and database migrations",
			"cleanup scripts that delete artifacts",
			"Never add generated `.mp4`",
		}
	default:
		return []string{
			"ZackVideo",
		}
	}
}

type claudeSettingsFile struct {
	Permissions struct {
		Allow []string `json:"allow"`
		Ask   []string `json:"ask"`
		Deny  []string `json:"deny"`
	} `json:"permissions"`
}

func checkClaudeSettings() ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	const relPath = ".claude/settings.json"
	path := filepath.Join(root, filepath.FromSlash(relPath))
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []skillIssue{{Path: relPath, Message: "missing claude settings"}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", relPath, err)
	}
	var settings claudeSettingsFile
	if err := json.Unmarshal(b, &settings); err != nil {
		return []skillIssue{{Path: relPath, Message: fmt.Sprintf("invalid json: %v", err)}}, nil
	}

	var issues []skillIssue
	for section, values := range map[string][]string{
		"allow": settings.Permissions.Allow,
		"ask":   settings.Permissions.Ask,
		"deny":  settings.Permissions.Deny,
	} {
		if len(values) == 0 {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("permissions.%s is empty", section)})
		}
	}
	for _, permission := range claudeRequiredAllowPermissions() {
		if !containsString(settings.Permissions.Allow, permission) {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("missing allow permission %q", permission)})
		}
	}
	for _, permission := range claudeRequiredAskPermissions() {
		if !containsString(settings.Permissions.Ask, permission) {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("missing ask permission %q", permission)})
		}
	}
	for _, permission := range claudeRequiredDenyPermissions() {
		if !containsString(settings.Permissions.Deny, permission) {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("missing deny permission %q", permission)})
		}
	}
	for _, permission := range settings.Permissions.Allow {
		if containsString(claudeRequiredAskPermissions(), permission) || containsString(claudeRequiredDenyPermissions(), permission) {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("dangerous permission %q must not be allowed", permission)})
		}
	}
	return issues, nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
