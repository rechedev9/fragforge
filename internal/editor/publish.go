package editor

import (
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/rechedev9/fragforge/internal/recording"
)

func publishFileBase(index int, segmentID, player, mapName string, killCount int, weapon string) string {
	parts := []string{
		fmt.Sprintf("%02d", index),
		safeFilenameToken(segmentID),
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
	if len(out) == 0 {
		return fmt.Sprintf("%02d_short", index)
	}
	return strings.Join(out, "_")
}

func safeFilenameToken(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var sb strings.Builder
	lastUnderscore := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			sb.WriteRune(r)
			lastUnderscore = false
		case r == '-' && strings.HasPrefix(raw, "seg-"):
			sb.WriteRune(r)
			lastUnderscore = false
		case r == '-':
			continue
		case unicode.IsSpace(r) || r == '_':
			if !lastUnderscore && sb.Len() > 0 {
				sb.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(sb.String(), "_")
}

func publishText(player, mapName string, killCount int, weapon string) (string, string, []string) {
	killLabel := "highlight"
	if killCount > 0 {
		killLabel = fmt.Sprintf("%dK", killCount)
	}
	displayMap := displayMapName(mapName)
	titleParts := []string{player, killLabel, weapon}
	if displayMap != "" {
		titleParts = append(titleParts, "|", displayMap, "CS2")
	}
	title := strings.Join(nonEmpty(titleParts), " ")
	if title == "" {
		title = "CS2 highlight"
	}

	weaponPhrase := ""
	if weapon != "" {
		weaponPhrase = " with the " + weapon
	}
	mapPhrase := "in CS2"
	if displayMap != "" {
		mapPhrase = "on " + displayMap
	}
	subject := player
	if subject == "" {
		subject = "This player"
	}
	caption := fmt.Sprintf("%s turns this round %s into a clean %s%s.", subject, mapPhrase, killLabel, weaponPhrase)
	hashtags := publishHashtags(player, mapName, weapon, "#CS2Highlights")
	return title, caption + "\n\n" + strings.Join(hashtags, " "), hashtags
}

func publishSmokeText(player, mapName string, smoke SmokeCue) (string, string, []string) {
	utility := utilityDisplayName(smoke.Type)
	displayMap := displayMapName(mapName)
	destination := displayPlaceName(smoke.Destination)
	if destination == "" {
		destination = strings.ToLower(utility)
	}
	fromArea := displayPlaceName(smoke.FromArea)
	route := destination
	if fromArea != "" && destination != "" {
		route = fromArea + " to " + destination
	}
	titleMain := strings.Join(nonEmpty([]string{player, destination, utility}), " ")
	if fromArea != "" {
		titleMain += " from " + fromArea
	}
	title := titleMain
	if displayMap != "" {
		title = strings.Join(nonEmpty([]string{titleMain, "|", displayMap, "CS2"}), " ")
	}
	if title == "" {
		title = "CS2 utility lineup"
	}
	mapPhrase := "in CS2"
	if displayMap != "" {
		mapPhrase = "on " + displayMap
	}
	subject := player
	if subject == "" {
		subject = "This player"
	}
	action := publishActionLabel(smoke)
	actionPhrase := ""
	if action != "" {
		actionPhrase = action + " "
	}
	caption := fmt.Sprintf("%s throws a %s%s %s: %s.", subject, actionPhrase, strings.ToLower(utility), mapPhrase, route)
	caption += "\nCS2 " + firstNonEmpty(displayMap, "utility") + " utility reference."
	if !smoke.Matched {
		caption = fmt.Sprintf("%s throws %s %s. Destination needs manual catalog review.", subject, articleFor(utility), mapPhrase)
	}
	hashtags := publishHashtags(player, mapName, utility, "#CS2Lineups", "#CS2Utility")
	return title, caption + "\n\n" + strings.Join(hashtags, " "), hashtags
}

func publishSmokeFileBase(index int, segmentID, player, mapName string, smoke SmokeCue) string {
	target := smoke.Destination
	if target == "" {
		target = utilityDisplayName(smoke.Type)
	}
	parts := []string{
		fmt.Sprintf("%02d", index),
		safeFilenameToken(segmentID),
		safeFilenameToken(player),
		safeFilenameToken(mapName),
		safeFilenameToken(utilityDisplayName(smoke.Type)),
		safeFilenameToken(target),
	}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "_")
}

func articleFor(word string) string {
	if word == "" {
		return "utility"
	}
	switch strings.ToLower(word[:1]) {
	case "a", "e", "i", "o", "u":
		return "an " + strings.ToLower(word)
	default:
		return "a " + strings.ToLower(word)
	}
}

func appendUniqueHashtags(tags []string, extra ...string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags)+len(extra))
	for _, tag := range append(tags, extra...) {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if !strings.HasPrefix(tag, "#") {
			tag = "#" + tag
		}
		key := strings.ToLower(tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, tag)
	}
	return out
}

func publishHashtags(player, mapName, weapon string, extra ...string) []string {
	raw := []string{"CS2", "CounterStrike2", socialTag(displayMapName(mapName)), socialTag(weapon)}
	raw = append(raw, extra...)
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, value := range raw {
		value = strings.TrimPrefix(strings.TrimSpace(value), "#")
		if value == "" {
			continue
		}
		tag := "#" + value
		if seen[strings.ToLower(tag)] {
			continue
		}
		seen[strings.ToLower(tag)] = true
		out = append(out, tag)
		if len(out) >= 5 {
			break
		}
	}
	return out
}

func publishActionLabel(smoke SmokeCue) string {
	action := strings.TrimSpace(smoke.ThrowAction)
	if action == "" {
		return ""
	}
	if action == "jumpthrow" {
		switch smoke.Stance {
		case "crouching", "crouching_in_progress":
			return "crouch jumpthrow"
		case "standing":
			return "standing jumpthrow"
		}
	}
	return strings.ReplaceAll(action, "_", " ")
}

func displayMapName(raw string) string {
	raw = strings.TrimSpace(raw)
	switch strings.ToLower(raw) {
	case "":
		return ""
	case "de_inferno":
		return "Inferno"
	case "de_dust2":
		return "Dust2"
	case "de_anubis":
		return "Anubis"
	case "de_ancient":
		return "Ancient"
	case "de_mirage":
		return "Mirage"
	case "de_nuke":
		return "Nuke"
	case "de_overpass":
		return "Overpass"
	case "de_train":
		return "Train"
	case "de_vertigo":
		return "Vertigo"
	default:
		return displayPlaceName(strings.TrimPrefix(raw, "de_"))
	}
}

func displayPlaceName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	switch strings.ToLower(raw) {
	case "ctspawn":
		return "CT Spawn"
	case "tspawn":
		return "T Spawn"
	case "bombsitea":
		return "A Site"
	case "bombsiteb":
		return "B Site"
	case "secondmid":
		return "Second Mid"
	default:
		return titleWords(raw)
	}
}

func titleWords(raw string) string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '_' || r == '-' || unicode.IsSpace(r)
	})
	for i, field := range fields {
		fields[i] = titleWord(field)
	}
	out := strings.Join(fields, " ")
	out = strings.ReplaceAll(out, " / ", "/")
	out = strings.ReplaceAll(out, "/", " / ")
	return strings.Join(strings.Fields(out), " ")
}

func titleWord(raw string) string {
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	switch lower {
	case "a", "b", "ct", "t":
		return strings.ToUpper(lower)
	case "cs2":
		return "CS2"
	default:
		return strings.ToUpper(lower[:1]) + lower[1:]
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func socialTag(raw string) string {
	var sb strings.Builder
	upperNext := true
	for _, r := range raw {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			if upperNext && r >= 'a' && r <= 'z' {
				r = r - 'a' + 'A'
			}
			sb.WriteRune(r)
			upperNext = false
			continue
		}
		upperNext = true
	}
	return sb.String()
}

func captionFileContent(short ShortEdit) string {
	var sb strings.Builder
	sb.WriteString("Title: ")
	sb.WriteString(short.Title)
	sb.WriteString("\n\nCaption:\n")
	sb.WriteString(short.Caption)
	sb.WriteString("\n")
	return sb.String()
}

func writeCaptions(manifest Manifest) []string {
	var warnings []string
	for _, short := range manifest.Shorts {
		if err := os.MkdirAll(filepath.Dir(short.CaptionPath), 0o750); err != nil {
			warnings = append(warnings, fmt.Sprintf("write caption for %s: %v", short.SegmentID, err))
			continue
		}
		if err := os.WriteFile(short.CaptionPath, []byte(captionFileContent(short)), 0o600); err != nil {
			warnings = append(warnings, fmt.Sprintf("write caption for %s: %v", short.SegmentID, err))
		}
	}
	return warnings
}

func publishShort(short ShortEdit) error {
	if short.Output == "" || short.PublishPath == "" {
		return fmt.Errorf("publish path is required")
	}
	if err := os.MkdirAll(filepath.Dir(short.PublishPath), 0o750); err != nil {
		return err
	}
	if err := os.Remove(short.PublishPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Link(short.Output, short.PublishPath); err == nil {
		return nil
	}
	return copyFile(short.Output, short.PublishPath)
}

func copyFile(src, dst string) error {
	// #nosec G304 -- src is a generated local render path from the edit manifest.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	// #nosec G304 -- dst is a generated publish path under the configured output directory.
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func PackManifestFromManifest(manifest Manifest, result Result) PackManifest {
	artifacts := map[string]recording.RecordingArtifact{}
	coverArtifacts := map[string]recording.RecordingArtifact{}
	coverSheetArtifacts := map[string]recording.RecordingArtifact{}
	for _, short := range result.Shorts {
		if short.SegmentID != "" {
			artifacts[short.SegmentID] = short.PublishArtifact
			coverArtifacts[short.SegmentID] = short.CoverArtifact
			coverSheetArtifacts[short.SegmentID] = short.CoverSheetArtifact
		}
	}
	pack := PackManifest{
		Preset:            manifest.Preset,
		RecordingResult:   manifest.RecordingResult,
		KillPlan:          manifest.KillPlan,
		PublishDir:        manifest.PublishDir,
		GalleryPath:       manifest.GalleryPath,
		SummaryPath:       manifest.SummaryPath,
		SegmentFilter:     append([]string(nil), manifest.SegmentFilter...),
		Limit:             manifest.Limit,
		SkipExisting:      manifest.SkipExisting,
		EffectsPath:       manifest.EffectsPath,
		EffectsPreset:     manifest.EffectsPreset,
		MusicPath:         manifest.MusicPath,
		RhythmPath:        manifest.RhythmPath,
		OutputFormat:      manifest.OutputFormat,
		KillEffect:        manifest.KillEffect,
		Transition:        manifest.Transition,
		Intro:             manifest.Intro,
		Outro:             manifest.Outro,
		OutputFPS:         manifest.OutputFPS,
		CompileSegments:   manifest.CompileSegments,
		LineupCatalogPath: manifest.LineupCatalogPath,
		UnmatchedSmokes:   manifest.UnmatchedSmokes,
		VideoCRF:          manifest.VideoCRF,
		VideoPreset:       manifest.VideoPreset,
		HQFilters:         manifest.HQFilters,
		AudioNormalize:    manifest.AudioNormalize,
		QualityChecks:     manifest.QualityChecks,
		CoverSheets:       manifest.CoverSheets,
		TemporalSmoothing: manifest.TemporalSmoothing,
		CoversEnabled:     manifest.CoversEnabled,
		Warnings:          append([]string(nil), result.Warnings...),
	}
	for _, short := range manifest.Shorts {
		pack.Items = append(pack.Items, PublishItem{
			Index:              short.Index,
			SegmentID:          short.SegmentID,
			Preset:             short.Preset,
			Player:             short.Player,
			Map:                short.Map,
			KillCount:          short.KillCount,
			PrimaryWeapon:      short.PrimaryWeapon,
			SmokeCount:         short.SmokeCount,
			PrimarySmoke:       short.PrimarySmoke,
			Source:             short.Output,
			Video:              short.PublishPath,
			SourceArtifact:     short.SourceArtifact,
			MusicPath:          short.MusicPath,
			RhythmPath:         short.RhythmPath,
			OutputFormat:       short.OutputFormat,
			KillEffect:         short.KillEffect,
			Transition:         short.Transition,
			Intro:              short.Intro,
			Outro:              short.Outro,
			OutputFPS:          short.OutputFPS,
			VideoCRF:           short.VideoCRF,
			VideoPreset:        short.VideoPreset,
			HQFilters:          short.HQFilters,
			AudioNormalize:     short.AudioNormalize,
			TemporalSmoothing:  short.TemporalSmoothing,
			CaptionPath:        short.CaptionPath,
			CoverPath:          short.CoverPath,
			CoverSheetPath:     short.CoverSheetPath,
			CoverTimeSeconds:   short.CoverTimeSeconds,
			Title:              short.Title,
			Headline:           short.Headline,
			Caption:            short.Caption,
			Hashtags:           append([]string(nil), short.Hashtags...),
			Effects:            append([]Effect(nil), short.Effects...),
			Smokes:             append([]SmokeCue(nil), short.Smokes...),
			Parts:              append([]ShortPart(nil), short.Parts...),
			DurationSeconds:    short.DurationSeconds,
			Artifact:           artifacts[short.SegmentID],
			CoverArtifact:      coverArtifacts[short.SegmentID],
			CoverSheetArtifact: coverSheetArtifacts[short.SegmentID],
		})
	}
	return pack
}

func WritePublishGallery(path string, manifest Manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	var sb strings.Builder
	galleryDir := filepath.Dir(path)
	weapons := galleryWeapons(manifest.Shorts)
	maxKills := maxKillCount(manifest.Shorts)
	sb.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n")
	sb.WriteString("  <meta charset=\"utf-8\">\n")
	sb.WriteString("  <meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	sb.WriteString("  <title>FragForge Publish Pack</title>\n")
	sb.WriteString("  <style>\n")
	sb.WriteString("    :root{color-scheme:dark;font-family:Arial,Helvetica,sans-serif;background:#101010;color:#f4f4f4}body{margin:0;padding:24px;background:#101010}header,main{max-width:1500px;margin:0 auto}header{margin-bottom:20px}h1{font-size:24px;margin:0 0 6px}.summary{margin:0;color:#bbb;font-size:14px}.summary span{color:#f4f4f4}.filters{display:flex;flex-wrap:wrap;gap:8px;margin-top:14px;align-items:center}.filters input,.filters select,.filters button{height:34px;border:1px solid #353535;border-radius:6px;background:#181818;color:#f4f4f4;padding:0 10px;font-size:13px}.filters input{min-width:260px}.filters button{cursor:pointer;background:#242424}.filters button:hover{background:#303030}.count{color:#bbb;font-size:13px}.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(230px,1fr));gap:18px}.item{background:#1a1a1a;border:1px solid #2c2c2c;border-radius:8px;overflow:hidden}.item[hidden]{display:none}.media{background:#000}.media video{display:block;width:100%;aspect-ratio:9/16;object-fit:contain;background:#000}.body{padding:12px}.title{font-size:15px;font-weight:700;margin-bottom:7px}.meta{color:#c8c8c8;font-size:12px;margin-bottom:8px}.diag{color:#aeb6bd;font-size:11px;line-height:1.35;margin-bottom:4px}.path{color:#999;font-size:12px;word-break:break-all}.tools{display:flex;flex-wrap:wrap;gap:6px;margin-top:10px}.tools a,.tools button{color:#f4f4f4;text-decoration:none;border:1px solid #3a3a3a;border-radius:6px;padding:5px 8px;font-size:12px;background:#242424;line-height:1.2;cursor:pointer}.tools a:hover,.tools button:hover{background:#303030}details{margin-top:10px;color:#ddd;font-size:13px;line-height:1.4}summary{cursor:pointer;color:#cfcfcf}.caption{white-space:pre-wrap;margin-top:8px}\n")
	sb.WriteString("  </style>\n</head>\n<body>\n")
	sb.WriteString("  <header><h1>FragForge publish pack</h1><p class=\"summary\">")
	sb.WriteString(html.EscapeString(fmt.Sprintf("%d shorts ready for upload", len(manifest.Shorts))))
	sb.WriteString(" · preset <span>")
	sb.WriteString(html.EscapeString(manifest.Preset))
	sb.WriteString("</span>")
	if len(manifest.SegmentFilter) > 0 {
		sb.WriteString(" · filtered <span>")
		sb.WriteString(html.EscapeString(strings.Join(manifest.SegmentFilter, ", ")))
		sb.WriteString("</span>")
	}
	if manifest.VideoCRF > 0 || manifest.VideoPreset != "" {
		sb.WriteString(" · video <span>")
		sb.WriteString(html.EscapeString(fmt.Sprintf("crf %d / %s", manifest.VideoCRF, manifest.VideoPreset)))
		sb.WriteString("</span>")
	}
	if qualityFeatureEnabled(manifest) {
		sb.WriteString(" · hq <span>")
		sb.WriteString(html.EscapeString(featureSummary(manifest)))
		sb.WriteString("</span>")
	}
	sb.WriteString("</p><div class=\"filters\"><input id=\"search\" type=\"search\" placeholder=\"Search title, segment, weapon\"><select id=\"weapon\"><option value=\"\">All weapons</option>")
	for _, weapon := range weapons {
		sb.WriteString("<option value=\"")
		sb.WriteString(html.EscapeString(strings.ToLower(weapon)))
		sb.WriteString("\">")
		sb.WriteString(html.EscapeString(weapon))
		sb.WriteString("</option>")
	}
	sb.WriteString("</select><select id=\"kills\"><option value=\"0\">All kills</option>")
	for i := 1; i <= maxKills; i++ {
		sb.WriteString("<option value=\"")
		sb.WriteString(fmt.Sprintf("%d", i))
		sb.WriteString("\">")
		sb.WriteString(fmt.Sprintf("%dK+", i))
		sb.WriteString("</option>")
	}
	sb.WriteString("</select><button type=\"button\" id=\"pauseAll\">Pause all</button><span class=\"count\"><span id=\"visibleCount\">")
	sb.WriteString(fmt.Sprintf("%d", len(manifest.Shorts)))
	sb.WriteString("</span>/")
	sb.WriteString(fmt.Sprintf("%d", len(manifest.Shorts)))
	sb.WriteString(" visible</span></div></header>\n")
	sb.WriteString("  <main class=\"grid\">\n")
	for _, short := range manifest.Shorts {
		video := galleryHref(galleryDir, short.PublishPath)
		cover := filepath.Base(short.CoverPath)
		searchText := strings.Join(nonEmpty([]string{
			short.Title,
			short.SegmentID,
			short.PrimaryWeapon,
			short.PrimarySmoke,
			short.Player,
			short.Map,
		}), " ")
		sb.WriteString("    <article class=\"item\" data-search=\"")
		sb.WriteString(html.EscapeString(strings.ToLower(searchText)))
		sb.WriteString("\" data-weapon=\"")
		sb.WriteString(html.EscapeString(strings.ToLower(short.PrimaryWeapon)))
		sb.WriteString("\" data-kills=\"")
		sb.WriteString(fmt.Sprintf("%d", short.KillCount))
		sb.WriteString("\">\n")
		sb.WriteString("      <div class=\"media\"><video controls preload=\"metadata\"")
		if short.CoverPath != "" {
			sb.WriteString(" poster=\"")
			sb.WriteString(html.EscapeString(cover))
			sb.WriteString("\"")
		}
		sb.WriteString(" src=\"")
		sb.WriteString(html.EscapeString(video))
		sb.WriteString("\"></video></div>\n")
		sb.WriteString("      <div class=\"body\"><div class=\"title\">")
		sb.WriteString(html.EscapeString(short.Title))
		sb.WriteString("</div><div class=\"meta\">")
		sb.WriteString(html.EscapeString(short.SegmentID))
		if short.DurationSeconds > 0 {
			sb.WriteString(html.EscapeString(fmt.Sprintf(" · %.1fs", short.DurationSeconds)))
		}
		if short.PrimaryWeapon != "" {
			sb.WriteString(" · ")
			sb.WriteString(html.EscapeString(short.PrimaryWeapon))
		}
		if short.PrimarySmoke != "" {
			sb.WriteString(" · ")
			sb.WriteString(html.EscapeString(short.PrimarySmoke))
		}
		sb.WriteString("</div>")
		if source := artifactSummary(short.SourceArtifact); source != "" {
			sb.WriteString("<div class=\"diag\">source: ")
			sb.WriteString(html.EscapeString(source))
			sb.WriteString("</div>")
		}
		if output := artifactSummary(short.OutputArtifact); output != "" {
			sb.WriteString("<div class=\"diag\">output: ")
			sb.WriteString(html.EscapeString(output))
			sb.WriteString("</div>")
		}
		sb.WriteString("<div class=\"path\">")
		sb.WriteString(html.EscapeString(video))
		sb.WriteString("</div><div class=\"tools\"><a href=\"")
		sb.WriteString(html.EscapeString(video))
		sb.WriteString("\">MP4</a>")
		if short.CoverPath != "" {
			sb.WriteString("<a href=\"")
			sb.WriteString(html.EscapeString(galleryHref(galleryDir, short.CoverPath)))
			sb.WriteString("\">Cover</a>")
		}
		if short.CoverSheetPath != "" {
			sb.WriteString("<a href=\"")
			sb.WriteString(html.EscapeString(galleryHref(galleryDir, short.CoverSheetPath)))
			sb.WriteString("\">Sheet</a>")
		}
		if short.CaptionPath != "" {
			sb.WriteString("<a href=\"")
			sb.WriteString(html.EscapeString(galleryHref(galleryDir, short.CaptionPath)))
			sb.WriteString("\">Caption</a>")
		}
		if short.PromptPath != "" {
			sb.WriteString("<a href=\"")
			sb.WriteString(html.EscapeString(galleryHref(galleryDir, short.PromptPath)))
			sb.WriteString("\">Prompt</a>")
		}
		sb.WriteString("<button type=\"button\" data-copy-target=\".title\">Copy title</button><button type=\"button\" data-copy-target=\".caption\">Copy caption</button>")
		sb.WriteString("</div><details><summary>Caption</summary><div class=\"caption\">")
		sb.WriteString(html.EscapeString(short.Caption))
		sb.WriteString("</div></details></div>\n")
		sb.WriteString("    </article>\n")
	}
	sb.WriteString("  </main>\n<script>\n")
	sb.WriteString("const items=[...document.querySelectorAll('.item')];const search=document.getElementById('search');const weapon=document.getElementById('weapon');const kills=document.getElementById('kills');const visible=document.getElementById('visibleCount');function applyFilters(){const q=search.value.trim().toLowerCase();const w=weapon.value;const k=Number(kills.value||0);let n=0;for(const item of items){const okSearch=!q||item.dataset.search.includes(q);const okWeapon=!w||item.dataset.weapon===w;const okKills=Number(item.dataset.kills||0)>=k;const show=okSearch&&okWeapon&&okKills;item.hidden=!show;if(show)n++;}visible.textContent=String(n);}search.addEventListener('input',applyFilters);weapon.addEventListener('change',applyFilters);kills.addEventListener('change',applyFilters);document.getElementById('pauseAll').addEventListener('click',()=>document.querySelectorAll('video').forEach(v=>v.pause()));document.addEventListener('click',async event=>{const button=event.target.closest('button[data-copy-target]');if(!button)return;const item=button.closest('.item');const source=item&&item.querySelector(button.dataset.copyTarget);if(!source)return;const original=button.textContent;try{await navigator.clipboard.writeText(source.textContent.trim());button.textContent='Copied';setTimeout(()=>button.textContent=original,900);}catch{button.textContent='Copy failed';setTimeout(()=>button.textContent=original,1200);}});\n")
	sb.WriteString("</script>\n</body>\n</html>\n")
	return os.WriteFile(path, []byte(sb.String()), 0o600)
}

func WritePublishSummary(path string, manifest Manifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	var sb strings.Builder
	totalKills := 0
	totalSmokes := 0
	totalDuration := 0.0
	weaponCounts := map[string]int{}
	effectCounts := map[EffectType]int{}
	for _, short := range manifest.Shorts {
		totalKills += short.KillCount
		totalSmokes += short.SmokeCount
		totalDuration += short.DurationSeconds
		if short.PrimaryWeapon != "" {
			weaponCounts[short.PrimaryWeapon]++
		}
		for _, effect := range short.Effects {
			effectCounts[effect.Type]++
		}
	}
	sb.WriteString("# FragForge Publish Summary\n\n")
	sb.WriteString(fmt.Sprintf("- Shorts: %d\n", len(manifest.Shorts)))
	sb.WriteString(fmt.Sprintf("- Total kills: %d\n", totalKills))
	if totalSmokes > 0 {
		sb.WriteString(fmt.Sprintf("- Total smokes: %d\n", totalSmokes))
	}
	if totalDuration > 0 {
		sb.WriteString(fmt.Sprintf("- Total duration: %.1fs\n", totalDuration))
	}
	sb.WriteString(fmt.Sprintf("- Preset: %s\n", manifest.Preset))
	if manifest.VideoCRF > 0 || manifest.VideoPreset != "" {
		sb.WriteString(fmt.Sprintf("- Video encoding: x264 CRF %d, preset %s\n", manifest.VideoCRF, manifest.VideoPreset))
	}
	if manifest.OutputFPS > 0 {
		sb.WriteString(fmt.Sprintf("- Output FPS: %d\n", manifest.OutputFPS))
	}
	if manifest.CompileSegments {
		sb.WriteString("- Output shape: compiled segments\n")
	}
	if manifest.MusicPath != "" {
		sb.WriteString(fmt.Sprintf("- Music: %s\n", manifest.MusicPath))
	}
	if manifest.RhythmPath != "" {
		sb.WriteString(fmt.Sprintf("- Rhythm sync: %s\n", manifest.RhythmPath))
	}
	if qualityFeatureEnabled(manifest) {
		sb.WriteString(fmt.Sprintf("- HQ features: %s\n", featureSummary(manifest)))
	}
	if manifest.EffectsPreset != "" {
		sb.WriteString(fmt.Sprintf("- Effects preset: %s\n", manifest.EffectsPreset))
	}
	if manifest.EffectsPath != "" {
		sb.WriteString(fmt.Sprintf("- Effects script: %s\n", manifest.EffectsPath))
	}
	if len(manifest.SegmentFilter) > 0 {
		sb.WriteString(fmt.Sprintf("- Segment filter: %s\n", strings.Join(manifest.SegmentFilter, ", ")))
	}
	if len(weaponCounts) > 0 {
		sb.WriteString("- Weapons: ")
		parts := []string{}
		for _, weapon := range galleryWeapons(manifest.Shorts) {
			parts = append(parts, fmt.Sprintf("%s x%d", weapon, weaponCounts[weapon]))
		}
		sb.WriteString(strings.Join(parts, ", "))
		sb.WriteString("\n")
	}
	if len(effectCounts) > 0 {
		sb.WriteString("- Effects: ")
		sb.WriteString(strings.Join(effectCountParts(effectCounts), ", "))
		sb.WriteString("\n")
	}
	sb.WriteString("\n| # | Segment | Title | Effects | Video | Cover | Sheet | Caption | Prompt |\n")
	sb.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for _, short := range manifest.Shorts {
		sb.WriteString(fmt.Sprintf("| %02d | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			short.Index,
			markdownCell(short.SegmentID),
			markdownCell(short.Title),
			markdownCell(effectSummary(short.Effects)),
			markdownCell(filepath.Base(short.PublishPath)),
			markdownCell(filepath.Base(short.CoverPath)),
			markdownCell(filepath.Base(short.CoverSheetPath)),
			markdownCell(filepath.Base(short.CaptionPath)),
			markdownCell(filepath.Base(short.PromptPath)),
		))
	}
	return os.WriteFile(path, []byte(sb.String()), 0o600)
}

func featureSummary(manifest Manifest) string {
	parts := []string{}
	if manifest.HQFilters {
		parts = append(parts, "lanczos")
	}
	if manifest.AudioNormalize {
		parts = append(parts, "loudnorm")
	}
	if manifest.QualityChecks {
		parts = append(parts, "qc")
	}
	if manifest.CoverSheets {
		parts = append(parts, "sheets")
	}
	if manifest.TemporalSmoothing {
		parts = append(parts, "temporal smooth")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func qualityFeatureEnabled(manifest Manifest) bool {
	return manifest.HQFilters || manifest.AudioNormalize || manifest.QualityChecks || manifest.CoverSheets || manifest.TemporalSmoothing
}

func artifactSummary(artifact recording.RecordingArtifact) string {
	parts := []string{}
	if artifact.Codec != "" {
		parts = append(parts, artifact.Codec)
	}
	if artifact.Width > 0 && artifact.Height > 0 {
		parts = append(parts, fmt.Sprintf("%dx%d", artifact.Width, artifact.Height))
	}
	if artifact.FrameRate != "" {
		parts = append(parts, frameRateLabel(artifact.FrameRate))
	}
	if artifact.DurationSeconds > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs", artifact.DurationSeconds))
	}
	if artifact.SizeBytes > 0 {
		parts = append(parts, formatBytes(artifact.SizeBytes))
	}
	return strings.Join(parts, " | ")
}

func frameRateLabel(raw string) string {
	parts := strings.Split(raw, "/")
	if len(parts) == 2 {
		n, nerr := strconv.ParseFloat(parts[0], 64)
		d, derr := strconv.ParseFloat(parts[1], 64)
		if nerr == nil && derr == nil && d != 0 {
			return fmt.Sprintf("%.2gfps", n/d)
		}
	}
	return raw + "fps"
}

func formatBytes(size int64) string {
	const mb = 1024 * 1024
	if size >= mb {
		return fmt.Sprintf("%.1f MB", float64(size)/mb)
	}
	const kb = 1024
	if size >= kb {
		return fmt.Sprintf("%.1f KB", float64(size)/kb)
	}
	return fmt.Sprintf("%d B", size)
}

func effectCountParts(counts map[EffectType]int) []string {
	order := []EffectType{EffectZoom, EffectFlash, EffectText, EffectGrade, EffectImage, EffectKillfeed}
	parts := make([]string, 0, len(counts))
	for _, typ := range order {
		if counts[typ] > 0 {
			parts = append(parts, fmt.Sprintf("%s x%d", typ, counts[typ]))
		}
	}
	return parts
}

func effectSummary(effects []Effect) string {
	if len(effects) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(effects))
	for _, effect := range effects {
		switch effect.Type {
		case EffectZoom:
			parts = append(parts, fmt.Sprintf("zoom %.2fx %.2f-%.2fs", effect.Scale, effect.StartSeconds, effect.EndSeconds))
		case EffectFlash:
			parts = append(parts, fmt.Sprintf("flash %.0f%% %.2f-%.2fs", effect.Opacity*100, effect.StartSeconds, effect.EndSeconds))
		case EffectText:
			parts = append(parts, fmt.Sprintf("text %q %.2f-%.2fs", effect.Value, effect.StartSeconds, effect.EndSeconds))
		case EffectGrade:
			parts = append(parts, fmt.Sprintf("grade c%.2f s%.2f g%.2f", effect.Contrast, effect.Saturation, effect.Gamma))
		case EffectImage:
			parts = append(parts, fmt.Sprintf("image %s %.2f-%.2fs", filepath.Base(effect.Path), effect.StartSeconds, effect.EndSeconds))
		case EffectKillfeed:
			parts = append(parts, fmt.Sprintf("killfeed crop %dx%d %.2f-%.2fs", effect.CropWidth, effect.CropHeight, effect.StartSeconds, effect.EndSeconds))
		default:
			parts = append(parts, fmt.Sprintf("%s %.2f-%.2fs", effect.Type, effect.StartSeconds, effect.EndSeconds))
		}
	}
	return strings.Join(parts, "<br>")
}

func galleryHref(galleryDir, target string) string {
	if target == "" {
		return ""
	}
	rel, err := filepath.Rel(galleryDir, target)
	if err != nil {
		rel = filepath.Base(target)
	}
	return filepath.ToSlash(rel)
}

func galleryWeapons(shorts []ShortEdit) []string {
	seen := map[string]bool{}
	var weapons []string
	for _, short := range shorts {
		weapon := strings.TrimSpace(short.PrimaryWeapon)
		if weapon == "" || seen[weapon] {
			continue
		}
		seen[weapon] = true
		weapons = append(weapons, weapon)
	}
	return weapons
}

func maxKillCount(shorts []ShortEdit) int {
	maxKills := 0
	for _, short := range shorts {
		if short.KillCount > maxKills {
			maxKills = short.KillCount
		}
	}
	return maxKills
}

func markdownCell(raw string) string {
	raw = strings.ReplaceAll(raw, "\r", " ")
	raw = strings.ReplaceAll(raw, "\n", " ")
	raw = strings.ReplaceAll(raw, "|", "\\|")
	return raw
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
