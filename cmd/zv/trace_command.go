package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/parser"
	"github.com/rechedev9/fragforge/internal/trace"
)

// traceOptions are the parsed `zv trace` command-line options.
type traceOptions struct {
	Demo          string
	SteamID       string
	FromPlan      string
	Rules         string
	SegmentMode   string
	Preset        string
	TailTrim      float64
	Out           string
	Pretty        bool
	Deterministic bool
}

func runTrace(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprint(stdout, traceUsage)
		return exitSuccess
	}
	opts, err := parseTraceArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		fmt.Fprint(stderr, traceUsage)
		return exitInvalidArgs
	}

	doc, err := trace.Run(context.Background(), trace.Options{
		DemoPath:        opts.Demo,
		SteamID:         opts.SteamID,
		RulesPath:       opts.Rules,
		SegmentMode:     parser.SegmentMode(opts.SegmentMode),
		FromPlan:        opts.FromPlan,
		Preset:          opts.Preset,
		TailTrimSeconds: opts.TailTrim,
		Deterministic:   opts.Deterministic,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitUnexpected
	}
	return writeTrace(doc, opts, stdout, stderr)
}

func parseTraceArgs(args []string) (traceOptions, error) {
	var opts traceOptions
	fs := flag.NewFlagSet("trace", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.Demo, "demo", "", "path to .dem file (with --steamid)")
	fs.StringVar(&opts.SteamID, "steamid", "", "target player SteamID64")
	fs.StringVar(&opts.FromPlan, "from-plan", "", "existing kill plan JSON; skips parsing")
	fs.StringVar(&opts.Rules, "rules", "", "optional JSON rules file (demo source only)")
	fs.StringVar(&opts.SegmentMode, "segment-mode", string(parser.SegmentModeKills), "segment mode: kills, smokes, or utility")
	fs.StringVar(&opts.Preset, "preset", "", "render preset; defaults to "+editor.DefaultPreset().Name)
	fs.Float64Var(&opts.TailTrim, "tail-trim", editor.DefaultTailTrimSeconds, "end each kill part this many seconds after its final kill; 0 disables")
	fs.StringVar(&opts.Out, "out", "-", "output path, or \"-\" for stdout")
	fs.BoolVar(&opts.Pretty, "pretty", false, "indent the JSON output")
	fs.BoolVar(&opts.Deterministic, "deterministic", false, "fixed job UUID, zeroed timestamps, placeholder ffmpeg path")
	if err := fs.Parse(args); err != nil {
		return traceOptions{}, err
	}
	if rest := fs.Args(); len(rest) != 0 {
		return traceOptions{}, fmt.Errorf("unexpected extra args %q", rest)
	}

	if opts.FromPlan == "" && opts.Demo == "" {
		return traceOptions{}, fmt.Errorf("a source is required: pass --from-plan <killplan.json> or --demo <path> --steamid <id>")
	}
	if opts.FromPlan != "" && opts.Demo != "" {
		return traceOptions{}, fmt.Errorf("--from-plan and --demo are mutually exclusive")
	}
	if opts.FromPlan != "" && opts.SteamID != "" {
		return traceOptions{}, fmt.Errorf("--from-plan and --steamid are mutually exclusive; the plan already names its target")
	}
	if opts.FromPlan != "" && opts.Rules != "" {
		return traceOptions{}, fmt.Errorf("--from-plan and --rules are mutually exclusive; rules only apply when parsing a demo")
	}
	if opts.Demo != "" {
		if opts.SteamID == "" {
			return traceOptions{}, fmt.Errorf("--demo requires --steamid")
		}
		if _, err := strconv.ParseUint(opts.SteamID, 10, 64); err != nil {
			return traceOptions{}, fmt.Errorf("--steamid must be a 64-bit unsigned integer")
		}
	}
	mode := parser.SegmentMode(opts.SegmentMode)
	if mode != parser.SegmentModeKills && mode != parser.SegmentModeSmokes && mode != parser.SegmentModeUtility {
		return traceOptions{}, fmt.Errorf("--segment-mode must be %q, %q, or %q", parser.SegmentModeKills, parser.SegmentModeSmokes, parser.SegmentModeUtility)
	}
	if opts.Preset != "" {
		if _, ok := editor.PresetByName(opts.Preset); !ok {
			return traceOptions{}, fmt.Errorf("unknown preset %q (valid presets: %s)", opts.Preset, strings.Join(editor.PresetNames(), ", "))
		}
	}
	if opts.TailTrim < 0 || opts.TailTrim > 10 {
		return traceOptions{}, fmt.Errorf("--tail-trim must be between 0 and 10")
	}
	return opts, nil
}

func writeTrace(doc trace.TraceDocument, opts traceOptions, stdout, stderr io.Writer) int {
	var (
		b   []byte
		err error
	)
	if opts.Pretty {
		b, err = json.MarshalIndent(doc, "", "  ")
	} else {
		b, err = json.Marshal(doc)
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: marshaling trace: %v\n", err)
		return exitUnexpected
	}
	b = append(b, '\n')
	if opts.Out == "-" {
		if _, err := stdout.Write(b); err != nil {
			fmt.Fprintf(stderr, "error: writing stdout: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	if err := os.WriteFile(opts.Out, b, 0o600); err != nil {
		fmt.Fprintf(stderr, "error: writing output: %v\n", err)
		return exitUnexpected
	}
	return exitSuccess
}
