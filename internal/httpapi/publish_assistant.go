package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/youtubeinsights"
	"github.com/rechedev9/fragforge/internal/youtubetrends"
)

const (
	publishAssistantSchemaVersion = "1.0"
	publishAssistantStudioURL     = "https://studio.youtube.com/"
	publishAssistantDefaultDays   = 7
	publishAssistantMaxDays       = 14
	publishAssistantRequestTTL    = 30 * time.Second
	publishAssistantCacheTTL      = 6 * time.Hour
	publishAssistantRetryTTL      = 30 * time.Minute
)

// YouTubeTrends is the optional public discovery input for the manual
// publishing assistant. Its terms are hints and are filtered against reel facts.
type YouTubeTrends interface {
	Fetch(context.Context, string) (youtubetrends.TrendReport, error)
}

type publishAssistantResponse struct {
	SchemaVersion   string                   `json:"schema_version"`
	StudioURL       string                   `json:"studio_url"`
	Metadata        publishAssistantMetadata `json:"metadata"`
	Recommendations []publishRecommendation  `json:"recommendations"`
	Keywords        []string                 `json:"keywords"`
	Tags            []string                 `json:"tags"`
	Schedule        publishAssistantSchedule `json:"schedule"`
	Trends          publishAssistantTrends   `json:"trends"`
}

type publishAssistantMetadata struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

type publishRecommendation struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Keywords    []string `json:"keywords"`
	Tags        []string `json:"tags"`
	Score       float64  `json:"score"`
	Rationale   string   `json:"rationale"`
}

type publishAssistantSchedule struct {
	TimeZone    string               `json:"time_zone"`
	GeneratedAt time.Time            `json:"generated_at"`
	Days        []publishScheduleDay `json:"days"`
	Sources     []publishSource      `json:"sources"`
	Caveat      string               `json:"caveat"`
}

type publishScheduleDay struct {
	Date    string                `json:"date"`
	Weekday string                `json:"weekday"`
	Slots   []publishScheduleSlot `json:"slots"`
}

type publishScheduleSlot struct {
	PublishAt  time.Time `json:"publish_at"`
	LocalTime  string    `json:"local_time"`
	Source     string    `json:"source"`
	Confidence float64   `json:"confidence"`
	Score      float64   `json:"score"`
	Rationale  string    `json:"rationale"`
}

type publishSource struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type publishAssistantTrends struct {
	Available bool            `json:"available"`
	Terms     []string        `json:"terms"`
	FetchedAt *time.Time      `json:"fetched_at,omitempty"`
	Sources   []publishSource `json:"sources,omitempty"`
	Reason    string          `json:"reason,omitempty"`
}

type publishAssistantFacts struct {
	Player        string
	Map           string
	KillCount     int
	PrimaryWeapon string
	Hook          string
}

type publishAssistantCacheEntry struct {
	expiresAt time.Time
	response  publishAssistantResponse
}

type publishAssistantCall struct {
	done     chan struct{}
	response publishAssistantResponse
	err      error
}

type publishAssistantCache struct {
	mu       sync.Mutex
	entries  map[string]publishAssistantCacheEntry
	inFlight map[string]*publishAssistantCall
}

func newPublishAssistantCache() *publishAssistantCache {
	return &publishAssistantCache{
		entries:  make(map[string]publishAssistantCacheEntry),
		inFlight: make(map[string]*publishAssistantCall),
	}
}

func (c *publishAssistantCache) getOrBuild(
	ctx context.Context,
	key string,
	now time.Time,
	build func(context.Context) (publishAssistantResponse, bool, error),
) (publishAssistantResponse, error) {
	if c == nil {
		return publishAssistantResponse{}, errors.New("publish assistant cache is unavailable")
	}
	c.mu.Lock()
	for cachedKey, entry := range c.entries {
		if !entry.expiresAt.After(now) {
			delete(c.entries, cachedKey)
		}
	}
	if entry, ok := c.entries[key]; ok && entry.expiresAt.After(now) {
		c.mu.Unlock()
		return entry.response, nil
	}
	if call, ok := c.inFlight[key]; ok {
		c.mu.Unlock()
		select {
		case <-call.done:
			return call.response, call.err
		case <-ctx.Done():
			return publishAssistantResponse{}, ctx.Err()
		}
	}
	call := &publishAssistantCall{done: make(chan struct{})}
	c.inFlight[key] = call
	c.mu.Unlock()
	go c.runBuild(key, call, build)
	select {
	case <-call.done:
		return call.response, call.err
	case <-ctx.Done():
		return publishAssistantResponse{}, ctx.Err()
	}
}

func (c *publishAssistantCache) runBuild(
	key string,
	call *publishAssistantCall,
	build func(context.Context) (publishAssistantResponse, bool, error),
) {
	ctx, cancel := context.WithTimeout(context.Background(), publishAssistantRequestTTL)
	defer cancel()
	response, retrySoon, err := build(ctx)
	ttl := publishAssistantCacheTTL
	if retrySoon {
		ttl = publishAssistantRetryTTL
	}
	c.mu.Lock()
	call.response = response
	call.err = err
	if err == nil {
		c.entries[key] = publishAssistantCacheEntry{expiresAt: time.Now().Add(ttl), response: response}
	}
	delete(c.inFlight, key)
	close(call.done)
	c.mu.Unlock()
}

// GetPublishAssistant prepares factual metadata and a deterministic Madrid
// schedule for one finished MP4. It never changes publication state.
func (h *Handlers) GetPublishAssistant(w http.ResponseWriter, r *http.Request) {
	if publishAssistantRequestIsCrossSite(r) {
		writeError(w, http.StatusForbidden, "cross-site request blocked")
		return
	}
	days, err := publishAssistantDays(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), publishAssistantRequestTTL)
	defer cancel()
	r = r.WithContext(ctx)

	j, ok := h.loadJob(w, r)
	if !ok {
		return
	}
	variant := chi.URLParam(r, "variant")
	name := chi.URLParam(r, "name")
	facts, ok := h.loadPublishAssistantFacts(w, j.ID, variant, name)
	if !ok {
		return
	}
	now := time.Now()
	key := publishAssistantCacheKey(j.ID, variant, name, facts, days, now)
	response, err := h.publishAssistant.getOrBuild(ctx, key, now, func(buildCtx context.Context) (publishAssistantResponse, bool, error) {
		return h.buildPublishAssistant(buildCtx, now, days, facts)
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			writeError(w, http.StatusGatewayTimeout, "publish assistant timed out")
			return
		}
		internalError(w, "build publish assistant", err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handlers) buildPublishAssistant(
	ctx context.Context,
	now time.Time,
	days int,
	facts publishAssistantFacts,
) (publishAssistantResponse, bool, error) {
	metadata := youtubeinsights.VideoMetadata{
		Player:    facts.Player,
		Map:       facts.Map,
		KillCount: facts.KillCount,
		Hook:      facts.Hook,
		Moment:    fmt.Sprintf("%d-kill highlight", facts.KillCount),
	}
	if facts.PrimaryWeapon != "" {
		metadata.Weapons = []string{facts.PrimaryWeapon}
	}

	report, trendErr := youtubetrends.TrendReport{}, youtubetrends.ErrNotConfigured
	if h.youtubeTrends != nil {
		report, trendErr = h.youtubeTrends.Fetch(ctx, publishAssistantFocus(facts))
	}
	if ctx.Err() != nil {
		return publishAssistantResponse{}, false, ctx.Err()
	}
	filteredTerms := youtubeinsights.FilterFactualSearchTerms(metadata, report.Terms)
	metadata.SearchTerms = filteredTerms
	metadata.Misspellings = []string{"Counter Strike 2"}
	candidates, err := youtubeinsights.GenerateContentCandidates(metadata, youtubeinsights.DefaultContentConfig())
	if err != nil {
		return publishAssistantResponse{}, trendErr != nil, err
	}
	recommendations, keywords, tags := mapPublishRecommendations(candidates)

	daily, err := youtubeinsights.RecommendDaily(now, days, youtubeinsights.DefaultScheduleConfig())
	if err != nil {
		return publishAssistantResponse{}, trendErr != nil, err
	}
	best := recommendations[0]
	return publishAssistantResponse{
		SchemaVersion: publishAssistantSchemaVersion,
		StudioURL:     publishAssistantStudioURL,
		Metadata: publishAssistantMetadata{
			Title:       best.Title,
			Description: best.Description,
			Tags:        append([]string(nil), best.Tags...),
		},
		Recommendations: recommendations,
		Keywords:        keywords,
		Tags:            tags,
		Schedule: publishAssistantSchedule{
			TimeZone:    youtubeinsights.MadridTimeZone,
			GeneratedAt: now.UTC(),
			Days:        mapPublishSchedule(daily),
			Sources: []publishSource{{
				Title: "YouTube: subir vídeos en YouTube Studio",
				URL:   "https://support.google.com/youtube/answer/57407?hl=es",
			}},
			Caveat: "El horario es una referencia determinista en Europe/Madrid, no una predicción de rendimiento. Confirma audiencia, visibilidad y programación en YouTube Studio.",
		},
		Trends: mapPublishTrends(report, filteredTerms, metadata, trendErr),
	}, trendErr != nil && !errors.Is(trendErr, youtubetrends.ErrNotConfigured), nil
}

func (h *Handlers) loadPublishAssistantFacts(
	w http.ResponseWriter,
	id uuid.UUID,
	variant string,
	name string,
) (publishAssistantFacts, bool) {
	if _, err := renderplan.LoadoutForVariant(variant); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return publishAssistantFacts{}, false
	}
	videoRef, err := renderplan.NewRenderVariantArtifactRef(id, variant, renderplan.RenderVariantArtifactVideo, name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return publishAssistantFacts{}, false
	}
	video, err := h.storage.Open(videoRef.Key)
	if err != nil {
		writeError(w, http.StatusNotFound, "render video not found")
		return publishAssistantFacts{}, false
	}
	if err := video.Close(); err != nil {
		internalError(w, "close render video", err)
		return publishAssistantFacts{}, false
	}

	result, _, ok := h.loadRenderResult(w, id, variant)
	if !ok {
		return publishAssistantFacts{}, false
	}
	var short *editor.ShortResult
	for i := range result.Shorts {
		if result.Shorts[i].SegmentID == name {
			short = &result.Shorts[i]
			break
		}
	}
	if short == nil {
		writeError(w, http.StatusNotFound, "render video metadata not found")
		return publishAssistantFacts{}, false
	}

	packRef, err := renderplan.NewRenderVariantArtifactRef(id, variant, renderplan.RenderVariantArtifactPackManifest, "")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return publishAssistantFacts{}, false
	}
	rc, err := h.storage.Open(packRef.Key)
	if err != nil {
		if storage.IsNotExist(err) {
			writeError(w, http.StatusConflict, "publish metadata is not ready")
			return publishAssistantFacts{}, false
		}
		internalError(w, "open publish manifest", err)
		return publishAssistantFacts{}, false
	}
	defer rc.Close()
	var pack editor.PackManifest
	if err := json.NewDecoder(io.LimitReader(rc, maxJSONBodyBytes+1)).Decode(&pack); err != nil {
		internalError(w, "decode publish manifest", err)
		return publishAssistantFacts{}, false
	}
	for _, item := range pack.Items {
		if item.SegmentID != name {
			continue
		}
		facts := publishAssistantFacts{
			Player:        strings.TrimSpace(item.Player),
			Map:           strings.TrimSpace(item.Map),
			KillCount:     item.KillCount,
			PrimaryWeapon: strings.TrimSpace(item.PrimaryWeapon),
			Hook:          strings.TrimSpace(short.Headline),
		}
		if facts.Player == "" || facts.Map == "" || facts.KillCount <= 0 {
			writeError(w, http.StatusConflict, "publish metadata is incomplete")
			return publishAssistantFacts{}, false
		}
		return facts, true
	}
	writeError(w, http.StatusConflict, "publish metadata is not ready")
	return publishAssistantFacts{}, false
}

func mapPublishRecommendations(candidates []youtubeinsights.ContentCandidate) ([]publishRecommendation, []string, []string) {
	result := make([]publishRecommendation, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, publishRecommendation{
			Title:       candidate.Title,
			Description: candidate.Description,
			Keywords:    append([]string(nil), candidate.Keywords...),
			Tags:        append([]string(nil), candidate.Tags...),
			Score:       candidate.Score,
			Rationale:   candidate.Rationale,
		})
	}
	if len(result) == 0 {
		return result, []string{}, []string{}
	}
	return result, append([]string(nil), result[0].Keywords...), append([]string(nil), result[0].Tags...)
}

func mapPublishSchedule(days []youtubeinsights.DailyRecommendation) []publishScheduleDay {
	result := make([]publishScheduleDay, 0, len(days))
	for _, day := range days {
		slots := make([]publishScheduleSlot, 0, len(day.Slots))
		for _, slot := range day.Slots {
			slots = append(slots, publishScheduleSlot{
				PublishAt:  slot.PublishAt,
				LocalTime:  slot.PublishAt.Format("15:04"),
				Source:     "baseline",
				Confidence: slot.Confidence,
				Score:      slot.Score,
				Rationale:  "Referencia diaria de FragForge en hora de España.",
			})
		}
		result = append(result, publishScheduleDay{
			Date:    day.Date,
			Weekday: spanishPublishWeekday(day.Weekday),
			Slots:   slots,
		})
	}
	return result
}

func mapPublishTrends(
	report youtubetrends.TrendReport,
	terms []string,
	metadata youtubeinsights.VideoMetadata,
	err error,
) publishAssistantTrends {
	if err != nil {
		return publishAssistantTrends{
			Available: false,
			Terms:     []string{},
			Reason:    publishTrendReason(err),
		}
	}
	sources := make([]publishSource, 0, len(report.Results))
	for _, result := range report.Results {
		if len(youtubeinsights.FilterFactualSearchTerms(metadata, []string{result.Title})) == 0 {
			continue
		}
		sources = append(sources, publishSource{Title: result.Title, URL: result.URL})
	}
	fetchedAt := report.FetchedAt
	return publishAssistantTrends{
		Available: true,
		Terms:     append([]string(nil), terms...),
		FetchedAt: &fetchedAt,
		Sources:   sources,
	}
}

func publishTrendReason(err error) string {
	switch {
	case errors.Is(err, youtubetrends.ErrNotConfigured):
		return "Firecrawl es opcional; se muestran las recomendaciones factuales y el horario base."
	case errors.Is(err, youtubetrends.ErrUnauthorized):
		return "Firecrawl rechazó la credencial configurada; se usa el contenido base."
	case errors.Is(err, youtubetrends.ErrRateLimited):
		return "Firecrawl ha limitado temporalmente la consulta; se usa el contenido base."
	default:
		return "Las tendencias públicas no están disponibles ahora; se usa el contenido base."
	}
}

func publishAssistantFocus(facts publishAssistantFacts) string {
	parts := []string{facts.Player, facts.Map, facts.PrimaryWeapon, facts.Hook, strconv.Itoa(facts.KillCount)}
	return strings.Join(parts, " ")
}

func publishAssistantCacheKey(id uuid.UUID, variant, name string, facts publishAssistantFacts, days int, now time.Time) string {
	fingerprint := sha256.Sum256([]byte(strings.Join([]string{
		facts.Player,
		facts.Map,
		strconv.Itoa(facts.KillCount),
		facts.PrimaryWeapon,
		facts.Hook,
	}, "\x00")))
	return strings.Join([]string{
		id.String(), variant, name, madridPublishDate(now), strconv.Itoa(days), hex.EncodeToString(fingerprint[:]),
	}, ":")
}

func publishAssistantDays(r *http.Request) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("days"))
	if raw == "" {
		return publishAssistantDefaultDays, nil
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 1 || days > publishAssistantMaxDays {
		return 0, fmt.Errorf("days must be between 1 and %d", publishAssistantMaxDays)
	}
	return days, nil
}

func publishAssistantRequestIsCrossSite(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return true
	}
	return r.Header.Get("Sec-Fetch-Site") == "" && r.Header.Get("Origin") != "" && !originMatchesHost(r.Header.Get("Origin"), r.Host)
}

func madridPublishDate(now time.Time) string {
	location, err := time.LoadLocation(youtubeinsights.MadridTimeZone)
	if err != nil {
		return now.UTC().Format(time.DateOnly)
	}
	return now.In(location).Format(time.DateOnly)
}

func spanishPublishWeekday(day time.Weekday) string {
	switch day {
	case time.Monday:
		return "lunes"
	case time.Tuesday:
		return "martes"
	case time.Wednesday:
		return "miércoles"
	case time.Thursday:
		return "jueves"
	case time.Friday:
		return "viernes"
	case time.Saturday:
		return "sábado"
	default:
		return "domingo"
	}
}
