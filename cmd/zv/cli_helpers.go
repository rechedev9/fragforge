package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

func parseFormatArgs(args []string) (string, []string, error) {
	format := "text"
	formatSet := false
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--format" {
			if formatSet {
				return "", nil, fmt.Errorf("duplicate flag --format")
			}
			if i+1 >= len(args) {
				return "", nil, fmt.Errorf("--format requires a value")
			}
			format = args[i+1]
			formatSet = true
			i++
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--format="); ok {
			if formatSet {
				return "", nil, fmt.Errorf("duplicate flag --format")
			}
			format = value
			formatSet = true
			continue
		}
		rest = append(rest, arg)
	}
	if format != "text" && format != "json" {
		return "", nil, fmt.Errorf("unsupported format %q", format)
	}
	return format, rest, nil
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func isHelp(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

func isSingleHelp(args []string) bool {
	return len(args) == 1 && isHelp(args[0])
}
