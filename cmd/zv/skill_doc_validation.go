package main

import (
	"fmt"
	"strings"
)

func validateSkillDocCoverage(skills []skillInfo, docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredSkills || doc.Body == "" {
			continue
		}
		for _, skill := range skills {
			name := strings.TrimSpace(skill.Name)
			if name == "" {
				continue
			}
			if strings.Contains(doc.Body, name) {
				showCommand := "./bin/zv skills show " + name
				if !docHasSkillShowCommand(doc.Body, name, "text") {
					issues = append(issues, skillIssue{
						Path:    doc.Path,
						Message: fmt.Sprintf("missing skill show command %s", showCommand),
					})
				}
				showJSONCommand := showCommand + " --format json"
				if !docHasSkillShowCommand(doc.Body, name, "json") {
					issues = append(issues, skillIssue{
						Path:    doc.Path,
						Message: fmt.Sprintf("missing skill show command %s", showJSONCommand),
					})
				}
				continue
			}
			issues = append(issues, skillIssue{
				Path:    doc.Path,
				Message: fmt.Sprintf("missing repo skill %s", name),
			})
		}
	}
	return issues
}

func validateSkillDocShowCommandOrder(skills []skillInfo, docs []workflowDoc) []skillIssue {
	order := make(map[string]int, len(skills))
	names := make([]string, 0, len(skills))
	for i, skill := range skills {
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		order[name] = i
		names = append(names, name)
	}

	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredSkills || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		last := -1
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "skills" || command[1] != "show" {
				continue
			}
			skillName := command[2]
			if _, ok := seen[skillName]; ok {
				continue
			}
			seen[skillName] = struct{}{}
			pos, ok := order[skillName]
			if !ok {
				continue
			}
			if pos < last {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("skill show commands must appear in skill order: %s", strings.Join(names, ", ")),
				})
				break
			}
			last = pos
		}
	}
	return issues
}

func validateSkillDocShowCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredSkills || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "skills" || command[1] != "show" {
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
					Message: fmt.Sprintf("duplicate skill show %s --format %s", command[2], format),
				})
				continue
			}
			seen[key] = struct{}{}
		}
	}
	return issues
}

func validateSkillDocListAndCheckCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredSkills || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "skills" {
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
					Message: fmt.Sprintf("duplicate skills %s --format %s", command[1], format),
				})
				continue
			}
			seen[key] = struct{}{}
		}
	}
	return issues
}

func docHasSkillShowCommand(body, name, wantFormat string) bool {
	for _, line := range skillCommandLines(body) {
		command, ok := skillCommand(line)
		if !ok || len(command) < 3 || command[0] != "skills" || command[1] != "show" || command[2] != name {
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
