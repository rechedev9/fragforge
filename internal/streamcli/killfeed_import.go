package streamcli

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

const killfeedImportSchemaVersion = "1.0"

type killfeedImportDocument struct {
	SchemaVersion string              `json:"schema_version"`
	ClipID        string              `json:"clip_id"`
	Cues          []killfeedImportCue `json:"cues"`
}

type killfeedImportCue struct {
	AtSeconds float64                    `json:"at_seconds"`
	Kills     []streamclips.KillfeedKill `json:"kills"`
}

type streamKillfeedResult struct {
	OK               bool                 `json:"ok"`
	DryRun           bool                 `json:"dry_run"`
	Executed         bool                 `json:"executed"`
	Input            string               `json:"input"`
	Events           string               `json:"events"`
	Output           string               `json:"output"`
	ClipID           string               `json:"clip_id"`
	CueCount         int                  `json:"cue_count"`
	RejectedCueCount int                  `json:"rejected_cue_count"`
	KillCount        int                  `json:"kill_count"`
	Plan             streamclips.EditPlan `json:"plan"`
}

func runStreamKillfeed(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, streamKillfeedUsage)
		return exitSuccess
	}
	fs := flag.NewFlagSet("stream killfeed", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	planPath := fs.String("plan", "", "input stream edit plan")
	eventsPath := fs.String("events", "", "reviewed factual killfeed events")
	outPath := fs.String("out", "", "enriched edit plan output")
	format := fs.String("format", "text", "text or json")
	dryRun := fs.Bool("dry-run", false, "validate and print without writing the plan")
	if err := fs.Parse(args); err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamKillfeedUsage)
	}
	if fs.NArg() != 0 {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unexpected positional arg %q", fs.Arg(0)), streamKillfeedUsage)
	}
	if *planPath == "" || *eventsPath == "" || *outPath == "" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("--plan, --events, and --out are required"), streamKillfeedUsage)
	}
	if *format != "text" && *format != "json" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unsupported format %q", *format), streamKillfeedUsage)
	}
	if err := rejectStreamOutputAliases(*outPath,
		streamInputPath{flag: "--plan", path: *planPath},
		streamInputPath{flag: "--events", path: *eventsPath},
	); err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamKillfeedUsage)
	}

	var plan streamclips.EditPlan
	if err := readStrictJSON(*planPath, &plan); err != nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("read stream edit plan: %w", err), streamKillfeedUsage)
	}
	var document killfeedImportDocument
	if err := readStrictJSON(*eventsPath, &document); err != nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("read killfeed events: %w", err), streamKillfeedUsage)
	}
	if document.SchemaVersion != killfeedImportSchemaVersion {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unsupported killfeed events schema_version %q", document.SchemaVersion), streamKillfeedUsage)
	}
	if document.ClipID == "" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("killfeed events clip_id is required"), streamKillfeedUsage)
	}

	clipIndex := -1
	for i := range plan.Clips {
		if plan.Clips[i].ID == document.ClipID {
			clipIndex = i
			break
		}
	}
	if clipIndex < 0 {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unknown clip_id %q", document.ClipID), streamKillfeedUsage)
	}
	clip := &plan.Clips[clipIndex]
	if len(document.Cues) != len(clip.KillfeedSeconds) {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("killfeed events has %d cues; clip %s has %d detected cues", len(document.Cues), clip.ID, len(clip.KillfeedSeconds)), streamKillfeedUsage)
	}

	confirmedCues := make([]float64, 0, len(document.Cues))
	confirmedKills := make([][]streamclips.KillfeedKill, 0, len(document.Cues))
	confirmedProvenance := make([]streamclips.KillfeedCueProvenance, 0, len(document.Cues))
	killCount := 0
	rejectedCueCount := 0
	for i, cue := range document.Cues {
		if math.Abs(cue.AtSeconds-clip.KillfeedSeconds[i]) > 0.002 {
			return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("killfeed cue %d at_seconds %.3f does not match detected cue %.3f", i, cue.AtSeconds, clip.KillfeedSeconds[i]), streamKillfeedUsage)
		}
		if len(cue.Kills) == 0 {
			rejectedCueCount++
			continue
		}
		// The reviewed document may round its display timestamp. Keep the
		// detector's exact PTS-derived cue as the durable render coordinate.
		exactCue := clip.KillfeedSeconds[i]
		confirmedCues = append(confirmedCues, exactCue)
		confirmedKills = append(confirmedKills, append([]streamclips.KillfeedKill(nil), cue.Kills...))
		provenance, exists := clip.KillfeedProvenanceAt(exactCue)
		if !exists {
			// stream killfeed imports review detections. This also upgrades
			// pre-provenance detected plans without guessing from the new kills.
			provenance = streamclips.KillfeedCueProvenance{
				CueSeconds: exactCue,
				Origin:     streamclips.KillfeedCueAutomatic,
			}
		}
		provenance.CueSeconds = exactCue
		confirmedProvenance = append(confirmedProvenance, provenance)
		killCount += len(cue.Kills)
	}
	clip.KillfeedSeconds = confirmedCues
	clip.KillfeedKills = confirmedKills
	clip.KillfeedCueProvenance = confirmedProvenance
	plan.UpdatedAt = time.Now().UTC()
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.Validate(); err != nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("invalid enriched stream edit plan: %w", err), streamKillfeedUsage)
	}

	absInput, _ := filepath.Abs(*planPath)
	absEvents, _ := filepath.Abs(*eventsPath)
	absOut, _ := filepath.Abs(*outPath)
	result := streamKillfeedResult{
		OK:               true,
		DryRun:           *dryRun,
		Executed:         !*dryRun,
		Input:            absInput,
		Events:           absEvents,
		Output:           absOut,
		ClipID:           document.ClipID,
		CueCount:         len(document.Cues),
		RejectedCueCount: rejectedCueCount,
		KillCount:        killCount,
		Plan:             plan,
	}
	if !*dryRun {
		body, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return writeStreamRuntimeError(args, stdout, stderr, err)
		}
		if err := putLocalFile(absOut, append(body, '\n')); err != nil {
			return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("write enriched stream edit plan: %w", err))
		}
	}
	if *format == "json" {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: write stream killfeed result: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	if *dryRun {
		fmt.Fprintf(stdout, "valid factual killfeed: %d reviewed cues (%d rejected), %d kills -> %s (not written)\n", result.CueCount, result.RejectedCueCount, result.KillCount, absOut)
	} else {
		fmt.Fprintf(stdout, "wrote factual killfeed: %d reviewed cues (%d rejected), %d kills -> %s\n", result.CueCount, result.RejectedCueCount, result.KillCount, absOut)
	}
	return exitSuccess
}

func readStrictJSON(path string, value any) error {
	// #nosec G304 -- paths are explicit local CLI inputs.
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}
