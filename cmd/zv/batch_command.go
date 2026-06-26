package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/rechedev9/fragforge/internal/batch"
	"github.com/rechedev9/fragforge/internal/obs"
	"github.com/rechedev9/fragforge/internal/parser"
)

// runBatch parses a folder of demos in-process and records every failure to the
// obs journal, so a folder of demos can be exercised without driving the CLI
// once per demo. Exit code is non-zero when any demo failed, so a fix loop can
// detect a non-empty error log.
func runBatch(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, batchUsage)
		return exitSuccess
	}
	// Accept the directory either before the flags (the common form,
	// `zv batch <dir> --flags`) or after them; Go's flag package stops at the
	// first non-flag argument, so a leading positional is pulled off here.
	var dir string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		dir = args[0]
		args = args[1:]
	}
	fs := flag.NewFlagSet("batch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		recursive   bool
		steamID     string
		outDir      string
		obsDir      string
		jobs        int
		segmentMode string
		format      string
		report      string
	)
	fs.BoolVar(&recursive, "recursive", false, "descend into subdirectories")
	fs.StringVar(&steamID, "steamid", "", "target SteamID64 for every demo; default auto-picks the top fragger")
	fs.StringVar(&outDir, "out", "", "optional directory to write each kill plan into")
	fs.StringVar(&obsDir, "obs-dir", obs.DefaultDir(), "observability directory for the error journal and metrics")
	fs.IntVar(&jobs, "jobs", 0, "max concurrent demos; 0 picks a CPU-based default")
	fs.StringVar(&segmentMode, "segment-mode", string(parser.SegmentModeKills), "segment mode: kills, smokes, or utility")
	fs.StringVar(&format, "format", "text", "summary format: text or json")
	fs.StringVar(&report, "report", "", "optional path to write the JSON summary report")
	if err := fs.Parse(args); err != nil {
		return exitInvalidArgs
	}
	rest := fs.Args()
	if dir == "" && len(rest) == 1 {
		dir, rest = rest[0], nil
	}
	if dir == "" || len(rest) != 0 {
		fmt.Fprintln(stderr, "error: batch takes exactly one directory argument")
		fmt.Fprint(stderr, batchUsage)
		return exitInvalidArgs
	}
	mode := parser.SegmentMode(segmentMode)
	if mode != parser.SegmentModeKills && mode != parser.SegmentModeSmokes && mode != parser.SegmentModeUtility {
		fmt.Fprintf(stderr, "error: --segment-mode must be %q, %q, or %q\n", parser.SegmentModeKills, parser.SegmentModeSmokes, parser.SegmentModeUtility)
		return exitInvalidArgs
	}

	rec, err := obs.New(obsDir)
	if err != nil {
		fmt.Fprintf(stderr, "error: open obs recorder: %v\n", err)
		return exitUnexpected
	}

	opts := batch.Options{
		Dir:         dir,
		Recursive:   recursive,
		SteamID:     steamID,
		OutDir:      outDir,
		Jobs:        jobs,
		SegmentMode: mode,
	}
	sum, err := batch.Run(context.Background(), opts, rec, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitUnexpected
	}

	if report != "" {
		b, _ := json.MarshalIndent(sum, "", "  ")
		if werr := os.WriteFile(report, append(b, '\n'), 0o600); werr != nil {
			fmt.Fprintf(stderr, "error: writing report: %v\n", werr)
			return exitUnexpected
		}
	}

	if format == "json" {
		if err := writeJSON(stdout, sum); err != nil {
			fmt.Fprintf(stderr, "error: writing json: %v\n", err)
			return exitUnexpected
		}
	} else {
		fmt.Fprintf(stdout, "\nbatch: %d demos, %d ok, %d failed\n", sum.Total, sum.OK, sum.Failed)
		fmt.Fprintf(stdout, "error journal: %s\n", rec.JournalPath())
		fmt.Fprintf(stdout, "metrics:       %s\n", rec.MetricsPromPath())
	}
	if sum.Failed > 0 {
		return exitUnexpected
	}
	return exitSuccess
}

// runMetrics prints the current obs counters in Prometheus text format.
func runMetrics(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, metricsUsage)
		return exitSuccess
	}
	fs := flag.NewFlagSet("metrics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var obsDir string
	var reset bool
	fs.StringVar(&obsDir, "obs-dir", obs.DefaultDir(), "observability directory")
	fs.BoolVar(&reset, "reset", false, "clear all counters")
	if err := fs.Parse(args); err != nil {
		return exitInvalidArgs
	}
	rec, err := obs.New(obsDir)
	if err != nil {
		fmt.Fprintf(stderr, "error: open obs recorder: %v\n", err)
		return exitUnexpected
	}
	if reset {
		for _, p := range []string{rec.MetricsPromPath(), obsDir + "/metrics.json"} {
			if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(stderr, "error: reset metrics: %v\n", err)
				return exitUnexpected
			}
		}
		fmt.Fprintln(stdout, "metrics reset")
		return exitSuccess
	}
	obs.WritePrometheus(stdout, rec.Snapshot())
	return exitSuccess
}

// runErrors summarizes the obs error journal.
func runErrors(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, errorsUsage)
		return exitSuccess
	}
	fs := flag.NewFlagSet("errors", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var obsDir string
	var asJSON, clear bool
	var tail int
	fs.StringVar(&obsDir, "obs-dir", obs.DefaultDir(), "observability directory")
	fs.BoolVar(&asJSON, "json", false, "print raw journal lines as a JSON array")
	fs.BoolVar(&clear, "clear", false, "truncate the error journal")
	fs.IntVar(&tail, "tail", 0, "show only the last N events (0 = all)")
	if err := fs.Parse(args); err != nil {
		return exitInvalidArgs
	}
	rec, err := obs.New(obsDir)
	if err != nil {
		fmt.Fprintf(stderr, "error: open obs recorder: %v\n", err)
		return exitUnexpected
	}
	if clear {
		if err := os.WriteFile(rec.JournalPath(), nil, 0o644); err != nil {
			fmt.Fprintf(stderr, "error: clear journal: %v\n", err)
			return exitUnexpected
		}
		fmt.Fprintln(stdout, "error journal cleared")
		return exitSuccess
	}
	events, err := readEvents(rec.JournalPath())
	if err != nil {
		fmt.Fprintf(stderr, "error: read journal: %v\n", err)
		return exitUnexpected
	}
	if tail > 0 && tail < len(events) {
		events = events[len(events)-tail:]
	}
	if asJSON {
		if err := writeJSON(stdout, events); err != nil {
			fmt.Fprintf(stderr, "error: writing json: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	for _, ev := range events {
		fmt.Fprintf(stdout, "%s  %-8s %-18s %s  %s\n",
			ev.Time.Format("2006-01-02T15:04:05Z"), ev.Stage, ev.Class, baseName(ev.Demo), ev.Message)
	}
	printErrorSummary(stdout, events)
	return exitSuccess
}

func printErrorSummary(w io.Writer, events []obs.Event) {
	counts := map[string]int{}
	for _, ev := range events {
		counts[ev.Stage+"/"+ev.Class]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(w, "\n%d error(s) in journal\n", len(events))
	for _, k := range keys {
		fmt.Fprintf(w, "  %-30s %d\n", k, counts[k])
	}
}

func readEvents(path string) ([]obs.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var events []obs.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev obs.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("invalid journal line: %w", err)
		}
		events = append(events, ev)
	}
	return events, sc.Err()
}

func baseName(p string) string {
	if p == "" {
		return "-"
	}
	i := strings.LastIndexAny(p, `/\`)
	if i < 0 {
		return p
	}
	return p[i+1:]
}
