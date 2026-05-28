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
		for _, legacy := range legacyWorkflowCommands() {
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

func validateWorkflowDocCoverage(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	documented := make(map[string]struct{})
	for _, doc := range docs {
		for _, required := range doc.Required {
			documented[required] = struct{}{}
		}
	}
	var issues []skillIssue
	for _, workflow := range workflows {
		for _, coverage := range []struct {
			name    string
			command string
		}{
			{name: "workflow command", command: workflow.Command},
			{name: "workflow run command", command: workflow.RunCommand},
		} {
			required := documentedWorkflowCommand(coverage.command)
			if required == "" {
				continue
			}
			if _, ok := documented[required]; ok {
				continue
			}
			issues = append(issues, skillIssue{
				Path:    "workflow:" + workflow.Name,
				Message: fmt.Sprintf("%s %s is not covered by workflow docs", coverage.name, required),
			})
		}
	}
	return issues
}

func validateWorkflowDocExecutableDirectCommands(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		documented := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok {
				continue
			}
			for _, workflow := range workflows {
				if !isExecutableDirectWorkflowCommand(command, workflow) {
					continue
				}
				documented[workflow.Name] = struct{}{}
				break
			}
		}
		for _, workflow := range workflows {
			if strings.TrimSpace(workflow.Name) == "" || documentedWorkflowCommand(workflow.Command) == "" {
				continue
			}
			if _, ok := documented[workflow.Name]; ok {
				continue
			}
			issues = append(issues, skillIssue{
				Path:    doc.Path,
				Message: fmt.Sprintf("missing executable workflow command %s", workflow.Name),
			})
		}
	}
	return issues
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

func validateWorkflowDocRequiredWorkflowRuns(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	cataloged := make(map[string]struct{})
	for _, workflow := range workflows {
		cataloged[workflow.Name] = struct{}{}
	}

	var issues []skillIssue
	for _, doc := range docs {
		for _, required := range doc.Required {
			name, ok := requiredWorkflowRunName(required)
			if !ok {
				continue
			}
			if _, ok := cataloged[name]; ok {
				continue
			}
			issues = append(issues, skillIssue{
				Path:    doc.Path,
				Message: fmt.Sprintf("required workflow run %q is not cataloged", name),
			})
		}
	}
	return issues
}

func validateWorkflowDocExecutableWorkflowRuns(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		documented := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			documented[command[2]] = struct{}{}
		}
		for _, workflow := range workflows {
			name := strings.TrimSpace(workflow.Name)
			if name == "" {
				continue
			}
			if _, ok := documented[name]; ok {
				continue
			}
			issues = append(issues, skillIssue{
				Path:    doc.Path,
				Message: fmt.Sprintf("missing executable workflow run %s", name),
			})
		}
	}
	return issues
}
