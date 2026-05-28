package main

import (
	"fmt"
	"strings"
)

func checkWorkflows() ([]skillInfo, []workflowInfo, []workflowDoc, int, []skillIssue, error) {
	skills, issues, err := checkSkills()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	workflows := workflowCatalog()
	issues = append(issues, validateWorkflowCatalog(workflows)...)
	issues = append(issues, validateInternalCheckWorkflows(workflows)...)
	issues = append(issues, validateWorkflowDelegationCoverage(workflows)...)
	issues = append(issues, validateSkillWorkflowRequirementCatalog(workflows, skillWorkflowRequirementMap())...)
	issues = append(issues, validateUsageCoverage(workflows, usage)...)
	issues = append(issues, validateGroupUsageCoverage(workflows, groupUsageTexts())...)
	issues = append(issues, validateLegacyPassThroughUsage(usage)...)
	docs, docIssues, err := checkWorkflowDocs()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, docIssues...)
	issues = append(issues, validateWorkflowDocCoverage(workflows, docs)...)
	issues = append(issues, validateWorkflowDocExecutableDirectCommands(workflows, docs)...)
	issues = append(issues, validateWorkflowDocRequiredWorkflowRuns(workflows, docs)...)
	issues = append(issues, validateWorkflowDocExecutableWorkflowRuns(workflows, docs)...)
	issues = append(issues, validateWorkflowDocRunCommandOrder(workflows, docs)...)
	issues = append(issues, validateWorkflowDocRunCommandUniqueness(docs)...)
	issues = append(issues, validateWorkflowDocShowCoverage(workflows, docs)...)
	issues = append(issues, validateWorkflowDocShowCommandOrder(workflows, docs)...)
	issues = append(issues, validateWorkflowDocShowCommandUniqueness(docs)...)
	issues = append(issues, validateWorkflowDocListAndCheckCommandUniqueness(docs)...)
	issues = append(issues, validateProjectDocCheckCommandUniqueness(docs)...)
	issues = append(issues, validateSkillDocCoverage(skills, docs)...)
	issues = append(issues, validateSkillDocShowCommandOrder(skills, docs)...)
	issues = append(issues, validateSkillDocShowCommandUniqueness(docs)...)
	issues = append(issues, validateSkillDocListAndCheckCommandUniqueness(docs)...)
	buildIssues, err := checkCommandBuildTargets()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, buildIssues...)
	commandCoverageIssues, err := checkCommandEntrypointCoverage(workflows)
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, commandCoverageIssues...)
	agentPromptWrappersChecked, promptIssues, err := checkAgentPromptWrappers()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, promptIssues...)
	promptContentIssues, err := checkCodexPromptContents()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, promptContentIssues...)
	claudeContentIssues, err := checkClaudeCommandContents()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, claudeContentIssues...)
	claudeAgentIssues, err := checkClaudeReviewerAgents()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, claudeAgentIssues...)
	claudeRuleIssues, err := checkClaudeRuleDocs()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, claudeRuleIssues...)
	claudeSettingsIssues, err := checkClaudeSettings()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, claudeSettingsIssues...)
	return skills, workflows, docs, agentPromptWrappersChecked, issues, nil
}

func validateWorkflowCatalog(workflows []workflowInfo) []skillIssue {
	seen := make(map[string]struct{}, len(workflows))
	seenRunArgs := make(map[string]string, len(workflows))
	var issues []skillIssue
	for i, workflow := range workflows {
		path := fmt.Sprintf("workflow:%d", i+1)
		if strings.TrimSpace(workflow.Name) != "" {
			path = "workflow:" + workflow.Name
		}
		if strings.TrimSpace(workflow.Name) == "" {
			issues = append(issues, skillIssue{Path: path, Message: "missing workflow name"})
		} else if !isWorkflowSlug(workflow.Name) {
			issues = append(issues, skillIssue{Path: path, Message: "workflow name must be a lowercase slug"})
		} else if _, ok := seen[workflow.Name]; ok {
			issues = append(issues, skillIssue{Path: path, Message: "duplicate workflow name"})
		} else {
			seen[workflow.Name] = struct{}{}
		}
		if len(workflow.RunArgs) > 0 {
			runArgsKey := strings.Join(workflow.RunArgs, " ")
			if firstWorkflow, ok := seenRunArgs[runArgsKey]; ok {
				issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("duplicate workflow run args %q also used by workflow %q", runArgsKey, firstWorkflow)})
			} else {
				seenRunArgs[runArgsKey] = workflow.Name
			}
		}
		if strings.TrimSpace(workflow.Description) == "" {
			issues = append(issues, skillIssue{Path: path, Message: "missing workflow description"})
		}
		if strings.TrimSpace(workflow.Name) != "" {
			wantRunCommand := workflowRunCommand(workflow.Name)
			if workflow.RunCommand != wantRunCommand {
				issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("workflow run command must be %q", wantRunCommand)})
			}
		}
		fields, ok := splitCommandFields(workflow.Command)
		if !ok {
			issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("could not parse workflow command: %s", workflow.Command)})
			continue
		}
		if len(fields) == 0 {
			issues = append(issues, skillIssue{Path: path, Message: "missing workflow command"})
			continue
		}
		if fields[0] != "zv" {
			issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("workflow command must start with zv: %s", workflow.Command)})
			continue
		}
		if issue := validateSkillCommand(fields[1:]); issue != "" {
			issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("workflow command is not canonical: %s", issue)})
		}
		if issue := validateWorkflowRunArgs(workflow); issue != "" {
			issues = append(issues, skillIssue{Path: path, Message: issue})
		}
	}
	return issues
}

func validateInternalCheckWorkflows(workflows []workflowInfo) []skillIssue {
	expected := map[string]workflowInfo{
		"skills-check": {
			Command: "zv skills check",
			RunArgs: []string{"skills", "check"},
		},
		"workflows-check": {
			Command: "zv workflows check",
			RunArgs: []string{"workflows", "check"},
		},
		"project-check": {
			Command: "zv check",
			RunArgs: []string{"check"},
		},
	}
	seen := make(map[string]workflowInfo, len(workflows))
	for _, workflow := range workflows {
		seen[workflow.Name] = workflow
	}
	var issues []skillIssue
	for name, want := range expected {
		workflow, ok := seen[name]
		if !ok {
			issues = append(issues, skillIssue{Path: "workflow:" + name, Message: "missing internal check workflow"})
			continue
		}
		if workflow.Command != want.Command {
			issues = append(issues, skillIssue{Path: "workflow:" + name, Message: fmt.Sprintf("internal check workflow command must be %q", want.Command)})
		}
		if !equalArgs(workflow.RunArgs, want.RunArgs) {
			issues = append(issues, skillIssue{Path: "workflow:" + name, Message: fmt.Sprintf("internal check workflow run args must be %q", strings.Join(want.RunArgs, " "))})
		}
	}
	return issues
}

func validateWorkflowDelegationCoverage(workflows []workflowInfo) []skillIssue {
	var issues []skillIssue
	for _, workflow := range workflows {
		if len(workflow.RunArgs) == 0 {
			continue
		}
		if workflowDelegatedCommand(workflow.RunArgs) != "" {
			continue
		}
		path := "workflow:" + workflow.Name
		if strings.TrimSpace(workflow.Name) == "" {
			path = "workflow"
		}
		issues = append(issues, skillIssue{
			Path:    path,
			Message: fmt.Sprintf("workflow run args %q are not mapped to a delegated command", strings.Join(workflow.RunArgs, " ")),
		})
	}
	return issues
}

func validateSkillWorkflowRequirementSkills(skills []skillInfo, requirements map[string][]string) []skillIssue {
	installed := make(map[string]struct{}, len(skills))
	hasKnownRequiredSkill := false
	for _, skill := range skills {
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		installed[name] = struct{}{}
		if _, ok := requirements[name]; ok {
			hasKnownRequiredSkill = true
		}
	}
	if !hasKnownRequiredSkill {
		return nil
	}
	var issues []skillIssue
	for skillName := range installed {
		if !strings.HasPrefix(skillName, "zackvideo-") {
			continue
		}
		if _, ok := requirements[skillName]; ok {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    "skill:" + skillName,
			Message: "missing workflow requirements for repo skill",
		})
	}
	for skillName := range requirements {
		if _, ok := installed[skillName]; ok {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    "skill:" + skillName,
			Message: "workflow requirements reference missing repo skill",
		})
	}
	return issues
}

func validateSkillWorkflowRequirementCatalog(workflows []workflowInfo, requirements map[string][]string) []skillIssue {
	cataloged := make(map[string]struct{}, len(workflows))
	for _, workflow := range workflows {
		if workflow.Name == "" {
			continue
		}
		cataloged[workflow.Name] = struct{}{}
	}
	var issues []skillIssue
	for skillName, requiredWorkflows := range requirements {
		if !isWorkflowSlug(skillName) {
			issues = append(issues, skillIssue{Path: "skill:" + skillName, Message: "skill workflow requirement name must be a lowercase slug"})
		}
		for _, workflowName := range requiredWorkflows {
			if _, ok := cataloged[workflowName]; ok {
				continue
			}
			issues = append(issues, skillIssue{
				Path:    "skill:" + skillName,
				Message: fmt.Sprintf("required workflow %q is not cataloged", workflowName),
			})
		}
	}
	return issues
}
