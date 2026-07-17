package streamcli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

const captionImportSchemaVersion = "1.0"

type captionImportDocument struct {
	SchemaVersion string                    `json:"schema_version"`
	ClipID        string                    `json:"clip_id"`
	Language      string                    `json:"language"`
	NoSpeech      bool                      `json:"no_speech,omitempty"`
	Words         []streamclips.CaptionWord `json:"words"`
}

type streamCaptionsResult struct {
	OK        bool                 `json:"ok"`
	DryRun    bool                 `json:"dry_run"`
	Executed  bool                 `json:"executed"`
	Input     string               `json:"input"`
	Words     string               `json:"words"`
	Output    string               `json:"output"`
	ClipID    string               `json:"clip_id"`
	WordCount int                  `json:"word_count"`
	Language  string               `json:"language"`
	Plan      streamclips.EditPlan `json:"plan"`
}

func runStreamCaptions(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, streamCaptionsUsage)
		return exitSuccess
	}
	fs := flag.NewFlagSet("stream captions", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	planPath := fs.String("plan", "", "input stream edit plan")
	wordsPath := fs.String("words", "", "reviewed Spanish word cues")
	outPath := fs.String("out", "", "caption-enriched edit plan output")
	format := fs.String("format", "text", "text or json")
	dryRun := fs.Bool("dry-run", false, "validate and print without writing the plan")
	if err := fs.Parse(args); err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamCaptionsUsage)
	}
	if fs.NArg() != 0 {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unexpected positional arg %q", fs.Arg(0)), streamCaptionsUsage)
	}
	if *planPath == "" || *wordsPath == "" || *outPath == "" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("--plan, --words, and --out are required"), streamCaptionsUsage)
	}
	if *format != "text" && *format != "json" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unsupported format %q", *format), streamCaptionsUsage)
	}
	if err := rejectStreamOutputAliases(*outPath,
		streamInputPath{flag: "--plan", path: *planPath},
		streamInputPath{flag: "--words", path: *wordsPath},
	); err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamCaptionsUsage)
	}

	var plan streamclips.EditPlan
	if err := readStrictJSON(*planPath, &plan); err != nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("read stream edit plan: %w", err), streamCaptionsUsage)
	}
	var document captionImportDocument
	if err := readStrictJSON(*wordsPath, &document); err != nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("read caption words: %w", err), streamCaptionsUsage)
	}
	if document.SchemaVersion != captionImportSchemaVersion {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unsupported caption words schema_version %q", document.SchemaVersion), streamCaptionsUsage)
	}
	if document.ClipID == "" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("caption words clip_id is required"), streamCaptionsUsage)
	}
	if document.Language != "es" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("caption words language must be %q", "es"), streamCaptionsUsage)
	}
	if len(document.Words) == 0 && !document.NoSpeech {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("caption words must contain at least one reviewed word or set no_speech to true"), streamCaptionsUsage)
	}
	if len(document.Words) > 0 && document.NoSpeech {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("caption words cannot contain words when no_speech is true"), streamCaptionsUsage)
	}

	clipIndex := -1
	for i := range plan.Clips {
		if plan.Clips[i].ID == document.ClipID {
			clipIndex = i
			break
		}
	}
	if clipIndex < 0 {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unknown clip_id %q", document.ClipID), streamCaptionsUsage)
	}
	plan.Clips[clipIndex].CaptionWords = append([]streamclips.CaptionWord(nil), document.Words...)
	plan.Clips[clipIndex].CaptionReviewed = true
	plan.Captions = streamclips.CaptionsPlan{Enabled: true, Language: "es"}
	plan.UpdatedAt = time.Now().UTC()
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.Validate(); err != nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("invalid caption-enriched stream edit plan: %w", err), streamCaptionsUsage)
	}

	absInput, _ := filepath.Abs(*planPath)
	absWords, _ := filepath.Abs(*wordsPath)
	absOut, _ := filepath.Abs(*outPath)
	result := streamCaptionsResult{
		OK: true, DryRun: *dryRun, Executed: !*dryRun,
		Input: absInput, Words: absWords, Output: absOut,
		ClipID: document.ClipID, WordCount: len(document.Words), Language: "es", Plan: plan,
	}
	if !*dryRun {
		body, err := json.MarshalIndent(plan, "", "  ")
		if err != nil {
			return writeStreamRuntimeError(args, stdout, stderr, err)
		}
		if err := putLocalFile(absOut, append(body, '\n')); err != nil {
			return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("write caption-enriched stream edit plan: %w", err))
		}
	}
	if *format == "json" {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: write stream captions result: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	if *dryRun {
		fmt.Fprintf(stdout, "valid reviewed Spanish captions: %d words -> %s (not written)\n", result.WordCount, absOut)
	} else {
		fmt.Fprintf(stdout, "wrote reviewed Spanish captions: %d words -> %s\n", result.WordCount, absOut)
	}
	return exitSuccess
}
