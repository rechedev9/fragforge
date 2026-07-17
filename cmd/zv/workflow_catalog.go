package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/parser"
	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

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

// buildWorkflowCatalog lists the stable workflows exposed to automation. It
// includes the composite `zv short` product flow as well as its granular stages.
func buildWorkflowCatalog() []workflowInfo {
	return withWorkflowRunCommands([]workflowInfo{
		{
			Name:        "short",
			Description: "Create one upload-ready Short through parse, capture, render, and publish stages.",
			Command:     "zv short <demo.dem> --prompt <prompt>",
			RunArgs:     []string{"short"},
		},
		{
			Name:        "capabilities",
			Description: "Inspect local capture and render tool readiness without starting work.",
			Command:     "zv capabilities",
			RunArgs:     []string{"capabilities"},
		},
		{
			Name:        "demo-parse",
			Description: "Parse a CS2 demo into a kill or utility plan.",
			Command:     "zv demo parse --demo <demo.dem> --steamid <SteamID64> --out <plan.json>",
			RunArgs:     []string{"demo", "parse"},
		},
		{
			Name:        "demo-players",
			Description: "List demo participants and SteamID64 values as text or structured JSON.",
			Command:     "zv demo players --demo <demo.dem>",
			RunArgs:     []string{"demo", "players"},
		},
		{
			Name:        "demo-moments",
			Description: "Score and rank planned demo segments before capture.",
			Command:     "zv demo moments --killplan <plan.json>",
			RunArgs:     []string{"demo", "moments"},
		},
		{
			Name:        "demo-select",
			Description: "Create a recorder-ready plan containing an ordered segment selection.",
			Command:     "zv demo select --killplan <plan.json> --segments <ids> --out <selected-plan.json>",
			RunArgs:     []string{"demo", "select"},
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
			Command:     "zv record --killplan <plan.json> --demo <demo.dem> --out <recording-dir>",
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
			Description: "Render vertical or landscape videos; the upload-ready pack defaults to <shorts-dir>/shortslistosparasubir.",
			Command:     "zv shorts render --recording-result <recording-result.json> --out <shorts-dir>",
			RunArgs:     []string{"shorts", "render"},
		},
		{
			Name:        "stream-variants",
			Description: "List local stream layout variants and default crops.",
			Command:     "zv stream variants",
			RunArgs:     []string{"stream", "variants"},
		},
		{
			Name:        "stream-plan",
			Description: "Probe a stream video and create a validated local edit plan.",
			Command:     "zv stream plan --input <stream.mp4> --out <edit-plan.json>",
			RunArgs:     []string{"stream", "plan"},
		},
		{
			Name:        "stream-killfeed",
			Description: "Import reviewed factual killfeed notices into a detected stream plan.",
			Command:     "zv stream killfeed --plan <edit-plan.json> --events <killfeed-events.json> --out <reviewed-plan.json>",
			RunArgs:     []string{"stream", "killfeed"},
		},
		{
			Name:        "stream-transcribe",
			Description: "Generate local multi-pass Whisper candidates that remain explicitly unreviewed.",
			Command:     "zv stream transcribe --input <stream.mp4> --plan <edit-plan.json> --model <ggml-model.bin> --vad-model <ggml-vad.bin> --out <transcript-review.json>",
			RunArgs:     []string{"stream", "transcribe"},
		},
		{
			Name:        "stream-captions",
			Description: "Import reviewed Spanish word timings without requiring a cloud transcription key.",
			Command:     "zv stream captions --plan <edit-plan.json> --words <caption-words.json> --out <captioned-plan.json>",
			RunArgs:     []string{"stream", "captions"},
		},
		{
			Name:        "stream-render",
			Description: "Render stream clips, killfeed, and Spanish captions into an upload-ready local pack.",
			Command:     "zv stream render --input <stream.mp4> --plan <edit-plan.json> --out <run-dir>",
			RunArgs:     []string{"stream", "render"},
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
			Command:     "zv gallery open --path <run>/shortslistosparasubir/index.html",
			RunArgs:     []string{"gallery", "open"},
		},
		{
			Name:        "flows-run",
			Description: "Chain a whole demo or stream journey in --dry-run mode into a run directory.",
			Command:     "zv flows run <demo|stream> --run-dir <run-dir> --dry-run",
			RunArgs:     []string{"flows", "run"},
		},
		{
			Name:        "serve",
			Description: "Start the orchestrator API and workers.",
			Command:     "zv serve",
			RunArgs:     []string{"serve"},
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
			Description: "Run the full FragForge CLI, workflow, docs, and skills contract.",
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
		if workflows[i].Name != "" && workflows[i].ValidateCommand == "" {
			workflows[i].ValidateCommand = workflowValidateCommand(workflows[i].Name)
		}
		workflows[i].Arguments = workflowArgumentMetadata(workflows[i])
		workflows[i].Safety = workflowSafetyMetadata(workflows[i], workflows[i].Arguments)
	}
	return workflows
}

func workflowArgumentMetadata(workflow workflowInfo) workflowArguments {
	required := workflowRequiredFlags(workflow)
	commandName := fmt.Sprintf("%q", strings.Join(workflow.RunArgs, " "))
	valueFlags := commandValueFlags(commandName, required)
	if workflow.Name == "capabilities" || workflow.Name == "stream-variants" || workflow.Name == "skills-check" || workflow.Name == "workflows-check" || workflow.Name == "project-check" {
		valueFlags = append(valueFlags, "--format")
	}

	positionals := []workflowPositionalArgument{}
	conditional := []workflowConditionalRequirement{}
	switch workflow.Name {
	case "short":
		positionals = append(positionals, workflowPositionalArgument{
			Name:        "demo",
			Placeholder: "<demo.dem>",
			Required:    false,
		})
		conditional = append(conditional, workflowConditionalRequirement{
			Description:         "a demo path is required unless an existing recording result is supplied",
			UnlessAnyFlags:      []string{"--from-recording"},
			RequiredFlags:       []string{},
			RequiredPositionals: []string{"demo"},
		})
	case "flows-run":
		positionals = append(positionals, workflowPositionalArgument{
			Name:        "flow",
			Placeholder: "<demo|stream>",
			Required:    true,
		})
		conditional = append(conditional,
			workflowConditionalRequirement{
				Description:         "the demo flow requires a demo path (--demo) unless an existing kill plan (--killplan) is supplied",
				UnlessAnyFlags:      []string{"--killplan"},
				RequiredFlags:       []string{"--demo"},
				RequiredPositionals: []string{},
			},
			workflowConditionalRequirement{
				Description:         "the stream flow requires a source video (--input)",
				UnlessAnyFlags:      []string{},
				RequiredFlags:       []string{"--input"},
				RequiredPositionals: []string{},
			},
		)
	}

	return workflowArguments{
		Positionals:             positionals,
		RequiredFlags:           copyStrings(required),
		OptionalValueFlags:      flagsExcept(valueFlags, required),
		BooleanFlags:            copyStrings(commandBoolFlags(commandName)),
		ValueConstraints:        workflowValueConstraints(workflow),
		ConditionalRequirements: conditional,
	}
}

func workflowValueConstraints(workflow workflowInfo) []workflowValueConstraint {
	constraint := func(flag, defaultValue, discoveryCommand string, allowed ...string) workflowValueConstraint {
		return workflowValueConstraint{
			Flag:             flag,
			AllowedValues:    copyStrings(allowed),
			Default:          defaultValue,
			DiscoveryCommand: discoveryCommand,
		}
	}

	switch workflow.Name {
	case "short":
		return []workflowValueConstraint{
			constraint("--preset", editor.DefaultPreset().Name, "zv presets --format json", supportedPresetNames()...),
			constraint("--output-format", editor.OutputFormatShort9x16, "", editor.OutputFormatShort9x16, editor.OutputFormatLandscape16x9),
			constraint("--kill-effect", editor.KillEffectPunchIn, "", editor.KillEffectClean, editor.KillEffectPunchIn, editor.KillEffectVelocity, editor.KillEffectFreezeFlash),
			constraint("--transition", editor.TransitionFlash, "", editor.TransitionCut, editor.TransitionFlash, editor.TransitionWhip, editor.TransitionDip),
			constraint("--format", "text", "", "text", "json"),
		}
	case "demo-parse":
		return []workflowValueConstraint{
			constraint("--segment-mode", string(parser.SegmentModeKills), "",
				string(parser.SegmentModeKills), string(parser.SegmentModeSmokes), string(parser.SegmentModeUtility)),
		}
	case "utility-audit":
		return []workflowValueConstraint{
			constraint("--format", "csv", "", "csv", "json"),
		}
	case "record":
		return []workflowValueConstraint{
			constraint("--hud", string(recording.HUDModeGameplay), "",
				string(recording.HUDModeGameplay), string(recording.HUDModeClean), string(recording.HUDModeDeathnotices)),
			constraint("--format", "text", "", "text", "json"),
		}
	case "shorts-render":
		defaultPreset := editor.DefaultPreset()
		return []workflowValueConstraint{
			constraint("--preset", defaultPreset.Name, "zv presets --format json", supportedPresetNames()...),
			constraint("--effects-preset", defaultPreset.EffectsPreset, "", editor.EffectsPresetViralUltraClean),
			constraint("--output-format", editor.OutputFormatShort9x16, "", editor.OutputFormatShort9x16, editor.OutputFormatLandscape16x9),
			constraint("--kill-effect", editor.KillEffectPunchIn, "", editor.KillEffectClean, editor.KillEffectPunchIn, editor.KillEffectVelocity, editor.KillEffectFreezeFlash),
			constraint("--transition", editor.TransitionFlash, "", editor.TransitionCut, editor.TransitionFlash, editor.TransitionWhip, editor.TransitionDip),
			constraint("--video-preset", defaultPreset.VideoPreset, "",
				"ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow", "slower", "veryslow"),
			constraint("--format", "text", "", "text", "json"),
		}
	case "compose-final":
		return []workflowValueConstraint{
			constraint("--format", "text", "", "text", "json"),
		}
	case "stream-plan":
		return []workflowValueConstraint{
			constraint("--variant", streamclips.DefaultVariant().Name, "zv stream variants --format json", streamclips.VariantNames()...),
			constraint("--format", "text", "", "text", "json"),
		}
	case "stream-render", "stream-killfeed", "stream-transcribe", "stream-captions", "stream-variants", "demo-players", "demo-moments", "demo-select", "flows-run":
		return []workflowValueConstraint{
			constraint("--format", "text", "", "text", "json"),
		}
	case "capabilities", "skills-check", "workflows-check", "project-check":
		return []workflowValueConstraint{
			constraint("--format", "text", "", "text", "json"),
		}
	default:
		return []workflowValueConstraint{}
	}
}

func workflowRequiredFlags(workflow workflowInfo) []string {
	if workflow.Name == "record" {
		return []string{"--killplan", "--demo", "--out"}
	}
	// requiredFlagsFromCommand already drops the boolean --dry-run that the
	// flows-run Command documents, so only --run-dir remains required there.
	return requiredFlagsFromCommand(workflow.Command)
}

func workflowSafetyMetadata(workflow workflowInfo, arguments workflowArguments) workflowSafety {
	readOnly := false
	switch workflow.Name {
	case "capabilities", "stream-variants", "analysis-viewer", "gallery-open", "skills-check", "workflows-check", "project-check":
		readOnly = true
	}

	longRunning := false
	switch workflow.Name {
	case "short", "record", "compose-final", "music-analyze", "shorts-render", "stream-plan", "stream-transcribe", "stream-render", "analysis-viewer", "serve", "flows-run":
		// flows-run really parses demos and probes media across a whole journey.
		longRunning = true
	}

	return workflowSafety{
		ReadOnly:       readOnly,
		SupportsDryRun: containsString(arguments.BooleanFlags, "--dry-run"),
		LongRunning:    longRunning,
	}
}

func flagsExcept(flags, excluded []string) []string {
	out := make([]string, 0, len(flags))
	seen := make(map[string]struct{}, len(flags))
	for _, flag := range flags {
		if containsString(excluded, flag) {
			continue
		}
		if _, ok := seen[flag]; ok {
			continue
		}
		seen[flag] = struct{}{}
		out = append(out, flag)
	}
	return out
}

func copyStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	return append([]string(nil), values...)
}

func workflowRunCommand(name string) string {
	return "zv workflows run " + name
}

func workflowValidateCommand(name string) string {
	return "zv workflows validate " + name
}
