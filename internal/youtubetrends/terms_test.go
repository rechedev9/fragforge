package youtubetrends

import (
	"reflect"
	"strings"
	"testing"
)

func TestExtractTermsIsDeterministicRelevantAndDeduplicated(t *testing.T) {
	t.Parallel()

	results := []Result{
		{Title: "CS2 Mirage smoke lineups for A site"},
		{Title: "Best Mirage smoke lineup | Counter-Strike 2 Shorts"},
		{Title: "AWP ace clutch on Inferno"},
		{Title: "AWP aces and clutches: Inferno retake"},
		{Title: "Deagle headshots, utility tricks and eco round retake"},
		{Title: "Ancient molotov lineup and flash assist guide"},
	}
	got := ExtractTerms(results)
	wantAgain := ExtractTerms(results)
	if !reflect.DeepEqual(got, wantAgain) {
		t.Fatalf("terms are nondeterministic: %v then %v", got, wantAgain)
	}
	if len(got) < 5 || len(got) > 10 {
		t.Fatalf("term count = %d, want 5..10: %v", len(got), got)
	}
	seen := make(map[string]struct{}, len(got))
	for _, term := range got {
		if term != strings.ToLower(strings.TrimSpace(term)) {
			t.Errorf("term is not normalized: %q", term)
		}
		if _, duplicate := seen[term]; duplicate {
			t.Errorf("duplicate term: %q", term)
		}
		seen[term] = struct{}{}
		if _, stopword := termStopwords[term]; stopword {
			t.Errorf("generic stopword was included: %q", term)
		}
	}
	for _, generic := range []string{"cs2", "counter", "strike", "shorts", "youtube", "and", "for"} {
		if _, exists := seen[generic]; exists {
			t.Errorf("generic term %q was included: %v", generic, got)
		}
	}
	if !containsTermOrPhrase(got, "mirage") {
		t.Errorf("terms do not include Mirage context: %v", got)
	}
	if !containsTermOrPhrase(got, "awp") {
		t.Errorf("terms do not include AWP context: %v", got)
	}
}

func TestExtractTermsReturnsOnlyAvailableMeaningfulTerms(t *testing.T) {
	t.Parallel()

	got := ExtractTerms([]Result{{Title: "The best CS2 Shorts on YouTube: AWP ace"}})
	if want := []string{"awp", "ace"}; !reflect.DeepEqual(got, want) {
		t.Errorf("terms = %v, want %v", got, want)
	}
}

func containsTermOrPhrase(terms []string, word string) bool {
	for _, term := range terms {
		for _, candidate := range strings.Fields(term) {
			if candidate == word {
				return true
			}
		}
	}
	return false
}
