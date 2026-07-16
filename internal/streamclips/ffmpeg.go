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
	// It mirrors the web preview's KILLFEED_WIDTH so the preview matches the
	// render, and is scaled ~1.5x with the synthetic notices for a matching look.
	killfeedFrozenWidth = 930
	// killfeedNoticeStackGap is the vertical gap between stacked synthetic notices.
	killfeedNoticeStackGap = 8

	// killfeedGameplayTopFraction places the top of the killfeed a fixed fraction
	// down the gameplay band, matching the reference viral Short (~24% into the
	// gameplay region, measured on a 1920-high frame).
	killfeedGameplayTopFraction = 0.24
	// Entrance animation: the notice slides in fast from the right edge, blurred
	// horizontally while it moves, then settles at center with a small overshoot.
	killfeedSlideInSeconds  = 0.08 // fast slide to just past center
	killfeedSettleSeconds   = 0.04 // short settle from the overshoot back to center
	killfeedOvershootPx     = 12   // how far past center the slide overshoots
	killfeedMotionBlurSigma = 24   // horizontal Gaussian blur during the slide
	// killfeedFadeOutSeconds fades the notice out over the tail of its window
	// instead of cutting it hard at the trail time.
	killfeedFadeOutSeconds = 0.35
	// KillfeedSampleDelaySeconds is how long after a cue a killfeed frame is
	// sampled: at the cue itself the newest notice may not be drawn at all, and
	// its highlight ring is still fading in. Sampling is deliberately separate
	// from display timing: a cue is the exact kill instant, so the rendered
	// notice starts at the cue rather than inheriting this read delay.
	KillfeedSampleDelaySeconds = 0.35
	killfeedTrailTime          = 2.8
	// killfeedFreezeEndGuard keeps a delayed freeze sample clear of the clip's
	// final frame, so trim=start always lands on a frame that exists.
	killfeedFreezeEndGuard = 0.05

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
	// index-aligned with the normalized clip's killfeed event cues. Each cue's
	// list is ordered top-first, and every PNG is
	// streamclips.KillfeedNoticeHeight tall.
	// A cue with no paths falls back to a frozen crop of the killfeed region.
	KillfeedNoticePaths [][]string
	// TextOverlayPaths holds materialized text files, index-aligned with the
	// clip's Edit.TextOverlays. drawtext reads each file with expansion=none,
	// so arbitrary user text never needs filtergraph escaping.
	TextOverlayPaths []string
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
	if clip.Edit != nil && len(clip.Edit.TextOverlays) > 0 {
		if in.BannerFontPath == "" {
			return nil, fmt.Errorf("text overlay font path is required")
		}
		if len(in.TextOverlayPaths) != len(clip.Edit.TextOverlays) {
			return nil, fmt.Errorf(
				"clip %s text overlay paths length %d must match %d text overlays",
				clip.ID, len(in.TextOverlayPaths), len(clip.Edit.TextOverlays),
			)
		}
	}
	duration := clip.EndSeconds - clip.StartSeconds

	// Notice PNGs are extra inputs after the source and the optional music input.
	noticeInputBase := 1
	if in.MusicPath != "" {
		noticeInputBase = 2
	}
	filter := buildFilterGraph(layout, plan, clip, in.KillfeedNoticePaths, in.BannerFontPath, in.TextOverlayPaths, duration, noticeInputBase)

	args := []string{
		"-y",
		"-ss", secondsArg(clip.StartSeconds),
		"-t", secondsArg(duration),
		"-i", in.SourcePath,
	}
	audioMap := "0:a?"
	shortest := false
	srcFilters := sourceAudioFilters(clip.Edit)
	fadeFilters := boundaryFades(clip.Edit, clip.OutputDurationSeconds(), "afade")
	if in.MusicPath != "" {
		// Loop the track so it always covers the clip; amix/-shortest bound it.
		args = append(args, "-stream_loop", "-1", "-i", in.MusicPath)
		volume := plan.Music.Volume
		if volume == 0 {
			volume = defaultMusicVolume
		}
		if in.SourceHasAudio {
			// Gain and tempo edits apply to the source before the mix so the
			// music keeps its own volume and pace; fades apply to the mix.
			mixInput := "[0:a]"
			if srcFilters != "" {
				filter += ";[0:a]" + srcFilters + "[srca]"
				mixInput = "[srca]"
			}
			filter += fmt.Sprintf(";[1:a]volume=%s[bgm];%s[bgm]amix=inputs=2:duration=first:dropout_transition=0:normalize=0", floatArg(volume), mixInput)
			if fadeFilters != "" {
				filter += "," + fadeFilters
			}
			filter += "[a]"
		} else {
			// No original audio to bound the mix: -shortest ends with the video.
			filter += fmt.Sprintf(";[1:a]volume=%s", floatArg(volume))
			if fadeFilters != "" {
				filter += "," + fadeFilters
			}
			filter += "[a]"
			shortest = true
		}
		audioMap = "[a]"
	} else if in.SourceHasAudio {
		chain := srcFilters
		if fadeFilters != "" {
			if chain != "" {
				chain += ","
			}
			chain += fadeFilters
		}
		if chain != "" {
			filter += ";[0:a]" + chain + "[a]"
			audioMap = "[a]"
		}
	} else if clip.Edit.speed() != 1 {
		// A probed-silent source renders without an audio track when speed
		// changes the timeline: with a correct probe 0:a? maps nothing anyway,
		// and with a stale probe passing the stream untouched would desync it.
		audioMap = ""
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
	)
	if audioMap != "" {
		args = append(args, "-map", audioMap)
	}
	args = append(args,
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
func buildFilterGraph(layout LayoutVariant, plan EditPlan, clip ClipRange, noticePaths [][]string, bannerFontPath string, textPaths []string, duration float64, noticeInputBase int) string {
	if len(clip.KillfeedSeconds) == 0 {
		return buildStandardFilterGraph(layout, plan, clip, bannerFontPath, textPaths, duration)
	}
	return buildKillfeedFilterGraph(layout, plan, clip, noticePaths, bannerFontPath, textPaths, duration, noticeInputBase)
}

// videoTail is the filter chain every graph applies after the layout and any
// banner/killfeed overlays: text overlays and the speed change first (both in
// source time up to setpts), then boundary fades in output time, then the
// grade and the output format. An unedited clip keeps the pre-edit chain.
func videoTail(plan EditPlan, clip ClipRange, fontPath string, textPaths []string) string {
	var parts []string
	if clip.Edit != nil {
		for i, overlay := range clip.Edit.TextOverlays {
			parts = append(parts, textOverlayFilter(overlay, fontPath, textPaths[i]))
		}
		if speed := clip.Edit.speed(); speed != 1 {
			parts = append(parts, "setpts=PTS/"+floatArg(speed))
		}
		if fades := boundaryFades(clip.Edit, clip.OutputDurationSeconds(), "fade"); fades != "" {
			parts = append(parts, fades)
		}
	}
	if plan.Effects.Grade {
		parts = append(parts, gradeFilter)
	}
	parts = append(parts, fmt.Sprintf("fps=%d,format=yuv420p[v]", outputFPS))
	return strings.Join(parts, ",")
}

// boundaryFades emits the clip-edge fades in output (post-speed) time. The
// name is the FFmpeg filter to use — "fade" for video, "afade" for audio — so
// both timelines share one timing implementation and can never drift apart.
func boundaryFades(edit *ClipEdit, outputDuration float64, name string) string {
	if edit == nil {
		return ""
	}
	var parts []string
	if fadeIn := edit.FadeInSeconds; fadeIn > 0 {
		parts = append(parts, fmt.Sprintf("%s=t=in:st=0:d=%s", name, floatArg(fadeIn)))
	}
	if fadeOut := edit.FadeOutSeconds; fadeOut > 0 {
		parts = append(parts, fmt.Sprintf("%s=t=out:st=%s:d=%s", name, floatArg(outputDuration-fadeOut), floatArg(fadeOut)))
	}
	return strings.Join(parts, ",")
}

// textOverlayFilter burns one centered text line. The text comes from a
// materialized file read with expansion=none, so no user character can reach
// FFmpeg's filtergraph or drawtext expansion syntax. The enable window is in
// source-relative seconds because it runs before the speed setpts.
func textOverlayFilter(overlay TextOverlay, fontPath, textPath string) string {
	size := overlay.FontSize
	if size == 0 {
		size = defaultOverlayFontSize
	}
	filter := fmt.Sprintf(
		"drawtext=fontfile='%s':textfile='%s':expansion=none:fontcolor=white:fontsize=%d:borderw=3:bordercolor=black:"+
			"shadowcolor=black@0.35:shadowx=2:shadowy=2:x=(w-text_w)/2:y=h*%s-text_h/2",
		ffmpegFilterPath(fontPath), ffmpegFilterPath(textPath), size, floatArg(overlay.PositionY),
	)
	switch {
	case overlay.StartSeconds != nil && overlay.EndSeconds != nil:
		filter += fmt.Sprintf(`:enable='between(t\,%s\,%s)'`, floatArg(*overlay.StartSeconds), floatArg(*overlay.EndSeconds))
	case overlay.StartSeconds != nil:
		filter += fmt.Sprintf(`:enable='gte(t\,%s)'`, floatArg(*overlay.StartSeconds))
	case overlay.EndSeconds != nil:
		filter += fmt.Sprintf(`:enable='lte(t\,%s)'`, floatArg(*overlay.EndSeconds))
	}
	return filter
}

// sourceAudioFilters is the chain applied to the clip's original audio: gain
// first, then the tempo chain. Empty means the stream passes through.
func sourceAudioFilters(edit *ClipEdit) string {
	if edit == nil {
		return ""
	}
	var parts []string
	if edit.SourceVolume != nil {
		parts = append(parts, "volume="+floatArg(*edit.SourceVolume))
	}
	if speed := edit.speed(); speed != 1 {
		parts = append(parts, atempoChain(speed))
	}
	return strings.Join(parts, ",")
}

// atempoChain expresses a rate in [0.25, 3] as chained atempo filters, since a
// single atempo instance only covers [0.5, 2].
func atempoChain(speed float64) string {
	switch {
	case speed > 2:
		return "atempo=2.000000,atempo=" + floatArg(speed/2)
	case speed < 0.5:
		return "atempo=0.500000,atempo=" + floatArg(speed/0.5)
	default:
		return "atempo=" + floatArg(speed)
	}
}

func buildStandardFilterGraph(layout LayoutVariant, plan EditPlan, clip ClipRange, bannerFontPath string, textPaths []string, duration float64) string {
	tail := videoTail(plan, clip, bannerFontPath, textPaths)

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
// event per cue horizontally centered a fixed fraction down the gameplay band.
// A cue with pre-rendered notice PNGs overlays them as looped inputs (stacked
// top-first across all still-live events); a cue without paths falls back to a
// WYSIWYG frozen crop of plan.KillfeedCrop scaled to killfeedFrozenWidth. Every
// overlay slides in from the right edge with a horizontal motion blur, settles
// at center with a small overshoot, and fades out over the tail of its window.
func buildKillfeedFilterGraph(layout LayoutVariant, plan EditPlan, clip ClipRange, noticePaths [][]string, bannerFontPath string, textPaths []string, duration float64, noticeInputBase int) string {
	tail := videoTail(plan, clip, bannerFontPath, textPaths)

	baseY := killfeedBaseY(layout)

	hasNotices := func(i int) bool {
		return i < len(noticePaths) && len(noticePaths[i]) > 0
	}
	var frozenCues []int
	for i := range clip.KillfeedSeconds {
		if !hasNotices(i) {
			frozenCues = append(frozenCues, i)
		}
	}

	// Per-cue visible window [start, end], bounded by the trail time. Both the
	// tail fade and the overlay enable windows key off these.
	starts := make([]float64, len(clip.KillfeedSeconds))
	ends := make([]float64, len(clip.KillfeedSeconds))
	for i := range clip.KillfeedSeconds {
		relative := clip.KillfeedSeconds[i] - clip.StartSeconds
		starts[i] = math.Max(0, relative)
		ends[i] = math.Min(duration, relative+killfeedTrailTime)
	}

	parts := make([]string, 0, len(clip.KillfeedSeconds)*3+6)

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

	// Per-cue source branches, in cue order: each notice is reset to a clean RGBA
	// still, faded out at the tail, and (unless its window is too short to slide)
	// split into a sharp variant and a horizontally blurred variant for the
	// slide-in motion blur. Frozen cues freeze a single crop of the killfeed
	// region and get the same sharp/blur split so they enter identically.
	// A window shorter than the slide+settle renders only sharp, since a slide
	// there would show nothing but blurred mid-motion frames (see fix below).
	inputIndex := noticeInputBase
	for i := range clip.KillfeedSeconds {
		if hasNotices(i) {
			suppressed := killfeedEntranceSuppressed(starts[i], ends[i])
			for j := range noticePaths[i] {
				if suppressed {
					parts = append(parts, fmt.Sprintf(
						"[%d:v]format=rgba,setpts=PTS-STARTPTS,%s[nsharp%d_%d]",
						inputIndex, killfeedFadeFilter(starts[i], ends[i]), i, j,
					))
				} else {
					parts = append(parts,
						fmt.Sprintf(
							"[%d:v]format=rgba,setpts=PTS-STARTPTS,%s,split=2[nsharp%d_%d][nblurpre%d_%d]",
							inputIndex, killfeedFadeFilter(starts[i], ends[i]), i, j, i, j,
						),
						fmt.Sprintf(
							"[nblurpre%d_%d]gblur=sigma=%d:sigmaV=0[nblur%d_%d]",
							i, j, killfeedMotionBlurSigma, i, j,
						),
					)
				}
				inputIndex++
			}
			continue
		}
		relative := clip.KillfeedSeconds[i] - clip.StartSeconds
		base := fmt.Sprintf(
			"[killfeedin%d]trim=start=%s,select='eq(n\\,0)',setpts=PTS-STARTPTS,%s,"+
				"scale=%d:-2:flags=lanczos,tpad=stop_mode=clone:stop_duration=%s,%s",
			i, floatArg(killfeedFreezeOffset(relative, duration)), cropFilter(*plan.KillfeedCrop),
			killfeedFrozenWidth, floatArg(duration), killfeedFadeFilter(starts[i], ends[i]),
		)
		if killfeedEntranceSuppressed(starts[i], ends[i]) {
			parts = append(parts, fmt.Sprintf("%s[killfeed%d]", base, i))
			continue
		}
		parts = append(parts,
			fmt.Sprintf("%s,split=2[kfsharp%d][kfblurpre%d]", base, i, i),
			fmt.Sprintf("[kfblurpre%d]gblur=sigma=%d:sigmaV=0[kfblur%d]", i, killfeedMotionBlurSigma, i),
		)
	}

	// Ordered overlay ops: synthetic event notices reflow around every earlier
	// notice that is still alive, while a cue without structured kills uses one
	// frozen strip. KillfeedKills contains event deltas rather than cumulative
	// snapshots, so independently timed cues must participate in the same stack.
	// Each notice contributes two ops sharing one stack slot: the blurred variant
	// during the slide window, then the sharp variant for the settle and hold.
	type overlayOp struct {
		input string
		x     string
		y     string
		start float64
		end   float64
	}
	var ops []overlayOp
	var priorNotices []noticeLifetime
	for i := range clip.KillfeedSeconds {
		start, end := starts[i], ends[i]
		x := killfeedSlideX(start)
		slideEnd := math.Min(end, start+killfeedSlideInSeconds)
		suppressed := killfeedEntranceSuppressed(start, end)
		if hasNotices(i) {
			for j := range noticePaths[i] {
				y := killfeedStackY(baseY, start, end, priorNotices)
				if suppressed {
					// Window too short to slide: hold the sharp notice at center
					// for the whole window rather than showing only blurred frames.
					ops = append(ops, overlayOp{input: fmt.Sprintf("nsharp%d_%d", i, j), x: killfeedCenterX, y: y, start: start, end: end})
				} else {
					ops = append(ops,
						overlayOp{input: fmt.Sprintf("nblur%d_%d", i, j), x: x, y: y, start: start, end: slideEnd},
						overlayOp{input: fmt.Sprintf("nsharp%d_%d", i, j), x: x, y: y, start: slideEnd, end: end},
					)
				}
				priorNotices = append(priorNotices, noticeLifetime{start: start, end: end})
			}
			continue
		}
		if suppressed {
			ops = append(ops, overlayOp{input: fmt.Sprintf("killfeed%d", i), x: killfeedCenterX, y: strconv.Itoa(baseY), start: start, end: end})
			continue
		}
		ops = append(ops,
			overlayOp{input: fmt.Sprintf("kfblur%d", i), x: x, y: strconv.Itoa(baseY), start: start, end: slideEnd},
			overlayOp{input: fmt.Sprintf("kfsharp%d", i), x: x, y: strconv.Itoa(baseY), start: slideEnd, end: end},
		)
	}

	baseLabel := "layout"
	for k, op := range ops {
		out := "content"
		if k < len(ops)-1 {
			out = fmt.Sprintf("kfover%d", k)
		}
		parts = append(parts, fmt.Sprintf(
			"[%s][%s]overlay=x='%s':y=%s:eval=frame:enable='between(t\\,%s\\,%s)':eof_action=pass:shortest=0[%s]",
			baseLabel, op.input, op.x, op.y, floatArg(op.start), floatArg(op.end), out,
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

// noticeLifetime is the [start, end] window a notice occupies its stack slot,
// used to reflow later notices around still-live earlier ones.
type noticeLifetime struct {
	start float64
	end   float64
}

// killfeedBaseY is the top of the killfeed in output pixels: a fixed fraction
// down the gameplay band. For a full-frame layout the gameplay band is the whole
// frame; for a facecam layout it starts below the facecam.
func killfeedBaseY(layout LayoutVariant) int {
	gameplayTop := 0
	if !layout.FullFrame {
		gameplayTop = layout.FaceOutputHeight
	}
	return gameplayTop + int(math.Round(killfeedGameplayTopFraction*float64(layout.GameOutputHeight)))
}

// killfeedFadeFilter fades a notice's alpha out over the tail of its window so
// it dissolves instead of cutting hard. The fade shortens for a window briefer
// than the fade so it always fits.
func killfeedFadeFilter(start, end float64) string {
	dur := math.Min(killfeedFadeOutSeconds, end-start)
	return fmt.Sprintf("fade=t=out:st=%s:d=%s:alpha=1", floatArg(end-dur), floatArg(dur))
}

// killfeedCenterX is the horizontal-center overlay x expression, written in
// terms of overlay's W (main width) and w (overlay width) so it is independent
// of the notice's own width. It has no commas, so it needs no filtergraph
// escaping. It is the static resting x, and the point every slide settles to.
const killfeedCenterX = "(W-w)/2"

// killfeedEntranceSuppressed reports whether a notice's visible window is too
// short to run the slide-in entrance. A cue landing within slide+settle of the
// clip end would otherwise render only blurred, mid-slide frames before the
// clip cuts, so such a window skips the slide and holds the sharp notice at
// center instead.
func killfeedEntranceSuppressed(start, end float64) bool {
	return end-start < killfeedSlideInSeconds+killfeedSettleSeconds
}

// killfeedSlideX is the overlay x expression (escaped for the filtergraph) that
// slides a notice in from the right edge to the horizontal center. It eases out
// to a point killfeedOvershootPx past center during the slide, then settles back
// to center, and holds there for the rest of the window. It is written in terms
// of overlay's W (main width) and w (overlay width) so it is independent of the
// notice's own width, and must be used with overlay eval=frame.
func killfeedSlideX(start float64) string {
	slideEnd := start + killfeedSlideInSeconds
	settleEnd := slideEnd + killfeedSettleSeconds
	center := killfeedCenterX
	ov := strconv.Itoa(killfeedOvershootPx)
	// Slide: quadratic ease-out from the right edge (W) to center-overshoot.
	p := fmt.Sprintf("(t-%s)/%s", floatArg(start), floatArg(killfeedSlideInSeconds))
	ease := fmt.Sprintf("(1-(1-%s)*(1-%s))", p, p)
	slide := fmt.Sprintf("W+((%s-%s)-W)*%s", center, ov, ease)
	// Settle: linear return from center-overshoot to center.
	q := fmt.Sprintf("(t-%s)/%s", floatArg(slideEnd), floatArg(killfeedSettleSeconds))
	settle := fmt.Sprintf("%s-%s*(1-%s)", center, ov, q)
	expr := fmt.Sprintf("if(lt(t,%s),%s,if(lt(t,%s),%s,%s))", floatArg(slideEnd), slide, floatArg(settleEnd), settle, center)
	return strings.ReplaceAll(expr, ",", `\,`)
}

// killfeedStackY is the overlay y expression for a notice starting at baseY,
// pushed UP by one slot for each still-live earlier notice. The first/oldest
// notice holds baseY and later concurrent ones stack above it, which keeps the
// caption band (~35% down the gameplay band, just below baseY) permanently
// clear. A notice with no live predecessors renders at the static baseY.
func killfeedStackY(baseY int, start, end float64, prior []noticeLifetime) string {
	var activeEarlier []string
	for _, p := range prior {
		// between() is inclusive, so keep the term when the lifetimes share only
		// their boundary frame as well.
		if p.end < start || p.start > end {
			continue
		}
		activeEarlier = append(activeEarlier, fmt.Sprintf("between(t\\,%s\\,%s)", floatArg(p.start), floatArg(p.end)))
	}
	if len(activeEarlier) == 0 {
		return strconv.Itoa(baseY)
	}
	return fmt.Sprintf("%d-%d*(%s)", baseY, KillfeedNoticeHeight+killfeedNoticeStackGap, strings.Join(activeEarlier, "+"))
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

// ffmpegFilterPath escapes a path for embedding in an ffmpeg filtergraph
// string (e.g. drawtext's fontfile). ffmpeg filtergraph syntax always wants
// forward slashes and an escaped drive-letter colon, regardless of the OS
// running this code, so backslashes are normalized unconditionally rather
// than with filepath.ToSlash: ToSlash only rewrites the host OS's own
// separator, which is a no-op for a Windows-style path like `C:\Windows\...`
// on a non-Windows build and left it double-escaped instead of slash-joined.
func ffmpegFilterPath(value string) string {
	value = strings.ReplaceAll(value, `\`, "/")
	value = strings.ReplaceAll(value, ":", `\:`)
	return strings.ReplaceAll(value, "'", `\'`)
}

// killfeedFreezeOffset returns the in-clip timestamp whose frame a frozen
// killfeed strip is cropped from. CS2 finishes drawing a kill notice shortly
// after the kill lands, so freezing the exact cue frame can catch a notice that
// has not appeared yet — a verified failure on a three-kill AWP burst, where the
// newest notice was still absent at the cue and only rendered 0.35s later. The
// delay is the same one the vision reader waits for the notice with, for the
// same physical reason. The offset is clamped inside the clip so a cue at the
// very end still resolves to a real frame instead of trimming past the last one.
func killfeedFreezeOffset(relative, duration float64) float64 {
	return KillfeedSampleSeconds(relative, duration)
}

// KillfeedSampleSeconds maps an exact kill cue onto the later frame used for
// vision/frozen-crop sampling. It keeps the sample inside the owning clip so a
// cue near the end cannot read the following scene, while never moving before
// the cue itself.
func KillfeedSampleSeconds(cue, clipEnd float64) float64 {
	return min(cue+KillfeedSampleDelaySeconds, max(cue, clipEnd-killfeedFreezeEndGuard))
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
