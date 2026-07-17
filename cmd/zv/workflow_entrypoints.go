package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func checkCommandBuildTargets() ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	commands, err := commandEntrypoints(root)
	if err != nil {
		return nil, err
	}

	makefileBody, err := readWorkflowFile(root, "Makefile")
	if err != nil {
		return nil, err
	}
	buildScriptBody, err := readWorkflowFile(root, "scripts/build.ps1")
	if err != nil {
		return nil, err
	}

	var issues []skillIssue
	known := make(map[string]struct{}, len(commands))
	for _, command := range commands {
		known[command] = struct{}{}
		makeTarget := fmt.Sprintf("go build -o bin/%s ./cmd/%s", command, command)
		if !strings.Contains(makefileBody, makeTarget) {
			issues = append(issues, skillIssue{Path: "Makefile", Message: fmt.Sprintf("missing command build target %s", makeTarget)})
		}
		buildEntry := fmt.Sprintf(`"%s"`, command)
		if !strings.Contains(buildScriptBody, buildEntry) {
			issues = append(issues, skillIssue{Path: "scripts/build.ps1", Message: fmt.Sprintf("missing command build entry %s", buildEntry)})
		}
	}
	if len(commands) > 0 {
		for _, target := range makefileCommandBuildTargets(makefileBody) {
			if _, ok := known[target.Command]; ok {
				continue
			}
			issues = append(issues, skillIssue{Path: "Makefile", Message: fmt.Sprintf("stale command build target %s", target.Line)})
		}
		for _, command := range buildScriptCommandEntries(buildScriptBody) {
			if _, ok := known[command]; ok {
				continue
			}
			issues = append(issues, skillIssue{Path: "scripts/build.ps1", Message: fmt.Sprintf("stale command build entry %q", command)})
		}
	}
	return issues, nil
}

func checkCommandEntrypointCoverage(workflows []workflowInfo) ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	commands, err := commandEntrypoints(root)
	if err != nil {
		return nil, err
	}

	covered := map[string]struct{}{
		"zv": {},
	}
	for _, workflow := range workflows {
		if command := workflowDelegatedCommand(workflow.RunArgs); command != "" {
			covered[command] = struct{}{}
		}
	}
	for _, passThrough := range legacyPassThroughs() {
		covered[passThrough.Binary] = struct{}{}
	}

	var issues []skillIssue
	for _, command := range commands {
		if _, ok := covered[command]; ok {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    filepath.ToSlash(filepath.Join("cmd", command)),
			Message: "command entrypoint is not covered by zv workflows or legacy pass-throughs",
		})
	}
	issues = append(issues, validateLegacyPassThroughEntrypoints(commands)...)
	return issues, nil
}

func validateLegacyPassThroughEntrypoints(commands []string) []skillIssue {
	known := make(map[string]struct{}, len(commands))
	for _, command := range commands {
		known[command] = struct{}{}
	}
	if _, ok := known["zv"]; !ok || len(commands) < len(legacyPassThroughs())+1 {
		return nil
	}

	var issues []skillIssue
	for _, passThrough := range legacyPassThroughs() {
		if _, ok := known[passThrough.Binary]; ok {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    "pass-through:" + passThrough.Command,
			Message: fmt.Sprintf("legacy pass-through references missing command entrypoint %s", passThrough.Binary),
		})
	}
	return issues
}

func workflowDelegatedCommand(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "short":
		return "zv"
	case "demo":
		if len(args) < 2 {
			return ""
		}
		switch args[1] {
		case "parse":
			return "zv-parser"
		case "players":
			return "zv-demo-players"
		case "moments", "select":
			return "zv"
		}
	case "utility":
		if len(args) >= 2 && args[1] == "audit" {
			return "zv-parser"
		}
	case "record":
		return "zv-recorder"
	case "compose":
		if len(args) >= 2 && args[1] == "final" {
			return "zv-composer"
		}
	case "shorts":
		if len(args) >= 2 && args[1] == "render" {
			return "zv-editor"
		}
	case "stream":
		if len(args) >= 2 && (args[1] == "variants" || args[1] == "plan" || args[1] == "killfeed" || args[1] == "transcribe" || args[1] == "captions" || args[1] == "render") {
			return "zv-stream"
		}
	case "music":
		if len(args) >= 2 && args[1] == "analyze" {
			return "zv-rhythm"
		}
	case "analysis":
		if len(args) < 2 {
			return ""
		}
		switch args[1] {
		case "tactical-data":
			return "zv-tactical-data"
		case "view":
			return "zv-analysis-viewer"
		}
	case "serve":
		return "zv-orchestrator"
	case "flows":
		if len(args) >= 2 && args[1] == "run" {
			return "zv"
		}
	case "capabilities", "gallery", "skills", "workflows", "check":
		return "zv"
	}
	return ""
}

type commandBuildTarget struct {
	Command string
	Line    string
}

func makefileCommandBuildTargets(body string) []commandBuildTarget {
	var out []commandBuildTarget
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		fields, ok := splitCommandFields(trimmed)
		if !ok || len(fields) != 5 {
			continue
		}
		if fields[0] != "go" || fields[1] != "build" || fields[2] != "-o" {
			continue
		}
		outName, ok := strings.CutPrefix(fields[3], "bin/")
		if !ok {
			continue
		}
		pkgName, ok := strings.CutPrefix(fields[4], "./cmd/")
		if !ok || outName != pkgName {
			continue
		}
		out = append(out, commandBuildTarget{Command: outName, Line: trimmed})
	}
	return out
}

func buildScriptCommandEntries(body string) []string {
	var out []string
	inCommands := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inCommands {
			if strings.HasPrefix(trimmed, "$commands") && strings.Contains(trimmed, "@(") {
				inCommands = true
			}
			continue
		}
		if trimmed == ")" {
			break
		}
		trimmed = strings.TrimSuffix(trimmed, ",")
		trimmed = strings.TrimSpace(trimmed)
		trimmed = strings.Trim(trimmed, `"'`)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func commandEntrypoints(root string) ([]string, error) {
	cmdDir := filepath.Join(root, "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cmd dir: %w", err)
	}
	var commands []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mainPath := filepath.Join(cmdDir, entry.Name(), "main.go")
		if _, err := os.Stat(mainPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("stat command main %s: %w", entry.Name(), err)
		}
		commands = append(commands, entry.Name())
	}
	return commands, nil
}

func readWorkflowFile(root, path string) (string, error) {
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	// #nosec G304 -- workflow files are fixed repo-local paths.
	b, err := os.ReadFile(fullPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(b), nil
}
