package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type fakeSubcommandCall struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
}

type fakeRunner struct {
	name string
	args []string
	err  error
}

func TestMain(m *testing.M) {
	if os.Getenv("ZV_FAKE_SUBCOMMAND") == "1" {
		os.Exit(runFakeSubcommand())
	}
	os.Exit(m.Run())
}

func runFakeSubcommand() int {
	logPath := os.Getenv("ZV_FAKE_SUBCOMMAND_LOG")
	if logPath == "" {
		return 1
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 1
	}
	defer f.Close()
	call := fakeSubcommandCall{
		Executable: filepath.Base(os.Args[0]),
		Args:       append([]string(nil), os.Args[1:]...),
	}
	if err := json.NewEncoder(f).Encode(call); err != nil {
		return 1
	}
	if os.Getenv("ZV_FAKE_SUBCOMMAND_ECHO_STDIO") == "1" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return 1
		}
		fmt.Fprintf(os.Stdout, "fake stdout: %s", string(b))
		fmt.Fprintf(os.Stderr, "fake stderr: %s", filepath.Base(os.Args[0]))
	}
	return 0
}

func (f *fakeRunner) Run(_ context.Context, name string, args []string, _ io.Reader, _ io.Writer, _ io.Writer) error {
	f.name = name
	f.args = append([]string(nil), args...)
	return f.err
}

func TestRunDemoParseDelegatesToParser(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{"zv", "demo", "parse", "--demo", "inferno.dem", "--steamid", "123", "--out", "plan.json"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := runner.name, executableName("zv-parser"); got != want && !strings.HasSuffix(got, want) {
		t.Fatalf("runner.name = %q, want suffix %q", got, want)
	}
	if got, want := strings.Join(runner.args, " "), "parse --demo inferno.dem --steamid 123 --out plan.json"; got != want {
		t.Fatalf("runner.args = %q, want %q", got, want)
	}
}

func TestRunUtilityAuditDelegatesToParser(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{"zv", "utility", "audit", "--plan", "plan.json", "--lineup-catalog", "data/lineups", "--out", "utility-audit.csv", "--format", "json"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := runner.name, executableName("zv-parser"); got != want && !strings.HasSuffix(got, want) {
		t.Fatalf("runner.name = %q, want suffix %q", got, want)
	}
	if got, want := strings.Join(runner.args, " "), "utility-audit --plan plan.json --lineup-catalog data/lineups --out utility-audit.csv --format json"; got != want {
		t.Fatalf("runner.args = %q, want %q", got, want)
	}
}

func TestRunShortsRenderDelegatesToEditor(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{"zv", "shorts", "render", "--recording-result", "recording.json", "--out", "shorts"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := runner.name, executableName("zv-editor"); got != want && !strings.HasSuffix(got, want) {
		t.Fatalf("runner.name = %q, want suffix %q", got, want)
	}
	if got, want := strings.Join(runner.args, " "), "--recording-result recording.json --out shorts"; got != want {
		t.Fatalf("runner.args = %q, want %q", got, want)
	}
}

func TestRunServeDelegatesToOrchestrator(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{"zv", "serve"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got, want := runner.name, executableName("zv-orchestrator"); got != want && !strings.HasSuffix(got, want) {
		t.Fatalf("runner.name = %q, want suffix %q", got, want)
	}
	if got, want := len(runner.args), 0; got != want {
		t.Fatalf("runner.args len = %d, want %d: %#v", got, want, runner.args)
	}
}

func TestRunUnknownCommandReturnsInvalidArgs(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder

	code := Run([]string{"zv", "wat"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), `unknown command "wat"`) {
		t.Fatalf("stderr = %q, want unknown command", stderr.String())
	}
}

func TestRunDelegateReportsRunnerError(t *testing.T) {
	runner := &fakeRunner{err: errors.New("boom")}
	var stdout, stderr strings.Builder

	code := Run([]string{"zv", "parser", "--help"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitUnexpected; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr = %q, want runner error", stderr.String())
	}
}

func TestRunCanonicalGroupHelpReturnsSuccess(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{name: "demo", argv: []string{"zv", "demo", "--help"}, want: demoUsage},
		{name: "utility", argv: []string{"zv", "utility", "--help"}, want: utilityUsage},
		{name: "compose", argv: []string{"zv", "compose", "--help"}, want: composeUsage},
		{name: "shorts", argv: []string{"zv", "shorts", "--help"}, want: shortsUsage},
		{name: "analysis", argv: []string{"zv", "analysis", "--help"}, want: analysisUsage},
		{name: "gallery", argv: []string{"zv", "gallery", "--help"}, want: galleryUsage},
		{name: "skills", argv: []string{"zv", "skills", "--help"}, want: skillsUsage},
		{name: "workflows", argv: []string{"zv", "workflows", "--help"}, want: workflowsUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			var stdout, stderr strings.Builder

			code := Run(tt.argv, &stdout, &stderr, nil, runner)

			if got, want := code, exitSuccess; got != want {
				t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
			}
			if got, want := stdout.String(), tt.want; got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
			if got := runner.name; got != "" {
				t.Fatalf("runner.name = %q, want no delegated command", got)
			}
		})
	}
}

func TestRunHelpDocumentsLegacyPassThroughs(t *testing.T) {
	var stdout, stderr strings.Builder

	code := Run([]string{"zv", "--help"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	for _, passThrough := range legacyPassThroughs() {
		want := legacyPassThroughUsageLine(passThrough)
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help output = %q, want legacy pass-through %q", stdout.String(), want)
		}
	}
}

func TestRunLegacyPassThroughsDelegate(t *testing.T) {
	for _, passThrough := range legacyPassThroughs() {
		t.Run(passThrough.Command, func(t *testing.T) {
			runner := &fakeRunner{}
			var stdout, stderr strings.Builder

			code := Run([]string{"zv", passThrough.Command, "--sentinel"}, &stdout, &stderr, nil, runner)

			if got, want := code, exitSuccess; got != want {
				t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
			}
			if got, want := runner.name, executableName(passThrough.Binary); got != want && !strings.HasSuffix(got, want) {
				t.Fatalf("runner.name = %q, want suffix %q", got, want)
			}
			if got, want := strings.Join(runner.args, " "), "--sentinel"; got != want {
				t.Fatalf("runner.args = %q, want %q", got, want)
			}
		})
	}
}

func TestFindLegacyPassThroughCoversCatalog(t *testing.T) {
	for _, passThrough := range legacyPassThroughs() {
		t.Run(passThrough.Command, func(t *testing.T) {
			got, ok := findLegacyPassThrough(passThrough.Command)
			if !ok {
				t.Fatalf("findLegacyPassThrough(%q) ok = false, want true", passThrough.Command)
			}
			if got != passThrough {
				t.Fatalf("findLegacyPassThrough(%q) = %#v, want %#v", passThrough.Command, got, passThrough)
			}
		})
	}
	if got, ok := findLegacyPassThrough("missing"); ok {
		t.Fatalf("findLegacyPassThrough missing = %#v, true; want false", got)
	}
}

func TestRunSkillsListDiscoversRepoLocalSkills(t *testing.T) {
	tempDir := t.TempDir()
	writeSkill(t, tempDir, "alpha", "Alpha workflow")
	writeSkill(t, tempDir, "bravo", "Bravo workflow")
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "list"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{
		"alpha\tAlpha workflow\n",
		"bravo\tBravo workflow\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("stdout = %q, want line %q", got, want)
		}
	}
}

func TestRunSkillsListJSON(t *testing.T) {
	tempDir := t.TempDir()
	writeSkill(t, tempDir, "alpha", "Alpha workflow")
	writeSkill(t, tempDir, "bravo", "Bravo workflow")
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "list", "--format", "json"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	var skills []skillInfo
	if err := json.Unmarshal([]byte(stdout.String()), &skills); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if got, want := len(skills), 2; got != want {
		t.Fatalf("skills len = %d, want %d", got, want)
	}
	if got, want := skills[0].Name, "alpha"; got != want {
		t.Fatalf("skills[0].Name = %q, want %q", got, want)
	}
	if got := stdout.String(); strings.Contains(got, `"path"`) {
		t.Fatalf("stdout = %q, want no local path in skill JSON", got)
	}
}

func TestRunSkillsShowPrintsSkillMarkdown(t *testing.T) {
	tempDir := t.TempDir()
	writeSkill(t, tempDir, "alpha", "Alpha workflow")
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "show", "alpha"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	for _, want := range []string{
		"name: alpha",
		"description: \"Alpha workflow\"",
		"# alpha",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunSkillsShowJSONPrintsSkillDetail(t *testing.T) {
	tempDir := t.TempDir()
	writeSkill(t, tempDir, "alpha", "Alpha workflow")
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "show", "--format", "json", "alpha"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	var detail skillDetail
	if err := json.Unmarshal([]byte(stdout.String()), &detail); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if got, want := detail.Name, "alpha"; got != want {
		t.Fatalf("detail.Name = %q, want %q", got, want)
	}
	if got, want := detail.Description, "Alpha workflow"; got != want {
		t.Fatalf("detail.Description = %q, want %q", got, want)
	}
	if !strings.Contains(detail.Body, "# alpha") {
		t.Fatalf("detail.Body = %q, want skill body", detail.Body)
	}
}

func TestRunSkillsShowMissingReturnsInvalidArgs(t *testing.T) {
	tempDir := t.TempDir()
	writeSkill(t, tempDir, "alpha", "Alpha workflow")
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "show", "missing"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), "skill not found: missing") {
		t.Fatalf("stderr = %q, want missing skill error", stderr.String())
	}
}

func TestRunSkillsCheckAcceptsStandardSkillWorkflows(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"# alpha",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		`.\bin\zv.exe workflows run record -- --killplan plan.json --demo demo.dem --out recording --dry-run`,
		`.\bin\zv.exe workflows run shorts-render -- --recording-result recording\recording-result.json --out shorts`,
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		`.\bin\zv.exe workflows run serve`,
		`.\bin\zv.exe skills show alpha --format json`,
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK: 1 skills checked") {
		t.Fatalf("stdout = %q, want OK count", stdout.String())
	}
}

func TestRunSkillsCheckJSONReportsSuccess(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "check", "--format=json"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if !result.OK {
		t.Fatalf("result.OK = false, want true: %#v", result)
	}
	if got, want := result.SkillsChecked, 1; got != want {
		t.Fatalf("result.SkillsChecked = %d, want %d", got, want)
	}
	if got, want := len(result.Issues), 0; got != want {
		t.Fatalf("issues len = %d, want %d: %#v", got, want, result.Issues)
	}
}

func TestRunSkillsCheckRejectsSkillWithoutWorkflowRun(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe demo parse --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), "does not document a cataloged workflow run command") {
		t.Fatalf("stderr = %q, want missing workflow run error", stderr.String())
	}
}

func TestRunSkillsCheckRejectsKnownSkillMissingRequiredWorkflowRun(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "zackvideo-cs2-utility-shorts", strings.Join([]string{
		"---",
		"name: zackvideo-cs2-utility-shorts",
		`description: "Create CS2 utility Shorts from a demo with ZackVideo."`,
		"---",
		"",
		"# ZackVideo CS2 Utility Shorts",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		`.\bin\zv.exe workflows run shorts-render -- --recording-result recording\recording-result.json --out shorts`,
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := "missing required workflow run record"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunSkillsCheckRejectsDirectWorkflowCommand(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe demo parse --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	want := `uses direct workflow command "demo parse"; use "zv workflows run demo-parse"`
	if !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunSkillsCheckRejectsDuplicateSkillNames(t *testing.T) {
	tempDir := t.TempDir()
	body := strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n")
	writeSkillBody(t, tempDir, "alpha-one", body)
	writeSkillBody(t, tempDir, "alpha-two", body)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "check", "--format", "json"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	if got, want := result.SkillsChecked, 2; got != want {
		t.Fatalf("result.SkillsChecked = %d, want %d", got, want)
	}
	if !hasIssueContaining(result.Issues, `duplicate skill name "alpha"`) {
		t.Fatalf("issues = %#v, want duplicate skill name issue", result.Issues)
	}
}

func TestRunSkillsCheckRejectsInvalidSkillNameContract(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		metaName string
		want     string
	}{
		{
			name:     "not slug",
			dir:      "alpha",
			metaName: "Alpha Skill",
			want:     "skill name must be a lowercase slug",
		},
		{
			name:     "directory mismatch",
			dir:      "alpha-dir",
			metaName: "alpha",
			want:     `skill name "alpha" must match directory "alpha-dir"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			writeSkillBody(t, tempDir, tt.dir, strings.Join([]string{
				"---",
				"name: " + tt.metaName,
				`description: "Alpha workflow"`,
				"---",
				"",
				"```powershell",
				`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
				"```",
				"",
			}, "\n"))
			withWorkingDir(t, tempDir)

			var stdout, stderr strings.Builder
			code := Run([]string{"zv", "skills", "check", "--format", "json"}, &stdout, &stderr, nil, &fakeRunner{})

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("code = %d, want %d", got, want)
			}
			var result skillCheckResult
			if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
				t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
			}
			if result.OK {
				t.Fatalf("result.OK = true, want false")
			}
			if !hasIssueContaining(result.Issues, tt.want) {
				t.Fatalf("issues = %#v, want %q", result.Issues, tt.want)
			}
		})
	}
}

func TestRunSkillsCheckJSONReportsIssues(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv-parser.exe parse --demo demo.dem --steamid 76561198000000000`,
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "check", "--format", "json"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	if got, want := result.SkillsChecked, 1; got != want {
		t.Fatalf("result.SkillsChecked = %d, want %d", got, want)
	}
	if got := len(result.Issues); got == 0 {
		t.Fatalf("issues len = 0, want issues")
	}
}

func TestRunSkillsCheckRejectsLegacyDirectBinary(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv-parser.exe parse --demo demo.dem --steamid 76561198000000000`,
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		"does not document the unified zv CLI",
		`documents legacy direct binary .\bin\zv-parser`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunSkillsCheckRejectsLegacyPassthroughCommand(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe parser parse --demo demo.dem --steamid 76561198000000000`,
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), `uses non-standard zv command "parser"`) {
		t.Fatalf("stderr = %q, want non-standard command error", stderr.String())
	}
}

func TestRunSkillsCheckRejectsNonCanonicalNestedWorkflowCommand(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantErr string
	}{
		{
			name:    "demo",
			line:    `.\bin\zv.exe demo inspect --demo demo.dem`,
			wantErr: `uses non-standard zv command "demo"; expected "demo parse" or "demo players"`,
		},
		{
			name:    "shorts",
			line:    `.\bin\zv.exe shorts export --recording-result recording.json`,
			wantErr: `uses non-standard zv command "shorts"; expected "shorts render"`,
		},
		{
			name:    "gallery",
			line:    `.\bin\zv.exe gallery view --path shorts\publish\index.html`,
			wantErr: `uses non-standard zv command "gallery"; expected "gallery open"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
				"---",
				"name: alpha",
				`description: "Alpha workflow"`,
				"---",
				"",
				"```powershell",
				tt.line,
				"```",
				"",
			}, "\n"))
			withWorkingDir(t, tempDir)

			var stdout, stderr strings.Builder
			code := Run([]string{"zv", "skills", "check"}, &stdout, &stderr, nil, &fakeRunner{})

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("code = %d, want %d", got, want)
			}
			if !strings.Contains(stderr.String(), tt.wantErr) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tt.wantErr)
			}
		})
	}
}

func TestRunSkillsCheckRejectsWorkflowCommandMissingRequiredFlags(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantErr string
	}{
		{
			name:    "demo parse",
			line:    `.\bin\zv.exe demo parse --demo demo.dem`,
			wantErr: `missing required flags --steamid, --out for "demo parse"`,
		},
		{
			name:    "utility audit",
			line:    `.\bin\zv.exe utility audit --out utility-audit.csv`,
			wantErr: `missing required flags --plan, --lineup-catalog for "utility audit"`,
		},
		{
			name:    "record",
			line:    `.\bin\zv.exe record --killplan plan.json --demo demo.dem`,
			wantErr: `missing required flag --out for "record"`,
		},
		{
			name:    "shorts render",
			line:    `.\bin\zv.exe shorts render --out shorts`,
			wantErr: `missing required flag --recording-result for "shorts render"`,
		},
		{
			name:    "gallery open",
			line:    `.\bin\zv.exe gallery open`,
			wantErr: `missing required flag --path for "gallery open"`,
		},
		{
			name:    "skills show",
			line:    `.\bin\zv.exe skills show`,
			wantErr: `missing skill name for "skills show"`,
		},
		{
			name:    "skills show extra",
			line:    `.\bin\zv.exe skills show alpha extra`,
			wantErr: `unexpected extra args for "skills show"`,
		},
		{
			name:    "skills show unsupported format",
			line:    `.\bin\zv.exe skills show alpha --format yaml`,
			wantErr: `unsupported format "yaml"`,
		},
		{
			name:    "skills show duplicate format",
			line:    `.\bin\zv.exe skills show alpha --format json --format text`,
			wantErr: `duplicate flag --format`,
		},
		{
			name:    "skills list extra",
			line:    `.\bin\zv.exe skills list alpha`,
			wantErr: `unexpected extra args for "skills list"`,
		},
		{
			name:    "serve",
			line:    `.\bin\zv.exe serve --unexpected`,
			wantErr: `unexpected extra args for "serve"`,
		},
		{
			name:    "workflows show missing workflow",
			line:    `.\bin\zv.exe workflows show missing-workflow`,
			wantErr: `unknown workflow name "missing-workflow" for "workflows show"`,
		},
		{
			name:    "workflows run missing workflow",
			line:    `.\bin\zv.exe workflows run missing-workflow`,
			wantErr: `unknown workflow name "missing-workflow" for "workflows run"`,
		},
		{
			name:    "workflows run missing forwarded args",
			line:    `.\bin\zv.exe workflows run demo-parse`,
			wantErr: `missing required flags --demo, --steamid, --out for "demo parse"`,
		},
		{
			name:    "workflows run missing separator",
			line:    `.\bin\zv.exe workflows run demo-parse --demo demo.dem --steamid 76561198000000000`,
			wantErr: `missing "--" separator before forwarded args for "workflows run"`,
		},
		{
			name:    "workflows run forwarded flags",
			line:    `.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --out plan.json`,
			wantErr: `missing required flag --steamid for "demo parse"`,
		},
		{
			name:    "workflows check duplicate format",
			line:    `.\bin\zv.exe workflows check --format json --format text`,
			wantErr: `duplicate flag --format`,
		},
		{
			name:    "unquoted path with spaces",
			line:    `.\bin\zv.exe workflows run pipeline -- --killplan plan.json --demo demo.dem --out pipeline --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe`,
			wantErr: `unexpected positional arg "2\\game\\bin\\win64\\cs2.exe" for "pipeline"; quote paths with spaces`,
		},
		{
			name:    "duplicate forwarded flag",
			line:    `.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --demo other.dem --steamid 76561198000000000 --out plan.json`,
			wantErr: `duplicate flag --demo for "demo parse"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
				"---",
				"name: alpha",
				`description: "Alpha workflow"`,
				"---",
				"",
				"```powershell",
				tt.line,
				"```",
				"",
			}, "\n"))
			withWorkingDir(t, tempDir)

			var stdout, stderr strings.Builder
			code := Run([]string{"zv", "skills", "check"}, &stdout, &stderr, nil, &fakeRunner{})

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("code = %d, want %d", got, want)
			}
			if !strings.Contains(stderr.String(), tt.wantErr) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tt.wantErr)
			}
		})
	}
}

func TestRunSkillsCheckAcceptsCanonicalHelpCommands(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --help`,
		`.\bin\zv.exe workflows run demo-players -- --help`,
		`.\bin\zv.exe workflows run utility-audit -- --help`,
		`.\bin\zv.exe workflows run record -- --help`,
		`.\bin\zv.exe workflows run compose-final -- --help`,
		`.\bin\zv.exe workflows run shorts-render -- --help`,
		`.\bin\zv.exe workflows run analysis-tactical-data -- --help`,
		`.\bin\zv.exe workflows run analysis-viewer -- --help`,
		`.\bin\zv.exe workflows run gallery-open -- --help`,
		`.\bin\zv.exe workflows run serve`,
		`.\bin\zv.exe skills list --help`,
		`.\bin\zv.exe skills show --help`,
		`.\bin\zv.exe workflows run skills-check -- --help`,
		`.\bin\zv.exe workflows list --help`,
		`.\bin\zv.exe workflows show --help`,
		`.\bin\zv.exe workflows run workflows-check -- --help`,
		`.\bin\zv.exe workflows run project-check -- --help`,
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK: 1 skills checked") {
		t.Fatalf("stdout = %q, want OK", stdout.String())
	}
}

func TestRunSkillsCheckReadsMultilinePowerShellCommands(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		".\\bin\\zv.exe workflows run demo-parse -- `",
		"  --demo demo.dem `",
		"  --steamid 76561198000000000 `",
		"  --out plan.json",
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "skills", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OK: 1 skills checked") {
		t.Fatalf("stdout = %q, want OK count", stdout.String())
	}
}

func TestSkillCommandLinesReadsMultilineBashCommands(t *testing.T) {
	body := strings.Join([]string{
		"```bash",
		"./bin/zv demo parse \\",
		"  --demo demo.dem \\",
		"  --steamid 76561198000000000 \\",
		"  --out plan.json",
		"```",
	}, "\n")

	lines := skillCommandLines(body)

	if got, want := len(lines), 1; got != want {
		t.Fatalf("lines len = %d, want %d: %#v", got, want, lines)
	}
	want := "./bin/zv demo parse --demo demo.dem --steamid 76561198000000000 --out plan.json"
	if got := lines[0]; got != want {
		t.Fatalf("line = %q, want %q", got, want)
	}
}

func TestSkillCommandLinesReadsWindowsBinZVCommands(t *testing.T) {
	body := strings.Join([]string{
		"bin\\zv serve",
		`Fail "Start bin\zv serve first"`,
		".\\bin\\zv workflows check",
		"",
	}, "\n")

	lines := skillCommandLines(body)

	want := []string{
		`bin\zv serve`,
		`.\bin\zv workflows check`,
	}
	if got := strings.Join(lines, "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("lines = %#v, want %#v", lines, want)
	}
	for _, line := range lines {
		if _, ok := skillCommand(line); !ok {
			t.Fatalf("skillCommand(%q) did not parse", line)
		}
	}
}

func TestSkillCommandLinesReadsDocumentedCommandPrefixes(t *testing.T) {
	body := strings.Join([]string{
		"- ./bin/zv check",
		"* ./bin/zv workflows list",
		"$ ./bin/zv skills list",
		"PS> .\\bin\\zv workflows check",
		"go build -o bin/zv ./cmd/zv",
		"",
	}, "\n")

	lines := skillCommandLines(body)

	want := []string{
		"- ./bin/zv check",
		"* ./bin/zv workflows list",
		"$ ./bin/zv skills list",
		`PS> .\bin\zv workflows check`,
	}
	if got := strings.Join(lines, "\n"); got != strings.Join(want, "\n") {
		t.Fatalf("lines = %#v, want %#v", lines, want)
	}

	tests := []struct {
		line string
		want []string
	}{
		{line: lines[0], want: []string{"check"}},
		{line: lines[1], want: []string{"workflows", "list"}},
		{line: lines[2], want: []string{"skills", "list"}},
		{line: lines[3], want: []string{"workflows", "check"}},
	}
	for _, tt := range tests {
		got, ok := skillCommand(tt.line)
		if !ok {
			t.Fatalf("skillCommand(%q) did not parse", tt.line)
		}
		if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
			t.Fatalf("skillCommand(%q) = %#v, want %#v", tt.line, got, tt.want)
		}
	}
}

func TestRunWorkflowsCheckAcceptsStandardRepoContracts(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	want := fmt.Sprintf("OK: 1 skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked", len(workflowCatalog()), len(workflowDocs()), len(agentPromptWrapperFixtures()))
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want workflow OK count", stdout.String())
	}
}

func TestRunCheckAcceptsStandardRepoContracts(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	want := fmt.Sprintf("OK: 1 skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked", len(workflowCatalog()), len(workflowDocs()), len(agentPromptWrapperFixtures()))
	if !strings.Contains(stdout.String(), want) {
		t.Fatalf("stdout = %q, want workflow OK count", stdout.String())
	}
}

func TestRunWorkflowsCheckJSONReportsSuccess(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check", "--format=json"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	var result workflowCheckResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if !result.OK {
		t.Fatalf("result.OK = false, want true: %#v", result)
	}
	if got, want := result.SkillsChecked, 1; got != want {
		t.Fatalf("result.SkillsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowsChecked, len(workflowCatalog()); got != want {
		t.Fatalf("result.WorkflowsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowDocsChecked, len(workflowDocs()); got != want {
		t.Fatalf("result.WorkflowDocsChecked = %d, want %d", got, want)
	}
	if got, want := result.AgentPromptWrappersChecked, len(agentPromptWrapperFixtures()); got != want {
		t.Fatalf("result.AgentPromptWrappersChecked = %d, want %d", got, want)
	}
	if got, want := len(result.Issues), 0; got != want {
		t.Fatalf("issues len = %d, want %d: %#v", got, want, result.Issues)
	}
}

func TestRunCheckJSONReportsSuccess(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "check", "--format=json"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	var result workflowCheckResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if !result.OK {
		t.Fatalf("result.OK = false, want true: %#v", result)
	}
	if got, want := result.WorkflowsChecked, len(workflowCatalog()); got != want {
		t.Fatalf("result.WorkflowsChecked = %d, want %d", got, want)
	}
	if got, want := result.AgentPromptWrappersChecked, len(agentPromptWrapperFixtures()); got != want {
		t.Fatalf("result.AgentPromptWrappersChecked = %d, want %d", got, want)
	}
}

func TestRunWorkflowsCheckRejectsLegacyWorkflowDocs(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{
			name:    "parser",
			command: "./bin/zv-parser parse --demo demo.dem --steamid 76561198000000000",
			want:    "README.md: documents legacy direct command ./bin/zv-parser",
		},
		{
			name:    "demo players",
			command: "./bin/zv-demo-players --demo demo.dem",
			want:    "README.md: documents legacy direct command ./bin/zv-demo-players",
		},
		{
			name:    "analysis viewer",
			command: "./bin/zv-analysis-viewer --input data/analysis.json",
			want:    "README.md: documents legacy direct command ./bin/zv-analysis-viewer",
		},
		{
			name:    "windows bin path",
			command: `bin\zv-recorder --killplan plan.json --demo demo.dem --out recording`,
			want:    `README.md: documents legacy direct command bin\zv-recorder`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
				"---",
				"name: alpha",
				`description: "Alpha workflow"`,
				"---",
				"",
				"```powershell",
				`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
				"```",
				"",
			}, "\n"))
			writeWorkflowDocs(t, tempDir)
			appendFile(t, filepath.Join(tempDir, "README.md"), "\n"+tt.command+"\n")
			withWorkingDir(t, tempDir)

			var stdout, stderr strings.Builder
			code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("code = %d, want %d", got, want)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("stderr = %q, want %q", stderr.String(), tt.want)
			}
		})
	}
}

func TestLegacyBinaryListsCoverCommandEntrypoints(t *testing.T) {
	root := repoRoot(t)
	cmdEntries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		t.Fatalf("read cmd dir: %v", err)
	}
	skillBinaries := stringSet(legacySkillBinaries())
	workflowBinaries := stringSet(legacyWorkflowCommands())
	for _, entry := range cmdEntries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "zv-") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, "cmd", entry.Name(), "main.go")); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			t.Fatalf("stat %s main.go: %v", entry.Name(), err)
		}
		for _, want := range []string{`.\bin\` + entry.Name(), `bin\` + entry.Name(), `./bin/` + entry.Name()} {
			if _, ok := skillBinaries[want]; !ok {
				t.Fatalf("legacySkillBinaries() does not include %q", want)
			}
			if _, ok := workflowBinaries[want]; !ok {
				t.Fatalf("legacyWorkflowCommands() does not include %q", want)
			}
		}
	}
}

func TestRunWorkflowsCheckRejectsDiscoveredLegacyCommandEntrypoint(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "cmd", "zv-new-flow", "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(tempDir, "Makefile"), strings.Join([]string{
		"build:",
		"\tgo build -o bin/zv ./cmd/zv",
		"\tgo build -o bin/zv-new-flow ./cmd/zv-new-flow",
		"",
		"test:",
		"\tgo run ./cmd/zv check",
		"\tgo run ./cmd/zv workflows check",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(tempDir, "scripts", "build.ps1"), strings.Join([]string{
		`$commands = @(`,
		`    "zv",`,
		`    "zv-new-flow"`,
		`)`,
		`foreach ($name in $commands) {`,
		`    $out = Join-Path $binDir "$name.exe"`,
		`    $pkg = "./cmd/$name"`,
		`    & go build -o $out $pkg`,
		`}`,
		"",
	}, "\n"))
	appendFile(t, filepath.Join(tempDir, "README.md"), "\n./bin/zv-new-flow --demo demo.dem\n")
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := "README.md: documents legacy direct command ./bin/zv-new-flow"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsReadmeMissingDiscoveredRepoSkill(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	b, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	body := strings.ReplaceAll(string(b), "alpha", "")
	writeFile(t, readmePath, body)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := "README.md: missing repo skill alpha"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsReadmeMissingDiscoveredRepoSkillShowCommand(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"# ZackVideo",
		"",
		"Repo-local skills currently exposed through `zv skills`:",
		"",
		"- `alpha`",
		"",
		"```bash",
		"./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv demo players --demo testdata/foo.dem",
		"./bin/zv utility audit --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv",
		"./bin/zv record --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv compose final --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4",
		"./bin/zv shorts render --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts",
		"./bin/zv analysis tactical-data --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000",
		"./bin/zv analysis view --json data/analysis/MarcusN1-deaths.json",
		"./bin/zv gallery open --path data/runs/run-004/shorts/publish/index.html",
		"./bin/zv check",
		"./bin/zv check --format json",
		"./bin/zv serve",
		"./bin/zv pipeline --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/pipeline --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv skills check",
		"./bin/zv skills list --format json",
		"./bin/zv skills show beta --format json",
		"./bin/zv skills check --format json",
		"./bin/zv workflows list",
		"./bin/zv workflows list --format json",
		"./bin/zv workflows show demo-parse",
		"./bin/zv workflows show demo-parse --format json",
		"./bin/zv workflows show demo-players",
		"./bin/zv workflows show demo-players --format json",
		"./bin/zv workflows show utility-audit",
		"./bin/zv workflows show utility-audit --format json",
		"./bin/zv workflows show record",
		"./bin/zv workflows show record --format json",
		"./bin/zv workflows show compose-final",
		"./bin/zv workflows show compose-final --format json",
		"./bin/zv workflows show shorts-render",
		"./bin/zv workflows show shorts-render --format json",
		"./bin/zv workflows show analysis-tactical-data",
		"./bin/zv workflows show analysis-tactical-data --format json",
		"./bin/zv workflows show analysis-viewer",
		"./bin/zv workflows show analysis-viewer --format json",
		"./bin/zv workflows show gallery-open",
		"./bin/zv workflows show gallery-open --format json",
		"./bin/zv workflows show serve",
		"./bin/zv workflows show serve --format json",
		"./bin/zv workflows show pipeline",
		"./bin/zv workflows show pipeline --format json",
		"./bin/zv workflows show skills-check",
		"./bin/zv workflows show skills-check --format json",
		"./bin/zv workflows show workflows-check",
		"./bin/zv workflows show workflows-check --format json",
		"./bin/zv workflows show project-check",
		"./bin/zv workflows show project-check --format json",
		"./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv workflows run demo-players -- --demo testdata/foo.dem",
		"./bin/zv workflows run utility-audit -- --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv",
		"./bin/zv workflows run record -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv workflows run compose-final -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4",
		"./bin/zv workflows run shorts-render -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts",
		"./bin/zv workflows run analysis-tactical-data -- --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000",
		"./bin/zv workflows run analysis-viewer -- --json data/analysis/MarcusN1-deaths.json",
		"./bin/zv workflows run gallery-open -- --path data/runs/run-004/shorts/publish/index.html",
		"./bin/zv workflows run serve",
		"./bin/zv workflows run pipeline -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/pipeline --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv workflows run skills-check",
		"./bin/zv workflows run skills-check -- --format json",
		"./bin/zv workflows run workflows-check",
		"./bin/zv workflows run workflows-check -- --format json",
		"./bin/zv workflows run project-check",
		"./bin/zv workflows run project-check -- --format json",
		"./bin/zv workflows check",
		"./bin/zv workflows check --format json",
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := "README.md: missing skill show command ./bin/zv skills show alpha"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsReadmeMissingDiscoveredRepoSkillShowJSONCommand(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	body := readFileString(t, readmePath)
	body = strings.ReplaceAll(body, "./bin/zv skills show alpha --format json\n", "")
	writeFile(t, readmePath, body)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := "README.md: missing skill show command ./bin/zv skills show alpha --format json"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsUndocumentedDiscoveredSkill(t *testing.T) {
	tempDir := t.TempDir()
	skillBody := func(name string) string {
		return strings.Join([]string{
			"---",
			"name: " + name,
			`description: "Workflow skill"`,
			"---",
			"",
			"```powershell",
			`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
			"```",
			"",
		}, "\n")
	}
	writeSkillBody(t, tempDir, "alpha", skillBody("alpha"))
	writeSkillBody(t, tempDir, "bravo", skillBody("bravo"))
	writeWorkflowDocs(t, tempDir)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		"README.md: missing repo skill bravo",
		".codex/README.md: missing repo skill bravo",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunWorkflowsCheckRejectsNonCanonicalWorkflowDocCommands(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv parser parse --demo demo.dem --steamid 76561198000000000",
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), `README.md: uses non-standard zv command "parser"`) {
		t.Fatalf("stderr = %q, want noncanonical doc command error", stderr.String())
	}
}

func TestRunWorkflowsCheckRejectsLegacySmokeRealScript(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "scripts", "smoke-real.ps1"), `Fail "Start bin\zv-orchestrator first"`)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), `scripts/smoke-real.ps1: documents legacy direct command bin\zv-orchestrator`) {
		t.Fatalf("stderr = %q, want smoke-real legacy command error", stderr.String())
	}
	if !strings.Contains(stderr.String(), `scripts/smoke-real.ps1: missing canonical workflow command bin\zv serve`) {
		t.Fatalf("stderr = %q, want smoke-real missing canonical command error", stderr.String())
	}
}

func TestRunWorkflowsCheckRejectsNonCanonicalWindowsBinZVScript(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "scripts", "smoke-real.ps1"), `bin\zv serve --unexpected`)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), `scripts/smoke-real.ps1: unexpected extra args for "serve" in "bin\\zv serve --unexpected"`) {
		t.Fatalf("stderr = %q, want bin\\zv canonical validation error", stderr.String())
	}
}

func TestRunWorkflowsCheckRejectsBrokenParserSmokeScript(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "scripts", "smoke.sh"), strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`BASE="${ZV_BASE_URL:-http://localhost:8080}"`,
		`curl -fsS "$BASE/api/jobs"`,
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		`scripts/smoke.sh: missing canonical workflow command /api/jobs/$ID`,
		`scripts/smoke.sh: missing canonical workflow command /api/jobs/$ID/plan`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunWorkflowsCheckRejectsBuildLoopWithoutUnifiedCLI(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe demo parse --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "Makefile"), strings.Join([]string{
		"build:",
		"\tgo build -o bin/zv-parser ./cmd/zv-parser",
		"",
		"test:",
		"\tgo test ./... -count=1",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		"Makefile: missing canonical workflow command go build -o bin/zv ./cmd/zv",
		"Makefile: missing canonical workflow command go run ./cmd/zv check",
		"Makefile: missing canonical workflow command go run ./cmd/zv workflows check",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunWorkflowsCheckRejectsMissingCommandBuildTarget(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "cmd", "zv", "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(tempDir, "cmd", "zv-analysis-viewer", "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(tempDir, "Makefile"), strings.Join([]string{
		"build:",
		"\tgo build -o bin/zv ./cmd/zv",
		"",
		"check:",
		"\tgo run ./cmd/zv check",
		"",
		"workflows-check:",
		"\tgo run ./cmd/zv workflows check",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(tempDir, "scripts", "build.ps1"), strings.Join([]string{
		`$commands = @(`,
		`    "zv"`,
		`)`,
		`foreach ($name in $commands) {`,
		`    $out = Join-Path $binDir "$name.exe"`,
		`    $pkg = "./cmd/$name"`,
		`    & go build -o $out $pkg`,
		`}`,
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		"Makefile: missing command build target go build -o bin/zv-analysis-viewer ./cmd/zv-analysis-viewer",
		`scripts/build.ps1: missing command build entry "zv-analysis-viewer"`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunWorkflowsCheckRejectsStaleCommandBuildTarget(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "cmd", "zv", "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(tempDir, "Makefile"), strings.Join([]string{
		"build:",
		"\tgo build -o bin/zv ./cmd/zv",
		"\tgo build -o bin/zv-old ./cmd/zv-old",
		"",
		"check:",
		"\tgo run ./cmd/zv check",
		"",
		"workflows-check:",
		"\tgo run ./cmd/zv workflows check",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(tempDir, "scripts", "build.ps1"), strings.Join([]string{
		`$commands = @(`,
		`    "zv",`,
		`    "zv-old"`,
		`)`,
		`foreach ($name in $commands) {`,
		`    $out = Join-Path $binDir "$name.exe"`,
		`    $pkg = "./cmd/$name"`,
		`    & go build -o $out $pkg`,
		`}`,
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		"Makefile: stale command build target go build -o bin/zv-old ./cmd/zv-old",
		`scripts/build.ps1: stale command build entry "zv-old"`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunWorkflowsCheckRejectsUncoveredCommandEntrypoint(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "cmd", "zv", "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(tempDir, "cmd", "zv-new-flow", "main.go"), "package main\n\nfunc main() {}\n")
	writeFile(t, filepath.Join(tempDir, "Makefile"), strings.Join([]string{
		"build:",
		"\tgo build -o bin/zv ./cmd/zv",
		"\tgo build -o bin/zv-new-flow ./cmd/zv-new-flow",
		"",
		"check:",
		"\tgo run ./cmd/zv check",
		"",
		"workflows-check:",
		"\tgo run ./cmd/zv workflows check",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(tempDir, "scripts", "build.ps1"), strings.Join([]string{
		`$commands = @(`,
		`    "zv",`,
		`    "zv-new-flow"`,
		`)`,
		`foreach ($name in $commands) {`,
		`    $out = Join-Path $binDir "$name.exe"`,
		`    $pkg = "./cmd/$name"`,
		`    & go build -o $out $pkg`,
		`}`,
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := "cmd/zv-new-flow: command entrypoint is not covered by zv workflows or legacy pass-throughs"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsAgentInstructionHLAEDrift(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "CLAUDE.md"), strings.Join([]string{
		"# Claude",
		"",
		"```bash",
		`CLAUDE_DRY_RUN=1 scripts/claude-run.sh .claude/commands/zv-tdd.md "custom prompt run"`,
		`scripts/claude-zv-tdd.sh "implement a behavior change"`,
		`scripts/claude-zv-bugfix.sh "fix a bug with a regression test"`,
		`scripts/claude-zv-pr-ready.sh`,
		`scripts/go-gate.sh --no-format`,
		`scripts/go-gate.sh --race`,
		`scripts/go-gate.sh --security`,
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		`CLAUDE.md: missing canonical workflow command C:\HLAE-2.190.1\HLAE.exe`,
		`CLAUDE.md: missing canonical workflow command C:\HLAE\HLAE.exe`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunWorkflowsCheckRejectsPartialCodexPromptChecks(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, ".codex", "prompts", "go-tdd.md"), strings.Join([]string{
		"# Prompt",
		"",
		"Run `go test ./... -count=1`.",
		"Run `go vet ./...`.",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		`.codex/prompts/go-tdd.md: missing standard gate guidance "scripts/go-gate.sh --no-format"`,
		`.codex/prompts/go-tdd.md: missing standard gate guidance "` + "`zv check`" + `"`,
		`.codex/prompts/go-tdd.md: uses partial check "` + "`go test ./... -count=1`" + `"; use scripts/go-gate.sh`,
		`.codex/prompts/go-tdd.md: uses partial check "` + "`go vet ./...`" + `"; use scripts/go-gate.sh`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunWorkflowsCheckRejectsPartialClaudeCommandChecks(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, ".claude", "commands", "zv-tdd.md"), strings.Join([]string{
		"# Command",
		"",
		"Run `go test ./... -count=1`.",
		"Run `go vet ./...`.",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		`.claude/commands/zv-tdd.md: missing standard gate guidance "scripts/go-gate.sh --no-format"`,
		`.claude/commands/zv-tdd.md: missing standard gate guidance "` + "`zv check`" + `"`,
		`.claude/commands/zv-tdd.md: uses partial check "` + "`go test ./... -count=1`" + `"; use scripts/go-gate.sh`,
		`.claude/commands/zv-tdd.md: uses partial check "` + "`go vet ./...`" + `"; use scripts/go-gate.sh`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunWorkflowsCheckRejectsMissingCodexRunner(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	if err := os.Remove(filepath.Join(tempDir, "scripts", "codex-run.sh")); err != nil {
		t.Fatalf("remove codex runner: %v", err)
	}
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := `scripts/codex-run.sh: missing codex prompt runner`; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsMissingClaudeRunner(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	if err := os.Remove(filepath.Join(tempDir, "scripts", "claude-run.sh")); err != nil {
		t.Fatalf("remove claude runner: %v", err)
	}
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := `scripts/claude-run.sh: missing claude prompt runner`; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsNonStandardAgentShellScript(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "scripts", "codex-go-tdd.sh"), strings.Join([]string{
		`root="$(git rev-parse --show-toplevel)"`,
		`exec "$root/scripts/codex-run.sh" .codex/prompts/go-tdd.md "$@"`,
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		"scripts/codex-go-tdd.sh: missing standard bash shebang",
		"scripts/codex-go-tdd.sh: missing strict shell mode set -euo pipefail",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunWorkflowsCheckRejectsClaudeCommandWithoutWrapper(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	if err := os.Remove(filepath.Join(tempDir, "scripts", "claude-zv-tdd.sh")); err != nil {
		t.Fatalf("remove claude wrapper: %v", err)
	}
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := `.claude/commands/zv-tdd.md: has no claude wrapper`; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsUndocumentedClaudeReviewerAgent(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, ".claude", "README.md"), strings.Join([]string{
		"# Claude",
		"",
		"```bash",
		"scripts/claude-run.sh",
		"scripts/claude-zv-tdd.sh",
		"scripts/claude-zv-bugfix.sh",
		"scripts/claude-zv-pr-ready.sh",
		"scripts/claude-zv-artifact-audit.sh",
		"scripts/claude-zv-media-change.sh",
		"scripts/claude-zv-parser-change.sh",
		"scripts/claude-zv-plan.sh",
		"scripts/claude-zv-toolchain-diagnose.sh",
		"scripts/claude-zv-worker-api-change.sh",
		"```",
		"",
		"```text",
		"@go-readability-reviewer review the current diff",
		"@go-test-reviewer review the tests in this diff",
		"@go-concurrency-reviewer review shared-state changes",
		"@zv-media-pipeline-reviewer review FFmpeg/rendering changes",
		"```",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := `.claude/README.md: does not document reviewer agent @go-security-reviewer`; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsRelaxedClaudeOperationRule(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, ".claude", "rules", "zackvideo-operations.md"), strings.Join([]string{
		"# ZackVideo operational rule",
		"",
		"Safe by default:",
		"",
		"- `scripts/go-gate.sh --no-format` after targeted tests pass",
		"",
		"Ask first:",
		"",
		"- HLAE/CS2 launch or real capture",
		"- Docker compose and database migrations",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		`.claude/rules/zackvideo-operations.md: missing claude rule guidance "cleanup scripts that delete artifacts"`,
		`.claude/rules/zackvideo-operations.md: missing claude rule guidance "Never add generated ` + "`.mp4`" + `"`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunWorkflowsCheckRejectsPermissiveClaudeSettings(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, ".claude", "settings.json"), strings.Join([]string{
		"{",
		`  "permissions": {`,
		`    "allow": [`,
		`      "Read",`,
		`      "Edit",`,
		`      "Write",`,
		`      "Bash(git status*)",`,
		`      "Bash(git diff*)",`,
		`      "Bash(go test*)",`,
		`      "Bash(go vet*)",`,
		`      "Bash(gofmt*)",`,
		`      "Bash(scripts/go-format-changed.sh*)",`,
		`      "Bash(scripts/go-gate.sh*)",`,
		`      "Bash(scripts/go-tools-check.sh*)",`,
		`      "Bash(docker*)"`,
		`    ],`,
		`    "ask": [`,
		`      "Bash(go mod tidy*)",`,
		`      "Bash(go get*)",`,
		`      "Bash(go install*)",`,
		`      "Bash(git commit*)",`,
		`      "Bash(git push*)",`,
		`      "Bash(git reset*)",`,
		`      "Bash(git clean*)",`,
		`      "Bash(docker compose*)",`,
		`      "Bash(ffmpeg*)",`,
		`      "Bash(powershell.exe*)",`,
		`      "Bash(pwsh*)",`,
		`      "Bash(scripts/build.ps1*)",`,
		`      "Bash(scripts/cleanup-artifacts.ps1*)",`,
		`      "Bash(scripts/audit-security-performance.ps1*)"`,
		`    ],`,
		`    "deny": [`,
		`      "Read(**/.env)",`,
		`      "Read(**/*id_rsa*)",`,
		`      "Read(**/*id_ed25519*)",`,
		`      "Read(**/*secret*)",`,
		`      "Read(**/*token*)",`,
		`      "Bash(rm -rf *)",`,
		`      "Bash(git reset --hard*)",`,
		`      "Bash(git push --force*)"`,
		`    ]`,
		`  }`,
		"}",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		`.claude/settings.json: missing ask permission "Bash(docker*)"`,
		`.claude/settings.json: missing deny permission "Read(.env)"`,
		`.claude/settings.json: dangerous permission "Bash(docker*)" must not be allowed`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunWorkflowsCheckJSONReportsIssues(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe demo parse --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), "\n./bin/zv-parser parse --demo demo.dem --steamid 76561198000000000\n")
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check", "--format", "json"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	var result workflowCheckResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	if got, want := result.SkillsChecked, 1; got != want {
		t.Fatalf("result.SkillsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowsChecked, len(workflowCatalog()); got != want {
		t.Fatalf("result.WorkflowsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowDocsChecked, len(workflowDocs()); got != want {
		t.Fatalf("result.WorkflowDocsChecked = %d, want %d", got, want)
	}
	if got, want := result.AgentPromptWrappersChecked, len(agentPromptWrapperFixtures()); got != want {
		t.Fatalf("result.AgentPromptWrappersChecked = %d, want %d", got, want)
	}
	if got := len(result.Issues); got == 0 {
		t.Fatalf("issues len = 0, want issues")
	}
}

func TestRunWorkflowsCheckIncludesSkillsContract(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv-parser.exe parse --demo demo.dem --steamid 76561198000000000`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), "does not document the unified zv CLI") {
		t.Fatalf("stderr = %q, want skill contract error", stderr.String())
	}
}

func TestRunWorkflowsListShowsCanonicalCatalog(t *testing.T) {
	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "list"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	for _, want := range []string{
		"demo-parse\tParse a CS2 demo",
		"compose-final\tConcatenate recorded segment clips into a final MP4.",
		"shorts-render\tRender vertical Shorts",
		"analysis-tactical-data\tExport sampled tactical data",
		"analysis-viewer\tServe a local analysis review UI.",
		"workflows-check\tValidate skills, workflow catalog, and current workflow docs.",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunWorkflowsShowPrintsCanonicalCommand(t *testing.T) {
	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "show", "shorts-render"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	for _, want := range []string{
		"shorts-render",
		"Render vertical Shorts",
		"command: zv shorts render --recording-result <recording-result.json> --out <shorts-dir>",
		"run_command: zv workflows run shorts-render",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestRunWorkflowsListJSON(t *testing.T) {
	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "list", "--format", "json"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	var workflows []workflowInfo
	if err := json.Unmarshal([]byte(stdout.String()), &workflows); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	var rawWorkflows []map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &rawWorkflows); err != nil {
		t.Fatalf("unmarshal raw stdout: %v\n%s", err, stdout.String())
	}
	if got, want := len(workflows), len(workflowCatalog()); got != want {
		t.Fatalf("workflow count = %d, want %d", got, want)
	}
	if got, want := len(rawWorkflows), len(workflowCatalog()); got != want {
		t.Fatalf("raw workflow count = %d, want %d", got, want)
	}
	if got, want := workflows[0].Name, "demo-parse"; got != want {
		t.Fatalf("workflows[0].Name = %q, want %q", got, want)
	}
	if got, want := workflows[0].Command, "zv demo parse --demo <demo.dem> --steamid <SteamID64> --out <plan.json>"; got != want {
		t.Fatalf("workflows[0].Command = %q, want %q", got, want)
	}
	if got, want := workflows[0].RunCommand, "zv workflows run demo-parse"; got != want {
		t.Fatalf("workflows[0].RunCommand = %q, want %q", got, want)
	}
	for i, workflow := range workflows {
		if workflow.RunCommand == "" {
			t.Fatalf("workflows[%d].RunCommand is empty", i)
		}
		if got, want := workflow.RunCommand, workflowRunCommand(workflow.Name); got != want {
			t.Fatalf("workflows[%d].RunCommand = %q, want %q", i, got, want)
		}
		if _, ok := rawWorkflows[i]["RunArgs"]; ok {
			t.Fatalf("raw workflows[%d] leaked RunArgs: %#v", i, rawWorkflows[i])
		}
		if _, ok := rawWorkflows[i]["run_args"]; ok {
			t.Fatalf("raw workflows[%d] leaked run_args: %#v", i, rawWorkflows[i])
		}
		if _, ok := rawWorkflows[i]["run_command"]; !ok {
			t.Fatalf("raw workflows[%d] missing run_command: %#v", i, rawWorkflows[i])
		}
	}
}

func TestRunWorkflowsShowJSON(t *testing.T) {
	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "show", "shorts-render", "--format=json"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	var workflow workflowInfo
	if err := json.Unmarshal([]byte(stdout.String()), &workflow); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	var rawWorkflow map[string]any
	if err := json.Unmarshal([]byte(stdout.String()), &rawWorkflow); err != nil {
		t.Fatalf("unmarshal raw stdout: %v\n%s", err, stdout.String())
	}
	if got, want := workflow.Name, "shorts-render"; got != want {
		t.Fatalf("workflow.Name = %q, want %q", got, want)
	}
	if got, want := workflow.Command, "zv shorts render --recording-result <recording-result.json> --out <shorts-dir>"; got != want {
		t.Fatalf("workflow.Command = %q, want %q", got, want)
	}
	if got, want := workflow.RunCommand, "zv workflows run shorts-render"; got != want {
		t.Fatalf("workflow.RunCommand = %q, want %q", got, want)
	}
	if _, ok := rawWorkflow["RunArgs"]; ok {
		t.Fatalf("raw workflow leaked RunArgs: %#v", rawWorkflow)
	}
	if _, ok := rawWorkflow["run_args"]; ok {
		t.Fatalf("raw workflow leaked run_args: %#v", rawWorkflow)
	}
	if _, ok := rawWorkflow["run_command"]; !ok {
		t.Fatalf("raw workflow missing run_command: %#v", rawWorkflow)
	}
}

func TestRunWorkflowsShowMissingReturnsInvalidArgs(t *testing.T) {
	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "show", "missing"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), "workflow not found: missing") {
		t.Fatalf("stderr = %q, want missing workflow error", stderr.String())
	}
}

func TestRunWorkflowsRunDelegatesCatalogWorkflow(t *testing.T) {
	tests := []struct {
		name           string
		argv           []string
		wantExecutable string
		wantArgs       []string
	}{
		{
			name:           "demo parse",
			argv:           []string{"zv", "workflows", "run", "demo-parse", "--", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json"},
		},
		{
			name:           "utility audit",
			argv:           []string{"zv", "workflows", "run", "utility-audit", "--", "--plan", "plan.json", "--lineup-catalog", "data/lineups", "--out", "utility-audit.csv"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"utility-audit", "--plan", "plan.json", "--lineup-catalog", "data/lineups", "--out", "utility-audit.csv"},
		},
		{
			name:           "record",
			argv:           []string{"zv", "workflows", "run", "record", "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run"},
			wantExecutable: executableName("zv-recorder"),
			wantArgs:       []string{"--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run"},
		},
		{
			name:           "shorts render",
			argv:           []string{"zv", "workflows", "run", "shorts-render", "--", "--recording-result", "recording-result.json", "--out", "shorts"},
			wantExecutable: executableName("zv-editor"),
			wantArgs:       []string{"--recording-result", "recording-result.json", "--out", "shorts"},
		},
		{
			name:           "pipeline",
			argv:           []string{"zv", "workflows", "run", "pipeline", "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"},
			wantExecutable: executableName("zv-pipeline"),
			wantArgs:       []string{"--killplan", "plan.json", "--demo", "inferno.dem", "--out", "pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"},
		},
		{
			name:           "serve",
			argv:           []string{"zv", "workflows", "run", "serve"},
			wantExecutable: executableName("zv-orchestrator"),
			wantArgs:       nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &fakeRunner{}
			var stdout, stderr strings.Builder
			code := Run(tt.argv, &stdout, &stderr, nil, runner)

			if got, want := code, exitSuccess; got != want {
				t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
			}
			if got, want := runner.name, tt.wantExecutable; got != want && !strings.HasSuffix(got, want) {
				t.Fatalf("runner.name = %q, want suffix %q", got, want)
			}
			if got, want := strings.Join(runner.args, "\x00"), strings.Join(tt.wantArgs, "\x00"); got != want {
				t.Fatalf("runner.args = %#v, want %#v", runner.args, tt.wantArgs)
			}
		})
	}
}

func TestRunWorkflowsRunMissingReturnsInvalidArgs(t *testing.T) {
	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "run"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if !strings.Contains(stderr.String(), "usage: zv workflows run <name>") {
		t.Fatalf("stderr = %q, want run usage", stderr.String())
	}
}

func TestRunWorkflowsRunRejectsForwardedArgsWithoutSeparator(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "run", "demo-parse", "--demo", "inferno.dem", "--steamid", "76561198000000000"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if got := runner.name; got != "" {
		t.Fatalf("runner.name = %q, want no delegated command", got)
	}
	if !strings.Contains(stderr.String(), `missing "--" separator before forwarded args`) {
		t.Fatalf("stderr = %q, want missing separator error", stderr.String())
	}
}

func TestRunWorkflowsRunRejectsMissingRequiredForwardedArgs(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "run", "demo-parse"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if got := runner.name; got != "" {
		t.Fatalf("runner.name = %q, want no delegated command", got)
	}
	if !strings.Contains(stderr.String(), `missing required flags --demo, --steamid, --out for "demo parse"`) {
		t.Fatalf("stderr = %q, want missing required flags error", stderr.String())
	}
}

func TestRunWorkflowsRunExecutesInternalCheckWorkflows(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	withWorkingDir(t, tempDir)

	tests := []struct {
		name string
		want string
	}{
		{name: "skills-check", want: "OK: 1 skills checked"},
		{name: "workflows-check", want: fmt.Sprintf("OK: 1 skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked", len(workflowCatalog()), len(workflowDocs()), len(agentPromptWrapperFixtures()))},
		{name: "project-check", want: fmt.Sprintf("OK: 1 skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked", len(workflowCatalog()), len(workflowDocs()), len(agentPromptWrapperFixtures()))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			code := Run([]string{"zv", "workflows", "run", tt.name}, &stdout, &stderr, nil, &fakeRunner{})

			if got, want := code, exitSuccess; got != want {
				t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tt.want)
			}
		})
	}
}

func TestRunWorkflowsRunForwardsJSONFormatToInternalChecks(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	withWorkingDir(t, tempDir)

	tests := []struct {
		name string
		argv []string
		want string
	}{
		{
			name: "skills-check",
			argv: []string{"zv", "workflows", "run", "skills-check", "--", "--format", "json"},
			want: `"skills_checked": 1`,
		},
		{
			name: "workflows-check",
			argv: []string{"zv", "workflows", "run", "workflows-check", "--", "--format", "json"},
			want: fmt.Sprintf(`"workflows_checked": %d`, len(workflowCatalog())),
		},
		{
			name: "project-check",
			argv: []string{"zv", "workflows", "run", "project-check", "--", "--format", "json"},
			want: fmt.Sprintf(`"workflows_checked": %d`, len(workflowCatalog())),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			code := Run(tt.argv, &stdout, &stderr, nil, &fakeRunner{})

			if got, want := code, exitSuccess; got != want {
				t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
			}
			if !strings.Contains(stdout.String(), `"ok": true`) {
				t.Fatalf("stdout = %q, want ok json", stdout.String())
			}
			if !strings.Contains(stdout.String(), tt.want) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tt.want)
			}
		})
	}
}

func TestValidateWorkflowCatalogRejectsNonCanonicalCommands(t *testing.T) {
	workflows := []workflowInfo{
		{
			Name:        "demo-parse",
			Description: "Parse a demo.",
			Command:     "zv demo inspect --demo <demo.dem>",
		},
		{
			Name:        "demo-parse",
			Description: "Duplicate name.",
			Command:     "zv demo parse --demo <demo.dem> --steamid <SteamID64>",
		},
		{
			Name:    "missing-description",
			Command: "zv skills check",
		},
		{
			Name:        "legacy",
			Description: "Legacy command.",
			Command:     "zv-parser parse --demo <demo.dem>",
		},
		{
			Name:        "Bad Workflow",
			Description: "Invalid name.",
			Command:     "zv skills check",
			RunCommand:  "zv workflows run Bad Workflow",
			RunArgs:     []string{"skills", "check"},
		},
		{
			Name:        "empty-command",
			Description: "No command.",
			RunCommand:  "zv workflows run empty-command",
		},
		{
			Name:        "mismatched-run-args",
			Description: "Run args do not match the documented command.",
			Command:     "zv demo parse --demo <demo.dem> --steamid <SteamID64> --out <plan.json>",
			RunCommand:  "zv workflows run mismatched-run-args",
			RunArgs:     []string{"record"},
		},
		{
			Name:        "partial-run-args",
			Description: "Run args must include the full command stem.",
			Command:     "zv demo parse --demo <demo.dem> --steamid <SteamID64> --out <plan.json>",
			RunCommand:  "zv workflows run partial-run-args",
			RunArgs:     []string{"demo"},
		},
		{
			Name:        "mismatched-run-command",
			Description: "Run command does not match the workflow name.",
			Command:     "zv skills check",
			RunCommand:  "zv workflows run other-workflow",
			RunArgs:     []string{"skills", "check"},
		},
		{
			Name:        "duplicate-run-args",
			Description: "Run args must uniquely identify a workflow.",
			Command:     "zv skills check",
			RunCommand:  "zv workflows run duplicate-run-args",
			RunArgs:     []string{"skills", "check"},
		},
	}

	issues := validateWorkflowCatalog(workflows)
	for _, want := range []string{
		`workflow:demo-parse: workflow command is not canonical: uses non-standard zv command "demo"; expected "demo parse" or "demo players"`,
		`workflow:demo-parse: workflow run command must be "zv workflows run demo-parse"`,
		"workflow:demo-parse: duplicate workflow name",
		`workflow:missing-description: workflow run command must be "zv workflows run missing-description"`,
		"workflow:missing-description: missing workflow description",
		`workflow:legacy: workflow run command must be "zv workflows run legacy"`,
		"workflow:legacy: workflow command must start with zv: zv-parser parse --demo <demo.dem>",
		"workflow:Bad Workflow: workflow name must be a lowercase slug",
		"workflow:empty-command: missing workflow command",
		`workflow:mismatched-run-args: workflow run args "record" do not match workflow command "zv demo parse --demo <demo.dem> --steamid <SteamID64> --out <plan.json>"`,
		`workflow:partial-run-args: workflow run args "demo" do not match workflow command "zv demo parse --demo <demo.dem> --steamid <SteamID64> --out <plan.json>"`,
		`workflow:mismatched-run-command: workflow run command must be "zv workflows run mismatched-run-command"`,
		`workflow:mismatched-run-command: duplicate workflow run args "skills check" also used by workflow "Bad Workflow"`,
		`workflow:duplicate-run-args: duplicate workflow run args "skills check" also used by workflow "Bad Workflow"`,
	} {
		if !hasIssue(issues, want) {
			t.Fatalf("issues = %#v, want %q", issues, want)
		}
	}
}

func TestValidateInternalCheckWorkflowsCoversCatalog(t *testing.T) {
	issues := validateInternalCheckWorkflows(workflowCatalog())
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidateInternalCheckWorkflowsRejectsDrift(t *testing.T) {
	workflows := []workflowInfo{
		{
			Name:    "skills-check",
			Command: "zv workflows check",
			RunArgs: []string{"workflows", "check"},
		},
		{
			Name:    "project-check",
			Command: "zv check",
			RunArgs: []string{"check"},
		},
	}

	issues := validateInternalCheckWorkflows(workflows)

	for _, want := range []string{
		`workflow:skills-check: internal check workflow command must be "zv skills check"`,
		`workflow:skills-check: internal check workflow run args must be "skills check"`,
		"workflow:workflows-check: missing internal check workflow",
	} {
		if !hasIssue(issues, want) {
			t.Fatalf("issues = %#v, want %q", issues, want)
		}
	}
}

func TestValidateWorkflowDelegationCoverageCoversCatalog(t *testing.T) {
	issues := validateWorkflowDelegationCoverage(workflowCatalog())
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidateWorkflowDelegationCoverageRejectsUnmappedWorkflow(t *testing.T) {
	workflows := []workflowInfo{
		{
			Name:    "new-flow",
			Command: "zv new flow",
			RunArgs: []string{"new", "flow"},
		},
	}

	issues := validateWorkflowDelegationCoverage(workflows)

	if !hasIssue(issues, `workflow:new-flow: workflow run args "new flow" are not mapped to a delegated command`) {
		t.Fatalf("issues = %#v, want unmapped delegation", issues)
	}
}

func TestValidateSkillWorkflowRequirementCatalogCoversCurrentRequirements(t *testing.T) {
	issues := validateSkillWorkflowRequirementCatalog(workflowCatalog(), skillWorkflowRequirementMap())
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidateSkillWorkflowRequirementCatalogRejectsUnknownWorkflow(t *testing.T) {
	requirements := map[string][]string{
		"zackvideo-cs2-utility-shorts": {"demo-parse", "missing-workflow"},
	}

	issues := validateSkillWorkflowRequirementCatalog(workflowCatalog(), requirements)

	if !hasIssue(issues, `skill:zackvideo-cs2-utility-shorts: required workflow "missing-workflow" is not cataloged`) {
		t.Fatalf("issues = %#v, want missing workflow requirement", issues)
	}
}

func TestValidateSkillWorkflowRequirementSkillsCoversCurrentRequirements(t *testing.T) {
	skills := []skillInfo{
		{Name: "zackvideo-cs2-utility-shorts"},
		{Name: "zackvideo-lineup-audit"},
		{Name: "zackvideo-youtube-shorts-publish"},
	}

	issues := validateSkillWorkflowRequirementSkills(skills, skillWorkflowRequirementMap())

	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidateSkillWorkflowRequirementSkillsRejectsMissingSkill(t *testing.T) {
	skills := []skillInfo{{Name: "alpha"}}
	requirements := map[string][]string{
		"alpha":   {"demo-parse"},
		"missing": {"demo-parse"},
	}

	issues := validateSkillWorkflowRequirementSkills(skills, requirements)

	if !hasIssue(issues, "skill:missing: workflow requirements reference missing repo skill") {
		t.Fatalf("issues = %#v, want missing skill requirement", issues)
	}
}

func TestValidateSkillWorkflowRequirementSkillsRejectsZackVideoSkillWithoutRequirements(t *testing.T) {
	skills := []skillInfo{
		{Name: "zackvideo-new-skill"},
		{Name: "zackvideo-cs2-utility-shorts"},
	}
	requirements := map[string][]string{
		"zackvideo-cs2-utility-shorts": {"demo-parse"},
	}

	issues := validateSkillWorkflowRequirementSkills(skills, requirements)

	if !hasIssue(issues, "skill:zackvideo-new-skill: missing workflow requirements for repo skill") {
		t.Fatalf("issues = %#v, want missing workflow requirements", issues)
	}
}

func TestValidateUsageCoverageCoversWorkflowCatalog(t *testing.T) {
	issues := validateUsageCoverage(workflowCatalog(), usage)
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidateUsageCoverageRejectsWorkflowMissingFromMainUsage(t *testing.T) {
	usageWithoutCompose := strings.ReplaceAll(usage, "  zv compose final [zv-composer flags]\n", "")

	issues := validateUsageCoverage(workflowCatalog(), usageWithoutCompose)

	if !hasIssue(issues, `workflow:compose-final: workflow command "zv compose final" is not covered by main usage`) {
		t.Fatalf("issues = %#v, want missing compose usage coverage", issues)
	}
}

func TestValidateGroupUsageCoverageCoversWorkflowCatalog(t *testing.T) {
	issues := validateGroupUsageCoverage(workflowCatalog(), groupUsageTexts())
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidateGroupUsageCoverageRejectsWorkflowMissingFromGroupUsage(t *testing.T) {
	groupUsages := groupUsageTexts()
	groupUsages["analysis"] = `usage: zv analysis tactical-data [zv-tactical-data flags]
`

	issues := validateGroupUsageCoverage(workflowCatalog(), groupUsages)

	if !hasIssue(issues, `workflow:analysis-viewer: workflow command "zv analysis view" is not covered by analysis usage`) {
		t.Fatalf("issues = %#v, want missing analysis view usage coverage", issues)
	}
}

func TestValidateLegacyPassThroughUsageCoversSupportedPassThroughs(t *testing.T) {
	issues := validateLegacyPassThroughUsage(usage)
	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidateLegacyPassThroughUsageRejectsMissingPassThrough(t *testing.T) {
	usageWithoutAnalysisViewer := strings.ReplaceAll(usage, "  zv analysis-viewer [zv-analysis-viewer args]\n", "")

	issues := validateLegacyPassThroughUsage(usageWithoutAnalysisViewer)

	if !hasIssue(issues, `usage: legacy pass-through "zv analysis-viewer [zv-analysis-viewer args]" is not covered by main usage`) {
		t.Fatalf("issues = %#v, want missing analysis-viewer pass-through coverage", issues)
	}
}

func TestValidateLegacyPassThroughEntrypointsCoversCurrentSurface(t *testing.T) {
	commands := append([]string{"zv"}, defaultLegacyCommandEntrypointNames()...)

	issues := validateLegacyPassThroughEntrypoints(commands)

	if len(issues) != 0 {
		t.Fatalf("issues = %#v, want none", issues)
	}
}

func TestValidateLegacyPassThroughEntrypointsRejectsMissingEntrypoint(t *testing.T) {
	commands := []string{
		"zv",
		"zv-parser",
		"zv-editor",
		"zv-recorder",
		"zv-composer",
		"zv-orchestrator",
		"zv-analysis-viewer",
		"zv-pipeline",
	}

	issues := validateLegacyPassThroughEntrypoints(commands)

	if !hasIssue(issues, "pass-through:tactical-data: legacy pass-through references missing command entrypoint zv-tactical-data") {
		t.Fatalf("issues = %#v, want missing tactical-data pass-through entrypoint", issues)
	}
}

func TestValidateWorkflowDocCoverageRejectsUndocumentedCatalogWorkflow(t *testing.T) {
	workflows := []workflowInfo{
		{
			Name:        "demo-parse",
			Description: "Parse a demo.",
			Command:     "zv demo parse --demo <demo.dem> --steamid <SteamID64> --out <plan.json>",
			RunCommand:  "zv workflows run demo-parse",
			RunArgs:     []string{"demo", "parse"},
		},
		{
			Name:        "pipeline",
			Description: "Run the local pipeline.",
			Command:     "zv pipeline --killplan <plan.json> --demo <demo.dem> --out <pipeline-dir> --hlae <HLAE.exe> --cs2 <cs2.exe>",
			RunCommand:  "zv workflows run pipeline",
			RunArgs:     []string{"pipeline"},
		},
	}
	docs := []workflowDoc{
		{
			Path: "README.md",
			Required: []string{
				"./bin/zv demo parse",
				"./bin/zv workflows run demo-parse",
			},
		},
	}

	issues := validateWorkflowDocCoverage(workflows, docs)

	if !hasIssue(issues, "workflow:pipeline: workflow command ./bin/zv pipeline is not covered by workflow docs") {
		t.Fatalf("issues = %#v, want missing pipeline coverage", issues)
	}
	if !hasIssue(issues, "workflow:pipeline: workflow run command ./bin/zv workflows run pipeline is not covered by workflow docs") {
		t.Fatalf("issues = %#v, want missing pipeline run coverage", issues)
	}
}

func TestValidateWorkflowDocRequiredWorkflowRunsRejectsUnknownRun(t *testing.T) {
	workflows := []workflowInfo{
		{
			Name:        "demo-parse",
			Description: "Parse a demo.",
			Command:     "zv demo parse --demo <demo.dem> --steamid <SteamID64> --out <plan.json>",
			RunCommand:  "zv workflows run demo-parse",
			RunArgs:     []string{"demo", "parse"},
		},
	}
	docs := []workflowDoc{
		{
			Path: "README.md",
			Required: []string{
				"./bin/zv workflows run demo-parse",
				"./bin/zv workflows run missing-workflow",
				"./bin/zv check",
			},
		},
	}

	issues := validateWorkflowDocRequiredWorkflowRuns(workflows, docs)

	if !hasIssue(issues, `README.md: required workflow run "missing-workflow" is not cataloged`) {
		t.Fatalf("issues = %#v, want unknown required workflow run", issues)
	}
}

func TestValidateWorkflowDocShowCoverageRejectsMissingCatalogShowCommands(t *testing.T) {
	workflows := []workflowInfo{
		{
			Name:        "demo-parse",
			Description: "Parse a demo.",
			Command:     "zv demo parse --demo <demo.dem> --steamid <SteamID64> --out <plan.json>",
			RunCommand:  "zv workflows run demo-parse",
			RunArgs:     []string{"demo", "parse"},
		},
		{
			Name:        "pipeline",
			Description: "Run the local pipeline.",
			Command:     "zv pipeline --killplan <plan.json> --demo <demo.dem> --out <pipeline-dir> --hlae <HLAE.exe> --cs2 <cs2.exe>",
			RunCommand:  "zv workflows run pipeline",
			RunArgs:     []string{"pipeline"},
		},
	}
	docs := []workflowDoc{
		{
			Path:              "README.md",
			RequiredWorkflows: true,
			Body: strings.Join([]string{
				"```bash",
				"./bin/zv workflows show demo-parse",
				"./bin/zv workflows show demo-parse --format json",
				"```",
			}, "\n"),
		},
	}

	issues := validateWorkflowDocShowCoverage(workflows, docs)

	if !hasIssue(issues, "README.md: missing workflow show command ./bin/zv workflows show pipeline") {
		t.Fatalf("issues = %#v, want missing pipeline show command", issues)
	}
	if !hasIssue(issues, "README.md: missing workflow show command ./bin/zv workflows show pipeline --format json") {
		t.Fatalf("issues = %#v, want missing pipeline json show command", issues)
	}
}

func TestZVBinaryCanonicalGroupHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "demo", args: []string{"demo", "--help"}, want: demoUsage},
		{name: "utility", args: []string{"utility", "--help"}, want: utilityUsage},
		{name: "compose", args: []string{"compose", "--help"}, want: composeUsage},
		{name: "shorts", args: []string{"shorts", "--help"}, want: shortsUsage},
		{name: "analysis", args: []string{"analysis", "--help"}, want: analysisUsage},
		{name: "gallery", args: []string{"gallery", "--help"}, want: galleryUsage},
		{name: "check", args: []string{"check", "--help"}, want: checkUsage},
		{name: "skills", args: []string{"skills", "--help"}, want: skillsUsage},
		{name: "workflows", args: []string{"workflows", "--help"}, want: workflowsUsage},
		{name: "serve", args: []string{"serve", "--help"}, want: serveUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runZVBinary(t, exe, tempDir, tt.args...)
			if got, want := out, tt.want; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryCanonicalHelpAliasesEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "root short", args: []string{"-h"}, want: usage},
		{name: "root long", args: []string{"--help"}, want: usage},
		{name: "root word", args: []string{"help"}, want: usage},
		{name: "demo short", args: []string{"demo", "-h"}, want: demoUsage},
		{name: "demo word", args: []string{"demo", "help"}, want: demoUsage},
		{name: "skills list short", args: []string{"skills", "list", "-h"}, want: skillsListUsage},
		{name: "skills list word", args: []string{"skills", "list", "help"}, want: skillsListUsage},
		{name: "workflows show short", args: []string{"workflows", "show", "-h"}, want: workflowsShowUsage},
		{name: "workflows show word", args: []string{"workflows", "show", "help"}, want: workflowsShowUsage},
		{name: "gallery open short", args: []string{"gallery", "open", "-h"}, want: galleryUsage},
		{name: "gallery open word", args: []string{"gallery", "open", "help"}, want: galleryUsage},
		{name: "workflows run project short", args: []string{"workflows", "run", "project-check", "--", "-h"}, want: checkUsage},
		{name: "workflows run project word", args: []string{"workflows", "run", "project-check", "--", "help"}, want: checkUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := runZVBinarySplit(t, exe, tempDir, tt.args...)

			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if got, want := stdout, tt.want; got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryHelpCoversWorkflowCatalogEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	stdout, stderr := runZVBinarySplit(t, exe, tempDir, "--help")
	if stderr != "" {
		t.Fatalf("root help stderr = %q, want empty", stderr)
	}
	rootStems := usageCommandStems(stdout)
	groupStems := make(map[string]map[string]struct{})
	for _, workflow := range workflowCatalog() {
		stem := helpCommandStem(documentedWorkflowCommand(workflow.Command))
		if stem == "" {
			continue
		}
		if _, ok := rootStems[stem]; !ok {
			t.Fatalf("root help does not cover workflow %q command %q; saw %#v", workflow.Name, stem, rootStems)
		}
		fields, ok := splitCommandFields(stem)
		if !ok || len(fields) < 2 || fields[0] != "zv" {
			t.Fatalf("workflow %q command stem %q is not a zv command", workflow.Name, stem)
		}
		if fields[1] == "record" || fields[1] == "pipeline" {
			continue
		}
		if groupStems[fields[1]] == nil {
			groupStdout, groupStderr := runZVBinarySplit(t, exe, tempDir, fields[1], "--help")
			if groupStderr != "" {
				t.Fatalf("%s help stderr = %q, want empty", fields[1], groupStderr)
			}
			groupStems[fields[1]] = usageCommandStems(groupStdout)
		}
		if _, ok := groupStems[fields[1]][stem]; !ok {
			t.Fatalf("%s help does not cover workflow %q command %q; saw %#v", fields[1], workflow.Name, stem, groupStems[fields[1]])
		}
	}
}

func TestZVBinaryCanonicalSubcommandHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "skills list", args: []string{"skills", "list", "--help"}, want: skillsListUsage},
		{name: "skills show", args: []string{"skills", "show", "--help"}, want: skillsShowUsage},
		{name: "skills check", args: []string{"skills", "check", "--help"}, want: skillsCheckUsage},
		{name: "gallery open", args: []string{"gallery", "open", "--help"}, want: galleryUsage},
		{name: "workflows list", args: []string{"workflows", "list", "--help"}, want: workflowsListUsage},
		{name: "workflows show", args: []string{"workflows", "show", "--help"}, want: workflowsShowUsage},
		{name: "workflows check", args: []string{"workflows", "check", "--help"}, want: workflowsCheckUsage},
		{name: "run skills check", args: []string{"workflows", "run", "skills-check", "--", "--help"}, want: skillsCheckUsage},
		{name: "run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--help"}, want: workflowsCheckUsage},
		{name: "run project check", args: []string{"workflows", "run", "project-check", "--", "--help"}, want: checkUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runZVBinary(t, exe, tempDir, tt.args...)
			if got, want := out, tt.want; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryCanonicalSubcommandHelpUsesStdoutOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "skills list", args: []string{"skills", "list", "--help"}, want: skillsListUsage},
		{name: "skills show", args: []string{"skills", "show", "--help"}, want: skillsShowUsage},
		{name: "skills check", args: []string{"skills", "check", "--help"}, want: skillsCheckUsage},
		{name: "gallery open", args: []string{"gallery", "open", "--help"}, want: galleryUsage},
		{name: "workflows list", args: []string{"workflows", "list", "--help"}, want: workflowsListUsage},
		{name: "workflows show", args: []string{"workflows", "show", "--help"}, want: workflowsShowUsage},
		{name: "workflows check", args: []string{"workflows", "check", "--help"}, want: workflowsCheckUsage},
		{name: "run skills check", args: []string{"workflows", "run", "skills-check", "--", "--help"}, want: skillsCheckUsage},
		{name: "run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--help"}, want: workflowsCheckUsage},
		{name: "run project check", args: []string{"workflows", "run", "project-check", "--", "--help"}, want: checkUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := runZVBinarySplit(t, exe, tempDir, tt.args...)
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if got, want := stdout, tt.want; got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryWorkflowRunForwardsDelegatedHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	logPath := filepath.Join(tempDir, "calls.jsonl")
	runZVBinaryWithEnv(t, exe, tempDir, []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + logPath,
	}, "workflows", "run", "demo-parse", "--", "--help")

	calls := readFakeSubcommandCalls(t, logPath)
	if got, want := len(calls), 1; got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, calls)
	}
	if got, want := calls[0].Executable, executableName("zv-parser"); got != want {
		t.Fatalf("executable = %q, want %q", got, want)
	}
	if got, want := strings.Join(calls[0].Args, "\x00"), strings.Join([]string{"parse", "--help"}, "\x00"); got != want {
		t.Fatalf("args = %#v, want parse --help", calls[0].Args)
	}
}

func TestZVBinaryEveryWorkflowHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	logPath := filepath.Join(tempDir, "calls.jsonl")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + logPath,
	}
	wantDelegated := 0
	for _, workflow := range workflowCatalog() {
		t.Run(workflow.Name, func(t *testing.T) {
			args := workflowRunCommandArgs(t, workflow)
			args = append(args, "--", "--help")
			out := runZVBinaryWithEnv(t, exe, tempDir, env, args...)
			if workflowHelpDelegatesExternally(workflow) {
				wantDelegated++
				return
			}
			if strings.TrimSpace(out) == "" {
				t.Fatalf("help output is empty")
			}
		})
	}
	if got, want := len(readFakeSubcommandCalls(t, logPath)), wantDelegated; got != want {
		t.Fatalf("delegated help calls = %d, want %d", got, want)
	}
}

func TestZVBinaryEveryDirectWorkflowHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	logPath := filepath.Join(tempDir, "calls.jsonl")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + logPath,
	}
	wantDelegated := 0
	for _, workflow := range workflowCatalog() {
		t.Run(workflow.Name, func(t *testing.T) {
			args := append([]string(nil), workflow.RunArgs...)
			args = append(args, "--help")
			out := runZVBinaryWithEnv(t, exe, tempDir, env, args...)
			if workflowHelpDelegatesExternally(workflow) {
				wantDelegated++
				return
			}
			if strings.TrimSpace(out) == "" {
				t.Fatalf("help output is empty")
			}
		})
	}
	if got, want := len(readFakeSubcommandCalls(t, logPath)), wantDelegated; got != want {
		t.Fatalf("delegated direct help calls = %d, want %d", got, want)
	}
}

func TestZVBinaryCanonicalSubcommandUsageErrorsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "skills list", args: []string{"skills", "list", "extra"}, want: "error: unexpected extra args for \"skills list\"\n" + skillsListUsage},
		{name: "skills show", args: []string{"skills", "show"}, want: "error: missing skill name for \"skills show\"\n" + skillsShowUsage},
		{name: "skills check", args: []string{"skills", "check", "extra"}, want: "error: unexpected extra args for \"skills check\"\n" + skillsCheckUsage},
		{name: "workflows list", args: []string{"workflows", "list", "extra"}, want: "error: unexpected extra args for \"workflows list\"\n" + workflowsListUsage},
		{name: "workflows show", args: []string{"workflows", "show"}, want: "error: missing workflow name for \"workflows show\"\n" + workflowsShowUsage},
		{name: "workflows run", args: []string{"workflows", "run"}, want: workflowsRunUsage},
		{name: "workflows check", args: []string{"workflows", "check", "extra"}, want: "error: unexpected extra args for \"workflows check\"\n" + workflowsCheckUsage},
		{name: "serve", args: []string{"serve", "extra"}, want: "error: unexpected extra args for \"serve\"\n" + serveUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, code := runZVBinaryFailure(t, exe, tempDir, tt.args...)
			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\n%s", got, want, out)
			}
			if got, want := out, tt.want; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryIncompleteCommandsUseStderrOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "root", args: nil, want: usage},
		{name: "demo", args: []string{"demo"}, want: demoUsage},
		{name: "utility", args: []string{"utility"}, want: utilityUsage},
		{name: "compose", args: []string{"compose"}, want: composeUsage},
		{name: "shorts", args: []string{"shorts"}, want: shortsUsage},
		{name: "analysis", args: []string{"analysis"}, want: analysisUsage},
		{name: "gallery", args: []string{"gallery"}, want: galleryUsage},
		{name: "skills", args: []string{"skills"}, want: skillsUsage},
		{name: "workflows", args: []string{"workflows"}, want: workflowsUsage},
		{name: "workflows run", args: []string{"workflows", "run"}, want: workflowsRunUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.want; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryServeRejectsExtraArgsBeforeDelegationEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	tests := []struct {
		name string
		args []string
	}{
		{name: "direct", args: []string{"serve", "extra"}},
		{name: "workflow run", args: []string{"workflows", "run", "serve", "--", "extra"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, tt.name+"-serve-extra.jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `unexpected extra args for "serve"`) {
				t.Fatalf("stderr = %q, want serve extra args error", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
		})
	}
}

func TestZVBinaryGalleryOpenRejectsEmptyPathBeforeOpenEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{name: "direct", args: []string{"gallery", "open", "--path", ""}, wantStderr: "error: missing required flag --path for \"gallery open\"\n"},
		{name: "workflow run", args: []string{"workflows", "run", "gallery-open", "--", "--path", ""}, wantStderr: "error: missing required flag --path for \"gallery open\"\n" + workflowsRunUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openPathLogPath := filepath.Join(tempDir, tt.name+"-gallery-open-empty-path.txt")
			env := []string{"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantStderr; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryGalleryOpenSuccessUsesStdoutOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	tests := []struct {
		name string
		args []string
	}{
		{name: "direct", args: []string{"gallery", "open", "--path", galleryPath}},
		{name: "workflow run", args: []string{"workflows", "run", "gallery-open", "--", "--path", galleryPath}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			openPathLogPath := filepath.Join(tempDir, tt.name+"-gallery-open-success.txt")
			env := []string{"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath}

			stdout, stderr := runZVBinarySplitWithEnv(t, exe, tempDir, env, tt.args...)

			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if got, want := stdout, fmt.Sprintf("opened: %s\n", galleryPath); got != want {
				t.Fatalf("stdout = %q, want %q", got, want)
			}
			if got, want := strings.Join(readLines(t, openPathLogPath), "\n"), galleryPath; got != want {
				t.Fatalf("open path log = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryUnknownGroupCommandsShowUsageEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "demo", args: []string{"demo", "wat"}, want: "unknown demo command \"wat\"\n" + demoUsage},
		{name: "utility", args: []string{"utility", "wat"}, want: "unknown utility command \"wat\"\n" + utilityUsage},
		{name: "compose", args: []string{"compose", "wat"}, want: "unknown compose command \"wat\"\n" + composeUsage},
		{name: "shorts", args: []string{"shorts", "wat"}, want: "unknown shorts command \"wat\"\n" + shortsUsage},
		{name: "analysis", args: []string{"analysis", "wat"}, want: "unknown analysis command \"wat\"\n" + analysisUsage},
		{name: "gallery", args: []string{"gallery", "wat"}, want: "unknown gallery command \"wat\"\n" + galleryUsage},
		{name: "skills", args: []string{"skills", "wat"}, want: "unknown skills command \"wat\"\n" + skillsUsage},
		{name: "workflows", args: []string{"workflows", "wat"}, want: "unknown workflows command \"wat\"\n" + workflowsUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, code := runZVBinaryFailure(t, exe, tempDir, tt.args...)
			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\n%s", got, want, out)
			}
			if got, want := out, tt.want; got != want {
				t.Fatalf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryUnknownRootCommandUsesStderrOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "wat")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if got, want := stderr, "unknown command \"wat\"\n"+usage; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestZVBinaryJSONCheckFailuresEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	t.Run("skills check", func(t *testing.T) {
		root := filepath.Join(tempDir, "skills-failure")
		writeSkillBody(t, root, "alpha", strings.Join([]string{
			"---",
			"name: alpha",
			`description: "Alpha workflow"`,
			"---",
			"",
			"```powershell",
			`.\bin\zv-parser.exe parse --demo demo.dem --steamid 76561198000000000`,
			"```",
			"",
		}, "\n"))

		tests := []struct {
			name string
			args []string
		}{
			{name: "direct", args: []string{"skills", "check", "--format", "json"}},
			{name: "workflow run", args: []string{"workflows", "run", "skills-check", "--", "--format", "json"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stdout, stderr, code := runZVBinaryFailureSplit(t, exe, root, tt.args...)

				if got, want := code, exitInvalidArgs; got != want {
					t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
				}
				if stderr != "" {
					t.Fatalf("stderr = %q, want empty", stderr)
				}
				var result skillCheckResult
				if err := json.Unmarshal([]byte(stdout), &result); err != nil {
					t.Fatalf("unmarshal skills check failure json: %v\n%s", err, stdout)
				}
				if result.OK {
					t.Fatalf("result.OK = true, want false")
				}
				if got := len(result.Issues); got == 0 {
					t.Fatalf("issues len = 0, want issues")
				}
				var row map[string]json.RawMessage
				if err := json.Unmarshal([]byte(stdout), &row); err != nil {
					t.Fatalf("unmarshal skills check failure json schema: %v\n%s", err, stdout)
				}
				assertJSONKeys(t, "skills check failure json", row, "ok", "skills_checked", "issues")
				assertIssueJSONKeys(t, "skills check failure json issues", row["issues"])
			})
		}
	})

	t.Run("workflow checks", func(t *testing.T) {
		root := filepath.Join(tempDir, "workflow-failure")
		writeSkillBody(t, root, "alpha", strings.Join([]string{
			"---",
			"name: alpha",
			`description: "Alpha workflow"`,
			"---",
			"",
			"```powershell",
			`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
			"```",
			"",
		}, "\n"))
		writeWorkflowDocs(t, root)
		appendFile(t, filepath.Join(root, "README.md"), "\n./bin/zv-parser parse --demo demo.dem --steamid 76561198000000000\n")

		tests := []struct {
			name string
			args []string
		}{
			{name: "workflows check", args: []string{"workflows", "check", "--format", "json"}},
			{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--format", "json"}},
			{name: "project check", args: []string{"check", "--format", "json"}},
			{name: "workflow run project check", args: []string{"workflows", "run", "project-check", "--", "--format", "json"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stdout, stderr, code := runZVBinaryFailureSplit(t, exe, root, tt.args...)

				if got, want := code, exitInvalidArgs; got != want {
					t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
				}
				if stderr != "" {
					t.Fatalf("stderr = %q, want empty", stderr)
				}
				var result workflowCheckResult
				if err := json.Unmarshal([]byte(stdout), &result); err != nil {
					t.Fatalf("unmarshal %s failure json: %v\n%s", tt.name, err, stdout)
				}
				if result.OK {
					t.Fatalf("result.OK = true, want false")
				}
				if got := len(result.Issues); got == 0 {
					t.Fatalf("issues len = 0, want issues")
				}
				var row map[string]json.RawMessage
				if err := json.Unmarshal([]byte(stdout), &row); err != nil {
					t.Fatalf("unmarshal %s failure json schema: %v\n%s", tt.name, err, stdout)
				}
				assertJSONKeys(t, tt.name+" failure json", row, "ok", "skills_checked", "workflows_checked", "workflow_docs_checked", "agent_prompt_wrappers_checked", "issues")
				assertIssueJSONKeys(t, tt.name+" failure json issues", row["issues"])
			})
		}
	})
}

func TestZVBinaryJSONSuccessUsesStdoutOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name string
		args []string
	}{
		{name: "skills list", args: []string{"skills", "list", "--format", "json"}},
		{name: "skills check", args: []string{"skills", "check", "--format", "json"}},
		{name: "workflows list", args: []string{"workflows", "list", "--format", "json"}},
		{name: "workflows check", args: []string{"workflows", "check", "--format", "json"}},
		{name: "project check", args: []string{"check", "--format", "json"}},
		{name: "workflow run skills check", args: []string{"workflows", "run", "skills-check", "--", "--format", "json"}},
		{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--format", "json"}},
		{name: "workflow run project check", args: []string{"workflows", "run", "project-check", "--", "--format", "json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := runZVBinarySplit(t, exe, tempDir, tt.args...)

			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if !json.Valid([]byte(stdout)) {
				t.Fatalf("stdout is not valid json: %q", stdout)
			}
		})
	}
}

func TestZVBinaryJSONEqualsFormatUsesStdoutOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name string
		args []string
	}{
		{name: "skills list", args: []string{"skills", "list", "--format=json"}},
		{name: "skills show", args: []string{"skills", "show", "alpha", "--format=json"}},
		{name: "skills check", args: []string{"skills", "check", "--format=json"}},
		{name: "workflows list", args: []string{"workflows", "list", "--format=json"}},
		{name: "workflows show", args: []string{"workflows", "show", "demo-parse", "--format=json"}},
		{name: "workflows check", args: []string{"workflows", "check", "--format=json"}},
		{name: "project check", args: []string{"check", "--format=json"}},
		{name: "workflow run skills check", args: []string{"workflows", "run", "skills-check", "--", "--format=json"}},
		{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--format=json"}},
		{name: "workflow run project check", args: []string{"workflows", "run", "project-check", "--", "--format=json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := runZVBinarySplit(t, exe, tempDir, tt.args...)

			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if !json.Valid([]byte(stdout)) {
				t.Fatalf("stdout is not valid json: %q", stdout)
			}
		})
	}
}

func TestZVBinaryFormattedCommandsRejectExtraArgsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name       string
		args       []string
		wantStderr string
		wantUsage  string
	}{
		{name: "skills list", args: []string{"skills", "list", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "skills list"` + "\n", wantUsage: skillsListUsage},
		{name: "skills show", args: []string{"skills", "show", "alpha", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "skills show"` + "\n", wantUsage: skillsShowUsage},
		{name: "skills check", args: []string{"skills", "check", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "skills check"` + "\n", wantUsage: skillsCheckUsage},
		{name: "workflows list", args: []string{"workflows", "list", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "workflows list"` + "\n", wantUsage: workflowsListUsage},
		{name: "workflows show", args: []string{"workflows", "show", "demo-parse", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "workflows show"` + "\n", wantUsage: workflowsShowUsage},
		{name: "workflows check", args: []string{"workflows", "check", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "workflows check"` + "\n", wantUsage: workflowsCheckUsage},
		{name: "project check", args: []string{"check", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "check"` + "\n", wantUsage: checkUsage},
		{name: "workflow run skills check", args: []string{"workflows", "run", "skills-check", "--", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "skills check"` + "\n", wantUsage: workflowsRunUsage},
		{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check", "--", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "workflows check"` + "\n", wantUsage: workflowsRunUsage},
		{name: "workflow run project check", args: []string{"workflows", "run", "project-check", "--", "--format=json", "extra"}, wantStderr: `error: unexpected extra args for "check"` + "\n", wantUsage: workflowsRunUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantStderr+tt.wantUsage; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryFormattedCommandsRejectInvalidFormatEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	exe := buildZVBinary(t, tempDir)

	commands := []struct {
		name      string
		args      []string
		wantUsage string
	}{
		{name: "skills list", args: []string{"skills", "list"}},
		{name: "skills show", args: []string{"skills", "show", "alpha"}},
		{name: "skills check", args: []string{"skills", "check"}},
		{name: "workflows list", args: []string{"workflows", "list"}},
		{name: "workflows show", args: []string{"workflows", "show", "demo-parse"}},
		{name: "workflows check", args: []string{"workflows", "check"}},
		{name: "project check", args: []string{"check"}},
		{name: "workflow run skills check", args: []string{"workflows", "run", "skills-check", "--"}, wantUsage: workflowsRunUsage},
		{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check", "--"}, wantUsage: workflowsRunUsage},
		{name: "workflow run project check", args: []string{"workflows", "run", "project-check", "--"}, wantUsage: workflowsRunUsage},
	}
	invalidFormats := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{name: "unsupported", args: []string{"--format=yaml"}, wantStderr: `unsupported format "yaml"`},
		{name: "missing value", args: []string{"--format"}, wantStderr: `--format requires a value`},
		{name: "duplicate", args: []string{"--format=json", "--format", "text"}, wantStderr: `duplicate flag --format`},
	}
	for _, command := range commands {
		for _, invalid := range invalidFormats {
			t.Run(command.name+"/"+invalid.name, func(t *testing.T) {
				args := append([]string(nil), command.args...)
				args = append(args, invalid.args...)
				stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, args...)

				if got, want := code, exitInvalidArgs; got != want {
					t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
				}
				if stdout != "" {
					t.Fatalf("stdout = %q, want empty", stdout)
				}
				if got, want := stderr, "error: "+invalid.wantStderr+"\n"+command.wantUsage; got != want {
					t.Fatalf("stderr = %q, want %q", got, want)
				}
			})
		}
	}
}

func TestZVBinaryDiscoveryCommandsRejectUnknownNamesEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name            string
		args            []string
		wantExactStderr string
	}{
		{name: "skills show text", args: []string{"skills", "show", "missing"}, wantExactStderr: "error: skill not found: missing\n"},
		{name: "skills show json", args: []string{"skills", "show", "missing", "--format=json"}, wantExactStderr: "error: skill not found: missing\n"},
		{name: "workflows show text", args: []string{"workflows", "show", "missing"}, wantExactStderr: "error: workflow not found: missing\n"},
		{name: "workflows show json", args: []string{"workflows", "show", "missing", "--format=json"}, wantExactStderr: "error: workflow not found: missing\n"},
		{name: "workflows run", args: []string{"workflows", "run", "missing"}, wantExactStderr: "error: workflow not found: missing\n"},
		{name: "workflows run with separator", args: []string{"workflows", "run", "missing", "--"}, wantExactStderr: "error: workflow not found: missing\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantExactStderr; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
		})
	}
}

func TestZVBinaryTextSuccessUsesStdoutOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"# alpha",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name string
		args []string
	}{
		{name: "root help", args: []string{"--help"}},
		{name: "skills list", args: []string{"skills", "list"}},
		{name: "skills show", args: []string{"skills", "show", "alpha"}},
		{name: "skills check", args: []string{"skills", "check"}},
		{name: "workflows list", args: []string{"workflows", "list"}},
		{name: "workflows show", args: []string{"workflows", "show", "demo-parse"}},
		{name: "workflows check", args: []string{"workflows", "check"}},
		{name: "project check", args: []string{"check"}},
		{name: "workflow run skills check", args: []string{"workflows", "run", "skills-check"}},
		{name: "workflow run workflows check", args: []string{"workflows", "run", "workflows-check"}},
		{name: "workflow run project check", args: []string{"workflows", "run", "project-check"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := runZVBinarySplit(t, exe, tempDir, tt.args...)

			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if strings.TrimSpace(stdout) == "" {
				t.Fatalf("stdout is empty")
			}
		})
	}
}

func TestZVBinaryTextFailuresUseStderrOnlyEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv-parser.exe parse --demo demo.dem --steamid 76561198000000000`,
		"```",
		"",
	}, "\n"))
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name string
		args []string
	}{
		{name: "root unknown", args: []string{"wat"}},
		{name: "group unknown", args: []string{"skills", "wat"}},
		{name: "usage error", args: []string{"skills", "show"}},
		{name: "missing workflow", args: []string{"workflows", "show", "missing"}},
		{name: "missing workflow run flags", args: []string{"workflows", "run", "demo-parse"}},
		{name: "skills check issue", args: []string{"skills", "check"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Fatalf("stderr is empty")
			}
		})
	}
}

func TestZVBinarySkillsCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"# alpha",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run skills-check`,
		`.\bin\zv.exe skills show alpha --format json`,
		`.\bin\zv.exe workflows run workflows-check -- --format json`,
		"```",
		"",
	}, "\n"))

	exe := buildZVBinary(t, tempDir)
	tests := []struct {
		name       string
		args       []string
		wantStdout []string
	}{
		{
			name:       "list",
			args:       []string{"skills", "list"},
			wantStdout: []string{"alpha\tAlpha workflow"},
		},
		{
			name:       "show",
			args:       []string{"skills", "show", "alpha"},
			wantStdout: []string{"name: alpha", "# alpha"},
		},
		{
			name:       "show json",
			args:       []string{"skills", "show", "alpha", "--format", "json"},
			wantStdout: []string{`"name": "alpha"`, `"body":`, `# alpha`},
		},
		{
			name:       "check",
			args:       []string{"skills", "check"},
			wantStdout: []string{"OK: 1 skills checked"},
		},
		{
			name:       "list json",
			args:       []string{"skills", "list", "--format", "json"},
			wantStdout: []string{`"name": "alpha"`, `"description": "Alpha workflow"`},
		},
		{
			name:       "check json",
			args:       []string{"skills", "check", "--format", "json"},
			wantStdout: []string{`"ok": true`, `"skills_checked": 1`, `"issues": []`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := runZVBinary(t, exe, tempDir, tt.args...)
			for _, want := range tt.wantStdout {
				if !strings.Contains(out, want) {
					t.Fatalf("output = %q, want %q", out, want)
				}
			}
		})
	}
}

func TestZVBinaryWorkflowsCheckEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)

	exe := buildZVBinary(t, tempDir)
	out := runZVBinary(t, exe, tempDir, "workflows", "check")
	want := fmt.Sprintf("OK: 1 skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked", len(workflowCatalog()), len(workflowDocs()), len(agentPromptWrapperFixtures()))
	if !strings.Contains(out, want) {
		t.Fatalf("output = %q, want workflow OK count", out)
	}
	checkOut := runZVBinary(t, exe, tempDir, "check")
	if !strings.Contains(checkOut, want) {
		t.Fatalf("output = %q, want project OK count", checkOut)
	}

	jsonOut := runZVBinary(t, exe, tempDir, "workflows", "check", "--format", "json")
	var result workflowCheckResult
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("unmarshal workflows check json: %v\n%s", err, jsonOut)
	}
	if !result.OK {
		t.Fatalf("result.OK = false, want true: %#v", result)
	}
	if got, want := result.WorkflowsChecked, len(workflowCatalog()); got != want {
		t.Fatalf("result.WorkflowsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowDocsChecked, len(workflowDocs()); got != want {
		t.Fatalf("result.WorkflowDocsChecked = %d, want %d", got, want)
	}
	if got, want := result.AgentPromptWrappersChecked, len(agentPromptWrapperFixtures()); got != want {
		t.Fatalf("result.AgentPromptWrappersChecked = %d, want %d", got, want)
	}

	runJSONOut := runZVBinary(t, exe, tempDir, "workflows", "run", "workflows-check", "--", "--format", "json")
	var runResult workflowCheckResult
	if err := json.Unmarshal([]byte(runJSONOut), &runResult); err != nil {
		t.Fatalf("unmarshal workflows run check json: %v\n%s", err, runJSONOut)
	}
	if !runResult.OK {
		t.Fatalf("runResult.OK = false, want true: %#v", runResult)
	}
	if got, want := runResult.WorkflowsChecked, len(workflowCatalog()); got != want {
		t.Fatalf("runResult.WorkflowsChecked = %d, want %d", got, want)
	}

	projectJSONOut := runZVBinary(t, exe, tempDir, "workflows", "run", "project-check", "--", "--format", "json")
	var projectResult workflowCheckResult
	if err := json.Unmarshal([]byte(projectJSONOut), &projectResult); err != nil {
		t.Fatalf("unmarshal project check json: %v\n%s", err, projectJSONOut)
	}
	if !projectResult.OK {
		t.Fatalf("projectResult.OK = false, want true: %#v", projectResult)
	}
}

func TestZVBinaryCurrentRepoCheckEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	root := repoRoot(t)
	wantSkills := currentRepoSkills(t, root)

	jsonOut := runZVBinary(t, exe, root, "check", "--format", "json")
	var result workflowCheckResult
	if err := json.Unmarshal([]byte(jsonOut), &result); err != nil {
		t.Fatalf("unmarshal current repo check json: %v\n%s", err, jsonOut)
	}
	if !result.OK {
		t.Fatalf("current repo check failed: %#v", result)
	}
	if got, want := result.SkillsChecked, len(wantSkills); got != want {
		t.Fatalf("SkillsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowsChecked, len(workflowCatalog()); got != want {
		t.Fatalf("WorkflowsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowDocsChecked, len(workflowDocs()); got != want {
		t.Fatalf("WorkflowDocsChecked = %d, want %d", got, want)
	}
	if got, want := result.AgentPromptWrappersChecked, len(currentAgentPromptWrappers(t, root)); got != want {
		t.Fatalf("AgentPromptWrappersChecked = %d, want %d", got, want)
	}
	if got := len(result.Issues); got != 0 {
		t.Fatalf("issues len = %d, want 0: %#v", got, result.Issues)
	}
}

func TestZVBinaryCurrentRepoWorkflowChecksEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	root := repoRoot(t)
	wantSkills := currentRepoSkills(t, root)
	wantWrappers := currentAgentPromptWrappers(t, root)
	wantText := fmt.Sprintf("OK: %d skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked\n", len(wantSkills), len(workflowCatalog()), len(workflowDocs()), len(wantWrappers))

	workflowText, workflowTextStderr := runZVBinarySplit(t, exe, root, "workflows", "check")
	if workflowTextStderr != "" {
		t.Fatalf("workflows check stderr = %q, want empty", workflowTextStderr)
	}
	if got, want := workflowText, wantText; got != want {
		t.Fatalf("workflows check text = %q, want %q", got, want)
	}

	projectText, projectTextStderr := runZVBinarySplit(t, exe, root, "check")
	if projectTextStderr != "" {
		t.Fatalf("check stderr = %q, want empty", projectTextStderr)
	}
	if got, want := projectText, workflowText; got != want {
		t.Fatalf("check text = %q, want workflows check text %q", got, want)
	}

	runWorkflowText, runWorkflowTextStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "workflows-check")
	if runWorkflowTextStderr != "" {
		t.Fatalf("workflows run workflows-check stderr = %q, want empty", runWorkflowTextStderr)
	}
	if got, want := runWorkflowText, workflowText; got != want {
		t.Fatalf("workflow run workflows-check text = %q, want direct output %q", got, want)
	}

	runProjectText, runProjectTextStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "project-check")
	if runProjectTextStderr != "" {
		t.Fatalf("workflows run project-check stderr = %q, want empty", runProjectTextStderr)
	}
	if got, want := runProjectText, projectText; got != want {
		t.Fatalf("workflow run project-check text = %q, want direct output %q", got, want)
	}

	workflowJSON, workflowJSONStderr := runZVBinarySplit(t, exe, root, "workflows", "check", "--format", "json")
	if workflowJSONStderr != "" {
		t.Fatalf("workflows check json stderr = %q, want empty", workflowJSONStderr)
	}
	result := decodeWorkflowCheckResult(t, workflowJSON)
	if !result.OK {
		t.Fatalf("workflows check json ok = false: %#v", result)
	}
	if got, want := result.SkillsChecked, len(wantSkills); got != want {
		t.Fatalf("SkillsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowsChecked, len(workflowCatalog()); got != want {
		t.Fatalf("WorkflowsChecked = %d, want %d", got, want)
	}
	if got, want := result.WorkflowDocsChecked, len(workflowDocs()); got != want {
		t.Fatalf("WorkflowDocsChecked = %d, want %d", got, want)
	}
	if got, want := result.AgentPromptWrappersChecked, len(wantWrappers); got != want {
		t.Fatalf("AgentPromptWrappersChecked = %d, want %d", got, want)
	}
	if got := len(result.Issues); got != 0 {
		t.Fatalf("issues len = %d, want 0: %#v", got, result.Issues)
	}

	projectJSON, projectJSONStderr := runZVBinarySplit(t, exe, root, "check", "--format", "json")
	if projectJSONStderr != "" {
		t.Fatalf("check json stderr = %q, want empty", projectJSONStderr)
	}
	if got, want := projectJSON, workflowJSON; got != want {
		t.Fatalf("check json = %q, want workflows check json %q", got, want)
	}

	runWorkflowJSON, runWorkflowJSONStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "workflows-check", "--", "--format", "json")
	if runWorkflowJSONStderr != "" {
		t.Fatalf("workflows run workflows-check json stderr = %q, want empty", runWorkflowJSONStderr)
	}
	if got, want := runWorkflowJSON, workflowJSON; got != want {
		t.Fatalf("workflow run workflows-check json = %q, want direct output %q", got, want)
	}

	runProjectJSON, runProjectJSONStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "project-check", "--", "--format", "json")
	if runProjectJSONStderr != "" {
		t.Fatalf("workflows run project-check json stderr = %q, want empty", runProjectJSONStderr)
	}
	if got, want := runProjectJSON, projectJSON; got != want {
		t.Fatalf("workflow run project-check json = %q, want direct output %q", got, want)
	}
}

func TestZVBinaryCurrentRepoSkillsDiscoveryEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	wantSkills := currentRepoSkills(t, root)

	listText := runZVBinary(t, exe, root, "skills", "list")
	for _, skill := range wantSkills {
		if !strings.Contains(listText, skill.Name) {
			t.Fatalf("skills list output = %q, want skill %q", listText, skill.Name)
		}
		if skill.Description != "" && !strings.Contains(listText, skill.Description) {
			t.Fatalf("skills list output = %q, want description %q", listText, skill.Description)
		}
	}
	if got, want := listText, skillListText(wantSkills); got != want {
		t.Fatalf("skills list text = %q, want %q", got, want)
	}

	listJSON := runZVBinary(t, exe, root, "skills", "list", "--format", "json")
	var gotSkills []skillInfo
	if err := json.Unmarshal([]byte(listJSON), &gotSkills); err != nil {
		t.Fatalf("unmarshal skills list json: %v\n%s", err, listJSON)
	}
	if got, want := len(gotSkills), len(wantSkills); got != want {
		t.Fatalf("skills list len = %d, want %d: %#v", got, want, gotSkills)
	}
	for i, want := range wantSkills {
		got := gotSkills[i]
		if got.Name != want.Name || got.Description != want.Description {
			t.Fatalf("skill %d = %#v, want %#v", i, got, want)
		}
		if strings.Contains(listJSON, want.Path) {
			t.Fatalf("skills list json leaked local path %q", want.Path)
		}
	}
	if got, want := skillNames(gotSkills), skillNames(wantSkills); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("skills list json order = %#v, want %#v", got, want)
	}

	for _, skill := range wantSkills {
		showJSON := runZVBinary(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		wantBody := readFileString(t, skill.Path)
		if detail.Name != skill.Name || detail.Description != skill.Description || detail.Body != wantBody {
			t.Fatalf("skills show %s = %#v, want name=%q description=%q body from %s", skill.Name, detail, skill.Name, skill.Description, skill.Path)
		}
		showText := runZVBinary(t, exe, root, "skills", "show", skill.Name)
		if strings.TrimRight(showText, "\n") != strings.TrimRight(wantBody, "\n") {
			t.Fatalf("skills show text for %s did not match %s", skill.Name, skill.Path)
		}
	}
}

func TestZVBinaryCurrentRepoPublicJSONSchemasEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	wantSkills := currentRepoSkills(t, root)

	skillsListJSON, skillsListStderr := runZVBinarySplit(t, exe, root, "skills", "list", "--format", "json")
	if skillsListStderr != "" {
		t.Fatalf("skills list json stderr = %q, want empty", skillsListStderr)
	}
	var skillListRows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(skillsListJSON), &skillListRows); err != nil {
		t.Fatalf("unmarshal skills list json schema: %v\n%s", err, skillsListJSON)
	}
	if got, want := len(skillListRows), len(wantSkills); got != want {
		t.Fatalf("skills list json rows = %d, want %d", got, want)
	}
	for i, row := range skillListRows {
		assertJSONKeys(t, fmt.Sprintf("skills list json row %d", i), row, "name", "description")
	}

	for _, skill := range wantSkills {
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("skills show json stderr for %s = %q, want empty", skill.Name, showStderr)
		}
		var showRow map[string]json.RawMessage
		if err := json.Unmarshal([]byte(showJSON), &showRow); err != nil {
			t.Fatalf("unmarshal skills show json schema for %s: %v\n%s", skill.Name, err, showJSON)
		}
		assertJSONKeys(t, "skills show json "+skill.Name, showRow, "name", "description", "body")
		if strings.Contains(showJSON, skill.Path) {
			t.Fatalf("skills show json for %s leaked local path %q", skill.Name, skill.Path)
		}
	}

	workflowsListJSON, workflowsListStderr := runZVBinarySplit(t, exe, root, "workflows", "list", "--format", "json")
	if workflowsListStderr != "" {
		t.Fatalf("workflows list json stderr = %q, want empty", workflowsListStderr)
	}
	var workflowListRows []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(workflowsListJSON), &workflowListRows); err != nil {
		t.Fatalf("unmarshal workflows list json schema: %v\n%s", err, workflowsListJSON)
	}
	if got, want := len(workflowListRows), len(workflowCatalog()); got != want {
		t.Fatalf("workflows list json rows = %d, want %d", got, want)
	}
	for i, row := range workflowListRows {
		assertJSONKeys(t, fmt.Sprintf("workflows list json row %d", i), row, "name", "description", "command", "run_command")
	}

	for _, workflow := range workflowCatalog() {
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "workflows", "show", workflow.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("workflows show json stderr for %s = %q, want empty", workflow.Name, showStderr)
		}
		var showRow map[string]json.RawMessage
		if err := json.Unmarshal([]byte(showJSON), &showRow); err != nil {
			t.Fatalf("unmarshal workflows show json schema for %s: %v\n%s", workflow.Name, err, showJSON)
		}
		assertJSONKeys(t, "workflows show json "+workflow.Name, showRow, "name", "description", "command", "run_command")
	}

	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "skills check json", args: []string{"skills", "check", "--format", "json"}},
		{name: "workflow run skills check json", args: []string{"workflows", "run", "skills-check", "--", "--format", "json"}},
	} {
		stdout, stderr := runZVBinarySplit(t, exe, root, tt.args...)
		if stderr != "" {
			t.Fatalf("%s stderr = %q, want empty", tt.name, stderr)
		}
		var row map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stdout), &row); err != nil {
			t.Fatalf("unmarshal %s schema: %v\n%s", tt.name, err, stdout)
		}
		assertJSONKeys(t, tt.name, row, "ok", "skills_checked", "issues")
	}

	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "workflows check json", args: []string{"workflows", "check", "--format", "json"}},
		{name: "project check json", args: []string{"check", "--format", "json"}},
		{name: "workflow run workflows check json", args: []string{"workflows", "run", "workflows-check", "--", "--format", "json"}},
		{name: "workflow run project check json", args: []string{"workflows", "run", "project-check", "--", "--format", "json"}},
	} {
		stdout, stderr := runZVBinarySplit(t, exe, root, tt.args...)
		if stderr != "" {
			t.Fatalf("%s stderr = %q, want empty", tt.name, stderr)
		}
		var row map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stdout), &row); err != nil {
			t.Fatalf("unmarshal %s schema: %v\n%s", tt.name, err, stdout)
		}
		assertJSONKeys(t, tt.name, row, "ok", "skills_checked", "workflows_checked", "workflow_docs_checked", "agent_prompt_wrappers_checked", "issues")
	}
}

func TestZVBinaryCurrentRepoDiscoveryTextUsesStdoutOnlyEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	wantSkills := currentRepoSkills(t, root)

	skillsList, skillsListStderr := runZVBinarySplit(t, exe, root, "skills", "list")
	if skillsListStderr != "" {
		t.Fatalf("skills list stderr = %q, want empty", skillsListStderr)
	}
	if got, want := skillsList, skillListText(wantSkills); got != want {
		t.Fatalf("skills list stdout = %q, want %q", got, want)
	}

	for _, skill := range wantSkills {
		showText, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name)
		if showStderr != "" {
			t.Fatalf("skills show %s stderr = %q, want empty", skill.Name, showStderr)
		}
		wantBody := readFileString(t, skill.Path)
		if strings.TrimRight(showText, "\n") != strings.TrimRight(wantBody, "\n") {
			t.Fatalf("skills show %s stdout did not match %s", skill.Name, skill.Path)
		}
	}

	workflowsList, workflowsListStderr := runZVBinarySplit(t, exe, root, "workflows", "list")
	if workflowsListStderr != "" {
		t.Fatalf("workflows list stderr = %q, want empty", workflowsListStderr)
	}
	if got, want := workflowsList, workflowListText(workflowCatalog()); got != want {
		t.Fatalf("workflows list stdout = %q, want %q", got, want)
	}

	for _, workflow := range workflowCatalog() {
		showText, showStderr := runZVBinarySplit(t, exe, root, "workflows", "show", workflow.Name)
		if showStderr != "" {
			t.Fatalf("workflows show %s stderr = %q, want empty", workflow.Name, showStderr)
		}
		if got, want := showText, workflowShowText(workflow); got != want {
			t.Fatalf("workflows show %s stdout = %q, want %q", workflow.Name, got, want)
		}
	}
}

func TestZVBinaryCurrentRepoSkillsCheckEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	wantSkills := currentRepoSkills(t, root)

	wantText := fmt.Sprintf("OK: %d skills checked\n", len(wantSkills))
	directText, directTextStderr := runZVBinarySplit(t, exe, root, "skills", "check")
	if directTextStderr != "" {
		t.Fatalf("skills check stderr = %q, want empty", directTextStderr)
	}
	if got, want := directText, wantText; got != want {
		t.Fatalf("skills check text = %q, want %q", got, want)
	}

	runText, runTextStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "skills-check")
	if runTextStderr != "" {
		t.Fatalf("workflows run skills-check stderr = %q, want empty", runTextStderr)
	}
	if got, want := runText, directText; got != want {
		t.Fatalf("workflow run skills-check text = %q, want direct output %q", got, want)
	}

	directJSON, directJSONStderr := runZVBinarySplit(t, exe, root, "skills", "check", "--format", "json")
	if directJSONStderr != "" {
		t.Fatalf("skills check json stderr = %q, want empty", directJSONStderr)
	}
	var directResult skillCheckResult
	if err := json.Unmarshal([]byte(directJSON), &directResult); err != nil {
		t.Fatalf("unmarshal skills check json: %v\n%s", err, directJSON)
	}
	if !directResult.OK {
		t.Fatalf("skills check json ok = false: %#v", directResult)
	}
	if got, want := directResult.SkillsChecked, len(wantSkills); got != want {
		t.Fatalf("skills checked = %d, want %d", got, want)
	}
	if got := len(directResult.Issues); got != 0 {
		t.Fatalf("skills check issues len = %d, want 0: %#v", got, directResult.Issues)
	}

	runJSON, runJSONStderr := runZVBinarySplit(t, exe, root, "workflows", "run", "skills-check", "--", "--format", "json")
	if runJSONStderr != "" {
		t.Fatalf("workflows run skills-check json stderr = %q, want empty", runJSONStderr)
	}
	if got, want := runJSON, directJSON; got != want {
		t.Fatalf("workflow run skills-check json = %q, want direct output %q", got, want)
	}
}

func TestZVBinaryCurrentRepoDiscoveryJSONEqualsFormatMatchesSpaceFormatEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name      string
		spaceArgs []string
		eqArgs    []string
	}{
		{
			name:      "skills list",
			spaceArgs: []string{"skills", "list", "--format", "json"},
			eqArgs:    []string{"skills", "list", "--format=json"},
		},
		{
			name:      "skills show",
			spaceArgs: []string{"skills", "show", "zackvideo-cs2-utility-shorts", "--format", "json"},
			eqArgs:    []string{"skills", "show", "zackvideo-cs2-utility-shorts", "--format=json"},
		},
		{
			name:      "workflows list",
			spaceArgs: []string{"workflows", "list", "--format", "json"},
			eqArgs:    []string{"workflows", "list", "--format=json"},
		},
		{
			name:      "workflows show",
			spaceArgs: []string{"workflows", "show", "demo-parse", "--format", "json"},
			eqArgs:    []string{"workflows", "show", "demo-parse", "--format=json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spaceStdout, spaceStderr := runZVBinarySplit(t, exe, root, tt.spaceArgs...)
			if spaceStderr != "" {
				t.Fatalf("space format stderr = %q, want empty", spaceStderr)
			}
			eqStdout, eqStderr := runZVBinarySplit(t, exe, root, tt.eqArgs...)
			if eqStderr != "" {
				t.Fatalf("equals format stderr = %q, want empty", eqStderr)
			}
			if got, want := eqStdout, spaceStdout; got != want {
				t.Fatalf("equals format stdout = %q, want space format stdout %q", got, want)
			}
			if !json.Valid([]byte(eqStdout)) {
				t.Fatalf("equals format stdout is not valid json: %q", eqStdout)
			}
		})
	}
}

func TestZVBinaryCurrentRepoCheckJSONEqualsFormatMatchesSpaceFormatEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	tests := []struct {
		name      string
		spaceArgs []string
		eqArgs    []string
	}{
		{
			name:      "skills check",
			spaceArgs: []string{"skills", "check", "--format", "json"},
			eqArgs:    []string{"skills", "check", "--format=json"},
		},
		{
			name:      "workflows check",
			spaceArgs: []string{"workflows", "check", "--format", "json"},
			eqArgs:    []string{"workflows", "check", "--format=json"},
		},
		{
			name:      "project check",
			spaceArgs: []string{"check", "--format", "json"},
			eqArgs:    []string{"check", "--format=json"},
		},
		{
			name:      "workflow run skills check",
			spaceArgs: []string{"workflows", "run", "skills-check", "--", "--format", "json"},
			eqArgs:    []string{"workflows", "run", "skills-check", "--", "--format=json"},
		},
		{
			name:      "workflow run workflows check",
			spaceArgs: []string{"workflows", "run", "workflows-check", "--", "--format", "json"},
			eqArgs:    []string{"workflows", "run", "workflows-check", "--", "--format=json"},
		},
		{
			name:      "workflow run project check",
			spaceArgs: []string{"workflows", "run", "project-check", "--", "--format", "json"},
			eqArgs:    []string{"workflows", "run", "project-check", "--", "--format=json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spaceStdout, spaceStderr := runZVBinarySplit(t, exe, root, tt.spaceArgs...)
			if spaceStderr != "" {
				t.Fatalf("space format stderr = %q, want empty", spaceStderr)
			}
			eqStdout, eqStderr := runZVBinarySplit(t, exe, root, tt.eqArgs...)
			if eqStderr != "" {
				t.Fatalf("equals format stderr = %q, want empty", eqStderr)
			}
			if got, want := eqStdout, spaceStdout; got != want {
				t.Fatalf("equals format stdout = %q, want space format stdout %q", got, want)
			}
			if !json.Valid([]byte(eqStdout)) {
				t.Fatalf("equals format stdout is not valid json: %q", eqStdout)
			}
		})
	}
}

func TestZVBinaryWorkflowsCatalogEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	listOut := runZVBinary(t, exe, tempDir, "workflows", "list")
	for _, want := range []string{
		"demo-parse\tParse a CS2 demo",
		"pipeline\tRun the local recorder-to-composer pipeline.",
		"workflows-check\tValidate skills, workflow catalog, and current workflow docs.",
		"project-check\tRun the full ZackVideo CLI, workflow, docs, and skills contract.",
	} {
		if !strings.Contains(listOut, want) {
			t.Fatalf("list output = %q, want %q", listOut, want)
		}
	}
	if got, want := listOut, workflowListText(workflowCatalog()); got != want {
		t.Fatalf("workflow list text = %q, want %q", got, want)
	}

	showOut := runZVBinary(t, exe, tempDir, "workflows", "show", "demo-parse")
	if !strings.Contains(showOut, "command: zv demo parse --demo <demo.dem> --steamid <SteamID64> --out <plan.json>") {
		t.Fatalf("show output = %q, want demo parse command", showOut)
	}
	if !strings.Contains(showOut, "run_command: zv workflows run demo-parse") {
		t.Fatalf("show output = %q, want demo parse run command", showOut)
	}

	jsonOut := runZVBinary(t, exe, tempDir, "workflows", "show", "demo-parse", "--format", "json")
	var workflow workflowInfo
	if err := json.Unmarshal([]byte(jsonOut), &workflow); err != nil {
		t.Fatalf("unmarshal show json: %v\n%s", err, jsonOut)
	}
	if got, want := workflow.Name, "demo-parse"; got != want {
		t.Fatalf("workflow.Name = %q, want %q", got, want)
	}
	if got, want := workflow.RunCommand, "zv workflows run demo-parse"; got != want {
		t.Fatalf("workflow.RunCommand = %q, want %q", got, want)
	}

	listJSONOut := runZVBinary(t, exe, tempDir, "workflows", "list", "--format", "json")
	var rawWorkflows []map[string]any
	if err := json.Unmarshal([]byte(listJSONOut), &rawWorkflows); err != nil {
		t.Fatalf("unmarshal list json: %v\n%s", err, listJSONOut)
	}
	if got, want := len(rawWorkflows), len(workflowCatalog()); got != want {
		t.Fatalf("list json workflow count = %d, want %d", got, want)
	}
	for i, workflow := range rawWorkflows {
		if _, ok := workflow["run_command"]; !ok {
			t.Fatalf("list json workflow %d missing run_command: %#v", i, workflow)
		}
		if _, ok := workflow["run_args"]; ok {
			t.Fatalf("list json workflow %d leaked run_args: %#v", i, workflow)
		}
	}
	var workflows []workflowInfo
	if err := json.Unmarshal([]byte(listJSONOut), &workflows); err != nil {
		t.Fatalf("unmarshal typed list json: %v\n%s", err, listJSONOut)
	}
	if got, want := workflowNames(workflows), workflowNames(workflowCatalog()); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("workflow list json order = %#v, want %#v", got, want)
	}
	for i, want := range workflowCatalog() {
		got := workflows[i]
		if got.Name != want.Name || got.Description != want.Description || got.Command != want.Command || got.RunCommand != want.RunCommand {
			t.Fatalf("list workflow %d = %#v, want %#v", i, got, want)
		}
		showOut := runZVBinary(t, exe, tempDir, "workflows", "show", want.Name)
		for _, expected := range []string{
			want.Name,
			want.Description,
			"command: " + want.Command,
			"run_command: " + want.RunCommand,
		} {
			if !strings.Contains(showOut, expected) {
				t.Fatalf("show output for %s = %q, want %q", want.Name, showOut, expected)
			}
		}
		showJSONOut := runZVBinary(t, exe, tempDir, "workflows", "show", want.Name, "--format", "json")
		var shown workflowInfo
		if err := json.Unmarshal([]byte(showJSONOut), &shown); err != nil {
			t.Fatalf("unmarshal show json for %s: %v\n%s", want.Name, err, showJSONOut)
		}
		if shown.Name != want.Name || shown.Description != want.Description || shown.Command != want.Command || shown.RunCommand != want.RunCommand {
			t.Fatalf("show workflow %s = %#v, want %#v", want.Name, shown, want)
		}
	}
}

func TestZVBinaryEveryWorkflowRunCommandEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	if err := os.MkdirAll(filepath.Join(tempDir, "gallery"), 0o755); err != nil {
		t.Fatalf("mkdir gallery: %v", err)
	}
	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	seen := make(map[string]bool)
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, workflow := range workflowCatalog() {
		t.Run(workflow.Name, func(t *testing.T) {
			args := workflowRunCommandArgs(t, workflow)
			args = append(args, workflowRunSampleForwardedArgs(t, workflow, galleryPath)...)
			runZVBinaryWithEnv(t, exe, tempDir, env, args...)
			seen[workflow.Name] = true
			switch {
			case workflow.RunArgs[0] == "gallery":
				wantOpenPathCalls++
			case workflow.RunArgs[0] == "skills" || workflow.RunArgs[0] == "workflows" || workflow.RunArgs[0] == "check":
			default:
				wantSubcommandCalls++
			}
		})
	}
	if got, want := len(seen), len(workflowCatalog()); got != want {
		t.Fatalf("executed workflows = %d, want %d: %#v", got, want, seen)
	}
	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryEveryWorkflowRunAcceptsEqualsRequiredFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	subcommandLogPath := filepath.Join(tempDir, "equals-required-subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "equals-required-open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	var wantSubcommandCalls, wantOpenPathCalls int
	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			args := workflowRunCommandArgs(t, workflow)
			forwarded := equalsRequiredFlags(t, workflowRunSampleForwardedArgs(t, workflow, galleryPath), required)
			args = append(args, forwarded...)
			runZVBinaryWithEnv(t, exe, tempDir, env, args...)
			switch {
			case workflow.RunArgs[0] == "gallery":
				wantOpenPathCalls++
			case workflow.RunArgs[0] == "skills" || workflow.RunArgs[0] == "workflows" || workflow.RunArgs[0] == "check":
			default:
				wantSubcommandCalls++
			}
		})
	}
	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryEveryWorkflowRunRejectsForwardedArgsWithoutSeparatorEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-missing-separator.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-missing-separator-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := workflowRunCommandArgs(t, workflow)
			args = append(args, workflowRunSampleArgsWithoutSeparator(t, workflow, galleryPath)...)
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `missing "--" separator before forwarded args for "workflows run"`) {
				t.Fatalf("stderr = %q, want missing separator error", stderr)
			}
			if !strings.Contains(stderr, workflowsRunUsage) {
				t.Fatalf("stderr = %q, want workflows run usage", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryRepresentativeValidationErrorsAreExactEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	validDemoParseFlags := []string{"--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json"}
	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "workflow run missing separator",
			args:       append([]string{"workflows", "run", "demo-parse"}, validDemoParseFlags...),
			wantStderr: "error: missing \"--\" separator before forwarded args for \"workflows run\"\n" + workflowsRunUsage,
		},
		{
			name:       "workflow run missing required flags",
			args:       []string{"workflows", "run", "demo-parse", "--"},
			wantStderr: "error: missing required flags --demo, --steamid, --out for \"demo parse\"\n" + workflowsRunUsage,
		},
		{
			name:       "workflow run unknown flag",
			args:       append(append([]string{"workflows", "run", "demo-parse", "--"}, validDemoParseFlags...), "--zv-unknown"),
			wantStderr: "error: unknown flag --zv-unknown for \"demo parse\"\n" + workflowsRunUsage,
		},
		{
			name:       "direct missing required flags",
			args:       []string{"demo", "parse"},
			wantStderr: "error: missing required flags --demo, --steamid, --out for \"demo parse\"\n",
		},
		{
			name:       "direct unexpected positional arg",
			args:       append(append([]string{"demo", "parse"}, validDemoParseFlags...), "tail"),
			wantStderr: "error: unexpected positional arg \"tail\" for \"demo parse\"; quote paths with spaces\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, tt.name+"-subcommands.jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantStderr; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
		})
	}
}

func TestZVBinaryCanonicalWorkflowFlagsAreScopedEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	valid := []struct {
		name string
		args []string
	}{
		{
			name: "demo parse optional flags",
			args: []string{"demo", "parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json", "--segment-mode", "utility", "--rules", "rules.json", "--verbose"},
		},
		{
			name: "utility audit optional format",
			args: []string{"utility", "audit", "--plan", "plan.json", "--lineup-catalog", "data/lineups", "--out", "audit.json", "--format", "json"},
		},
		{
			name: "shorts render optional flags",
			args: []string{"shorts", "render", "--recording-result", "recording-result.json", "--out", "shorts", "--killplan", "plan.json", "--publish-dir", "publish", "--preset", "smoke-lineups", "--effects", "effects.lua", "--effects-preset", "none", "--lineup-catalog", "lineups", "--segments", "seg-001", "--limit", "2", "--player-image", "player.png", "--player-key-color", "#000000", "--video-crf", "18", "--video-preset", "slow", "--ffmpeg", "ffmpeg.exe", "--ffprobe", "ffprobe.exe", "--hq-filters", "--audio-normalize", "--quality-checks", "--cover-sheets", "--temporal-smoothing", "--covers=false", "--no-covers", "--skip-existing", "--open-gallery", "--dry-run"},
		},
		{
			name: "analysis tactical data optional sample",
			args: []string{"analysis", "tactical-data", "--demo", "inferno.dem", "--out", "tactical.json", "--start", "1000", "--end", "2000", "--sample", "8"},
		},
		{
			name: "analysis viewer optional addr",
			args: []string{"analysis", "view", "--json", "analysis.json", "--addr", "127.0.0.1:0"},
		},
		{
			name: "pipeline optional tools",
			args: []string{"pipeline", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe", "--recorder", "zv-recorder.exe", "--composer", "zv-composer.exe", "--ffmpeg", "ffmpeg.exe", "--record-timeout", "1m", "--compose-timeout", "2m"},
		},
	}
	for _, tt := range valid {
		t.Run("valid/"+tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, "valid-"+strings.ReplaceAll(tt.name, " ", "-")+".jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr := runZVBinarySplitWithEnv(t, exe, tempDir, env, tt.args...)

			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			if got := len(readFakeSubcommandCalls(t, subcommandLogPath)); got != 1 {
				t.Fatalf("subcommand calls = %d, want 1", got)
			}
		})
	}

	invalid := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "demo parse rejects shorts boolean",
			args:       []string{"demo", "parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json", "--open-gallery"},
			wantStderr: "error: unknown flag --open-gallery for \"demo parse\"\n",
		},
		{
			name:       "shorts render rejects demo players value flag",
			args:       []string{"shorts", "render", "--recording-result", "recording-result.json", "--out", "shorts", "--contains", "zack"},
			wantStderr: "error: unknown flag --contains for \"shorts render\"\n",
		},
		{
			name:       "pipeline rejects unsupported dry run",
			args:       []string{"pipeline", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe", "--dry-run"},
			wantStderr: "error: unknown flag --dry-run for \"pipeline\"\n",
		},
		{
			name:       "workflow run demo parse rejects record boolean",
			args:       []string{"workflows", "run", "demo-parse", "--", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json", "--dry-run"},
			wantStderr: "error: unknown flag --dry-run for \"demo parse\"\n" + workflowsRunUsage,
		},
	}
	for _, tt := range invalid {
		t.Run("invalid/"+tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, "invalid-"+strings.ReplaceAll(tt.name, " ", "-")+".jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantStderr; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
		})
	}
}

func TestZVBinaryRecordRequiresCaptureToolsUnlessDryRunEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	valid := []struct {
		name           string
		args           []string
		wantExecutable string
		wantArgs       []string
	}{
		{
			name:           "direct dry run omits capture tools",
			args:           []string{"record", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run"},
			wantExecutable: executableName("zv-recorder"),
			wantArgs:       []string{"--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run"},
		},
		{
			name:           "workflow dry run true omits capture tools",
			args:           []string{"workflows", "run", "record", "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run=true"},
			wantExecutable: executableName("zv-recorder"),
			wantArgs:       []string{"--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run=true"},
		},
		{
			name:           "direct capture tools",
			args:           []string{"record", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"},
			wantExecutable: executableName("zv-recorder"),
			wantArgs:       []string{"--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"},
		},
	}
	for _, tt := range valid {
		t.Run("valid/"+tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, "valid-"+strings.ReplaceAll(tt.name, " ", "-")+".jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr := runZVBinarySplitWithEnv(t, exe, tempDir, env, tt.args...)

			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			calls := readFakeSubcommandCalls(t, subcommandLogPath)
			if got, want := len(calls), 1; got != want {
				t.Fatalf("subcommand calls = %d, want %d: %#v", got, want, calls)
			}
			if got, want := calls[0].Executable, tt.wantExecutable; got != want {
				t.Fatalf("executable = %q, want %q", got, want)
			}
			if got, want := strings.Join(calls[0].Args, "\x00"), strings.Join(tt.wantArgs, "\x00"); got != want {
				t.Fatalf("args = %#v, want %#v", calls[0].Args, tt.wantArgs)
			}
		})
	}

	invalid := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "direct missing both capture tools",
			args:       []string{"record", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording"},
			wantStderr: "error: missing required flags --hlae, --cs2 for \"record\" unless --dry-run is set\n",
		},
		{
			name:       "workflow dry run false missing both capture tools",
			args:       []string{"workflows", "run", "record", "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run=false"},
			wantStderr: "error: missing required flags --hlae, --cs2 for \"record\" unless --dry-run is set\n" + workflowsRunUsage,
		},
		{
			name:       "direct missing cs2",
			args:       []string{"record", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--hlae", "HLAE.exe"},
			wantStderr: "error: missing required flag --cs2 for \"record\" unless --dry-run is set\n",
		},
	}
	for _, tt := range invalid {
		t.Run("invalid/"+tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, "invalid-"+strings.ReplaceAll(tt.name, " ", "-")+".jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantStderr; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
		})
	}
}

func TestZVBinaryRecordWorkflowDiscoveryDocumentsCaptureContractEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	showText, showTextStderr := runZVBinarySplit(t, exe, tempDir, "workflows", "show", "record")
	if showTextStderr != "" {
		t.Fatalf("workflows show record stderr = %q, want empty", showTextStderr)
	}
	for _, want := range []string{
		"command: zv record --killplan <plan.json> --demo <demo.dem> --out <recording-dir> --hlae <HLAE.exe> --cs2 <cs2.exe>",
		"run_command: zv workflows run record",
	} {
		if !strings.Contains(showText, want) {
			t.Fatalf("workflows show record = %q, want %q", showText, want)
		}
	}

	showJSON, showJSONStderr := runZVBinarySplit(t, exe, tempDir, "workflows", "show", "record", "--format", "json")
	if showJSONStderr != "" {
		t.Fatalf("workflows show record json stderr = %q, want empty", showJSONStderr)
	}
	var shown workflowInfo
	if err := json.Unmarshal([]byte(showJSON), &shown); err != nil {
		t.Fatalf("unmarshal workflows show record json: %v\n%s", err, showJSON)
	}
	if got, want := shown.Command, "zv record --killplan <plan.json> --demo <demo.dem> --out <recording-dir> --hlae <HLAE.exe> --cs2 <cs2.exe>"; got != want {
		t.Fatalf("record workflow command = %q, want %q", got, want)
	}
	if got, want := shown.RunCommand, "zv workflows run record"; got != want {
		t.Fatalf("record workflow run_command = %q, want %q", got, want)
	}

	listJSON, listJSONStderr := runZVBinarySplit(t, exe, tempDir, "workflows", "list", "--format", "json")
	if listJSONStderr != "" {
		t.Fatalf("workflows list json stderr = %q, want empty", listJSONStderr)
	}
	var listed []workflowInfo
	if err := json.Unmarshal([]byte(listJSON), &listed); err != nil {
		t.Fatalf("unmarshal workflows list json: %v\n%s", err, listJSON)
	}
	var listedRecord workflowInfo
	for _, workflow := range listed {
		if workflow.Name == "record" {
			listedRecord = workflow
			break
		}
	}
	if listedRecord.Name == "" {
		t.Fatalf("workflows list json did not include record workflow")
	}
	if got, want := listedRecord.Command, shown.Command; got != want {
		t.Fatalf("listed record command = %q, want show command %q", got, want)
	}
	if got, want := listedRecord.RunCommand, shown.RunCommand; got != want {
		t.Fatalf("listed record run_command = %q, want show run_command %q", got, want)
	}

	subcommandLogPath := filepath.Join(tempDir, "record-discovery-dry-run.jsonl")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
	}
	stdout, stderr := runZVBinarySplitWithEnv(t, exe, tempDir, env, "workflows", "run", "record", "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run")
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	calls := readFakeSubcommandCalls(t, subcommandLogPath)
	if got, want := len(calls), 1; got != want {
		t.Fatalf("subcommand calls = %d, want %d: %#v", got, want, calls)
	}
	if got, want := calls[0].Executable, executableName("zv-recorder"); got != want {
		t.Fatalf("executable = %q, want %q", got, want)
	}
	if got, want := strings.Join(calls[0].Args, "\x00"), strings.Join([]string{"--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run"}, "\x00"); got != want {
		t.Fatalf("args = %#v, want dry-run record args", calls[0].Args)
	}

	captureLogPath := filepath.Join(tempDir, "record-discovery-capture.jsonl")
	captureEnv := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + captureLogPath,
	}
	discoveredArgs, ok := splitCommandFields(shown.RunCommand)
	if !ok {
		t.Fatalf("parse discovered run_command %q", shown.RunCommand)
	}
	discoveredArgs = append(discoveredArgs[1:], "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run=false", "--hlae", "HLAE.exe", "--cs2", "cs2.exe")
	captureStdout, captureStderr := runZVBinarySplitWithEnv(t, exe, tempDir, captureEnv, discoveredArgs...)
	if captureStdout != "" {
		t.Fatalf("capture stdout = %q, want empty", captureStdout)
	}
	if captureStderr != "" {
		t.Fatalf("capture stderr = %q, want empty", captureStderr)
	}
	captureCalls := readFakeSubcommandCalls(t, captureLogPath)
	if got, want := len(captureCalls), 1; got != want {
		t.Fatalf("capture subcommand calls = %d, want %d: %#v", got, want, captureCalls)
	}
	if got, want := captureCalls[0].Executable, executableName("zv-recorder"); got != want {
		t.Fatalf("capture executable = %q, want %q", got, want)
	}
	if got, want := strings.Join(captureCalls[0].Args, "\x00"), strings.Join([]string{"--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run=false", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"}, "\x00"); got != want {
		t.Fatalf("capture args = %#v, want capture record args", captureCalls[0].Args)
	}
}

func TestZVBinaryCanonicalWorkflowOptionalValueFlagsRequireValuesEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "direct demo players missing contains",
			args:       []string{"demo", "players", "--demo", "inferno.dem", "--contains"},
			wantStderr: "error: missing value for flag --contains for \"demo players\"\n",
		},
		{
			name:       "workflow demo players missing contains",
			args:       []string{"workflows", "run", "demo-players", "--", "--demo", "inferno.dem", "--contains"},
			wantStderr: "error: missing value for flag --contains for \"demo players\"\n" + workflowsRunUsage,
		},
		{
			name:       "direct record empty timeout",
			args:       []string{"record", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run", "--timeout="},
			wantStderr: "error: missing value for flag --timeout for \"record\"\n",
		},
		{
			name:       "workflow compose final missing ffmpeg",
			args:       []string{"workflows", "run", "compose-final", "--", "--recording-result", "recording-result.json", "--out", "final.mp4", "--dry-run", "--ffmpeg"},
			wantStderr: "error: missing value for flag --ffmpeg for \"compose final\"\n" + workflowsRunUsage,
		},
		{
			name:       "direct shorts render empty limit",
			args:       []string{"shorts", "render", "--recording-result", "recording-result.json", "--out", "shorts", "--limit="},
			wantStderr: "error: missing value for flag --limit for \"shorts render\"\n",
		},
		{
			name:       "workflow shorts render missing effects",
			args:       []string{"workflows", "run", "shorts-render", "--", "--recording-result", "recording-result.json", "--out", "shorts", "--effects"},
			wantStderr: "error: missing value for flag --effects for \"shorts render\"\n" + workflowsRunUsage,
		},
		{
			name:       "direct analysis tactical data empty sample",
			args:       []string{"analysis", "tactical-data", "--demo", "inferno.dem", "--out", "tactical.json", "--start", "1000", "--end", "2000", "--sample", ""},
			wantStderr: "error: missing value for flag --sample for \"analysis tactical-data\"\n",
		},
		{
			name:       "workflow analysis viewer missing addr",
			args:       []string{"workflows", "run", "analysis-viewer", "--", "--json", "analysis.json", "--addr"},
			wantStderr: "error: missing value for flag --addr for \"analysis view\"\n" + workflowsRunUsage,
		},
		{
			name:       "direct pipeline missing record timeout",
			args:       []string{"pipeline", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe", "--record-timeout"},
			wantStderr: "error: missing value for flag --record-timeout for \"pipeline\"\n",
		},
		{
			name:       "workflow pipeline empty compose timeout",
			args:       []string{"workflows", "run", "pipeline", "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe", "--compose-timeout="},
			wantStderr: "error: missing value for flag --compose-timeout for \"pipeline\"\n" + workflowsRunUsage,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, strings.ReplaceAll(tt.name, " ", "-")+".jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantStderr; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
		})
	}
}

func TestZVBinaryCanonicalWorkflowAllowsDashPrefixedValuesEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	tests := []struct {
		name           string
		args           []string
		wantExecutable string
		wantArgs       []string
	}{
		{
			name:           "direct demo parse dash prefixed demo path",
			args:           []string{"demo", "parse", "--demo", "-inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"parse", "--demo", "-inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json"},
		},
		{
			name:           "workflow record negative numeric option",
			args:           []string{"workflows", "run", "record", "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run", "--video-crf", "-1"},
			wantExecutable: executableName("zv-recorder"),
			wantArgs:       []string{"--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run", "--video-crf", "-1"},
		},
		{
			name:           "workflow analysis tactical data negative required tick",
			args:           []string{"workflows", "run", "analysis-tactical-data", "--", "--demo", "inferno.dem", "--out", "tactical.json", "--start", "-1", "--end", "2000"},
			wantExecutable: executableName("zv-tactical-data"),
			wantArgs:       []string{"--demo", "inferno.dem", "--out", "tactical.json", "--start", "-1", "--end", "2000"},
		},
		{
			name:           "direct shorts render negative numeric option",
			args:           []string{"shorts", "render", "--recording-result", "recording-result.json", "--out", "shorts", "--video-crf", "-1"},
			wantExecutable: executableName("zv-editor"),
			wantArgs:       []string{"--recording-result", "recording-result.json", "--out", "shorts", "--video-crf", "-1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, strings.ReplaceAll(tt.name, " ", "-")+".jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr := runZVBinarySplitWithEnv(t, exe, tempDir, env, tt.args...)

			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			calls := readFakeSubcommandCalls(t, subcommandLogPath)
			if got, want := len(calls), 1; got != want {
				t.Fatalf("subcommand calls = %d, want %d: %#v", got, want, calls)
			}
			if got, want := calls[0].Executable, tt.wantExecutable; got != want {
				t.Fatalf("executable = %q, want %q", got, want)
			}
			if got, want := strings.Join(calls[0].Args, "\x00"), strings.Join(tt.wantArgs, "\x00"); got != want {
				t.Fatalf("args = %#v, want %#v", calls[0].Args, tt.wantArgs)
			}
		})
	}
}

func TestZVBinaryCanonicalWorkflowRejectsSingleDashFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "direct demo parse single dash required flag",
			args:       []string{"demo", "parse", "-demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json"},
			wantStderr: "error: unknown flag -demo for \"demo parse\"\n",
		},
		{
			name:       "direct shorts render single dash option",
			args:       []string{"shorts", "render", "--recording-result", "recording-result.json", "--out", "shorts", "-limit", "1"},
			wantStderr: "error: unknown flag -limit for \"shorts render\"\n",
		},
		{
			name:       "workflow record single dash dry run",
			args:       []string{"workflows", "run", "record", "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "-dry-run"},
			wantStderr: "error: unknown flag -dry-run for \"record\"\n" + workflowsRunUsage,
		},
		{
			name:       "workflow pipeline short flag",
			args:       []string{"workflows", "run", "pipeline", "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe", "-x"},
			wantStderr: "error: unknown flag -x for \"pipeline\"\n" + workflowsRunUsage,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, strings.ReplaceAll(tt.name, " ", "-")+".jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantStderr; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
		})
	}
}

func TestZVBinaryCanonicalWorkflowBooleanEqualsValuesEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	valid := []struct {
		name           string
		args           []string
		wantExecutable string
		wantArgs       []string
	}{
		{
			name:           "direct demo parse verbose false",
			args:           []string{"demo", "parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json", "--verbose=false"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json", "--verbose=false"},
		},
		{
			name:           "workflow record dry run false",
			args:           []string{"workflows", "run", "record", "--", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run=false", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"},
			wantExecutable: executableName("zv-recorder"),
			wantArgs:       []string{"--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run=false", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"},
		},
		{
			name:           "direct shorts render open gallery zero",
			args:           []string{"shorts", "render", "--recording-result", "recording-result.json", "--out", "shorts", "--open-gallery=0", "--covers=True"},
			wantExecutable: executableName("zv-editor"),
			wantArgs:       []string{"--recording-result", "recording-result.json", "--out", "shorts", "--open-gallery=0", "--covers=True"},
		},
	}
	for _, tt := range valid {
		t.Run("valid/"+tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, "valid-"+strings.ReplaceAll(tt.name, " ", "-")+".jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr := runZVBinarySplitWithEnv(t, exe, tempDir, env, tt.args...)

			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			calls := readFakeSubcommandCalls(t, subcommandLogPath)
			if got, want := len(calls), 1; got != want {
				t.Fatalf("subcommand calls = %d, want %d: %#v", got, want, calls)
			}
			if got, want := calls[0].Executable, tt.wantExecutable; got != want {
				t.Fatalf("executable = %q, want %q", got, want)
			}
			if got, want := strings.Join(calls[0].Args, "\x00"), strings.Join(tt.wantArgs, "\x00"); got != want {
				t.Fatalf("args = %#v, want %#v", calls[0].Args, tt.wantArgs)
			}
		})
	}

	invalid := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "direct demo parse empty verbose",
			args:       []string{"demo", "parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json", "--verbose="},
			wantStderr: "error: invalid boolean value \"\" for flag --verbose for \"demo parse\"\n",
		},
		{
			name:       "direct record invalid dry run",
			args:       []string{"record", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run=maybe"},
			wantStderr: "error: invalid boolean value \"maybe\" for flag --dry-run for \"record\"\n",
		},
		{
			name:       "workflow shorts render invalid open gallery",
			args:       []string{"workflows", "run", "shorts-render", "--", "--recording-result", "recording-result.json", "--out", "shorts", "--open-gallery=maybe"},
			wantStderr: "error: invalid boolean value \"maybe\" for flag --open-gallery for \"shorts render\"\n" + workflowsRunUsage,
		},
	}
	for _, tt := range invalid {
		t.Run("invalid/"+tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, "invalid-"+strings.ReplaceAll(tt.name, " ", "-")+".jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantStderr; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
		})
	}
}

func TestZVBinaryCanonicalWorkflowBooleanFlagsRejectSeparateValuesEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	tests := []struct {
		name       string
		args       []string
		wantStderr string
	}{
		{
			name:       "direct demo parse verbose false",
			args:       []string{"demo", "parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "plan.json", "--verbose", "false"},
			wantStderr: "error: boolean flag --verbose for \"demo parse\" does not take separate value \"false\"; use --verbose=false\n",
		},
		{
			name:       "direct record dry run false",
			args:       []string{"record", "--killplan", "plan.json", "--demo", "inferno.dem", "--out", "recording", "--dry-run", "false"},
			wantStderr: "error: boolean flag --dry-run for \"record\" does not take separate value \"false\"; use --dry-run=false\n",
		},
		{
			name:       "workflow compose final dry run false",
			args:       []string{"workflows", "run", "compose-final", "--", "--recording-result", "recording-result.json", "--out", "final.mp4", "--dry-run", "false"},
			wantStderr: "error: boolean flag --dry-run for \"compose final\" does not take separate value \"false\"; use --dry-run=false\n" + workflowsRunUsage,
		},
		{
			name:       "workflow shorts render open gallery false",
			args:       []string{"workflows", "run", "shorts-render", "--", "--recording-result", "recording-result.json", "--out", "shorts", "--open-gallery", "false"},
			wantStderr: "error: boolean flag --open-gallery for \"shorts render\" does not take separate value \"false\"; use --open-gallery=false\n" + workflowsRunUsage,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, strings.ReplaceAll(tt.name, " ", "-")+".jsonl")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
			}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, tt.args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if got, want := stderr, tt.wantStderr; got != want {
				t.Fatalf("stderr = %q, want %q", got, want)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
		})
	}
}

func TestZVBinaryEveryWorkflowRunRejectsMissingRequiredArgsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-missing-required.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-missing-required-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := workflowRunCommandArgs(t, workflow)
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "missing required flag") {
				t.Fatalf("stderr = %q, want missing required flag error", stderr)
			}
			if !strings.Contains(stderr, workflowsRunUsage) {
				t.Fatalf("stderr = %q, want workflows run usage", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryWorkflowRunRejectsEmptyForwardedRequiredArgsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-empty-forwarded-required.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-empty-forwarded-required-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := workflowRunCommandArgs(t, workflow)
			args = append(args, "--")
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "missing required flag") {
				t.Fatalf("stderr = %q, want missing required flag error", stderr)
			}
			if !strings.Contains(stderr, workflowsRunUsage) {
				t.Fatalf("stderr = %q, want workflows run usage", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryWorkflowRunRejectsEmptyEqualsRequiredFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-empty-equals-required.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-empty-equals-required-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := workflowRunCommandArgs(t, workflow)
			args = append(args, emptyEqualsRequiredFlag(t, workflowRunSampleForwardedArgs(t, workflow, galleryPath), required[0])...)
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "missing required flag") {
				t.Fatalf("stderr = %q, want missing required flag error", stderr)
			}
			if !strings.Contains(stderr, workflowsRunUsage) {
				t.Fatalf("stderr = %q, want workflows run usage", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryWorkflowRunRejectsEmptySeparateRequiredFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-empty-separate-required.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-empty-separate-required-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := workflowRunCommandArgs(t, workflow)
			args = append(args, emptySeparateRequiredFlag(t, workflowRunSampleForwardedArgs(t, workflow, galleryPath), required[0])...)
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "missing required flag") {
				t.Fatalf("stderr = %q, want missing required flag error", stderr)
			}
			if !strings.Contains(stderr, workflowsRunUsage) {
				t.Fatalf("stderr = %q, want workflows run usage", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryWorkflowRunRejectsDuplicateRequiredFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-duplicate-required.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-duplicate-required-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := workflowRunCommandArgs(t, workflow)
			forwarded := duplicateFlagValue(t, workflowRunSampleForwardedArgs(t, workflow, galleryPath), required[0])
			args = append(args, forwarded...)
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if want := "duplicate flag " + required[0]; !strings.Contains(stderr, want) {
				t.Fatalf("stderr = %q, want %q", stderr, want)
			}
			if !strings.Contains(stderr, workflowsRunUsage) {
				t.Fatalf("stderr = %q, want workflows run usage", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryWorkflowRunRejectsUnexpectedPositionalArgsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-unexpected-positional.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-unexpected-positional-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := workflowRunCommandArgs(t, workflow)
			args = append(args, append(workflowRunSampleForwardedArgs(t, workflow, galleryPath), "unquoted path tail")...)
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `unexpected positional arg "unquoted path tail"`) {
				t.Fatalf("stderr = %q, want unexpected positional arg error", stderr)
			}
			if !strings.Contains(stderr, workflowsRunUsage) {
				t.Fatalf("stderr = %q, want workflows run usage", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryWorkflowRunRejectsUnknownFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-unknown-flag.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-unknown-flag-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := workflowRunCommandArgs(t, workflow)
			args = append(args, append(workflowRunSampleForwardedArgs(t, workflow, galleryPath), "--zv-unknown")...)
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `unknown flag --zv-unknown`) {
				t.Fatalf("stderr = %q, want unknown flag error", stderr)
			}
			if !strings.Contains(stderr, workflowsRunUsage) {
				t.Fatalf("stderr = %q, want workflows run usage", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryWorkflowRunRejectsUnknownEqualsFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-unknown-equals-flag.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-unknown-equals-flag-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := workflowRunCommandArgs(t, workflow)
			args = append(args, append(workflowRunSampleForwardedArgs(t, workflow, galleryPath), "--zv-unknown=value")...)
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `unknown flag --zv-unknown`) {
				t.Fatalf("stderr = %q, want unknown flag error", stderr)
			}
			if !strings.Contains(stderr, workflowsRunUsage) {
				t.Fatalf("stderr = %q, want workflows run usage", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryDirectWorkflowRejectsMissingRequiredArgsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-direct-missing-required.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-direct-missing-required-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, workflow.RunArgs...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "missing required flag") {
				t.Fatalf("stderr = %q, want missing required flag error", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryDirectWorkflowAcceptsEqualsRequiredFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	subcommandLogPath := filepath.Join(tempDir, "direct-equals-required-subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "direct-equals-required-open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	var wantSubcommandCalls, wantOpenPathCalls int
	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			args := equalsRequiredFlags(t, workflowDirectSampleArgs(t, workflow, galleryPath), required)
			runZVBinaryWithEnv(t, exe, tempDir, env, args...)
			switch {
			case workflow.RunArgs[0] == "gallery":
				wantOpenPathCalls++
			case workflow.RunArgs[0] == "skills" || workflow.RunArgs[0] == "workflows" || workflow.RunArgs[0] == "check":
			default:
				wantSubcommandCalls++
			}
		})
	}
	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryEveryDirectWorkflowRejectsEmptyEqualsRequiredFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-direct-empty-equals-required.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-direct-empty-equals-required-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := emptyEqualsRequiredFlag(t, workflowDirectSampleArgs(t, workflow, galleryPath), required[0])
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "missing required flag") {
				t.Fatalf("stderr = %q, want missing required flag error", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryDirectWorkflowRejectsEmptySeparateRequiredFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-direct-empty-separate-required.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-direct-empty-separate-required-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := emptySeparateRequiredFlag(t, workflowDirectSampleArgs(t, workflow, galleryPath), required[0])
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, "missing required flag") {
				t.Fatalf("stderr = %q, want missing required flag error", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryDirectWorkflowRejectsUnknownFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-direct-unknown-flag.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-direct-unknown-flag-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := append(workflowDirectSampleArgs(t, workflow, galleryPath), "--zv-unknown")
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `unknown flag --zv-unknown`) {
				t.Fatalf("stderr = %q, want unknown flag error", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryDirectWorkflowRejectsUnknownEqualsFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-direct-unknown-equals-flag.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-direct-unknown-equals-flag-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := append(workflowDirectSampleArgs(t, workflow, galleryPath), "--zv-unknown=value")
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `unknown flag --zv-unknown`) {
				t.Fatalf("stderr = %q, want unknown flag error", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryDirectWorkflowRejectsUnexpectedPositionalArgsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-direct-unexpected-positional.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-direct-unexpected-positional-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := append(workflowDirectSampleArgs(t, workflow, galleryPath), "unquoted path tail")
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, `unexpected positional arg "unquoted path tail"`) {
				t.Fatalf("stderr = %q, want unexpected positional arg error", stderr)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryEveryDirectWorkflowRejectsDuplicateRequiredFlagsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		required := requiredFlagsForRunArgs(workflow.RunArgs...)
		if len(required) == 0 {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			subcommandLogPath := filepath.Join(tempDir, workflow.Name+"-direct-duplicate-required.jsonl")
			openPathLogPath := filepath.Join(tempDir, workflow.Name+"-direct-duplicate-required-open.txt")
			env := []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
				"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
			}

			args := duplicateFlagValue(t, workflowDirectSampleArgs(t, workflow, galleryPath), required[0])
			stdout, stderr, code := runZVBinaryFailureSplitWithEnv(t, exe, tempDir, env, args...)

			if got, want := code, exitInvalidArgs; got != want {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if want := "duplicate flag " + required[0]; !strings.Contains(stderr, want) {
				t.Fatalf("stderr = %q, want %q", stderr, want)
			}
			assertPathDoesNotExist(t, subcommandLogPath)
			assertPathDoesNotExist(t, openPathLogPath)
		})
	}
}

func TestZVBinaryWorkflowListJSONRunCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	if err := os.MkdirAll(filepath.Join(tempDir, "gallery"), 0o755); err != nil {
		t.Fatalf("mkdir gallery: %v", err)
	}
	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	listJSON, stderr := runZVBinarySplit(t, exe, tempDir, "workflows", "list", "--format", "json")
	if stderr != "" {
		t.Fatalf("workflows list json wrote stderr %q", stderr)
	}
	var listed []workflowInfo
	if err := json.Unmarshal([]byte(listJSON), &listed); err != nil {
		t.Fatalf("unmarshal workflows list json: %v\n%s", err, listJSON)
	}

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	seen := make(map[string]bool)
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, listedWorkflow := range listed {
		catalogWorkflow, ok := findWorkflow(listedWorkflow.Name)
		if !ok {
			t.Fatalf("workflow %q from list json is not cataloged", listedWorkflow.Name)
		}
		args := workflowRunCommandArgs(t, listedWorkflow)
		args = append(args, workflowRunSampleForwardedArgs(t, catalogWorkflow, galleryPath)...)
		runZVBinaryWithEnv(t, exe, tempDir, env, args...)
		seen[listedWorkflow.Name] = true
		switch {
		case catalogWorkflow.RunArgs[0] == "gallery":
			wantOpenPathCalls++
		case catalogWorkflow.RunArgs[0] == "skills" || catalogWorkflow.RunArgs[0] == "workflows" || catalogWorkflow.RunArgs[0] == "check":
		default:
			wantSubcommandCalls++
		}
	}

	for _, workflow := range workflowCatalog() {
		if !seen[workflow.Name] {
			t.Fatalf("workflows list json did not expose executable run_command for %q; saw %#v", workflow.Name, seen)
		}
	}
	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryWorkflowListAndShowJSONMatchCatalogEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	listJSON, stderr := runZVBinarySplit(t, exe, tempDir, "workflows", "list", "--format", "json")
	if stderr != "" {
		t.Fatalf("workflows list json wrote stderr %q", stderr)
	}
	var listed []workflowInfo
	if err := json.Unmarshal([]byte(listJSON), &listed); err != nil {
		t.Fatalf("unmarshal workflows list json: %v\n%s", err, listJSON)
	}

	catalog := workflowCatalog()
	if got, want := len(listed), len(catalog); got != want {
		t.Fatalf("workflows list json count = %d, want %d", got, want)
	}
	for i, catalogWorkflow := range catalog {
		listedWorkflow := listed[i]
		assertWorkflowDiscoveryMatches(t, "workflows list json", listedWorkflow, catalogWorkflow)

		showJSON, stderr := runZVBinarySplit(t, exe, tempDir, "workflows", "show", catalogWorkflow.Name, "--format", "json")
		if stderr != "" {
			t.Fatalf("workflows show json for %s wrote stderr %q", catalogWorkflow.Name, stderr)
		}
		var shown workflowInfo
		if err := json.Unmarshal([]byte(showJSON), &shown); err != nil {
			t.Fatalf("unmarshal workflows show json for %s: %v\n%s", catalogWorkflow.Name, err, showJSON)
		}
		assertWorkflowDiscoveryMatches(t, "workflows show json", shown, catalogWorkflow)

		if listedWorkflow.Name != shown.Name ||
			listedWorkflow.Description != shown.Description ||
			listedWorkflow.Command != shown.Command ||
			listedWorkflow.RunCommand != shown.RunCommand {
			t.Fatalf("workflow discovery mismatch for %s\nlist: %#v\nshow: %#v", catalogWorkflow.Name, listedWorkflow, shown)
		}
	}
}

func TestZVBinaryWorkflowDiscoveryJSONRunCommandsMatchDirectCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	if err := os.MkdirAll(filepath.Join(tempDir, "gallery"), 0o755); err != nil {
		t.Fatalf("mkdir gallery: %v", err)
	}
	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	listJSON, stderr := runZVBinarySplit(t, exe, tempDir, "workflows", "list", "--format", "json")
	if stderr != "" {
		t.Fatalf("workflows list json wrote stderr %q", stderr)
	}
	var listed []workflowInfo
	if err := json.Unmarshal([]byte(listJSON), &listed); err != nil {
		t.Fatalf("unmarshal workflows list json: %v\n%s", err, listJSON)
	}

	seenList := make(map[string]bool)
	for i, discovered := range listed {
		catalogWorkflow, ok := findWorkflow(discovered.Name)
		if !ok {
			t.Fatalf("workflow %q from list json is not cataloged", discovered.Name)
		}
		if !workflowDirectDocCommandIsComparable(catalogWorkflow) {
			continue
		}
		seenList[catalogWorkflow.Name] = true
		assertDiscoveredWorkflowRunMatchesDirect(t, exe, tempDir, "list", i, discovered, catalogWorkflow, galleryPath)
	}

	seenShow := make(map[string]bool)
	for i, catalogWorkflow := range workflowCatalog() {
		showJSON, stderr := runZVBinarySplit(t, exe, tempDir, "workflows", "show", catalogWorkflow.Name, "--format", "json")
		if stderr != "" {
			t.Fatalf("workflows show json for %s wrote stderr %q", catalogWorkflow.Name, stderr)
		}
		var shown workflowInfo
		if err := json.Unmarshal([]byte(showJSON), &shown); err != nil {
			t.Fatalf("unmarshal workflows show json for %s: %v\n%s", catalogWorkflow.Name, err, showJSON)
		}
		if !workflowDirectDocCommandIsComparable(catalogWorkflow) {
			continue
		}
		seenShow[catalogWorkflow.Name] = true
		assertDiscoveredWorkflowRunMatchesDirect(t, exe, tempDir, "show", i, shown, catalogWorkflow, galleryPath)
	}

	for _, workflow := range workflowCatalog() {
		if !workflowDirectDocCommandIsComparable(workflow) {
			continue
		}
		if !seenList[workflow.Name] {
			t.Fatalf("workflows list json did not compare run_command for workflow %q", workflow.Name)
		}
		if !seenShow[workflow.Name] {
			t.Fatalf("workflows show json did not compare run_command for workflow %q", workflow.Name)
		}
	}
}

func TestZVBinaryWorkflowShowJSONRunCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	if err := os.MkdirAll(filepath.Join(tempDir, "gallery"), 0o755); err != nil {
		t.Fatalf("mkdir gallery: %v", err)
	}
	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	seen := make(map[string]bool)
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, catalogWorkflow := range workflowCatalog() {
		showJSON, stderr := runZVBinarySplit(t, exe, tempDir, "workflows", "show", catalogWorkflow.Name, "--format", "json")
		if stderr != "" {
			t.Fatalf("workflows show json for %s wrote stderr %q", catalogWorkflow.Name, stderr)
		}
		var shown workflowInfo
		if err := json.Unmarshal([]byte(showJSON), &shown); err != nil {
			t.Fatalf("unmarshal workflows show json for %s: %v\n%s", catalogWorkflow.Name, err, showJSON)
		}
		if shown.Name != catalogWorkflow.Name || shown.RunCommand == "" {
			t.Fatalf("workflows show json for %s = %#v, want matching name and non-empty run_command", catalogWorkflow.Name, shown)
		}

		args := workflowRunCommandArgs(t, shown)
		args = append(args, workflowRunSampleForwardedArgs(t, catalogWorkflow, galleryPath)...)
		runZVBinaryWithEnv(t, exe, tempDir, env, args...)
		seen[shown.Name] = true
		switch {
		case catalogWorkflow.RunArgs[0] == "gallery":
			wantOpenPathCalls++
		case catalogWorkflow.RunArgs[0] == "skills" || catalogWorkflow.RunArgs[0] == "workflows" || catalogWorkflow.RunArgs[0] == "check":
		default:
			wantSubcommandCalls++
		}
	}

	for _, workflow := range workflowCatalog() {
		if !seen[workflow.Name] {
			t.Fatalf("workflows show json did not expose executable run_command for %q; saw %#v", workflow.Name, seen)
		}
	}
	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryCanonicalWorkflowDelegatesEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	logPath := filepath.Join(tempDir, "calls.jsonl")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + logPath,
	}
	tests := []struct {
		name           string
		args           []string
		wantExecutable string
		wantArgs       []string
	}{
		{
			name:           "demo parse",
			args:           []string{"demo", "parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--segment-mode", "utility", "--out", "run/plan.json"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--segment-mode", "utility", "--out", "run/plan.json"},
		},
		{
			name:           "demo players",
			args:           []string{"demo", "players", "--demo", "inferno.dem", "--contains", "maaryy"},
			wantExecutable: executableName("zv-demo-players"),
			wantArgs:       []string{"--demo", "inferno.dem", "--contains", "maaryy"},
		},
		{
			name:           "utility audit",
			args:           []string{"utility", "audit", "--plan", "run/plan.json", "--lineup-catalog", "data/lineups", "--out", "run/utility-audit.csv"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"utility-audit", "--plan", "run/plan.json", "--lineup-catalog", "data/lineups", "--out", "run/utility-audit.csv"},
		},
		{
			name:           "record",
			args:           []string{"record", "--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/recording", "--dry-run"},
			wantExecutable: executableName("zv-recorder"),
			wantArgs:       []string{"--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/recording", "--dry-run"},
		},
		{
			name:           "compose final",
			args:           []string{"compose", "final", "--recording-result", "run/recording/recording-result.json", "--out", "run/final.mp4", "--dry-run"},
			wantExecutable: executableName("zv-composer"),
			wantArgs:       []string{"--recording-result", "run/recording/recording-result.json", "--out", "run/final.mp4", "--dry-run"},
		},
		{
			name:           "shorts render",
			args:           []string{"shorts", "render", "--recording-result", "run/recording/recording-result.json", "--killplan", "run/plan.json", "--out", "run/shorts", "--preset", "smoke-lineups"},
			wantExecutable: executableName("zv-editor"),
			wantArgs:       []string{"--recording-result", "run/recording/recording-result.json", "--killplan", "run/plan.json", "--out", "run/shorts", "--preset", "smoke-lineups"},
		},
		{
			name:           "analysis tactical data",
			args:           []string{"analysis", "tactical-data", "--demo", "inferno.dem", "--out", "run/tactical.json", "--start", "1000", "--end", "2000"},
			wantExecutable: executableName("zv-tactical-data"),
			wantArgs:       []string{"--demo", "inferno.dem", "--out", "run/tactical.json", "--start", "1000", "--end", "2000"},
		},
		{
			name:           "analysis view",
			args:           []string{"analysis", "view", "--json", "run/analysis.json", "--addr", "127.0.0.1:0"},
			wantExecutable: executableName("zv-analysis-viewer"),
			wantArgs:       []string{"--json", "run/analysis.json", "--addr", "127.0.0.1:0"},
		},
		{
			name:           "pipeline",
			args:           []string{"pipeline", "--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"},
			wantExecutable: executableName("zv-pipeline"),
			wantArgs:       []string{"--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"},
		},
		{
			name:           "serve",
			args:           []string{"serve"},
			wantExecutable: executableName("zv-orchestrator"),
			wantArgs:       nil,
		},
		{
			name:           "workflow run demo parse",
			args:           []string{"workflows", "run", "demo-parse", "--", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "run/plan.json"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "run/plan.json"},
		},
		{
			name:           "workflow run demo players",
			args:           []string{"workflows", "run", "demo-players", "--", "--demo", "inferno.dem"},
			wantExecutable: executableName("zv-demo-players"),
			wantArgs:       []string{"--demo", "inferno.dem"},
		},
		{
			name:           "workflow run utility audit",
			args:           []string{"workflows", "run", "utility-audit", "--", "--plan", "run/plan.json", "--lineup-catalog", "data/lineups", "--out", "run/utility-audit.csv"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"utility-audit", "--plan", "run/plan.json", "--lineup-catalog", "data/lineups", "--out", "run/utility-audit.csv"},
		},
		{
			name:           "workflow run record",
			args:           []string{"workflows", "run", "record", "--", "--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/recording", "--dry-run"},
			wantExecutable: executableName("zv-recorder"),
			wantArgs:       []string{"--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/recording", "--dry-run"},
		},
		{
			name:           "workflow run compose final",
			args:           []string{"workflows", "run", "compose-final", "--", "--recording-result", "run/recording/recording-result.json", "--out", "run/final.mp4", "--dry-run"},
			wantExecutable: executableName("zv-composer"),
			wantArgs:       []string{"--recording-result", "run/recording/recording-result.json", "--out", "run/final.mp4", "--dry-run"},
		},
		{
			name:           "workflow run shorts render",
			args:           []string{"workflows", "run", "shorts-render", "--", "--recording-result", "run/recording/recording-result.json", "--out", "run/shorts"},
			wantExecutable: executableName("zv-editor"),
			wantArgs:       []string{"--recording-result", "run/recording/recording-result.json", "--out", "run/shorts"},
		},
		{
			name:           "workflow run analysis tactical data",
			args:           []string{"workflows", "run", "analysis-tactical-data", "--", "--demo", "inferno.dem", "--out", "run/tactical.json", "--start", "1000", "--end", "2000"},
			wantExecutable: executableName("zv-tactical-data"),
			wantArgs:       []string{"--demo", "inferno.dem", "--out", "run/tactical.json", "--start", "1000", "--end", "2000"},
		},
		{
			name:           "workflow run analysis view",
			args:           []string{"workflows", "run", "analysis-viewer", "--", "--json", "run/analysis.json"},
			wantExecutable: executableName("zv-analysis-viewer"),
			wantArgs:       []string{"--json", "run/analysis.json"},
		},
		{
			name:           "workflow run pipeline",
			args:           []string{"workflows", "run", "pipeline", "--", "--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"},
			wantExecutable: executableName("zv-pipeline"),
			wantArgs:       []string{"--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"},
		},
		{
			name:           "workflow run serve",
			args:           []string{"workflows", "run", "serve"},
			wantExecutable: executableName("zv-orchestrator"),
			wantArgs:       nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runZVBinaryWithEnv(t, exe, tempDir, env, tt.args...)
		})
	}

	calls := readFakeSubcommandCalls(t, logPath)
	if got, want := len(calls), len(tests); got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, calls)
	}
	for i, tt := range tests {
		call := calls[i]
		if got, want := call.Executable, tt.wantExecutable; got != want {
			t.Fatalf("call %d executable = %q, want %q", i, got, want)
		}
		if got, want := strings.Join(call.Args, "\x00"), strings.Join(tt.wantArgs, "\x00"); got != want {
			t.Fatalf("call %d args = %#v, want %#v", i, call.Args, tt.wantArgs)
		}
	}
}

func TestZVBinaryWorkflowRunsMatchDirectDelegationEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	for _, workflow := range workflowCatalog() {
		if !workflowDelegatesExternally(workflow) {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			directLogPath := filepath.Join(tempDir, workflow.Name+"-direct.jsonl")
			runLogPath := filepath.Join(tempDir, workflow.Name+"-run.jsonl")

			directArgs := workflowDirectSampleArgs(t, workflow, galleryPath)
			runZVBinaryWithEnv(t, exe, tempDir, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + directLogPath,
			}, directArgs...)

			runArgs := workflowRunCommandArgs(t, workflow)
			runArgs = append(runArgs, workflowRunSampleForwardedArgs(t, workflow, galleryPath)...)
			runZVBinaryWithEnv(t, exe, tempDir, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + runLogPath,
			}, runArgs...)

			directCalls := readFakeSubcommandCalls(t, directLogPath)
			runCalls := readFakeSubcommandCalls(t, runLogPath)
			if got, want := len(directCalls), 1; got != want {
				t.Fatalf("direct calls len = %d, want %d: %#v", got, want, directCalls)
			}
			if got, want := len(runCalls), 1; got != want {
				t.Fatalf("workflow run calls len = %d, want %d: %#v", got, want, runCalls)
			}
			if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
				t.Fatalf("workflow run executable = %q, want direct executable %q", got, want)
			}
			if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
				t.Fatalf("workflow run args = %#v, want direct args %#v", runCalls[0].Args, directCalls[0].Args)
			}
		})
	}
}

func TestZVBinaryWorkflowRunPreservesDelegatedStdioEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	input := "demo stdin payload\n"
	directStdout, directStderr := runZVBinarySplitWithEnvAndInput(t, exe, tempDir, []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + filepath.Join(tempDir, "direct-stdio.jsonl"),
		"ZV_FAKE_SUBCOMMAND_ECHO_STDIO=1",
	}, input, "demo", "parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "run/plan.json")
	runStdout, runStderr := runZVBinarySplitWithEnvAndInput(t, exe, tempDir, []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + filepath.Join(tempDir, "run-stdio.jsonl"),
		"ZV_FAKE_SUBCOMMAND_ECHO_STDIO=1",
	}, input, "workflows", "run", "demo-parse", "--", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "run/plan.json")

	if got, want := runStdout, directStdout; got != want {
		t.Fatalf("workflow run stdout = %q, want direct stdout %q", got, want)
	}
	if got, want := runStderr, directStderr; got != want {
		t.Fatalf("workflow run stderr = %q, want direct stderr %q", got, want)
	}
	if !strings.Contains(runStdout, input) {
		t.Fatalf("workflow run stdout = %q, want echoed stdin %q", runStdout, input)
	}
	if !strings.Contains(runStderr, executableName("zv-parser")) {
		t.Fatalf("workflow run stderr = %q, want delegated stderr marker", runStderr)
	}
}

func TestZVBinaryWorkflowHelpMatchesDirectDelegationEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	for _, workflow := range workflowCatalog() {
		if !workflowHelpDelegatesExternally(workflow) {
			continue
		}
		t.Run(workflow.Name, func(t *testing.T) {
			directLogPath := filepath.Join(tempDir, workflow.Name+"-direct-help.jsonl")
			runLogPath := filepath.Join(tempDir, workflow.Name+"-run-help.jsonl")

			directArgs := append([]string(nil), workflow.RunArgs...)
			directArgs = append(directArgs, "--help")
			runZVBinaryWithEnv(t, exe, tempDir, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + directLogPath,
			}, directArgs...)

			runArgs := workflowRunCommandArgs(t, workflow)
			runArgs = append(runArgs, "--", "--help")
			runZVBinaryWithEnv(t, exe, tempDir, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + runLogPath,
			}, runArgs...)

			directCalls := readFakeSubcommandCalls(t, directLogPath)
			runCalls := readFakeSubcommandCalls(t, runLogPath)
			if got, want := len(directCalls), 1; got != want {
				t.Fatalf("direct help calls len = %d, want %d: %#v", got, want, directCalls)
			}
			if got, want := len(runCalls), 1; got != want {
				t.Fatalf("workflow help calls len = %d, want %d: %#v", got, want, runCalls)
			}
			if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
				t.Fatalf("workflow help executable = %q, want direct executable %q", got, want)
			}
			if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
				t.Fatalf("workflow help args = %#v, want direct args %#v", runCalls[0].Args, directCalls[0].Args)
			}
		})
	}
}

func TestZVBinaryWorkflowHelpAliasesMatchDirectDelegationEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	for _, alias := range []string{"-h", "help"} {
		for _, workflow := range workflowCatalog() {
			if !workflowHelpDelegatesExternally(workflow) {
				continue
			}
			t.Run(workflow.Name+"/"+alias, func(t *testing.T) {
				directLogPath := filepath.Join(tempDir, workflow.Name+"-"+strings.TrimPrefix(alias, "-")+"-direct-help-alias.jsonl")
				runLogPath := filepath.Join(tempDir, workflow.Name+"-"+strings.TrimPrefix(alias, "-")+"-run-help-alias.jsonl")
				directArgs := append([]string(nil), workflow.RunArgs...)
				directArgs = append(directArgs, alias)
				runArgs := workflowRunCommandArgs(t, workflow)
				runArgs = append(runArgs, "--", alias)

				runZVBinaryWithEnv(t, exe, tempDir, []string{
					"ZV_FAKE_SUBCOMMAND=1",
					"ZV_FAKE_SUBCOMMAND_LOG=" + directLogPath,
				}, directArgs...)
				runZVBinaryWithEnv(t, exe, tempDir, []string{
					"ZV_FAKE_SUBCOMMAND=1",
					"ZV_FAKE_SUBCOMMAND_LOG=" + runLogPath,
				}, runArgs...)

				directCalls := readFakeSubcommandCalls(t, directLogPath)
				runCalls := readFakeSubcommandCalls(t, runLogPath)
				if got, want := len(directCalls), 1; got != want {
					t.Fatalf("direct calls = %d, want %d: %#v", got, want, directCalls)
				}
				if got, want := len(runCalls), 1; got != want {
					t.Fatalf("workflow run calls = %d, want %d: %#v", got, want, runCalls)
				}
				if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
					t.Fatalf("workflow run executable = %q, want direct executable %q", got, want)
				}
				if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
					t.Fatalf("workflow run args = %#v, want direct args %#v", runCalls[0].Args, directCalls[0].Args)
				}
			})
		}
	}
}

func TestZVBinaryWorkflowRunsMatchInternalCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	exe := buildZVBinary(t, tempDir)

	galleryPath := filepath.Join(tempDir, "gallery", "index.html")
	writeFile(t, galleryPath, "<!doctype html><title>gallery</title>\n")

	tests := []struct {
		name       string
		directArgs []string
		runArgs    []string
	}{
		{
			name:       "gallery-open",
			directArgs: []string{"gallery", "open", "--path", galleryPath},
			runArgs:    []string{"workflows", "run", "gallery-open", "--", "--path", galleryPath},
		},
		{
			name:       "skills-check",
			directArgs: []string{"skills", "check", "--format", "json"},
			runArgs:    []string{"workflows", "run", "skills-check", "--", "--format", "json"},
		},
		{
			name:       "workflows-check",
			directArgs: []string{"workflows", "check", "--format", "json"},
			runArgs:    []string{"workflows", "run", "workflows-check", "--", "--format", "json"},
		},
		{
			name:       "project-check",
			directArgs: []string{"check", "--format", "json"},
			runArgs:    []string{"workflows", "run", "project-check", "--", "--format", "json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directOpenLog := filepath.Join(tempDir, tt.name+"-direct-open.txt")
			runOpenLog := filepath.Join(tempDir, tt.name+"-run-open.txt")

			directOut := runZVBinaryWithEnv(t, exe, tempDir, []string{
				"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
			}, tt.directArgs...)
			runOut := runZVBinaryWithEnv(t, exe, tempDir, []string{
				"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
			}, tt.runArgs...)

			if got, want := runOut, directOut; got != want {
				t.Fatalf("workflow run output = %q, want direct output %q", got, want)
			}
			if tt.name != "gallery-open" {
				return
			}
			if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
				t.Fatalf("workflow run open path log = %q, want direct log %q", got, want)
			}
		})
	}
}

func TestZVBinaryRepoSkillWorkflowRunsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := repoSkillWorkflowRunCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no repo skill workflow run commands found")
	}

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	seen := make(map[string]bool)
	wantWorkflows := map[string]bool{
		"demo-parse":    true,
		"utility-audit": true,
		"record":        true,
		"shorts-render": true,
		"gallery-open":  true,
	}
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, command := range commands {
		workflowName := command[2]
		seen[workflowName] = true
		workflow, ok := findWorkflow(workflowName)
		if !ok {
			t.Fatalf("workflow %q from repo skill is not cataloged", workflowName)
		}
		if len(workflow.RunArgs) >= 2 && workflow.RunArgs[0] == "gallery" && workflow.RunArgs[1] == "open" {
			wantOpenPathCalls++
		} else {
			wantSubcommandCalls++
		}
		runZVBinaryWithEnv(t, exe, root, env, command...)
	}

	for workflowName := range wantWorkflows {
		if !seen[workflowName] {
			t.Fatalf("repo skills do not exercise workflow %q; saw %#v", workflowName, seen)
		}
	}

	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryRepoSkillRequiredWorkflowRunsBySkillEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := repoSkillWorkflowRunCommandsBySkill(t, root)
	if len(commands) == 0 {
		t.Fatalf("no repo skill workflow run commands found")
	}

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	seenBySkill := make(map[string]map[string]bool)
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, entry := range commands {
		workflowName := entry.command[2]
		workflow, ok := findWorkflow(workflowName)
		if !ok {
			t.Fatalf("workflow %q from repo skill %s is not cataloged", workflowName, entry.skillName)
		}
		if seenBySkill[entry.skillName] == nil {
			seenBySkill[entry.skillName] = make(map[string]bool)
		}
		seenBySkill[entry.skillName][workflowName] = true
		if len(workflow.RunArgs) >= 2 && workflow.RunArgs[0] == "gallery" && workflow.RunArgs[1] == "open" {
			wantOpenPathCalls++
		} else {
			wantSubcommandCalls++
		}
		runZVBinaryWithEnv(t, exe, root, env, entry.command...)
	}

	for skillName, requiredWorkflows := range skillWorkflowRequirementMap() {
		if _, ok := seenBySkill[skillName]; !ok {
			t.Fatalf("repo skill %q did not execute any workflow runs; saw %#v", skillName, seenBySkill)
		}
		for _, workflowName := range requiredWorkflows {
			if !seenBySkill[skillName][workflowName] {
				t.Fatalf("repo skill %q did not execute required workflow %q; saw %#v", skillName, workflowName, seenBySkill[skillName])
			}
		}
	}

	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryRepoSkillWorkflowRunsMatchDirectCommandsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := repoSkillWorkflowRunCommandsBySkill(t, root)
	if len(commands) == 0 {
		t.Fatalf("no repo skill workflow run commands found")
	}

	seenBySkill := make(map[string]map[string]bool)
	for i, entry := range commands {
		if len(entry.command) < 3 {
			t.Fatalf("repo skill %s workflow run command = %#v, want workflows run <name>", entry.skillName, entry.command)
		}
		workflow, ok := findWorkflow(entry.command[2])
		if !ok {
			t.Fatalf("workflow %q from repo skill %s is not cataloged", entry.command[2], entry.skillName)
		}
		if !workflowDirectDocCommandIsComparable(workflow) {
			continue
		}
		if seenBySkill[entry.skillName] == nil {
			seenBySkill[entry.skillName] = make(map[string]bool)
		}
		seenBySkill[entry.skillName][workflow.Name] = true

		t.Run(fmt.Sprintf("%02d/%s/%s", i, entry.skillName, workflow.Name), func(t *testing.T) {
			directArgs := directArgsForWorkflowRunDocCommand(t, workflow, entry.command)
			runSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-%s-skill-run.jsonl", i, entry.skillName, workflow.Name))
			directSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-%s-skill-direct.jsonl", i, entry.skillName, workflow.Name))
			runOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-%s-skill-run-open.txt", i, entry.skillName, workflow.Name))
			directOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-%s-skill-direct-open.txt", i, entry.skillName, workflow.Name))

			runOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + runSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
			}, entry.command...)
			directOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + directSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
			}, directArgs...)

			if got, want := runOut, directOut; got != want {
				t.Fatalf("repo skill workflow run output = %q, want direct output %q", got, want)
			}
			if workflow.Name == "gallery-open" {
				if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
					t.Fatalf("repo skill workflow run open path log = %q, want direct log %q", got, want)
				}
				return
			}

			runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
			directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
			if got, want := len(runCalls), 1; got != want {
				t.Fatalf("workflow run calls len = %d, want %d: %#v", got, want, runCalls)
			}
			if got, want := len(directCalls), 1; got != want {
				t.Fatalf("direct calls len = %d, want %d: %#v", got, want, directCalls)
			}
			if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
				t.Fatalf("repo skill workflow run executable = %q, want direct executable %q", got, want)
			}
			if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
				t.Fatalf("repo skill workflow run args = %#v, want direct args %#v", runCalls[0].Args, directCalls[0].Args)
			}
		})
	}

	for skillName, requiredWorkflows := range skillWorkflowRequirementMap() {
		for _, workflowName := range requiredWorkflows {
			workflow, ok := findWorkflow(workflowName)
			if !ok || !workflowDirectDocCommandIsComparable(workflow) {
				continue
			}
			if !seenBySkill[skillName][workflowName] {
				t.Fatalf("repo skill %q did not compare required workflow %q; saw %#v", skillName, workflowName, seenBySkill[skillName])
			}
		}
	}
}

func TestZVBinarySkillsShowWorkflowRunsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	var wantSubcommandCalls, wantOpenPathCalls int
	for _, skill := range currentRepoSkills(t, root) {
		showText := runZVBinary(t, exe, root, "skills", "show", skill.Name)
		showJSON := runZVBinary(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		if detail.Name != skill.Name {
			t.Fatalf("skills show json name = %q, want %q", detail.Name, skill.Name)
		}

		textCommands := skillWorkflowRunCommandsFromBody(t, showText)
		jsonCommands := skillWorkflowRunCommandsFromBody(t, detail.Body)
		if got, want := commandKeys(textCommands), commandKeys(jsonCommands); strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("skills show text commands for %s = %#v, want json commands %#v", skill.Name, got, want)
		}
		if len(textCommands) == 0 {
			t.Fatalf("skills show %s did not expose workflow run commands", skill.Name)
		}

		seen := make(map[string]bool)
		for _, command := range textCommands {
			workflowName := command[2]
			seen[workflowName] = true
			workflow, ok := findWorkflow(workflowName)
			if !ok {
				t.Fatalf("workflow %q from skills show %s is not cataloged", workflowName, skill.Name)
			}
			if len(workflow.RunArgs) >= 2 && workflow.RunArgs[0] == "gallery" && workflow.RunArgs[1] == "open" {
				wantOpenPathCalls++
			} else {
				wantSubcommandCalls++
			}
			runZVBinaryWithEnv(t, exe, root, env, command...)
		}

		for _, workflowName := range skillWorkflowRequirements(skill.Name) {
			if !seen[workflowName] {
				t.Fatalf("skills show %s did not expose required workflow %q; saw %#v", skill.Name, workflowName, seen)
			}
		}
	}

	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinarySkillsShowJSONWorkflowRunsMatchDirectCommandsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	seenBySkill := make(map[string]map[string]bool)
	for _, skill := range currentRepoSkills(t, root) {
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("skills show json for %s wrote stderr %q", skill.Name, showStderr)
		}
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		commands := skillWorkflowRunCommandsFromBody(t, detail.Body)
		if len(commands) == 0 {
			t.Fatalf("skills show json for %s exposed no workflow run commands", skill.Name)
		}
		seenBySkill[skill.Name] = make(map[string]bool)

		for i, command := range commands {
			if len(command) < 3 {
				t.Fatalf("skills show json for %s exposed workflow command %#v, want workflows run <name>", skill.Name, command)
			}
			workflow, ok := findWorkflow(command[2])
			if !ok {
				t.Fatalf("workflow %q from skills show json for %s is not cataloged", command[2], skill.Name)
			}
			if !workflowDirectDocCommandIsComparable(workflow) {
				continue
			}
			seenBySkill[skill.Name][workflow.Name] = true

			t.Run(fmt.Sprintf("%s/%02d/%s", skill.Name, i, workflow.Name), func(t *testing.T) {
				directArgs := directArgsForWorkflowRunDocCommand(t, workflow, command)
				runSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-show-json-run.jsonl", skill.Name, i, workflow.Name))
				directSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-show-json-direct.jsonl", skill.Name, i, workflow.Name))
				runOpenLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-show-json-run-open.txt", skill.Name, i, workflow.Name))
				directOpenLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-show-json-direct-open.txt", skill.Name, i, workflow.Name))

				runOut := runZVBinaryWithEnv(t, exe, root, []string{
					"ZV_FAKE_SUBCOMMAND=1",
					"ZV_FAKE_SUBCOMMAND_LOG=" + runSubcommandLog,
					"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
				}, command...)
				directOut := runZVBinaryWithEnv(t, exe, root, []string{
					"ZV_FAKE_SUBCOMMAND=1",
					"ZV_FAKE_SUBCOMMAND_LOG=" + directSubcommandLog,
					"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
				}, directArgs...)

				if got, want := runOut, directOut; got != want {
					t.Fatalf("skills show json workflow run output = %q, want direct output %q", got, want)
				}
				if workflow.Name == "gallery-open" {
					if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
						t.Fatalf("skills show json workflow run open path log = %q, want direct log %q", got, want)
					}
					return
				}

				runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
				directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
				if got, want := len(runCalls), 1; got != want {
					t.Fatalf("workflow run calls len = %d, want %d: %#v", got, want, runCalls)
				}
				if got, want := len(directCalls), 1; got != want {
					t.Fatalf("direct calls len = %d, want %d: %#v", got, want, directCalls)
				}
				if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
					t.Fatalf("skills show json workflow run executable = %q, want direct executable %q", got, want)
				}
				if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
					t.Fatalf("skills show json workflow run args = %#v, want direct args %#v", runCalls[0].Args, directCalls[0].Args)
				}
			})
		}
	}

	for skillName, requiredWorkflows := range skillWorkflowRequirementMap() {
		for _, workflowName := range requiredWorkflows {
			workflow, ok := findWorkflow(workflowName)
			if !ok || !workflowDirectDocCommandIsComparable(workflow) {
				continue
			}
			if !seenBySkill[skillName][workflowName] {
				t.Fatalf("skills show json for %q did not compare required workflow %q; saw %#v", skillName, workflowName, seenBySkill[skillName])
			}
		}
	}
}

func TestZVBinarySkillsListJSONDiscoveryWorkflowRunsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	listJSON, listStderr := runZVBinarySplit(t, exe, root, "skills", "list", "--format", "json")
	if listStderr != "" {
		t.Fatalf("skills list json wrote stderr %q", listStderr)
	}
	var skills []skillInfo
	if err := json.Unmarshal([]byte(listJSON), &skills); err != nil {
		t.Fatalf("unmarshal skills list json: %v\n%s", err, listJSON)
	}
	if len(skills) == 0 {
		t.Fatalf("skills list json returned no skills")
	}

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	seenSkills := make(map[string]bool)
	seenBySkill := make(map[string]map[string]bool)
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, skill := range skills {
		seenSkills[skill.Name] = true
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("skills show json for %s wrote stderr %q", skill.Name, showStderr)
		}
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		commands := skillWorkflowRunCommandsFromBody(t, detail.Body)
		if len(commands) == 0 {
			t.Fatalf("skills show json for %s exposed no workflow run commands", skill.Name)
		}
		seenBySkill[skill.Name] = make(map[string]bool)
		for _, command := range commands {
			workflowName := command[2]
			workflow, ok := findWorkflow(workflowName)
			if !ok {
				t.Fatalf("workflow %q from skills list discovery for %s is not cataloged", workflowName, skill.Name)
			}
			seenBySkill[skill.Name][workflowName] = true
			if len(workflow.RunArgs) >= 2 && workflow.RunArgs[0] == "gallery" && workflow.RunArgs[1] == "open" {
				wantOpenPathCalls++
			} else {
				wantSubcommandCalls++
			}
			runZVBinaryWithEnv(t, exe, root, env, command...)
		}
	}

	for skillName, requiredWorkflows := range skillWorkflowRequirementMap() {
		if !seenSkills[skillName] {
			t.Fatalf("skills list json did not expose required repo skill %q; saw %#v", skillName, seenSkills)
		}
		for _, workflowName := range requiredWorkflows {
			if !seenBySkill[skillName][workflowName] {
				t.Fatalf("skills list json discovery for %s did not expose required workflow %q; saw %#v", skillName, workflowName, seenBySkill[skillName])
			}
		}
	}

	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinarySkillsDiscoveryJSONRequiredWorkflowRunsMatchContractEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	listJSON, listStderr := runZVBinarySplit(t, exe, root, "skills", "list", "--format", "json")
	if listStderr != "" {
		t.Fatalf("skills list json wrote stderr %q", listStderr)
	}
	var skills []skillInfo
	if err := json.Unmarshal([]byte(listJSON), &skills); err != nil {
		t.Fatalf("unmarshal skills list json: %v\n%s", err, listJSON)
	}
	seenSkills := make(map[string]bool)
	for _, skill := range skills {
		required := skillWorkflowRequirements(skill.Name)
		if len(required) == 0 {
			continue
		}
		seenSkills[skill.Name] = true
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("skills show json for %s wrote stderr %q", skill.Name, showStderr)
		}
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		var got []string
		for _, command := range skillWorkflowRunCommandsFromBody(t, detail.Body) {
			got = append(got, command[2])
		}
		if strings.Join(got, "\x00") != strings.Join(required, "\x00") {
			t.Fatalf("skills show json workflow runs for %s = %#v, want %#v", skill.Name, got, required)
		}
	}
	for skillName := range skillWorkflowRequirementMap() {
		if !seenSkills[skillName] {
			t.Fatalf("skills list json did not expose required repo skill %q; saw %#v", skillName, seenSkills)
		}
	}
}

func TestZVBinaryUtilityShortsSkillRecordWorkflowDocumentsCaptureToolsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", "zackvideo-cs2-utility-shorts", "--format", "json")
	if showStderr != "" {
		t.Fatalf("skills show json stderr = %q, want empty", showStderr)
	}
	var detail skillDetail
	if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
		t.Fatalf("unmarshal skills show json: %v\n%s", err, showJSON)
	}

	var recordCommand []string
	for _, command := range skillWorkflowRunCommandsFromBody(t, detail.Body) {
		if len(command) >= 3 && command[0] == "workflows" && command[1] == "run" && command[2] == "record" {
			recordCommand = command
			break
		}
	}
	if len(recordCommand) == 0 {
		t.Fatalf("zackvideo-cs2-utility-shorts did not document workflows run record")
	}
	for _, want := range []string{"--killplan", "--demo", "--out", "--hlae", "--cs2"} {
		if !containsString(recordCommand, want) {
			t.Fatalf("record workflow command = %#v, want flag %s", recordCommand, want)
		}
	}

	subcommandLogPath := filepath.Join(tempDir, "utility-shorts-record.jsonl")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
	}
	stdout, stderr := runZVBinarySplitWithEnv(t, exe, root, env, recordCommand...)
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	calls := readFakeSubcommandCalls(t, subcommandLogPath)
	if got, want := len(calls), 1; got != want {
		t.Fatalf("subcommand calls = %d, want %d: %#v", got, want, calls)
	}
	if got, want := calls[0].Executable, executableName("zv-recorder"); got != want {
		t.Fatalf("executable = %q, want %q", got, want)
	}
	for _, want := range []string{"--killplan", "--demo", "--out", "--hlae", "--cs2"} {
		if !containsString(calls[0].Args, want) {
			t.Fatalf("delegated record args = %#v, want flag %s", calls[0].Args, want)
		}
	}
}

func TestZVBinaryCurrentDocsAndSkillsRecordExamplesDocumentDryRunOrCaptureToolsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	type publishedRecordCommand struct {
		source  string
		command []string
	}

	var commands []publishedRecordCommand
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok {
				continue
			}
			if _, ok := recordCommandArgsForPublishedExample(command); !ok {
				continue
			}
			commands = append(commands, publishedRecordCommand{
				source:  doc.path,
				command: command,
			})
		}
	}
	for _, skill := range currentRepoSkillBodies(t, root) {
		for _, line := range skillCommandLines(skill.body) {
			command, ok := skillCommand(line)
			if !ok {
				continue
			}
			if _, ok := recordCommandArgsForPublishedExample(command); !ok {
				continue
			}
			commands = append(commands, publishedRecordCommand{
				source:  skill.path,
				command: command,
			})
		}
	}
	if len(commands) == 0 {
		t.Fatalf("no published record examples found in current docs or skills")
	}

	for _, command := range commands {
		args, ok := recordCommandArgsForPublishedExample(command.command)
		if !ok {
			continue
		}
		if recordCommandHasDryRunOrCaptureTools(args) {
			continue
		}
		t.Fatalf("%s: record example must include --dry-run or both --hlae and --cs2: %#v", command.source, command.command)
	}
}

func TestZVBinarySkillsCheckRejectsRecordExamplesWithoutDryRunOrCaptureToolsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run record -- --killplan plan.json --demo demo.dem --out recording`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := `missing required flags --hlae, --cs2 for "record" unless --dry-run is set`
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsRequiredWorkflowRunsOutOfOrderEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "zackvideo-cs2-utility-shorts", strings.Join([]string{
		"---",
		"name: zackvideo-cs2-utility-shorts",
		`description: "Create CS2 utility Shorts from a demo with ZackVideo."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run record -- --killplan plan.json --demo demo.dem --out recording --dry-run`,
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		`.\bin\zv.exe workflows run shorts-render -- --recording-result recording\recording-result.json --out shorts`,
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "required workflow runs must appear in order: demo-parse, utility-audit, record, shorts-render, gallery-open"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsRequiredWorkflowRunDocumentedOnlyAsHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "zackvideo-lineup-audit", strings.Join([]string{
		"---",
		"name: zackvideo-lineup-audit",
		`description: "Review and correct ZackVideo CS2 utility destination labels."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run utility-audit -- --help`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "missing required workflow run utility-audit"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsCatalogWorkflowRunsOutOfOrderEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run shorts-render -- --recording-result recording\recording-result.json --out shorts`,
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "workflow runs must follow catalog order; demo-parse appears after shorts-render"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsUnexpectedRequiredSkillWorkflowRunsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "zackvideo-lineup-audit", strings.Join([]string{
		"---",
		"name: zackvideo-lineup-audit",
		`description: "Review and correct ZackVideo CS2 utility destination labels."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "unexpected workflow run gallery-open; expected only: utility-audit"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsZackVideoSkillWithoutWorkflowRequirementsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "zackvideo-cs2-utility-shorts", strings.Join([]string{
		"---",
		"name: zackvideo-cs2-utility-shorts",
		`description: "Create CS2 utility Shorts from a demo with ZackVideo."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		`.\bin\zv.exe workflows run record -- --killplan plan.json --demo demo.dem --out recording --dry-run`,
		`.\bin\zv.exe workflows run shorts-render -- --recording-result recording\recording-result.json --out shorts`,
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		"```",
		"",
	}, "\n"))
	writeSkillBody(t, tempDir, "zackvideo-lineup-audit", strings.Join([]string{
		"---",
		"name: zackvideo-lineup-audit",
		`description: "Review and correct ZackVideo CS2 utility destination labels."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		"```",
		"",
	}, "\n"))
	writeSkillBody(t, tempDir, "zackvideo-youtube-shorts-publish", strings.Join([]string{
		"---",
		"name: zackvideo-youtube-shorts-publish",
		`description: "Prepare or upload ZackVideo YouTube Shorts publish packs."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		"```",
		"",
	}, "\n"))
	writeSkillBody(t, tempDir, "zackvideo-new-skill", strings.Join([]string{
		"---",
		"name: zackvideo-new-skill",
		`description: "New ZackVideo workflow skill."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "skill:zackvideo-new-skill: missing workflow requirements for repo skill"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinarySkillsCheckRejectsMissingRequiredRepoSkillEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "zackvideo-cs2-utility-shorts", strings.Join([]string{
		"---",
		"name: zackvideo-cs2-utility-shorts",
		`description: "Create CS2 utility Shorts from a demo with ZackVideo."`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run utility-audit -- --plan plan.json --lineup-catalog data\lineups --out utility-audit.csv`,
		`.\bin\zv.exe workflows run record -- --killplan plan.json --demo demo.dem --out recording --dry-run`,
		`.\bin\zv.exe workflows run shorts-render -- --recording-result recording\recording-result.json --out shorts`,
		`.\bin\zv.exe workflows run gallery-open -- --path shorts\publish\index.html`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"skill:zackvideo-lineup-audit: workflow requirements reference missing repo skill",
		"skill:zackvideo-youtube-shorts-publish: workflow requirements reference missing repo skill",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinarySkillsCheckRejectsDuplicateWorkflowRunsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		`.\bin\zv.exe workflows run demo-parse -- --demo other.dem --steamid 76561198000000000 --out other-plan.json`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "skills", "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	var result skillCheckResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout)
	}
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	if !hasIssueContaining(result.Issues, "duplicate workflow run demo-parse") {
		t.Fatalf("issues = %#v, want duplicate workflow run issue", result.Issues)
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDocsRecordExamplesWithoutDryRunOrCaptureToolsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, "docs", "toolchain.md"), strings.Join([]string{
		"# Toolchain",
		"",
		"`zv check` validates the unified CLI contract.",
		"",
		"```powershell",
		`.\bin\zv.exe record --killplan plan.json --demo demo.dem --out recording`,
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := `docs/toolchain.md: missing required flags --hlae, --cs2 for "record" unless --dry-run is set`
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDocRunDocumentedOnlyAsHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	readme := readFileString(t, readmePath)
	old := "./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json"
	if !strings.Contains(readme, old) {
		t.Fatalf("README fixture does not contain expected workflow run command")
	}
	readme = strings.ReplaceAll(readme, old, "./bin/zv workflows run demo-parse -- --help")
	writeFile(t, readmePath, readme)

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "README.md: missing executable workflow run demo-parse"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDirectCommandDocumentedOnlyAsHelpEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	readme := readFileString(t, readmePath)
	old := "./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json"
	if !strings.Contains(readme, old) {
		t.Fatalf("README fixture does not contain expected direct workflow command")
	}
	readme = strings.ReplaceAll(readme, old, "./bin/zv demo parse --help")
	writeFile(t, readmePath, readme)

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "README.md: missing executable workflow command demo-parse"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDirectCommandMissingRequiredFlagEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	readme := readFileString(t, readmePath)
	old := "./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json"
	if !strings.Contains(readme, old) {
		t.Fatalf("README fixture does not contain expected direct workflow command")
	}
	readme = strings.ReplaceAll(readme, old, "./bin/zv demo parse --demo testdata/foo.dem --out plan.json")
	writeFile(t, readmePath, readme)

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		`README.md: missing required flag --steamid for "demo parse"`,
		"README.md: missing executable workflow command demo-parse",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDocRunCommandsOutOfOrderEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	b, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README fixture: %v", err)
	}
	old := strings.Join([]string{
		"./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv workflows run demo-players -- --demo testdata/foo.dem",
	}, "\n")
	replacement := strings.Join([]string{
		"./bin/zv workflows run demo-players -- --demo testdata/foo.dem",
		"./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
	}, "\n")
	body := string(b)
	if !strings.Contains(body, old) {
		t.Fatalf("README fixture does not contain expected workflow run order")
	}
	writeFile(t, readmePath, strings.Replace(body, old, replacement, 1))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "README.md: workflow run commands must appear in catalog order: demo-parse, demo-players, utility-audit"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsWorkflowDocShowCommandsOutOfOrderEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	readmePath := filepath.Join(tempDir, "README.md")
	b, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read README fixture: %v", err)
	}
	old := strings.Join([]string{
		"./bin/zv workflows show demo-parse",
		"./bin/zv workflows show demo-parse --format json",
		"./bin/zv workflows show demo-players",
		"./bin/zv workflows show demo-players --format json",
	}, "\n")
	replacement := strings.Join([]string{
		"./bin/zv workflows show demo-players",
		"./bin/zv workflows show demo-players --format json",
		"./bin/zv workflows show demo-parse",
		"./bin/zv workflows show demo-parse --format json",
	}, "\n")
	body := string(b)
	if !strings.Contains(body, old) {
		t.Fatalf("README fixture does not contain expected workflow show order")
	}
	writeFile(t, readmePath, strings.Replace(body, old, replacement, 1))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "README.md: workflow show commands must appear in catalog order: demo-parse, demo-players, utility-audit"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateWorkflowDocShowCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv workflows show demo-parse",
		"./bin/zv workflows show demo-parse --format=json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"README.md: duplicate workflow show demo-parse --format text",
		"README.md: duplicate workflow show demo-parse --format json",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateWorkflowDocListAndCheckCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv workflows list",
		"./bin/zv workflows list --format=json",
		"./bin/zv workflows check",
		"./bin/zv workflows check --format=json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"README.md: duplicate workflows list --format text",
		"README.md: duplicate workflows list --format json",
		"README.md: duplicate workflows check --format text",
		"README.md: duplicate workflows check --format json",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateProjectCheckDocCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv check",
		"./bin/zv check --format=json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"README.md: duplicate check --format text",
		"README.md: duplicate check --format json",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateSkillDocShowCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv skills show alpha",
		"./bin/zv skills show alpha --format=json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"README.md: duplicate skill show alpha --format text",
		"README.md: duplicate skill show alpha --format json",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateSkillDocListAndCheckCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv skills list",
		"./bin/zv skills list",
		"./bin/zv skills list --format=json",
		"./bin/zv skills check",
		"./bin/zv skills check --format=json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	for _, want := range []string{
		"README.md: duplicate skills list --format text",
		"README.md: duplicate skills list --format json",
		"README.md: duplicate skills check --format text",
		"README.md: duplicate skills check --format json",
	} {
		if !hasIssueContaining(result.Issues, want) {
			t.Fatalf("issues = %#v, want %q", result.Issues, want)
		}
	}
}

func TestZVBinaryProjectCheckRejectsSkillDocShowCommandsOutOfOrderEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	for _, name := range []string{"alpha", "beta"} {
		writeSkillBody(t, tempDir, name, strings.Join([]string{
			"---",
			"name: " + name,
			`description: "Test workflow"`,
			"---",
			"",
			"```powershell",
			`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
			"```",
			"",
		}, "\n"))
	}
	writeWorkflowDocs(t, tempDir)

	readmePath := filepath.Join(tempDir, "README.md")
	replaceSkillShowFixture(t, readmePath, strings.Join([]string{
		"./bin/zv skills show beta",
		"./bin/zv skills show alpha",
	}, "\n"), strings.Join([]string{
		"./bin/zv skills show beta --format json",
		"./bin/zv skills show alpha --format json",
	}, "\n"))
	codexReadmePath := filepath.Join(tempDir, ".codex", "README.md")
	replaceSkillShowFixture(t, codexReadmePath, strings.Join([]string{
		"./bin/zv skills show alpha",
		"./bin/zv skills show beta",
	}, "\n"), strings.Join([]string{
		"./bin/zv skills show alpha --format json",
		"./bin/zv skills show beta --format json",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	want := "README.md: skill show commands must appear in skill order: alpha, beta"
	if !hasIssueContaining(result.Issues, want) {
		t.Fatalf("issues = %#v, want %q", result.Issues, want)
	}
}

func TestZVBinaryProjectCheckRejectsDuplicateWorkflowDocRunCommandsEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	appendFile(t, filepath.Join(tempDir, "README.md"), strings.Join([]string{
		"",
		"```bash",
		"./bin/zv workflows run demo-parse -- --demo testdata/other.dem --steamid 76561198000000000 --out other-plan.json",
		"```",
		"",
	}, "\n"))

	stdout, stderr, code := runZVBinaryFailureSplit(t, exe, tempDir, "check", "--format", "json")

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, want, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for json output", stderr)
	}
	result := decodeWorkflowCheckResult(t, stdout)
	if result.OK {
		t.Fatalf("result.OK = true, want false")
	}
	if !hasIssueContaining(result.Issues, "README.md: duplicate workflow run demo-parse") {
		t.Fatalf("issues = %#v, want duplicate workflow run issue", result.Issues)
	}
}

func TestZVBinarySkillsListJSONDiscoveryWorkflowRunsMatchDirectCommandsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	listJSON, listStderr := runZVBinarySplit(t, exe, root, "skills", "list", "--format", "json")
	if listStderr != "" {
		t.Fatalf("skills list json wrote stderr %q", listStderr)
	}
	var skills []skillInfo
	if err := json.Unmarshal([]byte(listJSON), &skills); err != nil {
		t.Fatalf("unmarshal skills list json: %v\n%s", err, listJSON)
	}
	if len(skills) == 0 {
		t.Fatalf("skills list json returned no skills")
	}

	seenSkills := make(map[string]bool)
	seenBySkill := make(map[string]map[string]bool)
	for _, skill := range skills {
		seenSkills[skill.Name] = true
		showJSON, showStderr := runZVBinarySplit(t, exe, root, "skills", "show", skill.Name, "--format", "json")
		if showStderr != "" {
			t.Fatalf("skills show json for %s wrote stderr %q", skill.Name, showStderr)
		}
		var detail skillDetail
		if err := json.Unmarshal([]byte(showJSON), &detail); err != nil {
			t.Fatalf("unmarshal skills show json for %s: %v\n%s", skill.Name, err, showJSON)
		}
		commands := skillWorkflowRunCommandsFromBody(t, detail.Body)
		if len(commands) == 0 {
			t.Fatalf("skills list json discovery for %s exposed no workflow run commands", skill.Name)
		}
		seenBySkill[skill.Name] = make(map[string]bool)

		for i, command := range commands {
			if len(command) < 3 {
				t.Fatalf("skills list json discovery for %s exposed workflow command %#v, want workflows run <name>", skill.Name, command)
			}
			workflow, ok := findWorkflow(command[2])
			if !ok {
				t.Fatalf("workflow %q from skills list json discovery for %s is not cataloged", command[2], skill.Name)
			}
			if !workflowDirectDocCommandIsComparable(workflow) {
				continue
			}
			seenBySkill[skill.Name][workflow.Name] = true

			t.Run(fmt.Sprintf("%s/%02d/%s", skill.Name, i, workflow.Name), func(t *testing.T) {
				directArgs := directArgsForWorkflowRunDocCommand(t, workflow, command)
				runSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-list-json-run.jsonl", skill.Name, i, workflow.Name))
				directSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-list-json-direct.jsonl", skill.Name, i, workflow.Name))
				runOpenLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-list-json-run-open.txt", skill.Name, i, workflow.Name))
				directOpenLog := filepath.Join(tempDir, fmt.Sprintf("%s-%02d-%s-list-json-direct-open.txt", skill.Name, i, workflow.Name))

				runOut := runZVBinaryWithEnv(t, exe, root, []string{
					"ZV_FAKE_SUBCOMMAND=1",
					"ZV_FAKE_SUBCOMMAND_LOG=" + runSubcommandLog,
					"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
				}, command...)
				directOut := runZVBinaryWithEnv(t, exe, root, []string{
					"ZV_FAKE_SUBCOMMAND=1",
					"ZV_FAKE_SUBCOMMAND_LOG=" + directSubcommandLog,
					"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
				}, directArgs...)

				if got, want := runOut, directOut; got != want {
					t.Fatalf("skills list json workflow run output = %q, want direct output %q", got, want)
				}
				if workflow.Name == "gallery-open" {
					if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
						t.Fatalf("skills list json workflow run open path log = %q, want direct log %q", got, want)
					}
					return
				}

				runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
				directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
				if got, want := len(runCalls), 1; got != want {
					t.Fatalf("workflow run calls len = %d, want %d: %#v", got, want, runCalls)
				}
				if got, want := len(directCalls), 1; got != want {
					t.Fatalf("direct calls len = %d, want %d: %#v", got, want, directCalls)
				}
				if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
					t.Fatalf("skills list json workflow run executable = %q, want direct executable %q", got, want)
				}
				if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
					t.Fatalf("skills list json workflow run args = %#v, want direct args %#v", runCalls[0].Args, directCalls[0].Args)
				}
			})
		}
	}

	for skillName, requiredWorkflows := range skillWorkflowRequirementMap() {
		if !seenSkills[skillName] {
			t.Fatalf("skills list json did not expose required repo skill %q; saw %#v", skillName, seenSkills)
		}
		for _, workflowName := range requiredWorkflows {
			workflow, ok := findWorkflow(workflowName)
			if !ok || !workflowDirectDocCommandIsComparable(workflow) {
				continue
			}
			if !seenBySkill[skillName][workflowName] {
				t.Fatalf("skills list json discovery for %q did not compare required workflow %q; saw %#v", skillName, workflowName, seenBySkill[skillName])
			}
		}
	}
}

func TestZVBinaryCurrentWorkflowDocExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := currentWorkflowDocRunCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no workflow run commands found in current workflow docs")
	}

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	seen := make(map[string]bool)
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, command := range commands {
		workflowName := command[2]
		seen[workflowName] = true
		workflow, ok := findWorkflow(workflowName)
		if !ok {
			t.Fatalf("workflow %q from docs is not cataloged", workflowName)
		}
		switch {
		case len(workflow.RunArgs) >= 2 && workflow.RunArgs[0] == "gallery" && workflow.RunArgs[1] == "open":
			wantOpenPathCalls++
		case workflow.RunArgs[0] == "skills" || workflow.RunArgs[0] == "workflows" || workflow.RunArgs[0] == "check":
		default:
			wantSubcommandCalls++
		}
		runZVBinaryWithEnv(t, exe, root, env, command...)
	}

	for _, workflow := range workflowCatalog() {
		if !seen[workflow.Name] {
			t.Fatalf("workflow docs do not exercise workflow %q; saw %#v", workflow.Name, seen)
		}
	}
	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryCurrentDirectDocExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := currentWorkflowDocDirectCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no direct zv commands found in current workflow docs")
	}

	subcommandLogPath := filepath.Join(tempDir, "subcommands.jsonl")
	openPathLogPath := filepath.Join(tempDir, "open-paths.txt")
	env := []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + subcommandLogPath,
		"ZV_FAKE_OPEN_PATH_LOG=" + openPathLogPath,
	}

	documented := make(map[string]bool)
	var wantSubcommandCalls, wantOpenPathCalls int
	for _, command := range commands {
		if got := documentedWorkflowCommand("zv " + strings.Join(command, " ")); got != "" {
			documented[got] = true
		}
		switch command[0] {
		case "gallery":
			wantOpenPathCalls++
		case "demo", "utility", "record", "compose", "shorts", "analysis", "serve", "pipeline":
			wantSubcommandCalls++
		}
		runZVBinaryWithEnv(t, exe, root, env, command...)
	}

	for _, workflow := range workflowCatalog() {
		want := documentedWorkflowCommand(workflow.Command)
		if want != "" && !documented[want] {
			t.Fatalf("workflow docs do not execute direct command %q; saw %#v", want, documented)
		}
	}
	if got, want := len(readFakeSubcommandCalls(t, subcommandLogPath)), wantSubcommandCalls; got != want {
		t.Fatalf("subcommand calls = %d, want %d", got, want)
	}
	if got, want := len(readLines(t, openPathLogPath)), wantOpenPathCalls; got != want {
		t.Fatalf("open path calls = %d, want %d", got, want)
	}
}

func TestZVBinaryCurrentRequiredWorkflowDocsCoverEveryExecutableWorkflowEndToEnd(t *testing.T) {
	root := repoRoot(t)
	var docsChecked int
	for _, doc := range workflowDocs() {
		if !doc.RequiredWorkflows {
			continue
		}
		docsChecked++
		path := filepath.Join(root, filepath.FromSlash(doc.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", doc.Path, err)
		}

		directSeen := make(map[string]bool)
		runSeen := make(map[string]bool)
		for _, line := range skillCommandLines(string(b)) {
			command, ok := skillCommand(line)
			if !ok {
				continue
			}
			if isExecutableWorkflowRunCommand(command) {
				runSeen[command[2]] = true
			}
			for _, workflow := range workflowCatalog() {
				if isExecutableDirectWorkflowCommand(command, workflow) {
					directSeen[workflow.Name] = true
					break
				}
			}
		}

		for _, workflow := range workflowCatalog() {
			if strings.TrimSpace(workflow.Name) == "" {
				continue
			}
			if documentedWorkflowCommand(workflow.Command) != "" && !directSeen[workflow.Name] {
				t.Fatalf("%s does not document executable direct command for workflow %q; saw %#v", doc.Path, workflow.Name, directSeen)
			}
			if !runSeen[workflow.Name] {
				t.Fatalf("%s does not document executable workflow run for workflow %q; saw %#v", doc.Path, workflow.Name, runSeen)
			}
		}
	}
	if docsChecked == 0 {
		t.Fatalf("no required workflow docs checked")
	}
}

func TestZVBinaryCurrentRequiredSkillDocsCoverEverySkillCommandEndToEnd(t *testing.T) {
	root := repoRoot(t)
	skills := currentRepoSkills(t, root)
	var docsChecked int
	for _, doc := range workflowDocs() {
		if !doc.RequiredSkills {
			continue
		}
		docsChecked++
		path := filepath.Join(root, filepath.FromSlash(doc.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", doc.Path, err)
		}

		listCheckSeen := make(map[string]bool)
		showSeen := make(map[string]map[string]bool)
		for _, line := range skillCommandLines(string(b)) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "skills" {
				continue
			}
			switch command[1] {
			case "list", "check":
				format, rest, err := parseFormatArgs(command[2:])
				if err != nil || len(rest) != 0 {
					continue
				}
				listCheckSeen[command[1]+":"+format] = true
			case "show":
				if len(command) < 3 {
					continue
				}
				format, rest, err := parseFormatArgs(command[3:])
				if err != nil || len(rest) != 0 {
					continue
				}
				if showSeen[command[2]] == nil {
					showSeen[command[2]] = make(map[string]bool)
				}
				showSeen[command[2]][format] = true
			}
		}

		for _, want := range []string{"list:text", "list:json", "check:text", "check:json"} {
			if !listCheckSeen[want] {
				t.Fatalf("%s does not document skills %s; saw %#v", doc.Path, strings.ReplaceAll(want, ":", " "), listCheckSeen)
			}
		}
		for _, skill := range skills {
			for _, format := range []string{"text", "json"} {
				if !showSeen[skill.Name][format] {
					t.Fatalf("%s does not document skills show %s with %s format; saw %#v", doc.Path, skill.Name, format, showSeen[skill.Name])
				}
			}
		}
	}
	if docsChecked == 0 {
		t.Fatalf("no required skill docs checked")
	}
}

func TestZVBinaryCurrentRequiredWorkflowDocsCoverEveryDiscoveryCommandEndToEnd(t *testing.T) {
	root := repoRoot(t)
	var docsChecked int
	for _, doc := range workflowDocs() {
		if !doc.RequiredWorkflows {
			continue
		}
		docsChecked++
		path := filepath.Join(root, filepath.FromSlash(doc.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", doc.Path, err)
		}

		listCheckSeen := make(map[string]bool)
		showSeen := make(map[string]map[string]bool)
		for _, line := range skillCommandLines(string(b)) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "workflows" {
				continue
			}
			switch command[1] {
			case "list", "check":
				format, rest, err := parseFormatArgs(command[2:])
				if err != nil || len(rest) != 0 {
					continue
				}
				listCheckSeen[command[1]+":"+format] = true
			case "show":
				if len(command) < 3 {
					continue
				}
				format, rest, err := parseFormatArgs(command[3:])
				if err != nil || len(rest) != 0 {
					continue
				}
				if showSeen[command[2]] == nil {
					showSeen[command[2]] = make(map[string]bool)
				}
				showSeen[command[2]][format] = true
			}
		}

		for _, want := range []string{"list:text", "list:json", "check:text", "check:json"} {
			if !listCheckSeen[want] {
				t.Fatalf("%s does not document workflows %s; saw %#v", doc.Path, strings.ReplaceAll(want, ":", " "), listCheckSeen)
			}
		}
		for _, workflow := range workflowCatalog() {
			for _, format := range []string{"text", "json"} {
				if !showSeen[workflow.Name][format] {
					t.Fatalf("%s does not document workflows show %s with %s format; saw %#v", doc.Path, workflow.Name, format, showSeen[workflow.Name])
				}
			}
		}
	}
	if docsChecked == 0 {
		t.Fatalf("no required workflow docs checked")
	}
}

func TestZVBinaryCurrentDirectDocExamplesMatchWorkflowRunsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := currentWorkflowDocDirectCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no direct zv commands found in current workflow docs")
	}

	seen := make(map[string]bool)
	for i, directArgs := range commands {
		workflow, ok := workflowForDirectCommand(directArgs)
		if !ok || !workflowDirectDocCommandIsComparable(workflow) {
			continue
		}
		seen[workflow.Name] = true

		t.Run(fmt.Sprintf("%02d/%s", i, workflow.Name), func(t *testing.T) {
			runArgs := workflowRunArgsForDirectCommand(t, workflow, directArgs)
			directSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-direct.jsonl", i, workflow.Name))
			runSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run.jsonl", i, workflow.Name))
			directOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-direct-open.txt", i, workflow.Name))
			runOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run-open.txt", i, workflow.Name))

			directOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + directSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
			}, directArgs...)
			runOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + runSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
			}, runArgs...)

			if got, want := runOut, directOut; got != want {
				t.Fatalf("workflow run output = %q, want documented direct output %q", got, want)
			}
			if workflow.Name == "gallery-open" {
				if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
					t.Fatalf("workflow run open path log = %q, want direct log %q", got, want)
				}
				return
			}

			directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
			runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
			if got, want := len(directCalls), 1; got != want {
				t.Fatalf("direct calls len = %d, want %d: %#v", got, want, directCalls)
			}
			if got, want := len(runCalls), 1; got != want {
				t.Fatalf("workflow run calls len = %d, want %d: %#v", got, want, runCalls)
			}
			if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
				t.Fatalf("workflow run executable = %q, want documented direct executable %q", got, want)
			}
			if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
				t.Fatalf("workflow run args = %#v, want documented direct args %#v", runCalls[0].Args, directCalls[0].Args)
			}
		})
	}

	for _, workflow := range workflowCatalog() {
		if !workflowDirectDocCommandIsComparable(workflow) {
			continue
		}
		if !seen[workflow.Name] {
			t.Fatalf("current workflow docs do not compare direct command for workflow %q", workflow.Name)
		}
	}
}

func TestZVBinaryCurrentWorkflowRunDocExamplesMatchDirectCommandsEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)
	installFakeDelegatedSubcommands(t, filepath.Dir(exe))

	commands := currentWorkflowDocRunCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no workflow run commands found in current workflow docs")
	}

	seen := make(map[string]bool)
	for i, runArgs := range commands {
		if len(runArgs) < 3 {
			t.Fatalf("workflow run command = %#v, want workflows run <name>", runArgs)
		}
		workflow, ok := findWorkflow(runArgs[2])
		if !ok {
			t.Fatalf("workflow %q from docs is not cataloged", runArgs[2])
		}
		if !workflowDirectDocCommandIsComparable(workflow) {
			continue
		}
		seen[workflow.Name] = true

		t.Run(fmt.Sprintf("%02d/%s", i, workflow.Name), func(t *testing.T) {
			directArgs := directArgsForWorkflowRunDocCommand(t, workflow, runArgs)
			directSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run-doc-direct.jsonl", i, workflow.Name))
			runSubcommandLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run-doc-run.jsonl", i, workflow.Name))
			directOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run-doc-direct-open.txt", i, workflow.Name))
			runOpenLog := filepath.Join(tempDir, fmt.Sprintf("%02d-%s-run-doc-run-open.txt", i, workflow.Name))

			runOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + runSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
			}, runArgs...)
			directOut := runZVBinaryWithEnv(t, exe, root, []string{
				"ZV_FAKE_SUBCOMMAND=1",
				"ZV_FAKE_SUBCOMMAND_LOG=" + directSubcommandLog,
				"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
			}, directArgs...)

			if got, want := runOut, directOut; got != want {
				t.Fatalf("documented workflow run output = %q, want direct output %q", got, want)
			}
			if workflow.Name == "gallery-open" {
				if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
					t.Fatalf("documented workflow run open path log = %q, want direct log %q", got, want)
				}
				return
			}

			runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
			directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
			if got, want := len(runCalls), 1; got != want {
				t.Fatalf("workflow run calls len = %d, want %d: %#v", got, want, runCalls)
			}
			if got, want := len(directCalls), 1; got != want {
				t.Fatalf("direct calls len = %d, want %d: %#v", got, want, directCalls)
			}
			if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
				t.Fatalf("documented workflow run executable = %q, want direct executable %q", got, want)
			}
			if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
				t.Fatalf("documented workflow run args = %#v, want direct args %#v", runCalls[0].Args, directCalls[0].Args)
			}
		})
	}

	for _, workflow := range workflowCatalog() {
		if !workflowDirectDocCommandIsComparable(workflow) {
			continue
		}
		if !seen[workflow.Name] {
			t.Fatalf("current workflow docs do not compare workflow run command for workflow %q", workflow.Name)
		}
	}
}

func TestZVBinaryCurrentWorkflowDocShowExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentWorkflowDocShowCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no workflow show commands found in current workflow docs")
	}

	seenText := make(map[string]bool)
	seenJSON := make(map[string]bool)
	for _, command := range commands {
		if len(command) < 3 {
			t.Fatalf("workflow show command = %#v, want workflows show <name>", command)
		}
		workflow, ok := findWorkflow(command[2])
		if !ok {
			t.Fatalf("workflow %q from docs is not cataloged", command[2])
		}
		format, rest, err := parseFormatArgs(command[3:])
		if err != nil || len(rest) != 0 {
			t.Fatalf("workflow show command %#v has invalid format args: rest=%#v err=%v", command, rest, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}

		switch format {
		case "text":
			seenText[workflow.Name] = true
			if got, want := stdout, workflowShowText(workflow); got != want {
				t.Fatalf("workflow show text for %s = %q, want %q", workflow.Name, got, want)
			}
		case "json":
			seenJSON[workflow.Name] = true
			if strings.Contains(stdout, `"run_args"`) {
				t.Fatalf("workflow show json for %s leaked run_args: %s", workflow.Name, stdout)
			}
			var got workflowInfo
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("unmarshal workflow show json for %s: %v\n%s", workflow.Name, err, stdout)
			}
			if got.Name != workflow.Name || got.Description != workflow.Description || got.Command != workflow.Command || got.RunCommand != workflow.RunCommand {
				t.Fatalf("workflow show json for %s = %#v, want %#v", workflow.Name, got, workflow)
			}
		default:
			t.Fatalf("workflow show command %#v used unsupported format %q", command, format)
		}
	}

	for _, workflow := range workflowCatalog() {
		if !seenText[workflow.Name] {
			t.Fatalf("current workflow docs do not execute text workflow show for %q", workflow.Name)
		}
		if !seenJSON[workflow.Name] {
			t.Fatalf("current workflow docs do not execute json workflow show for %q", workflow.Name)
		}
	}
}

func TestZVBinaryCurrentSkillDocShowExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentSkillDocShowCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no skill show commands found in current workflow docs")
	}

	skills := currentRepoSkills(t, root)
	skillByName := make(map[string]skillInfo, len(skills))
	for _, skill := range skills {
		skillByName[skill.Name] = skill
	}

	seenText := make(map[string]bool)
	seenJSON := make(map[string]bool)
	for _, command := range commands {
		if len(command) < 3 {
			t.Fatalf("skill show command = %#v, want skills show <name>", command)
		}
		skill, ok := skillByName[command[2]]
		if !ok {
			t.Fatalf("skill %q from docs is not a repo skill", command[2])
		}
		format, rest, err := parseFormatArgs(command[3:])
		if err != nil || len(rest) != 0 {
			t.Fatalf("skill show command %#v has invalid format args: rest=%#v err=%v", command, rest, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}

		wantBody := readFileString(t, skill.Path)
		switch format {
		case "text":
			seenText[skill.Name] = true
			if strings.TrimRight(stdout, "\n") != strings.TrimRight(wantBody, "\n") {
				t.Fatalf("skill show text for %s did not match %s", skill.Name, skill.Path)
			}
		case "json":
			seenJSON[skill.Name] = true
			if strings.Contains(stdout, `"path"`) || strings.Contains(stdout, skill.Path) {
				t.Fatalf("skill show json for %s leaked local path: %s", skill.Name, stdout)
			}
			var got skillDetail
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("unmarshal skill show json for %s: %v\n%s", skill.Name, err, stdout)
			}
			if got.Name != skill.Name || got.Description != skill.Description || got.Body != wantBody {
				t.Fatalf("skill show json for %s = %#v, want name=%q description=%q body from %s", skill.Name, got, skill.Name, skill.Description, skill.Path)
			}
		default:
			t.Fatalf("skill show command %#v used unsupported format %q", command, format)
		}
	}

	for _, skill := range skills {
		if !seenText[skill.Name] {
			t.Fatalf("current workflow docs do not execute text skill show for %q", skill.Name)
		}
		if !seenJSON[skill.Name] {
			t.Fatalf("current workflow docs do not execute json skill show for %q", skill.Name)
		}
	}
}

func TestZVBinaryCurrentSkillDocListAndCheckExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentSkillDocListAndCheckCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no skill list/check commands found in current workflow docs")
	}

	skills := currentRepoSkills(t, root)
	seen := make(map[string]bool)
	for _, command := range commands {
		if len(command) < 2 {
			t.Fatalf("skill command = %#v, want skills list/check", command)
		}
		format, rest, err := parseFormatArgs(command[2:])
		if err != nil || len(rest) != 0 {
			t.Fatalf("skill command %#v has invalid format args: rest=%#v err=%v", command, rest, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}
		seen[command[1]+":"+format] = true

		switch command[1] {
		case "list":
			switch format {
			case "text":
				if got, want := stdout, skillListText(skills); got != want {
					t.Fatalf("documented skills list stdout = %q, want %q", got, want)
				}
			case "json":
				if strings.Contains(stdout, `"path"`) {
					t.Fatalf("documented skills list json leaked local path: %s", stdout)
				}
				var got []skillInfo
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("unmarshal documented skills list json: %v\n%s", err, stdout)
				}
				if gotNames, wantNames := skillNames(got), skillNames(skills); strings.Join(gotNames, "\x00") != strings.Join(wantNames, "\x00") {
					t.Fatalf("documented skills list json names = %#v, want %#v", gotNames, wantNames)
				}
			default:
				t.Fatalf("skill list command %#v used unsupported format %q", command, format)
			}
		case "check":
			switch format {
			case "text":
				want := fmt.Sprintf("OK: %d skills checked\n", len(skills))
				if got := stdout; got != want {
					t.Fatalf("documented skills check stdout = %q, want %q", got, want)
				}
			case "json":
				var got skillCheckResult
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("unmarshal documented skills check json: %v\n%s", err, stdout)
				}
				if !got.OK || got.SkillsChecked != len(skills) || len(got.Issues) != 0 {
					t.Fatalf("documented skills check json = %#v, want ok with %d skills and no issues", got, len(skills))
				}
			default:
				t.Fatalf("skill check command %#v used unsupported format %q", command, format)
			}
		default:
			t.Fatalf("unexpected skill command %#v", command)
		}
	}

	for _, want := range []string{"list:text", "list:json", "check:text", "check:json"} {
		if !seen[want] {
			t.Fatalf("current workflow docs do not execute skills %s", strings.ReplaceAll(want, ":", " "))
		}
	}
}

func TestZVBinaryCurrentWorkflowDocListAndCheckExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentWorkflowDocListAndCheckCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no workflow list/check commands found in current workflow docs")
	}

	workflows := workflowCatalog()
	workflowByName := make(map[string]workflowInfo, len(workflows))
	for _, workflow := range workflows {
		workflowByName[workflow.Name] = workflow
	}
	wantSkills := currentRepoSkills(t, root)
	wantWrappers := currentAgentPromptWrappers(t, root)
	wantCheckText := fmt.Sprintf("OK: %d skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked\n", len(wantSkills), len(workflows), len(workflowDocs()), len(wantWrappers))

	seen := make(map[string]bool)
	for _, command := range commands {
		if len(command) < 2 {
			t.Fatalf("workflow command = %#v, want workflows list/check", command)
		}
		format, rest, err := parseFormatArgs(command[2:])
		if err != nil || len(rest) != 0 {
			t.Fatalf("workflow command %#v has invalid format args: rest=%#v err=%v", command, rest, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}
		seen[command[1]+":"+format] = true

		switch command[1] {
		case "list":
			switch format {
			case "text":
				if got, want := stdout, workflowListText(workflows); got != want {
					t.Fatalf("documented workflows list stdout = %q, want %q", got, want)
				}
			case "json":
				if strings.Contains(stdout, `"run_args"`) {
					t.Fatalf("documented workflows list json leaked run_args: %s", stdout)
				}
				var got []workflowInfo
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("unmarshal documented workflows list json: %v\n%s", err, stdout)
				}
				if gotNames, wantNames := workflowNames(got), workflowNames(workflows); strings.Join(gotNames, "\x00") != strings.Join(wantNames, "\x00") {
					t.Fatalf("documented workflows list json names = %#v, want %#v", gotNames, wantNames)
				}
				for _, gotWorkflow := range got {
					wantWorkflow, ok := workflowByName[gotWorkflow.Name]
					if !ok {
						t.Fatalf("documented workflows list json returned unknown workflow %q", gotWorkflow.Name)
					}
					assertWorkflowDiscoveryMatches(t, "documented workflows list json", gotWorkflow, wantWorkflow)
				}
			default:
				t.Fatalf("workflow list command %#v used unsupported format %q", command, format)
			}
		case "check":
			switch format {
			case "text":
				if got := stdout; got != wantCheckText {
					t.Fatalf("documented workflows check stdout = %q, want %q", got, wantCheckText)
				}
			case "json":
				got := decodeWorkflowCheckResult(t, stdout)
				if !got.OK ||
					got.SkillsChecked != len(wantSkills) ||
					got.WorkflowsChecked != len(workflows) ||
					got.WorkflowDocsChecked != len(workflowDocs()) ||
					got.AgentPromptWrappersChecked != len(wantWrappers) ||
					len(got.Issues) != 0 {
					t.Fatalf("documented workflows check json = %#v, want ok with current repo counts", got)
				}
			default:
				t.Fatalf("workflow check command %#v used unsupported format %q", command, format)
			}
		default:
			t.Fatalf("unexpected workflow command %#v", command)
		}
	}

	for _, want := range []string{"list:text", "list:json", "check:text", "check:json"} {
		if !seen[want] {
			t.Fatalf("current workflow docs do not execute workflows %s", strings.ReplaceAll(want, ":", " "))
		}
	}
}

func TestZVBinaryCurrentProjectCheckDocExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentProjectCheckDocCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no project check commands found in current workflow docs")
	}

	wantSkills := currentRepoSkills(t, root)
	wantWorkflows := workflowCatalog()
	wantWrappers := currentAgentPromptWrappers(t, root)
	wantText := fmt.Sprintf("OK: %d skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked\n", len(wantSkills), len(wantWorkflows), len(workflowDocs()), len(wantWrappers))

	seen := make(map[string]bool)
	for _, command := range commands {
		if len(command) == 0 || command[0] != "check" {
			t.Fatalf("project check command = %#v, want check", command)
		}
		format, rest, err := parseFormatArgs(command[1:])
		if err != nil || len(rest) != 0 {
			t.Fatalf("project check command %#v has invalid format args: rest=%#v err=%v", command, rest, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}
		seen[format] = true

		switch format {
		case "text":
			if got := stdout; got != wantText {
				t.Fatalf("documented project check stdout = %q, want %q", got, wantText)
			}
		case "json":
			got := decodeWorkflowCheckResult(t, stdout)
			if !got.OK ||
				got.SkillsChecked != len(wantSkills) ||
				got.WorkflowsChecked != len(wantWorkflows) ||
				got.WorkflowDocsChecked != len(workflowDocs()) ||
				got.AgentPromptWrappersChecked != len(wantWrappers) ||
				len(got.Issues) != 0 {
				t.Fatalf("documented project check json = %#v, want ok with current repo counts", got)
			}
		default:
			t.Fatalf("project check command %#v used unsupported format %q", command, format)
		}
	}

	for _, want := range []string{"text", "json"} {
		if !seen[want] {
			t.Fatalf("current workflow docs do not execute project check %s", want)
		}
	}
}

func TestZVBinaryCurrentInternalCheckWorkflowDocRunExamplesEndToEnd(t *testing.T) {
	root := repoRoot(t)
	tempDir := t.TempDir()
	exe := buildZVBinary(t, tempDir)

	commands := currentInternalCheckWorkflowDocRunCommands(t, root)
	if len(commands) == 0 {
		t.Fatalf("no internal check workflow run commands found in current workflow docs")
	}

	wantSkills := currentRepoSkills(t, root)
	wantWorkflows := workflowCatalog()
	wantWrappers := currentAgentPromptWrappers(t, root)
	wantSkillText := fmt.Sprintf("OK: %d skills checked\n", len(wantSkills))
	wantWorkflowText := fmt.Sprintf("OK: %d skills, %d workflows, %d workflow docs, and %d agent prompt wrappers checked\n", len(wantSkills), len(wantWorkflows), len(workflowDocs()), len(wantWrappers))

	seen := make(map[string]bool)
	for _, command := range commands {
		if len(command) < 3 {
			t.Fatalf("internal check workflow run command = %#v, want workflows run <name>", command)
		}
		workflowName := command[2]
		format, err := workflowRunCheckFormat(command)
		if err != nil {
			t.Fatalf("internal check workflow run command %#v has invalid format args: %v", command, err)
		}

		stdout, stderr := runZVBinarySplit(t, exe, root, command...)
		if stderr != "" {
			t.Fatalf("%#v wrote stderr %q", command, stderr)
		}
		seen[workflowName+":"+format] = true

		switch workflowName {
		case "skills-check":
			switch format {
			case "text":
				if got := stdout; got != wantSkillText {
					t.Fatalf("documented skills-check workflow stdout = %q, want %q", got, wantSkillText)
				}
			case "json":
				var got skillCheckResult
				if err := json.Unmarshal([]byte(stdout), &got); err != nil {
					t.Fatalf("unmarshal documented skills-check workflow json: %v\n%s", err, stdout)
				}
				if !got.OK || got.SkillsChecked != len(wantSkills) || len(got.Issues) != 0 {
					t.Fatalf("documented skills-check workflow json = %#v, want ok with %d skills and no issues", got, len(wantSkills))
				}
			default:
				t.Fatalf("skills-check workflow command %#v used unsupported format %q", command, format)
			}
		case "workflows-check", "project-check":
			switch format {
			case "text":
				if got := stdout; got != wantWorkflowText {
					t.Fatalf("documented %s workflow stdout = %q, want %q", workflowName, got, wantWorkflowText)
				}
			case "json":
				got := decodeWorkflowCheckResult(t, stdout)
				if !got.OK ||
					got.SkillsChecked != len(wantSkills) ||
					got.WorkflowsChecked != len(wantWorkflows) ||
					got.WorkflowDocsChecked != len(workflowDocs()) ||
					got.AgentPromptWrappersChecked != len(wantWrappers) ||
					len(got.Issues) != 0 {
					t.Fatalf("documented %s workflow json = %#v, want ok with current repo counts", workflowName, got)
				}
			default:
				t.Fatalf("%s workflow command %#v used unsupported format %q", workflowName, command, format)
			}
		default:
			t.Fatalf("unexpected internal check workflow command %#v", command)
		}
	}

	for _, workflowName := range []string{"skills-check", "workflows-check", "project-check"} {
		for _, format := range []string{"text", "json"} {
			key := workflowName + ":" + format
			if !seen[key] {
				t.Fatalf("current workflow docs do not execute %s in %s format", workflowName, format)
			}
		}
	}
}

func TestRunCanonicalSkillWorkflowDelegatesThroughLocalBinEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	binDir := filepath.Join(tempDir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	installFakeDelegatedSubcommands(t, binDir)

	logPath := filepath.Join(tempDir, "calls.jsonl")
	t.Setenv("ZV_FAKE_SUBCOMMAND", "1")
	t.Setenv("ZV_FAKE_SUBCOMMAND_LOG", logPath)

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	tests := []struct {
		name           string
		argv           []string
		wantExecutable string
		wantArgs       []string
	}{
		{
			name:           "demo parse",
			argv:           []string{"zv", "demo", "parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--segment-mode", "utility", "--out", "run/plan.json"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"parse", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--segment-mode", "utility", "--out", "run/plan.json"},
		},
		{
			name:           "utility audit",
			argv:           []string{"zv", "utility", "audit", "--plan", "run/plan.json", "--lineup-catalog", "data/lineups", "--out", "run/utility-audit.csv"},
			wantExecutable: executableName("zv-parser"),
			wantArgs:       []string{"utility-audit", "--plan", "run/plan.json", "--lineup-catalog", "data/lineups", "--out", "run/utility-audit.csv"},
		},
		{
			name:           "record",
			argv:           []string{"zv", "record", "--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/recording", "--dry-run"},
			wantExecutable: executableName("zv-recorder"),
			wantArgs:       []string{"--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/recording", "--dry-run"},
		},
		{
			name:           "shorts render",
			argv:           []string{"zv", "shorts", "render", "--recording-result", "run/recording/recording-result.json", "--killplan", "run/plan.json", "--out", "run/shorts", "--preset", "smoke-lineups"},
			wantExecutable: executableName("zv-editor"),
			wantArgs:       []string{"--recording-result", "run/recording/recording-result.json", "--killplan", "run/plan.json", "--out", "run/shorts", "--preset", "smoke-lineups"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			code := Run(tt.argv, &stdout, &stderr, nil, osCommandRunner{})
			if got, want := code, exitSuccess; got != want {
				t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
			}
		})
	}

	calls := readFakeSubcommandCalls(t, logPath)
	if got, want := len(calls), len(tests); got != want {
		t.Fatalf("calls len = %d, want %d: %#v", got, want, calls)
	}
	for i, tt := range tests {
		call := calls[i]
		if got, want := call.Executable, tt.wantExecutable; got != want {
			t.Fatalf("call %d executable = %q, want %q", i, got, want)
		}
		if got, want := strings.Join(call.Args, "\x00"), strings.Join(tt.wantArgs, "\x00"); got != want {
			t.Fatalf("call %d args = %#v, want %#v", i, call.Args, tt.wantArgs)
		}
	}
}

func TestRepoSkillsUseUnifiedCLI(t *testing.T) {
	root := repoRoot(t)
	skillsDir := filepath.Join(root, ".codex", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}

	foundSkill := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		foundSkill = true
		path := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(b)
		if !strings.Contains(body, `.\bin\zv.exe`) {
			t.Errorf("%s does not document the unified zv CLI", path)
		}
		if !strings.Contains(body, `.\bin\zv.exe workflows run`) {
			t.Errorf("%s does not document a cataloged workflow run command", path)
		}
		for _, legacy := range legacySkillBinaries() {
			if strings.Contains(body, legacy) {
				t.Errorf("%s documents legacy direct binary %s", path, legacy)
			}
		}
	}
	if !foundSkill {
		t.Fatalf("no repo skills found in %s", skillsDir)
	}
}

func TestGoGateRunsProjectCheck(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "scripts", "go-gate.sh")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(b)
	for _, want := range []string{
		"== zv check ==",
		"go run ./cmd/zv check",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s does not contain %q", path, want)
		}
	}
}

func TestGoBashScriptsBootstrapWindowsGoToolchain(t *testing.T) {
	root := repoRoot(t)
	tests := []string{
		filepath.Join(root, "scripts", "go-gate.sh"),
		filepath.Join(root, "scripts", "go-format-changed.sh"),
	}
	for _, path := range tests {
		t.Run(filepath.Base(path), func(t *testing.T) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			body := string(b)
			for _, want := range []string{
				"source scripts/go-env.sh",
				"ensure_go_toolchain",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s does not contain %q", path, want)
				}
			}
		})
	}

	path := filepath.Join(root, "scripts", "go-env.sh")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(b)
	for _, want := range []string{
		"ensure_go_toolchain()",
		"/c/Program Files/Go/bin",
		"/mnt/c/Program Files/Go/bin",
		"command -v go.exe",
		"command -v gofmt.exe",
		"go not found: install Go or add it to PATH",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s does not contain %q", path, want)
		}
	}
}

func TestCodexHarnessRunsProjectCheck(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "scripts", "check-codex-harness.sh")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(b)
	for _, want := range []string{
		"source scripts/go-env.sh",
		"ensure_go_toolchain",
		"mapfile -t shell_scripts",
		"find scripts -maxdepth 1 -type f -name '*.sh' | sort",
		`bash -n "${shell_scripts[@]}"`,
		"== ZackVideo workflow contract ==",
		"go run ./cmd/zv check",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s does not contain %q", path, want)
		}
	}
}

func TestCodexHarnessExecutesWorkflowContractEndToEnd(t *testing.T) {
	root := repoRoot(t)
	fakeBin := t.TempDir()
	fakeCodex := filepath.Join(fakeBin, "codex")
	writeFile(t, fakeCodex, strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`printf '%s\n' 'ZackVideo is a deterministic CS2 demo-to-video pipeline'`,
		`printf '%s\n' 'AGENTS.md'`,
	}, "\n"))
	if err := os.Chmod(fakeCodex, 0o755); err != nil {
		t.Fatalf("chmod fake codex: %v", err)
	}

	cmd := exec.Command("bash", "-c", "export PATH="+shellQuote(bashPath(fakeBin))+":\"$PATH\"; scripts/check-codex-harness.sh")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("scripts/check-codex-harness.sh: %v\n%s", err, out)
	}
	body := string(out)
	for _, want := range []string{
		"== shell syntax ==",
		"== Codex sees AGENTS.md ==",
		"== ZackVideo workflow contract ==",
		"OK: 3 skills, 14 workflows, 13 workflow docs, and 19 agent prompt wrappers checked",
		"OK: Codex harness is wired",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("check-codex-harness output = %q, want %q", body, want)
		}
	}
}

func TestRootShellScriptsParseEndToEnd(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "scripts"))
	if err != nil {
		t.Fatalf("read scripts dir: %v", err)
	}
	args := []string{"-n"}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sh" {
			continue
		}
		args = append(args, filepath.ToSlash(filepath.Join("scripts", entry.Name())))
	}
	if len(args) == 1 {
		t.Fatalf("no root shell scripts found")
	}
	cmd := exec.Command("bash", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestRootPowerShellScriptsParseEndToEnd(t *testing.T) {
	root := repoRoot(t)
	powerShell, ok := findPowerShell()
	if !ok {
		t.Skip("powershell or pwsh not found")
	}
	entries, err := os.ReadDir(filepath.Join(root, "scripts"))
	if err != nil {
		t.Fatalf("read scripts dir: %v", err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".ps1" {
			continue
		}
		files = append(files, filepath.Join("scripts", entry.Name()))
	}
	if len(files) == 0 {
		t.Fatalf("no root PowerShell scripts found")
	}

	script := strings.Join([]string{
		"param([string[]]$Paths)",
		"$failed = $false",
		"foreach ($path in $Paths) {",
		"  $tokens = $null",
		"  $errors = $null",
		"  [System.Management.Automation.Language.Parser]::ParseFile((Resolve-Path -LiteralPath $path).Path, [ref]$tokens, [ref]$errors) | Out-Null",
		"  if ($errors.Count -gt 0) {",
		"    Write-Error (\"${path}: \" + (($errors | ForEach-Object { $_.Message }) -join '; '))",
		"    $failed = $true",
		"  }",
		"}",
		"if ($failed) { exit 1 }",
	}, "\n")
	scriptPath := filepath.Join(t.TempDir(), "parse-powershell-scripts.ps1")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write parse script: %v", err)
	}

	args := []string{"-NoProfile"}
	if strings.Contains(strings.ToLower(filepath.Base(powerShell)), "powershell") {
		args = append(args, "-ExecutionPolicy", "Bypass")
	}
	args = append(args, "-File", scriptPath)
	args = append(args, files...)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, powerShell, args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("%s %s timed out\n%s", powerShell, strings.Join(args, " "), out)
	}
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", powerShell, strings.Join(args, " "), err, out)
	}
}

func TestCodexPromptWrappersHaveExistingPromptsAndDocs(t *testing.T) {
	root := repoRoot(t)
	scriptsDir := filepath.Join(root, "scripts")
	promptsDir := filepath.Join(root, ".codex", "prompts")

	promptEntries, err := os.ReadDir(promptsDir)
	if err != nil {
		t.Fatalf("read prompts dir: %v", err)
	}
	prompts := make(map[string]bool)
	for _, entry := range promptEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		prompts[filepath.ToSlash(filepath.Join(".codex", "prompts", entry.Name()))] = false
	}

	readme, err := os.ReadFile(filepath.Join(root, ".codex", "README.md"))
	if err != nil {
		t.Fatalf("read .codex/README.md: %v", err)
	}
	readmeBody := string(readme)
	runner := filepath.Join(scriptsDir, "codex-run.sh")
	if _, err := os.Stat(runner); err != nil {
		t.Fatalf("missing codex runner %s: %v", runner, err)
	}
	if !strings.Contains(readmeBody, "scripts/codex-run.sh") {
		t.Fatalf(".codex/README.md does not document runner scripts/codex-run.sh")
	}

	wrappers, err := filepath.Glob(filepath.Join(scriptsDir, "codex*.sh"))
	if err != nil {
		t.Fatalf("glob codex wrappers: %v", err)
	}
	var mappedWrappers int
	for _, wrapper := range wrappers {
		if filepath.Base(wrapper) == "codex-run.sh" {
			continue
		}
		b, err := os.ReadFile(wrapper)
		if err != nil {
			t.Fatalf("read %s: %v", wrapper, err)
		}
		prompt, ok := codexWrapperPromptPath(string(b))
		if !ok {
			t.Fatalf("%s does not exec scripts/codex-run.sh with a prompt", wrapper)
		}
		if _, ok := prompts[prompt]; !ok {
			t.Fatalf("%s references missing prompt %s", wrapper, prompt)
		}
		prompts[prompt] = true
		mappedWrappers++
		if name := filepath.ToSlash(filepath.Join("scripts", filepath.Base(wrapper))); !strings.Contains(readmeBody, name) {
			t.Fatalf(".codex/README.md does not document wrapper %s", name)
		}
	}
	if mappedWrappers == 0 {
		t.Fatalf("no codex prompt wrappers found")
	}
	for prompt, used := range prompts {
		if !used {
			t.Fatalf("prompt %s has no codex wrapper", prompt)
		}
	}
}

func TestClaudePromptWrappersHaveExistingCommandsAndDocs(t *testing.T) {
	root := repoRoot(t)
	scriptsDir := filepath.Join(root, "scripts")
	commandsDir := filepath.Join(root, ".claude", "commands")

	commandEntries, err := os.ReadDir(commandsDir)
	if err != nil {
		t.Fatalf("read commands dir: %v", err)
	}
	commands := make(map[string]bool)
	for _, entry := range commandEntries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		commands[filepath.ToSlash(filepath.Join(".claude", "commands", entry.Name()))] = false
	}

	readme, err := os.ReadFile(filepath.Join(root, ".claude", "README.md"))
	if err != nil {
		t.Fatalf("read .claude/README.md: %v", err)
	}
	readmeBody := string(readme)
	runner := filepath.Join(scriptsDir, "claude-run.sh")
	if _, err := os.Stat(runner); err != nil {
		t.Fatalf("missing claude runner %s: %v", runner, err)
	}
	if !strings.Contains(readmeBody, "scripts/claude-run.sh") {
		t.Fatalf(".claude/README.md does not document runner scripts/claude-run.sh")
	}

	wrappers, err := filepath.Glob(filepath.Join(scriptsDir, "claude-zv-*.sh"))
	if err != nil {
		t.Fatalf("glob claude wrappers: %v", err)
	}
	var mappedWrappers int
	for _, wrapper := range wrappers {
		b, err := os.ReadFile(wrapper)
		if err != nil {
			t.Fatalf("read %s: %v", wrapper, err)
		}
		command, ok := claudeWrapperCommandPath(string(b))
		if !ok {
			t.Fatalf("%s does not exec scripts/claude-run.sh with a command prompt", wrapper)
		}
		if _, ok := commands[command]; !ok {
			t.Fatalf("%s references missing claude command %s", wrapper, command)
		}
		commands[command] = true
		mappedWrappers++
		if name := filepath.ToSlash(filepath.Join("scripts", filepath.Base(wrapper))); !strings.Contains(readmeBody, name) {
			t.Fatalf(".claude/README.md does not document wrapper %s", name)
		}
	}
	if mappedWrappers == 0 {
		t.Fatalf("no claude prompt wrappers found")
	}
	for command, used := range commands {
		if !used {
			t.Fatalf("claude command %s has no wrapper", command)
		}
	}
}

func TestCodexPromptWrappersDryRunEndToEnd(t *testing.T) {
	root := repoRoot(t)
	for _, fixture := range currentCodexPromptWrappers(t, root) {
		t.Run(filepath.Base(fixture.wrapper), func(t *testing.T) {
			promptBody := readFileString(t, filepath.Join(root, filepath.FromSlash(fixture.prompt)))
			out := runAgentWrapperDryRun(t, root, fixture.wrapper, []string{"CODEX_DRY_RUN=1"}, "dry run task")
			for _, want := range []string{
				"repo:",
				"prompt:",
				filepath.ToSlash(fixture.prompt),
				"command: codex",
				"--- final prompt ---",
				"## User task",
				"dry run task",
			} {
				if !strings.Contains(filepath.ToSlash(out), filepath.ToSlash(want)) {
					t.Fatalf("output for %s = %q, want %q", fixture.wrapper, out, want)
				}
			}
			if !strings.Contains(normalizedText(out), strings.TrimRight(normalizedText(promptBody), "\n")) {
				t.Fatalf("output for %s did not include prompt body from %s", fixture.wrapper, fixture.prompt)
			}
		})
	}
}

func TestClaudePromptWrappersDryRunEndToEnd(t *testing.T) {
	root := repoRoot(t)
	for _, fixture := range currentClaudePromptWrappers(t, root) {
		t.Run(filepath.Base(fixture.wrapper), func(t *testing.T) {
			commandBody := readFileString(t, filepath.Join(root, filepath.FromSlash(fixture.command)))
			firstLine := strings.TrimSpace(strings.SplitN(commandBody, "\n", 2)[0])
			out := runAgentWrapperDryRun(t, root, fixture.wrapper, []string{"CLAUDE_DRY_RUN=1"}, "dry run task")
			for _, want := range []string{
				firstLine,
				"User task:",
				"dry run task",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("output for %s = %q, want %q", fixture.wrapper, out, want)
				}
			}
			if !strings.Contains(normalizedText(out), strings.TrimRight(normalizedText(commandBody), "\n")) {
				t.Fatalf("output for %s did not include command body from %s", fixture.wrapper, fixture.command)
			}
		})
	}
}

func TestCodexRunnerDryRunIncludesStdinContextEndToEnd(t *testing.T) {
	root := repoRoot(t)
	stdout, stderr := runAgentRunnerDryRunWithInput(t, root, "CODEX_DRY_RUN=1", "scripts/codex-run.sh", ".codex/prompts/go-plan.md", "argument task", "stdin task context\n")
	if stderr != "" {
		t.Fatalf("codex-run dry-run stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"--- final prompt ---",
		"## User task",
		"argument task",
		"## Stdin task/context",
		"stdin task context",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("codex-run dry-run stdout = %q, want %q", stdout, want)
		}
	}
}

func TestClaudeRunnerDryRunIncludesStdinContextEndToEnd(t *testing.T) {
	root := repoRoot(t)
	stdout, stderr := runAgentRunnerDryRunWithInput(t, root, "CLAUDE_DRY_RUN=1", "scripts/claude-run.sh", ".claude/commands/zv-plan.md", "argument task", "stdin task context\n")
	if stderr != "" {
		t.Fatalf("claude-run dry-run stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"User task:",
		"argument task",
		"## Stdin task/context",
		"stdin task context",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("claude-run dry-run stdout = %q, want %q", stdout, want)
		}
	}
}

func TestCodexPromptsUseProjectGate(t *testing.T) {
	root := repoRoot(t)
	tests := []struct {
		path string
		want []string
	}{
		{
			path: filepath.Join(root, ".codex", "prompts", "go-tdd.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".codex", "prompts", "go-bugfix.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".codex", "prompts", "go-pr-ready.md"),
			want: []string{
				"scripts/go-gate.sh",
				"scripts/go-gate.sh --no-format",
				"scripts/go-gate.sh --race",
				"scripts/go-gate.sh --security",
			},
		},
		{
			path: filepath.Join(root, ".codex", "prompts", "go-concurrency-review.md"),
			want: []string{
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".codex", "prompts", "go-security-review.md"),
			want: []string{
				"scripts/go-gate.sh --security",
			},
		},
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			b, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			body := string(b)
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Fatalf("%s does not contain %q", tt.path, want)
				}
			}
		})
	}
}

func TestClaudeCommandsUseProjectGate(t *testing.T) {
	root := repoRoot(t)
	tests := []struct {
		path string
		want []string
	}{
		{
			path: filepath.Join(root, ".claude", "commands", "zv-tdd.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".claude", "commands", "zv-bugfix.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".claude", "commands", "zv-parser-change.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
			},
		},
		{
			path: filepath.Join(root, ".claude", "commands", "zv-media-change.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
			},
		},
		{
			path: filepath.Join(root, ".claude", "commands", "zv-worker-api-change.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race --no-format",
			},
		},
		{
			path: filepath.Join(root, ".claude", "commands", "zv-pr-ready.md"),
			want: []string{
				"scripts/go-gate.sh --no-format",
				"`zv check`",
				"scripts/go-gate.sh --race",
				"scripts/go-gate.sh --security",
			},
		},
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.path), func(t *testing.T) {
			b, err := os.ReadFile(tt.path)
			if err != nil {
				t.Fatalf("read %s: %v", tt.path, err)
			}
			body := string(b)
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Fatalf("%s does not contain %q", tt.path, want)
				}
			}
		})
	}
}

func TestFixLoopRunsProjectCheck(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "scripts", "fix-loop.ps1")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(b)
	for _, want := range []string{
		`Invoke-Step "zv check"`,
		"go run ./cmd/zv check",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s does not contain %q", path, want)
		}
	}
}

func TestMakefileRunsProjectCheck(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "Makefile")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(b)
	for _, want := range []string{
		"check:",
		"go run ./cmd/zv check",
		"workflows-check:",
		"go run ./cmd/zv workflows check",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s does not contain %q", path, want)
		}
	}
	if !strings.Contains(body, ".PHONY:") || !strings.Contains(body, "check") || !strings.Contains(body, "workflows-check") {
		t.Fatalf("%s does not mark check targets as phony", path)
	}
}

func TestCurrentBuildScriptsCoverCommandEntrypoints(t *testing.T) {
	root := repoRoot(t)
	commands, err := commandEntrypoints(root)
	if err != nil {
		t.Fatalf("command entrypoints: %v", err)
	}
	if len(commands) == 0 {
		t.Fatalf("no command entrypoints found")
	}

	makefileBody := readFileString(t, filepath.Join(root, "Makefile"))
	buildScriptBody := readFileString(t, filepath.Join(root, "scripts", "build.ps1"))
	known := make(map[string]struct{}, len(commands))
	for _, command := range commands {
		known[command] = struct{}{}
		makeTarget := fmt.Sprintf("go build -o bin/%s ./cmd/%s", command, command)
		if !strings.Contains(makefileBody, makeTarget) {
			t.Fatalf("Makefile does not build %s with %q", command, makeTarget)
		}
		buildEntry := fmt.Sprintf(`"%s"`, command)
		if !strings.Contains(buildScriptBody, buildEntry) {
			t.Fatalf("scripts/build.ps1 does not include command entry %s", buildEntry)
		}
	}
	for _, target := range makefileCommandBuildTargets(makefileBody) {
		if _, ok := known[target.Command]; !ok {
			t.Fatalf("Makefile builds stale command %q with %q", target.Command, target.Line)
		}
	}
	for _, command := range buildScriptCommandEntries(buildScriptBody) {
		if _, ok := known[command]; !ok {
			t.Fatalf("scripts/build.ps1 includes stale command entry %q", command)
		}
	}
}

func TestCurrentWorkflowDocsUseUnifiedCLI(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "README.md"),
		filepath.Join(root, "docs", "README.md"),
		filepath.Join(root, "docs", "toolchain.md"),
	}
	legacyCommands := legacyWorkflowCommands()
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body := string(b)
		for _, legacy := range legacyCommands {
			if strings.Contains(body, legacy) {
				t.Fatalf("%s contains legacy workflow command %q; use ./bin/zv instead", path, legacy)
			}
		}
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	body := string(readme)
	for _, want := range []string{
		"./bin/zv demo parse",
		"./bin/zv demo players",
		"./bin/zv compose final",
		"./bin/zv shorts render",
		"./bin/zv analysis tactical-data",
		"./bin/zv analysis view",
		"./bin/zv serve",
		"./bin/zv pipeline",
		"./bin/zv check",
		"./bin/zv check --format json",
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
		"./bin/zv workflows run compose-final",
		"./bin/zv workflows run analysis-tactical-data",
		"./bin/zv workflows run analysis-viewer",
		"./bin/zv workflows run project-check",
		"./bin/zv workflows check",
		"./bin/zv workflows check --format json",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("README.md does not contain unified workflow command %q", want)
		}
	}
	skills, err := loadSkills()
	if err != nil {
		t.Fatalf("load skills: %v", err)
	}
	if len(skills) == 0 {
		t.Fatalf("no repo-local skills found")
	}
	for _, skill := range skills {
		if !strings.Contains(body, skill.Name) {
			t.Fatalf("README.md does not document repo skill %q", skill.Name)
		}
	}
	toolchain, err := os.ReadFile(filepath.Join(root, "docs", "toolchain.md"))
	if err != nil {
		t.Fatalf("read docs/toolchain.md: %v", err)
	}
	if !strings.Contains(string(toolchain), `.\\bin\\zv.exe record`) && !strings.Contains(string(toolchain), `.\bin\zv.exe record`) {
		t.Fatalf("docs/toolchain.md does not document unified capture command")
	}
	if !strings.Contains(string(toolchain), `zv check`) {
		t.Fatalf("docs/toolchain.md does not document the project check command")
	}
}

func installFakeSubcommands(t *testing.T, binDir string, names ...string) {
	t.Helper()
	currentExe, err := os.Executable()
	if err != nil {
		t.Fatalf("current executable: %v", err)
	}
	for _, name := range names {
		dst := filepath.Join(binDir, executableName(name))
		copyFile(t, currentExe, dst)
		if runtime.GOOS != "windows" {
			if err := os.Chmod(dst, 0o755); err != nil {
				t.Fatalf("chmod %s: %v", dst, err)
			}
		}
	}
}

func installFakeDelegatedSubcommands(t *testing.T, binDir string) {
	t.Helper()
	installFakeSubcommands(t, binDir, defaultLegacyCommandEntrypointNames()...)
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open %s: %v", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		t.Fatalf("copy %s to %s: %v", src, dst, err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close %s: %v", dst, err)
	}
}

func readFakeSubcommandCalls(t *testing.T, path string) []fakeSubcommandCall {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open calls log: %v", err)
	}
	defer f.Close()
	var calls []fakeSubcommandCall
	dec := json.NewDecoder(f)
	for dec.More() {
		var call fakeSubcommandCall
		if err := dec.Decode(&call); err != nil {
			t.Fatalf("decode calls log: %v", err)
		}
		calls = append(calls, call)
	}
	return calls
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = parent
	}
}

func currentRepoSkills(t *testing.T, root string) []skillInfo {
	t.Helper()
	skillsDir := filepath.Join(root, ".codex", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	var skills []skillInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			t.Fatalf("stat skill %s: %v", entry.Name(), err)
		}
		skill, err := parseSkill(path)
		if err != nil {
			t.Fatalf("parse skill %s: %v", path, err)
		}
		if skill.Name == "" {
			skill.Name = entry.Name()
		}
		skills = append(skills, skill)
	}
	if len(skills) == 0 {
		t.Fatalf("no repo skills found in %s", skillsDir)
	}
	return skills
}

func currentAgentPromptWrappers(t *testing.T, root string) []string {
	t.Helper()
	var wrappers []string
	for _, fixture := range currentCodexPromptWrappers(t, root) {
		wrappers = append(wrappers, fixture.wrapper)
	}
	for _, fixture := range currentClaudePromptWrappers(t, root) {
		wrappers = append(wrappers, fixture.wrapper)
	}
	return wrappers
}

func currentCodexPromptWrappers(t *testing.T, root string) []codexPromptWrapperFixture {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "scripts", "codex*.sh"))
	if err != nil {
		t.Fatalf("glob codex wrappers: %v", err)
	}
	var fixtures []codexPromptWrapperFixture
	for _, wrapper := range matches {
		if filepath.Base(wrapper) == "codex-run.sh" {
			continue
		}
		relWrapper := filepath.ToSlash(mustRel(root, wrapper))
		body := readFileString(t, wrapper)
		prompt, ok := codexWrapperPromptPath(body)
		if !ok {
			t.Fatalf("%s does not exec scripts/codex-run.sh with a prompt", relWrapper)
		}
		fixtures = append(fixtures, codexPromptWrapperFixture{
			wrapper: relWrapper,
			prompt:  prompt,
		})
	}
	if len(fixtures) == 0 {
		t.Fatalf("no codex prompt wrappers found")
	}
	return fixtures
}

func currentClaudePromptWrappers(t *testing.T, root string) []claudePromptWrapperFixture {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(root, "scripts", "claude-zv-*.sh"))
	if err != nil {
		t.Fatalf("glob claude wrappers: %v", err)
	}
	var fixtures []claudePromptWrapperFixture
	for _, wrapper := range matches {
		relWrapper := filepath.ToSlash(mustRel(root, wrapper))
		body := readFileString(t, wrapper)
		command, ok := claudeWrapperCommandPath(body)
		if !ok {
			t.Fatalf("%s does not exec scripts/claude-run.sh with a command prompt", relWrapper)
		}
		fixtures = append(fixtures, claudePromptWrapperFixture{
			wrapper: relWrapper,
			command: command,
		})
	}
	if len(fixtures) == 0 {
		t.Fatalf("no claude prompt wrappers found")
	}
	return fixtures
}

func repoSkillWorkflowRunCommands(t *testing.T, root string) [][]string {
	t.Helper()
	skillsDir := filepath.Join(root, ".codex", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	var commands [][]string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, line := range skillCommandLines(string(b)) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			commands = append(commands, command)
		}
	}
	return commands
}

type repoSkillWorkflowRunCommand struct {
	skillName string
	command   []string
}

func repoSkillWorkflowRunCommandsBySkill(t *testing.T, root string) []repoSkillWorkflowRunCommand {
	t.Helper()
	skillsDir := filepath.Join(root, ".codex", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	var commands []repoSkillWorkflowRunCommand
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, line := range skillCommandLines(string(b)) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			commands = append(commands, repoSkillWorkflowRunCommand{
				skillName: entry.Name(),
				command:   command,
			})
		}
	}
	return commands
}

func skillWorkflowRunCommandsFromBody(t *testing.T, body string) [][]string {
	t.Helper()
	var commands [][]string
	for _, line := range skillCommandLines(body) {
		command, ok := skillCommand(line)
		if !ok || !isExecutableWorkflowRunCommand(command) {
			continue
		}
		commands = append(commands, command)
	}
	return commands
}

func currentWorkflowDocRunCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || !isExecutableWorkflowRunCommand(command) {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentWorkflowDocDirectCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || isWorkflowRunCommand(command) {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentWorkflowDocShowCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "workflows" || command[1] != "show" {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentSkillDocShowCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 3 || command[0] != "skills" || command[1] != "show" {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentSkillDocListAndCheckCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "skills" {
				continue
			}
			if command[1] != "list" && command[1] != "check" {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentWorkflowDocListAndCheckCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || len(command) < 2 || command[0] != "workflows" {
				continue
			}
			if command[1] != "list" && command[1] != "check" {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentProjectCheckDocCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, doc := range currentWorkflowDocBodies(t, root) {
		for _, line := range skillCommandLines(doc.body) {
			command, ok := skillCommand(line)
			if !ok || len(command) == 0 || command[0] != "check" {
				continue
			}
			key := strings.Join(command, "\x00")
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			commands = append(commands, command)
		}
	}
	return commands
}

func currentInternalCheckWorkflowDocRunCommands(t *testing.T, root string) [][]string {
	t.Helper()
	seen := make(map[string]struct{})
	var commands [][]string
	for _, command := range currentWorkflowDocRunCommands(t, root) {
		if len(command) < 3 {
			continue
		}
		switch command[2] {
		case "skills-check", "workflows-check", "project-check":
		default:
			continue
		}
		key := strings.Join(command, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		commands = append(commands, command)
	}
	return commands
}

func workflowRunCheckFormat(command []string) (string, error) {
	if len(command) == 3 {
		return "text", nil
	}
	if len(command) < 4 || command[3] != "--" {
		return "", fmt.Errorf(`missing "--" separator before forwarded args`)
	}
	format, rest, err := parseFormatArgs(command[4:])
	if err != nil {
		return "", err
	}
	if len(rest) != 0 {
		return "", fmt.Errorf("unexpected forwarded args %q", strings.Join(rest, " "))
	}
	return format, nil
}

type workflowDocBody struct {
	path string
	body string
}

func currentWorkflowDocBodies(t *testing.T, root string) []workflowDocBody {
	t.Helper()
	var docs []workflowDocBody
	for _, doc := range workflowDocs() {
		path := filepath.Join(root, filepath.FromSlash(doc.Path))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", doc.Path, err)
		}
		docs = append(docs, workflowDocBody{path: doc.Path, body: string(b)})
	}
	return docs
}

func currentRepoSkillBodies(t *testing.T, root string) []workflowDocBody {
	t.Helper()
	skillsDir := filepath.Join(root, ".codex", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	var docs []workflowDocBody
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		relPath := filepath.ToSlash(filepath.Join(".codex", "skills", entry.Name(), "SKILL.md"))
		path := filepath.Join(root, filepath.FromSlash(relPath))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", relPath, err)
		}
		docs = append(docs, workflowDocBody{path: relPath, body: string(b)})
	}
	return docs
}

func recordCommandArgsForPublishedExample(command []string) ([]string, bool) {
	if len(command) > 0 && command[0] == "record" {
		return command[1:], true
	}
	if len(command) < 3 || command[0] != "workflows" || command[1] != "run" || command[2] != "record" {
		return nil, false
	}
	for i := 3; i < len(command); i++ {
		if command[i] == "--" {
			return command[i+1:], true
		}
	}
	return command[3:], true
}

func recordCommandHasDryRunOrCaptureTools(args []string) bool {
	return recordCommandDryRunIsTrue(args) ||
		(recordCommandFlagHasValue(args, "--hlae") && recordCommandFlagHasValue(args, "--cs2"))
}

func recordCommandDryRunIsTrue(args []string) bool {
	for i, arg := range args {
		if arg == "--dry-run" {
			if i+1 < len(args) {
				if value, err := strconv.ParseBool(args[i+1]); err == nil {
					return value
				}
			}
			return true
		}
		const prefix = "--dry-run="
		if strings.HasPrefix(arg, prefix) {
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, prefix))
			return err == nil && value
		}
	}
	return false
}

func recordCommandFlagHasValue(args []string, flag string) bool {
	for i, arg := range args {
		if arg == flag {
			return i+1 < len(args) && args[i+1] != "" && !strings.HasPrefix(args[i+1], "--")
		}
		if strings.HasPrefix(arg, flag+"=") {
			return strings.TrimPrefix(arg, flag+"=") != ""
		}
	}
	return false
}

func workflowRunCommandArgs(t *testing.T, workflow workflowInfo) []string {
	t.Helper()
	fields, ok := splitCommandFields(workflow.RunCommand)
	if !ok {
		t.Fatalf("parse workflow run command %q", workflow.RunCommand)
	}
	if len(fields) < 4 || fields[0] != "zv" || fields[1] != "workflows" || fields[2] != "run" {
		t.Fatalf("workflow run command = %#v, want zv workflows run <name>", fields)
	}
	return append([]string(nil), fields[1:]...)
}

func workflowRunSampleForwardedArgs(t *testing.T, workflow workflowInfo, galleryPath string) []string {
	t.Helper()
	switch workflow.Name {
	case "demo-parse":
		return []string{"--", "--demo", "inferno.dem", "--steamid", "76561198000000000", "--out", "run/plan.json"}
	case "demo-players":
		return []string{"--", "--demo", "inferno.dem"}
	case "utility-audit":
		return []string{"--", "--plan", "run/plan.json", "--lineup-catalog", "data/lineups", "--out", "run/utility-audit.csv"}
	case "record":
		return []string{"--", "--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/recording", "--dry-run"}
	case "compose-final":
		return []string{"--", "--recording-result", "run/recording/recording-result.json", "--out", "run/final.mp4", "--dry-run"}
	case "shorts-render":
		return []string{"--", "--recording-result", "run/recording/recording-result.json", "--out", "run/shorts"}
	case "analysis-tactical-data":
		return []string{"--", "--demo", "inferno.dem", "--out", "run/tactical.json", "--start", "1000", "--end", "2000"}
	case "analysis-viewer":
		return []string{"--", "--json", "run/analysis.json"}
	case "gallery-open":
		return []string{"--", "--path", galleryPath}
	case "pipeline":
		return []string{"--", "--killplan", "run/plan.json", "--demo", "inferno.dem", "--out", "run/pipeline", "--hlae", "HLAE.exe", "--cs2", "cs2.exe"}
	case "skills-check", "workflows-check", "project-check", "serve":
		return nil
	default:
		t.Fatalf("missing sample forwarded args for workflow %q", workflow.Name)
		return nil
	}
}

func workflowDirectSampleArgs(t *testing.T, workflow workflowInfo, galleryPath string) []string {
	t.Helper()
	args := append([]string(nil), workflow.RunArgs...)
	forwarded := workflowRunSampleForwardedArgs(t, workflow, galleryPath)
	if len(forwarded) == 0 {
		return args
	}
	if forwarded[0] != "--" {
		t.Fatalf("workflow %q sample forwarded args = %#v, want leading --", workflow.Name, forwarded)
	}
	return append(args, forwarded[1:]...)
}

func workflowRunSampleArgsWithoutSeparator(t *testing.T, workflow workflowInfo, galleryPath string) []string {
	t.Helper()
	forwarded := workflowRunSampleForwardedArgs(t, workflow, galleryPath)
	if len(forwarded) > 0 {
		if forwarded[0] != "--" {
			t.Fatalf("workflow %q sample forwarded args = %#v, want leading --", workflow.Name, forwarded)
		}
		return append([]string(nil), forwarded[1:]...)
	}
	switch workflow.Name {
	case "skills-check", "workflows-check", "project-check":
		return []string{"--format", "json"}
	case "serve":
		return []string{"--help"}
	default:
		t.Fatalf("missing separator sample args for workflow %q", workflow.Name)
		return nil
	}
}

func assertWorkflowDiscoveryMatches(t *testing.T, source string, got workflowInfo, want workflowInfo) {
	t.Helper()
	if got.Name != want.Name {
		t.Fatalf("%s name = %q, want %q", source, got.Name, want.Name)
	}
	if got.Description != want.Description {
		t.Fatalf("%s description for %s = %q, want %q", source, want.Name, got.Description, want.Description)
	}
	if got.Command != want.Command {
		t.Fatalf("%s command for %s = %q, want %q", source, want.Name, got.Command, want.Command)
	}
	if got.RunCommand != want.RunCommand {
		t.Fatalf("%s run_command for %s = %q, want %q", source, want.Name, got.RunCommand, want.RunCommand)
	}
	if got.RunArgs != nil {
		t.Fatalf("%s run args for %s = %#v, want omitted from json", source, want.Name, got.RunArgs)
	}
}

func assertJSONKeys(t *testing.T, source string, row map[string]json.RawMessage, want ...string) {
	t.Helper()
	wantSet := make(map[string]struct{}, len(want))
	for _, key := range want {
		wantSet[key] = struct{}{}
		if _, ok := row[key]; !ok {
			t.Fatalf("%s missing json key %q in %#v", source, key, row)
		}
	}
	for key := range row {
		if _, ok := wantSet[key]; !ok {
			t.Fatalf("%s has unexpected json key %q in %#v", source, key, row)
		}
	}
}

func assertIssueJSONKeys(t *testing.T, source string, raw json.RawMessage) {
	t.Helper()
	var issues []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &issues); err != nil {
		t.Fatalf("unmarshal %s: %v\n%s", source, err, raw)
	}
	if len(issues) == 0 {
		t.Fatalf("%s len = 0, want issues", source)
	}
	for i, issue := range issues {
		assertJSONKeys(t, fmt.Sprintf("%s[%d]", source, i), issue, "path", "message")
	}
}

func duplicateFlagValue(t *testing.T, args []string, flag string) []string {
	t.Helper()
	dup := append([]string(nil), args...)
	for i, arg := range dup {
		if arg != flag {
			continue
		}
		if i+1 >= len(dup) {
			t.Fatalf("flag %s has no value in %#v", flag, args)
		}
		insert := []string{flag, dup[i+1]}
		dup = append(dup[:i+2], append(insert, dup[i+2:]...)...)
		return dup
	}
	t.Fatalf("flag %s not found in %#v", flag, args)
	return nil
}

func equalsRequiredFlags(t *testing.T, args []string, required []string) []string {
	t.Helper()
	converted := append([]string(nil), args...)
	for _, flag := range required {
		var found bool
		for i := 0; i < len(converted); i++ {
			if converted[i] != flag {
				continue
			}
			if i+1 >= len(converted) {
				t.Fatalf("flag %s has no value in %#v", flag, args)
			}
			converted[i] = flag + "=" + converted[i+1]
			converted = append(converted[:i+1], converted[i+2:]...)
			found = true
			break
		}
		if !found {
			t.Fatalf("flag %s not found in %#v", flag, args)
		}
	}
	return converted
}

func emptyEqualsRequiredFlag(t *testing.T, args []string, flag string) []string {
	t.Helper()
	converted := append([]string(nil), args...)
	for i := 0; i < len(converted); i++ {
		if converted[i] != flag {
			continue
		}
		if i+1 >= len(converted) {
			t.Fatalf("flag %s has no value in %#v", flag, args)
		}
		converted[i] = flag + "="
		return append(converted[:i+1], converted[i+2:]...)
	}
	t.Fatalf("flag %s not found in %#v", flag, args)
	return nil
}

func emptySeparateRequiredFlag(t *testing.T, args []string, flag string) []string {
	t.Helper()
	converted := append([]string(nil), args...)
	for i := 0; i < len(converted); i++ {
		if converted[i] != flag {
			continue
		}
		if i+1 >= len(converted) {
			t.Fatalf("flag %s has no value in %#v", flag, args)
		}
		converted[i+1] = ""
		return converted
	}
	t.Fatalf("flag %s not found in %#v", flag, args)
	return nil
}

func skillListText(skills []skillInfo) string {
	var b strings.Builder
	for _, skill := range skills {
		if skill.Description == "" {
			fmt.Fprintln(&b, skill.Name)
			continue
		}
		fmt.Fprintf(&b, "%s\t%s\n", skill.Name, skill.Description)
	}
	return b.String()
}

func skillNames(skills []skillInfo) []string {
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	return names
}

func workflowListText(workflows []workflowInfo) string {
	var b strings.Builder
	for _, workflow := range workflows {
		fmt.Fprintf(&b, "%s\t%s\n", workflow.Name, workflow.Description)
	}
	return b.String()
}

func workflowShowText(workflow workflowInfo) string {
	return fmt.Sprintf("%s\n%s\n\ncommand: %s\nrun_command: %s\n", workflow.Name, workflow.Description, workflow.Command, workflow.RunCommand)
}

func decodeWorkflowCheckResult(t *testing.T, body string) workflowCheckResult {
	t.Helper()
	var result workflowCheckResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("unmarshal workflow check json: %v\n%s", err, body)
	}
	return result
}

func commandKeys(commands [][]string) []string {
	keys := make([]string, 0, len(commands))
	for _, command := range commands {
		keys = append(keys, strings.Join(command, "\x00"))
	}
	return keys
}

func helpCommandStem(stem string) string {
	fields, ok := splitCommandFields(stem)
	if !ok || len(fields) == 0 {
		return ""
	}
	switch fields[0] {
	case "./bin/zv", `.\bin\zv.exe`, "zv":
		fields[0] = "zv"
	default:
		return ""
	}
	return strings.Join(fields, " ")
}

func workflowNames(workflows []workflowInfo) []string {
	names := make([]string, 0, len(workflows))
	for _, workflow := range workflows {
		names = append(names, workflow.Name)
	}
	return names
}

func workflowForDirectCommand(command []string) (workflowInfo, bool) {
	var matched workflowInfo
	var matchedLen int
	for _, workflow := range workflowCatalog() {
		if len(workflow.RunArgs) <= matchedLen {
			continue
		}
		if isExecutableDirectWorkflowCommand(command, workflow) {
			matched = workflow
			matchedLen = len(workflow.RunArgs)
		}
	}
	return matched, matchedLen > 0
}

func workflowDirectDocCommandIsComparable(workflow workflowInfo) bool {
	return workflowDelegatesExternally(workflow) || workflow.Name == "gallery-open"
}

func workflowRunArgsForDirectCommand(t *testing.T, workflow workflowInfo, directArgs []string) []string {
	t.Helper()
	if len(directArgs) < len(workflow.RunArgs) {
		t.Fatalf("direct args %#v shorter than workflow run args %#v", directArgs, workflow.RunArgs)
	}
	for i, arg := range workflow.RunArgs {
		if directArgs[i] != arg {
			t.Fatalf("direct args %#v do not match workflow run args %#v", directArgs, workflow.RunArgs)
		}
	}
	runArgs := workflowRunCommandArgs(t, workflow)
	forwarded := directArgs[len(workflow.RunArgs):]
	if len(forwarded) > 0 {
		runArgs = append(runArgs, "--")
		runArgs = append(runArgs, forwarded...)
	}
	return runArgs
}

func directArgsForWorkflowRunDocCommand(t *testing.T, workflow workflowInfo, runArgs []string) []string {
	t.Helper()
	if len(runArgs) < 3 || runArgs[0] != "workflows" || runArgs[1] != "run" || runArgs[2] != workflow.Name {
		t.Fatalf("workflow run args %#v do not target workflow %q", runArgs, workflow.Name)
	}
	directArgs := append([]string(nil), workflow.RunArgs...)
	if len(runArgs) == 3 {
		return directArgs
	}
	if runArgs[3] != "--" {
		t.Fatalf(`workflow run args %#v are missing "--" separator before forwarded args`, runArgs)
	}
	return append(directArgs, runArgs[4:]...)
}

func assertDiscoveredWorkflowRunMatchesDirect(t *testing.T, exe, root, source string, index int, discovered workflowInfo, catalogWorkflow workflowInfo, galleryPath string) {
	t.Helper()
	runArgs := workflowRunCommandArgs(t, discovered)
	if len(runArgs) < 3 || runArgs[2] != catalogWorkflow.Name {
		t.Fatalf("%s discovered run_command for %s resolved to args %#v", source, catalogWorkflow.Name, runArgs)
	}
	runArgs = append(runArgs, workflowRunSampleForwardedArgs(t, catalogWorkflow, galleryPath)...)
	directArgs := workflowDirectSampleArgs(t, catalogWorkflow, galleryPath)

	prefix := fmt.Sprintf("%02d-%s-%s", index, source, catalogWorkflow.Name)
	runSubcommandLog := filepath.Join(root, prefix+"-discovered-run.jsonl")
	directSubcommandLog := filepath.Join(root, prefix+"-direct.jsonl")
	runOpenLog := filepath.Join(root, prefix+"-discovered-run-open.txt")
	directOpenLog := filepath.Join(root, prefix+"-direct-open.txt")

	runOut := runZVBinaryWithEnv(t, exe, root, []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + runSubcommandLog,
		"ZV_FAKE_OPEN_PATH_LOG=" + runOpenLog,
	}, runArgs...)
	directOut := runZVBinaryWithEnv(t, exe, root, []string{
		"ZV_FAKE_SUBCOMMAND=1",
		"ZV_FAKE_SUBCOMMAND_LOG=" + directSubcommandLog,
		"ZV_FAKE_OPEN_PATH_LOG=" + directOpenLog,
	}, directArgs...)

	if got, want := runOut, directOut; got != want {
		t.Fatalf("%s discovered run_command output = %q, want direct output %q", source, got, want)
	}
	if catalogWorkflow.Name == "gallery-open" {
		if got, want := strings.Join(readLines(t, runOpenLog), "\n"), strings.Join(readLines(t, directOpenLog), "\n"); got != want {
			t.Fatalf("%s discovered run_command open path log = %q, want direct log %q", source, got, want)
		}
		return
	}

	runCalls := readFakeSubcommandCalls(t, runSubcommandLog)
	directCalls := readFakeSubcommandCalls(t, directSubcommandLog)
	if got, want := len(runCalls), 1; got != want {
		t.Fatalf("%s discovered run_command calls len = %d, want %d: %#v", source, got, want, runCalls)
	}
	if got, want := len(directCalls), 1; got != want {
		t.Fatalf("%s direct calls len = %d, want %d: %#v", source, got, want, directCalls)
	}
	if got, want := runCalls[0].Executable, directCalls[0].Executable; got != want {
		t.Fatalf("%s discovered run_command executable = %q, want direct executable %q", source, got, want)
	}
	if got, want := strings.Join(runCalls[0].Args, "\x00"), strings.Join(directCalls[0].Args, "\x00"); got != want {
		t.Fatalf("%s discovered run_command args = %#v, want direct args %#v", source, runCalls[0].Args, directCalls[0].Args)
	}
}

func workflowDelegatesExternally(workflow workflowInfo) bool {
	if len(workflow.RunArgs) == 0 {
		return false
	}
	switch workflow.RunArgs[0] {
	case "check", "gallery", "skills", "workflows":
		return false
	default:
		return true
	}
}

func workflowHelpDelegatesExternally(workflow workflowInfo) bool {
	if workflow.Name == "serve" {
		return false
	}
	return workflowDelegatesExternally(workflow)
}

type codexPromptWrapperFixture struct {
	wrapper string
	prompt  string
}

func agentPromptWrapperFixtures() []string {
	var out []string
	for _, fixture := range codexPromptWrapperFixtures() {
		out = append(out, fixture.wrapper)
	}
	for _, fixture := range claudePromptWrapperFixtures() {
		out = append(out, fixture.wrapper)
	}
	return out
}

func codexPromptWrapperFixtures() []codexPromptWrapperFixture {
	return []codexPromptWrapperFixture{
		{wrapper: "scripts/codex-go-bugfix.sh", prompt: ".codex/prompts/go-bugfix.md"},
		{wrapper: "scripts/codex-go-concurrency-review.sh", prompt: ".codex/prompts/go-concurrency-review.md"},
		{wrapper: "scripts/codex-go-pr-ready.sh", prompt: ".codex/prompts/go-pr-ready.md"},
		{wrapper: "scripts/codex-go-readability-review.sh", prompt: ".codex/prompts/go-readability-review.md"},
		{wrapper: "scripts/codex-go-security-review.sh", prompt: ".codex/prompts/go-security-review.md"},
		{wrapper: "scripts/codex-go-tdd.sh", prompt: ".codex/prompts/go-tdd.md"},
		{wrapper: "scripts/codex-go-test-review.sh", prompt: ".codex/prompts/go-test-review.md"},
		{wrapper: "scripts/codex-plan.sh", prompt: ".codex/prompts/go-plan.md"},
		{wrapper: "scripts/codex-review-diff.sh", prompt: ".codex/prompts/review-diff.md"},
		{wrapper: "scripts/codex-spike.sh", prompt: ".codex/prompts/go-spike.md"},
	}
}

type claudePromptWrapperFixture struct {
	wrapper string
	command string
}

func claudePromptWrapperFixtures() []claudePromptWrapperFixture {
	return []claudePromptWrapperFixture{
		{wrapper: "scripts/claude-zv-artifact-audit.sh", command: ".claude/commands/zv-artifact-audit.md"},
		{wrapper: "scripts/claude-zv-bugfix.sh", command: ".claude/commands/zv-bugfix.md"},
		{wrapper: "scripts/claude-zv-media-change.sh", command: ".claude/commands/zv-media-change.md"},
		{wrapper: "scripts/claude-zv-parser-change.sh", command: ".claude/commands/zv-parser-change.md"},
		{wrapper: "scripts/claude-zv-plan.sh", command: ".claude/commands/zv-plan.md"},
		{wrapper: "scripts/claude-zv-pr-ready.sh", command: ".claude/commands/zv-pr-ready.md"},
		{wrapper: "scripts/claude-zv-tdd.sh", command: ".claude/commands/zv-tdd.md"},
		{wrapper: "scripts/claude-zv-toolchain-diagnose.sh", command: ".claude/commands/zv-toolchain-diagnose.md"},
		{wrapper: "scripts/claude-zv-worker-api-change.sh", command: ".claude/commands/zv-worker-api-change.md"},
	}
}

type claudeReviewerAgentFixture struct {
	path string
	body string
}

func claudeReviewerAgentFixtures() []claudeReviewerAgentFixture {
	names := []string{
		"go-readability-reviewer",
		"go-test-reviewer",
		"go-concurrency-reviewer",
		"go-security-reviewer",
		"zv-media-pipeline-reviewer",
	}
	fixtures := make([]claudeReviewerAgentFixture, 0, len(names))
	for _, name := range names {
		extra := ""
		switch name {
		case "go-concurrency-reviewer":
			extra = "\nRecommend `scripts/go-gate.sh --race` when shared state changed.\n"
		case "go-security-reviewer":
			extra = "\nDo not read `.env`, private keys, or token files.\n"
		case "zv-media-pipeline-reviewer":
			extra = "\nAvoid tests that require real HLAE/CS2/large media unless explicitly requested.\n"
		}
		fixtures = append(fixtures, claudeReviewerAgentFixture{
			path: ".claude/agents/" + name + ".md",
			body: strings.Join([]string{
				"---",
				"name: " + name,
				"description: Review " + name,
				"model: sonnet",
				"tools: [Read, Bash]",
				"---",
				"",
				"Use `BLOCKER`, `WARNING`, and `NIT`.",
				"Every finding needs file/path, problem, why it matters, and a practical fix.",
				"If clean, say `No blocking issues found.`.",
				extra,
			}, "\n"),
		})
	}
	return fixtures
}

func codexPromptFixtureBody(prompt string) string {
	switch prompt {
	case ".codex/prompts/go-tdd.md", ".codex/prompts/go-bugfix.md":
		return strings.Join([]string{
			"# Prompt",
			"",
			"Run `scripts/go-gate.sh --no-format` so tests, vet, `zv check`, and static analysis share the project contract.",
			"If concurrency/shared state changed, run `scripts/go-gate.sh --race --no-format`.",
			"",
		}, "\n")
	case ".codex/prompts/go-pr-ready.md":
		return strings.Join([]string{
			"# Prompt",
			"",
			"Run `scripts/go-gate.sh`; use `scripts/go-gate.sh --no-format` in dirty repos.",
			"If concurrency changed, run `scripts/go-gate.sh --race`.",
			"If security changed, run `scripts/go-gate.sh --security`.",
			"",
		}, "\n")
	case ".codex/prompts/go-concurrency-review.md":
		return "# Prompt\n\nRecommend `scripts/go-gate.sh --race --no-format`.\n"
	case ".codex/prompts/go-security-review.md":
		return "# Prompt\n\nRecommend `scripts/go-gate.sh --security`.\n"
	default:
		return "# " + prompt + "\n"
	}
}

func writeSkill(t *testing.T, root, name, description string) {
	t.Helper()
	writeSkillBody(t, root, name, strings.Join([]string{
		"---",
		"name: " + name,
		`description: "` + description + `"`,
		"---",
		"",
		"# " + name,
		"",
		"Workflow details.",
		"",
	}, "\n"))
}

func writeSkillBody(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, ".codex", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write skill: %v", err)
	}
}

func replaceSkillShowFixture(t *testing.T, path, textReplacement, jsonReplacement string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(b)
	for old, replacement := range map[string]string{
		"./bin/zv skills show alpha":               textReplacement,
		"./bin/zv skills show alpha --format json": jsonReplacement,
	} {
		if !strings.Contains(body, old) {
			t.Fatalf("%s fixture does not contain expected skill show line %q", path, old)
		}
		body = strings.Replace(body, old, replacement, 1)
	}
	writeFile(t, path, body)
}

func writeWorkflowDocs(t *testing.T, root string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "README.md"), strings.Join([]string{
		"# ZackVideo",
		"",
		"```bash",
		"./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv demo players --demo testdata/foo.dem",
		"./bin/zv utility audit --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv",
		"./bin/zv record --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv compose final --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4",
		"./bin/zv shorts render --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts",
		"./bin/zv analysis tactical-data --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000",
		"./bin/zv analysis view --json data/analysis/MarcusN1-deaths.json",
		"./bin/zv gallery open --path data/runs/run-004/shorts/publish/index.html",
		"./bin/zv check",
		"./bin/zv check --format json",
		"./bin/zv serve",
		"./bin/zv pipeline --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/pipeline --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv skills check",
		"./bin/zv skills list --format json",
		"./bin/zv skills show alpha",
		"./bin/zv skills show alpha --format json",
		"./bin/zv skills check --format json",
		"alpha",
		"./bin/zv workflows list",
		"./bin/zv workflows list --format json",
		"./bin/zv workflows show demo-parse",
		"./bin/zv workflows show demo-parse --format json",
		"./bin/zv workflows show demo-players",
		"./bin/zv workflows show demo-players --format json",
		"./bin/zv workflows show utility-audit",
		"./bin/zv workflows show utility-audit --format json",
		"./bin/zv workflows show record",
		"./bin/zv workflows show record --format json",
		"./bin/zv workflows show compose-final",
		"./bin/zv workflows show compose-final --format json",
		"./bin/zv workflows show shorts-render",
		"./bin/zv workflows show shorts-render --format json",
		"./bin/zv workflows show analysis-tactical-data",
		"./bin/zv workflows show analysis-tactical-data --format json",
		"./bin/zv workflows show analysis-viewer",
		"./bin/zv workflows show analysis-viewer --format json",
		"./bin/zv workflows show gallery-open",
		"./bin/zv workflows show gallery-open --format json",
		"./bin/zv workflows show serve",
		"./bin/zv workflows show serve --format json",
		"./bin/zv workflows show pipeline",
		"./bin/zv workflows show pipeline --format json",
		"./bin/zv workflows show skills-check",
		"./bin/zv workflows show skills-check --format json",
		"./bin/zv workflows show workflows-check",
		"./bin/zv workflows show workflows-check --format json",
		"./bin/zv workflows show project-check",
		"./bin/zv workflows show project-check --format json",
		"./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv workflows run demo-players -- --demo testdata/foo.dem",
		"./bin/zv workflows run utility-audit -- --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv",
		"./bin/zv workflows run record -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv workflows run compose-final -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4",
		"./bin/zv workflows run shorts-render -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts",
		"./bin/zv workflows run analysis-tactical-data -- --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000",
		"./bin/zv workflows run analysis-viewer -- --json data/analysis/MarcusN1-deaths.json",
		"./bin/zv workflows run gallery-open -- --path data/runs/run-004/shorts/publish/index.html",
		"./bin/zv workflows run serve",
		"./bin/zv workflows run pipeline -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/pipeline --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv workflows run skills-check",
		"./bin/zv workflows run skills-check -- --format json",
		"./bin/zv workflows run workflows-check",
		"./bin/zv workflows run workflows-check -- --format json",
		"./bin/zv workflows run project-check",
		"./bin/zv workflows run project-check -- --format json",
		"./bin/zv workflows check",
		"./bin/zv workflows check --format json",
		"```",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "docs", "toolchain.md"), strings.Join([]string{
		"# Toolchain",
		"",
		"`zv check` validates the unified CLI contract.",
		"",
		"```powershell",
		`.\bin\zv.exe record --killplan plan.json --demo demo.dem --out recording --hlae C:\HLAE-2.190.1\HLAE.exe --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"`,
		"```",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "docs", "README.md"), strings.Join([]string{
		"# Docs",
		"",
		"```bash",
		"./bin/zv check",
		"./bin/zv skills list",
		"./bin/zv workflows list",
		"./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"```",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "smoke-real.ps1"), strings.Join([]string{
		`Fail "Orchestrator is not reachable. Start bin\zv serve with the current environment and run migrations first."`,
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "smoke.sh"), strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`BASE="${ZV_BASE_URL:-http://localhost:8080}"`,
		`curl -fsS "$BASE/api/jobs"`,
		`curl -fsS "$BASE/api/jobs/$ID"`,
		`curl -fsS "$BASE/api/jobs/$ID/plan"`,
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "Makefile"), strings.Join([]string{
		"build:",
		"\tgo build -o bin/zv ./cmd/zv",
		"",
		"test:",
		"\tgo test ./... -count=1",
		"\tgo run ./cmd/zv check",
		"\tgo run ./cmd/zv workflows check",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "build.ps1"), strings.Join([]string{
		`$commands = @(`,
		`    "zv",`,
		`)`,
		`foreach ($name in $commands) {`,
		`    $out = Join-Path $binDir "$name.exe"`,
		`    $pkg = "./cmd/$name"`,
		`    & go build -o $out $pkg`,
		`}`,
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "go-gate.sh"), strings.Join([]string{
		`echo "== zv check =="`,
		"go run ./cmd/zv check",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "fix-loop.ps1"), strings.Join([]string{
		`Invoke-Step "zv check" {`,
		"    & go run ./cmd/zv check",
		"}",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "check-codex-harness.sh"), strings.Join([]string{
		"mapfile -t shell_scripts < <(find scripts -maxdepth 1 -type f -name '*.sh' | sort)",
		`bash -n "${shell_scripts[@]}"`,
		`echo "== ZackVideo workflow contract =="`,
		"go run ./cmd/zv check",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "scripts", "codex-run.sh"), strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`prompt_file="$1"`,
		`shift || true`,
		`exec codex --cd "$(git rev-parse --show-toplevel)" exec - < "$prompt_file"`,
		"",
	}, "\n"))
	for _, fixture := range codexPromptWrapperFixtures() {
		writeFile(t, filepath.Join(root, filepath.FromSlash(fixture.prompt)), codexPromptFixtureBody(fixture.prompt))
		writeFile(t, filepath.Join(root, filepath.FromSlash(fixture.wrapper)), strings.Join([]string{
			"#!/usr/bin/env bash",
			"set -euo pipefail",
			`root="$(git rev-parse --show-toplevel)"`,
			fmt.Sprintf(`exec "$root/scripts/codex-run.sh" %s "$@"`, fixture.prompt),
			"",
		}, "\n"))
	}
	writeFile(t, filepath.Join(root, ".codex", "README.md"), strings.Join([]string{
		"# Codex",
		"",
		"```bash",
		"scripts/codex-run.sh",
		"scripts/codex-plan.sh",
		"scripts/codex-go-tdd.sh",
		"scripts/codex-go-bugfix.sh",
		"scripts/codex-go-pr-ready.sh",
		"scripts/codex-review-diff.sh",
		"scripts/codex-spike.sh",
		"scripts/codex-go-readability-review.sh",
		"scripts/codex-go-test-review.sh",
		"scripts/codex-go-concurrency-review.sh",
		"scripts/codex-go-security-review.sh",
		"```",
		"",
		"```bash",
		"./bin/zv skills list",
		"./bin/zv skills show alpha",
		"./bin/zv skills check",
		"alpha",
		"./bin/zv check",
		"./bin/zv check --format json",
		"./bin/zv skills list --format json",
		"./bin/zv skills show alpha --format json",
		"./bin/zv skills check --format json",
		"./bin/zv workflows list",
		"./bin/zv workflows list --format json",
		"./bin/zv workflows show demo-parse",
		"./bin/zv workflows show demo-parse --format json",
		"./bin/zv workflows show demo-players",
		"./bin/zv workflows show demo-players --format json",
		"./bin/zv workflows show utility-audit",
		"./bin/zv workflows show utility-audit --format json",
		"./bin/zv workflows show record",
		"./bin/zv workflows show record --format json",
		"./bin/zv workflows show compose-final",
		"./bin/zv workflows show compose-final --format json",
		"./bin/zv workflows show shorts-render",
		"./bin/zv workflows show shorts-render --format json",
		"./bin/zv workflows show analysis-tactical-data",
		"./bin/zv workflows show analysis-tactical-data --format json",
		"./bin/zv workflows show analysis-viewer",
		"./bin/zv workflows show analysis-viewer --format json",
		"./bin/zv workflows show gallery-open",
		"./bin/zv workflows show gallery-open --format json",
		"./bin/zv workflows show serve",
		"./bin/zv workflows show serve --format json",
		"./bin/zv workflows show pipeline",
		"./bin/zv workflows show pipeline --format json",
		"./bin/zv workflows show skills-check",
		"./bin/zv workflows show skills-check --format json",
		"./bin/zv workflows show workflows-check",
		"./bin/zv workflows show workflows-check --format json",
		"./bin/zv workflows show project-check",
		"./bin/zv workflows show project-check --format json",
		"./bin/zv demo parse --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv demo players --demo testdata/foo.dem",
		"./bin/zv utility audit --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv",
		"./bin/zv record --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv compose final --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4",
		"./bin/zv shorts render --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts",
		"./bin/zv analysis tactical-data --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000",
		"./bin/zv analysis view --json data/analysis/MarcusN1-deaths.json",
		"./bin/zv gallery open --path data/runs/run-004/shorts/publish/index.html",
		"./bin/zv serve",
		"./bin/zv pipeline --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/pipeline --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv workflows run demo-parse -- --demo testdata/foo.dem --steamid 76561198000000000 --out plan.json",
		"./bin/zv workflows run demo-players -- --demo testdata/foo.dem",
		"./bin/zv workflows run utility-audit -- --plan plan-utility.json --lineup-catalog data/lineups --out utility-audit.csv",
		"./bin/zv workflows run record -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/recording --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv workflows run compose-final -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/final.mp4",
		"./bin/zv workflows run shorts-render -- --recording-result data/runs/run-004/recording/recording-result.json --out data/runs/run-004/shorts",
		"./bin/zv workflows run analysis-tactical-data -- --demo testdata/foo.dem --out data/runs/run-004/tactical.json --start 1000 --end 2000",
		"./bin/zv workflows run analysis-viewer -- --json data/analysis/MarcusN1-deaths.json",
		"./bin/zv workflows run gallery-open -- --path data/runs/run-004/shorts/publish/index.html",
		"./bin/zv workflows run serve",
		"./bin/zv workflows run pipeline -- --killplan plan.json --demo testdata/foo.dem --out data/runs/run-004/pipeline --hlae C:\\HLAE-2.190.1\\HLAE.exe --cs2 \"C:\\Games\\Counter-Strike 2\\game\\bin\\win64\\cs2.exe\"",
		"./bin/zv workflows run skills-check",
		"./bin/zv workflows run skills-check -- --format json",
		"./bin/zv workflows run workflows-check",
		"./bin/zv workflows run workflows-check -- --format json",
		"./bin/zv workflows run project-check",
		"./bin/zv workflows run project-check -- --format json",
		"./bin/zv workflows check",
		"./bin/zv workflows check --format json",
		"```",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, "AGENTS.md"), strings.Join([]string{
		"# Agents",
		"",
		"```bash",
		`CODEX_DRY_RUN=1 scripts/codex-run.sh .codex/prompts/go-tdd.md "custom prompt run"`,
		`C:\HLAE-2.190.1\HLAE.exe`,
		`C:\HLAE\HLAE.exe`,
		`scripts/codex-go-tdd.sh "implement a behavior change"`,
		`scripts/codex-go-bugfix.sh "fix a bug with a regression test"`,
		`scripts/codex-go-pr-ready.sh`,
		`scripts/go-gate.sh --no-format`,
		`scripts/go-gate.sh --race`,
		`scripts/go-gate.sh --security`,
		"```",
		"",
	}, "\n"))
	for _, fixture := range claudePromptWrapperFixtures() {
		writeFile(t, filepath.Join(root, filepath.FromSlash(fixture.command)), claudeCommandFixtureBody(fixture.command))
		writeFile(t, filepath.Join(root, filepath.FromSlash(fixture.wrapper)), strings.Join([]string{
			"#!/usr/bin/env bash",
			"set -euo pipefail",
			`root="$(git rev-parse --show-toplevel)"`,
			fmt.Sprintf(`exec "$root/scripts/claude-run.sh" %s "$@"`, fixture.command),
			"",
		}, "\n"))
	}
	writeFile(t, filepath.Join(root, "scripts", "claude-run.sh"), strings.Join([]string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`prompt_file="$1"`,
		`shift || true`,
		`exec claude -p "$(cat "$prompt_file")" "$@"`,
		"",
	}, "\n"))
	claudeReadmeLines := []string{
		"# Claude",
		"",
		"Rules: .claude/rules/go-style.md and .claude/rules/zackvideo-operations.md",
		"",
		"```bash",
		"scripts/claude-run.sh",
	}
	for _, fixture := range claudePromptWrapperFixtures() {
		claudeReadmeLines = append(claudeReadmeLines, fixture.wrapper)
	}
	claudeReadmeLines = append(claudeReadmeLines,
		"```",
		"",
		"```text",
		"@go-readability-reviewer review the current diff",
		"@go-test-reviewer review the tests in this diff",
		"@go-concurrency-reviewer review shared-state changes",
		"@go-security-reviewer review filesystem/subprocess/security changes",
		"@zv-media-pipeline-reviewer review FFmpeg/rendering changes",
		"```",
		"",
	)
	writeFile(t, filepath.Join(root, ".claude", "README.md"), strings.Join(claudeReadmeLines, "\n"))
	writeFile(t, filepath.Join(root, "CLAUDE.md"), strings.Join([]string{
		"# Claude",
		"",
		"Style rules: .claude/rules/go-style.md",
		"Operational rules: .claude/rules/zackvideo-operations.md",
		"",
		"```bash",
		`CLAUDE_DRY_RUN=1 scripts/claude-run.sh .claude/commands/zv-tdd.md "custom prompt run"`,
		`C:\HLAE-2.190.1\HLAE.exe`,
		`C:\HLAE\HLAE.exe`,
		`scripts/claude-zv-tdd.sh "implement a behavior change"`,
		`scripts/claude-zv-bugfix.sh "fix a bug with a regression test"`,
		`scripts/claude-zv-pr-ready.sh`,
		`scripts/go-gate.sh --no-format`,
		`scripts/go-gate.sh --race`,
		`scripts/go-gate.sh --security`,
		"```",
		"",
		"```text",
		"@go-readability-reviewer review the current diff",
		"@go-test-reviewer review the tests in this diff",
		"@go-concurrency-reviewer review shared-state changes",
		"@go-security-reviewer review filesystem/subprocess/security changes",
		"@zv-media-pipeline-reviewer review FFmpeg/rendering changes",
		"```",
		"",
	}, "\n"))
	for _, fixture := range claudeReviewerAgentFixtures() {
		writeFile(t, filepath.Join(root, filepath.FromSlash(fixture.path)), fixture.body)
	}
	writeFile(t, filepath.Join(root, ".claude", "rules", "go-style.md"), strings.Join([]string{
		"# Go style rule",
		"",
		"Use Google-style Go: clarity, simplicity, concision, maintainability, and repo consistency.",
		"Avoid `util`, `common`, `helper`, `manager`, and generic service layers.",
		"Return errors with context.",
		"Respect context cancellation around subprocesses, DB, Redis, HTTP, and workers.",
		"Every goroutine needs an owner, a stop condition, and a test strategy.",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, ".claude", "rules", "zackvideo-operations.md"), strings.Join([]string{
		"# ZackVideo operational rule",
		"",
		"Safe by default:",
		"- `scripts/go-gate.sh --no-format` after targeted tests pass",
		"",
		"Ask first:",
		"- HLAE/CS2 launch or real capture",
		"- Docker compose and database migrations",
		"- cleanup scripts that delete artifacts",
		"",
		"Never add generated `.mp4`, `.mov`, `.webm`, `.avi`, `.mkv`, `.dem`, frame, or large render artifacts to git.",
		"",
	}, "\n"))
	writeFile(t, filepath.Join(root, ".claude", "settings.json"), claudeSettingsFixture())
}

func claudeCommandFixtureBody(command string) string {
	switch command {
	case ".claude/commands/zv-plan.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Read-only. Do not edit files.",
			"Run `git status --short`.",
			"",
			"Output:",
			"",
			"- Tests and verification",
			"- Risks / open questions",
			"",
		}, "\n")
	case ".claude/commands/zv-tdd.md", ".claude/commands/zv-bugfix.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Run `scripts/go-gate.sh --no-format` so tests, vet, `zv check`, and static analysis share the project contract.",
			"If concurrency/shared state changed, run `scripts/go-gate.sh --race --no-format`.",
			"",
		}, "\n")
	case ".claude/commands/zv-parser-change.md", ".claude/commands/zv-media-change.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Run targeted package tests first.",
			"If broad, run `scripts/go-gate.sh --no-format`; it includes `zv check`.",
			"",
		}, "\n")
	case ".claude/commands/zv-worker-api-change.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Run targeted package tests first.",
			"If broad, run `scripts/go-gate.sh --no-format`; it includes `zv check`.",
			"If concurrency/shared state changed, run `scripts/go-gate.sh --race --no-format`.",
			"",
		}, "\n")
	case ".claude/commands/zv-pr-ready.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Run `scripts/go-gate.sh --no-format`; it includes `zv check`.",
			"If concurrency changed, run `scripts/go-gate.sh --race`.",
			"If security changed, run `scripts/go-gate.sh --security`.",
			"",
		}, "\n")
	case ".claude/commands/zv-artifact-audit.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Read-only. Do not edit or delete files.",
			"Run `git status --short`.",
			"Inspect `.gitignore`.",
			"Check generated run data under `data/`.",
			"Output Suggested commands for manual cleanup only.",
			"",
		}, "\n")
	case ".claude/commands/zv-toolchain-diagnose.md":
		return strings.Join([]string{
			"# Command",
			"",
			"Read-only diagnosis. Do not install tools or edit files unless the user asks.",
			"Run `scripts/go-tools-check.sh`.",
			"Inspect `scripts/check-toolchain.ps1`.",
			"Do not run CS2/HLAE, Docker compose, migrations, or renders.",
			"Output Exact next commands.",
			"",
		}, "\n")
	default:
		return "# " + command + "\n"
	}
}

func claudeSettingsFixture() string {
	return strings.Join([]string{
		"{",
		`  "permissions": {`,
		`    "allow": [`,
		`      "Read",`,
		`      "Edit",`,
		`      "Write",`,
		`      "WebSearch",`,
		`      "WebFetch",`,
		`      "Bash(git status*)",`,
		`      "Bash(git diff*)",`,
		`      "Bash(git log*)",`,
		`      "Bash(go test*)",`,
		`      "Bash(go vet*)",`,
		`      "Bash(gofmt*)",`,
		`      "Bash(goimports*)",`,
		`      "Bash(staticcheck*)",`,
		`      "Bash(govulncheck*)",`,
		`      "Bash(gosec*)",`,
		`      "Bash(scripts/go-format-changed.sh*)",`,
		`      "Bash(scripts/go-gate.sh*)",`,
		`      "Bash(scripts/go-tools-check.sh*)",`,
		`      "Bash(scripts/check-codex-harness.sh*)",`,
		`      "Bash(powershell.exe -NoProfile -ExecutionPolicy Bypass -File scripts/check-toolchain.ps1*)",`,
		`      "Bash(pwsh -NoProfile -ExecutionPolicy Bypass -File scripts/check-toolchain.ps1*)"`,
		`    ],`,
		`    "ask": [`,
		`      "Bash(go mod tidy*)",`,
		`      "Bash(go get*)",`,
		`      "Bash(go install*)",`,
		`      "Bash(git commit*)",`,
		`      "Bash(git push*)",`,
		`      "Bash(git reset*)",`,
		`      "Bash(git clean*)",`,
		`      "Bash(docker*)",`,
		`      "Bash(docker compose*)",`,
		`      "Bash(ffmpeg*)",`,
		`      "Bash(powershell.exe*)",`,
		`      "Bash(pwsh*)",`,
		`      "Bash(scripts/build.ps1*)",`,
		`      "Bash(scripts/cleanup-artifacts.ps1*)",`,
		`      "Bash(scripts/audit-security-performance.ps1*)"`,
		`    ],`,
		`    "deny": [`,
		`      "Read(.env)",`,
		`      "Read(**/.env)",`,
		`      "Read(**/*id_rsa*)",`,
		`      "Read(**/*id_ed25519*)",`,
		`      "Read(**/*secret*)",`,
		`      "Read(**/*token*)",`,
		`      "Bash(rm -rf *)",`,
		`      "Bash(git reset --hard*)",`,
		`      "Bash(git push --force*)"`,
		`    ]`,
		`  }`,
		"}",
		"",
	}, "\n")
}

func TestRunWorkflowsCheckRejectsUnsafeClaudeReadOnlyCommand(t *testing.T) {
	tempDir := t.TempDir()
	writeSkillBody(t, tempDir, "alpha", strings.Join([]string{
		"---",
		"name: alpha",
		`description: "Alpha workflow"`,
		"---",
		"",
		"```powershell",
		`.\bin\zv.exe workflows run demo-parse -- --demo demo.dem --steamid 76561198000000000 --out plan.json`,
		"```",
		"",
	}, "\n"))
	writeWorkflowDocs(t, tempDir)
	writeFile(t, filepath.Join(tempDir, ".claude", "commands", "zv-toolchain-diagnose.md"), strings.Join([]string{
		"Diagnose local toolchain.",
		"",
		"Run `scripts/go-tools-check.sh`.",
		"Install missing tools automatically.",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		`.claude/commands/zv-toolchain-diagnose.md: missing standard gate guidance "Read-only diagnosis. Do not install tools or edit files unless the user asks."`,
		`.claude/commands/zv-toolchain-diagnose.md: missing standard gate guidance "Do not run CS2/HLAE, Docker compose, migrations, or renders."`,
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func appendFile(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open append %s: %v", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(body); err != nil {
		t.Fatalf("append %s: %v", path, err)
	}
}

func hasIssue(issues []skillIssue, want string) bool {
	for _, issue := range issues {
		if issue.Path+": "+issue.Message == want {
			return true
		}
	}
	return false
}

func hasIssueContaining(issues []skillIssue, want string) bool {
	for _, issue := range issues {
		if strings.Contains(issue.Path+": "+issue.Message, want) {
			return true
		}
	}
	return false
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
}

func buildZVBinary(t *testing.T, tempDir string) string {
	t.Helper()
	exe := filepath.Join(tempDir, executableName("zv"))
	cmd := exec.Command("go", "build", "-o", exe, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build ./cmd/zv: %v\n%s", err, out)
	}
	return exe
}

func runZVBinary(t *testing.T, exe, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", exe, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func runZVBinarySplit(t *testing.T, exe, dir string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func runZVBinarySplitWithEnv(t *testing.T, exe, dir string, env []string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func runZVBinarySplitWithEnvAndInput(t *testing.T, exe, dir string, env []string, input string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func runZVBinaryFailure(t *testing.T, exe, dir string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s %s succeeded unexpectedly\n%s", exe, strings.Join(args, " "), out)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("%s %s failed without exit code: %v\n%s", exe, strings.Join(args, " "), err, out)
	}
	return string(out), exitErr.ExitCode()
}

func runZVBinaryFailureSplit(t *testing.T, exe, dir string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("%s %s succeeded unexpectedly\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), stdout.String(), stderr.String())
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("%s %s failed without exit code: %v\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String(), exitErr.ExitCode()
}

func runZVBinaryFailureSplitWithEnv(t *testing.T, exe, dir string, env []string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatalf("%s %s succeeded unexpectedly\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), stdout.String(), stderr.String())
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("%s %s failed without exit code: %v\nstdout:\n%s\nstderr:\n%s", exe, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String(), exitErr.ExitCode()
}

func runAgentWrapperDryRun(t *testing.T, root, wrapper string, env []string, task string) string {
	t.Helper()
	var script strings.Builder
	script.WriteString("set -euo pipefail\n")
	for _, item := range env {
		script.WriteString("export ")
		script.WriteString(item)
		script.WriteString("\n")
	}
	script.WriteString("bash ")
	script.WriteString(shellQuote(filepath.ToSlash(wrapper)))
	script.WriteString(" ")
	script.WriteString(shellQuote(task))
	cmd := exec.Command("bash", "-c", script.String())
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s dry run failed: %v\n%s", wrapper, err, out)
	}
	return string(out)
}

func runAgentRunnerDryRunWithInput(t *testing.T, root, env, runner, prompt, task, input string) (string, string) {
	t.Helper()
	cmd := exec.Command("bash", "-c", env+" "+shellQuote(runner)+" "+shellQuote(prompt)+" "+shellQuote(task))
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s dry run failed: %v\nstdout:\n%s\nstderr:\n%s", runner, err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func normalizedText(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}

func bashPath(path string) string {
	path = filepath.ToSlash(path)
	if len(path) >= 3 && path[1] == ':' && path[2] == '/' {
		return "/mnt/" + strings.ToLower(path[:1]) + "/" + path[3:]
	}
	return path
}

func findPowerShell() (string, bool) {
	for _, name := range []string{"powershell.exe", "powershell", "pwsh"} {
		path, err := exec.LookPath(name)
		if err == nil {
			return path, true
		}
	}
	return "", false
}

func runZVBinaryWithEnv(t *testing.T, exe, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", exe, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func assertPathDoesNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s exists, want no file", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s: %v", path, err)
	}
}
