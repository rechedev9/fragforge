package editor

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/lineups"
	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/rhythm"
)

func buildManifest(result recording.RecordingResult, opts ManifestOptions) (Manifest, error) {
	clips, warnings, clipErr := recording.ResolveSegmentClips(result)
	if clipErr != nil {
		// The manifest is built best-effort: a missing segment clip is recorded
		// as a warning rather than aborting manifest generation.
		warnings = append(warnings, clipErr.Error())
	}
	clipBySegment := map[string]recording.SegmentClip{}
	for _, clip := range clips {
		clipBySegment[clip.SegmentID] = clip
	}

	player := playerName(result.Plan)
	mapName, mapWarning := mapName(result.Plan, opts.KillPlan)
	if mapWarning != "" {
		warnings = append(warnings, mapWarning)
	}

	baseDir := filepath.Dir(opts.RecordingResultPath)
	promptDir := filepath.Join(opts.OutputDir, "prompts")
	preset := opts.Preset
	if preset == "" {
		preset = DefaultPreset().Name
	}
	renderPreset, ok := PresetByName(preset)
	if !ok {
		return Manifest{Warnings: warnings}, unknownPresetError(preset)
	}
	effectsPreset := opts.EffectsPreset
	effectsPath := opts.EffectsPath
	if effectsPath == "" && strings.TrimSpace(effectsPreset) == "" {
		effectsPreset = renderPreset.EffectsPreset
		effectsPath = renderPreset.EffectsPath
	}
	effectsSource, err := loadEffectsSource(effectsPath, effectsPreset)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	videoCRF, err := normalizeVideoCRFForPreset(preset, opts.VideoCRF)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	videoPreset, err := normalizeVideoPresetForPreset(preset, opts.VideoPreset)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	outputFPS, err := normalizeOutputFPSForPreset(renderPreset, opts.OutputFPS)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	outputFormat, err := normalizeOutputFormat(opts.OutputFormat)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	killEffect, err := normalizeKillEffect(opts.KillEffect)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	transition, err := normalizeTransition(opts.Transition)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	// Legacy vertical deathnotice captures leave the killfeed outside the
	// center crop, so they still need the historical crop-and-overlay path.
	// New portrait-safe captures move the native CS2 notices into the 9:16
	// frame during recording, where keeping them live is sharper and avoids
	// stacked frozen badges. Landscape output already retains the native
	// top-right HUD and must never duplicate it.
	nativePortraitKillfeed := result.Plan.Stream.HUDMode == recording.HUDModeDeathnotices && result.Plan.Stream.PortraitSafeKillfeed
	killfeedOverlay := opts.KillfeedOverlay && renderPreset.KillfeedSource && outputFormat == OutputFormatShort9x16 && !nativePortraitKillfeed
	hqFilters := opts.HQFilters || renderPreset.HQFilters
	audioNormalize := opts.AudioNormalize || renderPreset.AudioNormalize
	qualityChecks := opts.QualityChecks || renderPreset.QualityChecks
	coverSheets := opts.CoverSheets || renderPreset.CoverSheets
	temporalSmoothing := opts.TemporalSmoothing || renderPreset.TemporalSmoothing
	segmentFilter := uniqueSegmentIDs(opts.SegmentIDs)
	lineupCatalog, err := lineups.LoadDir(opts.LineupCatalogPath)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	logDir := filepath.Join(opts.OutputDir, "logs")
	manifest := Manifest{
		Preset:            preset,
		RecordingResult:   opts.RecordingResultPath,
		KillPlan:          opts.KillPlanPath,
		OutputDir:         opts.OutputDir,
		PublishDir:        opts.PublishDir,
		GalleryPath:       filepath.Join(opts.PublishDir, "index.html"),
		SummaryPath:       filepath.Join(opts.PublishDir, "publish-summary.md"),
		SegmentFilter:     append([]string(nil), segmentFilter...),
		Limit:             opts.Limit,
		SkipExisting:      opts.SkipExisting,
		EffectsPath:       effectsSource.Path,
		EffectsPreset:     effectsSource.Preset,
		MusicPath:         opts.MusicPath,
		RhythmPath:        opts.RhythmPath,
		OutputFormat:      outputFormat,
		KillEffect:        killEffect,
		Transition:        transition,
		Intro:             opts.Intro,
		Outro:             opts.Outro,
		IntroText:         opts.IntroText,
		OutroText:         opts.OutroText,
		HookText:          opts.HookText,
		KillCounter:       opts.KillCounter,
		KillfeedOverlay:   killfeedOverlay,
		TailTrimSeconds:   opts.TailTrimSeconds,
		OutputFPS:         outputFPS,
		CompileSegments:   opts.CompileSegments,
		LineupCatalogPath: opts.LineupCatalogPath,
		VideoCRF:          videoCRF,
		VideoPreset:       videoPreset,
		HQFilters:         hqFilters,
		AudioNormalize:    audioNormalize,
		QualityChecks:     qualityChecks,
		CoverSheets:       coverSheets,
		TemporalSmoothing: temporalSmoothing,
		CoversEnabled:     opts.CoversEnabled,
		Warnings:          warnings,
	}
	if opts.LineupCatalogPath != "" {
		manifest.UnmatchedSmokes = filepath.Join(opts.OutputDir, "unmatched-smokes.json")
	}
	selected := segmentIDSet(segmentFilter)
	availableSegments := map[string]bool{}
	availableClips := map[string]bool{}
	for _, segment := range result.Plan.Segments {
		availableSegments[segment.ID] = true
		if _, ok := clipBySegment[segment.ID]; ok {
			availableClips[segment.ID] = true
		}
	}
	for _, id := range segmentFilter {
		switch {
		case !availableSegments[id]:
			manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("requested segment %q was not found in recording plan", id))
		case !availableClips[id]:
			manifest.Warnings = append(manifest.Warnings, fmt.Sprintf("requested segment %q has no recorded clip", id))
		}
	}
	if opts.CompileSegments {
		rhythmSync, err := loadRhythmSync(opts.RhythmPath)
		if err != nil {
			return manifest, err
		}
		if rhythmSync != nil && opts.TailTrimSeconds > 0 {
			// Rhythm sync placed each segment on the beat grid using the full
			// clip durations; trimming tails here would shift every later
			// segment off its beat.
			manifest.Warnings = append(manifest.Warnings, "tail trim skipped: rhythm sync uses untrimmed segment durations")
		}
		compiled, err := buildCompiledShort(result, opts, compiledShortOptions{
			BaseDir:           baseDir,
			PromptDir:         promptDir,
			LogDir:            logDir,
			Preset:            preset,
			Player:            player,
			MapName:           mapName,
			ClipBySegment:     clipBySegment,
			Selected:          selected,
			RhythmSync:        rhythmSync,
			VideoCRF:          videoCRF,
			VideoPreset:       videoPreset,
			OutputFormat:      outputFormat,
			KillEffect:        killEffect,
			Transition:        transition,
			Intro:             opts.Intro,
			Outro:             opts.Outro,
			IntroText:         opts.IntroText,
			OutroText:         opts.OutroText,
			HookText:          opts.HookText,
			KillCounter:       opts.KillCounter,
			KillfeedOverlay:   killfeedOverlay,
			TailTrimSeconds:   opts.TailTrimSeconds,
			OutputFPS:         outputFPS,
			HQFilters:         hqFilters,
			AudioNormalize:    audioNormalize,
			TemporalSmoothing: temporalSmoothing,
			CoverSheets:       coverSheets,
			QualityChecks:     qualityChecks,
		})
		if err != nil {
			return manifest, err
		}
		if compiled.SegmentID != "" {
			for _, part := range compiled.Parts {
				manifest.Warnings = append(manifest.Warnings, ValidateSourceArtifact(part.SourceArtifact)...)
			}
			manifest.Shorts = append(manifest.Shorts, compiled)
		}
		if len(manifest.Shorts) == 0 {
			manifest.Warnings = append(manifest.Warnings, "segment selection produced no shorts")
		}
		if err := applyEffectsToManifest(&manifest, effectsSource, opts.FFmpegPath, opts.KillfeedFrameProbe); err != nil {
			return manifest, err
		}
		return manifest, nil
	}
	for i, segment := range result.Plan.Segments {
		if len(selected) > 0 && !selected[segment.ID] {
			continue
		}
		if opts.Limit > 0 && len(manifest.Shorts) >= opts.Limit {
			break
		}
		clip, ok := clipBySegment[segment.ID]
		if !ok {
			continue
		}
		index := i + 1
		input := resolvePath(baseDir, clip.Path)
		// Sanitize the segment ID before it lands in file paths so a crafted ID
		// cannot escape the output directory. safeFilenameToken preserves the
		// machine-generated "seg-NNN" form.
		safeID := safeFilenameToken(segment.ID)
		output := filepath.Join(opts.OutputDir, fmt.Sprintf("short-%03d-%s.mp4", index, safeID))
		promptPath := filepath.Join(promptDir, fmt.Sprintf("short-%03d-%s-cover.md", index, safeID))
		logBase := fmt.Sprintf("short-%03d-%s", index, safeID)
		kills := killCues(segment, result.Plan.Tickrate)
		smokes := smokeCues(segment, result.Plan.Tickrate, mapName, lineupCatalog, opts.LineupCatalogPath != "")
		killCount := len(segment.Kills)
		smokeCount := len(smokes)
		primaryWeapon := primaryWeapon(segment.Kills)
		primarySmoke := primarySmoke(smokes)
		label := shortLabel(player, mapName, killCount)
		headline := premiumHeadline(mapName, killCount, primaryWeapon)
		title, caption, hashtags := publishText(player, mapName, killCount, primaryWeapon)
		if smokeCount > 0 && killCount == 0 {
			label = smokeLabel(player, mapName, smokes[0])
			headline = smokeHeadline(mapName, smokes[0])
			title, caption, hashtags = publishSmokeText(player, mapName, smokes[0])
		}
		publishBase := publishFileBase(index, safeID, player, mapName, killCount, primaryWeapon)
		if smokeCount > 0 && killCount == 0 {
			publishBase = publishSmokeFileBase(index, safeID, player, mapName, smokes[0])
		}
		duration := tailTrimmedDuration(kills, clipDuration(segment, result.Plan.Tickrate, clip.DurationSeconds), opts.TailTrimSeconds)
		coverTime := coverTimeSeconds(kills, duration)
		if len(kills) == 0 && len(smokes) > 0 {
			coverTime = coverTimeSecondsForSmoke(smokes[0], duration)
		}
		edit := ShortEdit{
			Index:             index,
			SegmentID:         segment.ID,
			Preset:            preset,
			Player:            player,
			Map:               mapName,
			KillCount:         killCount,
			PrimaryWeapon:     primaryWeapon,
			SmokeCount:        smokeCount,
			PrimarySmoke:      primarySmoke,
			Input:             input,
			Output:            output,
			SourceArtifact:    clip.Artifact,
			PromptPath:        promptPath,
			PublishPath:       filepath.Join(opts.PublishDir, publishBase+".mp4"),
			MusicPath:         opts.MusicPath,
			RhythmPath:        opts.RhythmPath,
			OutputFormat:      outputFormat,
			KillEffect:        killEffect,
			Transition:        transition,
			Intro:             opts.Intro,
			Outro:             opts.Outro,
			IntroText:         opts.IntroText,
			OutroText:         opts.OutroText,
			HookText:          opts.HookText,
			KillCounter:       opts.KillCounter,
			KillfeedOverlay:   killfeedOverlay,
			TailTrimSeconds:   opts.TailTrimSeconds,
			OutputFPS:         outputFPS,
			VideoCRF:          videoCRF,
			VideoPreset:       videoPreset,
			HQFilters:         hqFilters,
			AudioNormalize:    audioNormalize,
			TemporalSmoothing: temporalSmoothing,
			CaptionPath:       filepath.Join(opts.PublishDir, publishBase+".caption.txt"),
			DurationSeconds:   duration,
			Label:             label,
			Title:             title,
			Headline:          headline,
			Caption:           caption,
			Hashtags:          hashtags,
			Kills:             kills,
			Smokes:            smokes,
			RenderLogPath:     filepath.Join(logDir, logBase+"-render.log"),
		}
		if opts.CoversEnabled {
			edit.CoverPath = filepath.Join(opts.PublishDir, publishBase+".cover.jpg")
			if coverSheets {
				edit.CoverSheetPath = filepath.Join(opts.PublishDir, publishBase+".sheet.jpg")
			}
			edit.CoverTimeSeconds = coverTime
		}
		if qualityChecks {
			edit.QualityLogPath = filepath.Join(logDir, logBase+"-quality.log")
		}
		manifest.Warnings = append(manifest.Warnings, ValidateSourceArtifact(edit.SourceArtifact)...)
		manifest.Shorts = append(manifest.Shorts, edit)
	}
	if len(manifest.Shorts) == 0 {
		manifest.Warnings = append(manifest.Warnings, "segment selection produced no shorts")
	}
	if err := applyEffectsToManifest(&manifest, effectsSource, opts.FFmpegPath, opts.KillfeedFrameProbe); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func uniqueSegmentIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

type compiledShortOptions struct {
	BaseDir           string
	PromptDir         string
	LogDir            string
	Preset            string
	Player            string
	MapName           string
	ClipBySegment     map[string]recording.SegmentClip
	Selected          map[string]bool
	RhythmSync        map[string]rhythm.SegmentSync
	VideoCRF          int
	VideoPreset       string
	OutputFPS         int
	OutputFormat      string
	KillEffect        string
	Transition        string
	Intro             bool
	Outro             bool
	IntroText         string
	OutroText         string
	HookText          bool
	KillCounter       bool
	KillfeedOverlay   bool
	TailTrimSeconds   float64
	HQFilters         bool
	AudioNormalize    bool
	TemporalSmoothing bool
	CoverSheets       bool
	QualityChecks     bool
}

func buildCompiledShort(result recording.RecordingResult, opts ManifestOptions, c compiledShortOptions) (ShortEdit, error) {
	const segmentID = "demo-compilation"

	var parts []ShortPart
	var kills []KillCue
	var allKills []killplan.Kill
	cursor := 0.0
	for _, segment := range result.Plan.Segments {
		if len(c.Selected) > 0 && !c.Selected[segment.ID] {
			continue
		}
		if opts.Limit > 0 && len(parts) >= opts.Limit {
			break
		}
		clip, ok := c.ClipBySegment[segment.ID]
		if !ok {
			continue
		}
		partKills := killCues(segment, result.Plan.Tickrate)
		duration := clipDuration(segment, result.Plan.Tickrate, clip.DurationSeconds)
		if c.RhythmSync == nil {
			// Rhythm sync placed segments using untrimmed durations, so tails
			// are only trimmed on beat-free compilations (see buildManifest).
			duration = tailTrimmedDuration(partKills, duration, c.TailTrimSeconds)
		}
		if duration <= 0 {
			continue
		}
		partStart := cursor
		gapBefore := 0.0
		if c.RhythmSync != nil && len(segment.Kills) > 0 {
			entry, ok := c.RhythmSync[segment.ID]
			if !ok {
				return ShortEdit{}, fmt.Errorf("rhythm json has no segment_sync entry for %s", segment.ID)
			}
			partStart = entry.TimelineStartSeconds
			gapBefore = entry.GapBeforeSeconds
			if gapBefore == 0 && partStart > cursor {
				gapBefore = partStart - cursor
			}
			if partStart < cursor-0.001 {
				return ShortEdit{}, fmt.Errorf("rhythm sync for %s starts before previous segment", segment.ID)
			}
		}
		for _, kill := range partKills {
			kill.TimeSeconds += partStart
			kills = append(kills, kill)
		}
		allKills = append(allKills, segment.Kills...)
		parts = append(parts, ShortPart{
			SegmentID:            segment.ID,
			Input:                resolvePath(c.BaseDir, clip.Path),
			SourceArtifact:       clip.Artifact,
			DurationSeconds:      duration,
			TimelineStartSeconds: partStart,
			GapBeforeSeconds:     gapBefore,
			Kills:                partKills,
		})
		cursor = partStart + duration
	}
	if len(parts) == 0 {
		return ShortEdit{}, nil
	}

	primary := primaryWeapon(allKills)
	killCount := len(allKills)
	title, caption, hashtags := publishText(c.Player, c.MapName, killCount, primary)
	publishBase := publishCompiledFileBase(1, c.Player, c.MapName, killCount, primary)
	duration := cursor
	coverTime := coverTimeSeconds(kills, duration)
	short := ShortEdit{
		Index:             1,
		SegmentID:         segmentID,
		Preset:            c.Preset,
		Player:            c.Player,
		Map:               c.MapName,
		KillCount:         killCount,
		PrimaryWeapon:     primary,
		Input:             parts[0].Input,
		Output:            filepath.Join(opts.OutputDir, "short-001-demo-compilation.mp4"),
		SourceArtifact:    parts[0].SourceArtifact,
		PromptPath:        filepath.Join(c.PromptDir, "short-001-demo-compilation-cover.md"),
		PublishPath:       filepath.Join(opts.PublishDir, publishBase+".mp4"),
		MusicPath:         opts.MusicPath,
		RhythmPath:        opts.RhythmPath,
		OutputFormat:      c.OutputFormat,
		KillEffect:        c.KillEffect,
		Transition:        c.Transition,
		Intro:             opts.Intro,
		Outro:             opts.Outro,
		IntroText:         opts.IntroText,
		OutroText:         opts.OutroText,
		HookText:          c.HookText,
		KillCounter:       c.KillCounter,
		KillfeedOverlay:   c.KillfeedOverlay,
		TailTrimSeconds:   tailTrimForRhythm(c),
		OutputFPS:         c.OutputFPS,
		VideoCRF:          c.VideoCRF,
		VideoPreset:       c.VideoPreset,
		HQFilters:         c.HQFilters,
		AudioNormalize:    c.AudioNormalize,
		TemporalSmoothing: c.TemporalSmoothing,
		CaptionPath:       filepath.Join(opts.PublishDir, publishBase+".caption.txt"),
		CoverTimeSeconds:  coverTime,
		DurationSeconds:   duration,
		Label:             shortLabel(c.Player, c.MapName, killCount),
		Title:             title,
		Headline:          premiumHeadline(c.MapName, killCount, primary),
		Caption:           caption,
		Hashtags:          hashtags,
		Kills:             kills,
		Parts:             parts,
		RenderLogPath:     filepath.Join(c.LogDir, "short-001-demo-compilation-render.log"),
	}
	if opts.CoversEnabled {
		short.CoverPath = filepath.Join(opts.PublishDir, publishBase+".cover.jpg")
		if c.CoverSheets {
			short.CoverSheetPath = filepath.Join(opts.PublishDir, publishBase+".sheet.jpg")
		}
	}
	if c.QualityChecks {
		short.QualityLogPath = filepath.Join(c.LogDir, "short-001-demo-compilation-quality.log")
	}
	return short, nil
}

func publishCompiledFileBase(index int, player, mapName string, killCount int, weapon string) string {
	parts := []string{
		fmt.Sprintf("%02d", index),
		"demo-compilation",
		safeFilenameToken(player),
		safeFilenameToken(mapName),
	}
	if killCount > 0 {
		parts = append(parts, fmt.Sprintf("%dk", killCount))
	}
	parts = append(parts, safeFilenameToken(weapon))

	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "_")
}

func normalizeVideoCRF(crf int) (int, error) {
	if crf == 0 {
		return DefaultVideoCRF, nil
	}
	if crf < 1 || crf > 51 {
		return 0, fmt.Errorf("video crf must be between 1 and 51")
	}
	return crf, nil
}

func normalizeVideoCRFForPreset(preset string, crf int) (int, error) {
	if crf == 0 {
		if renderPreset, ok := PresetByName(preset); ok {
			return renderPreset.VideoCRF, nil
		}
	}
	return normalizeVideoCRF(crf)
}

func normalizeVideoPreset(preset string) (string, error) {
	preset = strings.ToLower(strings.TrimSpace(preset))
	if preset == "" {
		return DefaultVideoPreset, nil
	}
	switch preset {
	case "ultrafast", "superfast", "veryfast", "faster", "fast", "medium", "slow", "slower", "veryslow":
		return preset, nil
	default:
		return "", fmt.Errorf("unknown video preset %q", preset)
	}
}

func normalizeVideoPresetForPreset(editPreset, videoPreset string) (string, error) {
	if strings.TrimSpace(videoPreset) == "" {
		if renderPreset, ok := PresetByName(editPreset); ok {
			return renderPreset.VideoPreset, nil
		}
	}
	return normalizeVideoPreset(videoPreset)
}

func normalizeOutputFPS(fps int) (int, error) {
	return normalizeOutputFPSForPreset(DefaultPreset(), fps)
}

func normalizeOutputFPSForPreset(preset RenderPreset, fps int) (int, error) {
	if fps == 0 {
		return preset.FPS, nil
	}
	if fps < 1 || fps > 240 {
		return 0, fmt.Errorf("output fps must be between 1 and 240")
	}
	return fps, nil
}

func normalizeOutputFormat(format string) (string, error) {
	format = strings.TrimSpace(format)
	if format == "" {
		return OutputFormatShort9x16, nil
	}
	switch format {
	case OutputFormatShort9x16, OutputFormatLandscape16x9:
		return format, nil
	default:
		return "", fmt.Errorf("unknown output format %q", format)
	}
}

func normalizeKillEffect(effect string) (string, error) {
	effect = strings.TrimSpace(effect)
	if effect == "" {
		return KillEffectClean, nil
	}
	switch effect {
	case KillEffectClean, KillEffectPunchIn, KillEffectVelocity, KillEffectFreezeFlash:
		return effect, nil
	default:
		return "", fmt.Errorf("unknown kill effect %q", effect)
	}
}

func normalizeTransition(transition string) (string, error) {
	transition = strings.TrimSpace(transition)
	if transition == "" {
		return TransitionCut, nil
	}
	switch transition {
	case TransitionCut, TransitionFlash, TransitionWhip, TransitionDip:
		return transition, nil
	default:
		return "", fmt.Errorf("unknown transition %q", transition)
	}
}

func segmentIDSet(ids []string) map[string]bool {
	if len(ids) == 0 {
		return nil
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func premiumHeadline(mapName string, killCount int, weapon string) string {
	parts := []string{}
	if killCount > 0 {
		parts = append(parts, fmt.Sprintf("%dK", killCount))
	} else {
		parts = append(parts, "Highlight")
	}
	if weapon != "" {
		parts = append(parts, "con", weapon)
	}
	if mapName != "" {
		parts = append(parts, "en", prettifyMapName(mapName))
	}
	return strings.Join(parts, " ")
}

// prettifyMapName turns a raw CS2 map name such as "de_dust2" into the
// human-readable form ("Dust2") used in headlines: it drops the de_/cs_
// workshop prefix and capitalizes what remains. Names without one of those
// prefixes, or that are empty, pass through unchanged.
func prettifyMapName(mapName string) string {
	name := strings.TrimSpace(mapName)
	if name == "" {
		return name
	}
	lower := strings.ToLower(name)
	for _, prefix := range []string{"de_", "cs_"} {
		if strings.HasPrefix(lower, prefix) {
			rest := name[len(prefix):]
			if rest == "" {
				return name
			}
			return strings.ToUpper(rest[:1]) + rest[1:]
		}
	}
	return name
}

func coverTimeSeconds(kills []KillCue, duration float64) float64 {
	if len(kills) > 0 {
		t := kills[0].TimeSeconds - 0.12
		if t < 0 {
			return 0
		}
		if duration > 0 && t > duration {
			return duration
		}
		return t
	}
	if duration <= 0 {
		return 0
	}
	return duration * 0.35
}

func killCues(segment recording.RecordingSegment, tickrate int) []KillCue {
	if tickrate <= 0 {
		return nil
	}
	recordStart := recording.EffectiveRecordStartTick(segment, tickrate)
	kills := append([]killplan.Kill(nil), segment.Kills...)
	sort.SliceStable(kills, func(i, j int) bool {
		return kills[i].Tick < kills[j].Tick
	})
	out := make([]KillCue, 0, len(kills))
	for _, kill := range kills {
		if kill.Tick <= 0 || kill.Tick < recordStart || kill.Tick > segment.TickEnd {
			continue
		}
		out = append(out, KillCue{
			Tick:        kill.Tick,
			TimeSeconds: float64(kill.Tick-recordStart) / float64(tickrate),
			Weapon:      formatWeapon(kill.Weapon),
			Victim:      kill.Victim.NameInDemo,
			Headshot:    kill.Headshot,
			Wallbang:    kill.Wallbang,
		})
	}
	return out
}

func smokeCues(segment recording.RecordingSegment, tickrate int, mapName string, catalog lineups.Catalog, catalogRequested bool) []SmokeCue {
	if tickrate <= 0 {
		return nil
	}
	recordStart := recording.EffectiveRecordStartTick(segment, tickrate)
	utility := append([]killplan.UtilityThrow(nil), segment.Utility...)
	sort.SliceStable(utility, func(i, j int) bool {
		return utility[i].ThrowTick < utility[j].ThrowTick
	})
	out := make([]SmokeCue, 0, len(utility))
	for _, smoke := range utility {
		if !isOverlayUtilityType(smoke.Type) {
			continue
		}
		if smoke.ThrowTick <= 0 || smoke.ThrowTick < recordStart || smoke.ThrowTick > segment.TickEnd {
			continue
		}
		match := smoke.LineupMatch
		if (match == nil || match.ID == "" || strings.HasPrefix(match.ID, "auto-")) && !catalog.Empty() {
			if m, ok := catalog.MatchSmoke(mapName, smoke); ok {
				match = &m
			}
		}
		cue := SmokeCue{
			ID:          smoke.ID,
			Type:        smoke.Type,
			Round:       smoke.Round,
			ThrowTick:   smoke.ThrowTick,
			PopTick:     smoke.PopTick,
			ExpireTick:  smoke.ExpireTick,
			TimeSeconds: roundMillis(float64(smoke.ThrowTick-recordStart) / float64(tickrate)),
			ThrowPlace:  smoke.ThrowPlace,
			ThrowAction: smoke.ThrowAction,
			Stance:      smoke.Stance,
			Movement:    smoke.Movement,
			Speed2D:     smoke.Speed2D,
			OnGround:    smoke.OnGround,
			Walking:     smoke.Walking,
			Ducking:     smoke.Ducking,
			ThrowPos:    smoke.ThrowPos,
			LandingPos:  smoke.LandingPos,
		}
		if smoke.PopTick > 0 && smoke.PopTick >= recordStart && smoke.PopTick <= segment.TickEnd {
			cue.PopTimeSeconds = roundMillis(float64(smoke.PopTick-recordStart) / float64(tickrate))
		}
		if match != nil && match.ID != "" {
			cue.Destination = match.Destination
			cue.FromArea = match.FromArea
			cue.Side = match.Side
			cue.MatchID = match.ID
			cue.Confidence = match.Confidence
			cue.DistanceUnits = match.DistanceUnits
			cue.Matched = true
		} else if catalogRequested {
			cue.UnmatchedReason = "no catalog match"
		}
		out = append(out, cue)
	}
	return out
}

func parserSmokeGrenadeType() string {
	return "smokegrenade"
}

func isOverlayUtilityType(typ string) bool {
	switch typ {
	case "smokegrenade", "flashbang", "molotov", "incgrenade":
		return true
	default:
		return false
	}
}

func primarySmoke(smokes []SmokeCue) string {
	if len(smokes) == 0 {
		return ""
	}
	if smokes[0].Destination != "" {
		return smokes[0].Destination
	}
	return utilityDisplayName(smokes[0].Type)
}

func smokeLabel(player, mapName string, smoke SmokeCue) string {
	parts := []string{}
	if player != "" {
		parts = append(parts, player)
	}
	if mapName != "" {
		parts = append(parts, mapName)
	}
	if smoke.Destination != "" {
		parts = append(parts, utilityDisplayName(smoke.Type)+" "+smoke.Destination)
	} else {
		parts = append(parts, utilityDisplayName(smoke.Type))
	}
	return strings.Join(parts, " | ")
}

func smokeHeadline(mapName string, smoke SmokeCue) string {
	destination := smoke.Destination
	if destination == "" {
		destination = utilityDisplayName(smoke.Type)
	}
	if smoke.FromArea != "" && smoke.Destination != "" {
		destination = smoke.FromArea + " -> " + smoke.Destination
	}
	if mapName != "" {
		return destination + " en " + prettifyMapName(mapName)
	}
	return destination
}

func utilityDisplayName(typ string) string {
	switch typ {
	case "flashbang":
		return "Flash"
	case "molotov":
		return "Molotov"
	case "incgrenade":
		return "Incendiary"
	case "smokegrenade":
		return "Smoke"
	default:
		return "Utility"
	}
}

func coverTimeSecondsForSmoke(smoke SmokeCue, duration float64) float64 {
	t := smoke.TimeSeconds + 0.25
	if smoke.PopTimeSeconds > 0 {
		t = smoke.PopTimeSeconds
	}
	if t < 0 {
		return 0
	}
	if duration > 0 && t > duration {
		return duration
	}
	return t
}

// tailTrimmedDuration shortens a clip to end tailSeconds after its final kill,
// cutting the recorded quit-tick dead air. Kill-less clips (smoke lineups) and
// a zero tail keep the full duration, and the trim never lands before the last
// kill itself so a tick/frame offset cannot clip the payoff off-screen.
func tailTrimmedDuration(kills []KillCue, duration, tailSeconds float64) float64 {
	if tailSeconds <= 0 || len(kills) == 0 || duration <= 0 {
		return duration
	}
	lastKill := kills[0].TimeSeconds
	for _, kill := range kills[1:] {
		if kill.TimeSeconds > lastKill {
			lastKill = kill.TimeSeconds
		}
	}
	trimmed := lastKill + tailSeconds
	if trimmed >= duration {
		return duration
	}
	if trimmed < lastKill {
		return lastKill
	}
	return roundMillis(trimmed)
}

// tailTrimForRhythm reports the effective tail trim for a compiled short: zero
// under rhythm sync, where trimming is skipped to keep segments on the beat.
func tailTrimForRhythm(c compiledShortOptions) float64 {
	if c.RhythmSync != nil {
		return 0
	}
	return c.TailTrimSeconds
}

func clipDuration(segment recording.RecordingSegment, tickrate int, clipDuration float64) float64 {
	if clipDuration > 0 {
		return clipDuration
	}
	if tickrate <= 0 {
		return 0
	}
	recordStart := recording.EffectiveRecordStartTick(segment, tickrate)
	if segment.TickEnd <= recordStart {
		return 0
	}
	return float64(segment.TickEnd-recordStart) / float64(tickrate)
}

func shortLabel(player, mapName string, killCount int) string {
	parts := []string{}
	if player != "" {
		parts = append(parts, player)
	}
	if mapName != "" {
		parts = append(parts, mapName)
	}
	if killCount > 0 {
		parts = append(parts, fmt.Sprintf("%dK", killCount))
	}
	if len(parts) == 0 {
		return "CS2 highlight"
	}
	return strings.Join(parts, " | ")
}

func playerName(plan recording.RecordingPlan) string {
	if plan.TargetNameInDemo != "" {
		return plan.TargetNameInDemo
	}
	if plan.TargetSteamID64 != "" {
		return plan.TargetSteamID64
	}
	return "target player"
}

func mapName(plan recording.RecordingPlan, kp *killplan.Plan) (string, string) {
	if plan.DemoMap != "" {
		return plan.DemoMap, ""
	}
	if kp != nil && kp.Demo.Map != "" {
		return kp.Demo.Map, ""
	}
	inferred := inferMapFromPath(plan.DemoPath)
	if inferred != "" {
		return inferred, ""
	}
	return "", "demo map missing from recording result; labels and prompts use a map-neutral fallback"
}

func inferMapFromPath(path string) string {
	name := strings.ToLower(filepath.Base(path))
	for _, candidate := range []string{
		"de_ancient",
		"de_anubis",
		"de_dust2",
		"de_inferno",
		"de_mirage",
		"de_nuke",
		"de_overpass",
		"de_train",
		"de_vertigo",
	} {
		if strings.Contains(name, candidate) {
			return candidate
		}
	}
	return ""
}

func resolvePath(baseDir, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Clean(filepath.Join(baseDir, path))
}

func primaryWeapon(kills []killplan.Kill) string {
	counts := map[string]int{}
	first := map[string]int{}
	for i, kill := range kills {
		weapon := formatWeapon(kill.Weapon)
		if weapon == "" {
			continue
		}
		counts[weapon]++
		if _, ok := first[weapon]; !ok {
			first[weapon] = i
		}
	}
	best := ""
	for weapon, count := range counts {
		if best == "" || count > counts[best] || count == counts[best] && first[weapon] < first[best] {
			best = weapon
		}
	}
	return best
}

func formatWeapon(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	raw = strings.TrimPrefix(raw, "weapon_")
	raw = strings.ReplaceAll(raw, "-", "_")
	switch raw {
	case "":
		return ""
	case "ak47":
		return "AK-47"
	case "aug":
		return "AUG"
	case "awp":
		return "AWP"
	case "deagle":
		return "Desert Eagle"
	case "famas":
		return "FAMAS"
	case "galilar":
		return "Galil AR"
	case "glock":
		return "Glock-18"
	case "hkp2000":
		return "P2000"
	case "m4a1":
		return "M4A4"
	case "m4a1_silencer":
		return "M4A1-S"
	case "mac10":
		return "MAC-10"
	case "mp9":
		return "MP9"
	case "ssg08":
		return "SSG 08"
	case "usp_silencer":
		return "USP-S"
	case "xm1014":
		return "XM1014"
	default:
		parts := strings.Split(raw, "_")
		for i, part := range parts {
			if part == "" {
				continue
			}
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
		return strings.Join(parts, " ")
	}
}
