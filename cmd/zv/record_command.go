package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/rechedev9/fragforge/internal/capturetools"
)

type recordErrorResult struct {
	OK       bool   `json:"ok"`
	DryRun   bool   `json:"dry_run"`
	Executed bool   `json:"executed"`
	Error    string `json:"error"`
}

func runRecord(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if isSingleHelp(args) {
		return runDelegate("zv-recorder", args, stdout, stderr, stdin, runner)
	}
	jsonOutput := recordJSONRequested(args)
	dryRun := booleanFlagIsTrue(args, "--dry-run")
	command := append([]string{"record"}, args...)
	if issue := validateSkillCommand(command); issue != "" {
		if jsonOutput {
			return writeRecordJSONError(issue, dryRun, exitInvalidArgs, stdout, stderr)
		}
		fmt.Fprintf(stderr, "error: %s\n", issue)
		return exitInvalidArgs
	}
	resolved, err := resolveRecordCaptureArgs(args, capturePathsFor(runner))
	if err != nil {
		if jsonOutput {
			return writeRecordJSONError(err.Error(), dryRun, exitInvalidArgs, stdout, stderr)
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitInvalidArgs
	}
	if !jsonOutput {
		return runDelegate("zv-recorder", resolved, stdout, stderr, stdin, runner)
	}
	var delegateStdout, delegateStderr bytes.Buffer
	code := runDelegate("zv-recorder", resolved, &delegateStdout, &delegateStderr, stdin, runner)
	if code == exitSuccess {
		if _, err := io.Copy(stdout, &delegateStdout); err != nil {
			fmt.Fprintf(stderr, "error: writing recorder output: %v\n", err)
			return exitUnexpected
		}
		if _, err := io.Copy(stderr, &delegateStderr); err != nil {
			return exitUnexpected
		}
		return exitSuccess
	}
	message := strings.TrimSpace(delegateStderr.String())
	message = strings.TrimPrefix(message, "error: ")
	if message == "" {
		message = fmt.Sprintf("zv-recorder exited with code %d", code)
	}
	return writeRecordJSONError(message, dryRun, code, stdout, stderr)
}

func recordJSONRequested(args []string) bool {
	format, ok := flagValue(args, "--format")
	return ok && format == "json"
}

func writeRecordJSONError(message string, dryRun bool, code int, stdout, stderr io.Writer) int {
	if err := writeJSON(stdout, recordErrorResult{
		OK:       false,
		DryRun:   dryRun,
		Executed: false,
		Error:    message,
	}); err != nil {
		fmt.Fprintf(stderr, "error: writing json: %v\n", err)
		return exitUnexpected
	}
	return code
}

func resolveRecordCaptureArgs(args []string, detected capturetools.Paths) ([]string, error) {
	resolved := append([]string(nil), args...)
	if booleanFlagIsTrue(args, "--dry-run") {
		return resolved, nil
	}

	hlae, hasHLAE := flagValue(args, "--hlae")
	cs2, hasCS2 := flagValue(args, "--cs2")
	if !hasHLAE && detected.HLAE != "" {
		hlae = detected.HLAE
		resolved = append(resolved, "--hlae", hlae)
	}
	if !hasCS2 && detected.CS2 != "" {
		cs2 = detected.CS2
		resolved = append(resolved, "--cs2", cs2)
	}

	var missing []string
	if hlae == "" {
		missing = append(missing, "HLAE")
	}
	if cs2 == "" {
		missing = append(missing, "CS2")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("capture tools are unavailable (%s); inspect zv capabilities --format json, pass --hlae/--cs2, or use --dry-run", strings.Join(missing, " and "))
	}
	return resolved, nil
}
