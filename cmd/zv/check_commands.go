package main

import (
	"fmt"
	"io"
)

func runCheck(args []string, stdout, stderr io.Writer) int {
	return runWorkflowContractCheck(args, stdout, stderr, checkUsage, "check")
}

func runWorkflowContractCheck(args []string, stdout, stderr io.Writer, usage string, commandName string) int {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(stdout, usage)
		return exitSuccess
	}
	format, rest, err := parseFormatArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitInvalidArgs
	}
	if len(rest) != 0 {
		fmt.Fprintf(stderr, "error: unexpected extra args for %q\n", commandName)
		fmt.Fprint(stderr, usage)
		return exitInvalidArgs
	}
	skills, workflows, docs, agentPromptWrappersChecked, issues, err := checkWorkflows()
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitUnexpected
	}
	if format == "json" {
		result := workflowCheckResult{
			OK:                         len(issues) == 0,
			SkillsChecked:              len(skills),
			WorkflowsChecked:           len(workflows),
			WorkflowDocsChecked:        len(docs),
			AgentPromptWrappersChecked: agentPromptWrappersChecked,
			Issues:                     issues,
		}
		if result.Issues == nil {
			result.Issues = []skillIssue{}
		}
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: writing json: %v\n", err)
			return exitUnexpected
		}
		if !result.OK {
			return exitInvalidArgs
		}
		return exitSuccess
	}
	if len(issues) > 0 {
		for _, issue := range issues {
			fmt.Fprintf(stderr, "%s: %s\n", issue.Path, issue.Message)
		}
		return exitInvalidArgs
	}
	fmt.Fprintf(stdout, "OK: %d skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked\n", len(skills), len(workflows), len(docs), agentPromptWrappersChecked)
	return exitSuccess
}
