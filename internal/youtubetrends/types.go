// Package youtubetrends discovers a small, current set of public CS2 Shorts
// references through Firecrawl. It deliberately reports no popularity or
// recommendation metrics: search results are trend hints, not YouTube data.
package youtubetrends

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

const (
	defaultEndpoint         = "https://api.firecrawl.dev/v2/search"
	defaultLimit            = 5
	maxLimit                = 5
	defaultRequestTimeout   = 10 * time.Second
	maxRequestTimeout       = 30 * time.Second
	defaultMaxResponseBytes = int64(512 * 1024)
	hardMaxResponseBytes    = int64(2 * 1024 * 1024)
)

var (
	// ErrNotConfigured means no Firecrawl API key was supplied. Callers can
	// treat it as a normal optional-integration state.
	ErrNotConfigured = errors.New("firecrawl trends are not configured")
	// ErrUnauthorized means Firecrawl rejected the configured API key.
	ErrUnauthorized = errors.New("firecrawl trends authorization failed")
	// ErrRateLimited means Firecrawl rejected the request due to a rate or
	// concurrency limit.
	ErrRateLimited = errors.New("firecrawl trends rate limited")
	// ErrUnavailable means Firecrawl or the request transport was unavailable.
	ErrUnavailable = errors.New("firecrawl trends unavailable")
	// ErrResponseTooLarge means Firecrawl exceeded the client's bounded
	// response budget.
	ErrResponseTooLarge = errors.New("firecrawl trends response is too large")
	// ErrInvalidResponse means Firecrawl returned malformed or unsuccessful
	// JSON with an HTTP success status.
	ErrInvalidResponse = errors.New("firecrawl trends response is invalid")
)

// Options supplies the Firecrawl credential and protocol seams. APIKey is
// retained only in unexported client state and is never included in reports or
// errors. Production callers normally leave every field except APIKey unset.
type Options struct {
	APIKey           string
	HTTPClient       *http.Client
	Endpoint         string
	Limit            int
	RequestTimeout   time.Duration
	MaxResponseBytes int64
	Now              func() time.Time
}

// Result is one browser-safe public search reference. Source is derived from
// the validated URL hostname rather than copied from untrusted response text.
type Result struct {
	Title  string `json:"title"`
	Source string `json:"source"`
	URL    string `json:"url"`
}

// TrendReport contains discovery hints only. It intentionally has no views,
// engagement, ranking score, or other purported YouTube algorithm metrics.
type TrendReport struct {
	Terms     []string  `json:"terms"`
	Results   []Result  `json:"results"`
	FetchedAt time.Time `json:"fetched_at"`
}

// APIError is a redacted non-success response from Firecrawl. Response bodies
// are never retained because an upstream service may reflect credentials.
type APIError struct {
	StatusCode int
	RetryAfter time.Duration
}

func (e *APIError) Error() string {
	switch {
	case e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden:
		return "firecrawl trends authorization failed"
	case e.StatusCode == http.StatusTooManyRequests:
		return "firecrawl trends rate limited"
	case e.StatusCode >= 500:
		return "firecrawl trends unavailable"
	default:
		return fmt.Sprintf("firecrawl trends request failed with status %d", e.StatusCode)
	}
}

// Is lets callers handle common Firecrawl failure classes without parsing an
// error message or exposing the upstream response body.
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
