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
	width, height := outputDimensions(short)
	clauses := []string{}
	concatLabels := []string{}
	concatCount := 0
	for i, part := range short.Parts {
		if part.GapBeforeSeconds > 0 {
			gapV := fmt.Sprintf("gapv%d", i)
			gapA := fmt.Sprintf("gapa%d", i)
			clauses = append(clauses,
				fmt.Sprintf("color=c=black:s=%dx%d:r=%d:d=%.3f[%s]", width, height, outputFPS(short), part.GapBeforeSeconds, gapV),
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
	killfeeds := killfeedEffects(short.Effects)
	if len(images) == 0 && len(killfeeds) == 0 {
		clauses = append(clauses, fmt.Sprintf("[catv]%s[v]", VideoFilter(short)))
	} else {
		clauses = append(clauses, fmt.Sprintf("[catv]%s[vbase]", VideoFilter(short)))
		current := "vbase"
		for i, effect := range killfeeds {
			partIndex, sampleSeconds := killfeedSamplePart(&short, effect)
			if partIndex < 0 {
				partIndex = 0
			}
			killfeedLabel := fmt.Sprintf("kf%d", i)
			next := fmt.Sprintf("vkf%d", i)
			clauses = append(clauses,
				fmt.Sprintf("[%d:v]%s[%s]", partIndex, compilationKillfeedCropFilter(effect, short, sampleSeconds), killfeedLabel),
				fmt.Sprintf("[%s][%s]overlay=x=%s:y=%s:format=auto:enable='%s'[%s]",
					current,
					killfeedLabel,
					effectPosition(effect.X, "W-w-18"),
					effectPosition(effect.Y, "438"),
					betweenExpression(effect.StartSeconds, effect.EndSeconds),
					next,
				),
			)
			current = next
		}
		if len(images) > 0 {
			imageInputStart := len(short.Parts)
			if short.MusicPath != "" {
				imageInputStart++
			}
			clauses = appendImageOverlayClauses(clauses, current, imageInputStart, images, short, "vimages")
			current = "vimages"
		}
		clauses = append(clauses, fmt.Sprintf("[%s]format=yuv420p[v]", current))
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

// compilationPartIndexAt finds the part whose compiled-timeline window covers
// the killfeed effect, so the overlay crops death notices from the source
// footage that is actually on screen. Falls back to the last part started
// before the effect (effects can outlive their segment into a gap).
func compilationPartIndexAt(parts []ShortPart, effect Effect) int {
	at := effect.AtSeconds
	if at == 0 {
		at = effect.StartSeconds
	}
	index := 0
	for i, part := range parts {
		if part.TimelineStartSeconds > at {
			break
		}
		index = i
	}
	return index
}

// compilationKillfeedCropFilter crops the killfeed region from the probed
// source frame and freezes it for the whole compiled timeline. The notice
// background is translucent, so playing it live would carry moving source
// footage inside the overlay; a frozen frame reads as a static badge.
func compilationKillfeedCropFilter(effect Effect, short ShortEdit, sampleSeconds float64) string {
	cropWidth := effect.CropWidth
	if cropWidth == 0 {
		cropWidth = 360
	}
	cropHeight := effect.CropHeight
	if cropHeight == 0 {
		cropHeight = 110
	}
	filters := []string{
		scaleFilter("1080", short),
		fmt.Sprintf("crop=%d:%d:%d:%d", cropWidth, cropHeight, effect.CropX, effect.CropY),
		sourceCropScaleFilter(effect),
		fmt.Sprintf("trim=start=%.3f:duration=0.050", sampleSeconds),
		"loop=loop=-1:size=1:start=0",
		fmt.Sprintf("setpts=N/%d/TB", outputFPS(short)),
	}
	if short.DurationSeconds > 0 {
		filters = append(filters, fmt.Sprintf("trim=duration=%.3f", short.DurationSeconds))
	}
	filters = append(filters, gradeFilters(short.Effects)...)
	// The notice background is translucent, so the frozen frame still bakes
	// in source-world pixels. Crushing the shadows flattens them into a
	// uniform dark backing while the bright text and icons stay untouched.
	filters = append(filters, "curves=all='0/0 0.35/0.08 1/1'")
	filters = append(filters, "format=rgba")
	filters = append(filters, overlayFadeFilters(effect)...)
	return strings.Join(filters, ",")
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
	width, height := outputDimensions(short)
	filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,setsar=1", width, height, width, height)
	if short.HQFilters {
		filter = fmt.Sprintf("thumbnail=30,scale=%d:%d:force_original_aspect_ratio=increase:flags=%s,crop=%d:%d,setsar=1", width, height, hqScaleFlags(short), width, height)
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
	tileW, tileH := 360, 640
	if isLandscapeOutput(short) {
		tileW, tileH = 640, 360
	}
	return []string{
		ffmpegPath,
		"-y",
		"-v", "error",
		"-i", short.Output,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("fps=2,scale=%d:%d:force_original_aspect_ratio=increase:flags=%s,crop=%d:%d,setsar=1,tile=3x3", tileW, tileH, hqScaleFlags(short), tileW, tileH),
		"-q:v", "3",
		short.CoverSheetPath,
	}
}

func BuildQualityCheckFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	filters := []string{"blackdetect=d=0.40:pix_th=0.10", "freezedetect=n=-60dB:d=1"}
	if !presetUsesFullFrame(short.Preset) {
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
