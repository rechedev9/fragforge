package main

import (
	"fmt"
	"io"
)

const (
	exitSuccess     = 0
	exitUnexpected  = 1
	exitInvalidArgs = 2
)

// Run executes the unified FragForge CLI. It is intentionally thin: current
// feature binaries remain the behavioral owners while zv provides one stable
// command surface for humans, scripts, and agent skills.
func Run(argv []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(argv) < 2 {
		fmt.Fprint(stderr, usage)
		return exitInvalidArgs
	}
	args := argv[1:]
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return exitSuccess
	case "short":
		return runShort(args[1:], stdout, stderr, stdin, runner)
	case "batch":
		return runBatch(args[1:], stdout, stderr)
	case "metrics":
		return runMetrics(args[1:], stdout, stderr)
	case "errors":
		return runErrors(args[1:], stdout, stderr)
	case "presets":
		return runPresets(args[1:], stdout, stderr)
	case "demo":
		return runDemo(args[1:], stdout, stderr, stdin, runner)
	case "utility":
		return runUtility(args[1:], stdout, stderr, stdin, runner)
	case "record":
		return runCanonicalDelegate(args, "zv-recorder", args[1:], stdout, stderr, stdin, runner)
	case "compose":
		return runCompose(args[1:], stdout, stderr, stdin, runner)
	case "shorts":
		return runShorts(args[1:], stdout, stderr, stdin, runner)
	case "music":
		return runMusic(args[1:], stdout, stderr, stdin, runner)
	case "analysis":
		return runAnalysis(args[1:], stdout, stderr, stdin, runner)
	case "gallery":
		return runGallery(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "skills":
		return runSkills(args[1:], stdout, stderr)
	case "workflows":
		return runWorkflows(args[1:], stdout, stderr, stdin, runner)
	case "serve":
		if len(args) == 2 && isHelp(args[1]) {
			fmt.Fprint(stdout, serveUsage)
			return exitSuccess
		}
		if len(args) != 1 {
			fmt.Fprintln(stderr, `error: unexpected extra args for "serve"`)
			fmt.Fprint(stderr, serveUsage)
			return exitInvalidArgs
		}
		return runDelegate("zv-orchestrator", args[1:], stdout, stderr, stdin, runner)
	default:
		if passThrough, ok := findLegacyPassThrough(args[0]); ok {
			return runDelegate(passThrough.Binary, args[1:], stdout, stderr, stdin, runner)
		}
		fmt.Fprintf(stderr, "unknown command %q\n%s", args[0], usage)
		return exitInvalidArgs
	}
}
