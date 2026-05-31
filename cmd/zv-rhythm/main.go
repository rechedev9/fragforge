package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/reche/zackvideo/internal/rhythm"
)

func main() {
	if err := run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func run(argv []string) error {
	if len(argv) < 2 {
		return fmt.Errorf("usage: zv-rhythm analyze --input <audio-or-video> --out <rhythm.json>")
	}
	switch argv[1] {
	case "analyze":
		return runAnalyze(argv[2:])
	case "-h", "--help", "help":
		fmt.Println("usage: zv-rhythm analyze --input <audio-or-video> --out <rhythm.json>")
		return nil
	default:
		return fmt.Errorf("unknown command %q", argv[1])
	}
}

func runAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	input := fs.String("input", "", "audio or video file to analyze")
	out := fs.String("out", "", "rhythm analysis JSON output path")
	killPlan := fs.String("killplan", "", "optional kill plan JSON to build segment-to-beat sync suggestions")
	ffmpegPath := fs.String("ffmpeg", "", "path to ffmpeg.exe; defaults to PATH")
	sampleRate := fs.Int("sample-rate", 22050, "analysis sample rate")
	minBPM := fs.Float64("min-bpm", 85, "minimum expected BPM")
	maxBPM := fs.Float64("max-bpm", 125, "maximum expected BPM")
	killOffsetMS := fs.Int("kill-offset-ms", 100, "target delay after beat for kill impact")
	maxBeats := fs.Int("max-beats", 256, "maximum beat timestamps to emit")
	maxOnsets := fs.Int("max-onsets", 32, "maximum strong onset timestamps to emit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *input == "" || *out == "" {
		return fmt.Errorf("--input and --out are required")
	}
	analysis, err := rhythm.AnalyzeFile(context.Background(), rhythm.Config{
		InputPath:         *input,
		KillPlanPath:      *killPlan,
		FFmpegPath:        *ffmpegPath,
		SampleRate:        *sampleRate,
		MinBPM:            *minBPM,
		MaxBPM:            *maxBPM,
		KillOffsetSeconds: float64(*killOffsetMS) / 1000,
		MaxBeats:          *maxBeats,
		MaxOnsets:         *maxOnsets,
	})
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return fmt.Errorf("encode rhythm analysis: %w", err)
	}
	if err := os.WriteFile(*out, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write rhythm analysis: %w", err)
	}
	fmt.Printf("rhythm: %.2f BPM, %d beats, %d onsets", analysis.EstimatedBPM, len(analysis.BeatTimesSeconds), len(analysis.StrongOnsets))
	if len(analysis.SegmentSync) > 0 {
		fmt.Printf(", %d segment sync entries", len(analysis.SegmentSync))
	}
	fmt.Printf("\nwritten %s\n", *out)
	return nil
}
