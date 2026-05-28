package main

import (
	"strings"
	"unicode"
)

func skillCommandLines(body string) []string {
	var out []string
	var current []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(current) == 0 {
			if containsZVCommand(trimmed) {
				current = append(current, trimmed)
				if !hasLineContinuation(trimmed) {
					out = append(out, joinSkillCommandLine(current))
					current = nil
				}
			}
			continue
		}
		current = append(current, trimmed)
		if !hasLineContinuation(trimmed) {
			out = append(out, joinSkillCommandLine(current))
			current = nil
		}
	}
	if len(current) > 0 {
		out = append(out, joinSkillCommandLine(current))
	}
	return out
}

func containsZVCommand(line string) bool {
	fields, ok := splitCommandFields(line)
	if !ok {
		return false
	}
	return zvExecutableFieldIndex(fields) >= 0
}

func hasLineContinuation(line string) bool {
	line = strings.TrimSpace(line)
	return (strings.HasSuffix(line, "`") && !strings.HasPrefix(line, "`")) || strings.HasSuffix(line, `\`)
}

func joinSkillCommandLine(lines []string) string {
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimSuffix(line, "`")
		line = strings.TrimSuffix(line, `\`)
		line = strings.TrimSpace(line)
		if line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}

func skillCommand(line string) ([]string, bool) {
	line = strings.TrimSuffix(line, "`")
	line = strings.TrimPrefix(line, "`")
	line = strings.TrimSpace(line)
	fields, ok := splitCommandFields(line)
	if !ok {
		return nil, false
	}
	i := zvExecutableFieldIndex(fields)
	if i < 0 {
		return nil, false
	}
	if i+1 >= len(fields) {
		return nil, false
	}
	return fields[i+1:], true
}

func zvExecutableFieldIndex(fields []string) int {
	if len(fields) == 0 {
		return -1
	}
	start := 0
	for start < len(fields) && isCommandPrefixField(fields[start]) {
		start++
	}
	if start < len(fields) && isZVExecutableField(fields[start]) {
		return start
	}
	return -1
}

func isCommandPrefixField(field string) bool {
	switch field {
	case "-", "*", "+", "$", ">", "PS>", "&":
		return true
	default:
		return false
	}
}

func isZVExecutableField(field string) bool {
	field = strings.Trim(field, "`")
	switch field {
	case `.\bin\zv`, `.\bin\zv.exe`, `bin\zv`, `bin\zv.exe`, `./bin/zv`, `./bin/zv.exe`, `bin/zv`, `bin/zv.exe`:
		return true
	default:
		return false
	}
}

func splitCommandFields(line string) ([]string, bool) {
	var fields []string
	var field strings.Builder
	var quote rune
	inField := false

	for _, r := range line {
		if quote != 0 {
			if r == quote {
				quote = 0
				inField = true
				continue
			}
			field.WriteRune(r)
			inField = true
			continue
		}

		switch {
		case r == '"' || r == '\'':
			quote = r
			inField = true
		case unicode.IsSpace(r):
			if inField {
				fields = append(fields, field.String())
				field.Reset()
				inField = false
			}
		default:
			field.WriteRune(r)
			inField = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	if inField {
		fields = append(fields, field.String())
	}
	return fields, true
}
