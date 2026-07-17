package main

import (
	"fmt"
	"io"
)

func runDemo(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, demoUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, demoUsage)
		return exitSuccess
	}
	switch args[0] {
	case "parse":
		return runCanonicalDelegate(append([]string{"demo"}, args...), "zv-parser", append([]string{"parse"}, args[1:]...), stdout, stderr, stdin, runner)
	case "players":
		return runCanonicalDelegate(append([]string{"demo"}, args...), "zv-demo-players", args[1:], stdout, stderr, stdin, runner)
	case "moments":
		if issue := validateSkillCommand(append([]string{"demo"}, args...)); issue != "" {
			return writeCanonicalValidationError(args[1:], issue, stdout, stderr)
		}
		return runDemoMoments(args[1:], stdout, stderr)
	case "select":
		if issue := validateSkillCommand(append([]string{"demo"}, args...)); issue != "" {
			return writeCanonicalValidationError(args[1:], issue, stdout, stderr)
		}
		return runDemoSelect(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown demo command %q\n%s", args[0], demoUsage)
		return exitInvalidArgs
	}
}

func runUtility(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, utilityUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, utilityUsage)
		return exitSuccess
	}
	switch args[0] {
	case "audit":
		return runCanonicalDelegate(append([]string{"utility"}, args...), "zv-parser", append([]string{"utility-audit"}, args[1:]...), stdout, stderr, stdin, runner)
	default:
		fmt.Fprintf(stderr, "unknown utility command %q\n%s", args[0], utilityUsage)
		return exitInvalidArgs
	}
}

func runCompose(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, composeUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, composeUsage)
		return exitSuccess
	}
	switch args[0] {
	case "final":
		return runCanonicalDelegate(append([]string{"compose"}, args...), "zv-composer", args[1:], stdout, stderr, stdin, runner)
	default:
		fmt.Fprintf(stderr, "unknown compose command %q\n%s", args[0], composeUsage)
		return exitInvalidArgs
	}
}

func runShorts(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, shortsUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, shortsUsage)
		return exitSuccess
	}
	switch args[0] {
	case "render":
		return runCanonicalDelegate(append([]string{"shorts"}, args...), "zv-editor", args[1:], stdout, stderr, stdin, runner)
	default:
		fmt.Fprintf(stderr, "unknown shorts command %q\n%s", args[0], shortsUsage)
		return exitInvalidArgs
	}
}

func runMusic(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, musicUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, musicUsage)
		return exitSuccess
	}
	switch args[0] {
	case "analyze":
		return runCanonicalDelegate(append([]string{"music"}, args...), "zv-rhythm", args, stdout, stderr, stdin, runner)
	default:
		fmt.Fprintf(stderr, "unknown music command %q\n%s", args[0], musicUsage)
		return exitInvalidArgs
	}
}

func runAnalysis(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, analysisUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, analysisUsage)
		return exitSuccess
	}
	switch args[0] {
	case "tactical-data":
		return runCanonicalDelegate(append([]string{"analysis"}, args...), "zv-tactical-data", args[1:], stdout, stderr, stdin, runner)
	case "view":
		return runCanonicalDelegate(append([]string{"analysis"}, args...), "zv-analysis-viewer", args[1:], stdout, stderr, stdin, runner)
	default:
		fmt.Fprintf(stderr, "unknown analysis command %q\n%s", args[0], analysisUsage)
		return exitInvalidArgs
	}
}
