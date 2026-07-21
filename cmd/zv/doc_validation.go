package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func checkWorkflowDocs() ([]workflowDoc, []skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, nil, err
	}
	docs := workflowDocs()
	// The legacy-command set is invariant; resolve it once rather than walking
	// the filesystem for it on every doc.
	legacyCommands := legacyWorkflowCommands()
	var issues []skillIssue
	readmePaths, err := findReadmeFiles(root)
	if err != nil {
		return nil, nil, err
	}
	for _, path := range readmePaths {
		issues = append(issues, skillIssue{
			Path:    path,
			Message: "README files are not allowed; use a purpose-specific document name",
		})
	}
	for i, doc := range docs {
		path := filepath.Join(root, filepath.FromSlash(doc.Path))
		// #nosec G304 -- workflow docs are fixed repo-local paths.
		b, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				issues = append(issues, skillIssue{Path: doc.Path, Message: "missing workflow doc"})
				continue
			}
			return nil, nil, fmt.Errorf("read workflow doc %s: %w", doc.Path, err)
		}
		body := string(b)
		docs[i].Body = body
		for _, legacy := range legacyCommands {
			if strings.Contains(body, legacy) {
				issues = append(issues, skillIssue{Path: doc.Path, Message: fmt.Sprintf("documents legacy direct command %s", legacy)})
			}
		}
		for _, required := range doc.Required {
			if !strings.Contains(body, required) {
				issues = append(issues, skillIssue{Path: doc.Path, Message: fmt.Sprintf("missing canonical workflow command %s", required)})
			}
		}
		for _, line := range skillCommandLines(body) {
			command, ok := skillCommand(line)
			if !ok {
				issues = append(issues, skillIssue{Path: doc.Path, Message: fmt.Sprintf("could not parse zv command line %q", line)})
				continue
			}
			if issue := validateSkillCommand(command); issue != "" {
				issues = append(issues, skillIssue{Path: doc.Path, Message: fmt.Sprintf("%s in %q", issue, line)})
			}
		}
	}
	return docs, issues, nil
}

func findReadmeFiles(root string) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && skipReadmeScanDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.EqualFold(strings.TrimSuffix(name, filepath.Ext(name)), "README") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("relativize README path %s: %w", path, err)
		}
		matches = append(matches, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan for README files: %w", err)
	}
	return matches, nil
}

func skipReadmeScanDirectory(name string) bool {
	switch name {
	case ".atl", ".git", ".next", ".playwright-cli", ".vercel", ".worktrees", "bin", "build-resources", "data", "dist", "dist-installer", "ds-bundle", "node_modules", "worktrees":
		return true
	default:
		return false
	}
}

func isExecutableDirectWorkflowCommand(command []string, workflow workflowInfo) bool {
	if !hasPrefixArgs(command, workflow.RunArgs) {
		return false
	}
	if isSingleHelp(command[len(workflow.RunArgs):]) {
		return false
	}
	return validateSkillCommand(command) == ""
}
