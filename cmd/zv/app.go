package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	exitSuccess     = 0
	exitUnexpected  = 1
	exitInvalidArgs = 2
)

const usage = `zv - deterministic CS2 demo-to-video workflows

Usage:
  zv demo parse [zv-parser parse flags]
  zv demo players [zv-demo-players flags]
  zv utility audit [zv-parser utility-audit flags]
  zv record [zv-recorder flags]
  zv compose final [zv-composer flags]
  zv shorts render [zv-editor flags]
  zv analysis tactical-data [zv-tactical-data flags]
  zv analysis view [zv-analysis-viewer flags]
  zv gallery open --path <index.html>
  zv check
  zv skills list
  zv skills show <name>
  zv skills check
  zv workflows list
  zv workflows show <name>
  zv workflows run <name> -- [workflow flags]
  zv workflows check
  zv serve
  zv pipeline [zv-pipeline flags]

Legacy pass-throughs:
  zv parser [zv-parser args]
  zv editor [zv-editor args]
  zv recorder [zv-recorder args]
  zv composer [zv-composer args]
  zv orchestrator [zv-orchestrator args]
  zv analysis-viewer [zv-analysis-viewer args]
  zv tactical-data [zv-tactical-data args]

Use "zv <command> --help" for the underlying command help.
`

const demoUsage = `usage: zv demo parse [zv-parser parse flags] | zv demo players [zv-demo-players flags]
`

const utilityUsage = `usage: zv utility audit [zv-parser utility-audit flags]
`

const composeUsage = `usage: zv compose final [zv-composer flags]
`

const shortsUsage = `usage: zv shorts render [zv-editor flags]
`

const analysisUsage = `usage: zv analysis tactical-data [zv-tactical-data flags] | zv analysis view [zv-analysis-viewer flags]
`

const galleryUsage = `usage: zv gallery open --path <index.html>
`

const serveUsage = `usage: zv serve
`

const checkUsage = `usage: zv check [--format text|json]
`

const skillsUsage = `usage: zv skills list [--format text|json] | zv skills show <name> [--format text|json] | zv skills check [--format text|json]
`

const skillsListUsage = `usage: zv skills list [--format text|json]
`

const skillsShowUsage = `usage: zv skills show <name> [--format text|json]
`

const skillsCheckUsage = `usage: zv skills check [--format text|json]
`

const workflowsUsage = `usage: zv workflows list [--format text|json] | zv workflows show <name> [--format text|json] | zv workflows run <name> -- [workflow flags] | zv workflows check [--format text|json]
`

const workflowsListUsage = `usage: zv workflows list [--format text|json]
`

const workflowsShowUsage = `usage: zv workflows show <name> [--format text|json]
`

const workflowsRunUsage = `usage: zv workflows run <name> -- [workflow flags]
`

const workflowsCheckUsage = `usage: zv workflows check [--format text|json]
`

type commandRunner interface {
	Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error
}

type osCommandRunner struct{}

type skillInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"-"`
}

type skillDetail struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

type skillIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type skillCheckResult struct {
	OK            bool         `json:"ok"`
	SkillsChecked int          `json:"skills_checked"`
	Issues        []skillIssue `json:"issues"`
}

type workflowDoc struct {
	Path              string
	Required          []string
	RequiredSkills    bool
	RequiredWorkflows bool
	Body              string
}

type workflowCheckResult struct {
	OK                         bool         `json:"ok"`
	SkillsChecked              int          `json:"skills_checked"`
	WorkflowsChecked           int          `json:"workflows_checked"`
	WorkflowDocsChecked        int          `json:"workflow_docs_checked"`
	AgentPromptWrappersChecked int          `json:"agent_prompt_wrappers_checked"`
	Issues                     []skillIssue `json:"issues"`
}

type workflowInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Command     string   `json:"command"`
	RunCommand  string   `json:"run_command"`
	RunArgs     []string `json:"-"`
}

func (osCommandRunner) Run(ctx context.Context, name string, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	// #nosec G204 -- this CLI delegates only to fixed ZackVideo subcommand binaries.
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

// Run executes the unified ZackVideo CLI. It is intentionally thin: current
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
	case "pipeline":
		return runCanonicalDelegate(args, "zv-pipeline", args[1:], stdout, stderr, stdin, runner)
	default:
		if passThrough, ok := findLegacyPassThrough(args[0]); ok {
			return runDelegate(passThrough.Binary, args[1:], stdout, stderr, stdin, runner)
		}
		fmt.Fprintf(stderr, "unknown command %q\n%s", args[0], usage)
		return exitInvalidArgs
	}
}

func runDemo(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, demoUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, demoUsage)
		return exitSuccess
	}
	switch args[0] {
	case "parse":
		return runCanonicalDelegate(append([]string{"demo"}, args...), "zv-parser", append([]string{"parse"}, args[1:]...), stdout, stderr, stdin, runner)
	case "players":
		return runCanonicalDelegate(append([]string{"demo"}, args...), "zv-demo-players", args[1:], stdout, stderr, stdin, runner)
	default:
		fmt.Fprintf(stderr, "unknown demo command %q\n%s", args[0], demoUsage)
		return exitInvalidArgs
	}
}

func runUtility(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, utilityUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, utilityUsage)
		return exitSuccess
	}
	switch args[0] {
	case "audit":
		return runCanonicalDelegate(append([]string{"utility"}, args...), "zv-parser", append([]string{"utility-audit"}, args[1:]...), stdout, stderr, stdin, runner)
	default:
		fmt.Fprintf(stderr, "unknown utility command %q\n%s", args[0], utilityUsage)
		return exitInvalidArgs
	}
}

func runCompose(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, composeUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, composeUsage)
		return exitSuccess
	}
	switch args[0] {
	case "final":
		return runCanonicalDelegate(append([]string{"compose"}, args...), "zv-composer", args[1:], stdout, stderr, stdin, runner)
	default:
		fmt.Fprintf(stderr, "unknown compose command %q\n%s", args[0], composeUsage)
		return exitInvalidArgs
	}
}

func runShorts(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, shortsUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, shortsUsage)
		return exitSuccess
	}
	switch args[0] {
	case "render":
		return runCanonicalDelegate(append([]string{"shorts"}, args...), "zv-editor", args[1:], stdout, stderr, stdin, runner)
	default:
		fmt.Fprintf(stderr, "unknown shorts command %q\n%s", args[0], shortsUsage)
		return exitInvalidArgs
	}
}

func runAnalysis(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, analysisUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, analysisUsage)
		return exitSuccess
	}
	switch args[0] {
	case "tactical-data":
		return runCanonicalDelegate(append([]string{"analysis"}, args...), "zv-tactical-data", args[1:], stdout, stderr, stdin, runner)
	case "view":
		return runCanonicalDelegate(append([]string{"analysis"}, args...), "zv-analysis-viewer", args[1:], stdout, stderr, stdin, runner)
	default:
		fmt.Fprintf(stderr, "unknown analysis command %q\n%s", args[0], analysisUsage)
		return exitInvalidArgs
	}
}

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

func runCheck(args []string, stdout, stderr io.Writer) int {
	return runWorkflowContractCheck(args, stdout, stderr, checkUsage, "check")
}

func runWorkflowContractCheck(args []string, stdout, stderr io.Writer, usage string, commandName string) int {
	if len(args) > 0 && isHelp(args[0]) {
		fmt.Fprint(stdout, usage)
		return exitSuccess
	}
	format, rest, err := parseFormatArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitInvalidArgs
	}
	if len(rest) != 0 {
		fmt.Fprintf(stderr, "error: unexpected extra args for %q\n", commandName)
		fmt.Fprint(stderr, usage)
		return exitInvalidArgs
	}
	skills, workflows, docs, agentPromptWrappersChecked, issues, err := checkWorkflows()
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitUnexpected
	}
	if format == "json" {
		result := workflowCheckResult{
			OK:                         len(issues) == 0,
			SkillsChecked:              len(skills),
			WorkflowsChecked:           len(workflows),
			WorkflowDocsChecked:        len(docs),
			AgentPromptWrappersChecked: agentPromptWrappersChecked,
			Issues:                     issues,
		}
		if result.Issues == nil {
			result.Issues = []skillIssue{}
		}
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: writing json: %v\n", err)
			return exitUnexpected
		}
		if !result.OK {
			return exitInvalidArgs
		}
		return exitSuccess
	}
	if len(issues) > 0 {
		for _, issue := range issues {
			fmt.Fprintf(stderr, "%s: %s\n", issue.Path, issue.Message)
		}
		return exitInvalidArgs
	}
	fmt.Fprintf(stdout, "OK: %d skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked\n", len(skills), len(workflows), len(docs), agentPromptWrappersChecked)
	return exitSuccess
}

func runSkills(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, skillsUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, skillsUsage)
		return exitSuccess
	}
	switch args[0] {
	case "list":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, skillsListUsage)
			return exitSuccess
		}
		format, rest, err := parseFormatArgs(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitInvalidArgs
		}
		if len(rest) != 0 {
			fmt.Fprintln(stderr, `error: unexpected extra args for "skills list"`)
			fmt.Fprint(stderr, skillsListUsage)
			return exitInvalidArgs
		}
		skills, err := loadSkills()
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitUnexpected
		}
		if format == "json" {
			if err := writeJSON(stdout, skills); err != nil {
				fmt.Fprintf(stderr, "error: writing json: %v\n", err)
				return exitUnexpected
			}
			return exitSuccess
		}
		for _, skill := range skills {
			if skill.Description == "" {
				fmt.Fprintln(stdout, skill.Name)
				continue
			}
			fmt.Fprintf(stdout, "%s\t%s\n", skill.Name, skill.Description)
		}
		return exitSuccess
	case "show":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, skillsShowUsage)
			return exitSuccess
		}
		format, rest, err := parseFormatArgs(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitInvalidArgs
		}
		if len(rest) == 0 {
			fmt.Fprintln(stderr, `error: missing skill name for "skills show"`)
			fmt.Fprint(stderr, skillsShowUsage)
			return exitInvalidArgs
		}
		if len(rest) > 1 {
			fmt.Fprintln(stderr, `error: unexpected extra args for "skills show"`)
			fmt.Fprint(stderr, skillsShowUsage)
			return exitInvalidArgs
		}
		skill, ok, err := findSkill(rest[0])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitUnexpected
		}
		if !ok {
			fmt.Fprintf(stderr, "error: skill not found: %s\n", rest[0])
			return exitInvalidArgs
		}
		// #nosec G304 -- skill path is resolved from the repo-local skills directory.
		b, err := os.ReadFile(skill.Path)
		if err != nil {
			fmt.Fprintf(stderr, "error: reading skill: %v\n", err)
			return exitUnexpected
		}
		if format == "json" {
			detail := skillDetail{
				Name:        skill.Name,
				Description: skill.Description,
				Body:        string(b),
			}
			if err := writeJSON(stdout, detail); err != nil {
				fmt.Fprintf(stderr, "error: writing json: %v\n", err)
				return exitUnexpected
			}
			return exitSuccess
		}
		if _, err := stdout.Write(b); err != nil {
			fmt.Fprintf(stderr, "error: writing stdout: %v\n", err)
			return exitUnexpected
		}
		if len(b) == 0 || b[len(b)-1] != '\n' {
			fmt.Fprintln(stdout)
		}
		return exitSuccess
	case "check":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, skillsCheckUsage)
			return exitSuccess
		}
		format, rest, err := parseFormatArgs(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitInvalidArgs
		}
		if len(rest) != 0 {
			fmt.Fprintln(stderr, `error: unexpected extra args for "skills check"`)
			fmt.Fprint(stderr, skillsCheckUsage)
			return exitInvalidArgs
		}
		skills, issues, err := checkSkills()
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitUnexpected
		}
		if format == "json" {
			result := skillCheckResult{
				OK:            len(issues) == 0,
				SkillsChecked: len(skills),
				Issues:        issues,
			}
			if result.Issues == nil {
				result.Issues = []skillIssue{}
			}
			if err := writeJSON(stdout, result); err != nil {
				fmt.Fprintf(stderr, "error: writing json: %v\n", err)
				return exitUnexpected
			}
			if !result.OK {
				return exitInvalidArgs
			}
			return exitSuccess
		}
		if len(issues) > 0 {
			for _, issue := range issues {
				fmt.Fprintf(stderr, "%s: %s\n", issue.Path, issue.Message)
			}
			return exitInvalidArgs
		}
		fmt.Fprintf(stdout, "OK: %d skills checked\n", len(skills))
		return exitSuccess
	default:
		fmt.Fprintf(stderr, "unknown skills command %q\n%s", args[0], skillsUsage)
		return exitInvalidArgs
	}
}

func runWorkflows(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, workflowsUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, workflowsUsage)
		return exitSuccess
	}
	switch args[0] {
	case "list":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, workflowsListUsage)
			return exitSuccess
		}
		format, rest, err := parseFormatArgs(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitInvalidArgs
		}
		if len(rest) != 0 {
			fmt.Fprintln(stderr, `error: unexpected extra args for "workflows list"`)
			fmt.Fprint(stderr, workflowsListUsage)
			return exitInvalidArgs
		}
		workflows := workflowCatalog()
		if format == "json" {
			if err := writeJSON(stdout, workflows); err != nil {
				fmt.Fprintf(stderr, "error: writing json: %v\n", err)
				return exitUnexpected
			}
			return exitSuccess
		}
		for _, workflow := range workflows {
			fmt.Fprintf(stdout, "%s\t%s\n", workflow.Name, workflow.Description)
		}
		return exitSuccess
	case "show":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, workflowsShowUsage)
			return exitSuccess
		}
		format, rest, err := parseFormatArgs(args[1:])
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitInvalidArgs
		}
		if len(rest) == 0 {
			fmt.Fprintln(stderr, `error: missing workflow name for "workflows show"`)
			fmt.Fprint(stderr, workflowsShowUsage)
			return exitInvalidArgs
		}
		if len(rest) > 1 {
			fmt.Fprintln(stderr, `error: unexpected extra args for "workflows show"`)
			fmt.Fprint(stderr, workflowsShowUsage)
			return exitInvalidArgs
		}
		workflow, ok := findWorkflow(rest[0])
		if !ok {
			fmt.Fprintf(stderr, "error: workflow not found: %s\n", rest[0])
			return exitInvalidArgs
		}
		if format == "json" {
			if err := writeJSON(stdout, workflow); err != nil {
				fmt.Fprintf(stderr, "error: writing json: %v\n", err)
				return exitUnexpected
			}
			return exitSuccess
		}
		fmt.Fprintf(stdout, "%s\n%s\n\ncommand: %s\nrun_command: %s\n", workflow.Name, workflow.Description, workflow.Command, workflow.RunCommand)
		return exitSuccess
	case "run":
		if len(args) < 2 {
			fmt.Fprint(stderr, workflowsRunUsage)
			return exitInvalidArgs
		}
		workflow, ok := findWorkflow(args[1])
		if !ok {
			fmt.Fprintf(stderr, "error: workflow not found: %s\n", args[1])
			return exitInvalidArgs
		}
		rest := args[2:]
		if issue := validateWorkflowRunForwardedArgs(workflow, rest); issue != "" {
			fmt.Fprintf(stderr, "error: %s\n", issue)
			fmt.Fprint(stderr, workflowsRunUsage)
			return exitInvalidArgs
		}
		if len(rest) > 0 {
			if rest[0] != "--" {
				fmt.Fprintln(stderr, `error: missing "--" separator before forwarded args`)
				fmt.Fprint(stderr, workflowsRunUsage)
				return exitInvalidArgs
			}
			rest = rest[1:]
		}
		runArgs := append([]string{"zv"}, workflow.RunArgs...)
		runArgs = append(runArgs, rest...)
		return Run(runArgs, stdout, stderr, stdin, runner)
	case "check":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, workflowsCheckUsage)
			return exitSuccess
		}
		return runWorkflowContractCheck(args[1:], stdout, stderr, workflowsCheckUsage, "workflows check")
	default:
		fmt.Fprintf(stderr, "unknown workflows command %q\n%s", args[0], workflowsUsage)
		return exitInvalidArgs
	}
}

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
		fmt.Fprintf(stderr, "error: %s\n", issue)
		return exitInvalidArgs
	}
	return runDelegate(name, args, stdout, stderr, stdin, runner)
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

func findSkill(name string) (skillInfo, bool, error) {
	skills, err := loadSkills()
	if err != nil {
		return skillInfo{}, false, err
	}
	for _, skill := range skills {
		if skill.Name == name {
			return skill, true, nil
		}
	}
	return skillInfo{}, false, nil
}

func loadSkills() ([]skillInfo, error) {
	dir, err := findSkillsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read skills dir: %w", err)
	}
	skills := make([]skillInfo, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("stat skill %s: %w", entry.Name(), err)
		}
		skill, err := parseSkill(path)
		if err != nil {
			return nil, err
		}
		if skill.Name == "" {
			skill.Name = entry.Name()
		}
		skills = append(skills, skill)
	}
	return skills, nil
}

func checkSkills() ([]skillInfo, []skillIssue, error) {
	skills, err := loadSkills()
	if err != nil {
		return nil, nil, err
	}
	var issues []skillIssue
	if len(skills) == 0 {
		issues = append(issues, skillIssue{Path: ".codex/skills", Message: "no skills found"})
		return skills, issues, nil
	}
	seenSkills := make(map[string]string, len(skills))
	for _, skill := range skills {
		// #nosec G304 -- skill path is resolved from the repo-local skills directory.
		b, err := os.ReadFile(skill.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("read skill %s: %w", skill.Path, err)
		}
		body := string(b)
		if strings.TrimSpace(skill.Name) == "" {
			issues = append(issues, skillIssue{Path: skill.Path, Message: "missing skill name"})
		} else {
			if !isWorkflowSlug(skill.Name) {
				issues = append(issues, skillIssue{Path: skill.Path, Message: "skill name must be a lowercase slug"})
			}
			if dirName := skillDirName(skill); dirName != "" && skill.Name != dirName {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("skill name %q must match directory %q", skill.Name, dirName)})
			}
			if firstPath, ok := seenSkills[skill.Name]; ok {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("duplicate skill name %q also used by %s", skill.Name, firstPath)})
			} else {
				seenSkills[skill.Name] = skill.Path
			}
		}
		if strings.TrimSpace(skill.Description) == "" {
			issues = append(issues, skillIssue{Path: skill.Path, Message: "missing skill description"})
		}
		if !strings.Contains(body, `.\\bin\\zv.exe`) && !strings.Contains(body, `.\bin\zv.exe`) && !strings.Contains(body, `./bin/zv`) {
			issues = append(issues, skillIssue{Path: skill.Path, Message: "does not document the unified zv CLI"})
		}
		for _, legacy := range legacySkillBinaries() {
			if strings.Contains(body, legacy) {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("documents legacy direct binary %s", legacy)})
			}
		}
		hasWorkflowRun := false
		documentedWorkflowRuns := make(map[string]struct{})
		var documentedWorkflowRunOrder []string
		for _, line := range skillCommandLines(body) {
			command, ok := skillCommand(line)
			if !ok {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("could not parse zv command line %q", line)})
				continue
			}
			if isExecutableWorkflowRunCommand(command) {
				hasWorkflowRun = true
				if _, ok := documentedWorkflowRuns[command[2]]; ok {
					issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("duplicate workflow run %s", command[2])})
				}
				documentedWorkflowRuns[command[2]] = struct{}{}
				documentedWorkflowRunOrder = append(documentedWorkflowRunOrder, command[2])
			}
			if issue := validateSkillCommand(command); issue != "" {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("%s in %q", issue, line)})
			}
			if issue := validateSkillWorkflowEntrypoint(command); issue != "" {
				issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("%s in %q", issue, line)})
			}
		}
		if !hasWorkflowRun {
			issues = append(issues, skillIssue{Path: skill.Path, Message: "does not document a cataloged workflow run command"})
		}
		for _, required := range skillWorkflowRequirements(skill.Name) {
			if _, ok := documentedWorkflowRuns[required]; ok {
				continue
			}
			issues = append(issues, skillIssue{Path: skill.Path, Message: fmt.Sprintf("missing required workflow run %s", required)})
		}
		if issue := validateSkillRequiredWorkflowRunSet(skill.Name, documentedWorkflowRunOrder); issue != "" {
			issues = append(issues, skillIssue{Path: skill.Path, Message: issue})
		}
		if issue := validateSkillWorkflowRunCatalogOrder(documentedWorkflowRunOrder); issue != "" {
			issues = append(issues, skillIssue{Path: skill.Path, Message: issue})
		}
		if issue := validateSkillRequiredWorkflowRunOrder(skill.Name, documentedWorkflowRunOrder); issue != "" {
			issues = append(issues, skillIssue{Path: skill.Path, Message: issue})
		}
	}
	issues = append(issues, validateSkillWorkflowRequirementSkills(skills, skillWorkflowRequirementMap())...)
	return skills, issues, nil
}

func skillWorkflowRequirements(name string) []string {
	return skillWorkflowRequirementMap()[name]
}

func validateSkillRequiredWorkflowRunSet(skillName string, documented []string) string {
	required := skillWorkflowRequirements(skillName)
	if len(required) == 0 {
		return ""
	}
	allowed := make(map[string]struct{}, len(required))
	for _, workflowName := range required {
		allowed[workflowName] = struct{}{}
	}
	for _, workflowName := range documented {
		if _, ok := allowed[workflowName]; ok {
			continue
		}
		return fmt.Sprintf("unexpected workflow run %s; expected only: %s", workflowName, strings.Join(required, ", "))
	}
	return ""
}

func validateSkillRequiredWorkflowRunOrder(skillName string, documented []string) string {
	required := skillWorkflowRequirements(skillName)
	if len(required) < 2 {
		return ""
	}
	positions := make(map[string]int, len(required))
	for i, workflowName := range documented {
		if _, ok := positions[workflowName]; ok {
			continue
		}
		positions[workflowName] = i
	}
	last := -1
	for _, workflowName := range required {
		pos, ok := positions[workflowName]
		if !ok {
			return ""
		}
		if pos < last {
			return fmt.Sprintf("required workflow runs must appear in order: %s", strings.Join(required, ", "))
		}
		last = pos
	}
	return ""
}

func validateSkillWorkflowRunCatalogOrder(documented []string) string {
	if len(documented) < 2 {
		return ""
	}
	catalog := workflowCatalog()
	catalogOrder := make(map[string]int, len(catalog))
	for i, workflow := range catalog {
		catalogOrder[workflow.Name] = i
	}
	lastIndex := -1
	lastWorkflow := ""
	for _, workflowName := range documented {
		index, ok := catalogOrder[workflowName]
		if !ok {
			continue
		}
		if index < lastIndex {
			return fmt.Sprintf("workflow runs must follow catalog order; %s appears after %s", workflowName, lastWorkflow)
		}
		lastIndex = index
		lastWorkflow = workflowName
	}
	return ""
}

func skillWorkflowRequirementMap() map[string][]string {
	return map[string][]string{
		"zackvideo-cs2-utility-shorts":     {"demo-parse", "utility-audit", "record", "shorts-render", "gallery-open"},
		"zackvideo-lineup-audit":           {"utility-audit"},
		"zackvideo-shorts-production":      {"demo-parse", "demo-players", "utility-audit", "record", "shorts-render", "gallery-open"},
		"zackvideo-youtube-shorts-publish": {"gallery-open"},
	}
}

func skillDirName(skill skillInfo) string {
	dir := filepath.Dir(skill.Path)
	if dir == "." || dir == string(filepath.Separator) {
		return ""
	}
	return filepath.Base(dir)
}

func isWorkflowRunCommand(command []string) bool {
	return len(command) >= 3 && command[0] == "workflows" && command[1] == "run"
}

func isExecutableWorkflowRunCommand(command []string) bool {
	return isWorkflowRunCommand(command) && !isWorkflowRunHelpCommand(command)
}

func isWorkflowRunHelpCommand(command []string) bool {
	return isWorkflowRunCommand(command) && len(command) == 5 && command[3] == "--" && isSingleHelp(command[4:])
}

func validateSkillWorkflowEntrypoint(command []string) string {
	if isWorkflowRunCommand(command) {
		return ""
	}
	for _, workflow := range workflowCatalog() {
		if hasPrefixArgs(command, workflow.RunArgs) {
			return fmt.Sprintf("uses direct workflow command %q; use %q", strings.Join(workflow.RunArgs, " "), workflow.RunCommand)
		}
	}
	return ""
}

func checkWorkflows() ([]skillInfo, []workflowInfo, []workflowDoc, int, []skillIssue, error) {
	skills, issues, err := checkSkills()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	workflows := workflowCatalog()
	issues = append(issues, validateWorkflowCatalog(workflows)...)
	issues = append(issues, validateInternalCheckWorkflows(workflows)...)
	issues = append(issues, validateWorkflowDelegationCoverage(workflows)...)
	issues = append(issues, validateSkillWorkflowRequirementCatalog(workflows, skillWorkflowRequirementMap())...)
	issues = append(issues, validateUsageCoverage(workflows, usage)...)
	issues = append(issues, validateGroupUsageCoverage(workflows, groupUsageTexts())...)
	issues = append(issues, validateLegacyPassThroughUsage(usage)...)
	docs, docIssues, err := checkWorkflowDocs()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, docIssues...)
	issues = append(issues, validateWorkflowDocCoverage(workflows, docs)...)
	issues = append(issues, validateWorkflowDocExecutableDirectCommands(workflows, docs)...)
	issues = append(issues, validateWorkflowDocRequiredWorkflowRuns(workflows, docs)...)
	issues = append(issues, validateWorkflowDocExecutableWorkflowRuns(workflows, docs)...)
	issues = append(issues, validateWorkflowDocRunCommandOrder(workflows, docs)...)
	issues = append(issues, validateWorkflowDocRunCommandUniqueness(docs)...)
	issues = append(issues, validateWorkflowDocShowCoverage(workflows, docs)...)
	issues = append(issues, validateWorkflowDocShowCommandOrder(workflows, docs)...)
	issues = append(issues, validateWorkflowDocShowCommandUniqueness(docs)...)
	issues = append(issues, validateWorkflowDocListAndCheckCommandUniqueness(docs)...)
	issues = append(issues, validateProjectDocCheckCommandUniqueness(docs)...)
	issues = append(issues, validateSkillDocCoverage(skills, docs)...)
	issues = append(issues, validateSkillDocShowCommandOrder(skills, docs)...)
	issues = append(issues, validateSkillDocShowCommandUniqueness(docs)...)
	issues = append(issues, validateSkillDocListAndCheckCommandUniqueness(docs)...)
	buildIssues, err := checkCommandBuildTargets()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, buildIssues...)
	commandCoverageIssues, err := checkCommandEntrypointCoverage(workflows)
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, commandCoverageIssues...)
	agentPromptWrappersChecked, promptIssues, err := checkAgentPromptWrappers()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, promptIssues...)
	promptContentIssues, err := checkCodexPromptContents()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, promptContentIssues...)
	claudeContentIssues, err := checkClaudeCommandContents()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, claudeContentIssues...)
	claudeAgentIssues, err := checkClaudeReviewerAgents()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, claudeAgentIssues...)
	claudeRuleIssues, err := checkClaudeRuleDocs()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, claudeRuleIssues...)
	claudeSettingsIssues, err := checkClaudeSettings()
	if err != nil {
		return nil, nil, nil, 0, nil, err
	}
	issues = append(issues, claudeSettingsIssues...)
	return skills, workflows, docs, agentPromptWrappersChecked, issues, nil
}

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
	case "demo":
		if len(args) < 2 {
			return ""
		}
		switch args[1] {
		case "parse":
			return "zv-parser"
		case "players":
			return "zv-demo-players"
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
	case "pipeline":
		return "zv-pipeline"
	case "gallery", "skills", "workflows", "check":
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

func findWorkflow(name string) (workflowInfo, bool) {
	for _, workflow := range workflowCatalog() {
		if workflow.Name == name {
			return workflow, true
		}
	}
	return workflowInfo{}, false
}

func workflowCatalog() []workflowInfo {
	return withWorkflowRunCommands([]workflowInfo{
		{
			Name:        "demo-parse",
			Description: "Parse a CS2 demo into a kill or utility plan.",
			Command:     "zv demo parse --demo <demo.dem> --steamid <SteamID64> --out <plan.json>",
			RunArgs:     []string{"demo", "parse"},
		},
		{
			Name:        "demo-players",
			Description: "List demo participants and SteamID64 values.",
			Command:     "zv demo players --demo <demo.dem>",
			RunArgs:     []string{"demo", "players"},
		},
		{
			Name:        "utility-audit",
			Description: "Audit utility destinations/actions against the lineup catalog.",
			Command:     "zv utility audit --plan <plan-utility.json> --lineup-catalog data/lineups --out <utility-audit.csv>",
			RunArgs:     []string{"utility", "audit"},
		},
		{
			Name:        "record",
			Description: "Record planned demo segments with HLAE/CS2.",
			Command:     "zv record --killplan <plan.json> --demo <demo.dem> --out <recording-dir> --hlae <HLAE.exe> --cs2 <cs2.exe>",
			RunArgs:     []string{"record"},
		},
		{
			Name:        "compose-final",
			Description: "Concatenate recorded segment clips into a final MP4.",
			Command:     "zv compose final --recording-result <recording-result.json> --out <final.mp4>",
			RunArgs:     []string{"compose", "final"},
		},
		{
			Name:        "shorts-render",
			Description: "Render vertical Shorts from a recording result.",
			Command:     "zv shorts render --recording-result <recording-result.json> --out <shorts-dir>",
			RunArgs:     []string{"shorts", "render"},
		},
		{
			Name:        "analysis-tactical-data",
			Description: "Export sampled tactical data for replay experiments.",
			Command:     "zv analysis tactical-data --demo <demo.dem> --out <tactical.json> --start <tick> --end <tick>",
			RunArgs:     []string{"analysis", "tactical-data"},
		},
		{
			Name:        "analysis-viewer",
			Description: "Serve a local analysis review UI.",
			Command:     "zv analysis view --json <analysis.json>",
			RunArgs:     []string{"analysis", "view"},
		},
		{
			Name:        "gallery-open",
			Description: "Open a generated publish gallery for review.",
			Command:     "zv gallery open --path <shorts-dir>/publish/index.html",
			RunArgs:     []string{"gallery", "open"},
		},
		{
			Name:        "serve",
			Description: "Start the orchestrator API and workers.",
			Command:     "zv serve",
			RunArgs:     []string{"serve"},
		},
		{
			Name:        "pipeline",
			Description: "Run the local recorder-to-composer pipeline.",
			Command:     "zv pipeline --killplan <plan.json> --demo <demo.dem> --out <pipeline-dir> --hlae <HLAE.exe> --cs2 <cs2.exe>",
			RunArgs:     []string{"pipeline"},
		},
		{
			Name:        "skills-check",
			Description: "Validate repo-local Codex skills.",
			Command:     "zv skills check",
			RunArgs:     []string{"skills", "check"},
		},
		{
			Name:        "workflows-check",
			Description: "Validate skills, workflow catalog, and current workflow docs.",
			Command:     "zv workflows check",
			RunArgs:     []string{"workflows", "check"},
		},
		{
			Name:        "project-check",
			Description: "Run the full ZackVideo CLI, workflow, docs, and skills contract.",
			Command:     "zv check",
			RunArgs:     []string{"check"},
		},
	})
}

func withWorkflowRunCommands(workflows []workflowInfo) []workflowInfo {
	for i := range workflows {
		if workflows[i].Name != "" && workflows[i].RunCommand == "" {
			workflows[i].RunCommand = workflowRunCommand(workflows[i].Name)
		}
	}
	return workflows
}

func workflowRunCommand(name string) string {
	return "zv workflows run " + name
}

func validateWorkflowCatalog(workflows []workflowInfo) []skillIssue {
	seen := make(map[string]struct{}, len(workflows))
	seenRunArgs := make(map[string]string, len(workflows))
	var issues []skillIssue
	for i, workflow := range workflows {
		path := fmt.Sprintf("workflow:%d", i+1)
		if strings.TrimSpace(workflow.Name) != "" {
			path = "workflow:" + workflow.Name
		}
		if strings.TrimSpace(workflow.Name) == "" {
			issues = append(issues, skillIssue{Path: path, Message: "missing workflow name"})
		} else if !isWorkflowSlug(workflow.Name) {
			issues = append(issues, skillIssue{Path: path, Message: "workflow name must be a lowercase slug"})
		} else if _, ok := seen[workflow.Name]; ok {
			issues = append(issues, skillIssue{Path: path, Message: "duplicate workflow name"})
		} else {
			seen[workflow.Name] = struct{}{}
		}
		if len(workflow.RunArgs) > 0 {
			runArgsKey := strings.Join(workflow.RunArgs, " ")
			if firstWorkflow, ok := seenRunArgs[runArgsKey]; ok {
				issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("duplicate workflow run args %q also used by workflow %q", runArgsKey, firstWorkflow)})
			} else {
				seenRunArgs[runArgsKey] = workflow.Name
			}
		}
		if strings.TrimSpace(workflow.Description) == "" {
			issues = append(issues, skillIssue{Path: path, Message: "missing workflow description"})
		}
		if strings.TrimSpace(workflow.Name) != "" {
			wantRunCommand := workflowRunCommand(workflow.Name)
			if workflow.RunCommand != wantRunCommand {
				issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("workflow run command must be %q", wantRunCommand)})
			}
		}
		fields, ok := splitCommandFields(workflow.Command)
		if !ok {
			issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("could not parse workflow command: %s", workflow.Command)})
			continue
		}
		if len(fields) == 0 {
			issues = append(issues, skillIssue{Path: path, Message: "missing workflow command"})
			continue
		}
		if fields[0] != "zv" {
			issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("workflow command must start with zv: %s", workflow.Command)})
			continue
		}
		if issue := validateSkillCommand(fields[1:]); issue != "" {
			issues = append(issues, skillIssue{Path: path, Message: fmt.Sprintf("workflow command is not canonical: %s", issue)})
		}
		if issue := validateWorkflowRunArgs(workflow); issue != "" {
			issues = append(issues, skillIssue{Path: path, Message: issue})
		}
	}
	return issues
}

func validateInternalCheckWorkflows(workflows []workflowInfo) []skillIssue {
	expected := map[string]workflowInfo{
		"skills-check": {
			Command: "zv skills check",
			RunArgs: []string{"skills", "check"},
		},
		"workflows-check": {
			Command: "zv workflows check",
			RunArgs: []string{"workflows", "check"},
		},
		"project-check": {
			Command: "zv check",
			RunArgs: []string{"check"},
		},
	}
	seen := make(map[string]workflowInfo, len(workflows))
	for _, workflow := range workflows {
		seen[workflow.Name] = workflow
	}
	var issues []skillIssue
	for name, want := range expected {
		workflow, ok := seen[name]
		if !ok {
			issues = append(issues, skillIssue{Path: "workflow:" + name, Message: "missing internal check workflow"})
			continue
		}
		if workflow.Command != want.Command {
			issues = append(issues, skillIssue{Path: "workflow:" + name, Message: fmt.Sprintf("internal check workflow command must be %q", want.Command)})
		}
		if !equalArgs(workflow.RunArgs, want.RunArgs) {
			issues = append(issues, skillIssue{Path: "workflow:" + name, Message: fmt.Sprintf("internal check workflow run args must be %q", strings.Join(want.RunArgs, " "))})
		}
	}
	return issues
}

func validateWorkflowDelegationCoverage(workflows []workflowInfo) []skillIssue {
	var issues []skillIssue
	for _, workflow := range workflows {
		if len(workflow.RunArgs) == 0 {
			continue
		}
		if workflowDelegatedCommand(workflow.RunArgs) != "" {
			continue
		}
		path := "workflow:" + workflow.Name
		if strings.TrimSpace(workflow.Name) == "" {
			path = "workflow"
		}
		issues = append(issues, skillIssue{
			Path:    path,
			Message: fmt.Sprintf("workflow run args %q are not mapped to a delegated command", strings.Join(workflow.RunArgs, " ")),
		})
	}
	return issues
}

func validateSkillWorkflowRequirementSkills(skills []skillInfo, requirements map[string][]string) []skillIssue {
	installed := make(map[string]struct{}, len(skills))
	hasKnownRequiredSkill := false
	for _, skill := range skills {
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		installed[name] = struct{}{}
		if _, ok := requirements[name]; ok {
			hasKnownRequiredSkill = true
		}
	}
	if !hasKnownRequiredSkill {
		return nil
	}
	var issues []skillIssue
	for skillName := range installed {
		if !strings.HasPrefix(skillName, "zackvideo-") {
			continue
		}
		if _, ok := requirements[skillName]; ok {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    "skill:" + skillName,
			Message: "missing workflow requirements for repo skill",
		})
	}
	for skillName := range requirements {
		if _, ok := installed[skillName]; ok {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    "skill:" + skillName,
			Message: "workflow requirements reference missing repo skill",
		})
	}
	return issues
}

func validateSkillWorkflowRequirementCatalog(workflows []workflowInfo, requirements map[string][]string) []skillIssue {
	cataloged := make(map[string]struct{}, len(workflows))
	for _, workflow := range workflows {
		if workflow.Name == "" {
			continue
		}
		cataloged[workflow.Name] = struct{}{}
	}
	var issues []skillIssue
	for skillName, requiredWorkflows := range requirements {
		if !isWorkflowSlug(skillName) {
			issues = append(issues, skillIssue{Path: "skill:" + skillName, Message: "skill workflow requirement name must be a lowercase slug"})
		}
		for _, workflowName := range requiredWorkflows {
			if _, ok := cataloged[workflowName]; ok {
				continue
			}
			issues = append(issues, skillIssue{
				Path:    "skill:" + skillName,
				Message: fmt.Sprintf("required workflow %q is not cataloged", workflowName),
			})
		}
	}
	return issues
}

func validateUsageCoverage(workflows []workflowInfo, usageText string) []skillIssue {
	covered := usageCommandStems(usageText)
	var issues []skillIssue
	for _, workflow := range workflows {
		stem := workflowCommandStem(workflow.Command)
		if stem == "" {
			continue
		}
		if _, ok := covered[stem]; ok {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    "workflow:" + workflow.Name,
			Message: fmt.Sprintf("workflow command %q is not covered by main usage", stem),
		})
	}
	return issues
}

func validateGroupUsageCoverage(workflows []workflowInfo, groupUsages map[string]string) []skillIssue {
	var issues []skillIssue
	for _, workflow := range workflows {
		stem := workflowCommandStem(workflow.Command)
		if stem == "" {
			continue
		}
		fields, ok := splitCommandFields(stem)
		if !ok || len(fields) < 2 {
			continue
		}
		usageText, ok := groupUsages[fields[1]]
		if !ok {
			continue
		}
		if _, ok := usageCommandStems(usageText)[stem]; ok {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    "workflow:" + workflow.Name,
			Message: fmt.Sprintf("workflow command %q is not covered by %s usage", stem, fields[1]),
		})
	}
	return issues
}

func validateLegacyPassThroughUsage(usageText string) []skillIssue {
	var issues []skillIssue
	for _, passThrough := range legacyPassThroughs() {
		line := legacyPassThroughUsageLine(passThrough)
		if strings.Contains(usageText, line) {
			continue
		}
		issues = append(issues, skillIssue{
			Path:    "usage",
			Message: fmt.Sprintf("legacy pass-through %q is not covered by main usage", line),
		})
	}
	return issues
}

func groupUsageTexts() map[string]string {
	return map[string]string{
		"demo":      demoUsage,
		"utility":   utilityUsage,
		"compose":   composeUsage,
		"shorts":    shortsUsage,
		"analysis":  analysisUsage,
		"gallery":   galleryUsage,
		"check":     checkUsage,
		"skills":    skillsUsage,
		"workflows": workflowsUsage,
	}
}

func workflowCommandStem(command string) string {
	fields, ok := splitCommandFields(command)
	if !ok || len(fields) == 0 || fields[0] != "zv" {
		return ""
	}
	stem := []string{"zv"}
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "--") || strings.HasPrefix(field, "<") {
			break
		}
		stem = append(stem, field)
	}
	return strings.Join(stem, " ")
}

func usageCommandStems(usageText string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, line := range strings.Split(usageText, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "usage:"); ok {
			line = strings.TrimSpace(after)
		}
		for _, part := range strings.Split(line, "|") {
			if stem := usageLineCommandStem(part); stem != "" {
				out[stem] = struct{}{}
			}
		}
	}
	return out
}

func usageLineCommandStem(line string) string {
	fields, ok := splitCommandFields(strings.TrimSpace(line))
	if !ok || len(fields) == 0 || fields[0] != "zv" {
		return ""
	}
	var usageStem []string
	for _, field := range fields {
		if strings.HasPrefix(field, "--") || strings.HasPrefix(field, "<") || strings.HasPrefix(field, "[") {
			break
		}
		usageStem = append(usageStem, field)
	}
	return strings.Join(usageStem, " ")
}

func isWorkflowSlug(name string) bool {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return false
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		if r == '-' {
			continue
		}
		return false
	}
	return true
}

func validateWorkflowRunArgs(workflow workflowInfo) string {
	args := workflow.RunArgs
	if len(args) == 0 {
		return "missing workflow run args"
	}
	command := strings.Join(args, " ")
	fields, ok := workflowCommandRunArgs(workflow.Command)
	if !ok {
		return fmt.Sprintf("could not parse workflow command: %s", workflow.Command)
	}
	if !equalArgs(fields, args) {
		return fmt.Sprintf("workflow run args %q do not match workflow command %q", command, workflow.Command)
	}
	return ""
}

func workflowCommandRunArgs(command string) ([]string, bool) {
	fields, ok := splitCommandFields(command)
	if !ok {
		return nil, false
	}
	if len(fields) == 0 || fields[0] != "zv" {
		return nil, true
	}
	var args []string
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "--") || strings.HasPrefix(field, "<") {
			break
		}
		args = append(args, field)
	}
	return args, true
}

func equalArgs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func hasPrefixArgs(fields, prefix []string) bool {
	if len(fields) < len(prefix) {
		return false
	}
	for i, want := range prefix {
		if fields[i] != want {
			return false
		}
	}
	return true
}

func checkWorkflowDocs() ([]workflowDoc, []skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, nil, err
	}
	docs := workflowDocs()
	var issues []skillIssue
	for i, doc := range docs {
		path := filepath.Join(root, filepath.FromSlash(doc.Path))
		// #nosec G304 -- workflow docs are fixed repo-local paths.
		b, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				issues = append(issues, skillIssue{Path: doc.Path, Message: "missing workflow doc"})
				continue
			}
			return nil, nil, fmt.Errorf("read workflow doc %s: %w", doc.Path, err)
		}
		body := string(b)
		docs[i].Body = body
		for _, legacy := range legacyWorkflowCommands() {
			if strings.Contains(body, legacy) {
				issues = append(issues, skillIssue{Path: doc.Path, Message: fmt.Sprintf("documents legacy direct command %s", legacy)})
			}
		}
		for _, required := range doc.Required {
			if !strings.Contains(body, required) {
				issues = append(issues, skillIssue{Path: doc.Path, Message: fmt.Sprintf("missing canonical workflow command %s", required)})
			}
		}
		for _, line := range skillCommandLines(body) {
			command, ok := skillCommand(line)
			if !ok {
				issues = append(issues, skillIssue{Path: doc.Path, Message: fmt.Sprintf("could not parse zv command line %q", line)})
				continue
			}
			if issue := validateSkillCommand(command); issue != "" {
				issues = append(issues, skillIssue{Path: doc.Path, Message: fmt.Sprintf("%s in %q", issue, line)})
			}
		}
	}
	return docs, issues, nil
}

func workflowDocs() []workflowDoc {
	return []workflowDoc{
		{
			Path:              "README.md",
			RequiredSkills:    true,
			RequiredWorkflows: true,
			Required: []string{
				"./bin/zv demo parse",
				"./bin/zv demo players",
				"./bin/zv utility audit",
				"./bin/zv record",
				"./bin/zv compose final",
				"./bin/zv shorts render",
				"./bin/zv analysis tactical-data",
				"./bin/zv analysis view",
				"./bin/zv gallery open",
				"./bin/zv check",
				"./bin/zv check --format json",
				"./bin/zv serve",
				"./bin/zv pipeline",
				"./bin/zv skills check",
				"./bin/zv skills list --format json",
				"./bin/zv skills show",
				"./bin/zv skills check --format json",
				"./bin/zv workflows list",
				"./bin/zv workflows list --format json",
				"./bin/zv workflows show",
				"./bin/zv workflows show demo-parse --format json",
				"./bin/zv workflows run demo-parse",
				"./bin/zv workflows run demo-players",
				"./bin/zv workflows run utility-audit",
				"./bin/zv workflows run record",
				"./bin/zv workflows run compose-final",
				"./bin/zv workflows run shorts-render",
				"./bin/zv workflows run analysis-tactical-data",
				"./bin/zv workflows run analysis-viewer",
				"./bin/zv workflows run gallery-open",
				"./bin/zv workflows run serve",
				"./bin/zv workflows run pipeline",
				"./bin/zv workflows run skills-check",
				"./bin/zv workflows run workflows-check",
				"./bin/zv workflows run project-check",
				"./bin/zv workflows check",
				"./bin/zv workflows check --format json",
			},
		},
		{
			Path: "docs/toolchain.md",
			Required: []string{
				`zv check`,
				`.\bin\zv.exe record`,
			},
		},
		{
			Path: "docs/README.md",
			Required: []string{
				"./bin/zv check",
				"./bin/zv skills list",
				"./bin/zv workflows list",
				"./bin/zv workflows run demo-parse",
			},
		},
		{
			Path: "scripts/smoke-real.ps1",
			Required: []string{
				`bin\zv serve`,
			},
		},
		{
			Path: "scripts/smoke.sh",
			Required: []string{
				"ZV_BASE_URL",
				"/api/jobs",
				"/api/jobs/$ID",
				"/api/jobs/$ID/plan",
			},
		},
		{
			Path: "Makefile",
			Required: []string{
				"go build -o bin/zv ./cmd/zv",
				"go run ./cmd/zv check",
				"go run ./cmd/zv workflows check",
			},
		},
		{
			Path: "scripts/build.ps1",
			Required: []string{
				`"zv"`,
				"& go build -o $out $pkg",
			},
		},
		{
			Path: "scripts/go-gate.sh",
			Required: []string{
				"== zv check ==",
				"go run ./cmd/zv check",
			},
		},
		{
			Path: "scripts/fix-loop.ps1",
			Required: []string{
				`Invoke-Step "zv check"`,
				"go run ./cmd/zv check",
			},
		},
		{
			Path: "scripts/check-codex-harness.sh",
			Required: []string{
				"== ZackVideo workflow contract ==",
				"go run ./cmd/zv check",
			},
		},
		{
			Path:              ".codex/README.md",
			RequiredSkills:    true,
			RequiredWorkflows: true,
			Required: []string{
				"./bin/zv skills list",
				"./bin/zv skills show",
				"./bin/zv skills check",
				"./bin/zv check",
				"./bin/zv check --format json",
				"./bin/zv skills list --format json",
				"./bin/zv skills show",
				"./bin/zv skills check --format json",
				"./bin/zv workflows list",
				"./bin/zv workflows list --format json",
				"./bin/zv workflows show",
				"./bin/zv workflows show demo-parse --format json",
				"./bin/zv workflows run demo-parse",
				"./bin/zv workflows run demo-players",
				"./bin/zv workflows run utility-audit",
				"./bin/zv workflows run record",
				"./bin/zv workflows run compose-final",
				"./bin/zv workflows run shorts-render",
				"./bin/zv workflows run analysis-tactical-data",
				"./bin/zv workflows run analysis-viewer",
				"./bin/zv workflows run gallery-open",
				"./bin/zv workflows run serve",
				"./bin/zv workflows run pipeline",
				"./bin/zv workflows run skills-check",
				"./bin/zv workflows run workflows-check",
				"./bin/zv workflows run project-check",
				"./bin/zv workflows check",
				"./bin/zv workflows check --format json",
			},
		},
		{
			Path: "AGENTS.md",
			Required: []string{
				"scripts/codex-run.sh",
				"scripts/codex-go-tdd.sh",
				"scripts/codex-go-bugfix.sh",
				"scripts/codex-go-pr-ready.sh",
				"CODEX_DRY_RUN=1",
				`C:\HLAE-2.190.1\HLAE.exe`,
				`C:\HLAE\HLAE.exe`,
				"scripts/go-gate.sh --no-format",
				"scripts/go-gate.sh --race",
				"scripts/go-gate.sh --security",
			},
		},
		{
			Path: "CLAUDE.md",
			Required: []string{
				"scripts/claude-run.sh",
				"scripts/claude-zv-tdd.sh",
				"scripts/claude-zv-bugfix.sh",
				"scripts/claude-zv-pr-ready.sh",
				"CLAUDE_DRY_RUN=1",
				`C:\HLAE-2.190.1\HLAE.exe`,
				`C:\HLAE\HLAE.exe`,
				"scripts/go-gate.sh --no-format",
				"scripts/go-gate.sh --race",
				"scripts/go-gate.sh --security",
			},
		},
	}
}

func validateWorkflowDocCoverage(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	documented := make(map[string]struct{})
	for _, doc := range docs {
		for _, required := range doc.Required {
			documented[required] = struct{}{}
		}
	}
	var issues []skillIssue
	for _, workflow := range workflows {
		for _, coverage := range []struct {
			name    string
			command string
		}{
			{name: "workflow command", command: workflow.Command},
			{name: "workflow run command", command: workflow.RunCommand},
		} {
			required := documentedWorkflowCommand(coverage.command)
			if required == "" {
				continue
			}
			if _, ok := documented[required]; ok {
				continue
			}
			issues = append(issues, skillIssue{
				Path:    "workflow:" + workflow.Name,
				Message: fmt.Sprintf("%s %s is not covered by workflow docs", coverage.name, required),
			})
		}
	}
	return issues
}

func validateWorkflowDocExecutableDirectCommands(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		documented := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok {
				continue
			}
			for _, workflow := range workflows {
				if !isExecutableDirectWorkflowCommand(command, workflow) {
					continue
				}
				documented[workflow.Name] = struct{}{}
				break
			}
		}
		for _, workflow := range workflows {
			if strings.TrimSpace(workflow.Name) == "" || documentedWorkflowCommand(workflow.Command) == "" {
				continue
			}
			if _, ok := documented[workflow.Name]; ok {
				continue
			}
			issues = append(issues, skillIssue{
				Path:    doc.Path,
				Message: fmt.Sprintf("missing executable workflow command %s", workflow.Name),
			})
		}
	}
	return issues
}

func isExecutableDirectWorkflowCommand(command []string, workflow workflowInfo) bool {
	if !hasPrefixArgs(command, workflow.RunArgs) {
		return false
	}
	if isSingleHelp(command[len(workflow.RunArgs):]) {
		return false
	}
	return validateSkillCommand(command) == ""
}

func validateWorkflowDocRequiredWorkflowRuns(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	cataloged := make(map[string]struct{})
	for _, workflow := range workflows {
		cataloged[workflow.Name] = struct{}{}
	}

	var issues []skillIssue
	for _, doc := range docs {
		for _, required := range doc.Required {
			name, ok := requiredWorkflowRunName(required)
			if !ok {
				continue
			}
			if _, ok := cataloged[name]; ok {
				continue
			}
			issues = append(issues, skillIssue{
				Path:    doc.Path,
				Message: fmt.Sprintf("required workflow run %q is not cataloged", name),
			})
		}
	}
	return issues
}

func validateWorkflowDocExecutableWorkflowRuns(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		documented := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			documented[command[2]] = struct{}{}
		}
		for _, workflow := range workflows {
			name := strings.TrimSpace(workflow.Name)
			if name == "" {
				continue
			}
			if _, ok := documented[name]; ok {
				continue
			}
			issues = append(issues, skillIssue{
				Path:    doc.Path,
				Message: fmt.Sprintf("missing executable workflow run %s", name),
			})
		}
	}
	return issues
}

func validateWorkflowDocRunCommandOrder(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	order := make(map[string]int, len(workflows))
	names := make([]string, 0, len(workflows))
	for i, workflow := range workflows {
		if workflow.Name == "" {
			continue
		}
		order[workflow.Name] = i
		names = append(names, workflow.Name)
	}

	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		last := -1
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			workflowName := command[2]
			if _, ok := seen[workflowName]; ok {
				continue
			}
			seen[workflowName] = struct{}{}
			pos, ok := order[workflowName]
			if !ok {
				continue
			}
			if pos < last {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("workflow run commands must appear in catalog order: %s", strings.Join(names, ", ")),
				})
				break
			}
			last = pos
		}
	}
	return issues
}

func validateWorkflowDocRunCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			workflowName := command[2]
			if workflowDocWorkflowRunMayRepeat(workflowName) {
				continue
			}
			if _, ok := seen[workflowName]; ok {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("duplicate workflow run %s", workflowName),
				})
				continue
			}
			seen[workflowName] = struct{}{}
		}
	}
	return issues
}

func workflowDocWorkflowRunMayRepeat(name string) bool {
	switch name {
	case "skills-check", "workflows-check", "project-check":
		return true
	default:
		return false
	}
}

func validateWorkflowDocShowCoverage(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		for _, workflow := range workflows {
			name := strings.TrimSpace(workflow.Name)
			if name == "" {
				continue
			}
			showCommand := "./bin/zv workflows show " + name
			if !docHasWorkflowShowCommand(doc.Body, name, "text") {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("missing workflow show command %s", showCommand),
				})
			}
			showJSONCommand := showCommand + " --format json"
			if !docHasWorkflowShowCommand(doc.Body, name, "json") {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("missing workflow show command %s", showJSONCommand),
				})
			}
		}
	}
	return issues
}

func validateWorkflowDocShowCommandOrder(workflows []workflowInfo, docs []workflowDoc) []skillIssue {
	order := make(map[string]int, len(workflows))
	names := make([]string, 0, len(workflows))
	for i, workflow := range workflows {
		if workflow.Name == "" {
			continue
		}
		order[workflow.Name] = i
		names = append(names, workflow.Name)
	}

	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		last := -1
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "workflows" || command[1] != "show" {
				continue
			}
			workflowName := command[2]
			if _, ok := seen[workflowName]; ok {
				continue
			}
			seen[workflowName] = struct{}{}
			pos, ok := order[workflowName]
			if !ok {
				continue
			}
			if pos < last {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("workflow show commands must appear in catalog order: %s", strings.Join(names, ", ")),
				})
				break
			}
			last = pos
		}
	}
	return issues
}

func validateWorkflowDocShowCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "workflows" || command[1] != "show" {
				continue
			}
			format, rest, err := parseFormatArgs(command[3:])
			if err != nil || len(rest) != 0 {
				continue
			}
			key := command[2] + "\x00" + format
			if _, ok := seen[key]; ok {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("duplicate workflow show %s --format %s", command[2], format),
				})
				continue
			}
			seen[key] = struct{}{}
		}
	}
	return issues
}

func validateWorkflowDocListAndCheckCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredWorkflows || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "workflows" {
				continue
			}
			if command[1] != "list" && command[1] != "check" {
				continue
			}
			format, rest, err := parseFormatArgs(command[2:])
			if err != nil || len(rest) != 0 {
				continue
			}
			key := command[1] + "\x00" + format
			if _, ok := seen[key]; ok {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("duplicate workflows %s --format %s", command[1], format),
				})
				continue
			}
			seen[key] = struct{}{}
		}
	}
	return issues
}

func validateProjectDocCheckCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if (!doc.RequiredWorkflows && !doc.RequiredSkills) || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) == 0 || command[0] != "check" {
				continue
			}
			format, rest, err := parseFormatArgs(command[1:])
			if err != nil || len(rest) != 0 {
				continue
			}
			if _, ok := seen[format]; ok {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("duplicate check --format %s", format),
				})
				continue
			}
			seen[format] = struct{}{}
		}
	}
	return issues
}

func requiredWorkflowRunName(command string) (string, bool) {
	fields, ok := splitCommandFields(command)
	if !ok || len(fields) < 4 {
		return "", false
	}
	if fields[1] != "workflows" || fields[2] != "run" {
		return "", false
	}
	switch fields[0] {
	case "zv", "./bin/zv", `.\bin\zv.exe`:
		return fields[3], true
	default:
		return "", false
	}
}

func validateSkillDocCoverage(skills []skillInfo, docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredSkills || doc.Body == "" {
			continue
		}
		for _, skill := range skills {
			name := strings.TrimSpace(skill.Name)
			if name == "" {
				continue
			}
			if strings.Contains(doc.Body, name) {
				showCommand := "./bin/zv skills show " + name
				if !docHasSkillShowCommand(doc.Body, name, "text") {
					issues = append(issues, skillIssue{
						Path:    doc.Path,
						Message: fmt.Sprintf("missing skill show command %s", showCommand),
					})
				}
				showJSONCommand := showCommand + " --format json"
				if !docHasSkillShowCommand(doc.Body, name, "json") {
					issues = append(issues, skillIssue{
						Path:    doc.Path,
						Message: fmt.Sprintf("missing skill show command %s", showJSONCommand),
					})
				}
				continue
			}
			issues = append(issues, skillIssue{
				Path:    doc.Path,
				Message: fmt.Sprintf("missing repo skill %s", name),
			})
		}
	}
	return issues
}

func validateSkillDocShowCommandOrder(skills []skillInfo, docs []workflowDoc) []skillIssue {
	order := make(map[string]int, len(skills))
	names := make([]string, 0, len(skills))
	for i, skill := range skills {
		name := strings.TrimSpace(skill.Name)
		if name == "" {
			continue
		}
		order[name] = i
		names = append(names, name)
	}

	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredSkills || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		last := -1
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "skills" || command[1] != "show" {
				continue
			}
			skillName := command[2]
			if _, ok := seen[skillName]; ok {
				continue
			}
			seen[skillName] = struct{}{}
			pos, ok := order[skillName]
			if !ok {
				continue
			}
			if pos < last {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("skill show commands must appear in skill order: %s", strings.Join(names, ", ")),
				})
				break
			}
			last = pos
		}
	}
	return issues
}

func validateSkillDocShowCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredSkills || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "skills" || command[1] != "show" {
				continue
			}
			format, rest, err := parseFormatArgs(command[3:])
			if err != nil || len(rest) != 0 {
				continue
			}
			key := command[2] + "\x00" + format
			if _, ok := seen[key]; ok {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("duplicate skill show %s --format %s", command[2], format),
				})
				continue
			}
			seen[key] = struct{}{}
		}
	}
	return issues
}

func validateSkillDocListAndCheckCommandUniqueness(docs []workflowDoc) []skillIssue {
	var issues []skillIssue
	for _, doc := range docs {
		if !doc.RequiredSkills || doc.Body == "" {
			continue
		}
		seen := make(map[string]struct{})
		for _, line := range skillCommandLines(doc.Body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "skills" {
				continue
			}
			if command[1] != "list" && command[1] != "check" {
				continue
			}
			format, rest, err := parseFormatArgs(command[2:])
			if err != nil || len(rest) != 0 {
				continue
			}
			key := command[1] + "\x00" + format
			if _, ok := seen[key]; ok {
				issues = append(issues, skillIssue{
					Path:    doc.Path,
					Message: fmt.Sprintf("duplicate skills %s --format %s", command[1], format),
				})
				continue
			}
			seen[key] = struct{}{}
		}
	}
	return issues
}

func docHasSkillShowCommand(body, name, wantFormat string) bool {
	for _, line := range skillCommandLines(body) {
		command, ok := skillCommand(line)
		if !ok || len(command) < 3 || command[0] != "skills" || command[1] != "show" || command[2] != name {
			continue
		}
		format, rest, err := parseFormatArgs(command[3:])
		if err != nil || len(rest) != 0 {
			continue
		}
		if format == wantFormat {
			return true
		}
	}
	return false
}

func docHasWorkflowShowCommand(body, name, wantFormat string) bool {
	for _, line := range skillCommandLines(body) {
		command, ok := skillCommand(line)
		if !ok || len(command) < 3 || command[0] != "workflows" || command[1] != "show" || command[2] != name {
			continue
		}
		format, rest, err := parseFormatArgs(command[3:])
		if err != nil || len(rest) != 0 {
			continue
		}
		if format == wantFormat {
			return true
		}
	}
	return false
}

func checkAgentPromptWrappers() (int, []skillIssue, error) {
	codexChecked, codexIssues, err := checkCodexPromptWrappers()
	if err != nil {
		return 0, nil, err
	}
	claudeChecked, claudeIssues, err := checkClaudePromptWrappers()
	if err != nil {
		return 0, nil, err
	}
	issues := append(codexIssues, claudeIssues...)
	return codexChecked + claudeChecked, issues, nil
}

func checkCodexPromptWrappers() (int, []skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return 0, nil, err
	}
	promptsDir := filepath.Join(root, ".codex", "prompts")
	entries, err := os.ReadDir(promptsDir)
	if err != nil {
		return 0, nil, fmt.Errorf("read codex prompts: %w", err)
	}
	prompts := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		prompts[filepath.ToSlash(filepath.Join(".codex", "prompts", entry.Name()))] = false
	}

	readmePath := filepath.Join(root, ".codex", "README.md")
	b, err := os.ReadFile(readmePath)
	if err != nil {
		return 0, nil, fmt.Errorf("read .codex/README.md: %w", err)
	}
	readmeBody := string(b)
	var issues []skillIssue
	runnerPath := filepath.Join(root, "scripts", "codex-run.sh")
	relRunner := filepath.ToSlash(mustRel(root, runnerPath))
	if b, err := os.ReadFile(runnerPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			issues = append(issues, skillIssue{Path: relRunner, Message: "missing codex prompt runner"})
		} else {
			return 0, nil, fmt.Errorf("read %s: %w", relRunner, err)
		}
	} else {
		issues = append(issues, validateAgentShellScript(relRunner, string(b))...)
		if !strings.Contains(readmeBody, relRunner) {
			issues = append(issues, skillIssue{Path: ".codex/README.md", Message: fmt.Sprintf("does not document runner %s", relRunner)})
		}
	}

	wrappers, err := filepath.Glob(filepath.Join(root, "scripts", "codex*.sh"))
	if err != nil {
		return 0, nil, fmt.Errorf("glob codex wrappers: %w", err)
	}
	var checked int
	for _, wrapper := range wrappers {
		if filepath.Base(wrapper) == "codex-run.sh" {
			continue
		}
		relWrapper := filepath.ToSlash(mustRel(root, wrapper))
		b, err := os.ReadFile(wrapper)
		if err != nil {
			return 0, nil, fmt.Errorf("read %s: %w", relWrapper, err)
		}
		body := string(b)
		issues = append(issues, validateAgentShellScript(relWrapper, body)...)
		prompt, ok := codexWrapperPromptPath(body)
		if !ok {
			issues = append(issues, skillIssue{Path: relWrapper, Message: "does not exec scripts/codex-run.sh with a prompt"})
			continue
		}
		if _, ok := prompts[prompt]; !ok {
			issues = append(issues, skillIssue{Path: relWrapper, Message: fmt.Sprintf("references missing prompt %s", prompt)})
			continue
		}
		prompts[prompt] = true
		checked++
		if !strings.Contains(readmeBody, relWrapper) {
			issues = append(issues, skillIssue{Path: ".codex/README.md", Message: fmt.Sprintf("does not document wrapper %s", relWrapper)})
		}
	}
	if checked == 0 {
		issues = append(issues, skillIssue{Path: "scripts", Message: "no codex prompt wrappers found"})
	}
	for prompt, used := range prompts {
		if !used {
			issues = append(issues, skillIssue{Path: prompt, Message: "has no codex wrapper"})
		}
	}
	return checked, issues, nil
}

func checkClaudePromptWrappers() (int, []skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return 0, nil, err
	}
	commandsDir := filepath.Join(root, ".claude", "commands")
	entries, err := os.ReadDir(commandsDir)
	if err != nil {
		return 0, nil, fmt.Errorf("read claude commands: %w", err)
	}
	commands := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		commands[filepath.ToSlash(filepath.Join(".claude", "commands", entry.Name()))] = false
	}

	readmePath := filepath.Join(root, ".claude", "README.md")
	b, err := os.ReadFile(readmePath)
	if err != nil {
		return 0, nil, fmt.Errorf("read .claude/README.md: %w", err)
	}
	readmeBody := string(b)
	var issues []skillIssue
	runnerPath := filepath.Join(root, "scripts", "claude-run.sh")
	relRunner := filepath.ToSlash(mustRel(root, runnerPath))
	if b, err := os.ReadFile(runnerPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			issues = append(issues, skillIssue{Path: relRunner, Message: "missing claude prompt runner"})
		} else {
			return 0, nil, fmt.Errorf("read %s: %w", relRunner, err)
		}
	} else {
		issues = append(issues, validateAgentShellScript(relRunner, string(b))...)
		if !strings.Contains(readmeBody, relRunner) {
			issues = append(issues, skillIssue{Path: ".claude/README.md", Message: fmt.Sprintf("does not document runner %s", relRunner)})
		}
	}

	wrappers, err := filepath.Glob(filepath.Join(root, "scripts", "claude-zv-*.sh"))
	if err != nil {
		return 0, nil, fmt.Errorf("glob claude wrappers: %w", err)
	}
	var checked int
	for _, wrapper := range wrappers {
		relWrapper := filepath.ToSlash(mustRel(root, wrapper))
		b, err := os.ReadFile(wrapper)
		if err != nil {
			return 0, nil, fmt.Errorf("read %s: %w", relWrapper, err)
		}
		body := string(b)
		issues = append(issues, validateAgentShellScript(relWrapper, body)...)
		command, ok := claudeWrapperCommandPath(body)
		if !ok {
			issues = append(issues, skillIssue{Path: relWrapper, Message: "does not exec scripts/claude-run.sh with a command prompt"})
			continue
		}
		if _, ok := commands[command]; !ok {
			issues = append(issues, skillIssue{Path: relWrapper, Message: fmt.Sprintf("references missing claude command %s", command)})
			continue
		}
		commands[command] = true
		checked++
		if !strings.Contains(readmeBody, relWrapper) {
			issues = append(issues, skillIssue{Path: ".claude/README.md", Message: fmt.Sprintf("does not document wrapper %s", relWrapper)})
		}
	}
	if checked == 0 {
		issues = append(issues, skillIssue{Path: "scripts", Message: "no claude prompt wrappers found"})
	}
	for command, used := range commands {
		if !used {
			issues = append(issues, skillIssue{Path: command, Message: "has no claude wrapper"})
		}
	}
	return checked, issues, nil
}

func validateAgentShellScript(path, body string) []skillIssue {
	var issues []skillIssue
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "#!/usr/bin/env bash" {
		issues = append(issues, skillIssue{Path: path, Message: "missing standard bash shebang"})
	}
	if !strings.Contains(body, "set -euo pipefail") {
		issues = append(issues, skillIssue{Path: path, Message: "missing strict shell mode set -euo pipefail"})
	}
	return issues
}

func codexWrapperPromptPath(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "scripts/codex-run.sh") {
			continue
		}
		for _, field := range strings.Fields(line) {
			field = strings.Trim(field, `"'`)
			if strings.HasPrefix(field, ".codex/prompts/") && strings.HasSuffix(field, ".md") {
				return filepath.ToSlash(field), true
			}
		}
	}
	return "", false
}

func claudeWrapperCommandPath(body string) (string, bool) {
	for _, line := range strings.Split(body, "\n") {
		if !strings.Contains(line, "scripts/claude-run.sh") {
			continue
		}
		for _, field := range strings.Fields(line) {
			field = strings.Trim(field, `"'`)
			if strings.HasPrefix(field, ".claude/commands/") && strings.HasSuffix(field, ".md") {
				return filepath.ToSlash(field), true
			}
		}
	}
	return "", false
}

type codexPromptContentRule struct {
	Path      string
	Required  []string
	Forbidden []string
}

func checkCodexPromptContents() ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	var issues []skillIssue
	for _, rule := range codexPromptContentRules() {
		path := filepath.Join(root, filepath.FromSlash(rule.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: "missing codex prompt"})
				continue
			}
			return nil, fmt.Errorf("read %s: %w", rule.Path, err)
		}
		body := string(b)
		for _, required := range rule.Required {
			if !strings.Contains(body, required) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: fmt.Sprintf("missing standard gate guidance %q", required)})
			}
		}
		for _, forbidden := range rule.Forbidden {
			if strings.Contains(body, forbidden) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: fmt.Sprintf("uses partial check %q; use scripts/go-gate.sh", forbidden)})
			}
		}
	}
	return issues, nil
}

func checkClaudeCommandContents() ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	var issues []skillIssue
	for _, rule := range claudeCommandContentRules() {
		path := filepath.Join(root, filepath.FromSlash(rule.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: "missing claude command"})
				continue
			}
			return nil, fmt.Errorf("read %s: %w", rule.Path, err)
		}
		body := string(b)
		for _, required := range rule.Required {
			if !strings.Contains(body, required) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: fmt.Sprintf("missing standard gate guidance %q", required)})
			}
		}
		for _, forbidden := range rule.Forbidden {
			if strings.Contains(body, forbidden) {
				issues = append(issues, skillIssue{Path: rule.Path, Message: fmt.Sprintf("uses partial check %q; use scripts/go-gate.sh", forbidden)})
			}
		}
	}
	return issues, nil
}

func checkClaudeReviewerAgents() ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	agentsDir := filepath.Join(root, ".claude", "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read claude agents: %w", err)
	}
	readmeBody, err := readWorkflowDocBody(root, ".claude/README.md")
	if err != nil {
		return nil, err
	}
	claudeBody, err := readWorkflowDocBody(root, "CLAUDE.md")
	if err != nil {
		return nil, err
	}
	var issues []skillIssue
	var checked int
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		checked++
		relPath := filepath.ToSlash(filepath.Join(".claude", "agents", entry.Name()))
		path := filepath.Join(agentsDir, entry.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", relPath, err)
		}
		body := string(b)
		name, ok := markdownFrontMatterValue(body, "name")
		wantName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if !ok {
			issues = append(issues, skillIssue{Path: relPath, Message: "missing agent front matter name"})
		} else if name != wantName {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("agent name %q does not match file name %q", name, wantName)})
		}
		for _, doc := range []struct {
			path string
			body string
		}{
			{path: ".claude/README.md", body: readmeBody},
			{path: "CLAUDE.md", body: claudeBody},
		} {
			if !strings.Contains(doc.body, "@"+wantName) {
				issues = append(issues, skillIssue{Path: doc.path, Message: fmt.Sprintf("does not document reviewer agent @%s", wantName)})
			}
		}
		for _, required := range claudeReviewerAgentRequiredText(wantName) {
			if !strings.Contains(body, required) {
				issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("missing reviewer guidance %q", required)})
			}
		}
	}
	if checked == 0 {
		issues = append(issues, skillIssue{Path: ".claude/agents", Message: "no claude reviewer agents found"})
	}
	return issues, nil
}

func readWorkflowDocBody(root, relPath string) (string, error) {
	path := filepath.Join(root, filepath.FromSlash(relPath))
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", relPath, err)
	}
	return string(b), nil
}

func markdownFrontMatterValue(body, key string) (string, bool) {
	scanner := bufio.NewScanner(strings.NewReader(body))
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return "", false
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			break
		}
		k, value, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		return trimMetadataValue(value), true
	}
	return "", false
}

func claudeReviewerAgentRequiredText(name string) []string {
	required := []string{
		"BLOCKER",
		"WARNING",
		"NIT",
		"Every finding",
		"file/path",
		"why",
		"practical fix",
		"No blocking",
		"issues found.",
	}
	switch name {
	case "go-concurrency-reviewer":
		required = append(required, "scripts/go-gate.sh --race")
	case "go-security-reviewer":
		required = append(required, "Do not read `.env`")
	case "zv-media-pipeline-reviewer":
		required = append(required, "HLAE/CS2/large media")
	}
	return required
}

func checkClaudeRuleDocs() ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	rulesDir := filepath.Join(root, ".claude", "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return nil, fmt.Errorf("read claude rules: %w", err)
	}
	readmeBody, err := readWorkflowDocBody(root, ".claude/README.md")
	if err != nil {
		return nil, err
	}
	claudeBody, err := readWorkflowDocBody(root, "CLAUDE.md")
	if err != nil {
		return nil, err
	}
	var issues []skillIssue
	var checked int
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		checked++
		relPath := filepath.ToSlash(filepath.Join(".claude", "rules", entry.Name()))
		path := filepath.Join(rulesDir, entry.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", relPath, err)
		}
		body := string(b)
		for _, doc := range []struct {
			path string
			body string
		}{
			{path: ".claude/README.md", body: readmeBody},
			{path: "CLAUDE.md", body: claudeBody},
		} {
			if !strings.Contains(doc.body, relPath) {
				issues = append(issues, skillIssue{Path: doc.path, Message: fmt.Sprintf("does not document claude rule %s", relPath)})
			}
		}
		for _, required := range claudeRuleRequiredText(relPath) {
			if !strings.Contains(body, required) {
				issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("missing claude rule guidance %q", required)})
			}
		}
	}
	if checked == 0 {
		issues = append(issues, skillIssue{Path: ".claude/rules", Message: "no claude rule docs found"})
	}
	return issues, nil
}

func claudeRuleRequiredText(path string) []string {
	switch path {
	case ".claude/rules/go-style.md":
		return []string{
			"clarity, simplicity, concision, maintainability",
			"Avoid `util`, `common`, `helper`, `manager`",
			"Return errors with context",
			"Respect context cancellation",
			"Every goroutine needs an owner",
		}
	case ".claude/rules/zackvideo-operations.md":
		return []string{
			"scripts/go-gate.sh --no-format",
			"HLAE/CS2 launch or real capture",
			"Docker compose and database migrations",
			"cleanup scripts that delete artifacts",
			"Never add generated `.mp4`",
		}
	default:
		return []string{
			"ZackVideo",
		}
	}
}

type claudeSettingsFile struct {
	Permissions struct {
		Allow []string `json:"allow"`
		Ask   []string `json:"ask"`
		Deny  []string `json:"deny"`
	} `json:"permissions"`
}

func checkClaudeSettings() ([]skillIssue, error) {
	root, err := findWorkflowRoot()
	if err != nil {
		return nil, err
	}
	const relPath = ".claude/settings.json"
	path := filepath.Join(root, filepath.FromSlash(relPath))
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []skillIssue{{Path: relPath, Message: "missing claude settings"}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", relPath, err)
	}
	var settings claudeSettingsFile
	if err := json.Unmarshal(b, &settings); err != nil {
		return []skillIssue{{Path: relPath, Message: fmt.Sprintf("invalid json: %v", err)}}, nil
	}

	var issues []skillIssue
	for section, values := range map[string][]string{
		"allow": settings.Permissions.Allow,
		"ask":   settings.Permissions.Ask,
		"deny":  settings.Permissions.Deny,
	} {
		if len(values) == 0 {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("permissions.%s is empty", section)})
		}
	}
	for _, permission := range claudeRequiredAllowPermissions() {
		if !containsString(settings.Permissions.Allow, permission) {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("missing allow permission %q", permission)})
		}
	}
	for _, permission := range claudeRequiredAskPermissions() {
		if !containsString(settings.Permissions.Ask, permission) {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("missing ask permission %q", permission)})
		}
	}
	for _, permission := range claudeRequiredDenyPermissions() {
		if !containsString(settings.Permissions.Deny, permission) {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("missing deny permission %q", permission)})
		}
	}
	for _, permission := range settings.Permissions.Allow {
		if containsString(claudeRequiredAskPermissions(), permission) || containsString(claudeRequiredDenyPermissions(), permission) {
			issues = append(issues, skillIssue{Path: relPath, Message: fmt.Sprintf("dangerous permission %q must not be allowed", permission)})
		}
	}
	return issues, nil
}

func claudeRequiredAllowPermissions() []string {
	return []string{
		"Read",
		"Edit",
		"Write",
		"Bash(git status*)",
		"Bash(git diff*)",
		"Bash(go test*)",
		"Bash(go vet*)",
		"Bash(gofmt*)",
		"Bash(scripts/go-format-changed.sh*)",
		"Bash(scripts/go-gate.sh*)",
		"Bash(scripts/go-tools-check.sh*)",
	}
}

func claudeRequiredAskPermissions() []string {
	return []string{
		"Bash(go mod tidy*)",
		"Bash(go get*)",
		"Bash(go install*)",
		"Bash(git commit*)",
		"Bash(git push*)",
		"Bash(git reset*)",
		"Bash(git clean*)",
		"Bash(docker*)",
		"Bash(docker compose*)",
		"Bash(ffmpeg*)",
		"Bash(powershell.exe*)",
		"Bash(pwsh*)",
		"Bash(scripts/build.ps1*)",
		"Bash(scripts/cleanup-artifacts.ps1*)",
		"Bash(scripts/audit-security-performance.ps1*)",
	}
}

func claudeRequiredDenyPermissions() []string {
	return []string{
		"Read(.env)",
		"Read(**/.env)",
		"Read(**/*id_rsa*)",
		"Read(**/*id_ed25519*)",
		"Read(**/*secret*)",
		"Read(**/*token*)",
		"Bash(rm -rf *)",
		"Bash(git reset --hard*)",
		"Bash(git push --force*)",
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func codexPromptContentRules() []codexPromptContentRule {
	return []codexPromptContentRule{
		{
			Path: ".codex/prompts/go-tdd.md",
			Required: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
			Forbidden: []string{
				"`go test ./... -count=1`",
				"`go vet ./...`",
			},
		},
		{
			Path: ".codex/prompts/go-bugfix.md",
			Required: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
			Forbidden: []string{
				"`go test ./... -count=1`",
				"`go vet ./...`",
			},
		},
		{
			Path: ".codex/prompts/go-pr-ready.md",
			Required: []string{
				"scripts/go-gate.sh",
				"scripts/go-gate.sh --no-format",
				"scripts/go-gate.sh --race",
				"scripts/go-gate.sh --security",
			},
		},
		{
			Path: ".codex/prompts/go-concurrency-review.md",
			Required: []string{
				"scripts/go-gate.sh --race --no-format",
			},
			Forbidden: []string{
				"`go test -race ./... -count=1`",
			},
		},
		{
			Path: ".codex/prompts/go-security-review.md",
			Required: []string{
				"scripts/go-gate.sh --security",
			},
		},
	}
}

func claudeCommandContentRules() []codexPromptContentRule {
	return []codexPromptContentRule{
		{
			Path: ".claude/commands/zv-plan.md",
			Required: []string{
				"Read-only. Do not edit files.",
				"git status --short",
				"Output:",
				"Tests and verification",
				"Risks / open questions",
			},
		},
		{
			Path: ".claude/commands/zv-tdd.md",
			Required: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
			Forbidden: []string{
				"`go test ./... -count=1`",
				"`go vet ./...`",
			},
		},
		{
			Path: ".claude/commands/zv-bugfix.md",
			Required: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
			Forbidden: []string{
				"`go test ./... -count=1`",
				"`go vet ./...`",
			},
		},
		{
			Path: ".claude/commands/zv-parser-change.md",
			Required: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
			},
		},
		{
			Path: ".claude/commands/zv-media-change.md",
			Required: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
			},
		},
		{
			Path: ".claude/commands/zv-worker-api-change.md",
			Required: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			Path: ".claude/commands/zv-pr-ready.md",
			Required: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race",
				"scripts/go-gate.sh --security",
			},
		},
		{
			Path: ".claude/commands/zv-artifact-audit.md",
			Required: []string{
				"Read-only. Do not edit or delete files.",
				"git status --short",
				".gitignore",
				"generated run data under `data/`",
				"Suggested commands",
			},
		},
		{
			Path: ".claude/commands/zv-toolchain-diagnose.md",
			Required: []string{
				"Read-only diagnosis. Do not install tools or edit files unless the user asks.",
				"scripts/go-tools-check.sh",
				"scripts/check-toolchain.ps1",
				"Do not run CS2/HLAE, Docker compose, migrations, or renders.",
				"Exact next commands",
			},
		},
	}
}

func mustRel(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func documentedWorkflowCommand(command string) string {
	fields, ok := splitCommandFields(command)
	if !ok {
		return ""
	}
	if len(fields) == 0 || fields[0] != "zv" {
		return ""
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

type legacyPassThrough struct {
	Command string
	Binary  string
}

func legacyPassThroughs() []legacyPassThrough {
	return []legacyPassThrough{
		{Command: "parser", Binary: "zv-parser"},
		{Command: "editor", Binary: "zv-editor"},
		{Command: "recorder", Binary: "zv-recorder"},
		{Command: "composer", Binary: "zv-composer"},
		{Command: "orchestrator", Binary: "zv-orchestrator"},
		{Command: "analysis-viewer", Binary: "zv-analysis-viewer"},
		{Command: "tactical-data", Binary: "zv-tactical-data"},
	}
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

func defaultLegacyCommandEntrypointNames() []string {
	return []string{
		"zv-parser",
		"zv-analysis-viewer",
		"zv-demo-players",
		"zv-recorder",
		"zv-editor",
		"zv-composer",
		"zv-pipeline",
		"zv-orchestrator",
		"zv-tactical-data",
	}
}

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

func validateSkillCommand(command []string) string {
	if len(command) == 0 {
		return "missing zv command"
	}
	switch command[0] {
	case "check":
		if issue := validateFormattedCommand("check", command[1:]); issue != "" {
			return issue
		}
	case "demo":
		if len(command) < 2 || (command[1] != "parse" && command[1] != "players") {
			return `uses non-standard zv command "demo"; expected "demo parse" or "demo players"`
		}
		switch command[1] {
		case "parse":
			return validateRequiredFlags(`"demo parse"`, command[2:], requiredFlagsForRunArgs("demo", "parse")...)
		case "players":
			return validateRequiredFlags(`"demo players"`, command[2:], requiredFlagsForRunArgs("demo", "players")...)
		}
	case "utility":
		if len(command) < 2 || command[1] != "audit" {
			return `uses non-standard zv command "utility"; expected "utility audit"`
		}
		return validateRequiredFlags(`"utility audit"`, command[2:], requiredFlagsForRunArgs("utility", "audit")...)
	case "compose":
		if len(command) < 2 || command[1] != "final" {
			return `uses non-standard zv command "compose"; expected "compose final"`
		}
		return validateRequiredFlags(`"compose final"`, command[2:], requiredFlagsForRunArgs("compose", "final")...)
	case "shorts":
		if len(command) < 2 || command[1] != "render" {
			return `uses non-standard zv command "shorts"; expected "shorts render"`
		}
		return validateRequiredFlags(`"shorts render"`, command[2:], requiredFlagsForRunArgs("shorts", "render")...)
	case "analysis":
		if len(command) < 2 || (command[1] != "tactical-data" && command[1] != "view") {
			return `uses non-standard zv command "analysis"; expected "analysis tactical-data" or "analysis view"`
		}
		switch command[1] {
		case "tactical-data":
			return validateRequiredFlags(`"analysis tactical-data"`, command[2:], requiredFlagsForRunArgs("analysis", "tactical-data")...)
		case "view":
			return validateRequiredFlags(`"analysis view"`, command[2:], requiredFlagsForRunArgs("analysis", "view")...)
		}
	case "gallery":
		if len(command) < 2 || command[1] != "open" {
			return `uses non-standard zv command "gallery"; expected "gallery open"`
		}
		return validateRequiredFlags(`"gallery open"`, command[2:], requiredFlagsForRunArgs("gallery", "open")...)
	case "skills":
		if len(command) < 2 || (command[1] != "list" && command[1] != "show" && command[1] != "check") {
			return `uses non-standard zv command "skills"; expected "skills list", "skills show", or "skills check"`
		}
		switch command[1] {
		case "list", "check":
			if issue := validateFormattedCommand(strings.Join(command[:2], " "), command[2:]); issue != "" {
				return issue
			}
		case "show":
			if issue := validateSkillShowCommand(command[2:]); issue != "" {
				return issue
			}
		}
	case "record":
		return validateRequiredFlags(`"record"`, command[1:], requiredFlagsForRunArgs("record")...)
	case "pipeline":
		return validateRequiredFlags(`"pipeline"`, command[1:], requiredFlagsForRunArgs("pipeline")...)
	case "serve":
		if isSingleHelp(command[1:]) {
			return ""
		}
		if len(command) != 1 {
			return `unexpected extra args for "serve"`
		}
	case "workflows":
		if len(command) < 2 || (command[1] != "list" && command[1] != "show" && command[1] != "run" && command[1] != "check") {
			return `uses non-standard zv command "workflows"; expected "workflows list", "workflows show", "workflows run", or "workflows check"`
		}
		switch command[1] {
		case "list":
			if issue := validateWorkflowListCommand(command[2:]); issue != "" {
				return issue
			}
		case "show":
			if issue := validateWorkflowShowCommand(command[2:]); issue != "" {
				return issue
			}
		case "run":
			if issue := validateWorkflowRunCommand(command[2:]); issue != "" {
				return issue
			}
		case "check":
			if issue := validateFormattedCommand(strings.Join(command[:2], " "), command[2:]); issue != "" {
				return issue
			}
		}
	default:
		return fmt.Sprintf("uses non-standard zv command %q", command[0])
	}
	return ""
}

func requiredFlagsForRunArgs(args ...string) []string {
	if equalArgs(args, []string{"record"}) {
		return []string{"--killplan", "--demo", "--out"}
	}
	for _, workflow := range workflowCatalog() {
		if !equalArgs(workflow.RunArgs, args) {
			continue
		}
		return requiredFlagsFromCommand(workflow.Command)
	}
	return nil
}

func requiredFlagsFromCommand(command string) []string {
	fields, ok := splitCommandFields(command)
	if !ok {
		return nil
	}
	var flags []string
	for _, field := range fields {
		if strings.HasPrefix(field, "--") {
			flags = append(flags, field)
		}
	}
	return flags
}

func validateFormattedCommand(commandName string, args []string) string {
	if isSingleHelp(args) {
		return ""
	}
	if _, rest, err := parseFormatArgs(args); err != nil {
		return err.Error()
	} else if len(rest) != 0 {
		return fmt.Sprintf("unexpected extra args for %q", commandName)
	}
	return ""
}

func validateWorkflowListCommand(args []string) string {
	return validateFormattedCommand("workflows list", args)
}

func validateSkillShowCommand(args []string) string {
	if isSingleHelp(args) {
		return ""
	}
	if _, rest, err := parseFormatArgs(args); err != nil {
		return err.Error()
	} else if len(rest) == 0 {
		return `missing skill name for "skills show"`
	} else if len(rest) > 1 {
		return `unexpected extra args for "skills show"`
	}
	return ""
}

func validateWorkflowShowCommand(args []string) string {
	if isSingleHelp(args) {
		return ""
	}
	if _, rest, err := parseFormatArgs(args); err != nil {
		return err.Error()
	} else if len(rest) == 0 {
		return `missing workflow name for "workflows show"`
	} else if len(rest) > 1 {
		return `unexpected extra args for "workflows show"`
	} else if _, ok := findWorkflow(rest[0]); !ok {
		return fmt.Sprintf(`unknown workflow name %q for "workflows show"`, rest[0])
	}
	return ""
}

func validateWorkflowRunCommand(args []string) string {
	if len(args) == 0 {
		return `missing workflow name for "workflows run"`
	}
	workflow, ok := findWorkflow(args[0])
	if !ok {
		return fmt.Sprintf(`unknown workflow name %q for "workflows run"`, args[0])
	}
	rest := args[1:]
	if issue := validateWorkflowRunForwardedArgs(workflow, rest); issue != "" {
		return issue
	}
	return ""
}

func validateWorkflowRunForwardedArgs(workflow workflowInfo, rest []string) string {
	if len(rest) > 0 && rest[0] != "--" {
		return `missing "--" separator before forwarded args for "workflows run"`
	}
	var forwarded []string
	if len(rest) > 0 {
		forwarded = rest[1:]
	}
	if isSingleHelp(forwarded) {
		return ""
	}
	command := append([]string(nil), workflow.RunArgs...)
	command = append(command, forwarded...)
	return validateSkillCommand(command)
}

func validateRequiredFlags(commandName string, args []string, required ...string) string {
	if isSingleHelp(args) {
		return ""
	}
	valueFlags := commandValueFlags(commandName, required)
	boolFlags := commandBoolFlags(commandName)
	if flag := duplicateFlag(args); flag != "" {
		return fmt.Sprintf("duplicate flag %s for %s", flag, commandName)
	}
	if flag := unknownFlag(args, valueFlags, boolFlags); flag != "" {
		return fmt.Sprintf("unknown flag %s for %s", flag, commandName)
	}
	if flag, value := invalidBooleanFlagValue(args, boolFlags); flag != "" {
		return fmt.Sprintf("invalid boolean value %q for flag %s for %s", value, flag, commandName)
	}
	var missing []string
	for _, name := range required {
		if !hasFlagValue(args, name) {
			missing = append(missing, name)
		}
	}
	if len(missing) == 1 {
		return fmt.Sprintf("missing required flag %s for %s", missing[0], commandName)
	}
	if len(missing) > 1 {
		return fmt.Sprintf("missing required flags %s for %s", strings.Join(missing, ", "), commandName)
	}
	if len(missing) == 0 && commandName == `"record"` && !booleanFlagIsTrue(args, "--dry-run") {
		var captureMissing []string
		for _, name := range []string{"--hlae", "--cs2"} {
			if !hasFlagValue(args, name) {
				captureMissing = append(captureMissing, name)
			}
		}
		if len(captureMissing) == 1 {
			return fmt.Sprintf("missing required flag %s for %s unless --dry-run is set", captureMissing[0], commandName)
		}
		if len(captureMissing) > 1 {
			return fmt.Sprintf("missing required flags %s for %s unless --dry-run is set", strings.Join(captureMissing, ", "), commandName)
		}
	}
	if flag, value := booleanFlagSeparateValue(args, boolFlags); flag != "" {
		return fmt.Sprintf("boolean flag %s for %s does not take separate value %q; use %s=%s", flag, commandName, value, flag, value)
	}
	if flag := optionalValueFlagMissingValue(args, valueFlags, required); flag != "" {
		return fmt.Sprintf("missing value for flag %s for %s", flag, commandName)
	}
	if arg := unexpectedPositionalArg(args, valueFlags); arg != "" {
		return fmt.Sprintf("unexpected positional arg %q for %s; quote paths with spaces", arg, commandName)
	}
	return ""
}

func commandValueFlags(commandName string, required []string) []string {
	flags := append([]string(nil), required...)
	switch commandName {
	case `"demo parse"`:
		flags = append(flags, "--segment-mode", "--rules")
	case `"demo players"`:
		flags = append(flags, "--contains")
	case `"utility audit"`:
		flags = append(flags, "--format")
	case `"record"`:
		flags = append(flags, "--hlae", "--cs2", "--hud", "--fps", "--video-crf", "--timeout")
	case `"compose final"`:
		flags = append(flags, "--ffmpeg", "--timeout")
	case `"shorts render"`:
		flags = append(flags,
			"--killplan",
			"--publish-dir",
			"--preset",
			"--effects",
			"--effects-preset",
			"--lineup-catalog",
			"--segments",
			"--limit",
			"--player-image",
			"--player-key-color",
			"--video-crf",
			"--video-preset",
			"--ffmpeg",
			"--ffprobe",
		)
	case `"analysis tactical-data"`:
		flags = append(flags, "--sample")
	case `"analysis view"`:
		flags = append(flags, "--addr")
	case `"pipeline"`:
		flags = append(flags, "--recorder", "--composer", "--ffmpeg", "--record-timeout", "--compose-timeout")
	}
	return flags
}

func commandBoolFlags(commandName string) []string {
	switch commandName {
	case `"demo parse"`:
		return []string{"--verbose"}
	case `"record"`, `"compose final"`:
		return []string{"--dry-run"}
	case `"shorts render"`:
		return []string{
			"--audio-normalize",
			"--cover-sheets",
			"--covers",
			"--dry-run",
			"--hq-filters",
			"--no-covers",
			"--open-gallery",
			"--quality-checks",
			"--skip-existing",
			"--temporal-smoothing",
		}
	default:
		return nil
	}
}

func duplicateFlag(args []string) string {
	seen := make(map[string]struct{})
	for _, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		name := arg
		if before, _, ok := strings.Cut(arg, "="); ok {
			name = before
		}
		if _, ok := seen[name]; ok {
			return name
		}
		seen[name] = struct{}{}
	}
	return ""
}

func unknownFlag(args []string, valueFlags []string, boolFlags []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		flag := arg
		hasEquals := false
		if before, _, ok := strings.Cut(arg, "="); ok {
			flag = before
			hasEquals = true
		}
		if !isLongFlag(flag) {
			return flag
		}
		if flagTakesValue(flag, valueFlags) {
			if !hasEquals && i+1 < len(args) && !isLongFlag(args[i+1]) {
				i++
			}
			continue
		}
		if flagIsBoolean(flag, boolFlags) {
			continue
		}
		return flag
	}
	return ""
}

func invalidBooleanFlagValue(args []string, boolFlags []string) (string, string) {
	for _, arg := range args {
		flag, value, ok := strings.Cut(arg, "=")
		if !ok || !flagIsBoolean(flag, boolFlags) {
			continue
		}
		if _, err := strconv.ParseBool(value); err != nil {
			return flag, value
		}
	}
	return "", ""
}

func booleanFlagSeparateValue(args []string, boolFlags []string) (string, string) {
	for i, arg := range args {
		if !flagIsBoolean(arg, boolFlags) || i+1 >= len(args) || isLongFlag(args[i+1]) {
			continue
		}
		if _, err := strconv.ParseBool(args[i+1]); err == nil {
			return arg, args[i+1]
		}
	}
	return "", ""
}

func booleanFlagIsTrue(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
		if value, ok := strings.CutPrefix(arg, name+"="); ok {
			parsed, err := strconv.ParseBool(value)
			return err == nil && parsed
		}
	}
	return false
}

func hasFlagValue(args []string, name string) bool {
	for i, arg := range args {
		if strings.HasPrefix(arg, name+"=") {
			return strings.TrimSpace(strings.TrimPrefix(arg, name+"=")) != ""
		}
		if arg == name {
			return i+1 < len(args) && !isLongFlag(args[i+1]) && strings.TrimSpace(args[i+1]) != ""
		}
	}
	return false
}

func optionalValueFlagMissingValue(args []string, valueFlags []string, required []string) string {
	for i, arg := range args {
		if !strings.HasPrefix(arg, "--") {
			continue
		}
		flag := arg
		if before, value, ok := strings.Cut(arg, "="); ok {
			flag = before
			if flagTakesValue(flag, valueFlags) && !flagTakesValue(flag, required) && strings.TrimSpace(value) == "" {
				return flag
			}
			continue
		}
		if !flagTakesValue(flag, valueFlags) || flagTakesValue(flag, required) {
			continue
		}
		if i+1 >= len(args) || isLongFlag(args[i+1]) || strings.TrimSpace(args[i+1]) == "" {
			return flag
		}
	}
	return ""
}

func unexpectedPositionalArg(args []string, valueFlags []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if strings.Contains(arg, "=") {
				continue
			}
			if flagTakesValue(arg, valueFlags) && i+1 < len(args) && !isLongFlag(args[i+1]) {
				i++
			}
			continue
		}
		return arg
	}
	return ""
}

func flagTakesValue(flag string, valueFlags []string) bool {
	for _, valueFlag := range valueFlags {
		if flag == valueFlag {
			return true
		}
	}
	return false
}

func isLongFlag(arg string) bool {
	return strings.HasPrefix(arg, "--")
}

func flagIsBoolean(flag string, boolFlags []string) bool {
	for _, boolFlag := range boolFlags {
		if flag == boolFlag {
			return true
		}
	}
	return false
}

func findSkillsDir() (string, error) {
	var starts []string
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		for dir := start; ; dir = filepath.Dir(dir) {
			candidate := filepath.Join(dir, ".codex", "skills")
			if st, err := os.Stat(candidate); err == nil && st.IsDir() {
				return candidate, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return "", fmt.Errorf("skills dir not found: .codex/skills")
}

func findWorkflowRoot() (string, error) {
	var starts []string
	if cwd, err := os.Getwd(); err == nil {
		starts = append(starts, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		starts = append(starts, filepath.Dir(exe))
	}
	for _, start := range starts {
		for dir := start; ; dir = filepath.Dir(dir) {
			if hasWorkflowRootMarker(dir) {
				return dir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return "", fmt.Errorf("workflow root not found")
}

func hasWorkflowRootMarker(dir string) bool {
	if st, err := os.Stat(filepath.Join(dir, ".codex", "skills")); err == nil && st.IsDir() {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return true
	}
	return false
}

func parseSkill(path string) (skillInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return skillInfo{}, fmt.Errorf("open skill %s: %w", path, err)
	}
	defer f.Close()

	skill := skillInfo{Path: path}
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return skillInfo{}, fmt.Errorf("scan skill %s: %w", path, err)
		}
		return skill, nil
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return skill, nil
	}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = trimMetadataValue(value)
		switch strings.TrimSpace(key) {
		case "name":
			skill.Name = value
		case "description":
			skill.Description = value
		}
	}
	if err := scanner.Err(); err != nil {
		return skillInfo{}, fmt.Errorf("scan skill %s: %w", path, err)
	}
	return skill, nil
}

func trimMetadataValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
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
