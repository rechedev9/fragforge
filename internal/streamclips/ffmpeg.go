package streamclips

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/rechedev9/fragforge/internal/mediafont"
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
	// killfeedRowOutputHeight keeps each notice legible on the 1080px-wide output.
	killfeedRowOutputHeight = 40
	// KillfeedSampleDelaySeconds delays cue-frame sampling until the notice
	// highlight ring is fully drawn. killfeedLeadTime must stay at least this long.
	KillfeedSampleDelaySeconds = 0.35
	killfeedLeadTime           = 0.35
	killfeedTrailTime          = 2.8

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
	KillfeedRows   [][]NoticeRow // index-aligned with the normalized clip's killfeed cues
}

func BuildFFmpegArgs(in FFmpegInputs, plan EditPlan, clip ClipRange) ([]string, error) {
	plan = NormalizeEditPlan(plan)
	clip = normalizeClipRange(clip)
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if err := clip.Validate(); err != nil {
		return nil, err
	}
	if plan.KillfeedCrop == nil && len(clip.KillfeedSeconds) > 0 {
		return nil, fmt.Errorf("clip %s has killfeed_seconds but killfeed_crop is not configured", clip.ID)
	}
	layout, ok := VariantByName(plan.Variant)
	if !ok {
		return nil, unknownVariantError(plan.Variant)
	}
	if plan.StreamerBanner.Nick != "" && in.BannerFontPath == "" {
		return nil, fmt.Errorf("streamer banner font path is required")
	}
	duration := clip.EndSeconds - clip.StartSeconds
	filter := buildFilterGraph(layout, plan, clip, in.KillfeedRows, in.BannerFontPath, duration)

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
// layout, or a single crop/scale chain for a full-frame (no facecam) layout.
// Plans without detected killfeed rows retain the original graph byte-for-byte.
// Detected rows each get a dedicated source branch so every notice can be
// frozen independently before the ordered overlay chain.
func buildFilterGraph(layout LayoutVariant, plan EditPlan, clip ClipRange, killfeedRows [][]NoticeRow, bannerFontPath string, duration float64) string {
	if len(clip.KillfeedSeconds) == 0 || killfeedRowCount(killfeedRows, len(clip.KillfeedSeconds)) == 0 {
		return buildStandardFilterGraph(layout, plan, bannerFontPath, duration)
	}
	return buildKillfeedFilterGraph(layout, plan, clip, killfeedRows, bannerFontPath, duration)
}

func buildStandardFilterGraph(layout LayoutVariant, plan EditPlan, bannerFontPath string, duration float64) string {
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

func buildKillfeedFilterGraph(layout LayoutVariant, plan EditPlan, clip ClipRange, killfeedRows [][]NoticeRow, bannerFontPath string, duration float64) string {
	tail := ""
	if plan.Effects.Grade {
		tail += gradeFilter + ","
	}
	tail += fmt.Sprintf("fps=%d,format=yuv420p[v]", outputFPS)

	cueCount := len(clip.KillfeedSeconds)
	killfeedBranchCount := killfeedRowCount(killfeedRows, cueCount)
	layoutBranchCount := 1
	var sourceSplit strings.Builder
	if layout.FullFrame {
		fmt.Fprintf(&sourceSplit, "[0:v]split=%d[layoutin]", layoutBranchCount+killfeedBranchCount)
	} else {
		layoutBranchCount = 2
		fmt.Fprintf(&sourceSplit, "[0:v]split=%d[facein][gamein]", layoutBranchCount+killfeedBranchCount)
	}
	for i := range cueCount {
		for j := range killfeedRowsForCue(killfeedRows, i) {
			fmt.Fprintf(&sourceSplit, "[killfeedin%d_%d]", i, j)
		}
	}

	parts := make([]string, 0, killfeedBranchCount*2+5)
	parts = append(parts, sourceSplit.String())
	if layout.FullFrame {
		parts = append(parts, fmt.Sprintf(
			"[layoutin]%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d[layout]",
			cropFilter(plan.GameplayCrop),
			layout.OutputWidth, layout.GameOutputHeight, layout.OutputWidth, layout.GameOutputHeight,
		))
	} else {
		parts = append(parts,
			fmt.Sprintf(
				"[facein]%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d[face]",
				cropFilter(plan.FaceCrop),
				layout.OutputWidth, layout.FaceOutputHeight, layout.OutputWidth, layout.FaceOutputHeight,
			),
			fmt.Sprintf(
				"[gamein]%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d[game]",
				cropFilter(plan.GameplayCrop),
				layout.OutputWidth, layout.GameOutputHeight, layout.OutputWidth, layout.GameOutputHeight,
			),
			"[face][game]vstack=inputs=2[layout]",
		)
	}

	for i, cue := range clip.KillfeedSeconds {
		relative := cue - clip.StartSeconds
		rows := killfeedRowsForCue(killfeedRows, i)
		if len(rows) == 0 {
			continue
		}
		scale := killfeedRowScale(rows)
		for j, row := range rows {
			outWidth := int(math.Round(float64(row.Width) * scale))
			outWidth -= outWidth % 2
			if outWidth < 2 {
				outWidth = 2
			}
			parts = append(parts, fmt.Sprintf(
				"[killfeedin%d_%d]trim=start=%s,select='eq(n\\,0)',setpts=PTS-STARTPTS,"+
					"crop=%d:%d:%d:%d,scale=%d:-2:flags=lanczos,"+
					"tpad=stop_mode=clone:stop_duration=%s[killfeed%d_%d]",
				i, j, floatArg(relative),
				row.Width, row.Height, row.X, row.Y, outWidth,
				floatArg(duration), i, j,
			))
		}
	}

	overlayY := 64
	if !layout.FullFrame {
		overlayY = layout.FaceOutputHeight + 72
	}
	baseLabel := "layout"
	overlayIndex := 0
	for i, cue := range clip.KillfeedSeconds {
		relative := cue - clip.StartSeconds
		start := math.Max(0, relative-killfeedLeadTime)
		end := math.Min(duration, relative+killfeedTrailTime)
		rows := killfeedRowsForCue(killfeedRows, i)
		if len(rows) == 0 {
			continue
		}

		scale := killfeedRowScale(rows)
		for j, row := range rows {
			rowY := overlayY + int(math.Round(float64(row.Y-rows[0].Y)*scale))
			outputLabel := fmt.Sprintf("killfeeded%d_%d", i, j)
			if overlayIndex == killfeedBranchCount-1 {
				outputLabel = "content"
			}
			parts = append(parts, fmt.Sprintf(
				"[%s][killfeed%d_%d]overlay=x=W-w-24:y=%d:"+
					"enable='between(t\\,%s\\,%s)':eof_action=pass:shortest=0[%s]",
				baseLabel, i, j, rowY, floatArg(start), floatArg(end), outputLabel,
			))
			baseLabel = outputLabel
			overlayIndex++
		}
	}

	if plan.StreamerBanner.Nick == "" {
		parts = append(parts, "[content]"+tail)
	} else {
		parts = append(parts,
			streamerBannerFilter(layout, plan.StreamerBanner, bannerFontPath, duration),
			"[bannered]"+tail,
		)
	}
	return strings.Join(parts, ";")
}

func killfeedRowCount(rows [][]NoticeRow, cueCount int) int {
	count := 0
	for i := range cueCount {
		count += len(killfeedRowsForCue(rows, i))
	}
	return count
}

func killfeedRowsForCue(rows [][]NoticeRow, cueIndex int) []NoticeRow {
	if cueIndex >= len(rows) {
		return nil
	}
	return rows[cueIndex]
}

func killfeedRowScale(rows []NoticeRow) float64 {
	tallest := rows[0].Height
	for _, row := range rows[1:] {
		tallest = max(tallest, row.Height)
	}
	return float64(killfeedRowOutputHeight) / float64(tallest)
}

// streamerBannerFilter builds the strip independently and overlays it on the
// completed layout so the entire banner can move as one unit.
func streamerBannerFilter(layout LayoutVariant, banner StreamerBannerPlan, fontPath string, duration float64) string {
	outputHeight := layout.FaceOutputHeight + layout.GameOutputHeight
	centerY := int(math.Round(layout.DefaultBannerPositionY * float64(outputHeight)))
	if banner.PositionY != nil {
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

// FindBannerFont prefers the bundled Montserrat ExtraBold used across all
// generated text. System fonts remain an exceptional fallback when the
// embedded font cannot be written to the user cache.
func FindBannerFont() string {
	if fontPath, err := mediafont.Materialize(); err == nil {
		return fontPath
	}
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
