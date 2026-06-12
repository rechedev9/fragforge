package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"

	"github.com/rechedev9/fragforge/internal/editor"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var (
		recordingResultPath = flag.String("recording-result", "", "path to recording-result.json")
		killPlanPath        = flag.String("killplan", "", "optional path to kill plan JSON; auto-discovered from pipeline-result.json when omitted")
		outDir              = flag.String("out", "", "shorts output directory")
		publishDir          = flag.String("publish-dir", "", "publish pack output directory; defaults to <out>/publish")
		preset              = flag.String("preset", editor.DefaultPreset().Name, "editor preset: "+strings.Join(editor.PresetNames(), ", "))
		effectsPath         = flag.String("effects", "", "optional Lua effects script; overrides --effects-preset")
		effectsPreset       = flag.String("effects-preset", "", "effects preset: builtin-clean, awpgod, smoke-lineups, viral-ultra, or none; defaults by preset")
		musicPath           = flag.String("music", "", "optional external music file to mix into rendered shorts")
		rhythmPath          = flag.String("rhythm", "", "optional rhythm JSON with segment_sync entries for compiled shorts")
		outputFPS           = flag.Int("fps", 0, "optional final output FPS; defaults to 60")
		compileSegments     = flag.Bool("compile-segments", false, "render selected segments as one compilation short")
		lineupCatalogPath   = flag.String("lineup-catalog", "", "optional directory with manual smoke lineup catalog JSON files")
		segments            = flag.String("segments", "", "optional comma-separated segment ids to render, e.g. seg-001,seg-004")
		limit               = flag.Int("limit", 0, "optional max number of shorts to render after segment filtering")
		videoCRF            = flag.Int("video-crf", 0, "x264 CRF quality from 1..51; lower is higher quality; defaults by preset")
		videoPreset         = flag.String("video-preset", "", "x264 preset; defaults by preset")
		hqFilters           = flag.Bool("hq-filters", false, "use Lanczos scaling and square-pixel normalization")
		audioNormalize      = flag.Bool("audio-normalize", false, "normalize audio with FFmpeg loudnorm")
		qualityChecks       = flag.Bool("quality-checks", false, "run FFmpeg black/freeze/crop detection after rendering")
		coverSheets         = flag.Bool("cover-sheets", false, "generate tiled cover contact sheets")
		temporalSmoothing   = flag.Bool("temporal-smoothing", false, "add subtle temporal frame blending for smoother perceived motion")
		ffmpegPath          = flag.String("ffmpeg", "", "path to ffmpeg.exe; defaults to PATH")
		ffprobePath         = flag.String("ffprobe", "", "path to ffprobe.exe; defaults to PATH")
		covers              = flag.Bool("covers", true, "generate local JPG covers for publish pack")
		noCovers            = flag.Bool("no-covers", false, "disable local JPG cover generation")
		skipExisting        = flag.Bool("skip-existing", false, "reuse existing short and cover files instead of rerendering them")
		renderJobs          = flag.Int("render-jobs", 0, "max shorts rendered concurrently; 0 selects an automatic CPU-based limit")
		openGallery         = flag.Bool("open-gallery", false, "open the publish gallery after a successful run")
		dryRun              = flag.Bool("dry-run", false, "write manifests and prompts without running FFmpeg")
		listPresets         = flag.Bool("list-presets", false, "print supported preset names, one per line, and exit; used by zv short to detect stale binaries")
	)
	flag.Parse()

	if *listPresets {
		fmt.Println(strings.Join(editor.PresetNames(), "\n"))
		return nil
	}
	if *recordingResultPath == "" || *outDir == "" {
		return fmt.Errorf("--recording-result and --out are required")
	}
	segmentIDs, err := parseSegments(*segments)
	if err != nil {
		return err
	}
	result, err := editor.Run(context.Background(), editor.Config{
		RecordingResultPath: *recordingResultPath,
		KillPlanPath:        *killPlanPath,
		OutputDir:           *outDir,
		PublishDir:          *publishDir,
		Preset:              *preset,
		EffectsPath:         *effectsPath,
		EffectsPreset:       *effectsPreset,
		MusicPath:           *musicPath,
		RhythmPath:          *rhythmPath,
		OutputFPS:           *outputFPS,
		CompileSegments:     *compileSegments,
		LineupCatalogPath:   *lineupCatalogPath,
		SegmentIDs:          segmentIDs,
		Limit:               *limit,
		VideoCRF:            *videoCRF,
		VideoPreset:         *videoPreset,
		HQFilters:           *hqFilters,
		AudioNormalize:      *audioNormalize,
		QualityChecks:       *qualityChecks,
		CoverSheets:         *coverSheets,
		TemporalSmoothing:   *temporalSmoothing,
		FFmpegPath:          *ffmpegPath,
		FFprobePath:         *ffprobePath,
		DisableCovers:       !*covers || *noCovers,
		SkipExisting:        *skipExisting,
		RenderJobs:          *renderJobs,
		DryRun:              *dryRun,
	})
	if err != nil {
		return err
	}
	if *openGallery {
		return openPath(result.GalleryPath)
	}
	return nil
}

func parseSegments(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--segments did not contain any segment ids")
	}
	return out, nil
}

func openPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("gallery path is empty")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// #nosec G204 -- opens the generated local gallery path with the OS handler.
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		// #nosec G204 -- opens the generated local gallery path with the OS handler.
		cmd = exec.Command("open", path)
	default:
		// #nosec G204 -- opens the generated local gallery path with the OS handler.
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open gallery: %w", err)
	}
	return nil
}
