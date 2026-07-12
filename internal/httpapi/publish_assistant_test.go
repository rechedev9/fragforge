package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/youtubetrends"
)

type fakePublishAssistantTrends struct {
	mu      sync.Mutex
	calls   int
	report  youtubetrends.TrendReport
	reports []youtubetrends.TrendReport
	err     error
	started chan struct{}
	release <-chan struct{}
}

func (f *fakePublishAssistantTrends) Fetch(ctx context.Context, _ string) (youtubetrends.TrendReport, error) {
	f.mu.Lock()
	f.calls++
	started := f.started
	if started != nil {
		f.started = nil
		close(started)
	}
	report := f.report
	if len(f.reports) > 0 {
		report = f.reports[0]
		f.reports = f.reports[1:]
	}
	err, release := f.err, f.release
	f.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return youtubetrends.TrendReport{}, ctx.Err()
		}
	}
	return report, err
}

func (f *fakePublishAssistantTrends) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestGetPublishAssistantReturnsFactualManualPack(t *testing.T) {
	trends := &fakePublishAssistantTrends{report: youtubetrends.TrendReport{
		Terms: []string{"mirage ace", "AK-47 5 kills", "inferno clutch"},
		Results: []youtubetrends.Result{{
			Title: "Mirage ace with AK-47",
			URL:   "https://youtube.com/shorts/abc123",
		}},
		FetchedAt: time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC),
	}}
	h, url := newPublishAssistantFixture(t, trends, publishAssistantFacts{
		Player: "reche", Map: "Mirage", KillCount: 5, PrimaryWeapon: "AK-47", Hook: "5K TOTAL",
	})

	rw := httptest.NewRecorder()
	h.GetPublishAssistant(rw, assistantRequest(http.MethodGet, url))
	if got, want := rw.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d: %s", got, want, rw.Body.String())
	}
	var response publishAssistantResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.SchemaVersion != publishAssistantSchemaVersion || response.StudioURL != publishAssistantStudioURL {
		t.Fatalf("assistant identity = version %q studio %q", response.SchemaVersion, response.StudioURL)
	}
	if got := len(response.Recommendations); got < 3 || got > 5 {
		t.Fatalf("recommendations = %d, want 3..5", got)
	}
	if len(response.Schedule.Days) != publishAssistantDefaultDays || response.Schedule.TimeZone != "Europe/Madrid" {
		t.Fatalf("schedule = %+v", response.Schedule)
	}
	if got, want := response.Trends.Terms, []string{"AK-47 5 kills"}; !equalStrings(got, want) {
		t.Fatalf("trend terms = %v, want %v", got, want)
	}
	values := append([]string{response.Metadata.Title, response.Metadata.Description}, response.Keywords...)
	values = append(values, response.Tags...)
	for _, recommendation := range response.Recommendations {
		values = append(values, recommendation.Title, recommendation.Description)
		values = append(values, recommendation.Keywords...)
		values = append(values, recommendation.Tags...)
	}
	for _, value := range values {
		if containsAnyWord(value, "ace", "clutch", "inferno") {
			t.Fatalf("non-factual claim in %q", value)
		}
	}
	if trends.callCount() != 1 {
		t.Fatalf("trend calls = %d, want 1", trends.callCount())
	}
}

func TestGetPublishAssistantFallsBackWithoutFirecrawl(t *testing.T) {
	h, url := newPublishAssistantFixture(t, nil, publishAssistantFacts{
		Player: "reche", Map: "Dust II", KillCount: 3, PrimaryWeapon: "M4A1-S", Hook: "3K HOLD",
	})
	rw := httptest.NewRecorder()
	h.GetPublishAssistant(rw, assistantRequest(http.MethodGet, url+"?days=2"))
	if got, want := rw.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d: %s", got, want, rw.Body.String())
	}
	var response publishAssistantResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Trends.Available || !strings.Contains(response.Trends.Reason, "opcional") {
		t.Fatalf("trends = %+v", response.Trends)
	}
	if len(response.Schedule.Days) != 2 || len(response.Recommendations) < 3 {
		t.Fatalf("fallback response = %+v", response)
	}
}

func TestGetPublishAssistantFallsBackWhenFirecrawlFails(t *testing.T) {
	trends := &fakePublishAssistantTrends{err: youtubetrends.ErrUnavailable}
	h, url := newPublishAssistantFixture(t, trends, publishAssistantFacts{
		Player: "reche", Map: "Ancient", KillCount: 4, PrimaryWeapon: "AWP", Hook: "4K DEFENSE",
	})
	rw := httptest.NewRecorder()
	h.GetPublishAssistant(rw, assistantRequest(http.MethodGet, url))
	if got, want := rw.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d: %s", got, want, rw.Body.String())
	}
	var response publishAssistantResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Trends.Available || response.Metadata.Title == "" {
		t.Fatalf("fallback response = %+v", response)
	}
}

func TestGetPublishAssistantCoalescesConcurrentTrendFetches(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	trends := &fakePublishAssistantTrends{
		started: started,
		release: release,
		report: youtubetrends.TrendReport{
			Terms:     []string{"Mirage"},
			FetchedAt: time.Now().UTC(),
		},
	}
	h, url := newPublishAssistantFixture(t, trends, publishAssistantFacts{
		Player: "reche", Map: "Mirage", KillCount: 3, PrimaryWeapon: "AK-47", Hook: "3K ENTRY",
	})

	const requests = 8
	start := make(chan struct{})
	results := make(chan int, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rw := httptest.NewRecorder()
			h.GetPublishAssistant(rw, assistantRequest(http.MethodGet, url))
			results <- rw.Code
		}()
	}
	close(start)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("trend fetch did not start")
	}
	// Give the remaining handlers a chance to join the keyed in-flight call.
	time.Sleep(25 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)
	for status := range results {
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
	}
	if got := trends.callCount(); got != 1 {
		t.Fatalf("trend calls = %d, want 1", got)
	}

	rw := httptest.NewRecorder()
	h.GetPublishAssistant(rw, assistantRequest(http.MethodGet, url))
	if got := trends.callCount(); got != 1 {
		t.Fatalf("cached trend calls = %d, want 1", got)
	}
}

func TestGetPublishAssistantCoalescedBuildSurvivesInitiatorCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	trends := &fakePublishAssistantTrends{
		started: started,
		release: release,
		report: youtubetrends.TrendReport{
			Terms:     []string{"Mirage"},
			FetchedAt: time.Now().UTC(),
		},
	}
	h, url := newPublishAssistantFixture(t, trends, publishAssistantFacts{
		Player: "reche", Map: "Mirage", KillCount: 3, PrimaryWeapon: "AK-47", Hook: "3K ENTRY",
	})

	firstRequest := assistantRequest(http.MethodGet, url)
	firstContext, cancelFirst := context.WithCancel(firstRequest.Context())
	firstRequest = firstRequest.WithContext(firstContext)
	firstStatus := make(chan int, 1)
	go func() {
		rw := httptest.NewRecorder()
		h.GetPublishAssistant(rw, firstRequest)
		firstStatus <- rw.Code
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("trend fetch did not start")
	}

	waiterStatus := make(chan int, 1)
	go func() {
		rw := httptest.NewRecorder()
		h.GetPublishAssistant(rw, assistantRequest(http.MethodGet, url))
		waiterStatus <- rw.Code
	}()
	time.Sleep(25 * time.Millisecond)
	cancelFirst()
	if got := <-firstStatus; got != http.StatusGatewayTimeout {
		t.Fatalf("initiator status = %d, want 504", got)
	}
	close(release)
	if got := <-waiterStatus; got != http.StatusOK {
		t.Fatalf("waiter status = %d, want 200", got)
	}
	if got := trends.callCount(); got != 1 {
		t.Fatalf("trend calls = %d, want 1", got)
	}
}

func TestPublishAssistantCachePrunesExpiredEntries(t *testing.T) {
	cache := newPublishAssistantCache()
	now := time.Now()
	cache.entries["expired"] = publishAssistantCacheEntry{expiresAt: now.Add(-time.Second)}
	_, err := cache.getOrBuild(context.Background(), "fresh", now, func(context.Context) (publishAssistantResponse, bool, error) {
		return publishAssistantResponse{SchemaVersion: publishAssistantSchemaVersion}, false, nil
	})
	if err != nil {
		t.Fatalf("getOrBuild() error = %v", err)
	}
	cache.mu.Lock()
	_, expiredFound := cache.entries["expired"]
	_, freshFound := cache.entries["fresh"]
	cache.mu.Unlock()
	if expiredFound || !freshFound {
		t.Fatalf("cache entries: expired=%v fresh=%v", expiredFound, freshFound)
	}
}

func TestGetPublishAssistantBlocksCrossSiteBeforeTrendFetch(t *testing.T) {
	trends := &fakePublishAssistantTrends{}
	h, url := newPublishAssistantFixture(t, trends, publishAssistantFacts{
		Player: "reche", Map: "Nuke", KillCount: 2, Hook: "2K HOLD",
	})
	req := assistantRequest(http.MethodGet, url)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rw := httptest.NewRecorder()
	h.GetPublishAssistant(rw, req)
	if got, want := rw.Code, http.StatusForbidden; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
	if trends.callCount() != 0 {
		t.Fatalf("trend calls = %d, want 0", trends.callCount())
	}
}

func TestGetPublishAssistantRejectsInvalidDays(t *testing.T) {
	h, url := newPublishAssistantFixture(t, nil, publishAssistantFacts{
		Player: "reche", Map: "Nuke", KillCount: 2, Hook: "2K HOLD",
	})
	for _, query := range []string{"?days=0", "?days=15", "?days=nope"} {
		rw := httptest.NewRecorder()
		h.GetPublishAssistant(rw, assistantRequest(http.MethodGet, url+query))
		if got, want := rw.Code, http.StatusBadRequest; got != want {
			t.Fatalf("%s status = %d, want %d", query, got, want)
		}
	}
}

func TestPublishAssistantCacheIsIsolatedByReelFacts(t *testing.T) {
	trends := &fakePublishAssistantTrends{reports: []youtubetrends.TrendReport{
		{Terms: []string{"Mirage"}, FetchedAt: time.Now().UTC()},
		{Terms: []string{"Inferno"}, FetchedAt: time.Now().UTC()},
	}}
	repo := newFakeRepo()
	store := newFakeStorage()
	h := NewHandlers(repo, store, &fakeQueue{}, WithPublishAssistantTrends(trends))
	firstURL := addPublishAssistantFixture(t, repo, store, publishAssistantFacts{
		Player: "alpha", Map: "Mirage", KillCount: 3, Hook: "3K ENTRY",
	})
	secondURL := addPublishAssistantFixture(t, repo, store, publishAssistantFacts{
		Player: "bravo", Map: "Inferno", KillCount: 4, Hook: "4K HOLD",
	})

	for _, testCase := range []struct {
		url       string
		player    string
		trendTerm string
	}{
		{url: firstURL, player: "alpha", trendTerm: "Mirage"},
		{url: secondURL, player: "bravo", trendTerm: "Inferno"},
	} {
		rw := httptest.NewRecorder()
		h.GetPublishAssistant(rw, assistantRequest(http.MethodGet, testCase.url))
		if rw.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", rw.Code, rw.Body.String())
		}
		var response publishAssistantResponse
		if err := json.Unmarshal(rw.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ToLower(response.Metadata.Title), testCase.player) {
			t.Fatalf("title %q does not contain %q", response.Metadata.Title, testCase.player)
		}
		if got, want := response.Trends.Terms, []string{testCase.trendTerm}; !equalStrings(got, want) {
			t.Fatalf("trend terms = %v, want %v", got, want)
		}
	}
	if got := trends.callCount(); got != 2 {
		t.Fatalf("trend calls = %d, want 2", got)
	}
}

func newPublishAssistantFixture(
	t *testing.T,
	trends YouTubeTrends,
	facts publishAssistantFacts,
) (*Handlers, string) {
	t.Helper()
	repo := newFakeRepo()
	store := newFakeStorage()
	h := NewHandlers(repo, store, &fakeQueue{}, WithPublishAssistantTrends(trends))
	return h, addPublishAssistantFixture(t, repo, store, facts)
}

func addPublishAssistantFixture(
	t *testing.T,
	repo *fakeRepo,
	store *fakeStorage,
	facts publishAssistantFacts,
) string {
	t.Helper()
	id := uuid.New()
	repo.jobs[id] = job.Job{ID: id, Status: job.StatusDone}
	const variant = "viral-60-clean"
	const name = "compiled-001"

	result := editor.Result{Shorts: []editor.ShortResult{{SegmentID: name, Headline: facts.Hook}}}
	pack := editor.PackManifest{Items: []editor.PublishItem{{
		SegmentID:     name,
		Player:        facts.Player,
		Map:           facts.Map,
		KillCount:     facts.KillCount,
		PrimaryWeapon: facts.PrimaryWeapon,
	}}}
	putAssistantJSON(t, store, mustAssistantRef(t, id, variant, renderplan.RenderVariantArtifactResult, ""), result)
	putAssistantJSON(t, store, mustAssistantRef(t, id, variant, renderplan.RenderVariantArtifactPackManifest, ""), pack)
	store.puts[mustAssistantRef(t, id, variant, renderplan.RenderVariantArtifactVideo, name)] = []byte("mp4")
	return "/api/jobs/" + id.String() + "/renders/" + variant + "/videos/" + name + "/publish-assistant"
}

func putAssistantJSON(t *testing.T, store *fakeStorage, key string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(key, bytes.NewReader(b)); err != nil {
		t.Fatal(err)
	}
}

func mustAssistantRef(
	t *testing.T,
	id uuid.UUID,
	variant string,
	kind renderplan.RenderVariantArtifactKind,
	name string,
) string {
	t.Helper()
	ref, err := renderplan.NewRenderVariantArtifactRef(id, variant, kind, name)
	if err != nil {
		t.Fatal(err)
	}
	return ref.Key
}

func assistantRequest(method, target string) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	parts := strings.Split(strings.Trim(strings.SplitN(target, "?", 2)[0], "/"), "/")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", parts[2])
	rctx.URLParams.Add("variant", parts[4])
	rctx.URLParams.Add("name", parts[6])
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsAnyWord(value string, targets ...string) bool {
	words := strings.FieldsFunc(strings.ToLower(value), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	})
	for _, word := range words {
		for _, target := range targets {
			if word == target {
				return true
			}
		}
	}
	return false
}
