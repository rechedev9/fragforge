package streamclips

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	outputFPS         = 60
	defaultVideoCRF   = 18
	defaultAACBitrate = "192k"
	defaultPreset     = "slow"
	bannerHeight      = 96
	bannerColor       = "0x9146ff"
	bannerAccentColor = "0x5b1ba9"

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
	filter := buildFilterGraph(layout, plan, in.BannerFontPath)

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
func buildFilterGraph(layout LayoutVariant, plan EditPlan, bannerFontPath string) string {
	tail := ""
	if plan.StreamerBanner.Nick != "" {
		tail = streamerBannerFilter(layout, plan.StreamerBanner.Nick, bannerFontPath) + ","
	}
	if plan.Effects.Grade {
		tail += gradeFilter + ","
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

// streamerBannerFilter draws a full-width branded strip over the facecam /
// gameplay seam without changing either band's geometry. Full-frame layouts
// have no seam, so the same strip sits one fifth of the way down the frame.
func streamerBannerFilter(layout LayoutVariant, nick, fontPath string) string {
	centerY := layout.FaceOutputHeight
	if layout.FullFrame {
		centerY = layout.GameOutputHeight / 5
	}
	top := centerY - bannerHeight/2
	iconTop := top + 27

	return fmt.Sprintf(
		"drawbox=x=0:y=%d:w=%d:h=%d:color=%s:t=fill,"+
			"drawbox=x=0:y=%d:w=116:h=%d:color=%s:t=fill,"+
			"drawbox=x=34:y=%d:w=48:h=36:color=white:t=fill,"+
			"drawbox=x=41:y=%d:w=34:h=22:color=%s:t=fill,"+
			"drawbox=x=43:y=%d:w=11:h=9:color=white:t=fill,"+
			"drawbox=x=50:y=%d:w=5:h=12:color=white:t=fill,"+
			"drawbox=x=64:y=%d:w=5:h=12:color=white:t=fill,"+
			"drawtext=fontfile='%s':text='%s':fontcolor=white:fontsize=52:borderw=1:bordercolor=%s:"+
			"shadowcolor=black@0.35:shadowx=2:shadowy=2:x=140:y=%d+(%d-text_h)/2",
		top, layout.OutputWidth, bannerHeight, bannerColor,
		top, bannerHeight, bannerAccentColor,
		iconTop,
		iconTop+7, bannerAccentColor,
		iconTop+34,
		iconTop+11,
		iconTop+11,
		ffmpegFilterPath(fontPath), nick, bannerAccentColor, top, bannerHeight,
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
