package editor

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func BuildFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if short.PlayerImage != "" {
		return BuildPremiumPlayerFFmpegCommand(ffmpegPath, short)
	}
	return []string{
		ffmpegPath,
		"-y",
		"-v", "error",
		"-i", short.Input,
		"-map", "0:v:0",
		"-map", "0:a?",
		"-vf", VideoFilter(short),
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "18",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		short.Output,
	}
}

func BuildPremiumPlayerFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return []string{
		ffmpegPath,
		"-y",
		"-v", "error",
		"-i", short.Input,
		"-loop", "1",
		"-i", short.PlayerImage,
		"-filter_complex", PremiumPlayerFilter(short),
		"-map", "[v]",
		"-map", "0:a?",
		"-c:v", "libx264",
		"-preset", "fast",
		"-crf", "18",
		"-c:a", "aac",
		"-b:a", "192k",
		"-movflags", "+faststart",
		"-shortest",
		short.Output,
	}
}

func BuildCoverFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return []string{
		ffmpegPath,
		"-y",
		"-v", "error",
		"-ss", fmt.Sprintf("%.3f", short.CoverTimeSeconds),
		"-i", short.Output,
		"-frames:v", "1",
		"-vf", "scale=1080:1920:force_original_aspect_ratio=increase,crop=1080:1920,setsar=1",
		"-q:v", "2",
		short.CoverPath,
	}
}

func runFFmpeg(ctx context.Context, command []string, label string) error {
	if len(command) == 0 || command[0] == "" {
		return fmt.Errorf("ffmpeg command is empty")
	}
	if label == "" {
		label = "command"
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("ffmpeg %s: %w: %s", label, err, msg)
		}
		return fmt.Errorf("ffmpeg %s: %w", label, err)
	}
	return nil
}
