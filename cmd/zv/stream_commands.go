package main

import (
	"fmt"
	"io"
)

func runStream(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, streamUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, streamUsage)
		return exitSuccess
	}
	return runCanonicalDelegate(append([]string{"stream"}, args...), "zv-stream", args, stdout, stderr, stdin, runner)
}
