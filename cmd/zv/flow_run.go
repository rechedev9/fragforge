package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// flowRunReport is the machine-readable summary of a `zv flows run` dry-run: one
// entry per phase in flow order, plus the global outcome. Global OK is false
// when any executed (or dry-run preflight) phase failed; intentional skips
// (creative gates, missing preconditions) do not flip it.
type flowRunReport struct {
	OK     bool                 `json:"ok"`
	Flow   string               `json:"flow"`
	RunDir string               `json:"run_dir"`
	DryRun bool                 `json:"dry_run"`
	Phases []flowRunPhaseReport `json:"phases"`
}

type flowRunPhaseReport struct {
	Phase    string   `json:"phase"`
	Argv     []string `json:"argv,omitempty"`
	OK       bool     `json:"ok"`
	DryRun   bool     `json:"dry_run"`
	Executed bool     `json:"executed"`
	Skipped  bool     `json:"skipped,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Outputs  []string `json:"outputs,omitempty"`
}

// flowRunStep is one phase of a production flow's dry-run. build is evaluated
// immediately before the phase runs so it can read the chainable JSON artifacts
// earlier phases wrote into the run dir (for example, `select` reads the kill
// plan `parse` produced to select every segment in plan order).
type flowRunStep struct {
	id    string
	build func() (flowRunAction, error)
}

// flowRunAction is what a phase does: run argv (cheap stages for real, expensive
// stages with dryRun set so --dry-run is already in argv), skip a creative gate
// (gate), or skip because a precondition is missing (skip). outputs are the
// artifact paths the phase is expected to write, filtered to those that exist.
type flowRunAction struct {
	argv    []string
	dryRun  bool
	outputs []string
	gate    bool
	skip    bool
	reason  string
}

// runFlowsRun executes a production flow end to end in --dry-run mode: cheap,
// deterministic stages run for real and write chainable JSON into --run-dir,
// expensive capture/render stages run with --dry-run appended, and creative
// gates are reported as skipped. Real execution stays stage by stage behind the
// creative gates, so a non-dry-run invocation is rejected.
func runFlowsRun(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, flowsRunUsage)
		return exitSuccess
	}
	// Validate the argv with the same canonical rules as every other command
	// (unknown/duplicate/missing flags, stray positionals) so direct invocations
	// and documented command lines report identical errors.
	if issue := validateFlowsRunCommand(args); issue != "" {
		return writeFlowError(args, stdout, stderr, fmt.Errorf("%s", issue), flowsRunUsage)
	}

	// The flow name is always the first token (validateFlowsRunCommand enforces
	// this), so the rest are the flow flags. Never scan for the first non-dash
	// token: that stole flag values from lines like "flows run --run-dir X demo".
	flowName := args[0]
	rest := args[1:]

	fs := flag.NewFlagSet("flows run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	runDir := fs.String("run-dir", "", "run output directory")
	dryRun := fs.Bool("dry-run", false, "resolve and chain every phase safely")
	format := fs.String("format", "text", "text or json")
	demo := fs.String("demo", "", "demo .dem path")
	steamid := fs.String("steamid", "", "target SteamID64")
	killplanPath := fs.String("killplan", "", "existing kill plan JSON; skips demo parse")
	input := fs.String("input", "", "stream video path")
	events := fs.String("events", "", "reviewed killfeed events JSON")
	words := fs.String("words", "", "reviewed caption words JSON")
	if err := fs.Parse(rest); err != nil {
		return writeFlowError(args, stdout, stderr, err, flowsRunUsage)
	}
	if *format != "text" && *format != "json" {
		return writeFlowError(args, stdout, stderr, fmt.Errorf("unsupported format %q", *format), flowsRunUsage)
	}
	if !*dryRun {
		return writeFlowError(args, stdout, stderr,
			fmt.Errorf("%q currently supports only --dry-run; real execution remains stage by stage behind the creative gates (see %q)", "flows run", "zv flows show "+flowName),
			flowsRunUsage)
	}
	// Reject the documentation template token "<demo|stream>" and any other
	// non-runnable flow BEFORE creating the run dir. validateFlowsRunCommand
	// tolerates the template for doc/catalog validation, so the runner must fail
	// closed here rather than exit 0 with an empty {ok:true} report after
	// creating a directory.
	var steps []flowRunStep
	switch flowName {
	case "demo":
		if err := os.MkdirAll(*runDir, 0o750); err != nil {
			return writeFlowError(args, stdout, stderr, fmt.Errorf("create run dir: %w", err), "")
		}
		steps = demoFlowRunSteps(*runDir, *demo, *steamid, *killplanPath)
	case "stream":
		if err := os.MkdirAll(*runDir, 0o750); err != nil {
			return writeFlowError(args, stdout, stderr, fmt.Errorf("create run dir: %w", err), "")
		}
		steps = streamFlowRunSteps(*runDir, *input, *events, *words)
	default:
		return writeFlowError(args, stdout, stderr,
			fmt.Errorf(`unknown flow %q for "flows run"; expected demo or stream`, flowName), flowsRunUsage)
	}

	report := flowRunReport{OK: true, Flow: flowName, RunDir: *runDir, DryRun: true}
	failed := false
	for _, step := range steps {
		if failed {
			report.Phases = append(report.Phases, flowRunPhaseReport{Phase: step.id, Skipped: true, Reason: "not run: an earlier phase failed"})
			continue
		}
		action, err := step.build()
		if err != nil {
			report.OK = false
			failed = true
			report.Phases = append(report.Phases, flowRunPhaseReport{Phase: step.id, Reason: err.Error()})
			continue
		}
		if action.gate || action.skip {
			report.Phases = append(report.Phases, flowRunPhaseReport{Phase: step.id, OK: true, Skipped: true, Reason: action.reason})
			continue
		}
		var out, errBuf bytes.Buffer
		code := Run(append([]string{"zv"}, action.argv...), &out, &errBuf, stdin, runner)
		// The command ran, so a real (non-dry-run) phase is executed regardless of
		// its exit code: a failure may have persisted partial mutations, and the
		// report must not hide that by only setting Executed on success.
		phase := flowRunPhaseReport{Phase: step.id, Argv: action.argv, DryRun: action.dryRun, Executed: !action.dryRun}
		if code == exitSuccess {
			phase.OK = true
			phase.Outputs = existingPaths(action.outputs)
		} else {
			phase.OK = false
			phase.Reason = flowPhaseFailureReason(errBuf.String(), out.String(), code)
			report.OK = false
			failed = true
		}
		report.Phases = append(report.Phases, phase)
	}

	if *format == "json" {
		if err := writeJSON(stdout, report); err != nil {
			fmt.Fprintf(stderr, "error: write flow run report: %v\n", err)
			return exitUnexpected
		}
	} else {
		writeFlowRunText(stdout, report)
	}
	if !report.OK {
		return exitUnexpected
	}
	return exitSuccess
}

// demoFlowRunSteps mirrors the demo journey's chain: parse (skipped when a kill
// plan is supplied), moments, the creative-brief gate, select (every segment in
// plan order, the documented dry-run default), the capture and render dry runs,
// and the thumbnail gate.
func demoFlowRunSteps(runDir, demo, steamid, killplanFlag string) []flowRunStep {
	killplanPath := killplanFlag
	if strings.TrimSpace(killplanPath) == "" {
		killplanPath = filepath.Join(runDir, "killplan.json")
	}
	momentsPath := filepath.Join(runDir, "moments.json")
	selectedPath := filepath.Join(runDir, "selected-plan.json")
	recordingDir := filepath.Join(runDir, "recording")
	recordingResult := filepath.Join(recordingDir, "recording-result.json")
	renderDir := filepath.Join(runDir, "render")
	publishDir := filepath.Join(runDir, "shortslistosparasubir")
	shortsResult := filepath.Join(renderDir, "shorts-result.json")

	return []flowRunStep{
		{id: "parse", build: func() (flowRunAction, error) {
			if strings.TrimSpace(killplanFlag) != "" {
				return flowRunAction{skip: true, reason: "kill plan supplied; skipping demo parse"}, nil
			}
			if strings.TrimSpace(demo) == "" {
				return flowRunAction{}, fmt.Errorf("demo parse requires --demo or --killplan")
			}
			if strings.TrimSpace(steamid) == "" {
				return flowRunAction{}, fmt.Errorf("demo parse requires --steamid")
			}
			return flowRunAction{
				argv:    []string{"demo", "parse", "--demo", demo, "--steamid", steamid, "--out", killplanPath},
				outputs: []string{killplanPath},
			}, nil
		}},
		{id: "moments", build: func() (flowRunAction, error) {
			return flowRunAction{
				argv:    []string{"demo", "moments", "--killplan", killplanPath, "--out", momentsPath, "--format", "json"},
				outputs: []string{momentsPath},
			}, nil
		}},
		{id: "creative-brief", build: func() (flowRunAction, error) {
			return flowRunAction{gate: true, reason: "creative gate: approve delivery format, HUD/killfeed, kill effect, transition, counter, intro/outro, music, and thumbnail strategy"}, nil
		}},
		{id: "select", build: func() (flowRunAction, error) {
			ids, err := demoFlowSegmentIDs(killplanPath)
			if err != nil {
				return flowRunAction{}, err
			}
			return flowRunAction{
				argv:    []string{"demo", "select", "--killplan", killplanPath, "--segments", strings.Join(ids, ","), "--out", selectedPath, "--format", "json"},
				outputs: []string{selectedPath},
			}, nil
		}},
		{id: "record", build: func() (flowRunAction, error) {
			if strings.TrimSpace(demo) == "" {
				return flowRunAction{skip: true, reason: "no --demo provided; skipping capture dry-run"}, nil
			}
			return flowRunAction{
				argv:    []string{"record", "--killplan", selectedPath, "--demo", demo, "--out", recordingDir, "--dry-run", "--format", "json"},
				dryRun:  true,
				outputs: []string{recordingResult},
			}, nil
		}},
		{id: "shorts-render", build: func() (flowRunAction, error) {
			if _, err := os.Stat(recordingResult); err != nil {
				return flowRunAction{skip: true, reason: "no recording-result.json (capture skipped); skipping render dry-run"}, nil
			}
			return flowRunAction{
				argv:    []string{"shorts", "render", "--recording-result", recordingResult, "--killplan", selectedPath, "--out", renderDir, "--publish-dir", publishDir, "--dry-run"},
				dryRun:  true,
				outputs: []string{shortsResult},
			}, nil
		}},
		{id: "thumbnail-selection", build: func() (flowRunAction, error) {
			return flowRunAction{gate: true, reason: "creative gate: choose a cover candidate or delegate automatic selection"}, nil
		}},
	}
}

// streamFlowRunSteps mirrors the stream journey's chain: plan (persisted for
// real; it probes media with ffprobe), the killfeed and captions imports (each
// skipped when its reviewed input is absent), and the render dry run. The plan
// input to each later phase advances to the latest persisted document.
func streamFlowRunSteps(runDir, input, events, words string) []flowRunStep {
	editPlan := filepath.Join(runDir, "edit-plan.json")
	reviewedPlan := filepath.Join(runDir, "reviewed-plan.json")
	finalPlan := filepath.Join(runDir, "final-plan.json")
	renderDir := filepath.Join(runDir, "render")

	captionsInput := func() string {
		if strings.TrimSpace(events) != "" {
			return reviewedPlan
		}
		return editPlan
	}
	renderInput := func() string {
		if strings.TrimSpace(words) != "" {
			return finalPlan
		}
		return captionsInput()
	}

	return []flowRunStep{
		{id: "plan", build: func() (flowRunAction, error) {
			if strings.TrimSpace(input) == "" {
				return flowRunAction{}, fmt.Errorf("stream plan requires --input")
			}
			return flowRunAction{
				argv:    []string{"stream", "plan", "--input", input, "--out", editPlan, "--format", "json"},
				outputs: []string{editPlan},
			}, nil
		}},
		{id: "killfeed", build: func() (flowRunAction, error) {
			if strings.TrimSpace(events) == "" {
				return flowRunAction{skip: true, reason: "no --events provided; skipping killfeed import"}, nil
			}
			return flowRunAction{
				argv:    []string{"stream", "killfeed", "--plan", editPlan, "--events", events, "--out", reviewedPlan, "--format", "json"},
				outputs: []string{reviewedPlan},
			}, nil
		}},
		{id: "captions", build: func() (flowRunAction, error) {
			if strings.TrimSpace(words) == "" {
				return flowRunAction{skip: true, reason: "no --words provided; skipping captions import"}, nil
			}
			return flowRunAction{
				argv:    []string{"stream", "captions", "--plan", captionsInput(), "--words", words, "--out", finalPlan, "--format", "json"},
				outputs: []string{finalPlan},
			}, nil
		}},
		{id: "render", build: func() (flowRunAction, error) {
			return flowRunAction{
				argv:    []string{"stream", "render", "--input", input, "--plan", renderInput(), "--out", renderDir, "--dry-run", "--format", "json"},
				dryRun:  true,
				outputs: []string{renderDir},
			}, nil
		}},
	}
}

func demoFlowSegmentIDs(path string) ([]string, error) {
	plan, err := loadDemoKillPlan(path)
	if err != nil {
		return nil, fmt.Errorf("read kill plan: %w", err)
	}
	ids := make([]string, 0, len(plan.Segments))
	for _, seg := range plan.Segments {
		if strings.TrimSpace(seg.ID) == "" {
			return nil, fmt.Errorf("kill plan contains a segment without an id")
		}
		ids = append(ids, seg.ID)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("kill plan has no segments to select")
	}
	return ids, nil
}

func existingPaths(paths []string) []string {
	var out []string
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func flowPhaseFailureReason(stderr, stdout string, code int) string {
	if line := firstNonEmptyLine(stderr); line != "" {
		return line
	}
	if line := firstNonEmptyLine(stdout); line != "" {
		return line
	}
	return fmt.Sprintf("exit %d", code)
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func writeFlowRunText(w io.Writer, report flowRunReport) {
	fmt.Fprintf(w, "flow: %s (run-dir: %s, dry-run)\n", report.Flow, report.RunDir)
	for i, phase := range report.Phases {
		fmt.Fprintf(w, "%d. %-20s %s\n", i+1, phase.Phase, flowPhaseStatus(phase))
	}
	if report.OK {
		fmt.Fprintln(w, "result: ok")
	} else {
		fmt.Fprintln(w, "result: failed")
	}
}

func flowPhaseStatus(phase flowRunPhaseReport) string {
	switch {
	case phase.Skipped:
		if phase.Reason != "" {
			return "skipped (" + phase.Reason + ")"
		}
		return "skipped"
	case !phase.OK:
		if phase.Reason != "" {
			return "failed (" + phase.Reason + ")"
		}
		return "failed"
	case phase.DryRun:
		return "ok (dry-run)"
	default:
		return "ok (executed)"
	}
}
