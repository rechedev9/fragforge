package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/recording"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var (
		killPlanPath = flag.String("killplan", "", "path to kill plan JSON")
		demoPath     = flag.String("demo", "", "path to .dem file")
		outDir       = flag.String("out", "", "recording output directory")
		hlaeExe      = flag.String("hlae", "", "path to HLAE.exe")
		cs2Exe       = flag.String("cs2", "", "path to cs2.exe")
		hudMode      = flag.String("hud", string(recording.HUDModeGameplay), "HUD mode: gameplay, clean, or deathnotices")
		fps          = flag.Int("fps", 0, "recording FPS; defaults to recorder preset")
		videoCRF     = flag.Int("video-crf", 0, "HLAE stream CRF; defaults to recorder preset")
		dryRun       = flag.Bool("dry-run", false, "generate plan and script without launching HLAE")
		timeout      = flag.Duration("timeout", 15*time.Minute, "maximum duration to wait for CS2")
	)
	flag.Parse()

	if *killPlanPath == "" || *demoPath == "" || *outDir == "" {
		return fmt.Errorf("--killplan, --demo, and --out are required")
	}
	if !*dryRun && (*hlaeExe == "" || *cs2Exe == "") {
		return fmt.Errorf("--hlae and --cs2 are required unless --dry-run is set")
	}

	absKillPlanPath, err := filepath.Abs(*killPlanPath)
	if err != nil {
		return fmt.Errorf("resolve killplan path: %w", err)
	}
	absDemoPath, err := filepath.Abs(*demoPath)
	if err != nil {
		return fmt.Errorf("resolve demo path: %w", err)
	}
	absOutDir, err := filepath.Abs(*outDir)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	absHLAEExe := *hlaeExe
	absCS2Exe := *cs2Exe
	if !*dryRun {
		absHLAEExe, err = filepath.Abs(*hlaeExe)
		if err != nil {
			return fmt.Errorf("resolve HLAE path: %w", err)
		}
		absCS2Exe, err = filepath.Abs(*cs2Exe)
		if err != nil {
			return fmt.Errorf("resolve CS2 path: %w", err)
		}
	}

	kp, err := readKillPlan(absKillPlanPath)
	if err != nil {
		return err
	}
	stream := recording.DefaultStreamConfig()
	stream.HUDMode = recording.HUDMode(*hudMode)
	if *fps > 0 {
		stream.FPS = *fps
	}
	if *videoCRF < 0 {
		return fmt.Errorf("--video-crf must be between 1 and 51, or 0 for default")
	}
	if *videoCRF > 0 {
		stream.CRF = *videoCRF
	}
	plan, err := recording.NewPlanFromKillPlan(kp, absDemoPath, absOutDir, stream)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(plan.OutputDir, 0o750); err != nil {
		return err
	}
	script, err := recording.GenerateHLAEJavaScript(plan)
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(plan.OutputDir, "recording.js")
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return err
	}

	result := recording.RecordingResult{
		Plan:   plan,
		Script: scriptPath,
	}

	if *dryRun {
		return writeResult(plan.OutputDir, result)
	}

	ffprobePath := recording.FindFFprobe()
	ffmpegPath := recording.FindFFmpeg()

	if err := validateExecutables(absHLAEExe, absCS2Exe); err != nil {
		result.Error = err.Error()
		_ = writeResult(plan.OutputDir, result)
		return err
	}
	if err := ensureHLAEFFmpegConfig(absHLAEExe); err != nil {
		result.Error = err.Error()
		_ = writeResult(plan.OutputDir, result)
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if err := launchAndWait(ctx, absHLAEExe, absCS2Exe, plan, scriptPath); err != nil {
		result.Error = err.Error()
		// Best-effort diagnostics: launchAndWait commonly fails because ctx hit
		// its deadline, so collect artifacts under a fresh, short-lived context
		// rather than the (likely cancelled) recording context.
		diagCtx, diagCancel := context.WithTimeout(context.Background(), 30*time.Second)
		result.Artifacts = recording.CollectArtifacts(diagCtx, plan, ffprobePath)
		diagCancel()
		result.Warnings = recording.ValidateArtifacts(plan, result.Artifacts)
		_ = writeResult(plan.OutputDir, result)
		return err
	}

	// Post-processing (ffprobe/ffmpeg) runs after recording, so give it its own
	// timeout budget: bounded so a hung subprocess cannot run indefinitely, but
	// not the recording's leftover budget — a capture that legitimately consumed
	// most of --timeout would otherwise starve muxing and drop its segment clips.
	postCtx, postCancel := context.WithTimeout(context.Background(), *timeout)
	defer postCancel()
	result.Artifacts = recording.CollectArtifacts(postCtx, plan, ffprobePath)
	result.Artifacts = append(result.Artifacts, recording.MuxSegmentClips(postCtx, plan, result.Artifacts, ffmpegPath, ffprobePath)...)
	result.Warnings = recording.ValidateArtifacts(plan, result.Artifacts)
	return writeResult(plan.OutputDir, result)
}

func readKillPlan(path string) (killplan.Plan, error) {
	// #nosec G304 -- kill plan path is an explicit local CLI input.
	b, err := os.ReadFile(path)
	if err != nil {
		return killplan.Plan{}, err
	}
	var p killplan.Plan
	if err := json.Unmarshal(b, &p); err != nil {
		return killplan.Plan{}, err
	}
	return p, nil
}

func validateExecutables(hlaeExe, cs2Exe string) error {
	if _, err := os.Stat(hlaeExe); err != nil {
		return fmt.Errorf("HLAE not found: %w", err)
	}
	if _, err := os.Stat(cs2Exe); err != nil {
		return fmt.Errorf("CS2 not found: %w", err)
	}
	if _, err := locateHookDLL(hlaeExe); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		running, err := processRunning("cs2.exe")
		if err != nil {
			return err
		}
		if running {
			return fmt.Errorf("cs2.exe is already running; close it before recording")
		}
	}
	return nil
}

func launchAndWait(ctx context.Context, hlaeExe, cs2Exe string, plan recording.RecordingPlan, scriptPath string) error {
	hook, err := locateHookDLL(hlaeExe)
	if err != nil {
		return err
	}
	cs2CmdLine := cs2LaunchCommandLine(plan, scriptPath)
	// #nosec G204 -- HLAE/CS2 paths are explicit local tool paths and args are not shell-interpolated.
	cmd := exec.CommandContext(ctx, hlaeExe,
		"-customLoader",
		"-noGui",
		"-autoStart",
		"-hookDllPath", hook,
		"-programPath", cs2Exe,
		"-cmdLine", cs2CmdLine,
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start HLAE: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("HLAE launcher failed: %w", err)
	}
	if runtime.GOOS != "windows" {
		return nil
	}
	return waitForWindowsProcessRunAndExit(ctx, "cs2.exe")
}

func cs2LaunchCommandLine(plan recording.RecordingPlan, scriptPath string) string {
	return fmt.Sprintf(`-insecure -condebug -windowed -w %d -h %d +cl_demo_predict 0 +playdemo "%s" +mirv_script_load "%s"`, plan.Stream.Width, plan.Stream.Height, plan.DemoPath, scriptPath)
}

func ensureHLAEFFmpegConfig(hlaeExe string) error {
	dir := filepath.Join(filepath.Dir(hlaeExe), "ffmpeg")
	if _, err := os.Stat(dir); err != nil {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "ffmpeg.exe")); err == nil {
		return nil
	}
	ini := filepath.Join(dir, "ffmpeg.ini")
	if _, err := os.Stat(ini); err == nil {
		return nil
	}
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("HLAE FFmpeg config missing: install ffmpeg under %s or create %s", filepath.Join(dir, "bin"), ini)
	}
	content := fmt.Sprintf("[Ffmpeg]\r\nPath=%s\r\n", ffmpegPath)
	return os.WriteFile(ini, []byte(content), 0o600)
}

func locateHookDLL(hlaeExe string) (string, error) {
	dir := filepath.Dir(hlaeExe)
	candidates := []string{
		filepath.Join(dir, "AfxHookSource2.dll"),
		filepath.Join(dir, "x64", "AfxHookSource2.dll"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("AfxHookSource2.dll not found next to HLAE.exe or under x64")
}

func waitForWindowsProcessRunAndExit(ctx context.Context, image string) error {
	seen := false
	firstDeadline := time.NewTimer(60 * time.Second)
	defer firstDeadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-firstDeadline.C:
			if !seen {
				return fmt.Errorf("%s did not appear within 60 seconds", image)
			}
		case <-ticker.C:
			running, err := processRunning(image)
			if err != nil {
				return err
			}
			if running {
				seen = true
				continue
			}
			if seen {
				return nil
			}
		}
	}
}

func processRunning(image string) (bool, error) {
	if runtime.GOOS != "windows" {
		return false, nil
	}
	// #nosec G204 -- tasklist executable is fixed and image is derived from a local executable path.
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+image, "/FO", "CSV", "/NH").Output()
	if err != nil {
		return false, err
	}
	text := strings.TrimSpace(string(out))
	return strings.Contains(strings.ToLower(text), strings.ToLower(image)), nil
}

func writeResult(outDir string, result recording.RecordingResult) error {
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "recording-result.json"), append(b, '\n'), 0o600)
}
