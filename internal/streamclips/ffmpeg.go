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
	// killfeedFrozenWidth is the on-output width of a frozen killfeed-crop strip.
	// It mirrors the web preview's KILLFEED_WIDTH so the preview matches the render.
	killfeedFrozenWidth = 620
	// killfeedNoticeStackGap is the vertical gap between stacked synthetic notices.
	killfeedNoticeStackGap = 8
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
	// KillfeedNoticePaths holds pre-rendered synthetic kill-notice PNG paths,
	// index-aligned with the normalized clip's killfeed cues. Each cue's list is
	// ordered top-first, and every PNG is streamclips.KillfeedNoticeHeight tall.
	// A cue with no paths falls back to a frozen crop of the killfeed region.
	KillfeedNoticePaths [][]string
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
	if n := len(in.KillfeedNoticePaths); n != 0 && n != len(clip.KillfeedSeconds) {
		return nil, fmt.Errorf(
			"clip %s killfeed notice paths length %d must be 0 or match %d killfeed cues",
			clip.ID, n, len(clip.KillfeedSeconds),
		)
	}
	layout, ok := VariantByName(plan.Variant)
	if !ok {
		return nil, unknownVariantError(plan.Variant)
	}
	if plan.StreamerBanner.Nick != "" && in.BannerFontPath == "" {
		return nil, fmt.Errorf("streamer banner font path is required")
	}
	duration := clip.EndSeconds - clip.StartSeconds

	// Notice PNGs are extra inputs after the source and the optional music input.
	noticeInputBase := 1
	if in.MusicPath != "" {
		noticeInputBase = 2
	}
	filter := buildFilterGraph(layout, plan, clip, in.KillfeedNoticePaths, in.BannerFontPath, duration, noticeInputBase)

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
	// Loop each notice PNG so it always covers the clip; the overlay enable window
	// and eof_action=pass bound it. Order matches the filtergraph input indices.
	for _, paths := range in.KillfeedNoticePaths {
		for _, noticePath := range paths {
			args = append(args, "-loop", "1", "-i", noticePath)
		}
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
// Plans without killfeed cues retain the original graph byte-for-byte. Clips
// with cues overlay a synthetic kill notice per cue (when a pre-rendered PNG is
// supplied) or a WYSIWYG frozen crop of the killfeed region as a fallback.
func buildFilterGraph(layout LayoutVariant, plan EditPlan, clip ClipRange, noticePaths [][]string, bannerFontPath string, duration float64, noticeInputBase int) string {
	if len(clip.KillfeedSeconds) == 0 {
		return buildStandardFilterGraph(layout, plan, bannerFontPath, duration)
	}
	return buildKillfeedFilterGraph(layout, plan, clip, noticePaths, bannerFontPath, duration, noticeInputBase)
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

// buildKillfeedFilterGraph composes the layout, then overlays one killfeed
// element per cue on the top-right. A cue with pre-rendered notice PNGs overlays
// them as looped inputs (stacked top-first); a cue without paths falls back to a
// WYSIWYG frozen crop of plan.KillfeedCrop scaled to killfeedFrozenWidth. Both
// share the same right margin and cue-timed enable window.
func buildKillfeedFilterGraph(layout LayoutVariant, plan EditPlan, clip ClipRange, noticePaths [][]string, bannerFontPath string, duration float64, noticeInputBase int) string {
	tail := ""
	if plan.Effects.Grade {
		tail += gradeFilter + ","
	}
	tail += fmt.Sprintf("fps=%d,format=yuv420p[v]", outputFPS)

	baseY := 64
	if !layout.FullFrame {
		baseY = layout.FaceOutputHeight + 72
	}

	hasNotices := func(i int) bool {
		return i < len(noticePaths) && len(noticePaths[i]) > 0
	}
	var frozenCues []int
	for i := range clip.KillfeedSeconds {
		if !hasNotices(i) {
			frozenCues = append(frozenCues, i)
		}
	}

	parts := make([]string, 0, len(clip.KillfeedSeconds)*2+6)

	// Layout branches, producing [layout]. Each frozen cue needs its own source
	// split branch so its killfeed strip can be frozen independently.
	if layout.FullFrame {
		total := 1 + len(frozenCues)
		layoutSrc := "[0:v]"
		if total > 1 {
			var split strings.Builder
			fmt.Fprintf(&split, "[0:v]split=%d[layoutin]", total)
			for _, i := range frozenCues {
				fmt.Fprintf(&split, "[killfeedin%d]", i)
			}
			parts = append(parts, split.String())
			layoutSrc = "[layoutin]"
		}
		parts = append(parts, fmt.Sprintf(
			"%s%s,scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d[layout]",
			layoutSrc, cropFilter(plan.GameplayCrop),
			layout.OutputWidth, layout.GameOutputHeight, layout.OutputWidth, layout.GameOutputHeight,
		))
	} else {
		var split strings.Builder
		fmt.Fprintf(&split, "[0:v]split=%d[facein][gamein]", 2+len(frozenCues))
		for _, i := range frozenCues {
			fmt.Fprintf(&split, "[killfeedin%d]", i)
		}
		parts = append(parts,
			split.String(),
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

	// Per-cue source branches, in cue order: notice inputs are reset to a clean
	// RGBA still; frozen cues freeze a single crop of the killfeed region.
	inputIndex := noticeInputBase
	for i := range clip.KillfeedSeconds {
		if hasNotices(i) {
			for j := range noticePaths[i] {
				parts = append(parts, fmt.Sprintf(
					"[%d:v]format=rgba,setpts=PTS-STARTPTS[notice%d_%d]", inputIndex, i, j,
				))
				inputIndex++
			}
			continue
		}
		relative := clip.KillfeedSeconds[i] - clip.StartSeconds
		parts = append(parts, fmt.Sprintf(
			"[killfeedin%d]trim=start=%s,select='eq(n\\,0)',setpts=PTS-STARTPTS,%s,"+
				"scale=%d:-2:flags=lanczos,tpad=stop_mode=clone:stop_duration=%s[killfeed%d]",
			i, floatArg(relative), cropFilter(*plan.KillfeedCrop),
			killfeedFrozenWidth, floatArg(duration), i,
		))
	}

	// Ordered overlays: stacked notices per cue, or a single frozen strip.
	type overlay struct {
		label string
		y     int
		start float64
		end   float64
	}
	var overlays []overlay
	for i := range clip.KillfeedSeconds {
		relative := clip.KillfeedSeconds[i] - clip.StartSeconds
		start := math.Max(0, relative-killfeedLeadTime)
		end := math.Min(duration, relative+killfeedTrailTime)
		if hasNotices(i) {
			for j := range noticePaths[i] {
				overlays = append(overlays, overlay{
					label: fmt.Sprintf("notice%d_%d", i, j),
					y:     baseY + j*(KillfeedNoticeHeight+killfeedNoticeStackGap),
					start: start, end: end,
				})
			}
			continue
		}
		overlays = append(overlays, overlay{label: fmt.Sprintf("killfeed%d", i), y: baseY, start: start, end: end})
	}

	baseLabel := "layout"
	for k, ov := range overlays {
		out := "content"
		if k < len(overlays)-1 {
			out = fmt.Sprintf("kfover%d", k)
		}
		parts = append(parts, fmt.Sprintf(
			"[%s][%s]overlay=x=W-w-24:y=%d:enable='between(t\\,%s\\,%s)':eof_action=pass:shortest=0[%s]",
			baseLabel, ov.label, ov.y, floatArg(ov.start), floatArg(ov.end), out,
		))
		baseLabel = out
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
