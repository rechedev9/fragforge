package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"

	"github.com/rechedev9/fragforge/internal/recording"
)

func Run(ctx context.Context, cfg Config) (Result, error) {
	if err := cfg.validate(); err != nil {
		return Result{}, err
	}

	recordingResultPath, err := filepath.Abs(cfg.RecordingResultPath)
	if err != nil {
		return Result{}, fmt.Errorf("resolve recording result path: %w", err)
	}
	outDir, err := filepath.Abs(cfg.OutputDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve output path: %w", err)
	}

	recordingResult, err := ReadRecordingResult(recordingResultPath)
	if err != nil {
		return Result{}, err
	}
	killPlan, killPlanPath, metadataWarnings, err := resolveKillPlan(recordingResultPath, cfg.KillPlanPath)
	if err != nil {
		return Result{}, err
	}
	publishDir := cfg.PublishDir
	if publishDir == "" {
		publishDir = filepath.Join(outDir, "publish")
	} else {
		publishDir, err = filepath.Abs(publishDir)
		if err != nil {
			return Result{}, fmt.Errorf("resolve publish path: %w", err)
		}
	}
	preset := cfg.Preset
	if preset == "" {
		preset = DefaultPreset().Name
	}
	videoCRF, err := normalizeVideoCRFForPreset(preset, cfg.VideoCRF)
	if err != nil {
		return Result{}, err
	}
	videoPreset, err := normalizeVideoPresetForPreset(preset, cfg.VideoPreset)
	if err != nil {
		return Result{}, err
	}
	effectsPath := cfg.EffectsPath
	if effectsPath != "" {
		effectsPath, err = filepath.Abs(effectsPath)
		if err != nil {
			return Result{}, fmt.Errorf("resolve effects script path: %w", err)
		}
	}
	musicPath := cfg.MusicPath
	if musicPath != "" {
		musicPath, err = filepath.Abs(musicPath)
		if err != nil {
			return Result{}, fmt.Errorf("resolve music path: %w", err)
		}
		if _, err := os.Stat(musicPath); err != nil {
			return Result{}, fmt.Errorf("music not found: %w", err)
		}
	}
	rhythmPath := cfg.RhythmPath
	if rhythmPath != "" {
		rhythmPath, err = filepath.Abs(rhythmPath)
		if err != nil {
			return Result{}, fmt.Errorf("resolve rhythm path: %w", err)
		}
		if _, err := os.Stat(rhythmPath); err != nil {
			return Result{}, fmt.Errorf("rhythm json not found: %w", err)
		}
	}

	ffmpegPath := cfg.FFmpegPath
	if ffmpegPath == "" {
		ffmpegPath = recording.FindFFmpeg()
	}
	commandFFmpeg := ffmpegPath
	if commandFFmpeg == "" {
		commandFFmpeg = "ffmpeg"
	}
	ffprobePath := cfg.FFprobePath
	if ffprobePath == "" {
		ffprobePath = recording.FindFFprobe()
	}
	coversEnabled := !cfg.DisableCovers

	// Per-kill killfeed crop measurement extracts source frames with FFmpeg,
	// so dry runs keep the static crop defaults.
	var killfeedProbe func(input string, atSeconds float64) (image.Image, error)
	if !cfg.DryRun {
		killfeedProbe = ffmpegFrameProbe(commandFFmpeg)
	}

	manifest, err := buildManifest(recordingResult, ManifestOptions{
		RecordingResultPath: recordingResultPath,
		KillPlanPath:        killPlanPath,
		OutputDir:           outDir,
		PublishDir:          publishDir,
		Preset:              preset,
		EffectsPath:         effectsPath,
		EffectsPreset:       cfg.EffectsPreset,
		MusicPath:           musicPath,
		RhythmPath:          rhythmPath,
		OutputFormat:        cfg.OutputFormat,
		KillEffect:          cfg.KillEffect,
		Transition:          cfg.Transition,
		Intro:               cfg.Intro,
		Outro:               cfg.Outro,
		OutputFPS:           cfg.OutputFPS,
		CompileSegments:     cfg.CompileSegments,
		LineupCatalogPath:   cfg.LineupCatalogPath,
		SegmentIDs:          cfg.SegmentIDs,
		Limit:               cfg.Limit,
		VideoCRF:            videoCRF,
		VideoPreset:         videoPreset,
		HQFilters:           cfg.HQFilters,
		AudioNormalize:      cfg.AudioNormalize,
		QualityChecks:       cfg.QualityChecks,
		CoverSheets:         cfg.CoverSheets,
		TemporalSmoothing:   cfg.TemporalSmoothing,
		FFmpegPath:          commandFFmpeg,
		CoversEnabled:       coversEnabled,
		SkipExisting:        cfg.SkipExisting,
		KillPlan:            killPlan,
		KillfeedFrameProbe:  killfeedProbe,
	})
	if err != nil {
		return Result{}, err
	}
	manifest.Warnings = append(metadataWarnings, manifest.Warnings...)
	result := resultFromManifest(manifest, cfg.DryRun)

	if err := os.MkdirAll(filepath.Join(outDir, "prompts"), 0o750); err != nil {
		result.Error = err.Error()
		_ = WriteResult(filepath.Join(outDir, "shorts-result.json"), result)
		return result, err
	}
	result.Warnings = append(result.Warnings, writePrompts(manifest)...)
	result.Warnings = append(result.Warnings, writeCaptions(manifest)...)
	if err := WritePublishSummary(manifest.SummaryPath, manifest); err != nil {
		result.Error = err.Error()
		_ = WriteResult(filepath.Join(outDir, "shorts-result.json"), result)
		return result, err
	}
	if err := WriteManifest(filepath.Join(outDir, "edit-manifest.json"), manifest); err != nil {
		result.Error = err.Error()
		_ = WriteResult(filepath.Join(outDir, "shorts-result.json"), result)
		return result, err
	}
	if err := WriteUnmatchedSmokes(manifest); err != nil {
		result.Error = err.Error()
		_ = WriteResult(filepath.Join(outDir, "shorts-result.json"), result)
		return result, err
	}

	resultPath := filepath.Join(outDir, "shorts-result.json")
	packPath := filepath.Join(publishDir, "pack-manifest.json")
	if cfg.DryRun {
		if err := WritePackManifest(packPath, PackManifestFromManifest(manifest, result)); err != nil {
			result.Error = err.Error()
			_ = WriteResult(resultPath, result)
			return result, err
		}
		if err := WritePublishGallery(manifest.GalleryPath, manifest); err != nil {
			result.Error = err.Error()
			_ = WriteResult(resultPath, result)
			return result, err
		}
		return result, WriteResult(resultPath, result)
	}
	if ffmpegPath == "" && mediaWorkRequired(manifest, coversEnabled, cfg.SkipExisting) {
		result.Error = "ffmpeg not found"
		_ = WriteResult(resultPath, result)
		return result, errors.New(result.Error)
	}
	if err := renderShortPack(ctx, &manifest, &result, shortPackOptions{
		OutputDir:      outDir,
		ResultPath:     resultPath,
		PackPath:       packPath,
		FFprobePath:    ffprobePath,
		CoversEnabled:  coversEnabled,
		SkipExisting:   cfg.SkipExisting,
		ValidateVideos: true,
		RenderJobs:     cfg.RenderJobs,
	}); err != nil {
		return result, err
	}
	return result, nil
}

func (c Config) validate() error {
	if c.RecordingResultPath == "" {
		return fmt.Errorf("recording result path is required")
	}
	if c.OutputDir == "" {
		return fmt.Errorf("output dir is required")
	}
	if c.Limit < 0 {
		return fmt.Errorf("limit must be >= 0")
	}
	if c.RenderJobs < 0 {
		return fmt.Errorf("render jobs must be >= 0")
	}
	preset := c.Preset
	if preset == "" {
		preset = DefaultPreset().Name
	}
	if _, ok := PresetByName(preset); !ok {
		return unknownPresetError(c.Preset)
	}
	if _, err := normalizeVideoCRFForPreset(preset, c.VideoCRF); err != nil {
		return err
	}
	if _, err := normalizeVideoPresetForPreset(preset, c.VideoPreset); err != nil {
		return err
	}
	if _, err := normalizeOutputFPS(c.OutputFPS); err != nil {
		return err
	}
	if _, err := normalizeOutputFormat(c.OutputFormat); err != nil {
		return err
	}
	if _, err := normalizeKillEffect(c.KillEffect); err != nil {
		return err
	}
	if _, err := normalizeTransition(c.Transition); err != nil {
		return err
	}
	if c.RhythmPath != "" && c.MusicPath == "" {
		return fmt.Errorf("rhythm path requires music path")
	}
	if c.RhythmPath != "" && !c.CompileSegments {
		return fmt.Errorf("rhythm path requires compile segments")
	}
	for _, id := range c.SegmentIDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("segment ids must not be empty")
		}
	}
	if c.EffectsPath == "" && c.EffectsPreset != "" {
		switch c.EffectsPreset {
		case EffectsPresetViralUltraClean:
		default:
			return fmt.Errorf("unknown effects preset %q", c.EffectsPreset)
		}
	}
	return nil
}

func ReadRecordingResult(path string) (recording.RecordingResult, error) {
	// #nosec G304 -- recording result path is an explicit local CLI/config input.
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

func WriteManifest(path string, manifest Manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func WriteResult(path string, result Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func WritePackManifest(path string, manifest PackManifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func resultFromManifest(manifest Manifest, dryRun bool) Result {
	result := Result{
		Preset:            manifest.Preset,
		RecordingResult:   manifest.RecordingResult,
		KillPlan:          manifest.KillPlan,
		OutputDir:         manifest.OutputDir,
		PublishDir:        manifest.PublishDir,
		GalleryPath:       manifest.GalleryPath,
		SummaryPath:       manifest.SummaryPath,
		SegmentFilter:     append([]string(nil), manifest.SegmentFilter...),
		Limit:             manifest.Limit,
		SkipExisting:      manifest.SkipExisting,
		EffectsPath:       manifest.EffectsPath,
		EffectsPreset:     manifest.EffectsPreset,
		MusicPath:         manifest.MusicPath,
		RhythmPath:        manifest.RhythmPath,
		OutputFormat:      manifest.OutputFormat,
		KillEffect:        manifest.KillEffect,
		Transition:        manifest.Transition,
		Intro:             manifest.Intro,
		Outro:             manifest.Outro,
		OutputFPS:         manifest.OutputFPS,
		CompileSegments:   manifest.CompileSegments,
		LineupCatalogPath: manifest.LineupCatalogPath,
		UnmatchedSmokes:   manifest.UnmatchedSmokes,
		VideoCRF:          manifest.VideoCRF,
		VideoPreset:       manifest.VideoPreset,
		HQFilters:         manifest.HQFilters,
		AudioNormalize:    manifest.AudioNormalize,
		QualityChecks:     manifest.QualityChecks,
		CoverSheets:       manifest.CoverSheets,
		TemporalSmoothing: manifest.TemporalSmoothing,
		CoversEnabled:     manifest.CoversEnabled,
		DryRun:            dryRun,
		Warnings:          append([]string(nil), manifest.Warnings...),
		Shorts:            make([]ShortResult, 0, len(manifest.Shorts)),
	}
	for _, short := range manifest.Shorts {
		result.Shorts = append(result.Shorts, ShortResult{
			Index:             short.Index,
			SegmentID:         short.SegmentID,
			Preset:            short.Preset,
			Input:             short.Input,
			Output:            short.Output,
			SourceArtifact:    short.SourceArtifact,
			PromptPath:        short.PromptPath,
			PublishPath:       short.PublishPath,
			MusicPath:         short.MusicPath,
			RhythmPath:        short.RhythmPath,
			OutputFormat:      short.OutputFormat,
			KillEffect:        short.KillEffect,
			Transition:        short.Transition,
			Intro:             short.Intro,
			Outro:             short.Outro,
			OutputFPS:         short.OutputFPS,
			VideoCRF:          short.VideoCRF,
			VideoPreset:       short.VideoPreset,
			HQFilters:         short.HQFilters,
			AudioNormalize:    short.AudioNormalize,
			TemporalSmoothing: short.TemporalSmoothing,
			CaptionPath:       short.CaptionPath,
			CoverPath:         short.CoverPath,
			CoverSheetPath:    short.CoverSheetPath,
			CoverTimeSeconds:  short.CoverTimeSeconds,
			DurationSeconds:   short.DurationSeconds,
			Title:             short.Title,
			Headline:          short.Headline,
			Caption:           short.Caption,
			Hashtags:          append([]string(nil), short.Hashtags...),
			SmokeCount:        short.SmokeCount,
			PrimarySmoke:      short.PrimarySmoke,
			Smokes:            append([]SmokeCue(nil), short.Smokes...),
			Parts:             append([]ShortPart(nil), short.Parts...),
			Effects:           append([]Effect(nil), short.Effects...),
			FFmpegCommand:     append([]string(nil), short.FFmpegCommand...),
			CoverCommand:      append([]string(nil), short.CoverCommand...),
			CoverSheetCommand: append([]string(nil), short.CoverSheetCommand...),
			QualityCommand:    append([]string(nil), short.QualityCommand...),
			RenderLogPath:     short.RenderLogPath,
			QualityLogPath:    short.QualityLogPath,
		})
	}
	return result
}

func WriteUnmatchedSmokes(manifest Manifest) error {
	if manifest.UnmatchedSmokes == "" {
		return nil
	}
	type unmatchedSmoke struct {
		SegmentID string   `json:"segment_id"`
		Player    string   `json:"player,omitempty"`
		Map       string   `json:"map,omitempty"`
		Smoke     SmokeCue `json:"smoke"`
	}
	var out []unmatchedSmoke
	for _, short := range manifest.Shorts {
		for _, smoke := range short.Smokes {
			if smoke.Matched {
				continue
			}
			out = append(out, unmatchedSmoke{
				SegmentID: short.SegmentID,
				Player:    short.Player,
				Map:       short.Map,
				Smoke:     smoke,
			})
		}
	}
	if err := os.MkdirAll(filepath.Dir(manifest.UnmatchedSmokes), 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifest.UnmatchedSmokes, append(b, '\n'), 0o600)
}

func mediaWorkRequired(manifest Manifest, coversEnabled, skipExisting bool) bool {
	if !skipExisting {
		return len(manifest.Shorts) > 0
	}
	for _, short := range manifest.Shorts {
		if !fileExistsNonEmpty(short.Output) {
			return true
		}
		if coversEnabled && short.CoverPath != "" && !fileExistsNonEmpty(short.CoverPath) {
			return true
		}
		if coversEnabled && short.CoverSheetPath != "" && !fileExistsNonEmpty(short.CoverSheetPath) {
			return true
		}
	}
	return false
}

func fileExistsNonEmpty(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0 && !info.IsDir()
}

func writePrompts(manifest Manifest) []string {
	var warnings []string
	for _, short := range manifest.Shorts {
		if err := os.MkdirAll(filepath.Dir(short.PromptPath), 0o750); err != nil {
			warnings = append(warnings, fmt.Sprintf("write prompt for %s: %v", short.SegmentID, err))
			continue
		}
		if err := os.WriteFile(short.PromptPath, []byte(GenerateCoverPrompt(short)), 0o600); err != nil {
			warnings = append(warnings, fmt.Sprintf("write prompt for %s: %v", short.SegmentID, err))
		}
	}
	return warnings
}
