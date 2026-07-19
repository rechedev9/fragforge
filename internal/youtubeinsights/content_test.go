package youtubeinsights

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestGenerateContentCandidates(t *testing.T) {
	metadata := VideoMetadata{
		Player:       "Zack",
		Map:          "Mirage",
		KillCount:    5,
		Weapons:      []string{"AK-47", "AWP"},
		Moment:       "ace clutch",
		Hook:         "They thought the round was over",
		SearchTerms:  []string{"cs2 ace", "mirage highlights", "best cs2 settings", "zack 5 kills"},
		Misspellings: []string{"counter strike two"},
	}

	got, err := GenerateContentCandidates(metadata, DefaultContentConfig())
	if err != nil {
		t.Fatalf("GenerateContentCandidates() error = %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("candidate count = %d, want 5", len(got))
	}
	for index, candidate := range got {
		if err := ValidateContentCandidate(candidate); err != nil {
			t.Errorf("candidate %d validation error = %v", index, err)
		}
		if utf8.RuneCountInString(candidate.Title) > MaxYouTubeTitleRunes {
			t.Errorf("candidate %d title exceeds limit", index)
		}
		if utf8.RuneCountInString(candidate.Description) > MaxYouTubeDescriptionRunes {
			t.Errorf("candidate %d description exceeds limit", index)
		}
		if len(candidate.Keywords) > 8 {
			t.Errorf("candidate %d keyword count = %d, want at most 8", index, len(candidate.Keywords))
		}
		if len(candidate.Tags) > 8 {
			t.Errorf("candidate %d tag count = %d, want at most 8", index, len(candidate.Tags))
		}
		if containsFold(candidate.Tags, "best cs2 settings") {
			t.Errorf("candidate %d copied search terms wholesale into tags: %v", index, candidate.Tags)
		}
		if !containsFold(candidate.Keywords, "mirage highlights") || !containsFold(candidate.Keywords, "zack 5 kills") {
			t.Errorf("candidate %d omitted a factually relevant search phrase: %v", index, candidate.Keywords)
		}
		if containsFold(candidate.Keywords, "cs2 ace") || containsFold(candidate.Keywords, "best cs2 settings") || strings.Contains(strings.ToLower(candidate.Description), "best cs2 settings") {
			t.Errorf("candidate %d used an unrelated search phrase", index)
		}
		if !containsFold(candidate.Tags, "counter strike two") {
			t.Errorf("candidate %d omitted spelling support tag: %v", index, candidate.Tags)
		}
		if index > 0 && candidate.Score > got[index-1].Score {
			t.Errorf("candidate scores not descending: %v then %v", got[index-1].Score, candidate.Score)
		}
		if candidate.Rationale == "" {
			t.Errorf("candidate %d rationale is empty", index)
		}
	}

	again, err := GenerateContentCandidates(metadata, DefaultContentConfig())
	if err != nil {
		t.Fatalf("second GenerateContentCandidates() error = %v", err)
	}
	if !reflect.DeepEqual(got, again) {
		t.Errorf("generation is not deterministic\nfirst:  %#v\nsecond: %#v", got, again)
	}
}

func TestGenerateContentCandidatesRejectsPartiallyMatchingTrendPhrase(t *testing.T) {
	metadata := VideoMetadata{
		Player:      "Zack",
		Map:         "Mirage",
		KillCount:   5,
		Weapons:     []string{"AK-47"},
		Moment:      "5-kill highlight",
		Hook:        "No podían parar estas bajas",
		SearchTerms: []string{"mirage ace", "mirage highlights", "AK-47 5 kills", "inferno clutch"},
	}

	got, err := GenerateContentCandidates(metadata, DefaultContentConfig())
	if err != nil {
		t.Fatalf("GenerateContentCandidates() error = %v", err)
	}
	for index, candidate := range got {
		if containsFold(candidate.Keywords, "mirage ace") || containsFold(candidate.Keywords, "inferno clutch") {
			t.Errorf("candidate %d admitted a partially matching trend phrase: %v", index, candidate.Keywords)
		}
		if !containsFold(candidate.Keywords, "mirage highlights") || !containsFold(candidate.Keywords, "AK-47 5 kills") {
			t.Errorf("candidate %d omitted factual trend phrases: %v", index, candidate.Keywords)
		}
		copy := strings.ToLower(candidate.Title + " " + candidate.Description + " " + strings.Join(candidate.Keywords, " "))
		if strings.Contains(copy, "ace") {
			t.Errorf("candidate %d invented an ace: %q", index, copy)
		}
	}
}

func TestGenerateContentCandidatesCount(t *testing.T) {
	metadata := VideoMetadata{Player: "Zack", Map: "Inferno", KillCount: 4}
	tests := []struct {
		name  string
		count int
	}{
		{name: "three", count: 3},
		{name: "four", count: 4},
		{name: "five", count: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultContentConfig()
			config.CandidateCount = tt.count
			got, err := GenerateContentCandidates(metadata, config)
			if err != nil {
				t.Fatalf("GenerateContentCandidates() error = %v", err)
			}
			if len(got) != tt.count {
				t.Errorf("candidate count = %d, want %d", len(got), tt.count)
			}
		})
	}
}

func TestGenerateContentCandidatesNormalizesAndBoundsText(t *testing.T) {
	metadata := VideoMetadata{
		Player:      "  Very   Long   Player Name  ",
		Map:         "de_ancient",
		KillCount:   12,
		Weapons:     []string{" Desert Eagle ", "Desert Eagle", strings.Repeat("x", 100)},
		Hook:        strings.Repeat("clutch ", 20),
		SearchTerms: []string{"  CS2   clutch ", "cs2 clutch"},
	}
	got, err := GenerateContentCandidates(metadata, DefaultContentConfig())
	if err != nil {
		t.Fatalf("GenerateContentCandidates() error = %v", err)
	}
	for _, candidate := range got {
		if err := ValidateContentCandidate(candidate); err != nil {
			t.Errorf("ValidateContentCandidate() error = %v", err)
		}
		if strings.Contains(candidate.Title, "  ") {
			t.Errorf("title contains repeated whitespace: %q", candidate.Title)
		}
		if countFold(candidate.Keywords, "CS2 clutch") != 1 {
			t.Errorf("keywords were not deduplicated: %v", candidate.Keywords)
		}
	}
}

func TestGenerateContentCandidatesValidation(t *testing.T) {
	validMetadata := VideoMetadata{Player: "Zack", Map: "Mirage", KillCount: 5}
	validConfig := DefaultContentConfig()
	tests := []struct {
		name     string
		metadata VideoMetadata
		config   ContentConfig
	}{
		{name: "missing player", metadata: VideoMetadata{Map: "Mirage", KillCount: 5}, config: validConfig},
		{name: "missing map", metadata: VideoMetadata{Player: "Zack", KillCount: 5}, config: validConfig},
		{name: "zero kills", metadata: VideoMetadata{Player: "Zack", Map: "Mirage"}, config: validConfig},
		{name: "too few candidates", metadata: validMetadata, config: ContentConfig{CandidateCount: 2, MaxKeywords: 8, MaxTags: 8}},
		{name: "too many tags", metadata: validMetadata, config: ContentConfig{CandidateCount: 3, MaxKeywords: 8, MaxTags: 16}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := GenerateContentCandidates(tt.metadata, tt.config); err == nil {
				t.Fatal("GenerateContentCandidates() error = nil, want validation error")
			}
		})
	}
}

func TestScoreContentRewardsClearHookAndContext(t *testing.T) {
	metadata := VideoMetadata{
		Player:    "Zack",
		Map:       "Mirage",
		KillCount: 5,
		Hook:      "Impossible ace",
	}
	base := ContentCandidate{
		Description: "Zack lands five kills on Mirage.\n\n#CS2 #Shorts",
		Keywords:    []string{"CS2 Shorts", "Mirage CS2", "Zack highlights"},
		Tags:        []string{"CS2", "Mirage", "Zack"},
	}
	clear := base
	clear.Title = "Impossible ace — Zack gets 5 kills on Mirage"
	vague := base
	vague.Title = "Watch this wild clip right now"

	clearScore, err := ScoreContent(clear, metadata)
	if err != nil {
		t.Fatalf("clear ScoreContent() error = %v", err)
	}
	vagueScore, err := ScoreContent(vague, metadata)
	if err != nil {
		t.Fatalf("vague ScoreContent() error = %v", err)
	}
	if clearScore <= vagueScore {
		t.Errorf("clear score = %v, want greater than vague score %v", clearScore, vagueScore)
	}
}

func TestValidateContentCandidateLimits(t *testing.T) {
	valid := ContentCandidate{
		Title:       "Five kills on Mirage",
		Description: "A concise description. #CS2 #Shorts",
		Keywords:    []string{"CS2 Shorts"},
		Tags:        []string{"CS2"},
	}
	tests := []struct {
		name      string
		candidate ContentCandidate
	}{
		{name: "valid", candidate: valid},
		{name: "long title", candidate: withTitle(valid, strings.Repeat("x", MaxYouTubeTitleRunes+1))},
		{name: "long description", candidate: withDescription(valid, strings.Repeat("x", MaxYouTubeDescriptionRunes+1))},
		{name: "duplicate keyword", candidate: withKeywords(valid, []string{"CS2", "cs2"})},
		{name: "duplicate tag", candidate: withTags(valid, []string{"CS2", "cs2"})},
		{name: "tag payload too long", candidate: withTags(valid, longDistinctTags())},
		{name: "too many hashtags", candidate: withDescription(valid, manyHashtags())},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateContentCandidate(tt.candidate)
			if tt.name == "valid" && err != nil {
				t.Fatalf("ValidateContentCandidate() error = %v", err)
			}
			if tt.name != "valid" && err == nil {
				t.Fatal("ValidateContentCandidate() error = nil, want validation error")
			}
		})
	}
}

func TestPrettyMapName(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "known ancient", raw: "de_ancient", want: "Ancient"},
		{name: "known dust2 has space", raw: "de_dust2", want: "Dust 2"},
		{name: "known office cs prefix", raw: "cs_office", want: "Office"},
		{name: "case insensitive key", raw: "DE_MIRAGE", want: "Mirage"},
		{name: "unknown de prefix", raw: "de_cache", want: "Cache"},
		{name: "unknown multi word", raw: "de_foo_bar", want: "Foo Bar"},
		{name: "already display name kept", raw: "Mirage", want: "Mirage"},
		{name: "display name with numerals kept", raw: "Dust II", want: "Dust II"},
		{name: "blank", raw: "  ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prettyMapName(tt.raw); got != tt.want {
				t.Errorf("prettyMapName(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestGenerateContentCandidatesMixesLanguages(t *testing.T) {
	metadata := VideoMetadata{
		Player:    "donk666",
		Map:       "de_ancient",
		KillCount: 12,
		Weapons:   []string{"AK-47"},
		Moment:    "12-kill highlight",
		Hook:      "No podían pararlo",
	}
	got, err := GenerateContentCandidates(metadata, DefaultContentConfig())
	if err != nil {
		t.Fatalf("GenerateContentCandidates() error = %v", err)
	}
	spanish, english := 0, 0
	for _, candidate := range got {
		lower := strings.ToLower(candidate.Description)
		switch {
		case strings.Contains(lower, "consigue"):
			spanish++
		case strings.Contains(lower, "lands"):
			english++
		}
		if strings.Contains(candidate.Title, "de_ancient") {
			t.Errorf("title uses raw engine map name: %q", candidate.Title)
		}
		if !strings.Contains(candidate.Title, "Ancient") {
			t.Errorf("title omits display map name: %q", candidate.Title)
		}
	}
	if spanish < 2 || english < 2 {
		t.Errorf("language mix = %d spanish, %d english; want at least 2 of each", spanish, english)
	}
}

func TestGenerateContentCandidatesMinimalMetadataYieldsFive(t *testing.T) {
	metadata := VideoMetadata{Player: "donk666", Map: "de_ancient", KillCount: 12}
	got, err := GenerateContentCandidates(metadata, DefaultContentConfig())
	if err != nil {
		t.Fatalf("GenerateContentCandidates() error = %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("candidate count = %d, want 5", len(got))
	}
	seen := make(map[string]struct{}, len(got))
	for _, candidate := range got {
		key := strings.ToLower(candidate.Title)
		if _, exists := seen[key]; exists {
			t.Errorf("duplicate title after dedup: %q", candidate.Title)
		}
		seen[key] = struct{}{}
	}

	again, err := GenerateContentCandidates(metadata, DefaultContentConfig())
	if err != nil {
		t.Fatalf("second GenerateContentCandidates() error = %v", err)
	}
	if !reflect.DeepEqual(got, again) {
		t.Errorf("generation is not deterministic\nfirst:  %#v\nsecond: %#v", got, again)
	}
}

func TestHookLedTitleAppendsOnlyMissingFacts(t *testing.T) {
	tests := []struct {
		name          string
		metadata      VideoMetadata
		mapName       string
		wantContains  []string
		wantNotSubstr []string
	}{
		{
			name:          "hook already states kills and map",
			metadata:      VideoMetadata{Player: "donk666", Map: "de_ancient", KillCount: 12, Hook: "12K con AK-47 en Ancient"},
			mapName:       "Ancient",
			wantContains:  []string{"12K con AK-47 en Ancient", "donk666", "(CS2)"},
			wantNotSubstr: []string{"12 kills", "kills en Ancient"},
		},
		{
			name:         "hook contains none of the facts",
			metadata:     VideoMetadata{Player: "donk666", Map: "de_ancient", KillCount: 12, Hook: "No podían pararlo"},
			mapName:      "Ancient",
			wantContains: []string{"No podían pararlo", "donk666", "12 kills en Ancient", "(CS2)"},
		},
		{
			name:          "hook already states player, kills, and map",
			metadata:      VideoMetadata{Player: "donk666", Map: "de_ancient", KillCount: 12, Hook: "donk666 imparable: 12K en Ancient"},
			mapName:       "Ancient",
			wantContains:  []string{"donk666 imparable: 12K en Ancient | CS2"},
			wantNotSubstr: []string{"—", "12 kills"},
		},
		{
			name:         "hook has kills but not map",
			metadata:     VideoMetadata{Player: "donk666", Map: "de_ancient", KillCount: 12, Hook: "12 bajas seguidas"},
			mapName:      "Ancient",
			wantContains: []string{"12 bajas seguidas", "donk666", "Ancient", "(CS2)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hookLedTitle(tt.metadata, tt.mapName)
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("hookLedTitle() = %q, want to contain %q", got, want)
				}
			}
			for _, unwanted := range tt.wantNotSubstr {
				if strings.Contains(got, unwanted) {
					t.Errorf("hookLedTitle() = %q, want NOT to contain %q", got, unwanted)
				}
			}
			if strings.Count(got, tt.mapName) > 1 {
				t.Errorf("hookLedTitle() = %q repeats map name %q", got, tt.mapName)
			}
		})
	}
}

func TestScoreContentAcceptsDisplayMapName(t *testing.T) {
	metadata := VideoMetadata{Player: "donk666", Map: "de_ancient", KillCount: 12}
	base := ContentCandidate{
		Description: "donk666 consigue 12 kills en Ancient.\n\n#CS2 #Shorts",
		Keywords:    []string{"CS2 Shorts", "Ancient CS2", "donk666 highlights"},
		Tags:        []string{"CS2", "Ancient", "donk666"},
	}
	display := base
	display.Title = "12 kills en Ancient — donk666"
	raw := base
	raw.Title = "12 kills en de_ancient — donk666"
	without := base
	without.Title = "12 kills — donk666"

	displayScore, err := ScoreContent(display, metadata)
	if err != nil {
		t.Fatalf("display ScoreContent() error = %v", err)
	}
	rawScore, err := ScoreContent(raw, metadata)
	if err != nil {
		t.Fatalf("raw ScoreContent() error = %v", err)
	}
	withoutScore, err := ScoreContent(without, metadata)
	if err != nil {
		t.Fatalf("without ScoreContent() error = %v", err)
	}
	// The +10 map credit must apply for both the display and raw engine name.
	if displayScore < withoutScore+10 {
		t.Errorf("display-map score = %v, want at least %v (no-map score + map credit)", displayScore, withoutScore+10)
	}
	if rawScore < withoutScore+10 {
		t.Errorf("raw-map score = %v, want at least %v (no-map score + map credit)", rawScore, withoutScore+10)
	}
}

func containsFold(values []string, target string) bool {
	return countFold(values, target) > 0
}

func countFold(values []string, target string) int {
	count := 0
	for _, value := range values {
		if strings.EqualFold(value, target) {
			count++
		}
	}
	return count
}

func withTitle(candidate ContentCandidate, title string) ContentCandidate {
	candidate.Title = title
	return candidate
}

func withDescription(candidate ContentCandidate, description string) ContentCandidate {
	candidate.Description = description
	return candidate
}

func withKeywords(candidate ContentCandidate, keywords []string) ContentCandidate {
	candidate.Keywords = keywords
	return candidate
}

func withTags(candidate ContentCandidate, tags []string) ContentCandidate {
	candidate.Tags = tags
	return candidate
}

func longDistinctTags() []string {
	tags := make([]string, 0, 6)
	for index := 0; index < 6; index++ {
		tags = append(tags, strings.Repeat(string(rune('a'+index)), 90))
	}
	return tags
}

func manyHashtags() string {
	hashtags := make([]string, 0, MaxYouTubeHashtags+1)
	for index := 0; index <= MaxYouTubeHashtags; index++ {
		hashtags = append(hashtags, "#tag"+string(rune('a'+index)))
	}
	return strings.Join(hashtags, " ")
}
