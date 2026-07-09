package main

import (
	"errors"
	"fmt"
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

func isExecutableDirectWorkflowCommand(command []string, workflow workflowInfo) bool {
	if !hasPrefixArgs(command, workflow.RunArgs) {
		return false
	}
	if isSingleHelp(command[len(workflow.RunArgs):]) {
		return false
	}
	return validateSkillCommand(command) == ""
}
