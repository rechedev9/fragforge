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
			want:    "PRODUCT.md: documents legacy direct command ./bin/zv-parser",
		},
		{
			name:    "demo players",
			command: "./bin/zv-demo-players --demo demo.dem",
			want:    "PRODUCT.md: documents legacy direct command ./bin/zv-demo-players",
		},
		{
			name:    "analysis viewer",
			command: "./bin/zv-analysis-viewer --input data/analysis.json",
			want:    "PRODUCT.md: documents legacy direct command ./bin/zv-analysis-viewer",
		},
		{
			name:    "windows bin path",
			command: `bin\zv-recorder --killplan plan.json --demo demo.dem --out recording`,
			want:    `PRODUCT.md: documents legacy direct command bin\zv-recorder`,
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
			appendFile(t, filepath.Join(tempDir, "PRODUCT.md"), "\n"+tt.command+"\n")
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

func TestRunWorkflowsCheckRejectsReadmeFiles(t *testing.T) {
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
	writeFile(t, filepath.Join(tempDir, "notes", "README.md"), "# Ambiguous documentation\n")
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := "notes/README.md: README files are not allowed; use a purpose-specific document name"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
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
	appendFile(t, filepath.Join(tempDir, "PRODUCT.md"), "\n./bin/zv-new-flow --demo demo.dem\n")
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := "PRODUCT.md: documents legacy direct command ./bin/zv-new-flow"; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
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
	appendFile(t, filepath.Join(tempDir, "PRODUCT.md"), strings.Join([]string{
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
	if !strings.Contains(stderr.String(), `PRODUCT.md: uses non-standard zv command "parser"`) {
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
		`CODEX_DRY_RUN=1 scripts/codex-run.sh .codex/prompts/go-tdd.md "custom prompt run"`,
		`scripts/codex-go-tdd.sh "implement a behavior change"`,
		`scripts/codex-go-bugfix.sh "fix a bug with a regression test"`,
		`scripts/codex-go-pr-ready.sh`,
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
		`CLAUDE.md: missing canonical workflow command highest installed HLAE version`,
		`CLAUDE.md: missing canonical workflow command latest official HLAE release`,
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

func TestRunWorkflowsCheckRejectsClaudeRulesMirror(t *testing.T) {
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
	writeFile(t, filepath.Join(tempDir, ".claude", "rules", "go-style.md"), strings.Join([]string{
		"# Go style rule",
		"",
		"A stray mirror of the CLAUDE.md style section.",
		"",
	}, "\n"))
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if want := `.claude/rules/go-style.md: style rules live in CLAUDE.md; remove this .claude/rules mirror`; !strings.Contains(stderr.String(), want) {
		t.Fatalf("stderr = %q, want %q", stderr.String(), want)
	}
}

func TestRunWorkflowsCheckRejectsMissingClaudeStyleGuidance(t *testing.T) {
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
	claudePath := filepath.Join(tempDir, "CLAUDE.md")
	body, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read CLAUDE.md fixture: %v", err)
	}
	stripped := strings.ReplaceAll(string(body), "Every goroutine must have a clear owner and stop condition.", "")
	writeFile(t, claudePath, stripped)
	webClaudePath := filepath.Join(tempDir, "web", "CLAUDE.md")
	webBody, err := os.ReadFile(webClaudePath)
	if err != nil {
		t.Fatalf("read web/CLAUDE.md fixture: %v", err)
	}
	webStripped := strings.ReplaceAll(string(webBody), "No `any`, ever: use `unknown` and narrow it.", "")
	writeFile(t, webClaudePath, webStripped)
	withWorkingDir(t, tempDir)

	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "check"}, &stdout, &stderr, nil, &fakeRunner{})

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	for _, want := range []string{
		`CLAUDE.md: missing style guidance "Every goroutine must have a clear owner and stop condition."`,
		`web/CLAUDE.md: missing style guidance "No ` + "`any`" + `, ever"`,
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
	appendFile(t, filepath.Join(tempDir, "PRODUCT.md"), "\n./bin/zv-parser parse --demo demo.dem --steamid 76561198000000000\n")
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
		"shorts-render\tRender vertical or landscape videos",
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
		"Render vertical or landscape videos",
		"command: zv shorts render --recording-result <recording-result.json> --out <shorts-dir>",
		"run_command: zv workflows run shorts-render",
		"validate_command: zv workflows validate shorts-render",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	}
}

func TestWorkflowCatalogExposesAgentExecutionMetadata(t *testing.T) {
	for _, workflow := range workflowCatalog() {
		if workflow.Arguments.Positionals == nil {
			t.Fatalf("workflow %q positionals are nil, want an empty array when absent", workflow.Name)
		}
		if workflow.Arguments.RequiredFlags == nil {
			t.Fatalf("workflow %q required flags are nil, want an empty array when absent", workflow.Name)
		}
		if workflow.Arguments.OptionalValueFlags == nil {
			t.Fatalf("workflow %q optional value flags are nil, want an empty array when absent", workflow.Name)
		}
		if workflow.Arguments.BooleanFlags == nil {
			t.Fatalf("workflow %q boolean flags are nil, want an empty array when absent", workflow.Name)
		}
		if workflow.Arguments.ValueConstraints == nil {
			t.Fatalf("workflow %q value constraints are nil, want an empty array when absent", workflow.Name)
		}
		if workflow.Arguments.ConditionalRequirements == nil {
			t.Fatalf("workflow %q conditional requirements are nil, want an empty array when absent", workflow.Name)
		}
		for _, flag := range workflow.Arguments.RequiredFlags {
			if containsString(workflow.Arguments.OptionalValueFlags, flag) {
				t.Fatalf("workflow %q exposes required flag %q as optional", workflow.Name, flag)
			}
		}
		if issues := validateWorkflowValueConstraintMetadata(workflow); len(issues) != 0 {
			t.Fatalf("workflow %q value constraint issues = %#v, want none", workflow.Name, issues)
		}
	}

	short, ok := findWorkflow("short")
	if !ok {
		t.Fatal("short workflow not found")
	}
	if got, want := strings.Join(short.Arguments.RequiredFlags, " "), "--prompt"; got != want {
		t.Fatalf("short required flags = %q, want %q", got, want)
	}
	if got, want := len(short.Arguments.Positionals), 1; got != want {
		t.Fatalf("short positionals = %#v, want one", short.Arguments.Positionals)
	}
	if got := short.Arguments.Positionals[0]; got.Name != "demo" || got.Placeholder != "<demo.dem>" || got.Required {
		t.Fatalf("short demo positional = %#v, want conditional demo path", got)
	}
	if got, want := len(short.Arguments.ConditionalRequirements), 1; got != want {
		t.Fatalf("short conditional requirements = %#v, want one", short.Arguments.ConditionalRequirements)
	}
	shortRequirement := short.Arguments.ConditionalRequirements[0]
	if got, want := strings.Join(shortRequirement.UnlessAnyFlags, " "), "--from-recording"; got != want {
		t.Fatalf("short conditional unless flags = %q, want %q", got, want)
	}
	if got, want := strings.Join(shortRequirement.RequiredPositionals, " "), "demo"; got != want {
		t.Fatalf("short conditional positionals = %q, want %q", got, want)
	}
	if !short.Safety.SupportsDryRun || !short.Safety.LongRunning || short.Safety.ReadOnly {
		t.Fatalf("short safety = %#v, want mutating long-running workflow with dry-run", short.Safety)
	}
	shortPreset := workflowValueConstraintForFlag(t, short, "--preset")
	if got, want := strings.Join(shortPreset.AllowedValues, " "), "viral-60-clean"; got != want {
		t.Fatalf("short preset values = %q, want %q", got, want)
	}
	if shortPreset.Default != "viral-60-clean" || shortPreset.DiscoveryCommand != "zv presets --format json" {
		t.Fatalf("short preset metadata = %#v, want default and discovery command", shortPreset)
	}
	shortFormat := workflowValueConstraintForFlag(t, short, "--format")
	if got, want := strings.Join(shortFormat.AllowedValues, " "), "text json"; got != want || shortFormat.Default != "text" {
		t.Fatalf("short format metadata = %#v, want text/json with text default", shortFormat)
	}

	parse, ok := findWorkflow("demo-parse")
	if !ok {
		t.Fatal("demo-parse workflow not found")
	}
	if got, want := strings.Join(parse.Arguments.RequiredFlags, " "), "--demo --steamid --out"; got != want {
		t.Fatalf("demo-parse required flags = %q, want %q", got, want)
	}
	if got, want := strings.Join(parse.Arguments.OptionalValueFlags, " "), "--segment-mode --rules"; got != want {
		t.Fatalf("demo-parse optional value flags = %q, want %q", got, want)
	}
	if got, want := strings.Join(parse.Arguments.BooleanFlags, " "), "--verbose --dry-run"; got != want {
		t.Fatalf("demo-parse boolean flags = %q, want %q", got, want)
	}
	if !parse.Safety.SupportsDryRun || parse.Safety.ReadOnly || parse.Safety.LongRunning {
		t.Fatalf("demo-parse safety = %#v, want short mutating workflow with dry-run", parse.Safety)
	}
	segmentMode := workflowValueConstraintForFlag(t, parse, "--segment-mode")
	if got, want := strings.Join(segmentMode.AllowedValues, " "), "kills smokes utility"; got != want || segmentMode.Default != "kills" {
		t.Fatalf("demo-parse segment mode metadata = %#v, want kills/smokes/utility with kills default", segmentMode)
	}

	moments, ok := findWorkflow("demo-moments")
	if !ok {
		t.Fatal("demo-moments workflow not found")
	}
	if !containsString(moments.Arguments.BooleanFlags, "--dry-run") {
		t.Fatalf("demo-moments boolean flags = %#v, want --dry-run", moments.Arguments.BooleanFlags)
	}
	if !moments.Safety.SupportsDryRun || moments.Safety.LongRunning {
		t.Fatalf("demo-moments safety = %#v, want dry-run capable and not long-running", moments.Safety)
	}

	demoPlayers, ok := findWorkflow("demo-players")
	if !ok {
		t.Fatal("demo-players workflow not found")
	}
	if demoPlayers.Safety.ReadOnly {
		t.Fatalf("demo-players safety = %#v, want mutating because --out can persist a roster", demoPlayers.Safety)
	}

	record, ok := findWorkflow("record")
	if !ok {
		t.Fatal("record workflow not found")
	}
	if got, want := strings.Join(record.Arguments.RequiredFlags, " "), "--killplan --demo --out"; got != want {
		t.Fatalf("record required flags = %q, want %q", got, want)
	}
	for _, flag := range []string{"--hlae", "--cs2", "--hud", "--fps"} {
		if !containsString(record.Arguments.OptionalValueFlags, flag) {
			t.Fatalf("record optional value flags = %#v, want %s", record.Arguments.OptionalValueFlags, flag)
		}
	}
	if got := len(record.Arguments.ConditionalRequirements); got != 0 {
		t.Fatalf("record conditional requirements = %#v, want none because capture paths are auto-detected", record.Arguments.ConditionalRequirements)
	}
	if !record.Safety.SupportsDryRun || !record.Safety.LongRunning || record.Safety.ReadOnly {
		t.Fatalf("record safety = %#v, want mutating long-running workflow with dry-run", record.Safety)
	}
	hud := workflowValueConstraintForFlag(t, record, "--hud")
	if got, want := strings.Join(hud.AllowedValues, " "), "gameplay clean deathnotices"; got != want || hud.Default != "gameplay" {
		t.Fatalf("record HUD metadata = %#v, want gameplay/clean/deathnotices with gameplay default", hud)
	}

	projectCheck, ok := findWorkflow("project-check")
	if !ok {
		t.Fatal("project-check workflow not found")
	}
	if got, want := strings.Join(projectCheck.Arguments.OptionalValueFlags, " "), "--format"; got != want {
		t.Fatalf("project-check optional value flags = %q, want %q", got, want)
	}
	if !projectCheck.Safety.ReadOnly || projectCheck.Safety.LongRunning || projectCheck.Safety.SupportsDryRun {
		t.Fatalf("project-check safety = %#v, want short read-only workflow", projectCheck.Safety)
	}

	capabilities, ok := findWorkflow("capabilities")
	if !ok {
		t.Fatal("capabilities workflow not found")
	}
	if got, want := strings.Join(capabilities.Arguments.OptionalValueFlags, " "), "--format"; got != want {
		t.Fatalf("capabilities optional value flags = %q, want %q", got, want)
	}
	if !capabilities.Safety.ReadOnly || capabilities.Safety.LongRunning || capabilities.Safety.SupportsDryRun {
		t.Fatalf("capabilities safety = %#v, want short read-only workflow", capabilities.Safety)
	}

	serve, ok := findWorkflow("serve")
	if !ok {
		t.Fatal("serve workflow not found")
	}
	if !serve.Safety.LongRunning || serve.Safety.ReadOnly || serve.Safety.SupportsDryRun {
		t.Fatalf("serve safety = %#v, want long-running service without dry-run", serve.Safety)
	}
}

func workflowValueConstraintForFlag(t *testing.T, workflow workflowInfo, flag string) workflowValueConstraint {
	t.Helper()
	for _, constraint := range workflow.Arguments.ValueConstraints {
		if constraint.Flag == flag {
			return constraint
		}
	}
	t.Fatalf("workflow %q has no value constraint for %s", workflow.Name, flag)
	return workflowValueConstraint{}
}

func TestRunWorkflowsValidateJSONDoesNotExecute(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder
	code := Run([]string{
		"zv", "workflows", "validate", "demo-parse", "--format", "json", "--",
		"--demo", "inferno.dem",
		"--steamid", "76561198000000000",
		"--out", "plan.json",
		"--verbose",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty", got)
	}
	if got := runner.name; got != "" {
		t.Fatalf("runner.name = %q, want no delegated command", got)
	}
	var result workflowValidationResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if !result.OK || result.Executed || result.Error != "" {
		t.Fatalf("result = %#v, want valid unexecuted preflight", result)
	}
	if got, want := result.Scope, "arguments"; got != want {
		t.Fatalf("scope = %q, want %q", got, want)
	}
	if got, want := result.Workflow, "demo-parse"; got != want {
		t.Fatalf("workflow = %q, want %q", got, want)
	}
	if got, want := strings.Join(result.Argv, " "), "zv demo parse --demo inferno.dem --steamid 76561198000000000 --out plan.json --verbose"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
	if result.Safety == nil || result.Safety.ReadOnly || result.Safety.LongRunning || !result.Safety.SupportsDryRun {
		t.Fatalf("safety = %#v, want short mutating workflow with dry-run", result.Safety)
	}
}

func TestRunWorkflowsValidateJSONReturnsStructuredFailureWithoutExecuting(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder
	code := Run([]string{
		"zv", "workflows", "validate", "record", "--format=json", "--",
		"--killplan", "plan.json",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty for JSON failure", got)
	}
	if got := runner.name; got != "" {
		t.Fatalf("runner.name = %q, want no delegated command", got)
	}
	var result workflowValidationResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if result.OK || result.Executed {
		t.Fatalf("result = %#v, want invalid unexecuted preflight", result)
	}
	if got, want := result.Scope, "arguments"; got != want {
		t.Fatalf("scope = %q, want %q", got, want)
	}
	if got, want := result.Error, `missing required flags --demo, --out for "record"`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
	if got, want := strings.Join(result.Argv, " "), "zv record --killplan plan.json"; got != want {
		t.Fatalf("argv = %q, want %q", got, want)
	}
}

func TestRunWorkflowsValidateRejectsUnknownConstrainedValueWithoutExecuting(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder
	code := Run([]string{
		"zv", "workflows", "validate", "demo-parse", "--format=json", "--",
		"--demo", "inferno.dem",
		"--steamid", "76561198000000000",
		"--out", "plan.json",
		"--segment-mode=banana",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if got := stderr.String(); got != "" {
		t.Fatalf("stderr = %q, want empty for JSON failure", got)
	}
	if got := runner.name; got != "" {
		t.Fatalf("runner.name = %q, want no delegated command", got)
	}
	var result workflowValidationResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if result.OK || result.Executed {
		t.Fatalf("result = %#v, want invalid unexecuted preflight", result)
	}
	if got, want := result.Error, `invalid value "banana" for flag --segment-mode in workflow "demo-parse"; allowed values: kills, smokes, utility`; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestRunWorkflowsValidateRejectsForwardedFlagsWithoutSeparator(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder
	code := Run([]string{
		"zv", "workflows", "validate", "demo-parse",
		"--demo", "inferno.dem",
		"--steamid", "76561198000000000",
		"--out", "plan.json",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if got := runner.name; got != "" {
		t.Fatalf("runner.name = %q, want no delegated command", got)
	}
	if !strings.Contains(stderr.String(), `use "--" before workflow flags`) {
		t.Fatalf("stderr = %q, want separator guidance", stderr.String())
	}
}

func TestRunWorkflowsValidateLongRunningWorkflowDoesNotExecute(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder
	code := Run([]string{"zv", "workflows", "validate", "serve", "--format", "json"}, &stdout, &stderr, nil, runner)

	if got, want := code, exitSuccess; got != want {
		t.Fatalf("code = %d, want %d; stderr=%s", got, want, stderr.String())
	}
	if got := runner.name; got != "" {
		t.Fatalf("runner.name = %q, want no delegated command", got)
	}
	var result workflowValidationResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("unmarshal stdout: %v\n%s", err, stdout.String())
	}
	if !result.OK || result.Executed || result.Safety == nil || !result.Safety.LongRunning {
		t.Fatalf("result = %#v, want valid unexecuted long-running preflight", result)
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
	if got, want := workflows[0].Name, "short"; got != want {
		t.Fatalf("workflows[0].Name = %q, want %q", got, want)
	}
	if got, want := workflows[0].Command, "zv short <demo.dem> --prompt <prompt>"; got != want {
		t.Fatalf("workflows[0].Command = %q, want %q", got, want)
	}
	if got, want := workflows[0].RunCommand, "zv workflows run short"; got != want {
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

func TestRunWorkflowsRunRejectsUnknownConstrainedValueWithoutExecuting(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder
	code := Run([]string{
		"zv", "workflows", "run", "record", "--",
		"--killplan", "plan.json",
		"--demo", "inferno.dem",
		"--out", "recording",
		"--dry-run",
		"--hud", "banana",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if got := runner.name; got != "" {
		t.Fatalf("runner.name = %q, want no delegated command", got)
	}
	if got, want := stderr.String(), "error: invalid value \"banana\" for flag --hud in workflow \"record\"; allowed values: gameplay, clean, deathnotices\n"+workflowsRunUsage; got != want {
		t.Fatalf("stderr = %q, want %q", got, want)
	}
}

func TestRunWorkflowsRunRejectsDuplicateConstrainedFlagWithoutExecuting(t *testing.T) {
	runner := &fakeRunner{}
	var stdout, stderr strings.Builder
	code := Run([]string{
		"zv", "workflows", "run", "record", "--",
		"--killplan", "plan.json",
		"--demo", "inferno.dem",
		"--out", "recording",
		"--dry-run",
		"--hud", "gameplay",
		"--hud", "banana",
	}, &stdout, &stderr, nil, runner)

	if got, want := code, exitInvalidArgs; got != want {
		t.Fatalf("code = %d, want %d", got, want)
	}
	if got := runner.name; got != "" {
		t.Fatalf("runner.name = %q, want no delegated command", got)
	}
	if !strings.Contains(stderr.String(), "error: duplicate flag --hud for \"record\"") {
		t.Fatalf("stderr = %q, want duplicate flag error", stderr.String())
	}
}

func TestValidateWorkflowValueConstraintMetadataRejectsDrift(t *testing.T) {
	workflow := workflowInfo{
		Name: "bad-values",
		Arguments: workflowArguments{
			OptionalValueFlags: []string{"--mode"},
			ValueConstraints: []workflowValueConstraint{
				{Flag: "--mode", AllowedValues: []string{"", "fast", "fast"}, Default: "slow"},
				{Flag: "--other", AllowedValues: []string{}},
				{Flag: "--mode", AllowedValues: []string{"fast"}, DiscoveryCommand: "other values"},
			},
		},
	}

	issues := validateWorkflowValueConstraintMetadata(workflow)
	for _, want := range []string{
		"value constraint for flag --mode has an empty allowed value",
		`value constraint for flag --mode has duplicate allowed value "fast"`,
		`default "slow" for flag --mode is not an allowed value`,
		"value constraint flag --other is not a declared value flag",
		"value constraint for flag --other has no allowed values",
		"duplicate value constraint for flag --mode",
		"discovery command for flag --mode must start with zv: other values",
	} {
		if !containsString(issues, want) {
			t.Fatalf("issues = %#v, want %q", issues, want)
		}
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
		`workflow:demo-parse: workflow command is not canonical: uses non-standard zv command "demo"; expected "demo parse", "demo players", "demo moments", or "demo select"`,
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
		{Name: "zackvideo-stream-clips"},
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

func TestValidateSkillWorkflowRequirementSkillsRejectsFragForgeSkillWithoutRequirements(t *testing.T) {
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
		"zv-agent",
		"zv-parser",
		"zv-editor",
		"zv-recorder",
		"zv-composer",
		"zv-orchestrator",
		"zv-analysis-viewer",
		"zv-rhythm",
		"zv-tui",
	}

	issues := validateLegacyPassThroughEntrypoints(commands)

	if !hasIssue(issues, "pass-through:tactical-data: legacy pass-through references missing command entrypoint zv-tactical-data") {
		t.Fatalf("issues = %#v, want missing tactical-data pass-through entrypoint", issues)
	}
}
