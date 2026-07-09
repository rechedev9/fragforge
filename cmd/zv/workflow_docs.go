package main

func workflowDocs() []workflowDoc {
	return []workflowDoc{
		{
			Path: "README.md",
			Required: []string{
				"./bin/zv demo parse",
				"./bin/zv demo players",
				"./bin/zv record",
				"./bin/zv compose final",
				"./bin/zv music analyze",
				"./bin/zv shorts render",
				"./bin/zv presets",
				"./bin/zv check",
				"./bin/zv serve",
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
				"== FragForge workflow contract ==",
				"go run ./cmd/zv check",
			},
		},
		{
			Path: ".codex/README.md",
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
				"./bin/zv workflows run music-analyze",
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
