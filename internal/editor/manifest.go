package editor

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reche/zackvideo/internal/composition"
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/lineups"
	"github.com/reche/zackvideo/internal/recording"
)

func BuildManifest(result recording.RecordingResult, opts ManifestOptions) Manifest {
	manifest, err := buildManifest(result, opts)
	if err != nil {
		manifest.Warnings = append(manifest.Warnings, err.Error())
	}
	return manifest
}

func buildManifest(result recording.RecordingResult, opts ManifestOptions) (Manifest, error) {
	clips, warnings := composition.SegmentClipsFromRecording(result)
	clipBySegment := map[string]composition.SegmentClip{}
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
		preset = PresetShortClean
	}
	effectsPreset := opts.EffectsPreset
	if preset == PresetSmokeLineups && opts.EffectsPath == "" && strings.TrimSpace(effectsPreset) == "" {
		effectsPreset = EffectsPresetSmokeLineups
	}
	if isNaturalPreset(preset) && opts.EffectsPath == "" && strings.TrimSpace(effectsPreset) == "" {
		effectsPreset = EffectsPresetNone
	}
	effectsSource, err := loadEffectsSource(opts.EffectsPath, effectsPreset)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	playerImagePath := ""
	playerKeyColor := ""
	if preset == PresetShortPremiumPlayer {
		playerImagePath = opts.PlayerImagePath
		playerKeyColor = opts.PlayerKeyColor
	}
	videoCRF, err := normalizeVideoCRFForPreset(preset, opts.VideoCRF)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	videoPreset, err := normalizeVideoPresetForPreset(preset, opts.VideoPreset)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	hqFeaturesDefault := preset == PresetShortNaturalHQ2 || preset == PresetShortNaturalHQ3 || preset == PresetShortNaturalHQ3Smooth || preset == PresetSmokeLineups
	hqFilters := opts.HQFilters || hqFeaturesDefault
	audioNormalize := opts.AudioNormalize || hqFeaturesDefault
	qualityChecks := opts.QualityChecks || hqFeaturesDefault
	coverSheets := opts.CoverSheets || hqFeaturesDefault
	temporalSmoothing := opts.TemporalSmoothing || preset == PresetShortNaturalHQ3Smooth
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
		LineupCatalogPath: opts.LineupCatalogPath,
		PlayerImage:       playerImagePath,
		PlayerKeyColor:    playerKeyColor,
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
		output := filepath.Join(opts.OutputDir, fmt.Sprintf("short-%03d-%s.mp4", index, segment.ID))
		promptPath := filepath.Join(promptDir, fmt.Sprintf("short-%03d-%s-cover.md", index, segment.ID))
		logBase := fmt.Sprintf("short-%03d-%s", index, segment.ID)
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
		publishBase := publishFileBase(index, segment.ID, player, mapName, killCount, primaryWeapon)
		if smokeCount > 0 && killCount == 0 {
			publishBase = publishSmokeFileBase(index, segment.ID, player, mapName, smokes[0])
		}
		duration := clipDuration(segment, result.Plan.Tickrate, clip.DurationSeconds)
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
			PlayerImage:       playerImagePath,
			PlayerKeyColor:    playerKeyColor,
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
	if err := applyEffectsToManifest(&manifest, effectsSource, opts.FFmpegPath); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func isNaturalPreset(preset string) bool {
	return preset == PresetShortNaturalHQ || preset == PresetShortNaturalHQ2 || preset == PresetShortNaturalHQ3 || preset == PresetShortNaturalHQ3Smooth
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
	if crf == 0 && (preset == PresetShortNaturalHQ3 || preset == PresetShortNaturalHQ3Smooth) {
		return NaturalHQ3VideoCRF, nil
	}
	if crf == 0 && (isNaturalPreset(preset) || preset == PresetSmokeLineups) {
		return NaturalHQVideoCRF, nil
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
	if strings.TrimSpace(videoPreset) == "" && (editPreset == PresetShortNaturalHQ3 || editPreset == PresetShortNaturalHQ3Smooth) {
		return NaturalHQ3VideoPreset, nil
	}
	if strings.TrimSpace(videoPreset) == "" && (isNaturalPreset(editPreset) || editPreset == PresetSmokeLineups) {
		return NaturalHQVideoPreset, nil
	}
	return normalizeVideoPreset(videoPreset)
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
		parts = append(parts, "en", mapName)
	}
	return strings.Join(parts, " ")
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
		return destination + " en " + mapName
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
