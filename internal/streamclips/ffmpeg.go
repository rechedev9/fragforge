package streamclips

import (
	"fmt"
	"strconv"
)

const (
	outputFPS         = 60
	defaultVideoCRF   = 18
	defaultAACBitrate = "192k"
	defaultPreset     = "slow"

	// gradeFilter is the light contrast/saturation lift EffectsPlan.Grade
	// applies — the same restrained look FragForge's viral presets use.
	gradeFilter = "eq=contrast=1.05:saturation=1.15"
)

// FFmpegInputs carries the machine-resolved inputs for one clip render. The
// edit plan stores only the music track KEY; the worker resolves it to an
// on-disk path (MusicPath) and reports whether the probed source has an audio
// stream, which decides how music is mixed.
type FFmpegInputs struct {
	SourcePath     string
	OutputPath     string
	MusicPath      string // resolved track file; empty renders without music
	SourceHasAudio bool
}

func BuildFFmpegArgs(in FFmpegInputs, plan EditPlan, clip ClipRange) ([]string, error) {
	plan = NormalizeEditPlan(plan)
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if err := clip.Validate(); err != nil {
		return nil, err
	}
	layout, ok := VariantByName(plan.Variant)
	if !ok {
		return nil, unknownVariantError(plan.Variant)
	}
	duration := clip.EndSeconds - clip.StartSeconds
	filter := buildFilterGraph(layout, plan)

	args := []string{
		"-y",
		"-ss", secondsArg(clip.StartSeconds),
		"-t", secondsArg(duration),
		"-i", in.SourcePath,
	}
	audioMap := "0:a?"
	shortest := false
	if in.MusicPath != "" {
		// Loop the track so it always covers the clip; amix/-shortest bound it.
		args = append(args, "-stream_loop", "-1", "-i", in.MusicPath)
		volume := plan.Music.Volume
		if volume == 0 {
			volume = defaultMusicVolume
		}
		if in.SourceHasAudio {
			filter += fmt.Sprintf(";[1:a]volume=%s[bgm];[0:a][bgm]amix=inputs=2:duration=first:dropout_transition=0:normalize=0[a]", floatArg(volume))
		} else {
			// No original audio to bound the mix: -shortest ends with the video.
			filter += fmt.Sprintf(";[1:a]volume=%s[a]", floatArg(volume))
			shortest = true
		}
		audioMap = "[a]"
	}

	args = append(args,
		"-filter_complex", filter,
		"-map", "[v]",
		"-map", audioMap,
		"-c:v", "libx264",
		"-preset", defaultPreset,
		"-crf", strconv.Itoa(defaultVideoCRF),
		"-c:a", "aac",
		"-b:a", defaultAACBitrate,
		"-movflags", "+faststart",
	)
	if shortest {
		args = append(args, "-shortest")
	}
	return append(args, in.OutputPath), nil
}

// buildFilterGraph renders the split/scale/stack filtergraph for a facecam
// layout, or a single crop/scale chain for a full-frame (no facecam) layout,
// with the optional grade inserted before the fps/format tail.
func buildFilterGraph(layout LayoutVariant, plan EditPlan) string {
	tail := ""
	if plan.Effects.Grade {
		tail = gradeFilter + ","
	}
	tail += fmt.Sprintf("fps=%d,format=yuv420p[v]", outputFPS)

	if layout.FullFrame {
		return fmt.Sprintf(
			"[0:v]%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,%s",
			cropFilter(plan.GameplayCrop),
			layout.OutputWidth, layout.GameOutputHeight, layout.OutputWidth, layout.GameOutputHeight,
			tail,
		)
	}
	return fmt.Sprintf(
		"[0:v]split=2[facein][gamein];"+
			"[facein]%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d[face];"+
			"[gamein]%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d[game];"+
			"[face][game]vstack=inputs=2,%s",
		cropFilter(plan.FaceCrop),
		layout.OutputWidth, layout.FaceOutputHeight, layout.OutputWidth, layout.FaceOutputHeight,
		cropFilter(plan.GameplayCrop),
		layout.OutputWidth, layout.GameOutputHeight, layout.OutputWidth, layout.GameOutputHeight,
		tail,
	)
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
