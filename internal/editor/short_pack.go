package editor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/reche/zackvideo/internal/recording"
)

type shortPackOptions struct {
	OutputDir      string
	ResultPath     string
	PackPath       string
	FFprobePath    string
	CoversEnabled  bool
	SkipExisting   bool
	ValidateVideos bool
}

func renderShortPack(ctx context.Context, manifest *Manifest, result *Result, opts shortPackOptions) error {
	pack := shortPackRenderer{
		manifest: manifest,
		result:   result,
		opts:     opts,
	}
	if err := pack.render(ctx); err != nil {
		return pack.fail(err)
	}
	if err := pack.writeOutputs(); err != nil {
		return pack.fail(err)
	}
	return nil
}

type shortPackRenderer struct {
	manifest *Manifest
	result   *Result
	opts     shortPackOptions
}

func (p *shortPackRenderer) render(ctx context.Context) error {
	for i, short := range p.manifest.Shorts {
		if err := p.renderShort(ctx, i, short); err != nil {
			return err
		}
		if err := p.publishShort(ctx, i, short); err != nil {
			return err
		}
		p.runQualityCheck(ctx, short)
		if p.opts.CoversEnabled {
			p.renderCover(ctx, i, short)
			p.renderCoverSheet(ctx, i, short)
		}
	}
	return nil
}

func (p *shortPackRenderer) renderShort(ctx context.Context, i int, short ShortEdit) error {
	if err := os.MkdirAll(filepath.Dir(short.Output), 0o750); err != nil {
		return err
	}
	if p.opts.SkipExisting && fileExistsNonEmpty(short.Output) {
		p.result.Shorts[i].RenderSkipped = true
	} else if err := runFFmpegWithOptionalLog(ctx, short.FFmpegCommand, "short edit", short.RenderLogPath); err != nil {
		return err
	}
	artifact := p.probeArtifact(ctx, short.SegmentID, "short", "video", short.Output)
	p.result.Shorts[i].OutputArtifact = artifact
	p.manifest.Shorts[i].OutputArtifact = artifact
	if p.opts.ValidateVideos {
		p.result.Warnings = append(p.result.Warnings, ValidateShortArtifact(artifact)...)
	}
	return nil
}

func (p *shortPackRenderer) publishShort(ctx context.Context, i int, short ShortEdit) error {
	if err := publishShort(short); err != nil {
		return err
	}
	artifact := p.probeArtifact(ctx, short.SegmentID, "publish", "video", short.PublishPath)
	p.result.Shorts[i].PublishArtifact = artifact
	p.manifest.Shorts[i].PublishArtifact = artifact
	if p.opts.ValidateVideos {
		p.result.Warnings = append(p.result.Warnings, ValidateShortArtifact(artifact)...)
	}
	return nil
}

func (p *shortPackRenderer) runQualityCheck(ctx context.Context, short ShortEdit) {
	if len(short.QualityCommand) == 0 {
		return
	}
	output, err := runFFmpegOutput(ctx, short.QualityCommand, "quality check")
	if short.QualityLogPath != "" {
		if writeErr := writeLogFile(short.QualityLogPath, output); writeErr != nil {
			p.result.Warnings = append(p.result.Warnings, fmt.Sprintf("quality log %s: %v", short.SegmentID, writeErr))
		}
	}
	if err != nil {
		p.result.Warnings = append(p.result.Warnings, fmt.Sprintf("quality check %s: %v", short.SegmentID, err))
		return
	}
	p.result.Warnings = append(p.result.Warnings, QualityWarningsFromFFmpegLog(short.SegmentID, output)...)
}

func (p *shortPackRenderer) renderCover(ctx context.Context, i int, short ShortEdit) {
	if p.opts.SkipExisting && fileExistsNonEmpty(short.CoverPath) {
		p.result.Shorts[i].CoverSkipped = true
		p.result.Shorts[i].CoverArtifact = p.probeCover(ctx, short.SegmentID, "cover", short.CoverPath)
		return
	}
	if err := runFFmpeg(ctx, short.CoverCommand, "cover extract"); err != nil {
		p.result.Warnings = append(p.result.Warnings, fmt.Sprintf("cover %s: %v", short.SegmentID, err))
		return
	}
	p.result.Shorts[i].CoverArtifact = p.probeCover(ctx, short.SegmentID, "cover", short.CoverPath)
}

func (p *shortPackRenderer) renderCoverSheet(ctx context.Context, i int, short ShortEdit) {
	if short.CoverSheetPath == "" {
		return
	}
	if p.opts.SkipExisting && fileExistsNonEmpty(short.CoverSheetPath) {
		p.result.Shorts[i].CoverSheetSkipped = true
		p.result.Shorts[i].CoverSheetArtifact = p.probeCover(ctx, short.SegmentID, "cover-sheet", short.CoverSheetPath)
		return
	}
	if err := runFFmpeg(ctx, short.CoverSheetCommand, "cover sheet"); err != nil {
		p.result.Warnings = append(p.result.Warnings, fmt.Sprintf("cover sheet %s: %v", short.SegmentID, err))
		return
	}
	p.result.Shorts[i].CoverSheetArtifact = p.probeCover(ctx, short.SegmentID, "cover-sheet", short.CoverSheetPath)
}

func (p *shortPackRenderer) probeCover(ctx context.Context, segmentID, role, path string) recording.RecordingArtifact {
	artifact := p.probeArtifact(ctx, segmentID, role, "image", path)
	p.result.Warnings = append(p.result.Warnings, ValidateCoverArtifact(artifact)...)
	return artifact
}

func (p *shortPackRenderer) probeArtifact(ctx context.Context, segmentID, role, artifactType, path string) recording.RecordingArtifact {
	artifact := recording.RecordingArtifact{
		SegmentID: segmentID,
		Role:      role,
		Type:      artifactType,
		Path:      path,
	}
	if info, err := os.Stat(path); err == nil {
		artifact.SizeBytes = info.Size()
	}
	if p.opts.FFprobePath != "" {
		recording.ProbeArtifact(ctx, p.opts.FFprobePath, &artifact)
	}
	return artifact
}

func (p *shortPackRenderer) writeOutputs() error {
	p.manifest.Warnings = append([]string(nil), p.result.Warnings...)
	if err := WriteManifest(filepath.Join(p.opts.OutputDir, "edit-manifest.json"), *p.manifest); err != nil {
		return err
	}
	if err := WritePackManifest(p.opts.PackPath, PackManifestFromManifest(*p.manifest, *p.result)); err != nil {
		return err
	}
	if err := WritePublishGallery(p.manifest.GalleryPath, *p.manifest); err != nil {
		return err
	}
	return WriteResult(p.opts.ResultPath, *p.result)
}

func (p *shortPackRenderer) fail(err error) error {
	p.result.Error = err.Error()
	_ = WriteResult(p.opts.ResultPath, *p.result)
	return err
}

func runFFmpegWithOptionalLog(ctx context.Context, command []string, label, logPath string) error {
	output, err := runFFmpegOutput(ctx, command, label)
	if err != nil && logPath != "" {
		_ = writeLogFile(logPath, output)
	}
	return err
}

func writeLogFile(path, content string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}
