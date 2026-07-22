package faceit

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
)

const (
	defaultBaseURL          = "https://open.faceit.com/data/v4"
	defaultRequestTimeout   = 30 * time.Second
	maxRequestTimeout       = 2 * time.Minute
	defaultMaxResponseBytes = int64(8 * 1024 * 1024)
	hardMaxResponseBytes    = int64(32 * 1024 * 1024)
	defaultDetailWorkers    = 4
	maxDetailWorkers        = 12
	maxRequestAttempts      = 4
)

type Client struct {
	apiKey           string
	baseURL          string
	httpClient       *http.Client
	requestTimeout   time.Duration
	maxResponseBytes int64
	detailWorkers    int
	now              func() time.Time
}

func New(opts Options) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	if err := validateBaseURL(baseURL); err != nil {
		return nil, err
	}
	requestTimeout := opts.RequestTimeout
	if requestTimeout == 0 {
		requestTimeout = defaultRequestTimeout
	}
	if requestTimeout < time.Millisecond || requestTimeout > maxRequestTimeout {
		return nil, fmt.Errorf("FACEIT request timeout must be between 1ms and %s", maxRequestTimeout)
	}
	maxResponseBytes := opts.MaxResponseBytes
	if maxResponseBytes == 0 {
		maxResponseBytes = defaultMaxResponseBytes
	}
	if maxResponseBytes < 1 || maxResponseBytes > hardMaxResponseBytes {
		return nil, fmt.Errorf("FACEIT response limit must be between 1 and %d bytes", hardMaxResponseBytes)
	}
	detailWorkers := opts.DetailWorkers
	if detailWorkers == 0 {
		detailWorkers = defaultDetailWorkers
	}
	if detailWorkers < 1 || detailWorkers > maxDetailWorkers {
		return nil, fmt.Errorf("FACEIT detail workers must be between 1 and %d", maxDetailWorkers)
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
		baseURL:          baseURL,
		httpClient:       httpClient,
		requestTimeout:   requestTimeout,
		maxResponseBytes: maxResponseBytes,
		detailWorkers:    detailWorkers,
		now:              now,
	}, nil
}

func (c *Client) MarshalJSON() ([]byte, error) {
	configured := c != nil && c.apiKey != ""
	return json.Marshal(struct {
		Configured bool `json:"configured"`
	}{Configured: configured})
}

func (c *Client) String() string {
	return "faceit.Client{api_key:[redacted]}"
}

func (c *Client) GoString() string {
	return c.String()
}

func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("FACEIT base URL must be an absolute HTTP(S) URL")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("FACEIT base URL cannot contain credentials, a query, or a fragment")
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("FACEIT base URL must use HTTPS unless it is local")
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

func (c *Client) getJSON(ctx context.Context, endpoint string, query url.Values, dst any) error {
	if c == nil || c.apiKey == "" {
		return ErrNotConfigured
	}
	requestURL := c.baseURL + endpoint
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}

	for attempt := 0; attempt < maxRequestAttempts; attempt++ {
		requestCtx, cancel := context.WithTimeout(ctx, c.requestTimeout)
		req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, requestURL, nil)
		if err != nil {
			cancel()
			return fmt.Errorf("build FACEIT request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")

		res, err := c.httpClient.Do(req)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				cancel()
				return fmt.Errorf("FACEIT request: %w", ctxErr)
			}
			if requestErr := requestCtx.Err(); requestErr != nil {
				cancel()
				return fmt.Errorf("FACEIT request: %w", requestErr)
			}
			cancel()
			// A custom transport can reflect headers in an error. Keep the
			// credential out of errors by returning a stable sentinel.
			return ErrUnavailable
		}

		if res.StatusCode == http.StatusOK {
			err = decodeBoundedJSON(res.Body, c.maxResponseBytes, dst)
			_ = res.Body.Close()
			cancel()
			return err
		}

		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		_ = res.Body.Close()
		apiErr := &APIError{
			StatusCode: res.StatusCode,
			RetryAfter: parseRetryAfter(res.Header.Get("Retry-After"), c.now()),
		}
		cancel()
		if !retryableStatus(res.StatusCode) || attempt == maxRequestAttempts-1 {
			return apiErr
		}
		delay := apiErr.RetryAfter
		if delay <= 0 {
			delay = time.Duration(1<<attempt) * 250 * time.Millisecond
		}
		if delay > 10*time.Second {
			delay = 10 * time.Second
		}
		if err := waitContext(ctx, delay); err != nil {
			return fmt.Errorf("FACEIT request retry: %w", err)
		}
	}
	return ErrUnavailable
}

func retryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func decodeBoundedJSON(r io.Reader, limit int64, dst any) error {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return ErrInvalidResponse
	}
	if int64(len(data)) > limit || len(bytes.TrimSpace(data)) == 0 {
		return ErrInvalidResponse
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return ErrInvalidResponse
	}
	return nil
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
