package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
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
	err := run()
	if err == nil {
		return
	}
	// Plain stderr, no log timestamps: the zv wrapper forwards this text as the
	// error field of its JSON envelope.
	var hookErr *hookIncompatibleError
	if errors.As(err, &hookErr) {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitHookIncompatible)
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

func run() error {
	var (
		killPlanPath         = flag.String("killplan", "", "path to kill plan JSON")
		demoPath             = flag.String("demo", "", "path to .dem file")
		outDir               = flag.String("out", "", "recording output directory")
		hlaeExe              = flag.String("hlae", "", "path to HLAE.exe")
		cs2Exe               = flag.String("cs2", "", "path to cs2.exe")
		hudMode              = flag.String("hud", string(recording.HUDModeGameplay), "HUD mode: gameplay, clean, or deathnotices")
		portraitSafeKillfeed = flag.Bool("portrait-safe-killfeed", false, "move filtered death notices into the 9:16 center-crop safe area")
		fps                  = flag.Int("fps", 0, "recording FPS; defaults to recorder preset")
		videoCRF             = flag.Int("video-crf", 0, "HLAE stream CRF; defaults to recorder preset")
		dryRun               = flag.Bool("dry-run", false, "generate plan and script without launching HLAE")
		format               = flag.String("format", "text", "result summary format: text or json")
		fake                 = flag.Bool("fake", false, "generate placeholder segment clips instead of launching HLAE/CS2 (e2e/CI)")
		timeout              = flag.Duration("timeout", 15*time.Minute, "maximum duration to wait for CS2")
	)
	flag.Parse()

	// Fake mode also engages via the environment so the orchestrator's record
	// worker (which builds the CLI args itself) can run a real end-to-end pipeline
	// without HLAE/CS2 by setting ZV_RECORDER_FAKE=1.
	fakeMode := *fake || os.Getenv("ZV_RECORDER_FAKE") == "1"

	if *killPlanPath == "" || *demoPath == "" || *outDir == "" {
		return fmt.Errorf("--killplan, --demo, and --out are required")
	}
	if *format != "text" && *format != "json" {
		return fmt.Errorf("unsupported format %q", *format)
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
	stream.PortraitSafeKillfeed = *portraitSafeKillfeed
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
		return writeResultAndReport(plan.OutputDir, result, true, *format, os.Stdout)
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
		return writeResultAndReport(plan.OutputDir, result, false, *format, os.Stdout)
	}

	ffprobePath := recording.FindFFprobe()
	ffmpegPath := recording.FindFFmpeg()

	if err := validateExecutables(absHLAEExe, absCS2Exe); err != nil {
		result.Error = err.Error()
		_ = writeResult(plan.OutputDir, result)
		return err
	}
	if err := ensureDefaultAvatar(absCS2Exe); err != nil {
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

	// Publish each segment clip while HLAE is still recording so observers (the
	// orchestrator's capture-progress poll) see segments as they finish instead
	// of only after the whole run. Owned by this run: cancelled and waited for
	// as soon as the capture process exits, and strictly best-effort — the
	// post-run MuxSegmentClips pass below re-muxes anything still missing.
	muxCtx, stopMux := context.WithCancel(ctx)
	muxDone := make(chan struct{})
	go func() {
		defer close(muxDone)
		muxer := recording.NewIncrementalMuxer(plan, ffmpegPath)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-muxCtx.Done():
				return
			case <-ticker.C:
				for _, id := range muxer.MuxFinished(muxCtx) {
					log.Printf("segment %s recorded", id)
				}
			}
		}
	}()
	stopIncrementalMux := func() {
		stopMux()
		<-muxDone
	}

	if err := launchAndWait(ctx, absHLAEExe, absCS2Exe, plan, scriptPath); err != nil {
		stopIncrementalMux()
		result.Error = err.Error()
		// Preserve completed takes before returning the capture failure. Avoid
		// probing partial files here: a single stuck ffprobe must not consume the
		// fresh recovery budget before FFmpeg can publish usable segment clips.
		result.Artifacts = recording.CollectArtifacts(context.Background(), plan, "")
		recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 30*time.Second)
		result.Artifacts = append(result.Artifacts, recording.MuxSegmentClips(recoveryCtx, plan, result.Artifacts, ffmpegPath, "")...)
		recoveryCancel()
		result.Warnings = recording.ValidateArtifacts(plan, result.Artifacts)
		_ = writeResult(plan.OutputDir, result)
		return err
	}
	stopIncrementalMux()

	// Post-processing (ffprobe/ffmpeg) runs after recording, so give it its own
	// timeout budget: bounded so a hung subprocess cannot run indefinitely, but
	// not the recording's leftover budget — a capture that legitimately consumed
	// most of --timeout would otherwise starve muxing and drop its segment clips.
	postCtx, postCancel := context.WithTimeout(context.Background(), *timeout)
	defer postCancel()
	result.Artifacts = recording.CollectArtifacts(postCtx, plan, ffprobePath)
	result.Artifacts = append(result.Artifacts, recording.MuxSegmentClips(postCtx, plan, result.Artifacts, ffmpegPath, ffprobePath)...)
	result.Warnings = recording.ValidateArtifacts(plan, result.Artifacts)
	if err := validateCaptureResult(result, absCS2Exe); err != nil {
		result.Error = err.Error()
		_ = writeResult(plan.OutputDir, result)
		return err
	}
	return writeResultAndReport(plan.OutputDir, result, false, *format, os.Stdout)
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

func ensureDefaultAvatar(cs2Exe string) error {
	gameDir := filepath.Clean(filepath.Join(filepath.Dir(cs2Exe), "..", ".."))
	avatarPath := filepath.Join(gameDir, "csgo", "avatars", "default.png")
	if _, err := os.Stat(avatarPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect default CS2 avatar: %w", err)
	}

	avatarDir := filepath.Dir(avatarPath)
	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		return fmt.Errorf("create default CS2 avatar directory: %w", err)
	}

	file, err := os.CreateTemp(avatarDir, "default-*.png")
	if err != nil {
		return fmt.Errorf("create default CS2 avatar: %w", err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)

	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: color.NRGBA{R: 64, G: 72, B: 88, A: 255}}, image.Point{}, draw.Src)
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		return fmt.Errorf("encode default CS2 avatar: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close default CS2 avatar: %w", err)
	}
	if err := os.Rename(tempPath, avatarPath); err != nil {
		return fmt.Errorf("install default CS2 avatar: %w", err)
	}
	log.Printf("installed missing CS2 default avatar at %s", avatarPath)
	return nil
}

func launchAndWait(ctx context.Context, hlaeExe, cs2Exe string, plan recording.RecordingPlan, scriptPath string) error {
	hook, err := locateHookDLL(hlaeExe)
	if err != nil {
		return err
	}
	consoleLogPath := cs2ConsoleLogPath(cs2Exe)
	if err := prepareCS2ConsoleLog(consoleLogPath); err != nil {
		return err
	}
	consoleLog := newCS2ConsoleLogMonitor(consoleLogPath)
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
	return waitForWindowsProcessRunAndExit(ctx, "cs2.exe", consoleLog)
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

func cs2ConsoleLogPath(cs2Exe string) string {
	gameDir := filepath.Dir(filepath.Dir(filepath.Dir(cs2Exe)))
	return filepath.Join(gameDir, "csgo", "console.log")
}

const demoParseFailureMarker = "NETWORK_DISCONNECT_MESSAGE_PARSE_ERROR"

type cs2ConsoleLogMonitor struct {
	path   string
	offset int64
	tail   string
}

func prepareCS2ConsoleLog(path string) error {
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return fmt.Errorf("reset cs2 console log %q: %w", path, err)
	}
	return nil
}

func newCS2ConsoleLogMonitor(path string) *cs2ConsoleLogMonitor {
	monitor := &cs2ConsoleLogMonitor{path: path}
	if info, err := os.Stat(path); err == nil {
		monitor.offset = info.Size()
	}
	return monitor
}

// failure checks only console output written after this monitor was created.
// CS2 can truncate console.log at startup, so a smaller file resets the cursor.
func (m *cs2ConsoleLogMonitor) failure() error {
	file, err := os.Open(m.path)
	if err != nil {
		return nil
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil
	}
	if info.Size() < m.offset {
		m.offset = 0
		m.tail = ""
	}
	if _, err := file.Seek(m.offset, io.SeekStart); err != nil {
		return nil
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil
	}
	m.offset += int64(len(data))
	if len(data) == 0 {
		return nil
	}

	content := m.tail + string(data)
	if strings.Contains(content, demoParseFailureMarker) {
		return &demoParseError{path: m.path}
	}
	keep := len(demoParseFailureMarker) - 1
	if len(content) > keep {
		m.tail = content[len(content)-keep:]
	} else {
		m.tail = content
	}
	return nil
}

type demoParseError struct {
	path string
}

func (e *demoParseError) Error() string {
	return fmt.Sprintf("cs2 demo playback failed with %s; check console log %q", demoParseFailureMarker, e.path)
}

func validateCaptureResult(result recording.RecordingResult, cs2Exe string) error {
	if err := recording.ValidateUploadResult(result); err != nil {
		return fmt.Errorf("%w; check HLAE capture output and CS2 console log %q", err, cs2ConsoleLogPath(cs2Exe))
	}
	return nil
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

func waitForWindowsProcessRunAndExit(ctx context.Context, image string, consoleLog *cs2ConsoleLogMonitor) error {
	return waitForWindowsProcessRunAndExitWith(
		ctx,
		image,
		60*time.Second,
		500*time.Millisecond,
		func(image string) (bool, string, error) {
			running, title, err := tasklistWindowTitle(image)
			if err != nil {
				return running, title, err
			}
			if consoleLog != nil {
				if err := consoleLog.failure(); err != nil {
					return running, title, err
				}
			}
			return running, title, nil
		},
		terminateWindowsProcess,
	)
}

func waitForWindowsProcessRunAndExitWith(
	ctx context.Context,
	image string,
	firstWait time.Duration,
	pollInterval time.Duration,
	status func(string) (bool, string, error),
	terminate func(string) error,
) error {
	seen := false
	firstDeadline := time.NewTimer(firstWait)
	defer firstDeadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return stopProcessAfterWaitFailure(image, ctx.Err(), seen, terminate)
		case <-firstDeadline.C:
			if !seen {
				running, title, err := status(image)
				if running {
					seen = true
				}
				if err != nil {
					return stopProcessAfterWaitFailure(image, err, shouldTerminateAfterStatusFailure(err, seen), terminate)
				}
				if isHookErrorWindowTitle(title) {
					return stopProcessAfterWaitFailure(image, &hookIncompatibleError{windowTitle: title}, true, terminate)
				}
				if !running {
					return fmt.Errorf("%s did not appear within %s", image, firstWait)
				}
			}
		case <-ticker.C:
			running, title, err := status(image)
			if running {
				seen = true
			}
			if err != nil {
				return stopProcessAfterWaitFailure(image, err, shouldTerminateAfterStatusFailure(err, seen), terminate)
			}
			if isHookErrorWindowTitle(title) {
				return stopProcessAfterWaitFailure(image, &hookIncompatibleError{windowTitle: title}, true, terminate)
			}
			if running {
				continue
			}
			if seen {
				return nil
			}
		}
	}
}

func shouldTerminateAfterStatusFailure(err error, processSeen bool) bool {
	var parseErr *demoParseError
	return processSeen || errors.As(err, &parseErr)
}

func stopProcessAfterWaitFailure(image string, cause error, processMayBeRunning bool, terminate func(string) error) error {
	if !processMayBeRunning {
		return cause
	}
	if err := terminate(image); err != nil {
		return fmt.Errorf("%w; stop %s after capture failure: %v", cause, image, err)
	}
	return cause
}

func terminateWindowsProcess(image string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	// #nosec G204 -- taskkill is fixed and image is the recorder-owned CS2 executable name.
	out, err := exec.Command("taskkill", "/IM", image, "/T", "/F").CombinedOutput()
	if err == nil {
		return nil
	}
	running, stateErr := processRunning(image)
	if stateErr == nil && !running {
		return nil
	}
	detail := strings.TrimSpace(string(out))
	if detail == "" {
		return fmt.Errorf("taskkill %s: %w", image, err)
	}
	return fmt.Errorf("taskkill %s: %w: %s", image, err, detail)
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

// exitHookIncompatible is the process exit code used when HLAE's injected
// hook crashes with a native "Error - Afx*" dialog instead of capturing.
// Keep this in sync with the zv-recorder case in cmd/zv/obs_record.go's
// shortStageClass, which maps this code to the "capture_incompatible"
// observability class.
const exitHookIncompatible = 6

// hookErrorWindowTitlePattern matches the native MessageBox titles
// advancedfx's hook modules (AfxHookSource2, AfxHookSource, ...) use when a
// memory signature scan fails to resolve an address in the current game
// binary — almost always caused by a CS2 update landing after the installed
// HLAE build was released.
var hookErrorWindowTitlePattern = regexp.MustCompile(`^Error - Afx`)

// isHookErrorWindowTitle reports whether title is a native HLAE hook crash
// dialog, e.g. "Error - AfxHookSource2".
func isHookErrorWindowTitle(title string) bool {
	return hookErrorWindowTitlePattern.MatchString(title)
}

// hookIncompatibleError reports that HLAE's injected hook crashed with a
// native error dialog instead of capturing. In practice this means the
// installed HLAE/AfxHookSource2 build does not match the currently installed
// CS2 version.
type hookIncompatibleError struct {
	windowTitle string
}

func (e *hookIncompatibleError) Error() string {
	return fmt.Sprintf(
		"HLAE hook crashed with a native error dialog (%q) instead of capturing: "+
			"the installed HLAE/AfxHookSource2 build is likely incompatible with the current CS2 version "+
			"(CS2 updates regularly break AfxHookSource2's signature scan until advancedfx ships a new build); "+
			"check https://github.com/advancedfx/advancedfx/releases for a newer HLAE build",
		e.windowTitle,
	)
}

// tasklistWindowTitle reports whether image is currently running and its
// current main window title (which is the dialog title when a modal error
// box has replaced the game window), by shelling out to `tasklist /V`. It
// mirrors processRunning's contract but also extracts the "Window Title"
// verbose column.
func tasklistWindowTitle(image string) (running bool, title string, err error) {
	// #nosec G204 -- tasklist executable is fixed and image is derived from a local executable path.
	out, err := exec.Command("tasklist", "/V", "/FI", "IMAGENAME eq "+image, "/FO", "CSV", "/NH").Output()
	if err != nil {
		return false, "", err
	}
	running, title = parseTasklistVerboseCSV(string(out), image)
	return running, title, nil
}

// parseTasklistVerboseCSV extracts the running state and window title for
// image from `tasklist /V /FO CSV /NH` output. Isolated from the exec call so
// it is testable against captured sample output. tasklist prints an
// "INFO: No tasks..." line (not valid multi-field CSV) when nothing matches;
// that line simply fails the image-name comparison and is skipped.
func parseTasklistVerboseCSV(out, image string) (running bool, title string) {
	r := csv.NewReader(strings.NewReader(out))
	for {
		record, err := r.Read()
		if err != nil {
			return running, title
		}
		if len(record) == 0 || !strings.EqualFold(record[0], image) {
			continue
		}
		running = true
		if len(record) >= 9 {
			title = record[8]
		}
		return running, title
	}
}

func writeResult(outDir string, result recording.RecordingResult) error {
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "recording-result.json"), append(b, '\n'), 0o600)
}

type recordingSummary struct {
	OK            bool     `json:"ok"`
	DryRun        bool     `json:"dry_run"`
	Executed      bool     `json:"executed"`
	ResultPath    string   `json:"result_path"`
	ScriptPath    string   `json:"script_path"`
	SegmentCount  int      `json:"segment_count"`
	ArtifactCount int      `json:"artifact_count"`
	Warnings      []string `json:"warnings"`
}

func writeResultAndReport(outDir string, result recording.RecordingResult, dryRun bool, format string, w io.Writer) error {
	if err := writeResult(outDir, result); err != nil {
		return err
	}
	summary := recordingSummary{
		OK:            true,
		DryRun:        dryRun,
		Executed:      !dryRun,
		ResultPath:    filepath.Join(outDir, "recording-result.json"),
		ScriptPath:    result.Script,
		SegmentCount:  len(result.Plan.Segments),
		ArtifactCount: len(result.Artifacts),
		Warnings:      append([]string{}, result.Warnings...),
	}
	if format == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		return encoder.Encode(summary)
	}
	fmt.Fprintf(w, "recording_result\t%s\n", summary.ResultPath)
	fmt.Fprintf(w, "recording_script\t%s\n", summary.ScriptPath)
	fmt.Fprintf(w, "segments\t%d\n", summary.SegmentCount)
	fmt.Fprintf(w, "artifacts\t%d\n", summary.ArtifactCount)
	fmt.Fprintf(w, "dry_run\t%t\n", summary.DryRun)
	return nil
}
