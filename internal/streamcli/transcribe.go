package streamcli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

const streamTranscriptReviewSchemaVersion = "1.0"

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }

func (f *stringListFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("value cannot be empty")
	}
	*f = append(*f, value)
	return nil
}

type streamTranscribeRequest struct {
	Input      string
	Clip       streamclips.ClipRange
	Models     []string
	VADModel   string
	Language   string
	FFmpeg     string
	WorkDir    string
	Timeout    time.Duration
	SourceHash string
}

type streamTranscriptSegment struct {
	StartSeconds float64 `json:"start_seconds"`
	EndSeconds   float64 `json:"end_seconds"`
	Text         string  `json:"text"`
}

type streamTranscriptPass struct {
	AudioPass   string                    `json:"audio_pass"`
	Model       string                    `json:"model"`
	ModelSHA256 string                    `json:"model_sha256"`
	Segments    []streamTranscriptSegment `json:"segments"`
}

type streamTranscriptReview struct {
	SchemaVersion string                 `json:"schema_version"`
	ReviewStatus  string                 `json:"review_status"`
	ClipID        string                 `json:"clip_id"`
	Language      string                 `json:"language"`
	Input         string                 `json:"input"`
	StartSeconds  float64                `json:"start_seconds"`
	EndSeconds    float64                `json:"end_seconds"`
	Passes        []streamTranscriptPass `json:"passes"`
	Warnings      []string               `json:"warnings"`
	GeneratedAt   time.Time              `json:"generated_at"`
}

type streamTranscribeResult struct {
	OK       bool                   `json:"ok"`
	DryRun   bool                   `json:"dry_run"`
	Executed bool                   `json:"executed"`
	Output   string                 `json:"output"`
	Review   streamTranscriptReview `json:"review"`
}

func runStreamTranscribe(args []string, stdout, stderr io.Writer, service streamService) int {
	if isSingleHelp(args) {
		fmt.Fprint(stdout, streamTranscribeUsage)
		return exitSuccess
	}
	fs := flag.NewFlagSet("stream transcribe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	input := fs.String("input", "", "source stream video")
	planPath := fs.String("plan", "", "stream edit plan")
	clipID := fs.String("clip-id", "", "clip id")
	var models stringListFlag
	fs.Var(&models, "model", "local Whisper GGML model; repeat for consensus")
	vadModel := fs.String("vad-model", "", "local Silero VAD GGML model")
	language := fs.String("language", "es", "transcription language")
	out := fs.String("out", "", "transcript review output")
	ffmpeg := fs.String("ffmpeg", "", "ffmpeg executable")
	ffprobe := fs.String("ffprobe", "", "ffprobe executable")
	workDir := fs.String("work-dir", "", "temporary work directory")
	timeoutText := fs.String("timeout", "10m", "total transcription timeout")
	format := fs.String("format", "text", "text or json")
	dryRun := fs.Bool("dry-run", false, "validate without transcribing or writing")
	if err := fs.Parse(args); err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamTranscribeUsage)
	}
	if fs.NArg() != 0 {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unexpected positional arg %q", fs.Arg(0)), streamTranscribeUsage)
	}
	if *input == "" || *planPath == "" || len(models) == 0 || *vadModel == "" || *out == "" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("--input, --plan, --model, --vad-model, and --out are required"), streamTranscribeUsage)
	}
	if *format != "text" && *format != "json" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("unsupported format %q", *format), streamTranscribeUsage)
	}
	outputInputs := []streamInputPath{
		{flag: "--input", path: *input},
		{flag: "--plan", path: *planPath},
		{flag: "--vad-model", path: *vadModel},
	}
	for _, model := range models {
		outputInputs = append(outputInputs, streamInputPath{flag: "--model", path: model})
	}
	if err := rejectStreamOutputAliases(*out, outputInputs...); err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamTranscribeUsage)
	}
	if !validTranscriptLanguage(strings.TrimSpace(*language)) {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("--language must be auto or a simple language tag such as es or en-US"), streamTranscribeUsage)
	}
	whisperLanguage := normalizeTranscriptLanguage(*language)
	timeout, err := time.ParseDuration(*timeoutText)
	if err != nil || timeout <= 0 {
		if err == nil {
			err = fmt.Errorf("must be positive")
		}
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("invalid --timeout: %w", err), streamTranscribeUsage)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var plan streamclips.EditPlan
	if err := readStrictJSON(*planPath, &plan); err != nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("read stream edit plan: %w", err), streamTranscribeUsage)
	}
	probePath := *ffprobe
	if probePath == "" {
		probePath = recording.FindFFprobe()
	}
	probe, err := service.Probe(ctx, *input, probePath)
	if err != nil {
		return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("probe stream input: %w", err))
	}
	if probe.AudioCodec == "" {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("stream input has no audio track"), streamTranscribeUsage)
	}
	plan = streamclips.NormalizeEditPlan(plan)
	if err := plan.ValidateForSourceDuration(probe.DurationSeconds); err != nil {
		return writeStreamCommandError(args, stdout, stderr, fmt.Errorf("invalid stream edit plan: %w", err), streamTranscribeUsage)
	}
	clip, err := findTranscriptClip(plan, *clipID)
	if err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamTranscribeUsage)
	}

	absInput, _ := filepath.Abs(*input)
	absOut, _ := filepath.Abs(*out)
	absModels := make([]string, 0, len(models))
	for _, model := range models {
		path, pathErr := requireTranscriptFile(model, "Whisper model")
		if pathErr != nil {
			return writeStreamCommandError(args, stdout, stderr, pathErr, streamTranscribeUsage)
		}
		absModels = append(absModels, path)
	}
	absVAD, err := requireTranscriptFile(*vadModel, "VAD model")
	if err != nil {
		return writeStreamCommandError(args, stdout, stderr, err, streamTranscribeUsage)
	}
	ffmpegPath := *ffmpeg
	if ffmpegPath == "" {
		ffmpegPath = recording.FindFFmpeg()
	}
	if err := service.ValidateFFmpeg(ctx, ffmpegPath, true); err != nil {
		return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("validate local transcription ffmpeg: %w", err))
	}

	review := streamTranscriptReview{
		SchemaVersion: streamTranscriptReviewSchemaVersion,
		ReviewStatus:  "requires_review",
		ClipID:        clip.ID,
		Language:      whisperLanguage,
		Input:         absInput,
		StartSeconds:  clip.StartSeconds,
		EndSeconds:    clip.EndSeconds,
		Passes:        []streamTranscriptPass{},
		Warnings: []string{
			"machine transcript candidates are not reviewed caption_words and must not be rendered directly",
			"verify every spoken word and clip-relative timing before running zv stream captions",
		},
		GeneratedAt: time.Now().UTC(),
	}
	if len(absModels) < 2 {
		review.Warnings = append(review.Warnings, "only one Whisper model was supplied; use repeated --model flags for stronger consensus evidence")
	}
	if !*dryRun {
		review, err = service.Transcribe(ctx, streamTranscribeRequest{
			Input: absInput, Clip: clip, Models: absModels, VADModel: absVAD,
			Language: review.Language, FFmpeg: ffmpegPath, WorkDir: *workDir, Timeout: timeout,
		})
		if err != nil {
			return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("transcribe stream clip: %w", err))
		}
		body, marshalErr := json.MarshalIndent(review, "", "  ")
		if marshalErr != nil {
			return writeStreamRuntimeError(args, stdout, stderr, marshalErr)
		}
		if err := putLocalFile(absOut, append(body, '\n')); err != nil {
			return writeStreamRuntimeError(args, stdout, stderr, fmt.Errorf("write transcript review: %w", err))
		}
	}
	result := streamTranscribeResult{OK: true, DryRun: *dryRun, Executed: !*dryRun, Output: absOut, Review: review}
	if *format == "json" {
		if err := writeJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: write stream transcription result: %v\n", err)
			return exitUnexpected
		}
		return exitSuccess
	}
	if *dryRun {
		fmt.Fprintf(stdout, "valid local transcription review: %s (not executed)\n", absOut)
	} else {
		fmt.Fprintf(stdout, "wrote unreviewed local transcription candidates: %s\n", absOut)
	}
	return exitSuccess
}

func findTranscriptClip(plan streamclips.EditPlan, clipID string) (streamclips.ClipRange, error) {
	if clipID == "" && len(plan.Clips) > 0 {
		return plan.Clips[0], nil
	}
	for _, clip := range plan.Clips {
		if clip.ID == clipID {
			return clip, nil
		}
	}
	return streamclips.ClipRange{}, fmt.Errorf("unknown clip_id %q", clipID)
}

func requireTranscriptFile(path, label string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", strings.ToLower(label), err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%s %q is not accessible: %w", label, abs, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s %q is not a regular file", label, abs)
	}
	return abs, nil
}

func (localStreamService) Transcribe(ctx context.Context, request streamTranscribeRequest) (streamTranscriptReview, error) {
	workDir, err := createTranscriptRunDir(request.WorkDir)
	if err != nil {
		return streamTranscriptReview{}, err
	}
	defer os.RemoveAll(workDir)

	duration := request.Clip.EndSeconds - request.Clip.StartSeconds
	rawPath := filepath.Join(workDir, "clip-raw-16k.wav")
	if err := extractTranscriptAudio(ctx, request, rawPath, false); err != nil {
		return streamTranscriptReview{}, err
	}
	audioPasses := []struct {
		name string
		path string
	}{{name: "raw", path: rawPath}}
	warnings := []string{
		"machine transcript candidates are not reviewed caption_words and must not be rendered directly",
		"verify every spoken word and clip-relative timing before running zv stream captions",
	}
	if len(request.Models) < 2 {
		warnings = append(warnings, "only one Whisper model was supplied; use repeated --model flags for stronger consensus evidence")
	}
	enhancedPath := filepath.Join(workDir, "clip-dialogue-enhanced-16k.wav")
	if err := extractTranscriptAudio(ctx, request, enhancedPath, true); err != nil {
		warnings = append(warnings, "dialogue-enhanced pass unavailable: "+err.Error())
	} else {
		audioPasses = append(audioPasses, struct {
			name string
			path string
		}{name: "dialogue_enhanced", path: enhancedPath})
	}

	modelHashes := make(map[string]string, len(request.Models))
	for _, model := range request.Models {
		hash, err := fileSHA256(model)
		if err != nil {
			return streamTranscriptReview{}, fmt.Errorf("hash Whisper model %q: %w", model, err)
		}
		modelHashes[model] = hash
	}
	passes := make([]streamTranscriptPass, 0, len(audioPasses)*len(request.Models))
	for _, audio := range audioPasses {
		for modelIndex, model := range request.Models {
			srtPath := filepath.Join(workDir, fmt.Sprintf("transcript-%s-%d.srt", audio.name, modelIndex+1))
			segments, err := runLocalWhisper(ctx, request.FFmpeg, audio.path, model, request.VADModel, request.Language, duration, srtPath)
			if err != nil {
				return streamTranscriptReview{}, fmt.Errorf("%s pass with model %q: %w", audio.name, model, err)
			}
			passes = append(passes, streamTranscriptPass{
				AudioPass: audio.name, Model: filepath.Base(model), ModelSHA256: modelHashes[model], Segments: segments,
			})
		}
	}
	return streamTranscriptReview{
		SchemaVersion: streamTranscriptReviewSchemaVersion,
		ReviewStatus:  "requires_review",
		ClipID:        request.Clip.ID,
		Language:      request.Language,
		Input:         request.Input,
		StartSeconds:  request.Clip.StartSeconds,
		EndSeconds:    request.Clip.EndSeconds,
		Passes:        passes,
		Warnings:      warnings,
		GeneratedAt:   time.Now().UTC(),
	}, nil
}

func createTranscriptRunDir(baseDir string) (string, error) {
	if baseDir != "" {
		if err := os.MkdirAll(baseDir, 0o755); err != nil {
			return "", fmt.Errorf("create transcription work directory: %w", err)
		}
	}
	workDir, err := os.MkdirTemp(baseDir, "fragforge-stream-transcribe-")
	if err != nil {
		return "", fmt.Errorf("create transcription run directory: %w", err)
	}
	return workDir, nil
}

func extractTranscriptAudio(ctx context.Context, request streamTranscribeRequest, output string, enhanced bool) error {
	duration := request.Clip.EndSeconds - request.Clip.StartSeconds
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y", "-nostdin",
		"-ss", strconv.FormatFloat(request.Clip.StartSeconds, 'f', 3, 64),
		"-t", strconv.FormatFloat(duration, 'f', 3, 64),
		"-i", request.Input, "-vn", "-map", "0:a:0",
	}
	if enhanced {
		args = append(args, "-af", "dialoguenhance=original=0:enhance=3:voice=2,pan=mono|c0=FC,highpass=f=100,lowpass=f=7600,speechnorm=e=6.25:c=2:r=0.0001:f=0.001")
	} else {
		args = append(args, "-ac", "1")
	}
	args = append(args, "-ar", "16000", "-c:a", "pcm_s16le", output)
	if err := runTranscriptCommand(ctx, request.FFmpeg, args...); err != nil {
		pass := "raw"
		if enhanced {
			pass = "dialogue-enhanced"
		}
		return fmt.Errorf("extract %s transcription audio: %w", pass, err)
	}
	return nil
}

func runLocalWhisper(ctx context.Context, ffmpegPath, audioPath, modelPath, vadPath, language string, duration float64, output string) ([]streamTranscriptSegment, error) {
	queue := 30.0
	if duration < queue {
		queue = duration + 0.5
	}
	if queue < 3 {
		queue = 3
	}
	filter := strings.Join([]string{
		"whisper=model=" + escapeWhisperPath(modelPath),
		"language=" + normalizeTranscriptLanguage(language),
		"destination=" + escapeWhisperPath(output),
		"format=srt",
		"queue=" + strconv.FormatFloat(queue, 'f', 3, 64),
		"vad_model=" + escapeWhisperPath(vadPath),
		"vad_threshold=0.65",
		"vad_min_speech_duration=0.10",
		"vad_min_silence_duration=0.50",
	}, ":")
	if err := runTranscriptCommand(ctx, ffmpegPath,
		"-hide_banner", "-loglevel", "error", "-y", "-nostdin", "-i", audioPath,
		"-af", filter, "-f", "null", os.DevNull,
	); err != nil {
		return nil, fmt.Errorf("run local Whisper: %w", err)
	}
	return parseWhisperSRT(output, duration)
}

func escapeWhisperPath(path string) string {
	return escapeWhisperPathForOS(path, runtime.GOOS)
}

func escapeWhisperPathForOS(path, goos string) string {
	if goos == "windows" {
		path = strings.ReplaceAll(path, `\`, "/")
	}
	optionValue := strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		`:`, `\:`,
	).Replace(path)
	return strings.NewReplacer(
		`\`, `\\`,
		`'`, `\'`,
		`[`, `\[`,
		`]`, `\]`,
		`,`, `\,`,
		`;`, `\;`,
	).Replace(optionValue)
}

func validTranscriptLanguage(language string) bool {
	if language == "auto" {
		return true
	}
	if len(language) < 2 || len(language) > 16 {
		return false
	}
	for i, r := range language {
		if unicode.IsLetter(r) {
			continue
		}
		if r == '-' && i > 0 && i < len(language)-1 {
			continue
		}
		return false
	}
	return true
}

func normalizeTranscriptLanguage(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "auto" {
		return language
	}
	base, _, _ := strings.Cut(language, "-")
	return base
}

func runTranscriptCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.TrimSpace(string(output))
	if len(message) > 4000 {
		message = message[len(message)-4000:]
	}
	if message == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, message)
}

func parseWhisperSRT(path string, duration float64) ([]streamTranscriptSegment, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Whisper output: %w", err)
	}
	defer file.Close()
	segments := []streamTranscriptSegment{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		startText, endText, found := strings.Cut(line, " --> ")
		if !found {
			continue
		}
		start, err := parseWhisperSRTTimestamp(startText)
		if err != nil {
			return nil, fmt.Errorf("decode Whisper start timestamp on line %d: %w", lineNumber, err)
		}
		end, err := parseWhisperSRTTimestamp(endText)
		if err != nil {
			return nil, fmt.Errorf("decode Whisper end timestamp on line %d: %w", lineNumber, err)
		}
		var textLines []string
		for scanner.Scan() {
			lineNumber++
			textLine := strings.TrimSuffix(scanner.Text(), "\r")
			if strings.TrimSpace(textLine) == "" {
				break
			}
			textLines = append(textLines, strings.TrimSpace(textLine))
		}
		text := strings.TrimSpace(strings.Join(textLines, " "))
		if start < 0 || end <= start || !hasTranscriptText(text) {
			continue
		}
		if end > duration {
			end = duration
		}
		if start >= end {
			continue
		}
		segments = append(segments, streamTranscriptSegment{StartSeconds: start, EndSeconds: end, Text: text})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read Whisper output: %w", err)
	}
	return segments, nil
}

func parseWhisperSRTTimestamp(value string) (float64, error) {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(value), ",", "."), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}
	hours, err := strconv.Atoi(parts[0])
	if err != nil || hours < 0 {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil || minutes < 0 || minutes >= 60 {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}
	seconds, err := strconv.ParseFloat(parts[2], 64)
	if err != nil || seconds < 0 || seconds >= 60 {
		return 0, fmt.Errorf("invalid timestamp %q", value)
	}
	return float64(hours*3600+minutes*60) + seconds, nil
}

func hasTranscriptText(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
