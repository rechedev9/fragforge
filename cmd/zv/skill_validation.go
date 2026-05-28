package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func findSkill(name string) (skillInfo, bool, error) {
	skills, err := loadSkills()
	if err != nil {
		return skillInfo{}, false, err
	}
	for _, skill := range skills {
		if skill.Name == name {
			return skill, true, nil
		}
	}
	return skillInfo{}, false, nil
}

func loadSkills() ([]skillInfo, error) {
	dir, err := findSkillsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}
	skills := make([]skillInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("stat skill %s: %w", entry.Name(), err)
		}
		skill, err := parseSkill(path)
		if err != nil {
			return nil, err
		}
		if skill.Name == "" {
			skill.Name = entry.Name()
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

func checkSkills() ([]skillInfo, []skillIssue, error) {
	skills, err := loadSkills()
	if err != nil {
		return nil, nil, err
	}
	var issues []skillIssue
	if len(skills) == 0 {
		issues = append(issues, skillIssue{Path: ".codex/skills", Message: "no skills found"})
		return skills, issues, nil
	}
	seenSkills := make(map[string]string, len(skills))
	// The legacy-binary set is invariant; resolve it once rather than walking the
	// filesystem for it on every skill.
	legacyBinaries := legacySkillBinaries()
	for _, skill := range skills {
		// loadSkills already read the file into skill.Body; reuse it.
		body := skill.Body
		if strings.TrimSpace(skill.Name) == "" {
			issues = append(issues, skillIssue{Path: skill.Path, Message: "missing skill name"})
		} else {
			if !isWorkflowSlug(skill.Name) {
				issues = append(issues, skillIssue{Path: skill.Path, Message: "skill name must be a lowercase slug"})
			}
			if dirName := skillDirName(skill); dirName != "" && skill.Name != dirName {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("skill name %q must match directory %q", skill.Name, dirName)})
			}
			if firstPath, ok := seenSkills[skill.Name]; ok {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("duplicate skill name %q also used by %s", skill.Name, firstPath)})
			} else {
				seenSkills[skill.Name] = skill.Path
			}
		}
		if strings.TrimSpace(skill.Description) == "" {
			issues = append(issues, skillIssue{Path: skill.Path, Message: "missing skill description"})
		}
		if !strings.Contains(body, `.\\bin\\zv.exe`) && !strings.Contains(body, `.\bin\zv.exe`) && !strings.Contains(body, `./bin/zv`) {
			issues = append(issues, skillIssue{Path: skill.Path, Message: "does not document the unified zv CLI"})
		}
		for _, legacy := range legacyBinaries {
			if strings.Contains(body, legacy) {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("documents legacy direct binary %s", legacy)})
			}
		}
		hasWorkflowRun := false
		documentedWorkflowRuns := make(map[string]struct{})
		var documentedWorkflowRunOrder []string
		for _, line := range skillCommandLines(body) {
			command, ok := skillCommand(line)
			if !ok {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("could not parse zv command line %q", line)})
				continue
			}
			if isExecutableWorkflowRunCommand(command) {
				hasWorkflowRun = true
				if _, ok := documentedWorkflowRuns[command[2]]; ok {
					issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("duplicate workflow run %s", command[2])})
				}
				documentedWorkflowRuns[command[2]] = struct{}{}
				documentedWorkflowRunOrder = append(documentedWorkflowRunOrder, command[2])
			}
			if issue := validateSkillCommand(command); issue != "" {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("%s in %q", issue, line)})
			}
			if issue := validateSkillWorkflowEntrypoint(command); issue != "" {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("%s in %q", issue, line)})
			}
		}
		if !hasWorkflowRun {
			issues = append(issues, skillIssue{Path: skill.Path, Message: "does not document a cataloged workflow run command"})
		}
		for _, required := range skillWorkflowRequirements(skill.Name) {
			if _, ok := documentedWorkflowRuns[required]; ok {
				continue
			}
			issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("missing required workflow run %s", required)})
		}
		if issue := validateSkillRequiredWorkflowRunSet(skill.Name, documentedWorkflowRunOrder); issue != "" {
			issues = append(issues, skillIssue{Path: skill.Path, Message: issue})
		}
		if issue := validateSkillWorkflowRunCatalogOrder(documentedWorkflowRunOrder); issue != "" {
			issues = append(issues, skillIssue{Path: skill.Path, Message: issue})
		}
		if issue := validateSkillRequiredWorkflowRunOrder(skill.Name, documentedWorkflowRunOrder); issue != "" {
			issues = append(issues, skillIssue{Path: skill.Path, Message: issue})
		}
	}
	issues = append(issues, validateSkillWorkflowRequirementSkills(skills, skillWorkflowRequirementMap())...)
	return skills, issues, nil
}

func skillWorkflowRequirements(name string) []string {
	return skillWorkflowRequirementMap()[name]
}

func validateSkillRequiredWorkflowRunSet(skillName string, documented []string) string {
	required := skillWorkflowRequirements(skillName)
	if len(required) == 0 {
		return ""
	}
	allowed := make(map[string]struct{}, len(required))
	for _, workflowName := range required {
		allowed[workflowName] = struct{}{}
	}
	for _, workflowName := range documented {
		if _, ok := allowed[workflowName]; ok {
			continue
		}
		return fmt.Sprintf("unexpected workflow run %s; expected only: %s", workflowName, strings.Join(required, ", "))
	}
	return ""
}

func validateSkillRequiredWorkflowRunOrder(skillName string, documented []string) string {
	required := skillWorkflowRequirements(skillName)
	if len(required) < 2 {
		return ""
	}
	positions := make(map[string]int, len(required))
	for i, workflowName := range documented {
		if _, ok := positions[workflowName]; ok {
			continue
		}
		positions[workflowName] = i
	}
	last := -1
	for _, workflowName := range required {
		pos, ok := positions[workflowName]
		if !ok {
			return ""
		}
		if pos < last {
			return fmt.Sprintf("required workflow runs must appear in order: %s", strings.Join(required, ", "))
		}
		last = pos
	}
	return ""
}

func validateSkillWorkflowRunCatalogOrder(documented []string) string {
	if len(documented) < 2 {
		return ""
	}
	catalog := workflowCatalog()
	catalogOrder := make(map[string]int, len(catalog))
	for i, workflow := range catalog {
		catalogOrder[workflow.Name] = i
	}
	lastIndex := -1
	lastWorkflow := ""
	for _, workflowName := range documented {
		index, ok := catalogOrder[workflowName]
		if !ok {
			continue
		}
		if index < lastIndex {
			return fmt.Sprintf("workflow runs must follow catalog order; %s appears after %s", workflowName, lastWorkflow)
		}
		lastIndex = index
		lastWorkflow = workflowName
	}
	return ""
}

func skillDirName(skill skillInfo) string {
	dir := filepath.Dir(skill.Path)
	if dir == "." || dir == string(filepath.Separator) {
		return ""
	}
	return filepath.Base(dir)
}

func isWorkflowRunCommand(command []string) bool {
	return len(command) >= 3 && command[0] == "workflows" && command[1] == "run"
}

func isExecutableWorkflowRunCommand(command []string) bool {
	return isWorkflowRunCommand(command) && !isWorkflowRunHelpCommand(command)
}

func isWorkflowRunHelpCommand(command []string) bool {
	return isWorkflowRunCommand(command) && len(command) == 5 && command[3] == "--" && isSingleHelp(command[4:])
}

func validateSkillWorkflowEntrypoint(command []string) string {
	if isWorkflowRunCommand(command) {
		return ""
	}
	for _, workflow := range workflowCatalog() {
		if hasPrefixArgs(command, workflow.RunArgs) {
			return fmt.Sprintf("uses direct workflow command %q; use %q", strings.Join(workflow.RunArgs, " "), workflow.RunCommand)
		}
	}
	return ""
}

func findSkillsDir() (string, error) {
	var starts []string
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		for dir := start; ; dir = filepath.Dir(dir) {
			candidate := filepath.Join(dir, ".codex", "skills")
			if st, err := os.Stat(candidate); err == nil && st.IsDir() {
				return candidate, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return "", fmt.Errorf("skills dir not found: .codex/skills")
}

func parseSkill(path string) (skillInfo, error) {
	// #nosec G304 -- skill path is resolved from the repo-local skills directory.
	b, err := os.ReadFile(path)
	if err != nil {
		return skillInfo{}, fmt.Errorf("read skill %s: %w", path, err)
	}

	// Keep the full body so callers (checkSkills) need not re-read the file.
	skill := skillInfo{Path: path, Body: string(b)}
	scanner := bufio.NewScanner(strings.NewReader(skill.Body))
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return skillInfo{}, fmt.Errorf("scan skill %s: %w", path, err)
		}
		return skill, nil
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return skill, nil
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = trimMetadataValue(value)
		switch strings.TrimSpace(key) {
		case "name":
			skill.Name = value
		case "description":
			skill.Description = value
		}
	}
	if err := scanner.Err(); err != nil {
		return skillInfo{}, fmt.Errorf("scan skill %s: %w", path, err)
	}
	return skill, nil
}

func trimMetadataValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}
