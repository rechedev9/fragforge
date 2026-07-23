// Package tuiclient is a small, dependency-light HTTP client for the FragForge
// orchestrator API (the /api/jobs and /api/stream-jobs surfaces). It exists so
// the terminal UI (cmd/zv-tui) can drive the exact same flow the web Studio
// drives, without importing the heavy server-side domain packages (parser,
// editor, lua, ...). The DTOs in types.go mirror the wire format emitted by
// internal/httpapi; they are versioned schemas, so a mismatch surfaces as a
// missing field rather than silent corruption.
package tuiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultBaseURL is the orchestrator's default loopback bind (see PRODUCT.md;
// ZV_HTTP_ADDR defaults to 127.0.0.1:8080).
const DefaultBaseURL = "http://127.0.0.1:8080"

// tokenHeader is the mutation/read token header the orchestrator checks
// (internal/httpapi/routes.go requireMutationToken).
const tokenHeader = "X-FragForge-Token"

// Config configures a Client. Zero values fall back to sensible defaults and
// the standard FragForge environment variables.
type Config struct {
	// BaseURL is the orchestrator root, e.g. "http://127.0.0.1:8080". Empty
	// falls back to ORCHESTRATOR_URL, then DefaultBaseURL.
	BaseURL string
	// Token is the X-FragForge-Token value. Empty falls back to
	// ZV_MUTATION_TOKEN. Production orchestrators require a per-session token
	// even on loopback; an empty value is retained only so the server can return
	// its canonical authentication error.
	Token string
	// HTTPClient is optional; a 30s-timeout client is used when nil.
	HTTPClient *http.Client
}

// Client talks to one orchestrator instance.
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

// New builds a Client, resolving BaseURL and Token from the environment when
// they are not set explicitly.
func New(cfg Config) *Client {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = strings.TrimRight(os.Getenv("ORCHESTRATOR_URL"), "/")
	}
	if base == "" {
		base = DefaultBaseURL
	}
	token := cfg.Token
	if token == "" {
		token = os.Getenv("ZV_MUTATION_TOKEN")
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: base, token: token, hc: hc}
}

// BaseURL returns the resolved orchestrator root, for display.
func (c *Client) BaseURL() string { return c.baseURL }

// APIError is a non-2xx response from the orchestrator. It carries the status
// code so callers can distinguish "not ready yet" (409) and "gone" (404) from
// real failures.
type APIError struct {
	StatusCode int
	Message    string
	Method     string
	Path       string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("%s %s: %d %s", e.Method, e.Path, e.StatusCode, msg)
}

// IsNotReady reports whether err is a 409 Conflict, which the orchestrator uses
// both for "artifact not ready yet" (e.g. GET plan before parse finishes) and
// for "action not valid in this state". The TUI treats it as "keep polling".
func IsNotReady(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict
}

// IsNotFound reports whether err is a 404.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// StatusCode returns the HTTP status of an APIError, or 0 if err is not one.
func StatusCode(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode
	}
	return 0
}

// getJSON performs a GET and decodes the JSON body into out.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

// doJSON performs an HTTP request with an optional JSON request body and decodes
// a JSON response into out (skipped when out is nil).
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, method, path, out)
}

// send dispatches a prepared request, applies auth, and handles the response.
func (c *Client) send(req *http.Request, method, path string, out any) error {
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set(tokenHeader, c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newAPIError(resp, method, path)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}

// newAPIError reads a bounded error body and extracts the orchestrator's
// {"error": "..."} message when present, else falls back to plain text.
func newAPIError(resp *http.Response, method, path string) *APIError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	msg := strings.TrimSpace(string(body))
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
		msg = payload.Error
	}
	return &APIError{StatusCode: resp.StatusCode, Message: msg, Method: method, Path: path}
}

// stream issues a GET and copies the raw (non-JSON) response body to dst, for
// endpoints that return an MP4 or HTML artifact. It returns the Content-Type.
func (c *Client) stream(ctx context.Context, path string, dst io.Writer) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	if c.token != "" {
		req.Header.Set(tokenHeader, c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", newAPIError(resp, http.MethodGet, path)
	}
	if _, err := io.Copy(dst, resp.Body); err != nil {
		return "", fmt.Errorf("GET %s: stream body: %w", path, err)
	}
	return resp.Header.Get("Content-Type"), nil
}

// configJSON marshals a multipart "config" field object, skipping empty values.
// Using json.Marshal (rather than fmt.Sprintf %q) keeps user-supplied values
// such as a stream title JSON-safe: %q emits Go escapes, not JSON escapes, so a
// title with a control character would otherwise produce a body the server
// rejects with 400.
func configJSON(fields map[string]string) string {
	obj := make(map[string]string, len(fields))
	for k, v := range fields {
		if v != "" {
			obj[k] = v
		}
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// query builds a "?k=v" suffix from a set of parameters, skipping empty values.
func query(params map[string]string) string {
	values := url.Values{}
	for k, v := range params {
		if v != "" {
			values.Set(k, v)
		}
	}
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}
