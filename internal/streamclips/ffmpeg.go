package streamclips

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	outputFPS          = 60
	defaultVideoCRF    = 18
	defaultAACBitrate  = "192k"
	defaultPreset      = "slow"
	bannerHeight       = 96
	bannerSlideSeconds = 0.35
	bannerColor        = "0x9146ff"
	bannerAccentColor  = "0x5b1ba9"

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
	BannerFontPath string // resolved bold font file; required when the banner has a nick
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
	if plan.StreamerBanner.Nick != "" && in.BannerFontPath == "" {
		return nil, fmt.Errorf("streamer banner font path is required")
	}
	duration := clip.EndSeconds - clip.StartSeconds
	filter := buildFilterGraph(layout, plan, in.BannerFontPath, duration)

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
func buildFilterGraph(layout LayoutVariant, plan EditPlan, bannerFontPath string, duration float64) string {
	tail := ""
	if plan.Effects.Grade {
		tail += gradeFilter + ","
	}
	tail += fmt.Sprintf("fps=%d,format=yuv420p[v]", outputFPS)

	outputLabel := ""
	if plan.StreamerBanner.Nick != "" {
		outputLabel = "[content]"
	}

	var content string
	if layout.FullFrame {
		content = fmt.Sprintf(
			"[0:v]%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d%s",
			cropFilter(plan.GameplayCrop),
			layout.OutputWidth, layout.GameOutputHeight, layout.OutputWidth, layout.GameOutputHeight,
			outputLabel,
		)
	} else {
		content = fmt.Sprintf(
			"[0:v]split=2[facein][gamein];"+
				"[facein]%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d[face];"+
				"[gamein]%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d[game];"+
				"[face][game]vstack=inputs=2%s",
			cropFilter(plan.FaceCrop),
			layout.OutputWidth, layout.FaceOutputHeight, layout.OutputWidth, layout.FaceOutputHeight,
			cropFilter(plan.GameplayCrop),
			layout.OutputWidth, layout.GameOutputHeight, layout.OutputWidth, layout.GameOutputHeight,
			outputLabel,
		)
	}

	if plan.StreamerBanner.Nick == "" {
		return content + "," + tail
	}
	return content + ";" + streamerBannerFilter(layout, plan.StreamerBanner, bannerFontPath, duration) + ";[bannered]" + tail
}

// streamerBannerFilter builds the strip independently and overlays it on the
// completed layout so the entire banner can move as one unit.
func streamerBannerFilter(layout LayoutVariant, banner StreamerBannerPlan, fontPath string, duration float64) string {
	centerY := layout.FaceOutputHeight
	if layout.FullFrame {
		centerY = layout.GameOutputHeight / 5
	}
	if banner.PositionY != nil {
		outputHeight := layout.FaceOutputHeight + layout.GameOutputHeight
		centerY = int(math.Round(*banner.PositionY * float64(outputHeight)))
	}
	top := centerY - bannerHeight/2
	x := "0"
	if banner.SlideEnabled {
		phase := math.Min(bannerSlideSeconds, duration/2)
		exitStart := duration - phase
		x = fmt.Sprintf(
			`if(lt(t\,%s)\,-w*(1-t/%s)\,if(lt(t\,%s)\,0\,-w*(t-%s)/%s))`,
			floatArg(phase), floatArg(phase), floatArg(exitStart), floatArg(exitStart), floatArg(phase),
		)
	}

	return fmt.Sprintf(
		"color=c=%s:s=%dx%d:r=%d:d=%s,"+
			"setpts=PTS-STARTPTS,"+
			"drawbox=x=0:y=0:w=116:h=%d:color=%s:t=fill,"+
			"drawbox=x=34:y=27:w=48:h=36:color=white:t=fill,"+
			"drawbox=x=41:y=34:w=34:h=22:color=%s:t=fill,"+
			"drawbox=x=43:y=61:w=11:h=9:color=white:t=fill,"+
			"drawbox=x=50:y=38:w=5:h=12:color=white:t=fill,"+
			"drawbox=x=64:y=38:w=5:h=12:color=white:t=fill,"+
			"drawtext=fontfile='%s':text='%s':fontcolor=white:fontsize=52:borderw=1:bordercolor=%s:"+
			"shadowcolor=black@0.35:shadowx=2:shadowy=2:x=140:y=(%d-text_h)/2[banner];"+
			"[content]setpts=PTS-STARTPTS[contentpts];"+
			"[contentpts][banner]overlay=x='%s':y=%d:eval=frame:eof_action=pass:shortest=0[bannered]",
		bannerColor, layout.OutputWidth, bannerHeight, outputFPS, secondsArg(duration),
		bannerHeight, bannerAccentColor,
		bannerAccentColor,
		ffmpegFilterPath(fontPath), banner.Nick, bannerAccentColor, bannerHeight,
		x, top,
	)
}

// FindBannerFont returns the first supported bold system font available on
// the current host. An explicit font file avoids drawtext's dependency on a
// working Fontconfig installation, which is commonly absent on Windows.
func FindBannerFont() string {
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		windowsDir := os.Getenv("WINDIR")
		if windowsDir == "" {
			windowsDir = `C:\Windows`
		}
		candidates = []string{
			filepath.Join(windowsDir, "Fonts", "arialbd.ttf"),
			filepath.Join(windowsDir, "Fonts", "segoeuib.ttf"),
		}
	case "darwin":
		candidates = []string{
			"/System/Library/Fonts/Supplemental/Arial Bold.ttf",
			"/System/Library/Fonts/Supplemental/Arial.ttf",
		}
	default:
		candidates = []string{
			"/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf",
			"/usr/share/fonts/truetype/liberation2/LiberationSans-Bold.ttf",
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func ffmpegFilterPath(value string) string {
	value = filepath.ToSlash(value)
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, ":", `\:`)
	return strings.ReplaceAll(value, "'", `\'`)
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
