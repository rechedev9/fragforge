package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type codexPromptContentRule struct {
	Path      string
	Required  []string
	Forbidden []string
}

func checkCodexPromptContents() ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	var issues []skillIssue
	for _, rule := range codexPromptContentRules() {
		path := filepath.Join(root, filepath.FromSlash(rule.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: "missing codex prompt"})
				continue
			}
			return nil, fmt.Errorf("read %s: %w", rule.Path, err)
		}
		body := string(b)
		for _, required := range rule.Required {
			if !strings.Contains(body, required) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: fmt.Sprintf("missing standard gate guidance %q", required)})
			}
		}
		for _, forbidden := range rule.Forbidden {
			if strings.Contains(body, forbidden) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: fmt.Sprintf("uses partial check %q; use scripts/go-gate.sh", forbidden)})
			}
		}
	}
	return issues, nil
}

func checkClaudeCommandContents() ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	var issues []skillIssue
	for _, rule := range claudeCommandContentRules() {
		path := filepath.Join(root, filepath.FromSlash(rule.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: "missing claude command"})
				continue
			}
			return nil, fmt.Errorf("read %s: %w", rule.Path, err)
		}
		body := string(b)
		for _, required := range rule.Required {
			if !strings.Contains(body, required) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: fmt.Sprintf("missing standard gate guidance %q", required)})
			}
		}
		for _, forbidden := range rule.Forbidden {
			if strings.Contains(body, forbidden) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: fmt.Sprintf("uses partial check %q; use scripts/go-gate.sh", forbidden)})
			}
		}
	}
	return issues, nil
}
