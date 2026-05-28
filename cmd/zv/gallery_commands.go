package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func runGallery(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, galleryUsage)
		return exitInvalidArgs
	}
	if args[0] != "open" {
		if len(args) > 0 && isHelp(args[0]) {
			fmt.Fprint(stdout, galleryUsage)
			return exitSuccess
		}
		fmt.Fprintf(stderr, "unknown gallery command %q\n%s", args[0], galleryUsage)
		return exitInvalidArgs
	}
	if isSingleHelp(args[1:]) {
		fmt.Fprint(stdout, galleryUsage)
		return exitSuccess
	}
	if issue := validateSkillCommand(append([]string{"gallery"}, args...)); issue != "" {
		fmt.Fprintf(stderr, "error: %s\n", issue)
		return exitInvalidArgs
	}
	fs := flag.NewFlagSet("gallery open", flag.ContinueOnError)
	fs.SetOutput(stderr)
	path := fs.String("path", "", "path to generated gallery index.html")
	if err := fs.Parse(args[1:]); err != nil {
		return exitInvalidArgs
	}
	if strings.TrimSpace(*path) == "" {
		fmt.Fprintln(stderr, "error: --path is required")
		return exitInvalidArgs
	}
	if err := openPath(*path); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitUnexpected
	}
	fmt.Fprintf(stdout, "opened: %s\n", *path)
	return exitSuccess
}

func openPath(path string) error {
	if logPath := os.Getenv("ZV_FAKE_OPEN_PATH_LOG"); logPath != "" {
		return appendOpenPathLog(logPath, path)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// #nosec G204 -- opens an explicit local gallery path with the OS handler.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		// #nosec G204 -- opens an explicit local gallery path with the OS handler.
		cmd = exec.Command("open", path)
	default:
		// #nosec G204 -- opens an explicit local gallery path with the OS handler.
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

func appendOpenPathLog(logPath, path string) error {
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open fake path log: %w", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, path); err != nil {
		return fmt.Errorf("write fake path log: %w", err)
	}
	return nil
}
