package main

import "github.com/rechedev9/fragforge/internal/capturetools"

// captureToolSource records how each capture/render tool path was resolved, so
// /api/capabilities can tell the user "auto-detected" vs "you set it" vs
// missing. Keys are env var names; values are "env" | "detected" | "none".
type captureToolSource = capturetools.Sources

// detectCaptureTools fills empty capture/render paths with the same shared
// probes used by the shell CLI's capability report.
func detectCaptureTools(cfg config) (config, captureToolSource) {
	paths, sources := capturetools.Detect(capturetools.Paths{
		Recorder: cfg.RecorderPath,
		HLAE:     cfg.HLAEPath,
		CS2:      cfg.CS2Path,
		Composer: cfg.ComposerPath,
		Editor:   cfg.EditorPath,
		FFmpeg:   cfg.FFmpegPath,
		FFprobe:  cfg.FFprobePath,
		Ytdlp:    cfg.YtdlpPath,
	})
	cfg.RecorderPath = paths.Recorder
	cfg.HLAEPath = paths.HLAE
	cfg.CS2Path = paths.CS2
	cfg.ComposerPath = paths.Composer
	cfg.EditorPath = paths.Editor
	cfg.FFmpegPath = paths.FFmpeg
	cfg.FFprobePath = paths.FFprobe
	cfg.YtdlpPath = paths.Ytdlp
	return cfg, sources
}
