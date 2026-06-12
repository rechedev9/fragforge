package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rechedev9/fragforge/internal/editor"
)

// shortOptions are the parsed `zv short` command-line options.
type shortOptions struct {
	DemoPath      string
	Prompt        string
	Preset        string
	OutDir        string
	MusicPath     string
	TargetSteamID string
	HLAEPath      string
	CS2Path       string
	FromRecording string
	DryRun        bool
}

// shortStage is one delegated step of the resolved demo-to-Short plan.
type shortStage struct {
	label  string
	binary string
	args   []string
}

// shortPlan is the fully resolved one-command plan: always a 1080x1920@60fps
// vertical Short by construction of the preset registry.
type shortPlan struct {
	preset     editor.RenderPreset
	intent     shortIntent
	player     string
	selection  string
	outDir     string
	shortsDir  string
	publishDir string
	stageDirs  []string // directories the stage binaries write into; created before stage 1
	stages     []shortStage
}

func runShort(args []string, stdout, stderr io.Writer, stdin io.Reader, runner commandRunner) int {
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprint(stdout, shortUsage)
		return exitSuccess
	}
	opts, err := parseShortArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, shortUsage)
		return exitInvalidArgs
	}
	plan, err := resolveShortPlan(opts)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitInvalidArgs
	}
	fmt.Fprintf(stdout, "player: %s\n", plan.player)
	fmt.Fprintf(stdout, "selection: %s\n", plan.selection)
	fmt.Fprintf(stdout, "preset: %s (%dx%d @ %dfps)\n", plan.preset.Name, plan.preset.Width, plan.preset.Height, plan.preset.FPS)
	if opts.DryRun {
		printShortPlan(stdout, plan)
		return exitSuccess
	}
	for _, dir := range plan.stageDirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(stderr, "error: create stage output directory: %v\n", err)
			return exitUnexpected
		}
	}
	if err := preflightShortEditorPreset(plan.preset.Name, runner); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitUnexpected
	}
	for i, stage := range plan.stages {
		fmt.Fprintf(stdout, "[%d/%d] %s...\n", i+1, len(plan.stages), stage.label)
		code := runDelegate(stage.binary, stage.args, stdout, stderr, stdin, runner)
		if code != exitSuccess {
			fmt.Fprintf(stderr, "error: stage %d/%d (%s) failed; fix the issue and re-run, or pass --from-recording <recording-result.json> to skip parse and record once footage exists\n",
				i+1, len(plan.stages), stage.label)
			return code
		}
	}
	fmt.Fprintf(stdout, "shorts: %s\n", plan.shortsDir)
	fmt.Fprintf(stdout, "publish pack: %s\n", plan.publishDir)
	return exitSuccess
}

func parseShortArgs(args []string) (shortOptions, error) {
	var opts shortOptions
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		opts.DemoPath = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("short", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.Prompt, "prompt", "", "editing instruction, e.g. \"haz un short con todas las kills de martinez\"")
	fs.StringVar(&opts.Preset, "preset", "", "render preset; defaults to "+editor.DefaultPreset().Name)
	fs.StringVar(&opts.OutDir, "out", "", "run output directory; defaults under data/runs")
	fs.StringVar(&opts.MusicPath, "music", "", "music file for beat-synced shorts")
	fs.StringVar(&opts.TargetSteamID, "target-steamid", "", "target player SteamID64 when the prompt names a player by name only")
	fs.StringVar(&opts.HLAEPath, "hlae", os.Getenv("ZV_HLAE_PATH"), "path to HLAE.exe; defaults to ZV_HLAE_PATH")
	fs.StringVar(&opts.CS2Path, "cs2", os.Getenv("ZV_CS2_PATH"), "path to cs2.exe; defaults to ZV_CS2_PATH")
	fs.StringVar(&opts.FromRecording, "from-recording", "", "existing recording-result.json; skips the parse and record stages")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "print the resolved plan without launching HLAE/CS2 or FFmpeg")
	if err := fs.Parse(args); err != nil {
		return shortOptions{}, err
	}
	if rest := fs.Args(); len(rest) != 0 {
		return shortOptions{}, fmt.Errorf("unexpected extra args %q; quote paths with spaces", strings.Join(rest, " "))
	}
	if strings.TrimSpace(opts.Prompt) == "" {
		return shortOptions{}, fmt.Errorf("missing required flag --prompt")
	}
	if opts.DemoPath == "" && opts.FromRecording == "" {
		return shortOptions{}, fmt.Errorf("missing demo path: pass <demo.dem> or --from-recording <recording-result.json>")
	}
	return opts, nil
}

func resolveShortPlan(opts shortOptions) (shortPlan, error) {
	intent := interpretShortPrompt(opts.Prompt)
	preset, err := resolveShortPreset(opts, intent)
	if err != nil {
		return shortPlan{}, err
	}
	needsRhythm := intent.BeatSync || opts.MusicPath != ""
	if needsRhythm && opts.MusicPath == "" {
		return shortPlan{}, fmt.Errorf("beat-synced shorts require --music <audio file>")
	}

	plan := shortPlan{preset: preset, intent: intent, outDir: shortOutDir(opts)}
	plan.stageDirs = []string{plan.outDir}
	plan.shortsDir = filepath.Join(plan.outDir, "shorts")
	plan.publishDir = filepath.Join(plan.shortsDir, "publish")
	plan.player = "from existing recording"
	plan.selection = "all kills (one compiled short)"
	if intent.BestMoments {
		plan.selection = fmt.Sprintf("best moments (top %d segments)", shortBestMomentsLimit)
	}

	killPlanPath := ""
	recordingResult := opts.FromRecording
	if opts.FromRecording == "" {
		steamID, err := resolveShortSteamID(opts, intent)
		if err != nil {
			return shortPlan{}, err
		}
		plan.player = steamID
		if intent.TargetName != "" {
			plan.player = steamID + " (" + intent.TargetName + ")"
		}
		hlae, cs2, err := resolveShortCaptureTools(opts)
		if err != nil {
			return shortPlan{}, err
		}
		killPlanPath = filepath.Join(plan.outDir, "killplan.json")
		recordingDir := filepath.Join(plan.outDir, "recording")
		plan.stageDirs = append(plan.stageDirs, recordingDir)
		recordingResult = filepath.Join(recordingDir, "recording-result.json")
		plan.stages = append(plan.stages, shortStage{
			label:  "parsing demo",
			binary: "zv-parser",
			args:   []string{"parse", "--demo", opts.DemoPath, "--steamid", steamID, "--out", killPlanPath},
		})
		recorderArgs := []string{"--killplan", killPlanPath, "--demo", opts.DemoPath, "--out", recordingDir, "--hlae", hlae, "--cs2", cs2}
		if preset.HUDMode != "" {
			recorderArgs = append(recorderArgs, "--hud", preset.HUDMode)
		}
		plan.stages = append(plan.stages,
			shortStage{
				label:  "recording segments with HLAE/CS2",
				binary: "zv-recorder",
				args:   recorderArgs,
			},
		)
	}

	rhythmPath := ""
	if needsRhythm {
		rhythmPath = filepath.Join(plan.outDir, "rhythm.json")
		rhythmArgs := []string{"analyze", "--input", opts.MusicPath, "--out", rhythmPath}
		if killPlanPath != "" {
			rhythmArgs = append(rhythmArgs, "--killplan", killPlanPath)
		}
		plan.stages = append(plan.stages, shortStage{
			label:  "analyzing music beats",
			binary: "zv-rhythm",
			args:   rhythmArgs,
		})
	}

	plan.stages = append(plan.stages, shortStage{
		label:  "rendering short and publish pack",
		binary: "zv-editor",
		args:   shortRenderArgs(opts, plan, killPlanPath, recordingResult, rhythmPath),
	})
	return plan, nil
}

// resolveShortPreset picks the render preset: explicit --preset wins, then a
// preset named in the prompt, then the product default (viral-60-clean).
func resolveShortPreset(opts shortOptions, intent shortIntent) (editor.RenderPreset, error) {
	name := opts.Preset
	if name == "" {
		name = intent.Preset
	}
	if name == "" {
		return editor.DefaultPreset(), nil
	}
	preset, ok := editor.PresetByName(name)
	if !ok {
		return editor.RenderPreset{}, fmt.Errorf("unknown preset %q (valid presets: %s)", name, strings.Join(editor.PresetNames(), ", "))
	}
	return preset, nil
}

// preflightShortEditorPreset verifies that the resolved zv-editor binary knows
// the chosen preset before any stage runs. Without it, a stale
// bin/zv-editor.exe rejects newly added presets only at the final render
// stage, after the expensive HLAE/CS2 recording.
func preflightShortEditorPreset(preset string, runner commandRunner) error {
	exe := resolveExecutable("zv-editor")
	var out strings.Builder
	if err := runner.Run(context.Background(), exe, []string{"--list-presets"}, nil, &out, io.Discard); err != nil {
		return fmt.Errorf("preflight %s --list-presets failed: %v; the stage binaries look stale or missing, rebuild them (scripts/build.ps1) and re-run", exe, err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.TrimSpace(line) == preset {
			return nil
		}
	}
	return fmt.Errorf("%s does not know preset %q; the stage binaries are stale, rebuild them (scripts/build.ps1) and re-run", exe, preset)
}

func resolveShortSteamID(opts shortOptions, intent shortIntent) (string, error) {
	steamID := opts.TargetSteamID
	if steamID == "" {
		steamID = intent.TargetSteamID
	}
	if steamID == "" {
		if intent.TargetName != "" {
			return "", fmt.Errorf("could not resolve player %q to a SteamID64: pass --target-steamid <SteamID64> (list players with: zv demo players --demo %s --contains %s)",
				intent.TargetName, opts.DemoPath, intent.TargetName)
		}
		return "", fmt.Errorf("the prompt does not identify a target player: pass --target-steamid <SteamID64> or include a SteamID64 in the prompt")
	}
	if _, err := strconv.ParseUint(steamID, 10, 64); err != nil {
		return "", fmt.Errorf("target steamid %q must be a 64-bit unsigned integer", steamID)
	}
	return steamID, nil
}

func resolveShortCaptureTools(opts shortOptions) (hlae, cs2 string, err error) {
	hlae, cs2 = opts.HLAEPath, opts.CS2Path
	if opts.DryRun {
		if hlae == "" {
			hlae = "<HLAE.exe>"
		}
		if cs2 == "" {
			cs2 = "<cs2.exe>"
		}
		return hlae, cs2, nil
	}
	var missing []string
	if hlae == "" {
		missing = append(missing, "--hlae (or ZV_HLAE_PATH)")
	}
	if cs2 == "" {
		missing = append(missing, "--cs2 (or ZV_CS2_PATH)")
	}
	if len(missing) > 0 {
		return "", "", fmt.Errorf("missing %s for the recording stage unless --dry-run or --from-recording is set", strings.Join(missing, " and "))
	}
	return hlae, cs2, nil
}

func shortRenderArgs(opts shortOptions, plan shortPlan, killPlanPath, recordingResult, rhythmPath string) []string {
	args := []string{"--recording-result", recordingResult, "--out", plan.shortsDir, "--preset", plan.preset.Name}
	if killPlanPath != "" {
		args = append(args, "--killplan", killPlanPath)
	}
	if opts.MusicPath != "" {
		args = append(args, "--music", opts.MusicPath)
	}
	if rhythmPath != "" {
		args = append(args, "--rhythm", rhythmPath)
	}
	// One upload-ready Short: compile all selected segments into a single
	// vertical video. Best-moments intent keeps only the top segments.
	args = append(args, "--compile-segments")
	if plan.intent.BestMoments {
		args = append(args, "--limit", strconv.Itoa(shortBestMomentsLimit))
	}
	return args
}

// shortBestMomentsLimit caps the compiled segments for best-moments prompts.
const shortBestMomentsLimit = 5

func shortOutDir(opts shortOptions) string {
	if opts.OutDir != "" {
		return opts.OutDir
	}
	if opts.DemoPath != "" {
		stem := strings.TrimSuffix(filepath.Base(opts.DemoPath), filepath.Ext(opts.DemoPath))
		return filepath.Join("data", "runs", stem+"-short")
	}
	return filepath.Join(filepath.Dir(opts.FromRecording), "short")
}

func printShortPlan(stdout io.Writer, plan shortPlan) {
	fmt.Fprintln(stdout, "plan:")
	for i, stage := range plan.stages {
		fmt.Fprintf(stdout, "  [%d/%d] %s: %s %s\n", i+1, len(plan.stages), stage.label, stage.binary, strings.Join(stage.args, " "))
	}
	fmt.Fprintf(stdout, "shorts: %s\n", plan.shortsDir)
	fmt.Fprintf(stdout, "publish pack: %s\n", plan.publishDir)
	fmt.Fprintln(stdout, "dry-run: no stages executed")
}
