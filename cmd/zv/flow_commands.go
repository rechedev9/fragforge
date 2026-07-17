package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

type productionFlow struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Source      string       `json:"source"`
	Outputs     []flowOutput `json:"outputs"`
	Phases      []flowPhase  `json:"phases"`
}

type flowOutput struct {
	Name        string `json:"name"`
	Format      string `json:"format"`
	Resolution  string `json:"resolution"`
	Destination string `json:"destination"`
}

type flowPhase struct {
	ID        string `json:"id"`
	Goal      string `json:"goal"`
	Command   string `json:"command,omitempty"`
	Decision  string `json:"decision,omitempty"`
	Produces  string `json:"produces,omitempty"`
	ReadOnly  bool   `json:"read_only"`
	Expensive bool   `json:"expensive"`
}

type flowListRow struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ShowCommand string `json:"show_command"`
}

func runFlows(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, flowsUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, flowsUsage)
		return exitSuccess
	}
	switch args[0] {
	case "list":
		return runFlowsList(args[1:], stdout, stderr)
	case "show":
		return runFlowsShow(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown flows command %q\n%s", args[0], flowsUsage)
		return exitInvalidArgs
	}
}

func runFlowsList(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, flowsListUsage)
		return exitSuccess
	}
	format, rest, err := parseFormatArgs(args)
	if err != nil || len(rest) != 0 {
		if err == nil {
			err = fmt.Errorf("unexpected extra args for flows list")
		}
		return writeFlowError(args, stdout, stderr, err, flowsListUsage)
	}
	flows := productionFlows()
	rows := make([]flowListRow, 0, len(flows))
	for _, flow := range flows {
		rows = append(rows, flowListRow{
			Name: flow.Name, Description: flow.Description, ShowCommand: "zv flows show " + flow.Name + " --format json",
		})
	}
	if format == "json" {
		if err := writeJSON(stdout, map[string]any{"ok": true, "flows": rows}); err != nil {
			fmt.Fprintf(stderr, "error: write flow list: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	for _, row := range rows {
		fmt.Fprintf(stdout, "%s\t%s\n", row.Name, row.Description)
	}
	return exitSuccess
}

func runFlowsShow(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, flowsShowUsage)
		return exitSuccess
	}
	format, rest, err := parseFormatArgs(args)
	if err != nil || len(rest) != 1 {
		if err == nil {
			err = fmt.Errorf("flows show requires exactly one flow name")
		}
		return writeFlowError(args, stdout, stderr, err, flowsShowUsage)
	}
	flow, ok := findProductionFlow(rest[0])
	if !ok {
		return writeFlowError(args, stdout, stderr, fmt.Errorf("unknown production flow %q (valid: demo, stream)", rest[0]), flowsShowUsage)
	}
	if format == "json" {
		if err := writeJSON(stdout, map[string]any{"ok": true, "flow": flow}); err != nil {
			fmt.Fprintf(stderr, "error: write production flow: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	fmt.Fprintf(stdout, "%s: %s\n", flow.Name, flow.Description)
	for i, phase := range flow.Phases {
		fmt.Fprintf(stdout, "%d. %s — %s\n", i+1, phase.ID, phase.Goal)
		if phase.Command != "" {
			fmt.Fprintf(stdout, "   %s\n", phase.Command)
		}
		if phase.Decision != "" {
			fmt.Fprintf(stdout, "   decide: %s\n", phase.Decision)
		}
	}
	return exitSuccess
}

func productionFlows() []productionFlow {
	demoOutputs := []flowOutput{
		{Name: "TikTok / Shorts", Format: editor.OutputFormatShort9x16, Resolution: "1080x1920@60", Destination: "<run>/shortslistosparasubir"},
		{Name: "YouTube long-form", Format: editor.OutputFormatLandscape16x9, Resolution: "1920x1080@60", Destination: "<run>/shortslistosparasubir"},
	}
	streamOutputs := []flowOutput{
		{Name: "TikTok / Shorts", Format: editor.OutputFormatShort9x16, Resolution: "1080x1920@60", Destination: "<run>/render/shortslistosparasubir"},
		{Name: "YouTube long-form", Format: editor.OutputFormatLandscape16x9, Resolution: "1920x1080@60", Destination: "<run>/render/shortslistosparasubir"},
	}
	return []productionFlow{
		{
			Name: "demo", Description: "CS2 demo to selected HLAE capture and upload-ready edit", Source: ".dem",
			Outputs: append([]flowOutput(nil), demoOutputs...),
			Phases: []flowPhase{
				{ID: "doctor", Goal: "verify local parser, HLAE, CS2, FFmpeg, and editor readiness", Command: "zv capabilities --format json", ReadOnly: true},
				{ID: "players", Goal: "inspect the roster and choose the POV SteamID64", Command: "zv demo players --demo <match.dem> --format json", Decision: "target player", ReadOnly: true},
				{ID: "parse", Goal: "derive a deterministic kill plan from the demo", Command: "zv demo parse --demo <match.dem> --steamid <SteamID64> --out <run>/killplan.json", Produces: "killplan.json"},
				{ID: "moments", Goal: "rank factual candidate plays before GPU capture", Command: "zv demo moments --killplan <run>/killplan.json --out <run>/moments.json --format json", Decision: "segments and narrative order", Produces: "moments.json", ReadOnly: false},
				{ID: "select-preflight", Goal: "validate the chosen plays and narrative order without writing", Command: "zv demo select --killplan <run>/killplan.json --segments <seg-ids> --out <run>/selected-plan.json --dry-run --format json", Decision: "approve selected segments", ReadOnly: true},
				{ID: "select", Goal: "persist only the chosen plays in their requested order", Command: "zv demo select --killplan <run>/killplan.json --segments <seg-ids> --out <run>/selected-plan.json --format json", Produces: "selected-plan.json"},
				{ID: "capture-preflight", Goal: "generate and inspect the HLAE capture contract without launching CS2", Command: "zv record --killplan <run>/selected-plan.json --demo <match.dem> --out <run>/recording --dry-run --format json", Decision: "approve capture plan", Produces: "recording dry-run artifacts", ReadOnly: false},
				{ID: "capture", Goal: "record the selected POV ranges with HLAE and CS2", Command: "zv record --killplan <run>/selected-plan.json --demo <match.dem> --out <run>/recording --format json", Produces: "recording-result.json", Expensive: true},
				{ID: "edit-preflight", Goal: "validate the final edit and delivery format without rendering", Command: "zv shorts render --recording-result <run>/recording/recording-result.json --killplan <run>/selected-plan.json --out <run>/render --publish-dir <run>/shortslistosparasubir --preset viral-60-clean --output-format <short-9x16|landscape-16x9> --compile-segments --dry-run", Decision: "format, kill effect, transition, intro/outro, music, thumbnail", Produces: "editor dry-run manifests and publish metadata", ReadOnly: false},
				{ID: "edit", Goal: "render one polished compilation in the selected delivery format", Command: "zv shorts render --recording-result <run>/recording/recording-result.json --killplan <run>/selected-plan.json --out <run>/render --publish-dir <run>/shortslistosparasubir --preset viral-60-clean --output-format <short-9x16|landscape-16x9> --compile-segments", Produces: "upload-ready pack", Expensive: true},
				{ID: "review", Goal: "inspect the gallery, covers, manifest, and QA before upload", Command: "zv gallery open --path <run>/shortslistosparasubir/index.html", ReadOnly: true},
			},
		},
		{
			Name: "stream", Description: "stream/VOD clips with factual killfeed and Spanish captions", Source: "video",
			Outputs: append([]flowOutput(nil), streamOutputs...),
			Phases: []flowPhase{
				{ID: "doctor", Goal: "verify FFmpeg, killfeed detection, and Spanish-caption readiness", Command: "zv capabilities --format json", ReadOnly: true},
				{ID: "layouts", Goal: "discover vertical and landscape output geometry", Command: "zv stream variants --format json", Decision: "layout and delivery format", ReadOnly: true},
				{ID: "plan-preflight", Goal: "probe media and preview the clip/crop/caption contract without writing", Command: "zv stream plan --input <stream.mp4> --out <run>/edit-plan.json --variant <" + strings.Join(streamclips.VariantNames(), "|") + "> --killfeed-crop <x,y,w,h> --detect-killfeed --dry-run --format json", Decision: "clip ranges, crops, factual notices, and subtitle source; add --captions for xAI or import reviewed words later", ReadOnly: true, Expensive: true},
				{ID: "plan", Goal: "persist the approved stream edit contract", Command: "zv stream plan --input <stream.mp4> --out <run>/edit-plan.json --variant <" + strings.Join(streamclips.VariantNames(), "|") + "> --killfeed-crop <x,y,w,h> --detect-killfeed --format json", Produces: "edit-plan.json", Expensive: true},
				{ID: "killfeed-preflight", Goal: "validate factual attacker/victim/weapon data against detected cues", Command: "zv stream killfeed --plan <run>/edit-plan.json --events <run>/killfeed-events.json --out <run>/reviewed-plan.json --dry-run --format json", Decision: "approve factual event document", ReadOnly: true},
				{ID: "enrich", Goal: "persist factual killfeed events without fabricating names", Command: "zv stream killfeed --plan <run>/edit-plan.json --events <run>/killfeed-events.json --out <run>/reviewed-plan.json --format json", Produces: "reviewed-plan.json"},
				{ID: "transcribe-preflight", Goal: "validate local Whisper models, VAD, media, and clip range without inference", Command: "zv stream transcribe --input <stream.mp4> --plan <run>/reviewed-plan.json --model <large-v3.bin> --model <large-v3-turbo.bin> --vad-model <silero-vad.bin> --out <run>/transcript-review.json --dry-run --format json", Decision: "approve local models and transcription scope", ReadOnly: true},
				{ID: "transcribe", Goal: "generate raw and dialogue-enhanced local transcript candidates", Command: "zv stream transcribe --input <stream.mp4> --plan <run>/reviewed-plan.json --model <large-v3.bin> --model <large-v3-turbo.bin> --vad-model <silero-vad.bin> --out <run>/transcript-review.json --format json", Produces: "requires-review transcript evidence", Expensive: true},
				{ID: "captions-preflight", Goal: "validate reviewed Spanish word timings", Command: "zv stream captions --plan <run>/reviewed-plan.json --words <run>/caption-words.json --out <run>/final-plan.json --dry-run --format json", Decision: "approve exact Spanish text and timings", ReadOnly: true},
				{ID: "captions", Goal: "persist reviewed Spanish captions without a cloud dependency", Command: "zv stream captions --plan <run>/reviewed-plan.json --words <run>/caption-words.json --out <run>/final-plan.json --format json", Produces: "final-plan.json"},
				{ID: "render-preflight", Goal: "validate tools, media, and the final plan without rendering", Command: "zv stream render --input <stream.mp4> --plan <run>/final-plan.json --out <run>/render --dry-run --format json", Decision: "approve final output", ReadOnly: true, Expensive: true},
				{ID: "render", Goal: "render video, killfeed, Spanish captions, audio, cover, manifest, and gallery", Command: "zv stream render --input <stream.mp4> --plan <run>/final-plan.json --out <run>/render --format json", Produces: "upload-ready pack", Expensive: true},
				{ID: "review", Goal: "inspect the final video, selected cover, and sidecar captions before upload", Command: "zv gallery open --path <run>/render/shortslistosparasubir/index.html", ReadOnly: true},
			},
		},
	}
}

func findProductionFlow(name string) (productionFlow, bool) {
	for _, flow := range productionFlows() {
		if flow.Name == name {
			return flow, true
		}
	}
	return productionFlow{}, false
}

func writeFlowError(args []string, stdout, stderr io.Writer, err error, commandUsage string) int {
	if shortJSONRequested(args) {
		if writeErr := writeJSON(stdout, map[string]any{"ok": false, "error": err.Error()}); writeErr != nil {
			fmt.Fprintf(stderr, "error: write flow json error: %v\n", writeErr)
			return exitUnexpected
		}
		return exitInvalidArgs
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	fmt.Fprint(stderr, commandUsage)
	return exitInvalidArgs
}
