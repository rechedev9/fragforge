package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runFlowsRunInProcess dispatches `zv flows run ...` in-process. It suits the
// media-free paths (no --demo, so capture and render are skipped) where every
// executed phase runs inside this process without shelling out.
func runFlowsRunInProcess(t *testing.T, args ...string) (flowRunReport, int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"zv", "flows", "run"}, args...), &stdout, &stderr, nil, &fakeRunner{})
	var report flowRunReport
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			// Non-JSON stdout (text mode or an argument error) is returned raw via
			// the string result; leave report zero.
			return flowRunReport{}, code, stdout.String() + stderr.String()
		}
	}
	return report, code, stderr.String()
}

func phaseByName(report flowRunReport, name string) (flowRunPhaseReport, bool) {
	for _, phase := range report.Phases {
		if phase.Phase == name {
			return phase, true
		}
	}
	return flowRunPhaseReport{}, false
}

func TestFlowsRunRejectsWithoutDryRun(t *testing.T) {
	ws := t.TempDir()
	plan := writeStageContractPlan(t, ws)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"zv", "flows", "run", "demo", "--killplan", plan, "--run-dir", filepath.Join(ws, "run")}, &stdout, &stderr, nil, &fakeRunner{})
	if code != exitInvalidArgs {
		t.Fatalf("code = %d, want %d\nstderr: %s", code, exitInvalidArgs, stderr.String())
	}
	if !strings.Contains(stderr.String(), "supports only --dry-run") {
		t.Fatalf("stderr = %q, want the stage-by-stage explanation", stderr.String())
	}
}

func TestFlowsRunRequiresFlowNameFirst(t *testing.T) {
	ws := t.TempDir()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "missing flow name",
			args: []string{"--run-dir", filepath.Join(ws, "a"), "--dry-run"},
			want: "missing flow name",
		},
		{
			// The flow name must be the first token; a leading flag must not be
			// mistaken for it, and its value must not be stolen as the flow.
			name: "flow name after flags is rejected",
			args: []string{"--run-dir", filepath.Join(ws, "b"), "demo", "--dry-run"},
			want: "missing flow name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(append([]string{"zv", "flows", "run"}, tc.args...), &stdout, &stderr, nil, &fakeRunner{})
			if code != exitInvalidArgs {
				t.Fatalf("code = %d, want %d\nstderr: %s", code, exitInvalidArgs, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tc.want)
			}
		})
	}
}

// TestFlowsRunRejectsTemplateFlowWithoutCreatingRunDir pins that the literal
// documentation token "<demo|stream>" is rejected at runtime with a non-zero
// exit BEFORE the run dir is created, rather than exiting 0 with an empty report.
func TestFlowsRunRejectsTemplateFlowWithoutCreatingRunDir(t *testing.T) {
	ws := t.TempDir()
	runDir := filepath.Join(ws, "run")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"zv", "flows", "run", "<demo|stream>", "--run-dir", runDir, "--dry-run"}, &stdout, &stderr, nil, &fakeRunner{})
	if code == exitSuccess {
		t.Fatalf("code = %d, want a non-zero exit for the template token\nstdout: %s", code, stdout.String())
	}
	if !strings.Contains(stderr.String()+stdout.String(), "unknown flow") {
		t.Fatalf("output = %q/%q, want unknown flow error", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(runDir); err == nil {
		t.Fatalf("run dir %s was created for a rejected flow, want no directory", runDir)
	}
}

// TestFlowRunnerStepsCoverRegistryPhases links the runner (flow_run.go) to the
// descriptive registry (productionFlows): every runner step must drive a real
// registry phase, and every registry phase must be either driven by a runner
// step or explicitly exempt (doctor, players, *-preflight, gates, review,
// transcribe...). A new registry phase added without runner coverage or an
// exemption fails here. Runner step ids intentionally differ from a few phase
// ids (record->capture, shorts-render->edit, killfeed->enrich), so the link is a
// documented correspondence map rather than raw id equality.
func TestFlowRunnerStepsCoverRegistryPhases(t *testing.T) {
	setOf := func(ids ...string) map[string]bool {
		m := make(map[string]bool, len(ids))
		for _, id := range ids {
			m[id] = true
		}
		return m
	}
	cases := []struct {
		flow        string
		steps       []flowRunStep
		runnerPhase map[string]string
		exempt      map[string]bool
	}{
		{
			flow:  "demo",
			steps: demoFlowRunSteps("run", "match.dem", "76561198000000000", ""),
			runnerPhase: map[string]string{
				"parse":               "parse",
				"moments":             "moments",
				"creative-brief":      "creative-brief",
				"select":              "select",
				"record":              "capture",
				"shorts-render":       "edit",
				"thumbnail-selection": "thumbnail-selection",
			},
			exempt: setOf("doctor", "players", "parse-preflight", "moments-preflight", "select-preflight", "capture-preflight", "edit-preflight", "review"),
		},
		{
			flow:  "stream",
			steps: streamFlowRunSteps("run", "stream.mp4", "", "", ""),
			runnerPhase: map[string]string{
				"creative-brief": "creative-brief",
				"plan":           "plan",
				"killfeed":       "enrich",
				"captions":       "captions",
				"render":         "render",
			},
			exempt: setOf("doctor", "layouts", "plan-preflight", "killfeed-preflight", "transcribe-preflight", "transcribe", "captions-preflight", "render-preflight", "review"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.flow, func(t *testing.T) {
			flow, ok := findProductionFlow(tc.flow)
			if !ok {
				t.Fatalf("production flow %q not found", tc.flow)
			}
			phaseIDs := make(map[string]bool, len(flow.Phases))
			for _, phase := range flow.Phases {
				phaseIDs[phase.ID] = true
			}
			covered := make(map[string]bool)
			for _, step := range tc.steps {
				target, ok := tc.runnerPhase[step.id]
				if !ok {
					t.Fatalf("runner step %q has no documented registry phase; map it or exempt it", step.id)
				}
				if !phaseIDs[target] {
					t.Fatalf("runner step %q maps to phase %q, which is not in flow %q", step.id, target, tc.flow)
				}
				covered[target] = true
			}
			for _, phase := range flow.Phases {
				if covered[phase.ID] || tc.exempt[phase.ID] {
					continue
				}
				t.Fatalf("flow %q phase %q has no runner step and is not exempt; add runner coverage or exempt it", tc.flow, phase.ID)
			}
		})
	}
}

func TestStreamFlowRunPlanDetectsKillfeedOnlyWhenEventsProvided(t *testing.T) {
	cases := []struct {
		name         string
		events       string
		killfeedCrop string
		wantDetect   bool
	}{
		{name: "events provided enables detection with crop", events: "events.json", killfeedCrop: "0.66,0.04,0.32,0.25", wantDetect: true},
		{name: "no events skips detection", events: "", killfeedCrop: "", wantDetect: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			steps := streamFlowRunSteps("run", "stream.mp4", tc.events, "", tc.killfeedCrop)
			var planStep flowRunStep
			for _, step := range steps {
				if step.id == "plan" {
					planStep = step
					break
				}
			}
			if planStep.id == "" {
				t.Fatalf("stream flow has no plan step: %#v", steps)
			}
			action, err := planStep.build()
			if err != nil {
				t.Fatalf("build plan step: %v", err)
			}
			if got, want := containsString(action.argv, "--detect-killfeed"), tc.wantDetect; got != want {
				t.Fatalf("plan argv --detect-killfeed = %v, want %v; argv = %v", got, want, action.argv)
			}
			if got, want := containsString(action.argv, "--killfeed-crop"), tc.wantDetect; got != want {
				t.Fatalf("plan argv --killfeed-crop = %v, want %v; argv = %v", got, want, action.argv)
			}
		})
	}
}

func TestFlowsRunStreamEventsRequireKillfeedCrop(t *testing.T) {
	ws := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"zv", "flows", "run", "stream",
		"--input", "stream.mp4",
		"--events", "events.json",
		"--run-dir", ws,
		"--dry-run"}, &stdout, &stderr, nil, &fakeRunner{})
	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if want := "--events requires --killfeed-crop"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestFlowsRunDemoFailsFastOnMissingParseInputs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "demo without steamid",
			args: []string{"--demo", "match.dem"},
			want: "--demo requires --steamid",
		},
		{
			name: "neither demo nor killplan",
			args: []string{},
			want: "requires --demo (or --killplan to skip parse)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runDir := filepath.Join(t.TempDir(), "run")
			args := append([]string{"zv", "flows", "run", "demo", "--run-dir", runDir, "--dry-run"}, tc.args...)
			var stdout, stderr bytes.Buffer
			code := Run(args, &stdout, &stderr, nil, &fakeRunner{})
			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tc.want)
			}
			if _, err := os.Stat(runDir); !os.IsNotExist(err) {
				t.Fatalf("run dir %s exists after fail-fast rejection; stat err = %v", runDir, err)
			}
		})
	}
}

func TestFlowPhaseFailureReason(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		stdout string
		code   int
		want   string
	}{
		{
			name:   "json error field wins over raw json lines",
			stdout: "{\n  \"ok\": false,\n  \"error\": \"killfeed events has 2 cues; clip clip-001 has 0 detected cues\"\n}",
			code:   1,
			want:   "killfeed events has 2 cues; clip clip-001 has 0 detected cues",
		},
		{
			name:   "stderr line wins when present",
			stderr: "error: plan file not found: plan.json",
			stdout: `{"ok":false,"error":"unused"}`,
			code:   3,
			want:   "error: plan file not found: plan.json",
		},
		{
			name:   "plain stdout falls back to first line",
			stdout: "something went wrong\nmore detail",
			code:   1,
			want:   "something went wrong",
		},
		{
			name: "empty output falls back to exit code",
			code: 4,
			want: "exit 4",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := flowPhaseFailureReason(tc.stderr, tc.stdout, tc.code)
			if got != tc.want {
				t.Fatalf("flowPhaseFailureReason() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFlowsRunUnknownFlowIsRejected(t *testing.T) {
	ws := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run([]string{"zv", "flows", "run", "movie", "--run-dir", ws, "--dry-run"}, &stdout, &stderr, nil, &fakeRunner{})
	if code != exitInvalidArgs {
		t.Fatalf("code = %d, want %d", code, exitInvalidArgs)
	}
	if !strings.Contains(stderr.String(), `unknown flow "movie"`) {
		t.Fatalf("stderr = %q, want unknown flow error", stderr.String())
	}
}

func TestFlowsRunDemoSkipsGatesAndCaptureWithoutDemo(t *testing.T) {
	ws := t.TempDir()
	plan := writeStageContractPlan(t, ws)
	runDir := filepath.Join(ws, "run")

	report, code, stderr := runFlowsRunInProcess(t, "demo", "--killplan", plan, "--run-dir", runDir, "--dry-run", "--format", "json")
	if code != exitSuccess {
		t.Fatalf("code = %d, want %d\nstderr: %s", code, exitSuccess, stderr)
	}
	if !report.OK || report.Flow != "demo" {
		t.Fatalf("report = %#v", report)
	}

	want := []struct {
		phase    string
		skipped  bool
		executed bool
		dryRun   bool
	}{
		{"parse", true, false, false},
		{"moments", false, true, false},
		{"creative-brief", true, false, false},
		{"select", false, true, false},
		{"record", true, false, false},
		{"shorts-render", true, false, false},
		{"thumbnail-selection", true, false, false},
	}
	if len(report.Phases) != len(want) {
		t.Fatalf("phases = %d, want %d: %#v", len(report.Phases), len(want), report.Phases)
	}
	for i, w := range want {
		got := report.Phases[i]
		if got.Phase != w.phase || got.Skipped != w.skipped || got.Executed != w.executed || got.DryRun != w.dryRun {
			t.Fatalf("phase %d = %#v, want %s skipped=%v executed=%v dryRun=%v", i, got, w.phase, w.skipped, w.executed, w.dryRun)
		}
	}

	// The creative-brief and thumbnail gates report as creative gates, not failures.
	for _, id := range []string{"creative-brief", "thumbnail-selection"} {
		phase, _ := phaseByName(report, id)
		if !strings.Contains(phase.Reason, "creative gate") {
			t.Fatalf("gate %s reason = %q, want a creative gate", id, phase.Reason)
		}
	}
	// moments and select actually wrote their chainable artifacts.
	for _, name := range []string{"moments.json", "selected-plan.json"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Fatalf("expected %s written: %v", name, err)
		}
	}
}

func TestFlowsRunStopsOnFirstFailure(t *testing.T) {
	ws := t.TempDir()
	runDir := filepath.Join(ws, "run")
	missing := filepath.Join(ws, "does-not-exist.json")

	report, code, _ := runFlowsRunInProcess(t, "demo", "--killplan", missing, "--run-dir", runDir, "--dry-run", "--format", "json")
	if code == exitSuccess {
		t.Fatalf("code = %d, want failure", code)
	}
	if report.OK {
		t.Fatalf("report.OK = true, want false: %#v", report)
	}

	parse, _ := phaseByName(report, "parse")
	if !parse.Skipped {
		t.Fatalf("parse = %#v, want skipped (killplan supplied)", parse)
	}
	moments, _ := phaseByName(report, "moments")
	if moments.OK || moments.Skipped {
		t.Fatalf("moments = %#v, want a hard failure", moments)
	}
	// moments is a real (non-dry-run) stage: it ran and failed, so the report
	// must show executed:true (the command ran and may have partially mutated),
	// not hide the failure by leaving executed:false.
	if !moments.Executed {
		t.Fatalf("moments.Executed = false after a real stage ran and failed, want true: %#v", moments)
	}
	// Every phase after the failure is reported as not run.
	for _, id := range []string{"select", "record", "shorts-render", "thumbnail-selection"} {
		phase, ok := phaseByName(report, id)
		if !ok {
			t.Fatalf("phase %s missing from report", id)
		}
		if phase.OK || !phase.Skipped || !strings.Contains(phase.Reason, "not run") {
			t.Fatalf("phase %s = %#v, want not-run", id, phase)
		}
	}
}

func TestFlowsRunDemoDryRunChainsCaptureAndRender(t *testing.T) {
	t.Parallel()
	exe := buildDelegatedBinaries(t)

	ws := t.TempDir()
	plan := writeStageContractPlan(t, ws)
	demo := filepath.Join(ws, "demo.dem")
	if err := os.WriteFile(demo, []byte("dummy demo"), 0o600); err != nil {
		t.Fatalf("write demo fixture: %v", err)
	}
	runDir := filepath.Join(ws, "run")

	cmd := exec.Command(exe, "flows", "run", "demo",
		"--demo", demo, "--killplan", plan, "--run-dir", runDir, "--dry-run", "--format", "json")
	cmd.Dir = ws
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("flows run demo: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	var report flowRunReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v\n%s", err, stdout.String())
	}
	if !report.OK {
		t.Fatalf("report.OK = false: %s", stdout.String())
	}

	// Capture and render run as dry runs (not executed) but succeed and chain.
	record, _ := phaseByName(report, "record")
	if !record.OK || !record.DryRun || record.Executed {
		t.Fatalf("record phase = %#v, want a successful dry run", record)
	}
	render, _ := phaseByName(report, "shorts-render")
	if !render.OK || !render.DryRun || render.Executed {
		t.Fatalf("shorts-render phase = %#v, want a successful dry run", render)
	}

	// Each stage's artifact is the literal input the next stage consumed.
	chain := []string{
		filepath.Join(runDir, "moments.json"),
		filepath.Join(runDir, "selected-plan.json"),
		filepath.Join(runDir, "recording", "recording-result.json"),
	}
	for _, path := range chain {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected chained artifact %s: %v", path, err)
		}
	}
	if len(record.Outputs) == 0 || record.Outputs[0] != filepath.Join(runDir, "recording", "recording-result.json") {
		t.Fatalf("record outputs = %#v, want the recording-result.json path", record.Outputs)
	}

	// The chain is verified through the reported argv, not just file existence:
	// record must consume the plan select persisted, and shorts render must
	// consume the recording result record produced.
	selectedPlan := filepath.Join(runDir, "selected-plan.json")
	if got, ok := flagValue(record.Argv, "--killplan"); !ok || got != selectedPlan {
		t.Fatalf("record --killplan = %q (ok=%v), want the selected plan %q", got, ok, selectedPlan)
	}
	recordingResult := filepath.Join(runDir, "recording", "recording-result.json")
	if got, ok := flagValue(render.Argv, "--recording-result"); !ok || got != recordingResult {
		t.Fatalf("shorts-render --recording-result = %q (ok=%v), want %q", got, ok, recordingResult)
	}
}
