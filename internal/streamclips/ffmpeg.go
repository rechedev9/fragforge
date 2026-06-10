package streamclips

import (
	"fmt"
	"strconv"
)

const (
	outputWidth       = 1080
	faceOutputHeight  = 520
	gameOutputHeight  = 1400
	outputFPS         = 60
	defaultVideoCRF   = 18
	defaultAACBitrate = "192k"
	defaultPreset     = "slow"
)

func BuildFFmpegArgs(sourcePath, outputPath string, plan EditPlan, clip ClipRange) ([]string, error) {
	plan = NormalizeEditPlan(plan)
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if err := clip.Validate(); err != nil {
		return nil, err
	}
	duration := clip.EndSeconds - clip.StartSeconds
	filter := fmt.Sprintf(
		"[0:v]split=2[facein][gamein];"+
			"[facein]%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d[face];"+
			"[gamein]%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d[game];"+
			"[face][game]vstack=inputs=2,fps=%d,format=yuv420p[v]",
		cropFilter(plan.FaceCrop),
		outputWidth, faceOutputHeight, outputWidth, faceOutputHeight,
		cropFilter(plan.GameplayCrop),
		outputWidth, gameOutputHeight, outputWidth, gameOutputHeight,
		outputFPS,
	)
	return []string{
		"-y",
		"-ss", secondsArg(clip.StartSeconds),
		"-t", secondsArg(duration),
		"-i", sourcePath,
		"-filter_complex", filter,
		"-map", "[v]",
		"-map", "0:a?",
		"-c:v", "libx264",
		"-preset", defaultPreset,
		"-crf", strconv.Itoa(defaultVideoCRF),
		"-c:a", "aac",
		"-b:a", defaultAACBitrate,
		"-movflags", "+faststart",
		outputPath,
	}, nil
}

func cropFilter(c CropRect) string {
	return fmt.Sprintf("crop=w=iw*%s:h=ih*%s:x=iw*%s:y=ih*%s",
		floatArg(c.Width), floatArg(c.Height), floatArg(c.X), floatArg(c.Y))
}

func secondsArg(v float64) string {
	return strconv.FormatFloat(v, 'f', 3, 64)
}

func floatArg(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}
