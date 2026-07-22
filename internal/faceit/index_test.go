package faceit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIndexBuildsCompleteManualDemoManifest(t *testing.T) {
	t.Parallel()

	const apiKey = "faceit-test-secret"
	generatedAt := time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	seenDetails := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer "+apiKey; got != want {
			t.Errorf("authorization = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/players":
			if got, want := r.URL.Query().Get("nickname"), "m0NESY"; got != want {
				t.Errorf("nickname = %q, want %q", got, want)
			}
			_, _ = w.Write([]byte(`{
				"player_id":"player-1","nickname":"m0NESY","country":"ru","steam_id_64":"76561198000000001",
				"games":{"cs2":{"region":"EU","skill_level":10,"faceit_elo":4000,"game_player_id":"76561198000000001"}}
			}`))
		case "/players/player-1/history":
			assertQueryValue(t, r.URL.Query(), "game", "cs2")
			assertQueryValue(t, r.URL.Query(), "limit", "100")
			_, _ = w.Write([]byte(`{"items":[
				{"match_id":"match-old","region":"EU","competition_name":"FACEIT 5v5","competition_type":"matchmaking","started_at":1767225600,"finished_at":1767229200,
				 "teams":{"faction1":{"players":[{"player_id":"player-1"}]},"faction2":{"players":[{"player_id":"other"}]}},
				 "results":{"winner":"faction1","score":{"faction1":13,"faction2":8}}},
				{"match_id":"match-new","region":"EU","competition_name":"FACEIT 5v5","competition_type":"matchmaking","started_at":1767312000,"finished_at":1767315600,
				 "teams":{"faction1":{"players":[{"player_id":"other"}]},"faction2":{"players":[{"player_id":"player-1"}]}},
				 "results":{"winner":"faction1","score":{"faction1":13,"faction2":11}}}
			]}`))
		case "/players/player-1/games/cs2/stats":
			_, _ = w.Write([]byte(`{"items":[
				{"stats":{"Match Id":"match-old","Match Finished At":1767229200000,"Map":"de_mirage","Result":"1","Rounds":"21","Kills":"22","Deaths":"14","Assists":"5","Damage":"2100","ADR":"100.0","K/D Ratio":"1.57","K/R Ratio":"1.05","Headshots":"5","Triple Kills":"1","Quadro Kills":"0","Penta Kills":"0"}},
				{"stats":{"Match Id":"match-new","Match Finished At":1767315600000,"Map":"de_ancient","Result":"0","Rounds":"24","Kills":"30","Deaths":"18","Assists":"4","Damage":"2640","ADR":"110.0","K/D Ratio":"1.67","K/R Ratio":"1.25","Headshots":"8","Triple Kills":"2","Quadro Kills":"1","Penta Kills":"0"}}
			]}`))
		case "/matches/match-old":
			mu.Lock()
			seenDetails["match-old"] = true
			mu.Unlock()
			_, _ = w.Write([]byte(`{"demo_url":[],"instances":[]}`))
		case "/matches/match-new":
			mu.Lock()
			seenDetails["match-new"] = true
			mu.Unlock()
			_, _ = w.Write([]byte(`{"demo_url":["https://demos.faceit.example/match-new.dem.zst","https://demos.faceit.example/` + apiKey + `.dem.zst"],"instances":[{"demos":["https://demos.faceit.example/match-new.dem.zst"]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := New(Options{
		APIKey:        apiKey,
		BaseURL:       server.URL,
		HTTPClient:    server.Client(),
		DetailWorkers: 2,
		Now:           func() time.Time { return generatedAt },
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	index, err := client.Index(context.Background(), IndexRequest{
		Profile: "https://www.faceit.com/es/players/m0NESY",
		From:    time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		To:      time.Date(2026, time.January, 2, 23, 59, 59, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if got, want := index.SchemaVersion, SchemaVersion; got != want {
		t.Errorf("schema version = %q, want %q", got, want)
	}
	if got, want := index.Player.Nickname, "m0NESY"; got != want {
		t.Errorf("nickname = %q, want %q", got, want)
	}
	if got, want := len(index.Matches), 2; got != want {
		t.Fatalf("matches = %d, want %d", got, want)
	}
	if got, want := index.Matches[0].ID, "match-new"; got != want {
		t.Errorf("first match = %q, want %q", got, want)
	}
	if got, want := index.Matches[0].RoomURL, "https://www.faceit.com/en/cs2/room/match-new"; got != want {
		t.Errorf("room URL = %q, want %q", got, want)
	}
	if !index.Matches[0].DemoAvailable || index.Matches[0].DemoLookupStatus != "available" || len(index.Matches[0].DemoResourceURLs) != 1 {
		t.Errorf("new demo state = %#v, want one available resource", index.Matches[0])
	}
	if index.Matches[1].DemoAvailable || index.Matches[1].DemoLookupStatus != "unavailable" {
		t.Errorf("old demo state = %#v, want unavailable", index.Matches[1])
	}
	if got, want := index.HighlightMatchIDs, []string{"match-new", "match-old"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("highlight ids = %v, want %v", got, want)
	}
	if got, want := index.Summary, (Summary{Matches: 2, StatsEnriched: 2, DemoAvailable: 1, DemoUnavailable: 1, Wins: 1, Losses: 1, Kills: 52, Deaths: 32, Assists: 9, AverageADR: 105}); got != want {
		t.Errorf("summary = %#v, want %#v", got, want)
	}
	mu.Lock()
	detailCount := len(seenDetails)
	mu.Unlock()
	if detailCount != 2 {
		t.Errorf("detail lookups = %d, want 2", detailCount)
	}
	serialized, err := json.Marshal(index)
	if err != nil {
		t.Fatalf("marshal index: %v", err)
	}
	if strings.Contains(string(serialized), apiKey) {
		t.Fatal("serialized index contains API key")
	}
}

func TestFetchHistoryPaginatesWithoutDroppingMatches(t *testing.T) {
	t.Parallel()

	var offsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		response := apiHistoryResponse{}
		if offset == "0" {
			for i := range pageSize {
				response.Items = append(response.Items, apiHistoryItem{MatchID: fmt.Sprintf("match-%03d", i)})
			}
		} else {
			response.Items = append(response.Items, apiHistoryItem{MatchID: "match-100"})
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	client, err := New(Options{APIKey: "test", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	items, err := client.fetchHistory(context.Background(), "player-1", from, from.Add(time.Hour))
	if err != nil {
		t.Fatalf("fetch history: %v", err)
	}
	if got, want := len(items), 101; got != want {
		t.Fatalf("items = %d, want %d", got, want)
	}
	if got, want := strings.Join(offsets, ","), "0,100"; got != want {
		t.Errorf("offsets = %q, want %q", got, want)
	}
}

func TestFetchStatsPaginatesWithoutDroppingMatches(t *testing.T) {
	t.Parallel()

	var offsets []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		response := apiStatsResponse{}
		if offset == "0" {
			for i := range pageSize {
				response.Items = append(response.Items, apiStatsItem{Stats: apiMatchStats{MatchID: statValue(fmt.Sprintf("match-%03d", i))}})
			}
		} else {
			response.Items = append(response.Items, apiStatsItem{Stats: apiMatchStats{MatchID: "match-100"}})
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	client, err := New(Options{APIKey: "test", BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	items, err := client.fetchStats(context.Background(), "player-1", from, from.Add(time.Hour))
	if err != nil {
		t.Fatalf("fetch stats: %v", err)
	}
	if got, want := len(items), 101; got != want {
		t.Fatalf("items = %d, want %d", got, want)
	}
	if got, want := strings.Join(offsets, ","), "0,100"; got != want {
		t.Errorf("offsets = %q, want %q", got, want)
	}
}

func TestParseProfileAcceptsNicknameAndOfficialURLs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "m0NESY", want: "m0NESY"},
		{input: "https://www.faceit.com/en/players/ZywOo", want: "ZywOo"},
		{input: "https://faceit.com/es/players/s1mple/cs2", want: "s1mple"},
		{input: "https://evil.example/players/m0NESY", wantErr: true},
		{input: "http://faceit.com/en/players/m0NESY", wantErr: true},
		{input: "name/with/slash", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseProfile(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("ParseProfile(%q) error = nil, want error", test.input)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("ParseProfile(%q) = %q, %v; want %q", test.input, got, err, test.want)
			}
		})
	}
}

func TestClientNeverReflectsAPIKeyFromTransportError(t *testing.T) {
	t.Parallel()
	const apiKey = "do-not-leak-this-key"
	client, err := New(Options{
		APIKey: apiKey,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, errors.New("transport saw " + r.Header.Get("Authorization"))
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.fetchPlayer(context.Background(), "m0NESY")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if strings.Contains(fmt.Sprint(err), apiKey) || strings.Contains(fmt.Sprintf("%#v", client), apiKey) {
		t.Fatal("client error or GoString contains API key")
	}
}

func TestIndexRejectsCredentialReflectedByUpstreamMetadata(t *testing.T) {
	t.Parallel()
	const apiKey = "faceit-reflected-credential-value"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/players":
			_, _ = w.Write([]byte(`{"player_id":"player-1","nickname":"` + apiKey + `","games":{"cs2":{}}}`))
		case "/players/player-1/history", "/players/player-1/games/cs2/stats":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := New(Options{APIKey: apiKey, BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, time.July, 22, 0, 0, 0, 0, time.UTC)
	_, err = client.Index(context.Background(), IndexRequest{Profile: "m0NESY", From: from, To: from.Add(time.Hour)})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("error = %v, want ErrInvalidResponse", err)
	}
}

func TestIndexRequiresAPIKey(t *testing.T) {
	t.Parallel()
	client, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Index(context.Background(), IndexRequest{Profile: "m0NESY", From: time.Now(), To: time.Now()})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("error = %v, want ErrNotConfigured", err)
	}
}

func assertQueryValue(t *testing.T, query url.Values, key, want string) {
	t.Helper()
	if got := query.Get(key); got != want {
		t.Errorf("query %s = %q, want %q", key, got, want)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}
