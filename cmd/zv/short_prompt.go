package main

import (
	"regexp"
	"strings"

	"github.com/rechedev9/fragforge/internal/editor"
)

// shortIntent is the deterministic interpretation of a `zv short --prompt`
// instruction. Interpretation is pure keyword/regex matching over Spanish and
// English prompts; there are no model calls.
type shortIntent struct {
	// TargetName is the player name mentioned in the prompt, if any.
	TargetName string
	// TargetSteamID is a SteamID64 found in the prompt, if any.
	TargetSteamID string
	// BestMoments selects a best-moments compilation instead of every kill.
	BestMoments bool
	// BeatSync requests music-synced editing and routes to viral-beatsync.
	BeatSync bool
	// Preset is a render preset named explicitly in the prompt, if any.
	Preset string
}

var (
	shortSteamIDPattern = regexp.MustCompile(`\b\d{17}\b`)
	// "todas las kills de martinez", "best moments of s1mple", ...
	shortTargetAfterSubjectPattern = regexp.MustCompile(`(?:kills?|muertes|frags?|moments?|momentos|highlights?|clips?|jugadas)\s+(?:de|del|of|by|from)\s+([a-z0-9_.\-]+)`)
	// "player donk", "jugador martinez"
	shortTargetAfterPlayerPattern = regexp.MustCompile(`(?:player|jugador)\s+([a-z0-9_.\-]+)`)
)

// shortTargetStopwords are words that follow "de"/"of"/"by" without naming a
// player.
var shortTargetStopwords = map[string]struct{}{
	"la": {}, "las": {}, "el": {}, "los": {}, "un": {}, "una": {},
	"the": {}, "a": {}, "an": {}, "this": {}, "that": {}, "my": {}, "mi": {},
	"music": {}, "musica": {}, "ritmo": {}, "beat": {}, "beats": {},
	"partida": {}, "match": {}, "demo": {}, "game": {}, "ronda": {}, "round": {},
}

var shortBeatSyncKeywords = []string{
	"beat", "ritmo", "music", "musica", "música", "song", "cancion", "canción", "sync",
}

var shortBestMomentsKeywords = []string{
	"best", "mejores", "highlight", "destacad", "top ",
}

// interpretShortPrompt maps a free-form prompt to a deterministic intent.
func interpretShortPrompt(prompt string) shortIntent {
	lowered := strings.ToLower(prompt)
	intent := shortIntent{
		TargetSteamID: shortSteamIDPattern.FindString(lowered),
		Preset:        promptPresetName(lowered),
		BeatSync:      containsAnyKeyword(lowered, shortBeatSyncKeywords),
		BestMoments:   containsAnyKeyword(lowered, shortBestMomentsKeywords),
	}
	if intent.Preset == editor.PresetViralBeatsync {
		intent.BeatSync = true
	}
	intent.TargetName = promptTargetName(lowered)
	return intent
}

func promptTargetName(lowered string) string {
	for _, pattern := range []*regexp.Regexp{shortTargetAfterSubjectPattern, shortTargetAfterPlayerPattern} {
		match := pattern.FindStringSubmatch(lowered)
		if match == nil {
			continue
		}
		name := match[1]
		if _, stop := shortTargetStopwords[name]; stop {
			continue
		}
		if shortSteamIDPattern.MatchString(name) {
			continue
		}
		return name
	}
	return ""
}

// promptPresetName returns the longest registered preset name mentioned in the
// prompt, so "natural-hq2-full" wins over its "natural-hq2" prefix.
func promptPresetName(lowered string) string {
	var best string
	for _, name := range editor.PresetNames() {
		if len(name) > len(best) && strings.Contains(lowered, name) {
			best = name
		}
	}
	return best
}

func containsAnyKeyword(lowered string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(lowered, keyword) {
			return true
		}
	}
	return false
}
