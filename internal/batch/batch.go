// Package batch runs a folder of CS2 demos through the parse stage in-process
// and records every failure to an obs.Recorder. It exists so an operator (or an
// agent) can point FragForge at a directory of demos, get a pass/fail summary,
// and drive the error journal to empty without invoking the per-demo CLI by
// hand.
package batch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/obs"
	"github.com/rechedev9/fragforge/internal/parser"
	"github.com/rechedev9/fragforge/internal/rules"
)

// Options configures a batch run.
type Options struct {
	Dir         string             // directory to scan for .dem files
	Recursive   bool               // descend into subdirectories
	SteamID     string             // target for every demo; empty means auto-pick the top fragger
	OutDir      string             // optional directory to write each kill plan into
	Jobs        int                // max concurrent demos; <= 0 picks a CPU-based default
	SegmentMode parser.SegmentMode // defaults to kills
}

// Result is the outcome of processing a single demo.
type Result struct {
	Demo     string `json:"demo"`
	Target   string `json:"target_steamid,omitempty"`
	OK       bool   `json:"ok"`
	Class    string `json:"class,omitempty"`
	Err      string `json:"error,omitempty"`
	Segments int    `json:"segments,omitempty"`
}

// Summary aggregates the results of a batch run.
type Summary struct {
	Total   int      `json:"total"`
	OK      int      `json:"ok"`
	Failed  int      `json:"failed"`
	Results []Result `json:"results"`
}

// Run processes every demo under opts.Dir, recording successes and failures to
// rec, and writes one progress line per demo to progress. It returns a Summary;
// individual demo failures do not abort the run (they are recorded), but a bad
// directory or a cancelled context returns an error.
func Run(ctx context.Context, opts Options, rec *obs.Recorder, progress io.Writer) (Summary, error) {
	demos, err := findDemos(opts.Dir, opts.Recursive)
	if err != nil {
		return Summary{}, err
	}
	if len(demos) == 0 {
		return Summary{}, fmt.Errorf("no .dem files found under %s", opts.Dir)
	}
	if opts.OutDir != "" {
		if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
			return Summary{}, fmt.Errorf("create out dir: %w", err)
		}
	}

	jobs := opts.Jobs
	if jobs <= 0 {
		jobs = defaultJobs()
	}
	if jobs > len(demos) {
		jobs = len(demos)
	}

	results := make([]Result, len(demos))
	sem := make(chan struct{}, jobs)
	var wg sync.WaitGroup
	var mu sync.Mutex // serializes progress writes

	for i, demo := range demos {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, demo string) {
			defer wg.Done()
			defer func() { <-sem }()
			res := processDemo(ctx, opts, rec, demo)
			results[i] = res
			mu.Lock()
			writeProgress(progress, res)
			mu.Unlock()
		}(i, demo)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return Summary{}, ctx.Err()
	}

	sum := Summary{Total: len(results), Results: results}
	for _, r := range results {
		if r.OK {
			sum.OK++
		} else {
			sum.Failed++
		}
	}
	return sum, nil
}

// processDemo resolves a target, parses one demo, and records the outcome.
// rec.RecordError results are intentionally ignored: a journal write failure
// must not abort the batch or mask the underlying demo failure.
func processDemo(ctx context.Context, opts Options, rec *obs.Recorder, demo string) Result {
	res := Result{Demo: demo}

	target := opts.SteamID
	if target == "" {
		top, err := safeTopFragger(ctx, demo)
		if err != nil {
			class, msg := classify(err)
			res.Class, res.Err = class, msg
			_ = rec.RecordError(obs.Event{Stage: obs.StageParse, Class: class, Message: msg, Demo: demo})
			return res
		}
		target = top
	}
	res.Target = target

	plan, err := safeParseDemo(ctx, demo, target, opts.SegmentMode)
	if err != nil {
		class, msg := classify(err)
		res.Class, res.Err = class, msg
		_ = rec.RecordError(obs.Event{Stage: obs.StageParse, Class: class, Message: msg, Demo: demo, Target: target})
		return res
	}

	res.OK = true
	res.Segments = len(plan.Segments)
	if opts.OutDir != "" {
		if err := writePlan(opts.OutDir, demo, plan); err != nil {
			res.OK = false
			res.Class, res.Err = "write_plan", err.Error()
			_ = rec.RecordError(obs.Event{Stage: obs.StageParse, Class: "write_plan", Message: err.Error(), Demo: demo, Target: target})
			return res
		}
	}
	_ = rec.RecordSuccess(obs.StageParse)
	return res
}

// errParsePanic marks a demo whose parsing panicked. A malformed demo must not
// crash the batch; it is recorded like any other failure.
var errParsePanic = errors.New("parser panicked")

// recoverParse runs fn and converts a panic into an errParsePanic error. It
// must execute on the same goroutine that calls the parser, since recover only
// catches panics from its own goroutine.
func recoverParse(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %v", errParsePanic, r)
		}
	}()
	return fn()
}

// safeParseDemo is parseDemo with panic recovery so one bad demo cannot abort
// the whole run.
func safeParseDemo(ctx context.Context, demo, target string, mode parser.SegmentMode) (plan killplan.Plan, err error) {
	err = recoverParse(func() error {
		var perr error
		plan, perr = parseDemo(ctx, demo, target, mode)
		return perr
	})
	return plan, err
}

// safeTopFragger is topFragger with panic recovery for any panic on the calling
// goroutine; the parse goroutine inside topFragger recovers its own.
func safeTopFragger(ctx context.Context, demo string) (id string, err error) {
	err = recoverParse(func() error {
		var terr error
		id, terr = topFragger(ctx, demo)
		return terr
	})
	return id, err
}

// parseDemo opens demo and builds a kill plan for target.
func parseDemo(ctx context.Context, demo, target string, mode parser.SegmentMode) (killplan.Plan, error) {
	// #nosec G304 -- demo path is an explicit local input from the batch dir.
	f, err := os.Open(demo)
	if err != nil {
		return killplan.Plan{}, err
	}
	defer f.Close()

	sha, err := sha256Hex(f)
	if err != nil {
		return killplan.Plan{}, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return killplan.Plan{}, err
	}

	meta := parser.PlanMeta{DemoPath: demo, SHA256: sha}
	p := demoinfocs.NewParser(f)
	defer p.Close()
	return parser.RunWithContext(ctx, p, target, rules.Default(), meta, parser.RunOptions{SegmentMode: mode})
}

// topFragger returns the SteamID64 (as a string) of the player with the most
// kills in demo. It mirrors `zv demo players` so batch can pick a sensible
// target without the operator naming one per demo.
func topFragger(ctx context.Context, demo string) (string, error) {
	// #nosec G304 -- demo path is an explicit local input from the batch dir.
	f, err := os.Open(demo)
	if err != nil {
		return "", err
	}
	defer f.Close()

	p := demoinfocs.NewParser(f)
	defer p.Close()

	kills := map[uint64]int{}
	names := map[uint64]string{}
	p.RegisterEventHandler(func(e events.Kill) {
		if e.Killer != nil {
			kills[e.Killer.SteamID64]++
			recordName(names, e.Killer)
		}
		if e.Victim != nil {
			recordName(names, e.Victim)
		}
	})

	// ParseToEnd runs in its own goroutine, so a panic on a malformed demo must
	// be recovered HERE; a recover in the calling goroutine (safeTopFragger)
	// cannot catch a panic raised on this one.
	done := make(chan error, 1)
	go func() { done <- recoverParse(p.ParseToEnd) }()
	select {
	case <-ctx.Done():
		p.Cancel()
		<-done
		return "", ctx.Err()
	case err := <-done:
		if err != nil && !errors.Is(err, demoinfocs.ErrUnexpectedEndOfDemo) {
			return "", fmt.Errorf("parse demo: %w", err)
		}
	}

	var bestID uint64
	bestKills := -1
	for id, k := range kills {
		if id == 0 {
			continue
		}
		if k > bestKills || (k == bestKills && names[id] < names[bestID]) {
			bestID, bestKills = id, k
		}
	}
	if bestID == 0 {
		return "", errNoTarget
	}
	return strconv.FormatUint(bestID, 10), nil
}

func recordName(names map[uint64]string, pl *common.Player) {
	if pl.Name != "" {
		names[pl.SteamID64] = pl.Name
	}
}

var errNoTarget = errors.New("no killer found in demo to auto-select a target")

// classify maps a parse error to a stable obs class label and message.
func classify(err error) (class, message string) {
	switch {
	case errors.Is(err, errParsePanic):
		return "panic", err.Error()
	case errors.Is(err, errNoTarget):
		return "no_target", err.Error()
	case errors.Is(err, parser.ErrTargetNotFound):
		return "target_not_found", err.Error()
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "canceled", err.Error()
	case errors.Is(err, os.ErrNotExist):
		return "file_not_found", err.Error()
	case errors.Is(err, os.ErrPermission):
		return "file_permission", err.Error()
	default:
		return "corrupt", err.Error()
	}
}

func findDemos(dir string, recursive bool) ([]string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("stat dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	var demos []string
	if recursive {
		err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && isDemo(path) {
				demos = append(demos, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && isDemo(e.Name()) {
				demos = append(demos, filepath.Join(dir, e.Name()))
			}
		}
	}
	sort.Strings(demos)
	return demos, nil
}

func isDemo(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".dem")
}

func writePlan(outDir, demo string, plan killplan.Plan) error {
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}
	stem := strings.TrimSuffix(filepath.Base(demo), filepath.Ext(demo))
	out := filepath.Join(outDir, stem+".killplan.json")
	if err := os.WriteFile(out, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}
	return nil
}

func writeProgress(w io.Writer, res Result) {
	if w == nil {
		return
	}
	if res.OK {
		fmt.Fprintf(w, "ok    %s  (target %s, %d segments)\n", filepath.Base(res.Demo), res.Target, res.Segments)
		return
	}
	fmt.Fprintf(w, "FAIL  %s  [%s] %s\n", filepath.Base(res.Demo), res.Class, res.Err)
}

func defaultJobs() int {
	n := runtime.NumCPU() - 1
	if n < 1 {
		return 1
	}
	if n > 4 {
		return 4 // demo parsing is memory-heavy; cap to keep a folder of demos sane
	}
	return n
}

func sha256Hex(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
