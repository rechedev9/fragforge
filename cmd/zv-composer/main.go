package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rechedev9/fragforge/internal/composition"
	"github.com/rechedev9/fragforge/internal/recording"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var (
		recordingResultPath = flag.String("recording-result", "", "path to recording-result.json")
		outPath             = flag.String("out", "", "final mp4 output path")
		ffmpegPath          = flag.String("ffmpeg", "", "path to ffmpeg.exe; defaults to PATH")
		timeout             = flag.Duration("timeout", 20*time.Minute, "maximum duration for FFmpeg composition")
		dryRun              = flag.Bool("dry-run", false, "write composition-result.json without running FFmpeg")
		format              = flag.String("format", "text", "result summary format: text or json")
	)
	flag.Parse()

	if *recordingResultPath == "" || *outPath == "" {
		return fmt.Errorf("--recording-result and --out are required")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("unsupported format %q", *format)
	}

	absRecordingResult, err := filepath.Abs(*recordingResultPath)
	if err != nil {
		return fmt.Errorf("resolve recording result path: %w", err)
	}
	absOut, err := filepath.Abs(*outPath)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}

	recordingResult, err := readRecordingResult(absRecordingResult)
	if err != nil {
		return err
	}
	clips, warnings, clipErr := recording.ResolveSegmentClips(recordingResult)
	result := composition.Result{
		RecordingResult: absRecordingResult,
		Output:          absOut,
		Clips:           clips,
		Warnings:        warnings,
	}

	resultPath := filepath.Join(filepath.Dir(absOut), "composition-result.json")
	if *dryRun {
		if err := writeResult(resultPath, result); err != nil {
			return err
		}
		return writeCompositionSummary(os.Stdout, *format, result, resultPath, true)
	}
	// A missing segment clip is fatal; duplicate clips are resolved
	// deterministically and recorded as warnings without aborting the render.
	if clipErr != nil {
		result.Error = clipErr.Error()
		_ = writeResult(resultPath, result)
		return clipErr
	}

	ffmpeg := *ffmpegPath
	if ffmpeg == "" {
		ffmpeg = recording.FindFFmpeg()
	}
	ffprobe := recording.FindFFprobe()
	if ffmpeg == "" {
		result.Error = "ffmpeg not found"
		_ = writeResult(resultPath, result)
		return errors.New(result.Error)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	if err := composition.ComposeConcat(ctx, ffmpeg, clips, absOut, filepath.Dir(absOut)); err != nil {
		result.Error = err.Error()
		_ = writeResult(resultPath, result)
		return err
	}
	outputArtifact := recording.RecordingArtifact{
		Role: "final",
		Type: "video",
		Path: absOut,
	}
	if info, err := os.Stat(absOut); err == nil {
		outputArtifact.SizeBytes = info.Size()
	}
	if ffprobe != "" {
		recording.ProbeArtifact(ctx, ffprobe, &outputArtifact)
	}
	result.OutputArtifact = outputArtifact
	result.Warnings = append(result.Warnings, composition.ValidateFinalArtifact(
		outputArtifact,
		recordingResult.Plan.Stream.Width,
		recordingResult.Plan.Stream.Height,
		recordingResult.Plan.Stream.FPS,
		composition.ClipDurationSum(clips),
	)...)
	if err := writeResult(resultPath, result); err != nil {
		return err
	}
	return writeCompositionSummary(os.Stdout, *format, result, resultPath, false)
}

// compositionSummary is the {ok, dry_run, executed} success envelope emitted on
// stdout, mirroring the record and shorts-render stages. The durable
// composition-result.json artifact keeps its own schema.
type compositionSummary struct {
	OK         bool     `json:"ok"`
	DryRun     bool     `json:"dry_run"`
	Executed   bool     `json:"executed"`
	ResultPath string   `json:"result_path"`
	Output     string   `json:"output"`
	ClipCount  int      `json:"clip_count"`
	Warnings   []string `json:"warnings"`
}

func writeCompositionSummary(w io.Writer, format string, result composition.Result, resultPath string, dryRun bool) error {
	summary := compositionSummary{
		OK:         true,
		DryRun:     dryRun,
		Executed:   !dryRun,
		ResultPath: resultPath,
		Output:     result.Output,
		ClipCount:  len(result.Clips),
		Warnings:   append([]string{}, result.Warnings...),
	}
	if format == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		return encoder.Encode(summary)
	}
	fmt.Fprintf(w, "composition_result\t%s\n", summary.ResultPath)
	fmt.Fprintf(w, "output\t%s\n", summary.Output)
	fmt.Fprintf(w, "clips\t%d\n", summary.ClipCount)
	fmt.Fprintf(w, "dry_run\t%t\n", summary.DryRun)
	return nil
}

func readRecordingResult(path string) (recording.RecordingResult, error) {
	// #nosec G304 -- recording result path is an explicit local CLI input.
	b, err := os.ReadFile(path)
	if err != nil {
		return recording.RecordingResult{}, err
	}
	var result recording.RecordingResult
	if err := json.Unmarshal(b, &result); err != nil {
		return recording.RecordingResult{}, err
	}
	return result, nil
}

func writeResult(path string, result composition.Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}
