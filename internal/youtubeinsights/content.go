package youtubeinsights

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxYouTubeTitleRunes       = 100
	MaxYouTubeDescriptionRunes = 5000
	MaxYouTubeTagRunes         = 500
	MaxYouTubeHashtags         = 15
)

// VideoMetadata is the structured, factual input used to generate copy. Hook,
// Moment, Weapons, SearchTerms, and Misspellings are optional; Player, Map, and
// KillCount are required so generated titles do not invent context.
type VideoMetadata struct {
	Player       string
	Map          string
	KillCount    int
	Weapons      []string
	Moment       string
	Hook         string
	SearchTerms  []string
	Misspellings []string
}

// ContentConfig bounds the number of alternatives and supporting metadata.
type ContentConfig struct {
	CandidateCount int
	MaxKeywords    int
	MaxTags        int
}

// DefaultContentConfig returns a small set of alternatives without keyword or
// tag stuffing.
func DefaultContentConfig() ContentConfig {
	return ContentConfig{
		CandidateCount: 5,
		MaxKeywords:    8,
		MaxTags:        8,
	}
}

// ContentCandidate is upload-ready metadata plus an explainable quality score.
// Keywords are planning/search phrases; Tags are the deliberately small list
// intended for YouTube's tags field.
type ContentCandidate struct {
	Title       string
	Description string
	Keywords    []string
	Tags        []string
	Score       float64
	Rationale   string
}

// GenerateContentCandidates returns three to five deterministic alternatives,
// ordered by score. It never performs trend lookup or adds facts absent from the
// supplied metadata.
func GenerateContentCandidates(metadata VideoMetadata, cfg ContentConfig) ([]ContentCandidate, error) {
	metadata = normalizeMetadata(metadata)
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	if err := validateContentConfig(cfg); err != nil {
		return nil, err
	}

	mapName := prettyMapName(metadata.Map)
	titles := titleAlternatives(metadata, mapName)
	keywords := buildKeywords(metadata, mapName, cfg.MaxKeywords)
	tags := buildTags(metadata, mapName, cfg.MaxTags)
	candidates := make([]ContentCandidate, 0, cfg.CandidateCount)
	seen := make(map[string]struct{}, len(titles))
	for _, template := range titles {
		title := truncateRunes(template.text, MaxYouTubeTitleRunes)
		key := strings.ToLower(title)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		candidate := ContentCandidate{
			Title:       title,
			Description: buildDescription(title, metadata, mapName, template.spanish),
			Keywords:    append([]string(nil), keywords...),
			Tags:        append([]string(nil), tags...),
		}
		score, err := ScoreContent(candidate, metadata)
		if err != nil {
			return nil, err
		}
		candidate.Score = score
		candidate.Rationale = contentRationale(candidate, metadata)
		candidates = append(candidates, candidate)
	}
	if len(candidates) < cfg.CandidateCount {
		return nil, errors.New("metadata did not produce enough distinct title candidates")
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	return candidates[:cfg.CandidateCount], nil
}

// ScoreContent validates and scores user-edited or generated metadata from 0 to
// 100. It rewards factual context, a clear hook, and readable title length.
func ScoreContent(candidate ContentCandidate, metadata VideoMetadata) (float64, error) {
	metadata = normalizeMetadata(metadata)
	if err := validateMetadata(metadata); err != nil {
		return 0, err
	}
	if err := ValidateContentCandidate(candidate); err != nil {
		return 0, err
	}

	title := strings.ToLower(candidate.Title)
	description := strings.ToLower(candidate.Description)
	mapMentions := mapMentions(metadata)
	score := 32.0
	if strings.Contains(title, strings.ToLower(metadata.Player)) {
		score += 10
	}
	if containsAny(title, mapMentions) {
		score += 10
	}
	if strings.Contains(title, strconv.Itoa(metadata.KillCount)) {
		score += 8
	}
	if metadata.Hook != "" && strings.Contains(title, strings.ToLower(metadata.Hook)) {
		score += 12
	}
	if metadata.Moment != "" && strings.Contains(title, strings.ToLower(metadata.Moment)) {
		score += 7
	}
	if containsAnyFold(title, metadata.Weapons) {
		score += 5
	}
	titleLength := utf8.RuneCountInString(candidate.Title)
	if titleLength >= 30 && titleLength <= 75 {
		score += 8
	} else if titleLength > 90 {
		score -= 8
	}
	if strings.Contains(description, strings.ToLower(metadata.Player)) &&
		containsAny(description, mapMentions) {
		score += 5
	}
	if strings.Contains(description, "#shorts") && strings.Contains(description, "#cs2") {
		score += 3
	}
	if len(metadata.SearchTerms) > 0 && containsExactFold(candidate.Keywords, metadata.SearchTerms[0]) {
		score += 3
	}
	if len(candidate.Keywords) >= 3 && len(candidate.Keywords) <= 8 {
		score += 3
	}
	if len(candidate.Tags) <= 8 {
		score += 2
	}
	return mathRoundOneDecimal(clamp(score, 0, 100)), nil
}

// ValidateContentCandidate enforces YouTube's relevant text limits and the
// package's anti-stuffing bounds.
func ValidateContentCandidate(candidate ContentCandidate) error {
	if strings.TrimSpace(candidate.Title) == "" {
		return errors.New("title is required")
	}
	if strings.ContainsAny(candidate.Title, "\r\n") {
		return errors.New("title cannot contain line breaks")
	}
	if utf8.RuneCountInString(candidate.Title) > MaxYouTubeTitleRunes {
		return fmt.Errorf("title exceeds %d characters", MaxYouTubeTitleRunes)
	}
	if strings.TrimSpace(candidate.Description) == "" {
		return errors.New("description is required")
	}
	if utf8.RuneCountInString(candidate.Description) > MaxYouTubeDescriptionRunes {
		return fmt.Errorf("description exceeds %d characters", MaxYouTubeDescriptionRunes)
	}
	if len(candidate.Keywords) == 0 || len(candidate.Keywords) > 12 {
		return errors.New("keywords must contain between 1 and 12 phrases")
	}
	if err := validateUniqueText(candidate.Keywords, "keyword"); err != nil {
		return err
	}
	if len(candidate.Tags) == 0 || len(candidate.Tags) > 15 {
		return errors.New("tags must contain between 1 and 15 entries")
	}
	if err := validateUniqueText(candidate.Tags, "tag"); err != nil {
		return err
	}
	if utf8.RuneCountInString(strings.Join(candidate.Tags, ",")) > MaxYouTubeTagRunes {
		return fmt.Errorf("tags exceed %d characters", MaxYouTubeTagRunes)
	}
	if hashtagCount(candidate.Description) > MaxYouTubeHashtags {
		return fmt.Errorf("description exceeds %d hashtags", MaxYouTubeHashtags)
	}
	return nil
}

func validateMetadata(metadata VideoMetadata) error {
	switch {
	case metadata.Player == "":
		return errors.New("player is required")
	case metadata.Map == "":
		return errors.New("map is required")
	case metadata.KillCount <= 0:
		return errors.New("kill count must be positive")
	case utf8.RuneCountInString(metadata.Player) > 80:
		return errors.New("player exceeds 80 characters")
	case utf8.RuneCountInString(metadata.Map) > 80:
		return errors.New("map exceeds 80 characters")
	case utf8.RuneCountInString(metadata.Hook) > 160:
		return errors.New("hook exceeds 160 characters")
	case utf8.RuneCountInString(metadata.Moment) > 160:
		return errors.New("moment exceeds 160 characters")
	}
	lists := []struct {
		name   string
		plural string
		values []string
	}{
		{name: "weapon", plural: "weapons", values: metadata.Weapons},
		{name: "search term", plural: "search terms", values: metadata.SearchTerms},
		{name: "misspelling", plural: "misspellings", values: metadata.Misspellings},
	}
	for _, list := range lists {
		if len(list.values) > 20 {
			return fmt.Errorf("%s exceed 20 entries", list.plural)
		}
		for index, value := range list.values {
			if utf8.RuneCountInString(value) > 100 {
				return fmt.Errorf("%s %d exceeds 100 characters", list.name, index)
			}
		}
	}
	return nil
}

func validateContentConfig(cfg ContentConfig) error {
	switch {
	case cfg.CandidateCount < 3 || cfg.CandidateCount > 5:
		return errors.New("candidate count must be between 3 and 5")
	case cfg.MaxKeywords < 3 || cfg.MaxKeywords > 12:
		return errors.New("maximum keywords must be between 3 and 12")
	case cfg.MaxTags < 3 || cfg.MaxTags > 15:
		return errors.New("maximum tags must be between 3 and 15")
	default:
		return nil
	}
}

func normalizeMetadata(metadata VideoMetadata) VideoMetadata {
	metadata.Player = normalizeText(metadata.Player)
	metadata.Map = normalizeText(metadata.Map)
	metadata.Moment = normalizeText(metadata.Moment)
	metadata.Hook = normalizeText(metadata.Hook)
	metadata.Weapons = normalizeTextList(metadata.Weapons)
	metadata.SearchTerms = normalizeTextList(metadata.SearchTerms)
	metadata.Misspellings = normalizeTextList(metadata.Misspellings)
	metadata.SearchTerms = factualSearchTerms(metadata)
	return metadata
}

// FilterFactualSearchTerms returns only search phrases whose meaningful tokens
// are all present in the reel's player, map, weapon, hook, or exact kill count.
// Generic CS2/Shorts words do not make an otherwise unrelated phrase factual.
func FilterFactualSearchTerms(metadata VideoMetadata, terms []string) []string {
	metadata.Player = normalizeText(metadata.Player)
	metadata.Map = normalizeText(metadata.Map)
	metadata.Hook = normalizeText(metadata.Hook)
	metadata.Weapons = normalizeTextList(metadata.Weapons)
	metadata.SearchTerms = normalizeTextList(terms)
	return factualSearchTerms(metadata)
}

func factualSearchTerms(metadata VideoMetadata) []string {
	facts := strings.Join(append([]string{
		metadata.Player,
		metadata.Map,
		metadata.Hook,
		strconv.Itoa(metadata.KillCount),
	}, metadata.Weapons...), " ")
	factTokens := meaningfulTokens(facts)
	result := make([]string, 0, len(metadata.SearchTerms))
	for _, term := range metadata.SearchTerms {
		termTokens := meaningfulTokens(term)
		if len(termTokens) == 0 {
			continue
		}
		factual := true
		for token := range termTokens {
			if _, ok := factTokens[token]; !ok {
				factual = false
				break
			}
		}
		if factual {
			result = append(result, term)
		}
	}
	return result
}

func meaningfulTokens(value string) map[string]struct{} {
	ignored := map[string]struct{}{
		"cs2": {}, "counter": {}, "strike": {}, "short": {}, "shorts": {},
		"highlight": {}, "highlights": {}, "game": {}, "gameplay": {},
		"kill": {}, "kills": {}, "baja": {}, "bajas": {},
	}
	tokens := make(map[string]struct{})
	for _, field := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if utf8.RuneCountInString(field) < 2 && !allDigits(field) {
			continue
		}
		if _, skip := ignored[field]; !skip {
			tokens[field] = struct{}{}
		}
	}
	return tokens
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if !unicode.IsDigit(character) {
			return false
		}
	}
	return true
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func normalizeTextList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = normalizeText(value)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

// mapDisplayNames maps CS2 engine map identifiers to the human display names
// creators actually put in titles. Unlisted maps fall back to prettyMapName's
// prefix/underscore normalization.
var mapDisplayNames = map[string]string{
	"de_ancient":  "Ancient",
	"de_dust2":    "Dust 2",
	"de_mirage":   "Mirage",
	"de_inferno":  "Inferno",
	"de_nuke":     "Nuke",
	"de_overpass": "Overpass",
	"de_vertigo":  "Vertigo",
	"de_anubis":   "Anubis",
	"de_train":    "Train",
	"cs_office":   "Office",
	"cs_italy":    "Italy",
}

// prettyMapName converts an engine map name (de_ancient) into a display name
// (Ancient). Names that are already display names, such as "Mirage" or
// "Dust II", are returned unchanged.
func prettyMapName(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return trimmed
	}
	if display, ok := mapDisplayNames[strings.ToLower(trimmed)]; ok {
		return display
	}
	name := trimmed
	lower := strings.ToLower(name)
	for _, prefix := range []string{"de_", "cs_"} {
		if strings.HasPrefix(lower, prefix) {
			name = name[len(prefix):]
			break
		}
	}
	fields := strings.Fields(strings.ReplaceAll(name, "_", " "))
	for index, field := range fields {
		// Only capitalize all-lowercase tokens so "II" or "Dust" survive intact.
		if field == strings.ToLower(field) {
			fields[index] = capitalizeFirst(field)
		}
	}
	return strings.Join(fields, " ")
}

func capitalizeFirst(value string) string {
	if value == "" {
		return value
	}
	runes := []rune(value)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// titleTemplate is a candidate title plus the language of its supporting
// description, so descriptions match the title instead of always being English.
type titleTemplate struct {
	text    string
	spanish bool
}

// titleAlternatives returns deterministic viral-realistic title candidates that
// mix Spanish and English. Every fact comes from metadata; mapName is the
// display map name. With only player, map, and kill count it still yields five
// distinct titles.
func titleAlternatives(metadata VideoMetadata, mapName string) []titleTemplate {
	player := metadata.Player
	kills := metadata.KillCount
	templates := make([]titleTemplate, 0, 8)

	// Hook first, then only the facts the hook does not already state.
	if metadata.Hook != "" {
		templates = append(templates, titleTemplate{
			text:    hookLedTitle(metadata, mapName),
			spanish: true,
		})
	}
	// Weapon-led Spanish punch line.
	if len(metadata.Weapons) > 0 {
		templates = append(templates, titleTemplate{
			text:    fmt.Sprintf("¡%d KILLS con la %s en %s! — %s", kills, metadata.Weapons[0], mapName, player),
			spanish: true,
		})
	}
	// Five language-mixed base templates that never depend on optional fields.
	templates = append(templates,
		titleTemplate{text: fmt.Sprintf("%s DESTROZA %s: %d kills en CS2", player, mapName, kills), spanish: true},
		titleTemplate{text: fmt.Sprintf("%s drops %d on %s 🤯 CS2 highlights", player, kills, mapName), spanish: false},
		titleTemplate{text: fmt.Sprintf("%d kills on %s — %s | CS2 Shorts", kills, mapName, player), spanish: false},
		titleTemplate{text: fmt.Sprintf("%d BAJAS en %s con %s | Counter-Strike 2", kills, mapName, player), spanish: true},
		titleTemplate{text: fmt.Sprintf("%s: %d kills en %s | CS2 highlights", player, kills, mapName), spanish: true},
	)
	// Weapon-led English variant, offered alongside the Spanish weapon line.
	if len(metadata.Weapons) > 0 {
		templates = append(templates, titleTemplate{
			text:    fmt.Sprintf("%s's %d kills with the %s on %s | CS2", player, kills, metadata.Weapons[0], mapName),
			spanish: false,
		})
	}
	return templates
}

// hookLedTitle leads with the supplied hook and appends only the facts the hook
// does not already state, so a rich hook like "12K con AK-47 en Ancient" is not
// echoed by a redundant "12 kills en Ancient" tail. "(CS2)" always survives so
// the game context is present even when the hook already carries every fact.
func hookLedTitle(metadata VideoMetadata, mapName string) string {
	hookLower := strings.ToLower(metadata.Hook)
	var facts []string
	if !strings.Contains(hookLower, strings.ToLower(metadata.Player)) {
		facts = append(facts, metadata.Player)
	}
	hasKills := strings.Contains(hookLower, strconv.Itoa(metadata.KillCount))
	hasMap := mapName != "" && strings.Contains(hookLower, strings.ToLower(mapName))
	switch {
	case !hasKills && !hasMap:
		facts = append(facts, fmt.Sprintf("%d kills en %s", metadata.KillCount, mapName))
	case !hasKills && hasMap:
		facts = append(facts, fmt.Sprintf("%d kills", metadata.KillCount))
	case hasKills && !hasMap:
		facts = append(facts, mapName)
	}
	if len(facts) == 0 {
		return metadata.Hook + " | CS2"
	}
	return fmt.Sprintf("%s — %s (CS2)", metadata.Hook, strings.Join(facts, ", "))
}

func buildDescription(title string, metadata VideoMetadata, mapName string, spanish bool) string {
	context := descriptionContext(metadata, mapName, spanish)
	hashtags := []string{"#CS2", "#CounterStrike2", "#Shorts"}
	if mapHashtag := hashtag(mapName); mapHashtag != "" {
		hashtags = append(hashtags, mapHashtag)
	}
	description := title + "\n\n" + context + "\n\n" + strings.Join(hashtags, " ")
	return truncateRunes(description, MaxYouTubeDescriptionRunes)
}

func descriptionContext(metadata VideoMetadata, mapName string, spanish bool) string {
	verb, mapPreposition, weaponJoiner := "lands", "on", " and "
	if spanish {
		verb, mapPreposition, weaponJoiner = "consigue", "en", " y "
	}
	weapons := ""
	if len(metadata.Weapons) > 0 {
		lead := " with "
		if spanish {
			lead = " con "
		}
		weapons = lead + strings.Join(metadata.Weapons[:min(2, len(metadata.Weapons))], weaponJoiner)
	}
	context := fmt.Sprintf("%s %s %d kills %s %s%s.", metadata.Player, verb, metadata.KillCount, mapPreposition, mapName, weapons)
	if metadata.Moment != "" {
		context += " " + ensureSentence(metadata.Moment)
	}
	return context
}

func buildKeywords(metadata VideoMetadata, mapName string, maximum int) []string {
	keywords := []string{
		"CS2 Shorts",
		mapName + " CS2",
		metadata.Player + " highlights",
	}
	if len(metadata.Weapons) > 0 {
		keywords = append(keywords, metadata.Weapons[0]+" CS2")
	}
	if metadata.Moment != "" {
		keywords = append(keywords, metadata.Moment+" CS2")
	}
	keywords = append(keywords, metadata.SearchTerms...)
	return takeUnique(keywords, maximum)
}

func buildTags(metadata VideoMetadata, mapName string, maximum int) []string {
	// Tags deliberately contain only identity/context and spelling variants.
	// SearchTerms are not copied wholesale because tags are not a ranking lever.
	tags := []string{"CS2", "Counter-Strike 2", mapName, metadata.Player}
	tags = append(tags, metadata.Weapons[:min(2, len(metadata.Weapons))]...)
	tags = append(tags, metadata.Misspellings...)
	tags = takeUnique(tags, maximum)
	for utf8.RuneCountInString(strings.Join(tags, ",")) > MaxYouTubeTagRunes && len(tags) > 1 {
		tags = tags[:len(tags)-1]
	}
	return tags
}

// mapMentions returns the lowercased raw engine name and display name, so
// scoring credits a map reference whether the title uses "de_ancient" or
// "Ancient".
func mapMentions(metadata VideoMetadata) []string {
	raw := strings.ToLower(strings.TrimSpace(metadata.Map))
	display := strings.ToLower(prettyMapName(metadata.Map))
	if display == raw || display == "" {
		return []string{raw}
	}
	return []string{raw, display}
}

func containsAny(haystack string, needles []string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

func takeUnique(values []string, maximum int) []string {
	values = normalizeTextList(values)
	result := make([]string, 0, min(len(values), maximum))
	for _, value := range values {
		if utf8.RuneCountInString(value) > 100 {
			value = truncateRunes(value, 100)
		}
		result = append(result, value)
		if len(result) == maximum {
			break
		}
	}
	return result
}

func validateUniqueText(values []string, label string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("%s cannot be empty", label)
		}
		if utf8.RuneCountInString(value) > 100 {
			return fmt.Errorf("%s exceeds 100 characters", label)
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate %s %q", label, value)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func contentRationale(candidate ContentCandidate, metadata VideoMetadata) string {
	parts := []string{"uses explicit player, map, and kill context"}
	if metadata.Hook != "" && strings.Contains(strings.ToLower(candidate.Title), strings.ToLower(metadata.Hook)) {
		parts = append(parts, "leads with the supplied hook")
	}
	if metadata.Moment != "" && strings.Contains(strings.ToLower(candidate.Title), strings.ToLower(metadata.Moment)) {
		parts = append(parts, "names the supplied moment")
	}
	parts = append(parts, "keeps tags limited to context and spelling support")
	return strings.Join(parts, "; ") + "."
}

func containsAnyFold(haystack string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(haystack, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}

func containsExactFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func ensureSentence(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, ".") || strings.HasSuffix(value, "!") || strings.HasSuffix(value, "?") {
		return value
	}
	return value + "."
}

func hashtag(value string) string {
	var builder strings.Builder
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
		}
	}
	if builder.Len() == 0 {
		return ""
	}
	return "#" + builder.String()
}

func hashtagCount(description string) int {
	count := 0
	for _, field := range strings.Fields(description) {
		if strings.HasPrefix(field, "#") && len(field) > 1 {
			count++
		}
	}
	return count
}

func truncateRunes(value string, maximum int) string {
	if utf8.RuneCountInString(value) <= maximum {
		return value
	}
	runes := []rune(value)
	value = strings.TrimSpace(string(runes[:maximum]))
	return strings.TrimRight(value, "-—|:,. ")
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func mathRoundOneDecimal(value float64) float64 {
	return float64(int(value*10+0.5)) / 10
}
