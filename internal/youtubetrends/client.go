package youtubetrends

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const baseQuery = `site:youtube.com/shorts (CS2 OR "Counter-Strike 2") Shorts`

type Client struct {
	apiKey           string
	httpClient       *http.Client
	endpoint         string
	limit            int
	requestTimeout   time.Duration
	maxResponseBytes int64
	now              func() time.Time
}

type searchRequest struct {
	Query          string   `json:"query"`
	Limit          int      `json:"limit"`
	Sources        []string `json:"sources"`
	IncludeDomains []string `json:"includeDomains"`
	TBS            string   `json:"tbs"`
	Country        string   `json:"country"`
	Timeout        int64    `json:"timeout"`
}

type searchResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Web []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
		} `json:"web"`
	} `json:"data"`
}

// New constructs a Firecrawl trend client without performing network I/O.
// An empty API key is allowed so this optional integration can be wired at
// startup; Fetch will return ErrNotConfigured until a key is supplied.
func New(opts Options) (*Client, error) {
	endpoint := strings.TrimSpace(opts.Endpoint)
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}
	limit := opts.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	if limit < 1 || limit > maxLimit {
		return nil, fmt.Errorf("firecrawl trend result limit must be between 1 and %d", maxLimit)
	}
	requestTimeout := opts.RequestTimeout
	if requestTimeout == 0 {
		requestTimeout = defaultRequestTimeout
	}
	if requestTimeout < time.Millisecond || requestTimeout > maxRequestTimeout {
		return nil, fmt.Errorf("firecrawl trend request timeout must be between 1ms and %s", maxRequestTimeout)
	}
	maxResponseBytes := opts.MaxResponseBytes
	if maxResponseBytes == 0 {
		maxResponseBytes = defaultMaxResponseBytes
	}
	if maxResponseBytes < 1 || maxResponseBytes > hardMaxResponseBytes {
		return nil, fmt.Errorf("firecrawl trend response limit must be between 1 and %d bytes", hardMaxResponseBytes)
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		apiKey:           strings.TrimSpace(opts.APIKey),
		httpClient:       httpClient,
		endpoint:         endpoint,
		limit:            limit,
		requestTimeout:   requestTimeout,
		maxResponseBytes: maxResponseBytes,
		now:              now,
	}, nil
}

// Fetch discovers a bounded set of recent public YouTube Shorts references
// about CS2. Focus is optional, tokenized, and capped before it is appended to
// the fixed query, so callers cannot turn this into an unrestricted web search.
func (c *Client) Fetch(ctx context.Context, focus string) (TrendReport, error) {
	if c == nil || c.apiKey == "" {
		return TrendReport{}, ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	payload := searchRequest{
		Query:          buildQuery(focus),
		Limit:          c.limit,
		Sources:        []string{"web"},
		IncludeDomains: []string{"youtube.com"},
		TBS:            "qdr:m",
		Country:        "ES",
		Timeout:        c.requestTimeout.Milliseconds(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return TrendReport{}, fmt.Errorf("encode firecrawl trend search: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return TrendReport{}, fmt.Errorf("build firecrawl trend search: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return TrendReport{}, fmt.Errorf("firecrawl trend search: %w", ctxErr)
		}
		// Do not wrap an arbitrary transport error: a custom transport could
		// reflect the Authorization header in its error text.
		return TrendReport{}, ErrUnavailable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return TrendReport{}, &APIError{
			StatusCode: res.StatusCode,
			RetryAfter: parseRetryAfter(res.Header.Get("Retry-After"), c.now()),
		}
	}

	var response searchResponse
	if err := decodeBoundedJSON(res.Body, c.maxResponseBytes, &response); err != nil {
		return TrendReport{}, err
	}
	if !response.Success {
		return TrendReport{}, ErrInvalidResponse
	}

	results := make([]Result, 0, c.limit)
	seenURLs := make(map[string]struct{}, c.limit)
	for _, raw := range response.Data.Web {
		if len(results) == c.limit {
			break
		}
		if strings.Contains(raw.URL, c.apiKey) {
			continue
		}
		cleanURL, source, ok := cleanWebURL(raw.URL)
		if !ok {
			continue
		}
		if _, exists := seenURLs[cleanURL]; exists {
			continue
		}
		title := cleanText(strings.ReplaceAll(raw.Title, c.apiKey, "[redacted]"), 180)
		if title == "" {
			continue
		}
		seenURLs[cleanURL] = struct{}{}
		results = append(results, Result{Title: title, Source: source, URL: cleanURL})
	}
	return TrendReport{
		Terms:     ExtractTerms(results),
		Results:   results,
		FetchedAt: c.now().UTC(),
	}, nil
}

// MarshalJSON exposes only whether this optional client is configured. It is
// intentionally impossible to serialize its credential or endpoint state.
func (c *Client) MarshalJSON() ([]byte, error) {
	configured := c != nil && c.apiKey != ""
	return json.Marshal(struct {
		Configured bool `json:"configured"`
	}{Configured: configured})
}

func (c *Client) String() string {
	return "youtubetrends.Client{api_key:[redacted]}"
}

func (c *Client) GoString() string {
	return c.String()
}

func validateEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("firecrawl trend endpoint must be an absolute HTTP(S) URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("firecrawl trend endpoint cannot contain credentials, a query, or a fragment")
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("firecrawl trend endpoint must use HTTPS unless it is local")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func buildQuery(focus string) string {
	tokens := queryTokens(focus, 4)
	if len(tokens) == 0 {
		return baseQuery
	}
	return baseQuery + " " + strings.Join(tokens, " ")
}

func queryTokens(value string, limit int) []string {
	tokens := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	var word strings.Builder
	flush := func() {
		if word.Len() == 0 || len(tokens) == limit {
			word.Reset()
			return
		}
		token := strings.ToLower(word.String())
		word.Reset()
		if utf8.RuneCountInString(token) < 2 || utf8.RuneCountInString(token) > 24 {
			return
		}
		if _, exists := seen[token]; exists {
			return
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word.WriteRune(r)
			continue
		}
		flush()
		if len(tokens) == limit {
			break
		}
	}
	flush()
	return tokens
}

func decodeBoundedJSON(r io.Reader, limit int64, dst any) error {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return fmt.Errorf("read firecrawl trend response: %w", err)
	}
	if int64(len(data)) > limit {
		return ErrResponseTooLarge
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return ErrInvalidResponse
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return ErrInvalidResponse
	}
	return nil
}

func cleanWebURL(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\t\\") {
		return "", "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return "", "", false
	}
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	u.RawQuery = ""
	source := strings.ToLower(u.Hostname())
	source = strings.TrimPrefix(source, "www.")
	source = strings.TrimPrefix(source, "m.")
	if source != "youtube.com" && !strings.HasSuffix(source, ".youtube.com") {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "shorts" || parts[1] == "" {
		return "", "", false
	}
	return u.String(), "youtube.com", true
}

func cleanText(value string, maxRunes int) string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
	clean := strings.Join(fields, " ")
	if utf8.RuneCountInString(clean) <= maxRunes {
		return clean
	}
	runes := []rune(clean)
	return strings.TrimSpace(string(runes[:maxRunes]))
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	seconds, err := strconv.Atoi(raw)
	if err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	at, err := http.ParseTime(raw)
	if err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
