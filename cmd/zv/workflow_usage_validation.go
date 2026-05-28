package main

import (
	"fmt"
	"strings"
)

func validateUsageCoverage(workflows []workflowInfo, usageText string) []skillIssue {
	covered := usageCommandStems(usageText)
	var issues []skillIssue
	for _, workflow := range workflows {
		stem := workflowCommandStem(workflow.Command)
		if stem == "" {
			continue
		}
		if _, ok := covered[stem]; ok {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    "workflow:" + workflow.Name,
			Message: fmt.Sprintf("workflow command %q is not covered by main usage", stem),
		})
	}
	return issues
}

func validateGroupUsageCoverage(workflows []workflowInfo, groupUsages map[string]string) []skillIssue {
	var issues []skillIssue
	for _, workflow := range workflows {
		stem := workflowCommandStem(workflow.Command)
		if stem == "" {
			continue
		}
		fields, ok := splitCommandFields(stem)
		if !ok || len(fields) < 2 {
			continue
		}
		usageText, ok := groupUsages[fields[1]]
		if !ok {
			continue
		}
		if _, ok := usageCommandStems(usageText)[stem]; ok {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    "workflow:" + workflow.Name,
			Message: fmt.Sprintf("workflow command %q is not covered by %s usage", stem, fields[1]),
		})
	}
	return issues
}

func validateLegacyPassThroughUsage(usageText string) []skillIssue {
	var issues []skillIssue
	for _, passThrough := range legacyPassThroughs() {
		line := legacyPassThroughUsageLine(passThrough)
		if strings.Contains(usageText, line) {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    "usage",
			Message: fmt.Sprintf("legacy pass-through %q is not covered by main usage", line),
		})
	}
	return issues
}

func workflowCommandStem(command string) string {
	fields, ok := splitCommandFields(command)
	if !ok || len(fields) == 0 || fields[0] != "zv" {
		return ""
	}
	stem := []string{"zv"}
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "--") || strings.HasPrefix(field, "<") {
			break
		}
		stem = append(stem, field)
	}
	return strings.Join(stem, " ")
}

func usageCommandStems(usageText string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, line := range strings.Split(usageText, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "usage:"); ok {
			line = strings.TrimSpace(after)
		}
		for _, part := range strings.Split(line, "|") {
			if stem := usageLineCommandStem(part); stem != "" {
				out[stem] = struct{}{}
			}
		}
	}
	return out
}

func usageLineCommandStem(line string) string {
	fields, ok := splitCommandFields(strings.TrimSpace(line))
	if !ok || len(fields) == 0 || fields[0] != "zv" {
		return ""
	}
	var usageStem []string
	for _, field := range fields {
		if strings.HasPrefix(field, "--") || strings.HasPrefix(field, "<") || strings.HasPrefix(field, "[") {
			break
		}
		usageStem = append(usageStem, field)
	}
	return strings.Join(usageStem, " ")
}

func isWorkflowSlug(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' {
			continue
		}
		return false
	}
	return true
}

func validateWorkflowRunArgs(workflow workflowInfo) string {
	args := workflow.RunArgs
	if len(args) == 0 {
		return "missing workflow run args"
	}
	command := strings.Join(args, " ")
	fields, ok := workflowCommandRunArgs(workflow.Command)
	if !ok {
		return fmt.Sprintf("could not parse workflow command: %s", workflow.Command)
	}
	if !equalArgs(fields, args) {
		return fmt.Sprintf("workflow run args %q do not match workflow command %q", command, workflow.Command)
	}
	return ""
}

func workflowCommandRunArgs(command string) ([]string, bool) {
	fields, ok := splitCommandFields(command)
	if !ok {
		return nil, false
	}
	if len(fields) == 0 || fields[0] != "zv" {
		return nil, true
	}
	var args []string
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "--") || strings.HasPrefix(field, "<") {
			break
		}
		args = append(args, field)
	}
	return args, true
}

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func hasPrefixArgs(fields, prefix []string) bool {
	if len(fields) < len(prefix) {
		return false
	}
	for i, want := range prefix {
		if fields[i] != want {
			return false
		}
	}
	return true
}
