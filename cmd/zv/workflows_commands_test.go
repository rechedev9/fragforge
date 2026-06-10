package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestRunWorkflowsCheckRejectsCatalogMissingDiscoveredRepoSkill(t *testing.T) {
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
	catalogPath := filepath.Join(tempDir, "docs", "workflows", "catalog.md")
	b, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatalf("read docs/workflows/catalog.md: %v", err)
	}
	body := strings.ReplaceAll(string(b), "alpha", "")
	writeFile(t, catalogPath, body)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := "docs/workflows/catalog.md: missing repo skill alpha"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsCatalogMissingDiscoveredRepoSkillShowCommand(t *testing.T) {
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
	writeFile(t, filepath.Join(tempDir, "docs", "workflows", "catalog.md"), strings.Join([]string{
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
		"./bin/zv music analyze --input data/music/track.mp4 --out data/runs/run-004/rhythm.json",
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
		"./bin/zv workflows show music-analyze",
		"./bin/zv workflows show music-analyze --format json",
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
		"./bin/zv workflows run music-analyze -- --input data/music/track.mp4 --out data/runs/run-004/rhythm.json",
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
	if want := "docs/workflows/catalog.md: missing skill show command ./bin/zv skills show alpha"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsCatalogMissingDiscoveredRepoSkillShowJSONCommand(t *testing.T) {
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
	catalogPath := filepath.Join(tempDir, "docs", "workflows", "catalog.md")
	body := readFileString(t, catalogPath)
	body = strings.ReplaceAll(body, "./bin/zv skills show alpha --format json\n", "")
	writeFile(t, catalogPath, body)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := "docs/workflows/catalog.md: missing skill show command ./bin/zv skills show alpha --format json"; !strings.Contains(stderr.String(), want) {
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
		"docs/workflows/catalog.md: missing repo skill bravo",
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
		{Name: "zackvideo-cheater-pov-reels"},
		{Name: "zackvideo-cs2-utility-shorts"},
		{Name: "zackvideo-lineup-audit"},
		{Name: "zackvideo-music-scripted-shorts"},
		{Name: "zackvideo-shorts-production"},
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
		"zv-rhythm",
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
