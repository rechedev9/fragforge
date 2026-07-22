// Package faceit indexes a player's FACEIT match history for deterministic
// demo acquisition. Match statistics are triage metadata only; the downloaded
// CS2 demo remains FragForge's source of truth for recording decisions.
package faceit

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

const SchemaVersion = "fragforge.faceit-demo-index/v1"

var (
	ErrNotConfigured   = errors.New("FACEIT Data API is not configured")
	ErrUnauthorized    = errors.New("FACEIT Data API authorization failed")
	ErrRateLimited     = errors.New("FACEIT Data API rate limited")
	ErrUnavailable     = errors.New("FACEIT Data API unavailable")
	ErrInvalidResponse = errors.New("FACEIT Data API response is invalid")
)

// Options supplies the FACEIT credential and protocol seams. APIKey is kept in
// unexported client state and is never serialized or copied into an error.
type Options struct {
	APIKey           string
	BaseURL          string
	HTTPClient       *http.Client
	RequestTimeout   time.Duration
	MaxResponseBytes int64
	DetailWorkers    int
	Now              func() time.Time
}

type IndexRequest struct {
	Profile string
	From    time.Time
	To      time.Time
}

type Index struct {
	SchemaVersion      string      `json:"schema_version"`
	GeneratedAt        time.Time   `json:"generated_at"`
	Source             string      `json:"source"`
	Player             Player      `json:"player"`
	Range              DateRange   `json:"range"`
	Acquisition        Acquisition `json:"acquisition"`
	Summary            Summary     `json:"summary"`
	HighlightRankBasis []string    `json:"highlight_rank_basis"`
	HighlightMatchIDs  []string    `json:"highlight_match_ids"`
	Matches            []Match     `json:"matches"`
}

type Player struct {
	ID         string `json:"id"`
	Nickname   string `json:"nickname"`
	SteamID64  string `json:"steam_id64,omitempty"`
	ProfileURL string `json:"profile_url"`
	Country    string `json:"country,omitempty"`
	Region     string `json:"region,omitempty"`
	SkillLevel int    `json:"skill_level,omitempty"`
	ELO        int    `json:"elo,omitempty"`
}

type DateRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type Acquisition struct {
	Mode                         string `json:"mode"`
	Instructions                 string `json:"instructions"`
	DownloadAPIAutomationPending bool   `json:"download_api_automation_pending"`
}

type Summary struct {
	Matches         int     `json:"matches"`
	StatsEnriched   int     `json:"stats_enriched"`
	DemoAvailable   int     `json:"demo_available"`
	DemoUnavailable int     `json:"demo_unavailable"`
	Wins            int     `json:"wins"`
	Losses          int     `json:"losses"`
	Kills           int     `json:"kills"`
	Deaths          int     `json:"deaths"`
	Assists         int     `json:"assists"`
	AverageADR      float64 `json:"average_adr"`
}

type Match struct {
	ID               string      `json:"id"`
	RoomURL          string      `json:"room_url"`
	StartedAt        time.Time   `json:"started_at,omitempty"`
	FinishedAt       time.Time   `json:"finished_at,omitempty"`
	Competition      string      `json:"competition,omitempty"`
	CompetitionType  string      `json:"competition_type,omitempty"`
	Region           string      `json:"region,omitempty"`
	Score            MatchScore  `json:"score"`
	Stats            *MatchStats `json:"stats,omitempty"`
	DemoLookupStatus string      `json:"demo_lookup_status"`
	DemoAvailable    bool        `json:"demo_available"`
	DemoResourceURLs []string    `json:"demo_resource_urls"`
}

type MatchScore struct {
	PlayerTeam string `json:"player_team,omitempty"`
	WinnerTeam string `json:"winner_team,omitempty"`
	For        int    `json:"for,omitempty"`
	Against    int    `json:"against,omitempty"`
}

type MatchStats struct {
	Map              string  `json:"map,omitempty"`
	Result           string  `json:"result"`
	Rounds           int     `json:"rounds,omitempty"`
	Kills            int     `json:"kills"`
	Deaths           int     `json:"deaths"`
	Assists          int     `json:"assists"`
	Damage           int     `json:"damage,omitempty"`
	ADR              float64 `json:"adr,omitempty"`
	KDRatio          float64 `json:"kd_ratio,omitempty"`
	KRRatio          float64 `json:"kr_ratio,omitempty"`
	Headshots        int     `json:"headshots,omitempty"`
	HeadshotsPercent float64 `json:"headshots_percent,omitempty"`
	DoubleKills      int     `json:"double_kills,omitempty"`
	TripleKills      int     `json:"triple_kills,omitempty"`
	QuadroKills      int     `json:"quadro_kills,omitempty"`
	PentaKills       int     `json:"penta_kills,omitempty"`
}

// APIError is a redacted non-success response. Upstream response bodies are
// never retained because services may reflect request credentials.
type APIError struct {
	StatusCode int
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	switch {
	case e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden:
		return ErrUnauthorized.Error()
	case e.StatusCode == http.StatusTooManyRequests:
		return ErrRateLimited.Error()
	case e.StatusCode >= 500:
		return ErrUnavailable.Error()
	default:
		return fmt.Sprintf("FACEIT Data API request failed with status %d", e.StatusCode)
	}
}

func (e *APIError) Is(target error) bool {
	switch target {
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrUnavailable:
		return e.StatusCode >= 500
	default:
		return false
	}
}
