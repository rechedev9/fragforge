// Package streamcli implements the local stream-to-Short command surface used
// by the thin zv-stream binary and, through it, the unified zv CLI.
package streamcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
	"github.com/rechedev9/fragforge/internal/workers"
)

type streamService interface {
	Probe(ctx context.Context, input, ffprobe string) (streamclips.SourceProbe, error)
	ValidateFFmpeg(ctx context.Context, ffmpeg string, requireWhisper bool) error
	DetectKillfeed(ctx context.Context, input, ffmpeg string, crop streamclips.CropRect, start, end float64) ([]float64, error)
	Transcribe(ctx context.Context, request streamTranscribeRequest) (streamTranscriptReview, error)
	Render(ctx context.Context, request streamRenderRequest) (streamRenderResult, error)
}

type localStreamService struct{}

type streamRenderRequest struct {
	Input          string
	PlanPath       string
	OutDir         string
	Title          string
	FFmpeg         string
	Timeout        string
	WorkDir        string
	MusicDir       string
	Plan           streamclips.EditPlan
	PlanJSON       []byte
	Probe          streamclips.SourceProbe
	CoverGenerator streamCoverGenerator
}

type streamLocalVideo struct {
	ClipID          string  `json:"clip_id"`
	Title           string  `json:"title,omitempty"`
	Path            string  `json:"path"`
	CoverPath       string  `json:"cover_path,omitempty"`
	CaptionsPath    string  `json:"captions_path,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

type streamRenderResult struct {
	OK         bool                    `json:"ok"`
	DryRun     bool                    `json:"dry_run"`
	Executed   bool                    `json:"executed"`
	JobID      string                  `json:"job_id,omitempty"`
	Input      string                  `json:"input"`
	Plan       string                  `json:"plan"`
	Variant    string                  `json:"variant"`
	Probe      streamclips.SourceProbe `json:"probe"`
	OutputDir  string                  `json:"output_dir"`
	PublishDir string                  `json:"publish_dir"`
	Manifest   string                  `json:"manifest,omitempty"`
	Gallery    string                  `json:"gallery,omitempty"`
	Videos     []streamLocalVideo      `json:"videos"`
	Warnings   []string                `json:"warnings"`
}

type streamPlanResult struct {
	OK       bool                    `json:"ok"`
	DryRun   bool                    `json:"dry_run"`
	Executed bool                    `json:"executed"`
	Input    string                  `json:"input"`
	Output   string                  `json:"output"`
	Probe    streamclips.SourceProbe `json:"probe"`
	Plan     streamclips.EditPlan    `json:"plan"`
}

type streamVariantRow struct {
	Name                   string               `json:"name"`
	Label                  string               `json:"label"`
	Description            string               `json:"description"`
	FullFrame              bool                 `json:"full_frame"`
	OutputWidth            int                  `json:"output_width"`
	FaceOutputHeight       int                  `json:"face_output_height"`
	GameplayOutputHeight   int                  `json:"gameplay_output_height"`
	DefaultFaceCrop        streamclips.CropRect `json:"default_face_crop"`
	DefaultGameplayCrop    streamclips.CropRect `json:"default_gameplay_crop"`
	DefaultBannerPositionY float64              `json:"default_banner_position_y"`
}

type streamErrorResult struct {
	OK       bool   `json:"ok"`
	Executed bool   `json:"executed"`
	Error    string `json:"error"`
}

func Run(args []string, stdout, stderr io.Writer) int {
	return runStreamWithService(args, stdout, stderr, localStreamService{})
}

func runStreamWithService(args []string, stdout, stderr io.Writer, service streamService) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, streamUsage)
		return exitInvalidArgs
	}
	if isHelp(args[0]) {
		fmt.Fprint(stdout, streamUsage)
		return exitSuccess
	}
	switch args[0] {
	case "variants":
		return runStreamVariants(args[1:], stdout, stderr)
	case "plan":
		return runStreamPlan(args[1:], stdout, stderr, service)
	case "killfeed":
		return runStreamKillfeed(args[1:], stdout, stderr)
	case "transcribe":
		return runStreamTranscribe(args[1:], stdout, stderr, service)
	case "captions":
		return runStreamCaptions(args[1:], stdout, stderr)
	case "render":
		return runStreamRender(args[1:], stdout, stderr, service)
	default:
		fmt.Fprintf(stderr, "unknown stream command %q\n%s", args[0], streamUsage)
		return exitInvalidArgs
	}
}

func runStreamVariants(args []string, stdout, stderr io.Writer) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, streamVariantsUsage)
		return exitSuccess
	}
	format, rest, err := parseFormatArgs(args)
	if err != nil || len(rest) != 0 {
		if err == nil {
			err = fmt.Errorf("unexpected extra args")
		}
		return writeStreamCommandError(args, stdout, stderr, err, streamVariantsUsage)
	}
	rows := make([]streamVariantRow, 0, len(streamclips.VariantNames()))
	for _, name := range streamclips.VariantNames() {
		variant, _ := streamclips.VariantByName(name)
		rows = append(rows, streamVariantRow{
			Name:                   variant.Name,
			Label:                  variant.Label,
			Description:            variant.Description,
			FullFrame:              variant.FullFrame,
			OutputWidth:            variant.OutputWidth,
			FaceOutputHeight:       variant.FaceOutputHeight,
			GameplayOutputHeight:   variant.GameOutputHeight,
			DefaultFaceCrop:        variant.DefaultFaceCrop,
			DefaultGameplayCrop:    variant.DefaultGameplayCrop,
			DefaultBannerPositionY: variant.DefaultBannerPositionY,
		})
	}
	if format == "json" {
		if err := writeJSON(stdout, struct {
			OK       bool               `json:"ok"`
			Variants []streamVariantRow `json:"variants"`
		}{OK: true, Variants: rows}); err != nil {
			fmt.Fprintf(stderr, "error: write stream variants: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	for _, row := range rows {
		fmt.Fprintf(stdout, "%s\t%s\n", row.Name, row.Description)
	}
	return exitSuccess
}

func runStreamPlan(args []string, stdout, stderr io.Writer, service streamService) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, streamPlanUsage)
		return exitSuccess
	}
	fs := flag.NewFlagSet("stream plan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	input := fs.String("input", "", "source stream video")
	out := fs.String("out", "", "edit plan output")
	variantName := fs.String("variant", streamclips.DefaultVariant().Name, "layout variant")
	clipID := fs.String("clip-id", "clip-001", "clip id")
	clipStart := fs.Float64("clip-start", 0, "clip start in seconds")
	clipEnd := fs.Float64("clip-end", 0, "clip end in seconds; 0 uses source duration")
	title := fs.String("title", "", "clip title")
	nick := fs.String("streamer", "", "streamer nick banner")
	faceCrop := fs.String("face-crop", "", "normalized x,y,width,height")
	gameplayCrop := fs.String("gameplay-crop", "", "normalized x,y,width,height")
	killfeedCrop := fs.String("killfeed-crop", "", "normalized x,y,width,height")
	detectKillfeed := fs.Bool("detect-killfeed", false, "detect highlighted killfeed notice cues")
	captionsEnabled := fs.Bool("captions", false, "enable Spanish burned captions")
	ffmpeg := fs.String("ffmpeg", "", "ffmpeg executable for killfeed detection")
	ffprobe := fs.String("ffprobe", "", "ffprobe executable")
	format := fs.String("format", "text", "text or json")
	dryRun := fs.Bool("dry-run", false, "validate and print without writing the plan")
	if err := fs.Parse(args); err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamPlanUsage)
	}
	if fs.NArg() != 0 {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unexpected positional arg %q", fs.Arg(0)), streamPlanUsage)
	}
	if *input == "" || *out == "" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("--input and --out are required"), streamPlanUsage)
	}
	if *format != "text" && *format != "json" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unsupported format %q", *format), streamPlanUsage)
	}
	if err := rejectStreamOutputAliases(*out, streamInputPath{flag: "--input", path: *input}); err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamPlanUsage)
	}
	variant, ok := streamclips.VariantByName(*variantName)
	if !ok {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unsupported stream render variant %q (valid variants: %s)", *variantName, strings.Join(streamclips.VariantNames(), ", ")), streamPlanUsage)
	}
	probePath := *ffprobe
	if probePath == "" {
		probePath = recording.FindFFprobe()
	}
	probe, err := service.Probe(context.Background(), *input, probePath)
	if err != nil {
		return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("probe stream input: %w", err))
	}
	end := *clipEnd
	if end == 0 {
		end = probe.DurationSeconds
	}
	plan := streamclips.DefaultEditPlan()
	plan.Variant = variant.Name
	plan.FaceCrop = variant.DefaultFaceCrop
	plan.GameplayCrop = variant.DefaultGameplayCrop
	if *faceCrop != "" {
		plan.FaceCrop, err = parseStreamCrop(*faceCrop)
		if err != nil {
			return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("--face-crop: %w", err), streamPlanUsage)
		}
	}
	if *gameplayCrop != "" {
		plan.GameplayCrop, err = parseStreamCrop(*gameplayCrop)
		if err != nil {
			return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("--gameplay-crop: %w", err), streamPlanUsage)
		}
	}
	if *killfeedCrop != "" {
		crop, cropErr := parseStreamCrop(*killfeedCrop)
		if cropErr != nil {
			return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("--killfeed-crop: %w", cropErr), streamPlanUsage)
		}
		plan.KillfeedCrop = &crop
	}
	if *detectKillfeed && plan.KillfeedCrop == nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("--detect-killfeed requires --killfeed-crop"), streamPlanUsage)
	}
	plan.Clips = []streamclips.ClipRange{{
		ID:           *clipID,
		StartSeconds: *clipStart,
		EndSeconds:   end,
		Title:        *title,
	}}
	plan.StreamerBanner.Nick = *nick
	plan.Captions = streamclips.CaptionsPlan{Enabled: *captionsEnabled, Language: "es"}
	if *detectKillfeed {
		ffmpegPath := *ffmpeg
		if ffmpegPath == "" {
			ffmpegPath = recording.FindFFmpeg()
		}
		cues, detectErr := service.DetectKillfeed(context.Background(), *input, ffmpegPath, *plan.KillfeedCrop, *clipStart, end)
		if detectErr != nil {
			return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("detect stream killfeed: %w", detectErr))
		}
		plan.Clips[0].KillfeedSeconds = cues
	}
	plan.UpdatedAt = time.Now().UTC()
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.ValidateForSourceDuration(probe.DurationSeconds); err != nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("invalid stream edit plan: %w", err), streamPlanUsage)
	}
	absInput, _ := filepath.Abs(*input)
	absOut, _ := filepath.Abs(*out)
	result := streamPlanResult{
		OK:       true,
		DryRun:   *dryRun,
		Executed: !*dryRun,
		Input:    absInput,
		Output:   absOut,
		Probe:    probe,
		Plan:     plan,
	}
	if !*dryRun {
		body, marshalErr := json.MarshalIndent(plan, "", "  ")
		if marshalErr != nil {
			return writeStreamRuntimeError(args, stdout, stderr, marshalErr)
		}
		body = append(body, '\n')
		if err := putLocalFile(absOut, body); err != nil {
			return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("write stream edit plan: %w", err))
		}
	}
	if *format == "json" {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: write stream plan result: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	if *dryRun {
		fmt.Fprintf(stdout, "valid stream plan: %s (not written)\n", absOut)
	} else {
		fmt.Fprintf(stdout, "wrote stream plan: %s\n", absOut)
	}
	return exitSuccess
}

func runStreamRender(args []string, stdout, stderr io.Writer, service streamService) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, streamRenderUsage)
		return exitSuccess
	}
	fs := flag.NewFlagSet("stream render", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	input := fs.String("input", "", "source stream video")
	planPath := fs.String("plan", "", "stream edit plan")
	out := fs.String("out", "", "run output directory")
	title := fs.String("title", "", "gallery title")
	ffmpeg := fs.String("ffmpeg", "", "ffmpeg executable")
	ffprobe := fs.String("ffprobe", "", "ffprobe executable")
	timeout := fs.String("timeout", "20m", "render timeout")
	workDir := fs.String("work-dir", "", "temporary work directory")
	musicDir := fs.String("music-dir", "", "music catalog directory")
	format := fs.String("format", "text", "text or json")
	dryRun := fs.Bool("dry-run", false, "probe and validate without rendering")
	if err := fs.Parse(args); err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamRenderUsage)
	}
	if fs.NArg() != 0 {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unexpected positional arg %q", fs.Arg(0)), streamRenderUsage)
	}
	if *input == "" || *planPath == "" || *out == "" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("--input, --plan, and --out are required"), streamRenderUsage)
	}
	if *format != "text" && *format != "json" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unsupported format %q", *format), streamRenderUsage)
	}
	renderTimeout, err := time.ParseDuration(*timeout)
	if err != nil || renderTimeout <= 0 {
		if err == nil {
			err = fmt.Errorf("must be positive")
		}
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("invalid --timeout: %w", err), streamRenderUsage)
	}
	ctx, cancel := context.WithTimeout(context.Background(), renderTimeout)
	defer cancel()
	planBody, err := os.ReadFile(*planPath)
	if err != nil {
		return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("read stream edit plan: %w", err))
	}
	var plan streamclips.EditPlan
	dec := json.NewDecoder(bytes.NewReader(planBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&plan); err != nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("decode stream edit plan: %w", err), streamRenderUsage)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("decode stream edit plan: %w", err), streamRenderUsage)
	}
	probePath := *ffprobe
	if probePath == "" {
		probePath = recording.FindFFprobe()
	}
	probe, err := service.Probe(ctx, *input, probePath)
	if err != nil {
		return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("probe stream input: %w", err))
	}
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.ValidateForSourceDuration(probe.DurationSeconds); err != nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("invalid stream edit plan: %w", err), streamRenderUsage)
	}
	if probe.AudioCodec != "" && plan.CaptionsNeedBackend() && strings.TrimSpace(os.Getenv("XAI_API_KEY")) == "" {
		return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("validate stream caption readiness: edit plan has an audible clip with neither reviewed caption words, a reviewed no-speech decision, nor XAI_API_KEY configured (review it with zv stream captions or set XAI_API_KEY)"))
	}
	absInput, _ := filepath.Abs(*input)
	absPlan, _ := filepath.Abs(*planPath)
	absOut, _ := filepath.Abs(*out)
	publishDir := filepath.Join(absOut, "shortslistosparasubir")
	publishInputs := []streamInputPath{
		{flag: "--input", path: absInput},
		{flag: "--plan", path: absPlan},
	}
	if strings.TrimSpace(*musicDir) != "" {
		publishInputs = append(publishInputs, streamInputPath{flag: "--music-dir", path: *musicDir})
	}
	if err := rejectStreamInputsWithinDirectory(publishDir, publishInputs...); err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamRenderUsage)
	}
	ffmpegPath := *ffmpeg
	if ffmpegPath == "" {
		ffmpegPath = recording.FindFFmpeg()
	}
	if err := service.ValidateFFmpeg(ctx, ffmpegPath, false); err != nil {
		return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("validate stream render ffmpeg: %w", err))
	}
	if *dryRun {
		result := streamRenderResult{
			OK:         true,
			DryRun:     true,
			Executed:   false,
			Input:      absInput,
			Plan:       absPlan,
			Variant:    plan.Variant,
			Probe:      probe,
			OutputDir:  absOut,
			PublishDir: publishDir,
			Videos:     []streamLocalVideo{},
			Warnings:   []string{},
		}
		if *format == "json" {
			if err := writeJSON(stdout, result); err != nil {
				fmt.Fprintf(stderr, "error: write stream dry-run: %v\n", err)
				return exitUnexpected
			}
			return exitSuccess
		}
		fmt.Fprintf(stdout, "valid stream render plan: %s -> %s (not executed)\n", absPlan, publishDir)
		return exitSuccess
	}
	request := streamRenderRequest{
		Input:    absInput,
		PlanPath: absPlan,
		OutDir:   absOut,
		Title:    *title,
		FFmpeg:   ffmpegPath,
		Timeout:  *timeout,
		WorkDir:  *workDir,
		MusicDir: *musicDir,
		Plan:     plan,
		PlanJSON: planBody,
		Probe:    probe,
	}
	result, err := service.Render(ctx, request)
	if err != nil {
		return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("render stream: %w", err))
	}
	if *format == "json" {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: write stream render result: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	fmt.Fprintf(stdout, "stream render ready: %s\n", result.PublishDir)
	for _, video := range result.Videos {
		fmt.Fprintf(stdout, "video: %s\n", video.Path)
	}
	return exitSuccess
}

func (localStreamService) Probe(ctx context.Context, input, ffprobe string) (streamclips.SourceProbe, error) {
	if ffprobe == "" {
		return streamclips.SourceProbe{}, fmt.Errorf("ffprobe is not configured")
	}
	info, err := os.Stat(input)
	if err != nil {
		return streamclips.SourceProbe{}, err
	}
	if !info.Mode().IsRegular() {
		return streamclips.SourceProbe{}, fmt.Errorf("input is not a regular file")
	}
	return (streamclips.FFprobeProber{Path: ffprobe}).Probe(ctx, input)
}

func (localStreamService) DetectKillfeed(ctx context.Context, input, ffmpeg string, crop streamclips.CropRect, start, end float64) ([]float64, error) {
	if ffmpeg == "" {
		return nil, fmt.Errorf("ffmpeg is not configured")
	}
	return detectKillfeedCues(ctx, ffmpeg, input, crop, start, end)
}

func (localStreamService) Render(ctx context.Context, request streamRenderRequest) (streamRenderResult, error) {
	if request.FFmpeg == "" {
		return streamRenderResult{}, fmt.Errorf("ffmpeg is not configured")
	}
	if _, err := time.ParseDuration(request.Timeout); err != nil {
		return streamRenderResult{}, fmt.Errorf("invalid timeout: %w", err)
	}
	sourceHash, err := sha256File(request.Input)
	if err != nil {
		return streamRenderResult{}, fmt.Errorf("hash source: %w", err)
	}
	planHash := sha256.Sum256(request.PlanJSON)
	jobID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(sourceHash+hex.EncodeToString(planHash[:])))
	store, err := storage.NewLocal(request.OutDir)
	if err != nil {
		return streamRenderResult{}, err
	}
	source, err := os.Open(request.Input)
	if err != nil {
		return streamRenderResult{}, err
	}
	if err := store.Put(streamclips.SourceKey(jobID), source); err != nil {
		_ = source.Close()
		return streamRenderResult{}, fmt.Errorf("stage stream source: %w", err)
	}
	if err := source.Close(); err != nil {
		return streamRenderResult{}, fmt.Errorf("close stream source: %w", err)
	}
	if err := store.Put(streamclips.EditPlanKey(jobID), bytes.NewReader(request.PlanJSON)); err != nil {
		return streamRenderResult{}, fmt.Errorf("stage stream plan: %w", err)
	}
	workDir := request.WorkDir
	if workDir == "" {
		workDir = filepath.Join(request.OutDir, ".work")
	}
	now := time.Now().UTC()
	job := streamclips.Job{
		ID:           jobID,
		Status:       streamclips.StatusReady,
		SourcePath:   streamclips.SourceKey(jobID),
		SourceSHA256: sourceHash,
		Title:        request.Title,
		Probe:        request.Probe,
		EditPlan:     append(json.RawMessage(nil), request.PlanJSON...),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	repo := &directStreamRepository{job: job}
	worker := workers.NewStreamRenderWorker(repo, store, workers.StreamRenderWorkerConfig{
		WorkDir:    workDir,
		FFmpegPath: request.FFmpeg,
		Timeout:    request.Timeout,
		MusicDir:   request.MusicDir,
		XAIAPIKey:  os.Getenv("XAI_API_KEY"),
	})
	task, err := tasks.NewRenderStreamClipTask(jobID, request.Plan.Variant)
	if err != nil {
		return streamRenderResult{}, err
	}
	if err := worker.HandleRenderStreamClip(ctx, task); err != nil {
		return streamRenderResult{}, err
	}
	resultKey, err := streamclips.RenderResultKey(jobID, request.Plan.Variant)
	if err != nil {
		return streamRenderResult{}, err
	}
	reader, err := store.Open(resultKey)
	if err != nil {
		return streamRenderResult{}, err
	}
	var workerResult streamclips.RenderResult
	decodeErr := json.NewDecoder(reader).Decode(&workerResult)
	closeErr := reader.Close()
	if decodeErr != nil {
		return streamRenderResult{}, decodeErr
	}
	if closeErr != nil {
		return streamRenderResult{}, closeErr
	}
	if request.CoverGenerator == nil {
		request.CoverGenerator = ffmpegStreamCoverGenerator{}
	}
	return publishLocalStreamResult(ctx, store, job, request, workerResult)
}

type directStreamRepository struct {
	job streamclips.Job
}

func (r *directStreamRepository) Get(_ context.Context, id uuid.UUID) (streamclips.Job, error) {
	if id != r.job.ID {
		return streamclips.Job{}, streamclips.ErrNotFound
	}
	return r.job, nil
}

func (r *directStreamRepository) UpdateStatus(_ context.Context, id uuid.UUID, status streamclips.Status, reason string) error {
	if id != r.job.ID {
		return streamclips.ErrNotFound
	}
	r.job.Status = status
	r.job.FailureReason = reason
	r.job.UpdatedAt = time.Now().UTC()
	return nil
}

func publishLocalStreamResult(ctx context.Context, store *storage.Local, job streamclips.Job, request streamRenderRequest, workerResult streamclips.RenderResult) (streamRenderResult, error) {
	const publishKey = "shortslistosparasubir"
	publishDir, err := store.ResolvePath(publishKey)
	if err != nil {
		return streamRenderResult{}, err
	}
	stagingDir, err := os.MkdirTemp(request.OutDir, ".zv-publish-*")
	if err != nil {
		return streamRenderResult{}, err
	}
	stagingActive := true
	defer func() {
		if stagingActive {
			_ = os.RemoveAll(stagingDir)
		}
	}()
	publishStore, err := storage.NewLocal(stagingDir)
	if err != nil {
		return streamRenderResult{}, err
	}
	videos := make([]streamLocalVideo, 0, len(workerResult.Clips))
	for _, entry := range workerResult.Clips {
		source, err := store.Open(entry.Key)
		if err != nil {
			return streamRenderResult{}, err
		}
		filename := filepath.Base(filepath.FromSlash(entry.Key))
		if err := publishStore.Put(filename, source); err != nil {
			_ = source.Close()
			return streamRenderResult{}, err
		}
		if err := source.Close(); err != nil {
			return streamRenderResult{}, err
		}
		video := streamLocalVideo{
			ClipID:          entry.ClipID,
			Title:           entry.Title,
			Path:            filepath.Join(publishDir, filename),
			DurationSeconds: entry.DurationSeconds,
		}
		if request.FFmpeg != "" && request.CoverGenerator != nil {
			coverFilename := strings.TrimSuffix(filename, filepath.Ext(filename)) + ".cover.jpg"
			coverPath := filepath.Join(stagingDir, coverFilename)
			coverAt := streamCoverTimestamp(request.Plan, entry.ClipID, entry.DurationSeconds)
			coverCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			err := request.CoverGenerator.Generate(coverCtx, request.FFmpeg, filepath.Join(stagingDir, filename), coverPath, coverAt)
			cancel()
			if err != nil {
				return streamRenderResult{}, fmt.Errorf("generate cover for clip %s: %w", entry.ClipID, err)
			}
			video.CoverPath = filepath.Join(publishDir, coverFilename)
		}
		// Video keys gain a _captioned suffix after the burn pass, but
		// NewVideoEntry deliberately keeps ClipID equal to the original plan
		// clip. Caption artifacts are keyed by that stable source clip ID.
		captionKey, err := streamclips.RenderCaptionKey(job.ID, request.Plan.Variant, entry.ClipID)
		if err != nil {
			return streamRenderResult{}, err
		}
		exists, err := store.Exists(captionKey)
		if err != nil {
			return streamRenderResult{}, err
		}
		if exists {
			caption, err := store.Open(captionKey)
			if err != nil {
				return streamRenderResult{}, err
			}
			captionDestinationKey := filepath.ToSlash(filepath.Join("captions", entry.ClipID+".ass"))
			if err := publishStore.Put(captionDestinationKey, caption); err != nil {
				_ = caption.Close()
				return streamRenderResult{}, err
			}
			if err := caption.Close(); err != nil {
				return streamRenderResult{}, err
			}
			video.CaptionsPath = filepath.Join(publishDir, "captions", entry.ClipID+".ass")
		}
		videos = append(videos, video)
	}
	if err := publishStore.Put("index.html", strings.NewReader(renderLocalStreamGallery(job.Title, videos))); err != nil {
		return streamRenderResult{}, err
	}
	galleryPath := filepath.Join(publishDir, "index.html")
	manifestPath := filepath.Join(publishDir, "stream-render-result.json")
	result := streamRenderResult{
		OK:         true,
		DryRun:     false,
		Executed:   true,
		JobID:      job.ID.String(),
		Input:      request.Input,
		Plan:       request.PlanPath,
		Variant:    request.Plan.Variant,
		Probe:      request.Probe,
		OutputDir:  request.OutDir,
		PublishDir: publishDir,
		Manifest:   manifestPath,
		Gallery:    galleryPath,
		Videos:     videos,
		Warnings:   append([]string{}, workerResult.Warnings...),
	}
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return streamRenderResult{}, err
	}
	body = append(body, '\n')
	if err := publishStore.Put("stream-render-result.json", bytes.NewReader(body)); err != nil {
		return streamRenderResult{}, err
	}
	if err := replaceLocalPublishDirectory(stagingDir, publishDir); err != nil {
		return streamRenderResult{}, err
	}
	stagingActive = false
	return result, nil
}

func replaceLocalPublishDirectory(stagingDir, publishDir string) error {
	return replaceLocalPublishDirectoryWithCleanup(stagingDir, publishDir, os.RemoveAll)
}

func replaceLocalPublishDirectoryWithCleanup(stagingDir, publishDir string, cleanup func(string) error) error {
	backupDir := publishDir + ".previous-" + uuid.NewString()
	hadPrevious := false
	if _, err := os.Stat(publishDir); err == nil {
		if err := os.Rename(publishDir, backupDir); err != nil {
			return fmt.Errorf("stage previous publish pack: %w", err)
		}
		hadPrevious = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect previous publish pack: %w", err)
	}
	if err := os.Rename(stagingDir, publishDir); err != nil {
		if hadPrevious {
			if restoreErr := os.Rename(backupDir, publishDir); restoreErr != nil {
				return fmt.Errorf("publish new pack: %w (restore previous pack: %v)", err, restoreErr)
			}
		}
		return fmt.Errorf("publish new pack: %w", err)
	}
	if hadPrevious {
		// Publication is complete once staging has been atomically installed.
		// A transient cleanup failure must not report the successful render as
		// failed or encourage an expensive retry; the uniquely named backup is
		// safe to remove on a later maintenance pass.
		_ = cleanup(backupDir)
	}
	return nil
}

func renderLocalStreamGallery(title string, videos []streamLocalVideo) string {
	if strings.TrimSpace(title) == "" {
		title = "FragForge stream clips"
	}
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width\"><title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title></head><body><h1>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</h1>")
	for _, video := range videos {
		b.WriteString("<section><h2>")
		b.WriteString(html.EscapeString(video.ClipID))
		b.WriteString("</h2>")
		if video.CoverPath != "" {
			b.WriteString("<img alt=\"Cover for ")
			b.WriteString(html.EscapeString(video.ClipID))
			b.WriteString("\" src=\"")
			b.WriteString(html.EscapeString(url.PathEscape(filepath.Base(video.CoverPath))))
			b.WriteString("\">")
		}
		b.WriteString("<video controls preload=\"metadata\" src=\"")
		b.WriteString(html.EscapeString(url.PathEscape(filepath.Base(video.Path))))
		b.WriteString("\"></video></section>")
	}
	b.WriteString("</body></html>")
	return b.String()
}

func parseStreamCrop(value string) (streamclips.CropRect, error) {
	parts := strings.Split(value, ",")
	if len(parts) != 4 {
		return streamclips.CropRect{}, fmt.Errorf("expected x,y,width,height")
	}
	values := make([]float64, 4)
	for i, part := range parts {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return streamclips.CropRect{}, fmt.Errorf("value %d: %w", i+1, err)
		}
		values[i] = parsed
	}
	return streamclips.CropRect{X: values[0], Y: values[1], Width: values[2], Height: values[3]}, nil
}

func sha256File(filePath string) (string, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, f)
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func putLocalFile(filePath string, body []byte) error {
	dir := filepath.Dir(filePath)
	store, err := storage.NewLocal(dir)
	if err != nil {
		return err
	}
	return store.Put(filepath.Base(filePath), bytes.NewReader(body))
}

func writeStreamCommandError(args []string, stdout, stderr io.Writer, err error, commandUsage string) int {
	if streamJSONRequested(args) {
		if writeErr := writeJSON(stdout, streamErrorResult{OK: false, Executed: false, Error: err.Error()}); writeErr != nil {
			fmt.Fprintf(stderr, "error: write stream json error: %v\n", writeErr)
			return exitUnexpected
		}
		return exitInvalidArgs
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	fmt.Fprint(stderr, commandUsage)
	return exitInvalidArgs
}

func writeStreamRuntimeError(args []string, stdout, stderr io.Writer, err error) int {
	if streamJSONRequested(args) {
		if writeErr := writeJSON(stdout, streamErrorResult{OK: false, Executed: false, Error: err.Error()}); writeErr != nil {
			fmt.Fprintf(stderr, "error: write stream json error: %v\n", writeErr)
		}
		return exitUnexpected
	}
	fmt.Fprintf(stderr, "error: %v\n", err)
	return exitUnexpected
}

func streamJSONRequested(args []string) bool {
	for i, arg := range args {
		if arg == "--format=json" {
			return true
		}
		if arg == "--format" && i+1 < len(args) && args[i+1] == "json" {
			return true
		}
	}
	return false
}
