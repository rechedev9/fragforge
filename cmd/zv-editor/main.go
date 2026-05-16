package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"

	"github.com/reche/zackvideo/internal/editor"
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
		preset              = flag.String("preset", editor.PresetShortClean, "editor preset: short-clean or short-premium-player")
		effectsPath         = flag.String("effects", "", "optional Lua effects script; overrides --effects-preset")
		effectsPreset       = flag.String("effects-preset", editor.EffectsPresetBuiltinClean, "effects preset: builtin-clean, awpgod, or none")
		segments            = flag.String("segments", "", "optional comma-separated segment ids to render, e.g. seg-001,seg-004")
		limit               = flag.Int("limit", 0, "optional max number of shorts to render after segment filtering")
		playerImage         = flag.String("player-image", "", "player image asset for short-premium-player preset")
		playerKeyColor      = flag.String("player-key-color", "", "optional chromakey color for player image, e.g. #000000")
		ffmpegPath          = flag.String("ffmpeg", "", "path to ffmpeg.exe; defaults to PATH")
		ffprobePath         = flag.String("ffprobe", "", "path to ffprobe.exe; defaults to PATH")
		covers              = flag.Bool("covers", true, "generate local JPG covers for publish pack")
		noCovers            = flag.Bool("no-covers", false, "disable local JPG cover generation")
		skipExisting        = flag.Bool("skip-existing", false, "reuse existing short and cover files instead of rerendering them")
		openGallery         = flag.Bool("open-gallery", false, "open the publish gallery after a successful run")
		dryRun              = flag.Bool("dry-run", false, "write manifests and prompts without running FFmpeg")
	)
	flag.Parse()

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
		SegmentIDs:          segmentIDs,
		Limit:               *limit,
		PlayerImagePath:     *playerImage,
		PlayerKeyColor:      *playerKeyColor,
		FFmpegPath:          *ffmpegPath,
		FFprobePath:         *ffprobePath,
		DisableCovers:       !*covers || *noCovers,
		SkipExisting:        *skipExisting,
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
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open gallery: %w", err)
	}
	return nil
}
