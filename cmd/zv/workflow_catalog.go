package main

import "sync"

func findWorkflow(name string) (workflowInfo, bool) {
	w, ok := workflowCatalogByName()[name]
	return w, ok
}

// The workflow catalog is static data, identical for the life of the process.
// Build it (and its name index) once: validation rebuilt and re-scanned it on
// every documented command line across every doc and skill.
var (
	workflowCatalogOnce   = sync.OnceValue(buildWorkflowCatalog)
	workflowCatalogByName = sync.OnceValue(func() map[string]workflowInfo {
		catalog := workflowCatalogOnce()
		byName := make(map[string]workflowInfo, len(catalog))
		for _, w := range catalog {
			byName[w.Name] = w
		}
		return byName
	})
)

// workflowCatalog returns the shared catalog. Callers must treat it as read-only.
func workflowCatalog() []workflowInfo {
	return workflowCatalogOnce()
}

// buildWorkflowCatalog lists the delegated single-binary workflows. The
// primary product flow is the composite built-in `zv short` (parse -> record
// -> render -> publish pack in one command), documented in
// docs/workflows/zv-short.md; it chains these stage workflows instead of
// appearing as a catalog entry of its own.
func buildWorkflowCatalog() []workflowInfo {
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
			Name:        "music-analyze",
			Description: "Analyze music beats and build optional kill-to-beat sync suggestions.",
			Command:     "zv music analyze --input <audio-or-video> --out <rhythm.json>",
			RunArgs:     []string{"music", "analyze"},
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
