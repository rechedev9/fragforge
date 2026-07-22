package main

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

func skillWorkflowRequirementMap() map[string][]string {
	return map[string][]string{
		"zackvideo-cheater-pov-reels":      {"demo-players", "record", "shorts-render"},
		"zackvideo-cs2-utility-shorts":     {"demo-parse", "utility-audit", "record", "shorts-render", "gallery-open"},
		"zackvideo-lineup-audit":           {"utility-audit"},
		"zackvideo-music-scripted-shorts":  {"demo-parse", "demo-players", "record", "music-analyze", "shorts-render", "gallery-open"},
		"zackvideo-shorts-production":      {"demo-parse", "demo-players", "demo-moments", "demo-select", "utility-audit", "record", "shorts-render", "gallery-open"},
		"zackvideo-stream-clips":           {"stream-variants", "stream-plan", "stream-killfeed", "stream-transcribe", "stream-captions", "stream-render"},
		"zackvideo-youtube-shorts-publish": {"gallery-open"},
	}
}

func groupUsageTexts() map[string]string {
	return map[string]string{
		"faceit":    faceitUsage,
		"demo":      demoUsage,
		"utility":   utilityUsage,
		"compose":   composeUsage,
		"shorts":    shortsUsage,
		"music":     musicUsage,
		"analysis":  analysisUsage,
		"gallery":   galleryUsage,
		"check":     checkUsage,
		"skills":    skillsUsage,
		"workflows": workflowsUsage,
	}
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
		{Command: "rhythm", Binary: "zv-rhythm"},
		{Command: "tui", Binary: "zv-tui"},
	}
}

func defaultLegacyCommandEntrypointNames() []string {
	return []string{
		"zv-parser",
		"zv-analysis-viewer",
		"zv-demo-players",
		"zv-recorder",
		"zv-editor",
		"zv-stream",
		"zv-composer",
		"zv-orchestrator",
		"zv-tactical-data",
		"zv-rhythm",
		"zv-tui",
	}
}
