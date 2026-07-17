package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/rechedev9/fragforge/internal/editor"
)

// flowPlaceholderSubstitutions resolves the angle-bracket placeholders used in
// productionFlows() Command templates to concrete stand-in values that keep each
// command structurally valid for validateSkillCommand.
//
// Keep this table adjacent to the flow validator: a placeholder added to
// flow_commands.go but missing here leaves an unresolved "<...>" token and fails
// validateProductionFlows, so the descriptive flow templates cannot silently
// drift from real CLI commands. Alternatives ("<a|b|c>", "<true|false>") and
// optional groups ("[--flag <v>]") are handled structurally, not through this
// table.
var flowPlaceholderSubstitutions = map[string]string{
	"run":                 "run",
	"match.dem":           "match.dem",
	"stream.mp4":          "stream.mp4",
	"SteamID64":           "76561198000000000",
	"seg-ids":             "seg-1",
	"approved-format":     editor.OutputFormatShort9x16,
	"approved-effect":     editor.KillEffectPunchIn,
	"approved-transition": editor.TransitionCut,
	"x,y,w,h":             "0,0,100,100",
	"large-v3.bin":        "large-v3.bin",
	"large-v3-turbo.bin":  "large-v3-turbo.bin",
	"silero-vad.bin":      "silero-vad.bin",
}

var flowPlaceholderPattern = regexp.MustCompile(`<[^<>]*>`)

// validateProductionFlows binds the descriptive flowPhase.Command templates to
// the real CLI command contract: every phase whose command starts with "zv " is
// tokenized, its placeholders resolved, and validated with validateSkillCommand.
// Read-only phases running a mutating command must preflight with --dry-run.
func validateProductionFlows(flows []productionFlow) []skillIssue {
	var issues []skillIssue
	for _, flow := range flows {
		for _, phase := range flow.Phases {
			path := fmt.Sprintf("flow:%s/%s", flow.Name, phase.ID)
			if strings.TrimSpace(phase.Command) == "" {
				continue
			}
			fields, ok := splitCommandFields(phase.Command)
			if !ok || len(fields) == 0 || fields[0] != "zv" {
				issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("flow command must start with zv: %s", phase.Command)})
				continue
			}
			args, issue := resolveFlowCommandArgs(fields[1:])
			if issue != "" {
				issues = append(issues, skillIssue{Path: path, Message: issue})
				continue
			}
			if issue := validateSkillCommand(args); issue != "" {
				issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("flow command is not canonical: %s", issue)})
			}
			if issue := validateFlowReadOnlyDryRun(phase, args); issue != "" {
				issues = append(issues, skillIssue{Path: path, Message: issue})
			}
		}
	}
	return issues
}

// resolveFlowCommandArgs drops optional "[--flag <v>]" groups, resolves every
// placeholder in the remaining tokens, and reports the first token that still
// carries an unresolved angle bracket after substitution.
func resolveFlowCommandArgs(rawArgs []string) ([]string, string) {
	var args []string
	dropping := false
	for _, token := range rawArgs {
		if dropping {
			if strings.HasSuffix(token, "]") {
				dropping = false
			}
			continue
		}
		if strings.HasPrefix(token, "[") {
			if !strings.HasSuffix(token, "]") {
				dropping = true
			}
			continue
		}
		resolved := resolveFlowPlaceholders(token)
		if strings.ContainsAny(resolved, "<>") {
			return nil, fmt.Sprintf("unresolved placeholder %q in flow command; add it to flowPlaceholderSubstitutions", token)
		}
		args = append(args, resolved)
	}
	return args, ""
}

// resolveFlowPlaceholders substitutes each "<...>" placeholder in a single token.
// Alternatives ("<a|b>") resolve to the first choice; every other placeholder is
// looked up in flowPlaceholderSubstitutions and left untouched when unknown so
// resolveFlowCommandArgs can flag it.
func resolveFlowPlaceholders(token string) string {
	return flowPlaceholderPattern.ReplaceAllStringFunc(token, func(match string) string {
		inner := match[1 : len(match)-1]
		if strings.Contains(inner, "|") {
			return strings.SplitN(inner, "|", 2)[0]
		}
		if value, ok := flowPlaceholderSubstitutions[inner]; ok {
			return value
		}
		return match
	})
}

// validateFlowReadOnlyDryRun requires a --dry-run flag on a read-only phase whose
// command actually mutates. The command is resolved to its catalog workflow by
// matching the workflow's RunArgs as a leading token prefix (so positional-bearing
// commands like "short match.dem" and "flows run demo" resolve correctly), and it
// is treated as mutating when the workflow is not marked ReadOnly and either
// supports --dry-run or is long-running. This closes the earlier bypasses: the old
// heuristic keyed on commandBoolFlags containing --dry-run, which missed both
// commands with no --dry-run flag at all (music analyze) and positional-bearing
// commands whose display name never matched the commandBoolFlags table (short).
//
// Note: this deliberately does not treat every non-ReadOnly workflow as mutating.
// demo-players is intentionally marked non-ReadOnly (its --out can persist a
// roster), yet the demo flow's read-only "players" phase invokes it without --out;
// requiring a --dry-run it does not support would wrongly break a clean flow. The
// "supports dry-run or long-running" gate isolates the genuinely expensive/mutating
// stages (short, music analyze, record, render, ...) that a read-only phase must
// preflight, while leaving lightweight inspection commands alone.
func validateFlowReadOnlyDryRun(phase flowPhase, args []string) string {
	if !phase.ReadOnly {
		return ""
	}
	workflow, ok := workflowForRunArgsPrefix(args)
	if !ok {
		return ""
	}
	if workflow.Safety.ReadOnly || !(workflow.Safety.SupportsDryRun || workflow.Safety.LongRunning) {
		return ""
	}
	if booleanFlagIsTrue(args, "--dry-run") {
		return ""
	}
	return fmt.Sprintf("read-only flow phase runs mutating command %s without --dry-run", flowCommandDisplayName(args))
}

// workflowForRunArgsPrefix resolves resolved command args to the catalog
// workflow whose RunArgs form the longest leading token prefix (e.g. args
// starting with "demo select ..." resolve to demo-select, "flows run demo ..."
// to flows-run). Positionals and flags after the RunArgs prefix are ignored.
func workflowForRunArgsPrefix(args []string) (workflowInfo, bool) {
	var matched workflowInfo
	matchedLen := 0
	for _, workflow := range workflowCatalog() {
		runArgs := workflow.RunArgs
		if len(runArgs) == 0 || len(runArgs) <= matchedLen || len(runArgs) > len(args) {
			continue
		}
		if hasLeadingTokens(args, runArgs) {
			matched = workflow
			matchedLen = len(runArgs)
		}
	}
	return matched, matchedLen > 0
}

func hasLeadingTokens(args, prefix []string) bool {
	if len(prefix) > len(args) {
		return false
	}
	for i, token := range prefix {
		if args[i] != token {
			return false
		}
	}
	return true
}

// flowCommandDisplayName returns the quoted subcommand path (e.g. `"demo select"`)
// used as the key into commandBoolFlags, i.e. the leading non-flag tokens.
func flowCommandDisplayName(args []string) string {
	var parts []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		parts = append(parts, arg)
	}
	return `"` + strings.Join(parts, " ") + `"`
}
