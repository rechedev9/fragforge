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
	"regexp"
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
		fake         = flag.Bool("fake", false, "generate placeholder segment clips instead of launching HLAE/CS2 (e2e/CI)")
		timeout      = flag.Duration("timeout", 15*time.Minute, "maximum duration to wait for CS2")
	)
	flag.Parse()

	// Fake mode also engages via the environment so the orchestrator's record
	// worker (which builds the CLI args itself) can run a real end-to-end pipeline
	// without HLAE/CS2 by setting ZV_RECORDER_FAKE=1.
	fakeMode := *fake || os.Getenv("ZV_RECORDER_FAKE") == "1"

	if *killPlanPath == "" || *demoPath == "" || *outDir == "" {
		return fmt.Errorf("--killplan, --demo, and --out are required")
	}
	if !*dryRun && !fakeMode && (*hlaeExe == "" || *cs2Exe == "") {
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
	if !*dryRun && !fakeMode {
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

	if fakeMode {
		fakeCtx, cancelFake := context.WithTimeout(context.Background(), *timeout)
		defer cancelFake()
		artifacts, err := generateFakeSegments(fakeCtx, plan)
		if err != nil {
			result.Error = err.Error()
			_ = writeResult(plan.OutputDir, result)
			return err
		}
		result.Artifacts = artifacts
		result.Warnings = recording.ValidateArtifacts(plan, result.Artifacts)
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

	// CS2 must record in a real window: the player's own video settings
	// (fullscreen / borderless) override the -windowed launch flag and turn the
	// capture into a borderless topmost screen-sized window that hijacks the
	// desktop and glitches more than a plain window. Patch the saved settings
	// for the run and put the originals back afterwards. validateExecutables
	// already guaranteed cs2.exe is not running, so the file is safe to edit.
	restoreVideoConfig := forceWindowedVideoConfig(absCS2Exe)
	defer restoreVideoConfig()

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

// generateFakeSegments produces one placeholder mp4 per plan segment (at the
// recording resolution, with a silent-ish tone) so the downstream compose/render
// pipeline can run end-to-end without launching HLAE/CS2. Gated behind --fake /
// ZV_RECORDER_FAKE and intended for local e2e and CI only.
func generateFakeSegments(ctx context.Context, plan recording.RecordingPlan) ([]recording.RecordingArtifact, error) {
	ffmpeg := recording.FindFFmpeg()
	if ffmpeg == "" {
		return nil, fmt.Errorf("ffmpeg not found (required for fake recording)")
	}
	segDir := filepath.Join(plan.OutputDir, "segments")
	if err := os.MkdirAll(segDir, 0o750); err != nil {
		return nil, err
	}
	const fakeDurationSec = 5
	width, height, fps := plan.Stream.Width, plan.Stream.Height, plan.Stream.FPS
	if width <= 0 || height <= 0 {
		width, height = 1920, 1080
	}
	if fps <= 0 {
		fps = 60
	}
	out := make([]recording.RecordingArtifact, 0, len(plan.Segments))
	for _, seg := range plan.Segments {
		clip := filepath.Join(segDir, seg.ID+".mp4")
		// #nosec G204 -- ffmpeg path is discovered locally; args are not shell-interpolated.
		cmd := exec.CommandContext(ctx, ffmpeg, "-y",
			"-f", "lavfi", "-i", fmt.Sprintf("testsrc=size=%dx%d:rate=%d:duration=%d", width, height, fps, fakeDurationSec),
			"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=220:duration=%d", fakeDurationSec),
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-preset", "ultrafast",
			"-c:a", "aac", "-shortest", clip,
		)
		if combined, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("ffmpeg fake clip %q: %w: %q", seg.ID, err, strings.TrimSpace(string(combined)))
		}
		info, err := os.Stat(clip)
		if err != nil {
			return nil, err
		}
		out = append(out, recording.RecordingArtifact{
			SegmentID:       seg.ID,
			Role:            "segment",
			Type:            "video",
			Path:            clip,
			SizeBytes:       info.Size(),
			DurationSeconds: fakeDurationSec,
			FrameRate:       fmt.Sprintf("%d/1", fps),
			Codec:           "h264",
			Width:           width,
			Height:          height,
		})
	}
	return out, nil
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

// windowedVideoSettings are the CS2 saved video settings that must be off for
// the -windowed launch flag to yield a real bordered window instead of a
// borderless topmost screen-sized one.
var windowedVideoSettings = []string{
	"setting.fullscreen",
	"setting.coop_fullscreen",
	"setting.nowindowborder",
}

// forceWindowedVideoConfig patches every Steam cs2_video.txt so the capture
// runs in a real window, returning a restore func that puts the original
// bytes back after the run (a hard-killed recorder leaves the settings
// windowed, which the next capture re-patches harmlessly). Failures only log:
// a missing config means CS2 already follows the launch flags.
func forceWindowedVideoConfig(cs2Exe string) func() {
	originals := map[string][]byte{}
	for _, path := range cs2VideoConfigPaths(cs2Exe) {
		// #nosec G304 -- paths are discovered under the local Steam install.
		b, err := os.ReadFile(path)
		if err != nil {
			log.Printf("windowed capture: read %s: %v", path, err)
			continue
		}
		patched, changed := patchWindowedVideoSettings(string(b))
		if !changed {
			continue
		}
		if err := os.WriteFile(path, []byte(patched), 0o600); err != nil {
			log.Printf("windowed capture: patch %s: %v", path, err)
			continue
		}
		log.Printf("windowed capture: patched %s (fullscreen/borderless off for this run)", path)
		originals[path] = b
	}
	return func() {
		for path, b := range originals {
			if err := os.WriteFile(path, b, 0o600); err != nil {
				log.Printf("windowed capture: restore %s: %v", path, err)
			}
		}
	}
}

// patchWindowedVideoSettings forces the fullscreen/borderless settings to "0"
// in a cs2_video.txt body, reporting whether anything changed. Settings that
// are absent are left absent; CS2 then follows the launch flags.
func patchWindowedVideoSettings(content string) (string, bool) {
	changed := false
	for _, key := range windowedVideoSettings {
		pattern := regexp.MustCompile(`("` + regexp.QuoteMeta(key) + `"\s+")([^"]*)(")`)
		next := pattern.ReplaceAllStringFunc(content, func(match string) string {
			groups := pattern.FindStringSubmatch(match)
			if groups[2] == "0" {
				return match
			}
			changed = true
			return groups[1] + "0" + groups[3]
		})
		content = next
	}
	return content, changed
}

// cs2VideoConfigPaths finds every cs2_video.txt under the Steam userdata
// roots: the install that owns cs2.exe (walking up from the executable) plus
// the default Steam location, since cs2 may live in a secondary library while
// userdata stays in the main install.
func cs2VideoConfigPaths(cs2Exe string) []string {
	var roots []string
	if dir := steamRootFromCS2Path(cs2Exe); dir != "" {
		roots = append(roots, dir)
	}
	if pf := os.Getenv("ProgramFiles(x86)"); pf != "" {
		roots = append(roots, filepath.Join(pf, "Steam"))
	}
	roots = append(roots, `C:\Program Files (x86)\Steam`)

	seen := map[string]bool{}
	var paths []string
	for _, root := range roots {
		matches, _ := filepath.Glob(filepath.Join(root, "userdata", "*", "730", "local", "cfg", "cs2_video.txt"))
		for _, match := range matches {
			if seen[match] {
				continue
			}
			seen[match] = true
			paths = append(paths, match)
		}
	}
	return paths
}

// steamRootFromCS2Path walks up from cs2.exe to the directory containing
// steamapps, i.e. the Steam (library) root. Returns "" when cs2.exe does not
// live under a steamapps tree.
func steamRootFromCS2Path(cs2Exe string) string {
	dir := filepath.Dir(cs2Exe)
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		if strings.EqualFold(filepath.Base(dir), "steamapps") {
			return parent
		}
		dir = parent
	}
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
