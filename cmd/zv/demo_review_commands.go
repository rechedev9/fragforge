package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/moments"
	"github.com/rechedev9/fragforge/internal/pathguard"
	"github.com/rechedev9/fragforge/internal/storage"
)

type demoMomentsResult struct {
	OK       bool             `json:"ok"`
	DryRun   bool             `json:"dry_run"`
	Executed bool             `json:"executed"`
	Input    string           `json:"input"`
	Output   string           `json:"output,omitempty"`
	Count    int              `json:"count"`
	Document moments.Document `json:"document"`
}

type demoSelectionResult struct {
	OK               bool          `json:"ok"`
	DryRun           bool          `json:"dry_run"`
	Executed         bool          `json:"executed"`
	Input            string        `json:"input"`
	Output           string        `json:"output"`
	SelectedSegments []string      `json:"selected_segments"`
	Plan             killplan.Plan `json:"plan"`
}

func runDemoMoments(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, demoMomentsUsage)
		return exitSuccess
	}
	fs := flag.NewFlagSet("demo moments", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	killPlanPath := fs.String("killplan", "", "kill plan JSON to score")
	outPath := fs.String("out", "", "optional moments JSON artifact")
	top := fs.Int("top", 0, "maximum moments, highest score first; 0 keeps all")
	format := fs.String("format", "text", "text or json")
	dryRun := fs.Bool("dry-run", false, "validate and score without writing")
	if err := fs.Parse(args); err != nil {
		return writeDemoReviewError(args, stdout, stderr, err, demoMomentsUsage, exitInvalidArgs)
	}
	if fs.NArg() != 0 {
		return writeDemoReviewError(args, stdout, stderr, fmt.Errorf("unexpected positional arg %q", fs.Arg(0)), demoMomentsUsage, exitInvalidArgs)
	}
	if strings.TrimSpace(*killPlanPath) == "" {
		return writeDemoReviewError(args, stdout, stderr, fmt.Errorf("--killplan is required"), demoMomentsUsage, exitInvalidArgs)
	}
	if *top < 0 {
		return writeDemoReviewError(args, stdout, stderr, fmt.Errorf("--top must be >= 0"), demoMomentsUsage, exitInvalidArgs)
	}
	if *format != "text" && *format != "json" {
		return writeDemoReviewError(args, stdout, stderr, fmt.Errorf("unsupported format %q", *format), demoMomentsUsage, exitInvalidArgs)
	}
	if strings.TrimSpace(*outPath) != "" {
		if err := pathguard.RejectOutputAliases(*outPath, pathguard.Input{Flag: "--killplan", Path: *killPlanPath}); err != nil {
			return writeDemoReviewError(args, stdout, stderr, err, demoMomentsUsage, exitInvalidArgs)
		}
	}

	plan, err := loadDemoKillPlan(*killPlanPath)
	if err != nil {
		return writeDemoReviewError(args, stdout, stderr, fmt.Errorf("read kill plan: %w", err), "", exitUnexpected)
	}
	doc := moments.Build(demoMomentsJobID(plan), plan)
	sort.SliceStable(doc.Moments, func(i, j int) bool {
		if doc.Moments[i].Score != doc.Moments[j].Score {
			return doc.Moments[i].Score > doc.Moments[j].Score
		}
		return doc.Moments[i].SegmentID < doc.Moments[j].SegmentID
	})
	if *top > 0 && len(doc.Moments) > *top {
		doc.Moments = doc.Moments[:*top]
	}
	absInput, _ := filepath.Abs(*killPlanPath)
	result := demoMomentsResult{OK: true, DryRun: *dryRun, Executed: !*dryRun, Input: absInput, Count: len(doc.Moments), Document: doc}
	if strings.TrimSpace(*outPath) != "" {
		absOut, _ := filepath.Abs(*outPath)
		result.Output = absOut
		if !*dryRun {
			if err := writeDemoReviewJSON(absOut, doc); err != nil {
				return writeDemoReviewError(args, stdout, stderr, fmt.Errorf("write moments: %w", err), "", exitUnexpected)
			}
		}
	}
	if *format == "json" {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: write demo moments result: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	fmt.Fprintln(stdout, "score\tsegment\tround\tkills\theadshots\twallbangs\tduration\treasons")
	for _, moment := range doc.Moments {
		fmt.Fprintf(stdout, "%.2f\t%s\t%d\t%d\t%d\t%d\t%.3f\t%s\n",
			moment.Score, moment.SegmentID, moment.Round, moment.Events.Kills,
			moment.Events.Headshots, moment.Events.Wallbangs, moment.DurationSeconds,
			strings.Join(moment.ReasonCodes, ","))
	}
	if result.Output != "" {
		if *dryRun {
			fmt.Fprintf(stdout, "moments: %s (not written)\n", result.Output)
		} else {
			fmt.Fprintf(stdout, "moments: %s\n", result.Output)
		}
	}
	return exitSuccess
}

func runDemoSelect(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, demoSelectUsage)
		return exitSuccess
	}
	fs := flag.NewFlagSet("demo select", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	killPlanPath := fs.String("killplan", "", "source kill plan JSON")
	segmentsValue := fs.String("segments", "", "ordered comma-separated segment ids")
	outPath := fs.String("out", "", "selected kill plan JSON")
	format := fs.String("format", "text", "text or json")
	dryRun := fs.Bool("dry-run", false, "validate selection without writing")
	if err := fs.Parse(args); err != nil {
		return writeDemoReviewError(args, stdout, stderr, err, demoSelectUsage, exitInvalidArgs)
	}
	if fs.NArg() != 0 {
		return writeDemoReviewError(args, stdout, stderr, fmt.Errorf("unexpected positional arg %q", fs.Arg(0)), demoSelectUsage, exitInvalidArgs)
	}
	if strings.TrimSpace(*killPlanPath) == "" || strings.TrimSpace(*segmentsValue) == "" || strings.TrimSpace(*outPath) == "" {
		return writeDemoReviewError(args, stdout, stderr, fmt.Errorf("--killplan, --segments, and --out are required"), demoSelectUsage, exitInvalidArgs)
	}
	if *format != "text" && *format != "json" {
		return writeDemoReviewError(args, stdout, stderr, fmt.Errorf("unsupported format %q", *format), demoSelectUsage, exitInvalidArgs)
	}
	if err := pathguard.RejectOutputAliases(*outPath, pathguard.Input{Flag: "--killplan", Path: *killPlanPath}); err != nil {
		return writeDemoReviewError(args, stdout, stderr, err, demoSelectUsage, exitInvalidArgs)
	}
	segmentIDs, err := parseOrderedSegmentIDs(*segmentsValue)
	if err != nil {
		return writeDemoReviewError(args, stdout, stderr, err, demoSelectUsage, exitInvalidArgs)
	}
	plan, err := loadDemoKillPlan(*killPlanPath)
	if err != nil {
		return writeDemoReviewError(args, stdout, stderr, fmt.Errorf("read kill plan: %w", err), "", exitUnexpected)
	}
	selected, err := selectDemoSegments(plan, segmentIDs)
	if err != nil {
		return writeDemoReviewError(args, stdout, stderr, err, demoSelectUsage, exitInvalidArgs)
	}
	absInput, _ := filepath.Abs(*killPlanPath)
	absOut, _ := filepath.Abs(*outPath)
	result := demoSelectionResult{
		OK:               true,
		DryRun:           *dryRun,
		Executed:         !*dryRun,
		Input:            absInput,
		Output:           absOut,
		SelectedSegments: append([]string(nil), segmentIDs...),
		Plan:             selected,
	}
	if !*dryRun {
		if err := writeDemoReviewJSON(absOut, selected); err != nil {
			return writeDemoReviewError(args, stdout, stderr, fmt.Errorf("write selected kill plan: %w", err), "", exitUnexpected)
		}
	}
	if *format == "json" {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: write demo selection result: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	if *dryRun {
		fmt.Fprintf(stdout, "valid demo selection: %s (not written)\n", strings.Join(segmentIDs, ","))
	} else {
		fmt.Fprintf(stdout, "selected demo plan: %s\n", absOut)
	}
	fmt.Fprintf(stdout, "segments: %d, kills: %d, duration: %.3fs\n",
		selected.Stats.SegmentsCreated, selected.Stats.KillsAfterFilters, selected.Stats.DurationSecondsTotal)
	return exitSuccess
}

func loadDemoKillPlan(path string) (killplan.Plan, error) {
	// #nosec G304 -- the CLI operator explicitly supplies the local plan path.
	body, err := os.ReadFile(path)
	if err != nil {
		return killplan.Plan{}, err
	}
	var plan killplan.Plan
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&plan); err != nil {
		return killplan.Plan{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return killplan.Plan{}, err
	}
	if plan.SchemaVersion != killplan.SchemaVersion {
		return killplan.Plan{}, fmt.Errorf("unsupported kill plan schema %q (want %q)", plan.SchemaVersion, killplan.SchemaVersion)
	}
	if plan.Demo.Tickrate <= 0 {
		return killplan.Plan{}, fmt.Errorf("kill plan tickrate must be positive")
	}
	return plan, nil
}

func demoMomentsJobID(plan killplan.Plan) uuid.UUID {
	identity := plan.Demo.SHA256 + "\n" + plan.Target.SteamID64
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(identity))
}

func parseOrderedSegmentIDs(value string) ([]string, error) {
	seen := map[string]bool{}
	var ids []string
	for _, raw := range strings.Split(value, ",") {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate segment id %q", id)
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("--segments did not contain any segment ids")
	}
	return ids, nil
}

func selectDemoSegments(plan killplan.Plan, ids []string) (killplan.Plan, error) {
	byID := make(map[string]killplan.Segment, len(plan.Segments))
	for _, segment := range plan.Segments {
		if strings.TrimSpace(segment.ID) == "" {
			return killplan.Plan{}, fmt.Errorf("kill plan contains a segment without an id")
		}
		if _, exists := byID[segment.ID]; exists {
			return killplan.Plan{}, fmt.Errorf("kill plan contains duplicate segment id %q", segment.ID)
		}
		byID[segment.ID] = segment
	}
	selected := plan
	selected.GeneratedAt = time.Now().UTC()
	selected.Segments = make([]killplan.Segment, 0, len(ids))
	var missing []string
	for _, id := range ids {
		segment, ok := byID[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		selected.Segments = append(selected.Segments, segment)
	}
	if len(missing) > 0 {
		return killplan.Plan{}, fmt.Errorf("unknown segment ids: %s", strings.Join(missing, ", "))
	}
	selected.Stats.KillsAfterFilters = 0
	selected.Stats.UtilityAfterFilters = 0
	selected.Stats.SmokesAfterFilters = 0
	selected.Stats.SegmentsCreated = len(selected.Segments)
	selected.Stats.DurationSecondsTotal = 0
	for _, segment := range selected.Segments {
		selected.Stats.KillsAfterFilters += len(segment.Kills)
		selected.Stats.UtilityAfterFilters += len(segment.Utility)
		for _, utility := range segment.Utility {
			if strings.EqualFold(utility.Type, "smoke") || strings.EqualFold(utility.Type, "smokegrenade") {
				selected.Stats.SmokesAfterFilters++
			}
		}
		if segment.TickEnd > segment.TickStart {
			selected.Stats.DurationSecondsTotal += float64(segment.TickEnd-segment.TickStart) / float64(selected.Demo.Tickrate)
		}
	}
	return selected, nil
}

func writeDemoReviewJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
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

func writeDemoReviewError(args []string, stdout, stderr io.Writer, err error, commandUsage string, code int) int {
	if shortJSONRequested(args) {
		if writeErr := writeJSON(stdout, map[string]any{"ok": false, "executed": false, "error": err.Error()}); writeErr != nil {
			fmt.Fprintf(stderr, "error: write demo json error: %v\n", writeErr)
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
