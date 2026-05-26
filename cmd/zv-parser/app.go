// Package main implements the zv-parser CLI: given a CS2 demo file and a
// target SteamID, it emits a structured kill plan JSON.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/lineups"
	"github.com/reche/zackvideo/internal/parser"
	"github.com/reche/zackvideo/internal/rules"
	"github.com/reche/zackvideo/internal/utilityaudit"
)

const (
	exitSuccess        = 0
	exitUnexpected     = 1
	exitInvalidArgs    = 2
	exitFileError      = 3
	exitDemoCorrupt    = 4
	exitTargetNotFound = 5
)

const usage = `zv-parser - parse a CS2 demo into a kill plan JSON

Usage:
  zv-parser parse --demo <path> --steamid <id> [--segment-mode kills|smokes|utility] [--rules <path>] [--out <path>] [--verbose]
  zv-parser utility-audit --plan <plan-utility.json> [--lineup-catalog <dir>] [--format csv|json] [--out <path>]

Flags:
  --demo      Path to the .dem file
  --steamid   Target player SteamID64
  --segment-mode  Segment mode: kills, smokes, or utility (default "kills")
  --rules     Optional JSON file with segmentation rules
  --out       Output path, or "-" for stdout (default "-")
  --verbose   Log progress to stderr
  --plan      Path to a utility kill plan JSON
  --lineup-catalog  Optional directory with manual lineup catalog JSON files
  --format    Audit output format: csv or json (default "csv")
`

// Run is the CLI entrypoint with explicit I/O streams so it can be tested.
// It returns the process exit code per the spec (see exit* constants).
func Run(argv []string, stdout, stderr io.Writer) int {
	if len(argv) < 2 {
		fmt.Fprint(stderr, usage)
		return exitInvalidArgs
	}

	switch argv[1] {
	case "parse":
		return runParse(argv[2:], stdout, stderr)
	case "utility-audit":
		return runUtilityAudit(argv[2:], stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return exitSuccess
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n%s", argv[1], usage)
		return exitInvalidArgs
	}
}

type parseArgs struct {
	demo        string
	steamid     string
	segmentMode string
	rules       string
	out         string
	verbose     bool
}

type utilityAuditArgs struct {
	plan          string
	lineupCatalog string
	format        string
	out           string
}

func runParse(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("parse", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var pa parseArgs
	fs.StringVar(&pa.demo, "demo", "", "path to .dem file")
	fs.StringVar(&pa.steamid, "steamid", "", "target SteamID64")
	fs.StringVar(&pa.segmentMode, "segment-mode", string(parser.SegmentModeKills), "segment mode: kills, smokes, or utility")
	fs.StringVar(&pa.rules, "rules", "", "optional JSON rules file")
	fs.StringVar(&pa.out, "out", "-", "output path or \"-\" for stdout")
	fs.BoolVar(&pa.verbose, "verbose", false, "log progress to stderr")

	if err := fs.Parse(args); err != nil {
		return exitInvalidArgs
	}

	if pa.demo == "" {
		fmt.Fprintln(stderr, "error: --demo is required")
		return exitInvalidArgs
	}
	if pa.steamid == "" {
		fmt.Fprintln(stderr, "error: --steamid is required")
		return exitInvalidArgs
	}
	if _, err := strconv.ParseUint(pa.steamid, 10, 64); err != nil {
		fmt.Fprintf(stderr, "error: --steamid must be a 64-bit unsigned integer: %v\n", err)
		return exitInvalidArgs
	}
	mode := parser.SegmentMode(pa.segmentMode)
	if mode != parser.SegmentModeKills && mode != parser.SegmentModeSmokes && mode != parser.SegmentModeUtility {
		fmt.Fprintf(stderr, "error: --segment-mode must be %q, %q, or %q\n", parser.SegmentModeKills, parser.SegmentModeSmokes, parser.SegmentModeUtility)
		return exitInvalidArgs
	}

	r, code := loadRules(pa.rules, stderr)
	if code != exitSuccess {
		return code
	}

	demoFile, err := os.Open(pa.demo)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "error: demo file not found: %s\n", pa.demo)
			return exitFileError
		}
		fmt.Fprintf(stderr, "error: opening demo: %v\n", err)
		return exitFileError
	}
	defer demoFile.Close()

	sha, err := sha256Hex(demoFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: hashing demo: %v\n", err)
		return exitFileError
	}
	if _, err := demoFile.Seek(0, io.SeekStart); err != nil {
		fmt.Fprintf(stderr, "error: rewinding demo: %v\n", err)
		return exitFileError
	}

	if pa.verbose {
		fmt.Fprintf(stderr, "zv-parser: parsing %s (sha256=%s)\n", pa.demo, sha)
	}

	meta := parser.PlanMeta{
		DemoPath: pa.demo,
		SHA256:   sha,
	}

	p := demoinfocs.NewParser(demoFile)
	defer p.Close()

	plan, err := parser.RunWithOptions(p, pa.steamid, r, meta, parser.RunOptions{SegmentMode: mode})
	if err != nil {
		switch {
		case errors.Is(err, parser.ErrTargetNotFound):
			fmt.Fprintf(stderr, "error: %s: %s\n", err.Error(), pa.steamid)
			return exitTargetNotFound
		default:
			fmt.Fprintf(stderr, "error: parsing demo: %v\n", err)
			return exitDemoCorrupt
		}
	}

	return writePlan(plan, pa.out, stdout, stderr)
}

func runUtilityAudit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("utility-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var aa utilityAuditArgs
	fs.StringVar(&aa.plan, "plan", "", "path to utility kill plan JSON")
	fs.StringVar(&aa.lineupCatalog, "lineup-catalog", "", "optional directory with manual lineup catalog JSON files")
	fs.StringVar(&aa.format, "format", "csv", "output format: csv or json")
	fs.StringVar(&aa.out, "out", "-", "output path or \"-\" for stdout")
	if err := fs.Parse(args); err != nil {
		return exitInvalidArgs
	}
	if aa.plan == "" {
		fmt.Fprintln(stderr, "error: --plan is required")
		return exitInvalidArgs
	}
	format := strings.ToLower(strings.TrimSpace(aa.format))
	if format == "" {
		format = "csv"
	}
	if err := utilityaudit.ValidateFormat(format); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitInvalidArgs
	}
	plan, code := loadPlan(aa.plan, stderr)
	if code != exitSuccess {
		return code
	}
	catalog, err := lineups.LoadDir(aa.lineupCatalog)
	if err != nil {
		fmt.Fprintf(stderr, "error: loading lineup catalog: %v\n", err)
		return exitFileError
	}
	rows := utilityaudit.Build(plan, catalog)
	switch format {
	case "json":
		b, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: marshaling audit: %v\n", err)
			return exitUnexpected
		}
		return writeBytes(append(b, '\n'), aa.out, stdout, stderr)
	default:
		var b strings.Builder
		if err := utilityaudit.WriteCSV(&b, rows); err != nil {
			fmt.Fprintf(stderr, "error: writing audit csv: %v\n", err)
			return exitUnexpected
		}
		return writeBytes([]byte(b.String()), aa.out, stdout, stderr)
	}
}

func loadPlan(path string, stderr io.Writer) (killplan.Plan, int) {
	// #nosec G304 -- plan path is an explicit local CLI input.
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "error: plan file not found: %s\n", path)
			return killplan.Plan{}, exitFileError
		}
		fmt.Fprintf(stderr, "error: reading plan: %v\n", err)
		return killplan.Plan{}, exitFileError
	}
	var plan killplan.Plan
	if err := json.Unmarshal(b, &plan); err != nil {
		fmt.Fprintf(stderr, "error: invalid plan JSON: %v\n", err)
		return killplan.Plan{}, exitInvalidArgs
	}
	return plan, exitSuccess
}

func loadRules(path string, stderr io.Writer) (rules.Rules, int) {
	if path == "" {
		return rules.Default(), exitSuccess
	}
	// #nosec G304 -- demo/rules path is an explicit local CLI input.
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "error: rules file not found: %s\n", path)
			return rules.Rules{}, exitFileError
		}
		fmt.Fprintf(stderr, "error: opening rules: %v\n", err)
		return rules.Rules{}, exitFileError
	}
	defer f.Close()

	r, err := rules.Load(f)
	if err != nil {
		fmt.Fprintf(stderr, "error: invalid rules: %v\n", err)
		return rules.Rules{}, exitInvalidArgs
	}
	return r, exitSuccess
}

func writePlan(plan killplan.Plan, out string, stdout, stderr io.Writer) int {
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "error: marshaling plan: %v\n", err)
		return exitUnexpected
	}
	if out == "-" {
		return writeBytes(append(b, '\n'), out, stdout, stderr)
	}
	return writeBytes(append(b, '\n'), out, stdout, stderr)
}

func writeBytes(b []byte, out string, stdout, stderr io.Writer) int {
	if out == "-" {
		if _, err := stdout.Write(b); err != nil {
			fmt.Fprintf(stderr, "error: writing stdout: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	if err := os.WriteFile(out, b, 0o600); err != nil {
		fmt.Fprintf(stderr, "error: writing output: %v\n", err)
		return exitFileError
	}
	return exitSuccess
}

func sha256Hex(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
