package composition

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reche/zackvideo/internal/recording"
)

func SegmentClipsFromRecording(result recording.RecordingResult) ([]SegmentClip, []string) {
	bySegment := map[string][]recording.RecordingArtifact{}
	for _, artifact := range result.Artifacts {
		if artifact.Role == "segment" && artifact.Type == "video" && artifact.SegmentID != "" {
			bySegment[artifact.SegmentID] = append(bySegment[artifact.SegmentID], artifact)
		}
	}

	var warnings []string
	clips := make([]SegmentClip, 0, len(result.Plan.Segments))
	for _, segment := range result.Plan.Segments {
		artifacts := bySegment[segment.ID]
		if len(artifacts) == 0 {
			warnings = append(warnings, fmt.Sprintf("segment %s missing composed input clip", segment.ID))
			continue
		}
		sort.SliceStable(artifacts, func(i, j int) bool {
			return artifacts[i].Path < artifacts[j].Path
		})
		if len(artifacts) > 1 {
			warnings = append(warnings, fmt.Sprintf("segment %s has %d composed input clips; using %s", segment.ID, len(artifacts), artifacts[0].Path))
		}
		clips = append(clips, SegmentClip{
			SegmentID:       segment.ID,
			Path:            artifacts[0].Path,
			DurationSeconds: artifacts[0].DurationSeconds,
		})
	}
	return clips, warnings
}

func ComposeConcat(ctx context.Context, ffmpegPath string, clips []SegmentClip, outputPath, workDir string) error {
	if ffmpegPath == "" {
		return fmt.Errorf("ffmpeg path is required")
	}
	if len(clips) == 0 {
		return fmt.Errorf("at least one clip is required")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	if workDir == "" {
		workDir = filepath.Dir(outputPath)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return err
	}
	listPath := filepath.Join(workDir, "concat-list.txt")
	if err := os.WriteFile(listPath, []byte(ConcatList(clips)), 0o644); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-y",
		"-v", "error",
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-vf", "fps=60,format=yuv420p",
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "18",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		outputPath,
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg concat: %w", err)
	}
	return nil
}

func ConcatList(clips []SegmentClip) string {
	var sb strings.Builder
	for _, clip := range clips {
		sb.WriteString("file '")
		sb.WriteString(escapeConcatPath(filepath.ToSlash(clip.Path)))
		sb.WriteString("'\n")
	}
	return sb.String()
}

func escapeConcatPath(path string) string {
	return strings.ReplaceAll(path, "'", "'\\''")
}
