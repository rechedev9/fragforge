package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rechedev9/fragforge/internal/faceit"
	"github.com/rechedev9/fragforge/internal/storage"
)

type faceitIndexer interface {
	Index(context.Context, faceit.IndexRequest) (faceit.Index, error)
}

type faceitIndexOptions struct {
	Profile string
	OutPath string
	From    time.Time
	To      time.Time
	Format  string
	DryRun  bool
}

type faceitIndexResult struct {
	OK       bool          `json:"ok"`
	DryRun   bool          `json:"dry_run"`
	Executed bool          `json:"executed"`
	Profile  string        `json:"profile"`
	From     time.Time     `json:"from"`
	To       time.Time     `json:"to"`
	Output   string        `json:"output"`
	Index    *faceit.Index `json:"index,omitempty"`
}

func runFaceit(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, faceitUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, faceitUsage)
		return exitSuccess
	}
	switch args[0] {
	case "index":
		if isSingleHelp(args[1:]) {
			fmt.Fprint(stdout, faceitIndexUsage)
			return exitSuccess
		}
		if issue := validateRequiredFlags(`"faceit index"`, args[1:], "--profile", "--out"); issue != "" {
			return writeFaceitError(args[1:], stdout, stderr, fmt.Errorf("%s", issue), faceitIndexUsage, exitInvalidArgs)
		}
		return runFaceitIndex(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown faceit command %q\n%s", args[0], faceitUsage)
		return exitInvalidArgs
	}
}

func runFaceitIndex(args []string, stdout, stderr io.Writer) int {
	now := time.Now().UTC()
	opts, err := parseFaceitIndexOptions(args, now)
	if err != nil {
		return writeFaceitError(args, stdout, stderr, err, faceitIndexUsage, exitInvalidArgs)
	}
	if opts.DryRun {
		return executeFaceitIndexDryRun(opts, stdout, stderr)
	}
	client, err := faceit.New(faceit.Options{APIKey: os.Getenv("FACEIT_API_KEY")})
	if err != nil {
		return writeFaceitError(args, stdout, stderr, err, "", exitUnexpected)
	}
	return executeFaceitIndex(context.Background(), opts, client, stdout, stderr)
}

func parseFaceitIndexOptions(args []string, now time.Time) (faceitIndexOptions, error) {
	defaultFrom := time.Date(now.UTC().Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
	defaultTo := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 23, 59, 59, int(time.Second-time.Nanosecond), time.UTC)
	var profile, outPath, fromValue, toValue, format string
	var dryRun bool
	fs := flag.NewFlagSet("faceit index", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&profile, "profile", "", "FACEIT profile URL or nickname")
	fs.StringVar(&outPath, "out", "", "output demo index JSON")
	fs.StringVar(&fromValue, "from", defaultFrom.Format(time.DateOnly), "first UTC date, YYYY-MM-DD")
	fs.StringVar(&toValue, "to", defaultTo.Format(time.DateOnly), "last UTC date, YYYY-MM-DD")
	fs.StringVar(&format, "format", "text", "text or json")
	fs.BoolVar(&dryRun, "dry-run", false, "validate the index request without network or writes")
	if err := fs.Parse(args); err != nil {
		return faceitIndexOptions{}, err
	}
	if fs.NArg() != 0 {
		return faceitIndexOptions{}, fmt.Errorf("unexpected positional arg %q", fs.Arg(0))
	}
	if _, err := faceit.ParseProfile(profile); err != nil {
		return faceitIndexOptions{}, err
	}
	if strings.TrimSpace(outPath) == "" {
		return faceitIndexOptions{}, fmt.Errorf("--out is required")
	}
	if !strings.EqualFold(filepath.Ext(outPath), ".json") {
		return faceitIndexOptions{}, fmt.Errorf("--out must use a .json extension")
	}
	if format != "text" && format != "json" {
		return faceitIndexOptions{}, fmt.Errorf("unsupported format %q", format)
	}
	from, err := time.Parse(time.DateOnly, fromValue)
	if err != nil {
		return faceitIndexOptions{}, fmt.Errorf("invalid --from date %q; use YYYY-MM-DD", fromValue)
	}
	toDate, err := time.Parse(time.DateOnly, toValue)
	if err != nil {
		return faceitIndexOptions{}, fmt.Errorf("invalid --to date %q; use YYYY-MM-DD", toValue)
	}
	to := toDate.Add(24*time.Hour - time.Millisecond)
	if to.Before(from) {
		return faceitIndexOptions{}, fmt.Errorf("--to must be on or after --from")
	}
	return faceitIndexOptions{
		Profile: strings.TrimSpace(profile),
		OutPath: outPath,
		From:    from.UTC(),
		To:      to.UTC(),
		Format:  format,
		DryRun:  dryRun,
	}, nil
}

func executeFaceitIndexDryRun(opts faceitIndexOptions, stdout, stderr io.Writer) int {
	absOut, err := filepath.Abs(opts.OutPath)
	if err != nil {
		return writeFaceitErrorWithFormat(opts.Format, stdout, stderr, fmt.Errorf("resolve FACEIT index output: %w", err), "", exitUnexpected)
	}
	result := faceitIndexResult{
		OK:       true,
		DryRun:   true,
		Executed: false,
		Profile:  opts.Profile,
		From:     opts.From,
		To:       opts.To,
		Output:   absOut,
	}
	if opts.Format == "json" {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: write FACEIT JSON preflight: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	fmt.Fprintf(stdout, "valid FACEIT index request: %s (%s to %s UTC)\n", opts.Profile, opts.From.Format(time.DateOnly), opts.To.Format(time.DateOnly))
	fmt.Fprintf(stdout, "index: %s (not written)\n", absOut)
	return exitSuccess
}

func executeFaceitIndex(ctx context.Context, opts faceitIndexOptions, indexer faceitIndexer, stdout, stderr io.Writer) int {
	index, err := indexer.Index(ctx, faceit.IndexRequest{Profile: opts.Profile, From: opts.From, To: opts.To})
	if err != nil {
		return writeFaceitErrorWithFormat(opts.Format, stdout, stderr, err, "", exitUnexpected)
	}
	absOut, err := filepath.Abs(opts.OutPath)
	if err != nil {
		return writeFaceitErrorWithFormat(opts.Format, stdout, stderr, fmt.Errorf("resolve FACEIT index output: %w", err), "", exitUnexpected)
	}
	if err := writeFaceitIndex(absOut, index); err != nil {
		return writeFaceitErrorWithFormat(opts.Format, stdout, stderr, fmt.Errorf("write FACEIT demo index: %w", err), "", exitUnexpected)
	}

	result := faceitIndexResult{OK: true, Executed: true, Profile: opts.Profile, From: opts.From, To: opts.To, Output: absOut, Index: &index}
	if opts.Format == "json" {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: write FACEIT JSON result: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	writeFaceitIndexText(stdout, result)
	return exitSuccess
}

func writeFaceitIndex(path string, index faceit.Index) error {
	body, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	store, err := storage.NewLocal(filepath.Dir(path))
	if err != nil {
		return err
	}
	return store.Put(filepath.Base(path), bytes.NewReader(body))
}

func writeFaceitIndexText(w io.Writer, result faceitIndexResult) {
	index := result.Index
	if index == nil {
		return
	}
	fmt.Fprintf(w, "FACEIT player: %s (%s)\n", index.Player.Nickname, index.Player.ProfileURL)
	fmt.Fprintf(w, "range: %s to %s UTC\n", index.Range.From.Format(time.DateOnly), index.Range.To.Format(time.DateOnly))
	fmt.Fprintf(w, "matches: %d, demos available: %d, stats enriched: %d\n", index.Summary.Matches, index.Summary.DemoAvailable, index.Summary.StatsEnriched)
	fmt.Fprintln(w, "top match candidates (open the room URL and download the demo manually):")
	byID := make(map[string]faceit.Match, len(index.Matches))
	for _, match := range index.Matches {
		byID[match.ID] = match
	}
	limit := min(10, len(index.HighlightMatchIDs))
	for rank, matchID := range index.HighlightMatchIDs[:limit] {
		match := byID[matchID]
		if match.Stats == nil {
			continue
		}
		fmt.Fprintf(w, "%d. %s %s — %dK/%dD ADR %.1f — %s\n", rank+1, match.FinishedAt.Format(time.DateOnly), match.Stats.Map, match.Stats.Kills, match.Stats.Deaths, match.Stats.ADR, match.RoomURL)
	}
	fmt.Fprintf(w, "index: %s\n", result.Output)
}

func writeFaceitError(args []string, stdout, stderr io.Writer, err error, commandUsage string, code int) int {
	format := "text"
	if shortJSONRequested(args) {
		format = "json"
	}
	return writeFaceitErrorWithFormat(format, stdout, stderr, err, commandUsage, code)
}

func writeFaceitErrorWithFormat(format string, stdout, stderr io.Writer, err error, commandUsage string, code int) int {
	if format == "json" {
		if writeErr := writeJSON(stdout, map[string]any{"ok": false, "executed": false, "error": err.Error()}); writeErr != nil {
			fmt.Fprintf(stderr, "error: write FACEIT JSON error: %v\n", writeErr)
			return exitUnexpected
		}
		return code
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	if commandUsage != "" {
		fmt.Fprint(stderr, commandUsage)
	}
	return code
}
