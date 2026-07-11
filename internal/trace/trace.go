// Package trace inspects the pure decision layers of the pipeline
// (parse -> moments -> selection -> render plan) and captures every decision
// for a given input in a single JSON document, without recording, executing
// FFmpeg, or touching a browser. It is the headless QA seam: a kill plan (from
// a real .dem or a committed JSON fixture) flows through moments.Build and the
// pure FFmpeg argv builders, so goldens can regression-test the roots of the
// pipeline in seconds.
package trace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/moments"
	"github.com/rechedev9/fragforge/internal/parser"
	"github.com/rechedev9/fragforge/internal/rules"
)

// SchemaVersion is the trace document contract version.
const SchemaVersion = 1

// deterministicJobID is the fixed moments job UUID used in deterministic mode
// so two runs over the same input produce byte-identical output.
var deterministicJobID = uuid.Nil

// Options configures a single trace run. Exactly one source is required:
// DemoPath (a real .dem parsed with SteamID) or FromPlan (an existing kill
// plan JSON). RulesPath and SegmentMode only apply to the demo source and
// mirror `zv-parser parse`. TailTrimSeconds follows editor.Config semantics
// (zero disables trimming); `zv trace` defaults it to
// editor.DefaultTailTrimSeconds so the traced argv matches production.
type Options struct {
	DemoPath        string
	SteamID         string
	RulesPath       string
	SegmentMode     parser.SegmentMode
	FromPlan        string
	Preset          string
	TailTrimSeconds float64
	Deterministic   bool
}

// TraceDocument is the single artifact capturing every pipeline decision for
// one input: the parse (killplan), the scoring (moments), the segment
// selection, and the render plan whose FFmpeg argv would run. It reuses the
// existing JSON-tagged contracts verbatim rather than inventing parallel
// structs.
type TraceDocument struct {
	SchemaVersion int              `json:"schema_version"`
	GeneratedAt   time.Time        `json:"generated_at,omitzero"`
	Source        Source           `json:"source"`
	KillPlan      killplan.Plan    `json:"killplan"`
	Moments       moments.Document `json:"moments"`
	Selection     Selection        `json:"selection"`
	Render        Render           `json:"render"`
	Warnings      []string         `json:"warnings,omitempty"`
}

// Source records where the kill plan came from. SHA256 is only set for the
// demo source, where the trace hashes the .dem exactly like `zv-parser parse`.
type Source struct {
	Kind   string `json:"kind"` // "demo" or "plan"
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
}

// Selection lists the segments (and their moments) chosen for recording, in
// plan order. The trace selects every segment; the ordering and per-moment
// rationale already live in the moments document.
type Selection struct {
	SegmentIDs []string `json:"segment_ids"`
	MomentIDs  []string `json:"moment_ids"`
}

// Run drives one trace: it resolves the kill plan (from a .dem or an existing
// plan JSON), scores it into moments, records the segment selection, and
// builds the render plan whose FFmpeg argv would run. It never executes FFmpeg
// or any capture tool. Deterministic mode fixes the job UUID, omits the
// trace's own timestamp, zeroes the killplan/moments timestamps (their types
// are upstream contracts and always serialize the field), and uses a
// placeholder ffmpeg path so goldens stay stable.
func Run(ctx context.Context, opts Options) (TraceDocument, error) {
	preset, err := resolvePreset(opts.Preset)
	if err != nil {
		return TraceDocument{}, err
	}

	source, plan, err := resolveKillPlan(ctx, opts)
	if err != nil {
		return TraceDocument{}, err
	}
	if opts.Deterministic {
		// Timestamps are the only nondeterministic bits the parse can inject;
		// zero the plan's own timestamp so a demo-sourced trace is stable too.
		plan.GeneratedAt = time.Time{}
	}

	jobID := uuid.New()
	if opts.Deterministic {
		jobID = deterministicJobID
	}
	momentsDoc := moments.Build(jobID, plan)
	if opts.Deterministic {
		momentsDoc.GeneratedAt = time.Time{}
	}

	doc := TraceDocument{
		SchemaVersion: SchemaVersion,
		Source:        source,
		KillPlan:      plan,
		Moments:       momentsDoc,
		Selection:     selection(momentsDoc),
		Render:        buildRender(plan, preset, opts.TailTrimSeconds, opts.Deterministic),
	}
	if !opts.Deterministic {
		doc.GeneratedAt = time.Now().UTC()
	}
	if len(plan.Segments) == 0 {
		doc.Warnings = append(doc.Warnings, "kill plan has no segments; render plan is empty")
	}
	doc.Warnings = append(doc.Warnings, planWarnings(plan)...)
	return doc, nil
}

func resolvePreset(name string) (editor.RenderPreset, error) {
	if name == "" {
		return editor.DefaultPreset(), nil
	}
	preset, ok := editor.PresetByName(name)
	if !ok {
		return editor.RenderPreset{}, fmt.Errorf("unknown preset %q (valid presets: %s)", name, strings.Join(editor.PresetNames(), ", "))
	}
	return preset, nil
}

// planWarnings surfaces structural problems in the kill plan that would
// silently degrade the trace (zero durations, dropped kill cues) so a fixture
// or parser regression is visible in the document instead of masked by it.
func planWarnings(plan killplan.Plan) []string {
	var out []string
	if plan.Demo.Tickrate <= 0 && len(plan.Segments) > 0 {
		out = append(out, fmt.Sprintf("demo tickrate is %d; segment durations and kill cues cannot be derived", plan.Demo.Tickrate))
	}
	for _, segment := range plan.Segments {
		if segment.TickEnd <= segment.TickStart {
			out = append(out, fmt.Sprintf("segment %s has an invalid tick range [%d, %d]", segment.ID, segment.TickStart, segment.TickEnd))
		}
		for _, kill := range segment.Kills {
			if kill.Tick < segment.TickStart || kill.Tick > segment.TickEnd {
				out = append(out, fmt.Sprintf("segment %s kill at tick %d is outside the segment tick window [%d, %d]", segment.ID, kill.Tick, segment.TickStart, segment.TickEnd))
			}
		}
	}
	return out
}

func selection(doc moments.Document) Selection {
	sel := Selection{
		SegmentIDs: make([]string, 0, len(doc.Moments)),
		MomentIDs:  make([]string, 0, len(doc.Moments)),
	}
	for _, m := range doc.Moments {
		sel.SegmentIDs = append(sel.SegmentIDs, m.SegmentID)
		sel.MomentIDs = append(sel.MomentIDs, m.ID)
	}
	return sel
}

// resolveKillPlan returns the kill plan for the run: loaded from an existing
// JSON plan when --from-plan is set, otherwise parsed from the .dem. The demo
// path replicates `cmd/zv-parser/app.go` runParse: open, sha256, rewind,
// demoinfocs.NewParser, parser.RunWithOptions.
func resolveKillPlan(ctx context.Context, opts Options) (Source, killplan.Plan, error) {
	if opts.FromPlan != "" {
		plan, err := loadPlan(opts.FromPlan)
		if err != nil {
			return Source{}, killplan.Plan{}, err
		}
		return Source{Kind: "plan", Path: opts.FromPlan}, plan, nil
	}
	if opts.DemoPath == "" {
		return Source{}, killplan.Plan{}, errors.New("a source is required: pass --from-plan <killplan.json> or --demo <path> --steamid <id>")
	}
	return parseDemo(ctx, opts)
}

func loadPlan(path string) (killplan.Plan, error) {
	// #nosec G304 -- plan path is an explicit local CLI input.
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return killplan.Plan{}, fmt.Errorf("plan file not found: %s", path)
		}
		return killplan.Plan{}, fmt.Errorf("reading plan: %w", err)
	}
	var plan killplan.Plan
	if err := json.Unmarshal(b, &plan); err != nil {
		return killplan.Plan{}, fmt.Errorf("invalid plan JSON: %w", err)
	}
	// Validate the unmarshaled value: Plan.MarshalJSON force-writes the current
	// SchemaVersion on output, so only the loaded document reveals a mismatch.
	if plan.SchemaVersion != killplan.SchemaVersion {
		return killplan.Plan{}, fmt.Errorf("plan schema version %q does not match supported version %q", plan.SchemaVersion, killplan.SchemaVersion)
	}
	return plan, nil
}

func parseDemo(ctx context.Context, opts Options) (Source, killplan.Plan, error) {
	r, err := loadRules(opts.RulesPath)
	if err != nil {
		return Source{}, killplan.Plan{}, err
	}

	// #nosec G304 -- demo path is an explicit local CLI input.
	demoFile, err := os.Open(opts.DemoPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Source{}, killplan.Plan{}, fmt.Errorf("demo file not found: %s", opts.DemoPath)
		}
		return Source{}, killplan.Plan{}, fmt.Errorf("opening demo: %w", err)
	}
	defer demoFile.Close()

	sha, err := sha256Hex(demoFile)
	if err != nil {
		return Source{}, killplan.Plan{}, fmt.Errorf("hashing demo: %w", err)
	}
	if _, err := demoFile.Seek(0, io.SeekStart); err != nil {
		return Source{}, killplan.Plan{}, fmt.Errorf("rewinding demo: %w", err)
	}

	meta := parser.PlanMeta{DemoPath: opts.DemoPath, SHA256: sha}
	p := demoinfocs.NewParser(demoFile)
	defer p.Close()

	plan, err := parser.RunWithContext(ctx, p, opts.SteamID, r, meta, parser.RunOptions{SegmentMode: opts.SegmentMode})
	if err != nil {
		if errors.Is(err, parser.ErrTargetNotFound) {
			return Source{}, killplan.Plan{}, fmt.Errorf("%w: %s", err, opts.SteamID)
		}
		return Source{}, killplan.Plan{}, fmt.Errorf("parsing demo: %w", err)
	}
	return Source{Kind: "demo", Path: opts.DemoPath, SHA256: sha}, plan, nil
}

func loadRules(path string) (rules.Rules, error) {
	if path == "" {
		return rules.Default(), nil
	}
	// #nosec G304 -- rules path is an explicit local CLI input.
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rules.Rules{}, fmt.Errorf("rules file not found: %s", path)
		}
		return rules.Rules{}, fmt.Errorf("opening rules: %w", err)
	}
	defer f.Close()

	r, err := rules.Load(f)
	if err != nil {
		return rules.Rules{}, fmt.Errorf("invalid rules: %w", err)
	}
	return r, nil
}

func sha256Hex(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
