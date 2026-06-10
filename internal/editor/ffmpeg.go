package editor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const filterComplexScriptThreshold = 4096

func BuildFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	if len(short.Parts) > 0 {
		return BuildCompilationFFmpegCommand(ffmpegPath, short)
	}
	if short.PlayerImage != "" {
		return BuildPremiumPlayerFFmpegCommand(ffmpegPath, short)
	}
	switch presetFilterKind(short.Preset) {
	case FilterKindViralSquare:
		return BuildViralSquareFFmpegCommand(ffmpegPath, short)
	case FilterKindSmokeLineups:
		if len(short.Smokes) > 0 {
			return BuildSmokeLineupFFmpegCommand(ffmpegPath, short)
		}
	}
	if short.MusicPath != "" {
		return BuildMusicFFmpegCommand(ffmpegPath, short)
	}
	command := []string{
		ffmpegPath,
		"-y",
		"-v", "error",
		"-i", short.Input,
		"-map", "0:v:0",
		"-map", "0:a?",
		"-vf", VideoFilter(short),
		"-c:v", "libx264",
		"-preset", videoPresetForCommand(short.VideoPreset),
		"-crf", fmt.Sprintf("%d", videoCRFForCommand(short.VideoCRF)),
	}
	command = appendVideoEncodeArgs(command, short)
	command = appendAudioEncodeArgs(command, short)
	return append(command,
		"-movflags", "+faststart",
		short.Output,
	)
}

func BuildViralSquareFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	command := []string{
		ffmpegPath,
		"-y",
		"-v", "error",
		"-i", short.Input,
	}
	for _, effect := range imageEffects(short.Effects) {
		command = append(command, "-i", effect.Path)
	}
	command = append(command,
		"-filter_complex", ViralSquareFilter(short),
		"-map", "[v]",
		"-map", "0:a?",
		"-c:v", "libx264",
		"-preset", videoPresetForCommand(short.VideoPreset),
		"-crf", fmt.Sprintf("%d", videoCRFForCommand(short.VideoCRF)),
	)
	command = appendVideoEncodeArgs(command, short)
	command = appendAudioEncodeArgs(command, short)
	return append(command,
		"-movflags", "+faststart",
		short.Output,
	)
}

func BuildMusicFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	audioOut := "[game][music]amix=inputs=2:duration=first:dropout_transition=0,aresample=48000"
	if short.AudioNormalize {
		audioOut += ",loudnorm=I=-16:TP=-1.5:LRA=11"
	}
	audioOut += "[a]"
	filter := fmt.Sprintf(
		"[0:v]%s[v];[0:a]volume=0.20[game];[1:a]volume=1.00[music];%s",
		VideoFilter(short),
		audioOut,
	)
	command := []string{
		ffmpegPath,
		"-y",
		"-v", "error",
		"-i", short.Input,
		"-stream_loop", "-1",
		"-i", short.MusicPath,
		"-filter_complex", filter,
		"-map", "[v]",
		"-map", "[a]",
		"-c:v", "libx264",
		"-preset", videoPresetForCommand(short.VideoPreset),
		"-crf", fmt.Sprintf("%d", videoCRFForCommand(short.VideoCRF)),
	}
	command = appendVideoEncodeArgs(command, short)
	command = appendAudioCodecArgs(command)
	return append(command,
		"-movflags", "+faststart",
		"-shortest",
		short.Output,
	)
}

func appendAudioEncodeArgs(command []string, short ShortEdit) []string {
	if short.AudioNormalize {
		command = append(command, "-af", "loudnorm=I=-16:TP=-1.5:LRA=11")
	}
	return appendAudioCodecArgs(command)
}

func appendAudioCodecArgs(command []string) []string {
	return append(command,
		"-c:a", "aac",
		"-b:a", "192k",
	)
}

func BuildSmokeLineupFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	command := []string{
		ffmpegPath,
		"-y",
		"-v", "error",
		"-i", short.Input,
		"-filter_complex", SmokeLineupSlowMotionFilter(short),
		"-map", "[v]",
		"-map", "[a]",
		"-c:v", "libx264",
		"-preset", videoPresetForCommand(short.VideoPreset),
		"-crf", fmt.Sprintf("%d", videoCRFForCommand(short.VideoCRF)),
	}
	command = appendVideoEncodeArgs(command, short)
	command = appendAudioCodecArgs(command)
	return append(command,
		"-movflags", "+faststart",
		short.Output,
	)
}

func BuildPremiumPlayerFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	command := []string{
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
		"-preset", videoPresetForCommand(short.VideoPreset),
		"-crf", fmt.Sprintf("%d", videoCRFForCommand(short.VideoCRF)),
	}
	command = appendVideoEncodeArgs(command, short)
	command = appendAudioEncodeArgs(command, short)
	return append(command,
		"-movflags", "+faststart",
		"-shortest",
		short.Output,
	)
}

func BuildCompilationFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	command := []string{
		ffmpegPath,
		"-y",
		"-v", "error",
	}
	for _, part := range short.Parts {
		command = append(command, "-i", part.Input)
	}
	if short.MusicPath != "" {
		command = append(command, "-stream_loop", "-1", "-i", short.MusicPath)
	}
	for _, effect := range imageEffects(short.Effects) {
		command = append(command, "-i", effect.Path)
	}
	command = append(command,
		"-filter_complex", CompilationFilter(short),
		"-map", "[v]",
		"-map", "[a]",
		"-c:v", "libx264",
		"-preset", videoPresetForCommand(short.VideoPreset),
		"-crf", fmt.Sprintf("%d", videoCRFForCommand(short.VideoCRF)),
	)
	command = appendVideoEncodeArgs(command, short)
	command = appendAudioCodecArgs(command)
	command = append(command, "-movflags", "+faststart")
	if short.MusicPath != "" {
		command = append(command, "-shortest")
	}
	return append(command, short.Output)
}

func CompilationFilter(short ShortEdit) string {
	partShort := short
	partShort.Effects = nil
	partShort.Parts = nil
	clauses := []string{}
	concatLabels := []string{}
	concatCount := 0
	for i, part := range short.Parts {
		if part.GapBeforeSeconds > 0 {
			gapV := fmt.Sprintf("gapv%d", i)
			gapA := fmt.Sprintf("gapa%d", i)
			clauses = append(clauses,
				fmt.Sprintf("color=c=black:s=1080x1920:r=%d:d=%.3f[%s]", outputFPS(short), part.GapBeforeSeconds, gapV),
				fmt.Sprintf("anullsrc=channel_layout=stereo:sample_rate=48000:d=%.3f[%s]", part.GapBeforeSeconds, gapA),
			)
			concatLabels = append(concatLabels, "["+gapV+"]["+gapA+"]")
			concatCount++
		}
		videoLabel := fmt.Sprintf("pv%d", i)
		audioLabel := fmt.Sprintf("pa%d", i)
		clauses = append(clauses,
			fmt.Sprintf("[%d:v]%s[%s]", i, VideoFilter(partShort), videoLabel),
			fmt.Sprintf("[%d:a]aformat=channel_layouts=stereo,aresample=48000,asetpts=PTS-STARTPTS[%s]", i, audioLabel),
		)
		concatLabels = append(concatLabels, "["+videoLabel+"]["+audioLabel+"]")
		concatCount++
	}
	clauses = append(clauses, fmt.Sprintf("%sconcat=n=%d:v=1:a=1[catv][gamea]", strings.Join(concatLabels, ""), concatCount))
	images := imageEffects(short.Effects)
	if len(images) == 0 {
		clauses = append(clauses, fmt.Sprintf("[catv]%s[v]", VideoFilter(short)))
	} else {
		clauses = append(clauses, fmt.Sprintf("[catv]%s[vbase]", VideoFilter(short)))
		imageInputStart := len(short.Parts)
		if short.MusicPath != "" {
			imageInputStart++
		}
		clauses = appendImageOverlayClauses(clauses, "vbase", imageInputStart, images, short, "vimages")
		clauses = append(clauses, "[vimages]format=yuv420p[v]")
	}
	if short.MusicPath != "" {
		musicInput := len(short.Parts)
		audio := fmt.Sprintf("[%d:a]volume=1.00[music];[gamea]volume=0.20[game];[game][music]amix=inputs=2:duration=first:dropout_transition=0,aresample=48000", musicInput)
		if short.AudioNormalize {
			audio += ",loudnorm=I=-16:TP=-1.5:LRA=11"
		}
		clauses = append(clauses, audio+"[a]")
	} else if short.AudioNormalize {
		clauses = append(clauses, "[gamea]loudnorm=I=-16:TP=-1.5:LRA=11[a]")
	} else {
		clauses = append(clauses, "[gamea]anull[a]")
	}
	return strings.Join(clauses, ";")
}

func appendVideoEncodeArgs(command []string, short ShortEdit) []string {
	renderPreset, ok := PresetByName(short.Preset)
	if !ok || !renderPreset.MasteringBT709 {
		return command
	}
	return append(command,
		"-profile:v", "high",
		"-color_primaries", "bt709",
		"-color_trc", "bt709",
		"-colorspace", "bt709",
		"-x264-params", "colorprim=bt709:transfer=bt709:colormatrix=bt709",
	)
}

func videoCRFForCommand(crf int) int {
	if crf <= 0 {
		return DefaultVideoCRF
	}
	return crf
}

func videoPresetForCommand(preset string) string {
	preset = strings.ToLower(strings.TrimSpace(preset))
	if preset == "" {
		return DefaultVideoPreset
	}
	return preset
}

func BuildCoverFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	filter := "scale=1080:1920:force_original_aspect_ratio=increase,crop=1080:1920,setsar=1"
	if short.HQFilters {
		filter = "thumbnail=30,scale=1080:1920:force_original_aspect_ratio=increase:flags=" + hqScaleFlags(short) + ",crop=1080:1920,setsar=1"
	}
	return []string{
		ffmpegPath,
		"-y",
		"-v", "error",
		"-ss", fmt.Sprintf("%.3f", short.CoverTimeSeconds),
		"-i", short.Output,
		"-frames:v", "1",
		"-vf", filter,
		"-q:v", "2",
		short.CoverPath,
	}
}

func BuildCoverSheetFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return []string{
		ffmpegPath,
		"-y",
		"-v", "error",
		"-i", short.Output,
		"-frames:v", "1",
		"-vf", "fps=2,scale=360:640:force_original_aspect_ratio=increase:flags=" + hqScaleFlags(short) + ",crop=360:640,setsar=1,tile=3x3",
		"-q:v", "3",
		short.CoverSheetPath,
	}
}

func BuildQualityCheckFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	filters := []string{"blackdetect=d=0.40:pix_th=0.10", "freezedetect=n=-60dB:d=1"}
	if presetFilterKind(short.Preset) != FilterKindFullFrame {
		filters = append(filters, "cropdetect=24:16:0")
	}
	return []string{
		ffmpegPath,
		"-hide_banner",
		"-nostats",
		"-v", "info",
		"-i", short.Output,
		"-vf", strings.Join(filters, ","),
		"-an",
		"-f", "null",
		"-",
	}
}

func runFFmpeg(ctx context.Context, command []string, label string) error {
	_, err := runFFmpegOutput(ctx, command, label)
	return err
}

func runFFmpegOutput(ctx context.Context, command []string, label string) (string, error) {
	if len(command) == 0 || command[0] == "" {
		return "", fmt.Errorf("ffmpeg command is empty")
	}
	if label == "" {
		label = "command"
	}
	command, cleanup, err := commandWithFilterComplexScript(command)
	if err != nil {
		return "", err
	}
	defer cleanup()
	// #nosec G204 -- commands are generated by this package as argument slices, not shell strings.
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		msg := strings.TrimSpace(output)
		if msg != "" {
			return output, fmt.Errorf("ffmpeg %s: %w: %s", label, err, msg)
		}
		return output, fmt.Errorf("ffmpeg %s: %w", label, err)
	}
	return output, nil
}

func commandWithFilterComplexScript(command []string) ([]string, func(), error) {
	for i := 0; i < len(command)-1; i++ {
		if command[i] != "-filter_complex" || len(command[i+1]) <= filterComplexScriptThreshold {
			continue
		}
		f, err := os.CreateTemp("", "zv-filter-complex-*.txt")
		if err != nil {
			return nil, func() {}, fmt.Errorf("creating filter_complex script: %w", err)
		}
		path := f.Name()
		if _, err := f.WriteString(command[i+1]); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return nil, func() {}, fmt.Errorf("writing filter_complex script: %w", err)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(path)
			return nil, func() {}, fmt.Errorf("closing filter_complex script: %w", err)
		}

		next := append([]string(nil), command...)
		next[i] = "-filter_complex_script"
		next[i+1] = path
		return next, func() { _ = os.Remove(path) }, nil
	}
	return command, func() {}, nil
}
