package main

import (
	"fmt"
	"io"
)

func runWorkflows(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, workflowsUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, workflowsUsage)
		return exitSuccess
	}
	switch args[0] {
	case "list":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, workflowsListUsage)
			return exitSuccess
		}
		format, rest, err := parseFormatArgs(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitInvalidArgs
		}
		if len(rest) != 0 {
			fmt.Fprintln(stderr, `error: unexpected extra args for "workflows list"`)
			fmt.Fprint(stderr, workflowsListUsage)
			return exitInvalidArgs
		}
		workflows := workflowCatalog()
		if format == "json" {
			if err := writeJSON(stdout, workflows); err != nil {
				fmt.Fprintf(stderr, "error: writing json: %v\n", err)
				return exitUnexpected
			}
			return exitSuccess
		}
		for _, workflow := range workflows {
			fmt.Fprintf(stdout, "%s\t%s\n", workflow.Name, workflow.Description)
		}
		return exitSuccess
	case "show":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, workflowsShowUsage)
			return exitSuccess
		}
		format, rest, err := parseFormatArgs(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitInvalidArgs
		}
		if len(rest) == 0 {
			fmt.Fprintln(stderr, `error: missing workflow name for "workflows show"`)
			fmt.Fprint(stderr, workflowsShowUsage)
			return exitInvalidArgs
		}
		if len(rest) > 1 {
			fmt.Fprintln(stderr, `error: unexpected extra args for "workflows show"`)
			fmt.Fprint(stderr, workflowsShowUsage)
			return exitInvalidArgs
		}
		workflow, ok := findWorkflow(rest[0])
		if !ok {
			fmt.Fprintf(stderr, "error: workflow not found: %s\n", rest[0])
			return exitInvalidArgs
		}
		if format == "json" {
			if err := writeJSON(stdout, workflow); err != nil {
				fmt.Fprintf(stderr, "error: writing json: %v\n", err)
				return exitUnexpected
			}
			return exitSuccess
		}
		fmt.Fprintf(stdout, "%s\n%s\n\ncommand: %s\nrun_command: %s\n", workflow.Name, workflow.Description, workflow.Command, workflow.RunCommand)
		return exitSuccess
	case "run":
		if len(args) < 2 {
			fmt.Fprint(stderr, workflowsRunUsage)
			return exitInvalidArgs
		}
		workflow, ok := findWorkflow(args[1])
		if !ok {
			fmt.Fprintf(stderr, "error: workflow not found: %s\n", args[1])
			return exitInvalidArgs
		}
		rest := args[2:]
		if issue := validateWorkflowRunForwardedArgs(workflow, rest); issue != "" {
			fmt.Fprintf(stderr, "error: %s\n", issue)
			fmt.Fprint(stderr, workflowsRunUsage)
			return exitInvalidArgs
		}
		if len(rest) > 0 {
			if rest[0] != "--" {
				fmt.Fprintln(stderr, `error: missing "--" separator before forwarded args`)
				fmt.Fprint(stderr, workflowsRunUsage)
				return exitInvalidArgs
			}
			rest = rest[1:]
		}
		runArgs := append([]string{"zv"}, workflow.RunArgs...)
		runArgs = append(runArgs, rest...)
		return Run(runArgs, stdout, stderr, stdin, runner)
	case "check":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, workflowsCheckUsage)
			return exitSuccess
		}
		return runWorkflowContractCheck(args[1:], stdout, stderr, workflowsCheckUsage, "workflows check")
	default:
		fmt.Fprintf(stderr, "unknown workflows command %q\n%s", args[0], workflowsUsage)
		return exitInvalidArgs
	}
}
