package editor

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reche/zackvideo/internal/composition"
	"github.com/reche/zackvideo/internal/killplan"
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
	effectsSource, err := loadEffectsSource(opts.EffectsPath, opts.EffectsPreset)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
	playerImagePath := ""
	playerKeyColor := ""
	if preset == PresetShortPremiumPlayer {
		playerImagePath = opts.PlayerImagePath
		playerKeyColor = opts.PlayerKeyColor
	}
	segmentFilter := uniqueSegmentIDs(opts.SegmentIDs)
	manifest := Manifest{
		Preset:          preset,
		RecordingResult: opts.RecordingResultPath,
		KillPlan:        opts.KillPlanPath,
		OutputDir:       opts.OutputDir,
		PublishDir:      opts.PublishDir,
		GalleryPath:     filepath.Join(opts.PublishDir, "index.html"),
		SummaryPath:     filepath.Join(opts.PublishDir, "publish-summary.md"),
		SegmentFilter:   append([]string(nil), segmentFilter...),
		Limit:           opts.Limit,
		SkipExisting:    opts.SkipExisting,
		EffectsPath:     effectsSource.Path,
		EffectsPreset:   effectsSource.Preset,
		PlayerImage:     playerImagePath,
		PlayerKeyColor:  playerKeyColor,
		CoversEnabled:   opts.CoversEnabled,
		Warnings:        warnings,
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
		kills := killCues(segment, result.Plan.Tickrate)
		killCount := len(segment.Kills)
		primaryWeapon := primaryWeapon(segment.Kills)
		label := shortLabel(player, mapName, killCount)
		headline := premiumHeadline(player, killCount, primaryWeapon)
		title, caption, hashtags := publishText(player, mapName, killCount, primaryWeapon)
		publishBase := publishFileBase(index, segment.ID, player, mapName, killCount, primaryWeapon)
		coverTime := coverTimeSeconds(kills, clipDuration(segment, result.Plan.Tickrate, clip.DurationSeconds))
		edit := ShortEdit{
			Index:           index,
			SegmentID:       segment.ID,
			Preset:          preset,
			Player:          player,
			Map:             mapName,
			KillCount:       killCount,
			PrimaryWeapon:   primaryWeapon,
			Input:           input,
			Output:          output,
			PromptPath:      promptPath,
			PublishPath:     filepath.Join(opts.PublishDir, publishBase+".mp4"),
			PlayerImage:     playerImagePath,
			PlayerKeyColor:  playerKeyColor,
			CaptionPath:     filepath.Join(opts.PublishDir, publishBase+".caption.txt"),
			DurationSeconds: clipDuration(segment, result.Plan.Tickrate, clip.DurationSeconds),
			Label:           label,
			Title:           title,
			Headline:        headline,
			Caption:         caption,
			Hashtags:        hashtags,
			Kills:           kills,
		}
		if opts.CoversEnabled {
			edit.CoverPath = filepath.Join(opts.PublishDir, publishBase+".cover.jpg")
			edit.CoverTimeSeconds = coverTime
		}
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

func premiumHeadline(player string, killCount int, weapon string) string {
	parts := []string{}
	if player != "" {
		parts = append(parts, player)
	}
	if killCount > 0 {
		parts = append(parts, fmt.Sprintf("%dK", killCount))
	}
	if weapon != "" {
		parts = append(parts, weapon)
	}
	if len(parts) == 0 {
		return "CS2 highlight"
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
