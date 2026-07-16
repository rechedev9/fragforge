package main

import (
	"fmt"
	"sort"
	"strings"
)

func documentedWorkflowCommand(command string) string {
	fields, ok := splitCommandFields(command)
	if !ok {
		return ""
	}
	if len(fields) == 0 || fields[0] != "zv" {
		return ""
	}
	if len(fields) >= 2 && fields[1] == "short" {
		return "./bin/zv short"
	}
	out := []string{"./bin/zv"}
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "--") || strings.HasPrefix(field, "<") {
			break
		}
		out = append(out, field)
	}
	return strings.Join(out, " ")
}

func legacySkillBinaries() []string {
	return legacyDirectBinaries()
}

func legacyWorkflowCommands() []string {
	return legacyDirectBinaries()
}

func findLegacyPassThrough(command string) (legacyPassThrough, bool) {
	for _, passThrough := range legacyPassThroughs() {
		if passThrough.Command == command {
			return passThrough, true
		}
	}
	return legacyPassThrough{}, false
}

func legacyPassThroughUsageLine(passThrough legacyPassThrough) string {
	return fmt.Sprintf("zv %s [%s args]", passThrough.Command, passThrough.Binary)
}

func legacyDirectBinaries() []string {
	names := defaultLegacyCommandEntrypointNames()
	if root, err := findWorkflowRoot(); err == nil {
		if commands, err := commandEntrypoints(root); err == nil {
			var discovered []string
			for _, command := range commands {
				if strings.HasPrefix(command, "zv-") {
					discovered = append(discovered, command)
				}
			}
			if len(discovered) > 0 {
				names = discovered
			}
		}
	}
	sort.Strings(names)
	out := make([]string, 0, len(names)*3)
	for _, name := range names {
		out = append(out, `.\bin\`+name, `bin\`+name, `./bin/`+name)
	}
	return out
}
