package youtubetrends

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFetchSendsBoundedSearchAndReturnsBrowserSafeReport(t *testing.T) {
	t.Parallel()

	const apiKey = "fc-test-secret"
	fetchedAt := time.Date(2026, time.July, 12, 20, 15, 0, 0, time.FixedZone("CEST", 2*60*60))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Method, http.MethodPost; got != want {
			t.Errorf("method = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer "+apiKey; got != want {
			t.Errorf("authorization = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Content-Type"), "application/json"; got != want {
			t.Errorf("content type = %q, want %q", got, want)
		}
		var payload searchRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if strings.Contains(fmt.Sprintf("%+v", payload), apiKey) {
			t.Fatal("request payload contains the API key")
		}
		if got, want := payload.Limit, 5; got != want {
			t.Errorf("limit = %d, want %d", got, want)
		}
		if got, want := payload.Sources, []string{"web"}; !reflect.DeepEqual(got, want) {
			t.Errorf("sources = %v, want %v", got, want)
		}
		if got, want := payload.IncludeDomains, []string{"youtube.com"}; !reflect.DeepEqual(got, want) {
			t.Errorf("include domains = %v, want %v", got, want)
		}
		if !strings.HasPrefix(payload.Query, baseQuery) {
			t.Errorf("query = %q, want fixed CS2 Shorts prefix", payload.Query)
		}
		if strings.Contains(payload.Query, "site:evil.example") {
			t.Errorf("query accepted a search operator from focus: %q", payload.Query)
		}
		if got, want := payload.TBS, "qdr:m"; got != want {
			t.Errorf("time filter = %q, want %q", got, want)
		}
		if got, want := payload.Country, "ES"; got != want {
			t.Errorf("country = %q, want %q", got, want)
		}
		if got, want := payload.Timeout, int64(10_000); got != want {
			t.Errorf("timeout = %d, want %d", got, want)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": {"web": [
				{"title":"Mirage AWP ace and clutch guide #CS2","url":"https://www.youtube.com/shorts/one"},
				{"title":"duplicate","url":"https://www.youtube.com/shorts/one"},
				{"title":"unsafe","url":"javascript:alert(1)"},
				{"title":"Smoke lineups on Mirage | Counter-Strike 2 Shorts","url":"http://youtube.com/shorts/two"},
				{"title":"AWP ace on Mirage with utility tricks","url":"https://m.youtube.com/shorts/three#comments"},
				{"title":"   ","url":"https://youtube.com/shorts/empty"},
				{"title":"reflected secret","url":"https://youtube.com/shorts/` + apiKey + `"},
				{"title":"Inferno deagle headshots and retake tips","url":"https://youtube.com/shorts/four"}
			]}
		}`))
	}))
	defer server.Close()

	client, err := New(Options{
		APIKey:     apiKey,
		HTTPClient: server.Client(),
		Endpoint:   server.URL,
		Now:        func() time.Time { return fetchedAt },
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	report, err := client.Fetch(context.Background(), `Mirage site:evil.example "OR" AWP extra ignored`)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got, want := report.FetchedAt, fetchedAt.UTC(); !got.Equal(want) {
		t.Errorf("fetched at = %s, want %s", got, want)
	}
	if got, want := len(report.Results), 3; got != want {
		t.Fatalf("result count = %d, want %d: %#v", got, want, report.Results)
	}
	for _, result := range report.Results {
		if result.Source != "youtube.com" {
			t.Errorf("source = %q, want youtube.com", result.Source)
		}
		if strings.Contains(result.URL, "#") {
			t.Errorf("URL retained fragment: %q", result.URL)
		}
		if strings.Contains(result.Title+result.URL, apiKey) {
			t.Fatal("report contains API key")
		}
	}
	if got := len(report.Terms); got < 5 || got > 10 {
		t.Errorf("term count = %d, want 5..10: %v", got, report.Terms)
	}
	serialized, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if strings.Contains(string(serialized), apiKey) {
		t.Fatal("serialized report contains API key")
	}
}

func TestFetchWithoutAPIKeyIsOptionalNotConfigured(t *testing.T) {
	t.Parallel()

	client, err := New(Options{})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Fetch(context.Background(), "mirage")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("fetch error = %v, want ErrNotConfigured", err)
	}
	var nilClient *Client
	_, err = nilClient.Fetch(context.Background(), "mirage")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil client fetch error = %v, want ErrNotConfigured", err)
	}
}

func TestFetchCapsReturnedResultsAtLowConfiguredLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"success":true,
			"data":{"web":[
				{"title":"one","url":"https://youtube.com/shorts/1"},
				{"title":"two","url":"https://youtube.com/shorts/2"},
				{"title":"three","url":"https://youtube.com/shorts/3"},
				{"title":"four","url":"https://youtube.com/shorts/4"},
				{"title":"five","url":"https://youtube.com/shorts/5"},
				{"title":"six","url":"https://youtube.com/shorts/6"}
			]}
		}`))
	}))
	defer server.Close()
	client, err := New(Options{APIKey: "fc-test", HTTPClient: server.Client(), Endpoint: server.URL, Limit: 3})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	report, err := client.Fetch(context.Background(), "")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got, want := len(report.Results), 3; got != want {
		t.Errorf("result count = %d, want %d", got, want)
	}
}

func TestFetchMapsRedactedHTTPFailures(t *testing.T) {
	t.Parallel()

	const apiKey = "fc-never-expose-this"
	tests := []struct {
		name       string
		status     int
		wantClass  error
		wantRetry  time.Duration
		retryAfter string
	}{
		{name: "unauthorized", status: http.StatusUnauthorized, wantClass: ErrUnauthorized},
		{name: "rate limited", status: http.StatusTooManyRequests, wantClass: ErrRateLimited, wantRetry: 7 * time.Second, retryAfter: "7"},
		{name: "server unavailable", status: http.StatusServiceUnavailable, wantClass: ErrUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"error":"` + apiKey + ` echoed by upstream"}`))
			}))
			defer server.Close()
			client, err := New(Options{APIKey: apiKey, HTTPClient: server.Client(), Endpoint: server.URL})
			if err != nil {
				t.Fatalf("new client: %v", err)
			}
			_, err = client.Fetch(context.Background(), "")
			if !errors.Is(err, test.wantClass) {
				t.Fatalf("fetch error = %v, want class %v", err, test.wantClass)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("fetch error type = %T, want *APIError", err)
			}
			if got, want := apiErr.StatusCode, test.status; got != want {
				t.Errorf("status = %d, want %d", got, want)
			}
			if got, want := apiErr.RetryAfter, test.wantRetry; got != want {
				t.Errorf("retry after = %s, want %s", got, want)
			}
			if strings.Contains(err.Error(), apiKey) {
				t.Fatal("error contains API key")
			}
		})
	}
}

func TestClientNeverSerializesOrFormatsAPIKey(t *testing.T) {
	t.Parallel()

	const apiKey = "fc-json-secret"
	client, err := New(Options{APIKey: apiKey})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	data, err := json.Marshal(client)
	if err != nil {
		t.Fatalf("marshal client: %v", err)
	}
	if got, want := string(data), `{"configured":true}`; got != want {
		t.Errorf("serialized client = %s, want %s", got, want)
	}
	for _, formatted := range []string{
		fmt.Sprintf("%v", client),
		fmt.Sprintf("%+v", client),
		fmt.Sprintf("%#v", client),
	} {
		if strings.Contains(formatted, apiKey) {
			t.Fatalf("formatted client contains API key: %s", formatted)
		}
	}
}

func TestFetchBoundsResponseAndRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "too large", body: strings.Repeat("x", 65), want: ErrResponseTooLarge},
		{name: "malformed", body: `{not-json}`, want: ErrInvalidResponse},
		{name: "unsuccessful", body: `{"success":false,"error":"unsafe details"}`, want: ErrInvalidResponse},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client, err := New(Options{
				APIKey:           "fc-test",
				HTTPClient:       server.Client(),
				Endpoint:         server.URL,
				MaxResponseBytes: 64,
			})
			if err != nil {
				t.Fatalf("new client: %v", err)
			}
			_, err = client.Fetch(context.Background(), "")
			if !errors.Is(err, test.want) {
				t.Fatalf("fetch error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestFetchHonorsContextTimeoutWithoutLeakingTransportError(t *testing.T) {
	t.Parallel()

	const apiKey = "fc-transport-secret"
	client, err := New(Options{
		APIKey: apiKey,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, fmt.Errorf("transport reflected %s: %w", apiKey, r.Context().Err())
		})},
		RequestTimeout: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	_, err = client.Fetch(context.Background(), "")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("fetch error = %v, want context deadline exceeded", err)
	}
	if strings.Contains(err.Error(), apiKey) {
		t.Fatal("timeout error contains API key")
	}
}

func TestNewRejectsUnsafeOrUnboundedOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts Options
	}{
		{name: "non http endpoint", opts: Options{Endpoint: "ftp://api.example/search"}},
		{name: "non local cleartext endpoint", opts: Options{Endpoint: "http://api.example/search"}},
		{name: "endpoint credentials", opts: Options{Endpoint: "https://user:pass@api.example/search"}},
		{name: "endpoint query", opts: Options{Endpoint: "https://api.example/search?key=secret"}},
		{name: "too many results", opts: Options{Limit: maxLimit + 1}},
		{name: "negative limit", opts: Options{Limit: -1}},
		{name: "too long timeout", opts: Options{RequestTimeout: maxRequestTimeout + time.Millisecond}},
		{name: "too large response", opts: Options{MaxResponseBytes: hardMaxResponseBytes + 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(test.opts); err == nil {
				t.Fatal("New() error = nil, want validation error")
			}
		})
	}
}

func TestCleanWebURLAllowsOnlyHTTPAndHTTPS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		raw    string
		wantOK bool
	}{
		{name: "https", raw: "https://www.youtube.com/shorts/abc", wantOK: true},
		{name: "http", raw: "http://youtube.com/shorts/abc"},
		{name: "javascript", raw: "javascript:alert(1)"},
		{name: "ftp", raw: "ftp://youtube.com/file"},
		{name: "userinfo", raw: "https://user:pass@youtube.com/shorts/abc"},
		{name: "backslash", raw: `https://youtube.com\\@evil.example/shorts/abc`},
		{name: "relative", raw: "/shorts/abc"},
		{name: "different host", raw: "https://evil.example/shorts/abc"},
		{name: "not a short", raw: "https://youtube.com/watch?v=abc"},
		{name: "missing short id", raw: "https://youtube.com/shorts/"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, got := cleanWebURL(test.raw)
			if got != test.wantOK {
				t.Errorf("cleanWebURL(%q) ok = %t, want %t", test.raw, got, test.wantOK)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}
