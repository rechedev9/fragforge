package main

import (
	"fmt"
	"io"
	"os"
)

func runSkills(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, skillsUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, skillsUsage)
		return exitSuccess
	}
	switch args[0] {
	case "list":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, skillsListUsage)
			return exitSuccess
		}
		format, rest, err := parseFormatArgs(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitInvalidArgs
		}
		if len(rest) != 0 {
			fmt.Fprintln(stderr, `error: unexpected extra args for "skills list"`)
			fmt.Fprint(stderr, skillsListUsage)
			return exitInvalidArgs
		}
		skills, err := loadSkills()
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitUnexpected
		}
		if format == "json" {
			if err := writeJSON(stdout, skills); err != nil {
				fmt.Fprintf(stderr, "error: writing json: %v\n", err)
				return exitUnexpected
			}
			return exitSuccess
		}
		for _, skill := range skills {
			if skill.Description == "" {
				fmt.Fprintln(stdout, skill.Name)
				continue
			}
			fmt.Fprintf(stdout, "%s\t%s\n", skill.Name, skill.Description)
		}
		return exitSuccess
	case "show":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, skillsShowUsage)
			return exitSuccess
		}
		format, rest, err := parseFormatArgs(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitInvalidArgs
		}
		if len(rest) == 0 {
			fmt.Fprintln(stderr, `error: missing skill name for "skills show"`)
			fmt.Fprint(stderr, skillsShowUsage)
			return exitInvalidArgs
		}
		if len(rest) > 1 {
			fmt.Fprintln(stderr, `error: unexpected extra args for "skills show"`)
			fmt.Fprint(stderr, skillsShowUsage)
			return exitInvalidArgs
		}
		skill, ok, err := findSkill(rest[0])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitUnexpected
		}
		if !ok {
			fmt.Fprintf(stderr, "error: skill not found: %s\n", rest[0])
			return exitInvalidArgs
		}
		// #nosec G304 -- skill path is resolved from the repo-local skills directory.
		b, err := os.ReadFile(skill.Path)
		if err != nil {
			fmt.Fprintf(stderr, "error: reading skill: %v\n", err)
			return exitUnexpected
		}
		if format == "json" {
			detail := skillDetail{
				Name:        skill.Name,
				Description: skill.Description,
				Body:        string(b),
			}
			if err := writeJSON(stdout, detail); err != nil {
				fmt.Fprintf(stderr, "error: writing json: %v\n", err)
				return exitUnexpected
			}
			return exitSuccess
		}
		if _, err := stdout.Write(b); err != nil {
			fmt.Fprintf(stderr, "error: writing stdout: %v\n", err)
			return exitUnexpected
		}
		if len(b) == 0 || b[len(b)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return exitSuccess
	case "check":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, skillsCheckUsage)
			return exitSuccess
		}
		format, rest, err := parseFormatArgs(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitInvalidArgs
		}
		if len(rest) != 0 {
			fmt.Fprintln(stderr, `error: unexpected extra args for "skills check"`)
			fmt.Fprint(stderr, skillsCheckUsage)
			return exitInvalidArgs
		}
		skills, issues, err := checkSkills()
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitUnexpected
		}
		if format == "json" {
			result := skillCheckResult{
				OK:            len(issues) == 0,
				SkillsChecked: len(skills),
				Issues:        issues,
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
		fmt.Fprintf(stdout, "OK: %d skills checked\n", len(skills))
		return exitSuccess
	default:
		fmt.Fprintf(stderr, "unknown skills command %q\n%s", args[0], skillsUsage)
		return exitInvalidArgs
	}
}
