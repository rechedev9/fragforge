package main

import (
	"fmt"
	"strings"
)

func validateWorkflowDocRunCommandOrder(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	order := make(map[string]int, len(workflows))
	names := make([]string, 0, len(workflows))
	for i, workflow := range workflows {
		if workflow.Name == "" {
			continue
		}
		order[workflow.Name] = i
		names = append(names, workflow.Name)
	}

	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		last := -1
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			workflowName := command[2]
			if _, ok := seen[workflowName]; ok {
				continue
			}
			seen[workflowName] = struct{}{}
			pos, ok := order[workflowName]
			if !ok {
				continue
			}
			if pos < last {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("workflow run commands must appear in catalog order: %s", strings.Join(names, ", ")),
				})
				break
			}
			last = pos
		}
	}
	return issues
}

func validateWorkflowDocRunCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			workflowName := command[2]
			if workflowDocWorkflowRunMayRepeat(workflowName) {
				continue
			}
			if _, ok := seen[workflowName]; ok {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("duplicate workflow run %s", workflowName),
				})
				continue
			}
			seen[workflowName] = struct{}{}
		}
	}
	return issues
}

func workflowDocWorkflowRunMayRepeat(name string) bool {
	switch name {
	case "skills-check", "workflows-check", "project-check":
		return true
	default:
		return false
	}
}

func validateWorkflowDocShowCoverage(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		for _, workflow := range workflows {
			name := strings.TrimSpace(workflow.Name)
			if name == "" {
				continue
			}
			showCommand := "./bin/zv workflows show " + name
			if !docHasWorkflowShowCommand(doc.Body, name, "text") {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("missing workflow show command %s", showCommand),
				})
			}
			showJSONCommand := showCommand + " --format json"
			if !docHasWorkflowShowCommand(doc.Body, name, "json") {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("missing workflow show command %s", showJSONCommand),
				})
			}
		}
	}
	return issues
}

func validateWorkflowDocShowCommandOrder(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	order := make(map[string]int, len(workflows))
	names := make([]string, 0, len(workflows))
	for i, workflow := range workflows {
		if workflow.Name == "" {
			continue
		}
		order[workflow.Name] = i
		names = append(names, workflow.Name)
	}

	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		last := -1
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "workflows" || command[1] != "show" {
				continue
			}
			workflowName := command[2]
			if _, ok := seen[workflowName]; ok {
				continue
			}
			seen[workflowName] = struct{}{}
			pos, ok := order[workflowName]
			if !ok {
				continue
			}
			if pos < last {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("workflow show commands must appear in catalog order: %s", strings.Join(names, ", ")),
				})
				break
			}
			last = pos
		}
	}
	return issues
}

func validateWorkflowDocShowCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "workflows" || command[1] != "show" {
				continue
			}
			format, rest, err := parseFormatArgs(command[3:])
			if err != nil || len(rest) != 0 {
				continue
			}
			key := command[2] + "\x00" + format
			if _, ok := seen[key]; ok {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("duplicate workflow show %s --format %s", command[2], format),
				})
				continue
			}
			seen[key] = struct{}{}
		}
	}
	return issues
}

func validateWorkflowDocListAndCheckCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "workflows" {
				continue
			}
			if command[1] != "list" && command[1] != "check" {
				continue
			}
			format, rest, err := parseFormatArgs(command[2:])
			if err != nil || len(rest) != 0 {
				continue
			}
			key := command[1] + "\x00" + format
			if _, ok := seen[key]; ok {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("duplicate workflows %s --format %s", command[1], format),
				})
				continue
			}
			seen[key] = struct{}{}
		}
	}
	return issues
}

func validateProjectDocCheckCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if (!doc.RequiredWorkflows && !doc.RequiredSkills) || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) == 0 || command[0] != "check" {
				continue
			}
			format, rest, err := parseFormatArgs(command[1:])
			if err != nil || len(rest) != 0 {
				continue
			}
			if _, ok := seen[format]; ok {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("duplicate check --format %s", format),
				})
				continue
			}
			seen[format] = struct{}{}
		}
	}
	return issues
}

func requiredWorkflowRunName(command string) (string, bool) {
	fields, ok := splitCommandFields(command)
	if !ok || len(fields) < 4 {
		return "", false
	}
	if fields[1] != "workflows" || fields[2] != "run" {
		return "", false
	}
	switch fields[0] {
	case "zv", "./bin/zv", `.\bin\zv.exe`:
		return fields[3], true
	default:
		return "", false
	}
}

func docHasWorkflowShowCommand(body, name, wantFormat string) bool {
	for _, line := range skillCommandLines(body) {
		command, ok := skillCommand(line)
		if !ok || len(command) < 3 || command[0] != "workflows" || command[1] != "show" || command[2] != name {
			continue
		}
		format, rest, err := parseFormatArgs(command[3:])
		if err != nil || len(rest) != 0 {
			continue
		}
		if format == wantFormat {
			return true
		}
	}
	return false
}
