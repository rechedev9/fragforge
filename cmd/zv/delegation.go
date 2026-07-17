package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rechedev9/fragforge/internal/capturetools"
)

type commandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

type capturePathProvider interface {
	CapturePaths() capturetools.Paths
}

type osCommandRunner struct{}

func (osCommandRunner) CapturePaths() capturetools.Paths {
	paths, _ := capturetools.Detect(capturetools.FromEnvironment())
	return paths
}

func (osCommandRunner) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	// #nosec G204 -- this CLI delegates only to fixed FragForge subcommand binaries.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func runDelegate(name string, args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	exe := resolveExecutable(name)
	if err := runner.Run(context.Background(), exe, args, stdin, stdout, stderr); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		fmt.Fprintf(stderr, "error: run %s: %v\n", name, err)
		return exitUnexpected
	}
	return exitSuccess
}

func runCanonicalDelegate(command []string, name string, args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if issue := validateSkillCommand(command); issue != "" {
		return writeCanonicalValidationError(command, issue, stdout, stderr)
	}
	return runDelegate(name, args, stdout, stderr, stdin, runner)
}

func writeCanonicalValidationError(command []string, issue string, stdout, stderr io.Writer) int {
	if shortJSONRequested(command) {
		if err := writeJSON(stdout, map[string]any{
			"ok":       false,
			"scope":    "arguments",
			"executed": false,
			"error":    issue,
		}); err != nil {
			fmt.Fprintf(stderr, "error: writing json: %v\n", err)
			return exitUnexpected
		}
		return exitInvalidArgs
	}
	fmt.Fprintf(stderr, "error: %s\n", issue)
	return exitInvalidArgs
}

func capturePathsFor(runner commandRunner) capturetools.Paths {
	if provider, ok := runner.(capturePathProvider); ok {
		return provider.CapturePaths()
	}
	return capturetools.FromEnvironment()
}

func resolveExecutable(name string) string {
	for _, candidate := range executableCandidates(name) {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if found, err := exec.LookPath(executableName(name)); err == nil {
		return found
	}
	return executableName(name)
}

func executableCandidates(name string) []string {
	exeName := executableName(name)
	var out []string
	if current, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(current), exeName))
	}
	if cwd, err := os.Getwd(); err == nil {
		out = append(out, filepath.Join(cwd, "bin", exeName))
	}
	return out
}

func executableName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}
