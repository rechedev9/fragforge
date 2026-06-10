package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rechedev9/fragforge/internal/pipeline"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var (
		killPlanPath   = flag.String("killplan", "", "path to kill plan JSON")
		demoPath       = flag.String("demo", "", "path to .dem file")
		outDir         = flag.String("out", "", "pipeline output directory")
		hlaeExe        = flag.String("hlae", "", "path to HLAE.exe")
		cs2Exe         = flag.String("cs2", "", "path to cs2.exe")
		recorderExe    = flag.String("recorder", "", "path to zv-recorder")
		composerExe    = flag.String("composer", "", "path to zv-composer")
		ffmpegExe      = flag.String("ffmpeg", "", "path to ffmpeg.exe passed to composer")
		recordTimeout  = flag.Duration("record-timeout", 20*time.Minute, "maximum recorder duration")
		composeTimeout = flag.Duration("compose-timeout", 20*time.Minute, "maximum composer duration")
	)
	flag.Parse()

	cfg := pipeline.Config{
		KillPlanPath:   mustAbs("killplan", *killPlanPath),
		DemoPath:       mustAbs("demo", *demoPath),
		OutputDir:      mustAbs("out", *outDir),
		HLAEPath:       mustAbs("hlae", *hlaeExe),
		CS2Path:        mustAbs("cs2", *cs2Exe),
		RecorderPath:   resolveCommand("zv-recorder", *recorderExe),
		ComposerPath:   resolveCommand("zv-composer", *composerExe),
		FFmpegPath:     optionalAbs(*ffmpegExe),
		RecordTimeout:  recordTimeout.String(),
		ComposeTimeout: composeTimeout.String(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result, err := pipeline.Run(ctx, cfg)
	resultPath := filepath.Join(cfg.OutputDir, "pipeline-result.json")
	if writeErr := pipeline.WriteResult(resultPath, result); writeErr != nil && err == nil {
		err = writeErr
	}
	if err != nil {
		return err
	}
	fmt.Printf("final: %s\n", result.FinalOutput)
	fmt.Printf("result: %s\n", resultPath)
	return nil
}

func mustAbs(name, path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		log.Fatalf("resolve %s path: %v", name, err)
	}
	return abs
}

func optionalAbs(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		log.Fatalf("resolve optional path: %v", err)
	}
	return abs
}

func resolveCommand(name, explicit string) string {
	if explicit != "" {
		return mustAbs(name, explicit)
	}
	if current, err := os.Executable(); err == nil {
		for _, candidateName := range executableNames(name) {
			candidate := filepath.Join(filepath.Dir(current), candidateName)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	if found, err := exec.LookPath(name); err == nil {
		return found
	}
	return ""
}

func executableNames(name string) []string {
	if runtime.GOOS == "windows" {
		return []string{name + ".exe", name}
	}
	return []string{name}
}
