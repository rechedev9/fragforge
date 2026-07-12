package youtubetrends

import (
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxExtractedTerms = 10

var termStopwords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "are": {}, "at": {}, "be": {}, "best": {}, "by": {},
	"como": {}, "con": {}, "counter": {}, "counter-strike": {}, "counterstrike": {}, "cs": {}, "cs2": {},
	"de": {}, "del": {}, "el": {}, "en": {}, "for": {}, "from": {}, "gameplay": {}, "gaming": {},
	"highlight": {}, "highlights": {}, "how": {}, "in": {}, "insane": {}, "is": {}, "it": {},
	"la": {}, "las": {}, "los": {}, "mejor": {}, "mejores": {}, "new": {}, "nuevo": {}, "nueva": {},
	"of": {}, "on": {}, "or": {}, "para": {}, "por": {}, "que": {}, "short": {}, "shorts": {},
	"sin": {}, "strike": {}, "the": {}, "this": {}, "to": {}, "tu": {}, "tus": {}, "un": {}, "una": {},
	"video": {}, "videos": {}, "viral": {}, "vs": {}, "with": {}, "y": {}, "you": {}, "your": {}, "youtube": {},
}

var termAliases = map[string]string{
	"aces":      "ace",
	"clutches":  "clutch",
	"headshots": "headshot",
	"kills":     "kill",
	"lineups":   "lineup",
	"smokes":    "smoke",
	"tips":      "tip",
	"tricks":    "trick",
}

type termStat struct {
	value       string
	documents   int
	occurrences int
	wordCount   int
	firstSeen   int
}

// ExtractTerms deterministically derives at most ten distinct, normalized
// terms from result titles. Repeated two-word phrases are preferred over their
// component words, while English/Spanish glue words and generic platform/query
// terms are removed. It returns fewer than five terms only when the supplied
// titles do not contain five meaningful candidates.
func ExtractTerms(results []Result) []string {
	stats := make(map[string]*termStat)
	firstSeen := 0
	for _, result := range results {
		tokens := titleTokens(result.Title)
		seenDocument := make(map[string]struct{})
		for _, token := range tokens {
			firstSeen = countTerm(stats, token, 1, firstSeen, seenDocument)
		}
		for index := 0; index+1 < len(tokens); index++ {
			phrase := tokens[index] + " " + tokens[index+1]
			firstSeen = countTerm(stats, phrase, 2, firstSeen, seenDocument)
		}
	}

	candidates := make([]termStat, 0, len(stats))
	for _, stat := range stats {
		if stat.wordCount == 2 && stat.documents < 2 {
			continue
		}
		candidates = append(candidates, *stat)
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		leftScore := left.documents*4 + left.occurrences
		rightScore := right.documents*4 + right.occurrences
		if left.wordCount == 2 {
			leftScore += left.documents * 2
		}
		if right.wordCount == 2 {
			rightScore += right.documents * 2
		}
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		if left.documents != right.documents {
			return left.documents > right.documents
		}
		if left.wordCount != right.wordCount {
			return left.wordCount > right.wordCount
		}
		if left.firstSeen != right.firstSeen {
			return left.firstSeen < right.firstSeen
		}
		return left.value < right.value
	})

	terms := make([]string, 0, min(maxExtractedTerms, len(candidates)))
	coveredWords := make(map[string]struct{})
	for _, candidate := range candidates {
		words := strings.Fields(candidate.value)
		if candidate.wordCount == 1 {
			if _, covered := coveredWords[candidate.value]; covered {
				continue
			}
		} else if slices.ContainsFunc(words, func(word string) bool {
			_, covered := coveredWords[word]
			return covered
		}) {
			continue
		}
		terms = append(terms, candidate.value)
		for _, word := range words {
			coveredWords[word] = struct{}{}
		}
		if len(terms) == maxExtractedTerms {
			break
		}
	}
	return terms
}

func countTerm(stats map[string]*termStat, value string, wordCount, firstSeen int, seenDocument map[string]struct{}) int {
	stat, exists := stats[value]
	if !exists {
		stat = &termStat{value: value, wordCount: wordCount, firstSeen: firstSeen}
		stats[value] = stat
		firstSeen++
	}
	stat.occurrences++
	if _, seen := seenDocument[value]; !seen {
		stat.documents++
		seenDocument[value] = struct{}{}
	}
	return firstSeen
}

func titleTokens(title string) []string {
	raw := splitTermWords(strings.ToLower(title))
	tokens := make([]string, 0, len(raw))
	for _, token := range raw {
		if alias, ok := termAliases[token]; ok {
			token = alias
		}
		if _, stop := termStopwords[token]; stop || !meaningfulTerm(token) {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func splitTermWords(value string) []string {
	words := make([]string, 0, 12)
	var word strings.Builder
	flush := func() {
		if word.Len() == 0 {
			return
		}
		value := strings.Trim(word.String(), "-_")
		word.Reset()
		if value != "" {
			words = append(words, strings.ReplaceAll(value, "_", "-"))
		}
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || ((r == '-' || r == '_') && word.Len() > 0) {
			word.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return words
}

func meaningfulTerm(value string) bool {
	length := utf8.RuneCountInString(value)
	if length < 2 || length > 32 {
		return false
	}
	hasLetter := false
	for _, r := range value {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}
	return hasLetter
}
