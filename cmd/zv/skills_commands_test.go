package main

import (
	"encoding/json"
	"strings"
	"testing"
)

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
		`description: "Create CS2 utility Shorts from a demo with FragForge."`,
		"---",
		"",
		"# FragForge CS2 Utility Shorts",
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
