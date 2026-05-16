package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/reche/zackvideo/internal/recording"
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
		preset = PresetShortClean
	}
	playerImagePath := cfg.PlayerImagePath
	if playerImagePath != "" {
		playerImagePath, err = filepath.Abs(playerImagePath)
		if err != nil {
			return Result{}, fmt.Errorf("resolve player image path: %w", err)
		}
	}
	effectsPath := cfg.EffectsPath
	if effectsPath != "" {
		effectsPath, err = filepath.Abs(effectsPath)
		if err != nil {
			return Result{}, fmt.Errorf("resolve effects script path: %w", err)
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

	manifest, err := buildManifest(recordingResult, ManifestOptions{
		RecordingResultPath: recordingResultPath,
		KillPlanPath:        killPlanPath,
		OutputDir:           outDir,
		PublishDir:          publishDir,
		Preset:              preset,
		EffectsPath:         effectsPath,
		EffectsPreset:       cfg.EffectsPreset,
		SegmentIDs:          cfg.SegmentIDs,
		Limit:               cfg.Limit,
		PlayerImagePath:     playerImagePath,
		PlayerKeyColor:      cfg.PlayerKeyColor,
		FFmpegPath:          commandFFmpeg,
		CoversEnabled:       coversEnabled,
		SkipExisting:        cfg.SkipExisting,
		KillPlan:            killPlan,
	})
	if err != nil {
		return Result{}, err
	}
	manifest.Warnings = append(metadataWarnings, manifest.Warnings...)
	result := resultFromManifest(manifest, cfg.DryRun)

	if err := os.MkdirAll(filepath.Join(outDir, "prompts"), 0o755); err != nil {
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

	for i, short := range manifest.Shorts {
		if err := os.MkdirAll(filepath.Dir(short.Output), 0o755); err != nil {
			result.Error = err.Error()
			_ = WriteResult(resultPath, result)
			return result, err
		}
		if cfg.SkipExisting && fileExistsNonEmpty(short.Output) {
			result.Shorts[i].RenderSkipped = true
		} else {
			if err := runFFmpeg(ctx, short.FFmpegCommand, "short edit"); err != nil {
				result.Error = err.Error()
				_ = WriteResult(resultPath, result)
				return result, err
			}
		}
		artifact := recording.RecordingArtifact{
			SegmentID: short.SegmentID,
			Role:      "short",
			Type:      "video",
			Path:      short.Output,
		}
		if info, err := os.Stat(short.Output); err == nil {
			artifact.SizeBytes = info.Size()
		}
		if ffprobePath != "" {
			recording.ProbeArtifact(ctx, ffprobePath, &artifact)
		}
		result.Shorts[i].OutputArtifact = artifact
		result.Warnings = append(result.Warnings, ValidateShortArtifact(artifact)...)
		if err := publishShort(short); err != nil {
			result.Error = err.Error()
			_ = WriteResult(resultPath, result)
			return result, err
		}
		publishArtifact := recording.RecordingArtifact{
			SegmentID: short.SegmentID,
			Role:      "publish",
			Type:      "video",
			Path:      short.PublishPath,
		}
		if info, err := os.Stat(short.PublishPath); err == nil {
			publishArtifact.SizeBytes = info.Size()
		}
		if ffprobePath != "" {
			recording.ProbeArtifact(ctx, ffprobePath, &publishArtifact)
		}
		result.Shorts[i].PublishArtifact = publishArtifact
		result.Warnings = append(result.Warnings, ValidateShortArtifact(publishArtifact)...)
		if coversEnabled {
			if cfg.SkipExisting && fileExistsNonEmpty(short.CoverPath) {
				result.Shorts[i].CoverSkipped = true
				coverArtifact := recording.RecordingArtifact{
					SegmentID: short.SegmentID,
					Role:      "cover",
					Type:      "image",
					Path:      short.CoverPath,
				}
				if info, err := os.Stat(short.CoverPath); err == nil {
					coverArtifact.SizeBytes = info.Size()
				}
				if ffprobePath != "" {
					recording.ProbeArtifact(ctx, ffprobePath, &coverArtifact)
				}
				result.Shorts[i].CoverArtifact = coverArtifact
				result.Warnings = append(result.Warnings, ValidateCoverArtifact(coverArtifact)...)
			} else if err := runFFmpeg(ctx, short.CoverCommand, "cover extract"); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("cover %s: %v", short.SegmentID, err))
			} else {
				coverArtifact := recording.RecordingArtifact{
					SegmentID: short.SegmentID,
					Role:      "cover",
					Type:      "image",
					Path:      short.CoverPath,
				}
				if info, err := os.Stat(short.CoverPath); err == nil {
					coverArtifact.SizeBytes = info.Size()
				}
				if ffprobePath != "" {
					recording.ProbeArtifact(ctx, ffprobePath, &coverArtifact)
				}
				result.Shorts[i].CoverArtifact = coverArtifact
				result.Warnings = append(result.Warnings, ValidateCoverArtifact(coverArtifact)...)
			}
		}
	}
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
	for _, id := range c.SegmentIDs {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("segment ids must not be empty")
		}
	}
	preset := c.Preset
	if preset == "" {
		preset = PresetShortClean
	}
	if c.EffectsPath == "" && c.EffectsPreset != "" {
		switch c.EffectsPreset {
		case EffectsPresetBuiltinClean, EffectsPresetAWPGod, EffectsPresetNone:
		default:
			return fmt.Errorf("unknown effects preset %q", c.EffectsPreset)
		}
	}
	switch preset {
	case PresetShortClean:
		if c.PlayerImagePath != "" {
			return fmt.Errorf("--player-image requires --preset %q", PresetShortPremiumPlayer)
		}
		if c.PlayerKeyColor != "" {
			return fmt.Errorf("--player-key-color requires --preset %q", PresetShortPremiumPlayer)
		}
	case PresetShortPremiumPlayer:
		if c.PlayerImagePath == "" {
			return fmt.Errorf("--player-image is required for preset %q", PresetShortPremiumPlayer)
		}
		if _, err := os.Stat(c.PlayerImagePath); err != nil {
			return fmt.Errorf("player image not found: %w", err)
		}
		if !supportedPlayerImage(c.PlayerImagePath) {
			return fmt.Errorf("player image must be png, jpg, jpeg, or webp")
		}
	default:
		return fmt.Errorf("unknown preset %q", c.Preset)
	}
	return nil
}

func ReadRecordingResult(path string) (recording.RecordingResult, error) {
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func WriteResult(path string, result Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func WritePackManifest(path string, manifest PackManifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func resultFromManifest(manifest Manifest, dryRun bool) Result {
	result := Result{
		Preset:          manifest.Preset,
		RecordingResult: manifest.RecordingResult,
		KillPlan:        manifest.KillPlan,
		OutputDir:       manifest.OutputDir,
		PublishDir:      manifest.PublishDir,
		GalleryPath:     manifest.GalleryPath,
		SummaryPath:     manifest.SummaryPath,
		SegmentFilter:   append([]string(nil), manifest.SegmentFilter...),
		Limit:           manifest.Limit,
		SkipExisting:    manifest.SkipExisting,
		EffectsPath:     manifest.EffectsPath,
		EffectsPreset:   manifest.EffectsPreset,
		PlayerImage:     manifest.PlayerImage,
		PlayerKeyColor:  manifest.PlayerKeyColor,
		CoversEnabled:   manifest.CoversEnabled,
		DryRun:          dryRun,
		Warnings:        append([]string(nil), manifest.Warnings...),
		Shorts:          make([]ShortResult, 0, len(manifest.Shorts)),
	}
	for _, short := range manifest.Shorts {
		result.Shorts = append(result.Shorts, ShortResult{
			Index:            short.Index,
			SegmentID:        short.SegmentID,
			Preset:           short.Preset,
			Input:            short.Input,
			Output:           short.Output,
			PromptPath:       short.PromptPath,
			PublishPath:      short.PublishPath,
			PlayerImage:      short.PlayerImage,
			PlayerKeyColor:   short.PlayerKeyColor,
			CaptionPath:      short.CaptionPath,
			CoverPath:        short.CoverPath,
			CoverTimeSeconds: short.CoverTimeSeconds,
			DurationSeconds:  short.DurationSeconds,
			Title:            short.Title,
			Headline:         short.Headline,
			Caption:          short.Caption,
			Hashtags:         append([]string(nil), short.Hashtags...),
			Effects:          append([]Effect(nil), short.Effects...),
			FFmpegCommand:    append([]string(nil), short.FFmpegCommand...),
			CoverCommand:     append([]string(nil), short.CoverCommand...),
		})
	}
	return result
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

func supportedPlayerImage(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	default:
		return false
	}
}

func writePrompts(manifest Manifest) []string {
	var warnings []string
	for _, short := range manifest.Shorts {
		if err := os.MkdirAll(filepath.Dir(short.PromptPath), 0o755); err != nil {
			warnings = append(warnings, fmt.Sprintf("write prompt for %s: %v", short.SegmentID, err))
			continue
		}
		if err := os.WriteFile(short.PromptPath, []byte(GenerateCoverPrompt(short)), 0o644); err != nil {
			warnings = append(warnings, fmt.Sprintf("write prompt for %s: %v", short.SegmentID, err))
		}
	}
	return warnings
}
