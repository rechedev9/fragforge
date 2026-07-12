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

	titles := titleAlternatives(metadata)
	keywords := buildKeywords(metadata, cfg.MaxKeywords)
	tags := buildTags(metadata, cfg.MaxTags)
	candidates := make([]ContentCandidate, 0, cfg.CandidateCount)
	seen := make(map[string]struct{}, len(titles))
	for _, title := range titles {
		title = truncateRunes(title, MaxYouTubeTitleRunes)
		key := strings.ToLower(title)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		candidate := ContentCandidate{
			Title:       title,
			Description: buildDescription(title, metadata),
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
	score := 32.0
	if strings.Contains(title, strings.ToLower(metadata.Player)) {
		score += 10
	}
	if strings.Contains(title, strings.ToLower(metadata.Map)) {
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
		strings.Contains(description, strings.ToLower(metadata.Map)) {
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

func titleAlternatives(metadata VideoMetadata) []string {
	primary := fmt.Sprintf("%s: %d kills on %s | CS2", metadata.Player, metadata.KillCount, metadata.Map)
	hook := fmt.Sprintf("%d kills. One %s game. %s.", metadata.KillCount, metadata.Map, metadata.Player)
	if metadata.Hook != "" {
		hook = fmt.Sprintf("%s — %s on %s", metadata.Hook, metadata.Player, metadata.Map)
	}
	weapon := fmt.Sprintf("%s's %d-kill highlight on %s", metadata.Player, metadata.KillCount, metadata.Map)
	if len(metadata.Weapons) > 0 {
		weapon = fmt.Sprintf("%s highlights: %s's %d kills on %s", metadata.Weapons[0], metadata.Player, metadata.KillCount, metadata.Map)
	}
	moment := fmt.Sprintf("%s takes over %s with %d kills", metadata.Player, metadata.Map, metadata.KillCount)
	if metadata.Moment != "" {
		moment = fmt.Sprintf("%s | %s on %s (CS2)", metadata.Moment, metadata.Player, metadata.Map)
	}
	short := fmt.Sprintf("%d kills on %s — %s CS2 Short", metadata.KillCount, metadata.Map, metadata.Player)
	return []string{primary, hook, weapon, moment, short}
}

func buildDescription(title string, metadata VideoMetadata) string {
	weapons := ""
	if len(metadata.Weapons) > 0 {
		weapons = " with " + strings.Join(metadata.Weapons[:min(2, len(metadata.Weapons))], " and ")
	}
	context := fmt.Sprintf("%s lands %d kills on %s%s.", metadata.Player, metadata.KillCount, metadata.Map, weapons)
	if metadata.Moment != "" {
		context += " " + ensureSentence(metadata.Moment)
	}
	hashtags := []string{"#CS2", "#CounterStrike2", "#Shorts"}
	if mapHashtag := hashtag(metadata.Map); mapHashtag != "" {
		hashtags = append(hashtags, mapHashtag)
	}
	description := title + "\n\n" + context + "\n\n" + strings.Join(hashtags, " ")
	return truncateRunes(description, MaxYouTubeDescriptionRunes)
}

func buildKeywords(metadata VideoMetadata, maximum int) []string {
	keywords := []string{
		"CS2 Shorts",
		metadata.Map + " CS2",
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

func buildTags(metadata VideoMetadata, maximum int) []string {
	// Tags deliberately contain only identity/context and spelling variants.
	// SearchTerms are not copied wholesale because tags are not a ranking lever.
	tags := []string{"CS2", "Counter-Strike 2", metadata.Map, metadata.Player}
	tags = append(tags, metadata.Weapons[:min(2, len(metadata.Weapons))]...)
	tags = append(tags, metadata.Misspellings...)
	tags = takeUnique(tags, maximum)
	for utf8.RuneCountInString(strings.Join(tags, ",")) > MaxYouTubeTagRunes && len(tags) > 1 {
		tags = tags[:len(tags)-1]
	}
	return tags
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
