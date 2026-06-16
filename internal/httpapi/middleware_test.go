package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCrossSiteGuardBlocksBrowserCrossSiteMutations(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		secFetch   string
		origin     string
		host       string
		wantStatus int
	}{
		{name: "same-origin POST allowed", method: http.MethodPost, secFetch: "same-origin", host: "127.0.0.1:8080", wantStatus: http.StatusOK},
		{name: "cross-site POST blocked", method: http.MethodPost, secFetch: "cross-site", host: "127.0.0.1:8080", wantStatus: http.StatusForbidden},
		{name: "cross-origin POST blocked", method: http.MethodPost, secFetch: "cross-origin", host: "127.0.0.1:8080", wantStatus: http.StatusForbidden},
		{name: "same-site POST blocked", method: http.MethodPost, secFetch: "same-site", host: "127.0.0.1:8080", wantStatus: http.StatusForbidden},
		{name: "none POST allowed (direct navigation)", method: http.MethodPost, secFetch: "none", host: "127.0.0.1:8080", wantStatus: http.StatusOK},
		{name: "mismatched Origin blocked", method: http.MethodPost, origin: "http://evil.example", host: "127.0.0.1:8080", wantStatus: http.StatusForbidden},
		{name: "matching Origin allowed", method: http.MethodPost, origin: "http://127.0.0.1:8080", host: "127.0.0.1:8080", wantStatus: http.StatusOK},
		{name: "no headers POST allowed (curl/proxy)", method: http.MethodPost, host: "127.0.0.1:8080", wantStatus: http.StatusOK},
		{name: "cross-site GET not blocked", method: http.MethodGet, secFetch: "cross-site", host: "127.0.0.1:8080", wantStatus: http.StatusOK},
		{name: "mismatched Origin GET not blocked", method: http.MethodGet, origin: "http://evil.example", host: "127.0.0.1:8080", wantStatus: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(tc.method, "/api/jobs", nil)
			req.Host = tc.host
			if tc.secFetch != "" {
				req.Header.Set("Sec-Fetch-Site", tc.secFetch)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rw := httptest.NewRecorder()
			crossSiteGuard(next).ServeHTTP(rw, req)

			if rw.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rw.Code, tc.wantStatus, rw.Body.String())
			}
			if tc.wantStatus == http.StatusForbidden && rw.Body.String() == "" {
				t.Fatalf("blocked response has empty body")
			}
		})
	}
}

func TestRequireReadAuthGatesApiReadsWhenExposed(t *testing.T) {
	cases := []struct {
		name       string
		readAuth   bool
		path       string
		token      string
		wantStatus int
	}{
		{name: "exposed api read without token", readAuth: true, path: "/api/jobs", token: "", wantStatus: http.StatusUnauthorized},
		{name: "exposed api read with token", readAuth: true, path: "/api/jobs", token: "secret", wantStatus: http.StatusOK},
		{name: "exposed workbench shell stays open", readAuth: true, path: "/", token: "", wantStatus: http.StatusOK},
		{name: "loopback default api read open", readAuth: false, path: "/api/jobs", token: "", wantStatus: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handlers{mutationToken: "secret", requireReadAuth: tc.readAuth}
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.token != "" {
				req.Header.Set("X-FragForge-Token", tc.token)
			}
			rw := httptest.NewRecorder()
			h.requireMutationToken(next).ServeHTTP(rw, req)

			if rw.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rw.Code, tc.wantStatus, rw.Body.String())
			}
		})
	}
}

func TestRequireMutationTokenUsesConstantTimeCompare(t *testing.T) {
	cases := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{name: "wrong token", token: "wrong", wantStatus: http.StatusUnauthorized},
		{name: "correct token", token: "secret", wantStatus: http.StatusOK},
		{name: "missing token", token: "", wantStatus: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handlers{mutationToken: "secret"}
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodPost, "/api/jobs", nil)
			if tc.token != "" {
				req.Header.Set("X-FragForge-Token", tc.token)
			}
			rw := httptest.NewRecorder()
			h.requireMutationToken(next).ServeHTTP(rw, req)

			if rw.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rw.Code, tc.wantStatus)
			}
		})
	}
}

func TestRateLimiterDisabledNeverBlocks(t *testing.T) {
	l := newRateLimiter(0, 0)
	if l != nil {
		t.Fatalf("limiter = %v, want nil for disabled rps", l)
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := l.middleware(next)
	for i := range 100 {
		req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rw := httptest.NewRecorder()
		handler.ServeHTTP(rw, req)
		if rw.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200 (disabled limiter must not block)", i, rw.Code)
		}
	}
}

func TestRateLimiterThrottlesBurst(t *testing.T) {
	l := newRateLimiter(1, 3)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := l.middleware(next)

	var got429 bool
	for range 10 {
		req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
		req.RemoteAddr = "10.0.0.2:5555"
		rw := httptest.NewRecorder()
		handler.ServeHTTP(rw, req)
		if rw.Code == http.StatusTooManyRequests {
			got429 = true
			if rw.Header().Get("Retry-After") == "" {
				t.Fatalf("429 response missing Retry-After header")
			}
			break
		}
	}
	if !got429 {
		t.Fatal("burst never throttled, want a 429")
	}
}

func TestRateLimiterRefillsOverTime(t *testing.T) {
	l := newRateLimiter(10, 1)
	now := time.Now()
	if !l.allow("k", now) {
		t.Fatal("first request denied, want allowed")
	}
	if l.allow("k", now) {
		t.Fatal("second immediate request allowed, want denied (burst=1)")
	}
	// After 200ms at 10rps, ~2 tokens refill; one request should pass.
	if !l.allow("k", now.Add(200*time.Millisecond)) {
		t.Fatal("request after refill denied, want allowed")
	}
}

func TestRateLimiterKeysPerClientIP(t *testing.T) {
	l := newRateLimiter(1, 1)
	now := time.Now()
	if !l.allow("1.1.1.1", now) {
		t.Fatal("first client request denied, want allowed")
	}
	// A different client must not be throttled by the first client's usage.
	if !l.allow("2.2.2.2", now) {
		t.Fatal("second client request denied, want allowed (per-IP buckets)")
	}
}
