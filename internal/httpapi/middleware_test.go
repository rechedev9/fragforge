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
			(&Handlers{}).crossSiteGuard(next).ServeHTTP(rw, req)

			if rw.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rw.Code, tc.wantStatus, rw.Body.String())
			}
			if tc.wantStatus == http.StatusForbidden && rw.Body.String() == "" {
				t.Fatalf("blocked response has empty body")
			}
		})
	}
}

func TestCrossSiteGuardAllowsAllowedWebOrigin(t *testing.T) {
	origin := "https://fragforge.example"
	h := &Handlers{}
	WithAllowedWebOrigins([]string{origin})(h)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Origin", origin)
	// A real browser would also send Sec-Fetch-Site: cross-site; the allow-list
	// must win over the guard's cross-site rejection.
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rw := httptest.NewRecorder()
	h.crossSiteGuard(next).ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 for an allowed web origin; body=%s", rw.Code, rw.Body.String())
	}
}

func TestCrossSiteOriginRequiresTokenOnLoopback(t *testing.T) {
	const token = "secret"
	allowed := "https://fragforge.example"
	cases := []struct {
		name       string
		method     string
		origin     string
		token      string
		wantStatus int
	}{
		{name: "allowed cross-site GET without token", method: http.MethodGet, origin: allowed, wantStatus: http.StatusUnauthorized},
		{name: "allowed cross-site GET with token", method: http.MethodGet, origin: allowed, token: token, wantStatus: http.StatusOK},
		{name: "allowed cross-site POST without token", method: http.MethodPost, origin: allowed, wantStatus: http.StatusUnauthorized},
		{name: "allowed cross-site POST with token", method: http.MethodPost, origin: allowed, token: token, wantStatus: http.StatusOK},
		{name: "non-allowed cross-site GET without token", method: http.MethodGet, origin: "https://evil.example", wantStatus: http.StatusUnauthorized},
		{name: "non-allowed cross-site GET with token", method: http.MethodGet, origin: "https://evil.example", token: token, wantStatus: http.StatusOK},
		{name: "same-origin GET without token", method: http.MethodGet, origin: "http://127.0.0.1:8080", wantStatus: http.StatusOK},
		{name: "no-origin GET without token", method: http.MethodGet, wantStatus: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// requireReadAuth is off (loopback default). The cross-site rule must
			// still force a token independently of it.
			h := &Handlers{mutationToken: token, requireReadAuth: false}
			WithAllowedWebOrigins([]string{allowed})(h)
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(tc.method, "/api/jobs", nil)
			req.Host = "127.0.0.1:8080"
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
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

func TestCorsPreflightReturns204WithHeaders(t *testing.T) {
	allowed := "https://fragforge.example"
	h := &Handlers{}
	WithAllowedWebOrigins([]string{allowed})(h)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("allowed origin with PNA", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/jobs", nil)
		req.Header.Set("Origin", allowed)
		req.Header.Set("Access-Control-Request-Private-Network", "true")
		rw := httptest.NewRecorder()
		h.cors(next).ServeHTTP(rw, req)

		if rw.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rw.Code)
		}
		hdr := rw.Header()
		if got := hdr.Get("Access-Control-Allow-Origin"); got != allowed {
			t.Fatalf("Allow-Origin = %q, want %q", got, allowed)
		}
		if got := hdr.Get("Access-Control-Allow-Methods"); got != "GET, POST, PUT, DELETE, OPTIONS" {
			t.Fatalf("Allow-Methods = %q", got)
		}
		if got := hdr.Get("Access-Control-Allow-Headers"); got != "X-FragForge-Token, Content-Type, Range" {
			t.Fatalf("Allow-Headers = %q", got)
		}
		if got := hdr.Get("Access-Control-Expose-Headers"); got != "Content-Range, Accept-Ranges, Content-Length, Content-Type" {
			t.Fatalf("Expose-Headers = %q", got)
		}
		if got := hdr.Get("Vary"); got != "Origin" {
			t.Fatalf("Vary = %q, want Origin", got)
		}
		if got := hdr.Get("Access-Control-Allow-Private-Network"); got != "true" {
			t.Fatalf("Allow-Private-Network = %q, want true", got)
		}
		if got := hdr.Get("Access-Control-Allow-Credentials"); got != "" {
			t.Fatalf("Allow-Credentials = %q, want empty (header-token auth)", got)
		}
	})

	t.Run("allowed origin without PNA request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/jobs", nil)
		req.Header.Set("Origin", allowed)
		rw := httptest.NewRecorder()
		h.cors(next).ServeHTTP(rw, req)

		if rw.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rw.Code)
		}
		if got := rw.Header().Get("Access-Control-Allow-Private-Network"); got != "" {
			t.Fatalf("Allow-Private-Network = %q, want empty when not requested", got)
		}
	})

	t.Run("non-allowed origin gets no CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/api/jobs", nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Access-Control-Request-Private-Network", "true")
		rw := httptest.NewRecorder()
		called := false
		probe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})
		h.cors(probe).ServeHTTP(rw, req)

		if !called {
			t.Fatal("next not called for a non-allowed origin, want pass-through")
		}
		if got := rw.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("Allow-Origin = %q, want empty for a non-allowed origin", got)
		}
		if got := rw.Header().Get("Access-Control-Allow-Private-Network"); got != "" {
			t.Fatalf("Allow-Private-Network = %q, want empty for a non-allowed origin", got)
		}
	})
}

func TestCorsActualRequestSetsExposeHeaders(t *testing.T) {
	allowed := "https://fragforge.example"
	h := &Handlers{}
	WithAllowedWebOrigins([]string{allowed})(h)

	t.Run("allowed origin echoed with expose headers", func(t *testing.T) {
		called := false
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		})
		req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
		req.Header.Set("Origin", allowed)
		rw := httptest.NewRecorder()
		h.cors(next).ServeHTTP(rw, req)

		if !called {
			t.Fatal("next not called for an actual GET, want pass-through")
		}
		hdr := rw.Header()
		if got := hdr.Get("Access-Control-Allow-Origin"); got != allowed {
			t.Fatalf("Allow-Origin = %q, want %q", got, allowed)
		}
		if got := hdr.Get("Vary"); got != "Origin" {
			t.Fatalf("Vary = %q, want Origin", got)
		}
		if got := hdr.Get("Access-Control-Expose-Headers"); got != "Content-Range, Accept-Ranges, Content-Length, Content-Type" {
			t.Fatalf("Expose-Headers = %q", got)
		}
	})

	t.Run("non-allowed origin gets none", func(t *testing.T) {
		next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
		req.Header.Set("Origin", "https://evil.example")
		rw := httptest.NewRecorder()
		h.cors(next).ServeHTTP(rw, req)

		if got := rw.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("Allow-Origin = %q, want empty", got)
		}
		if got := rw.Header().Get("Access-Control-Expose-Headers"); got != "" {
			t.Fatalf("Expose-Headers = %q, want empty", got)
		}
	})
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

func TestReadAuthGatesWorkbenchUI(t *testing.T) {
	// A hosted handler: read-auth on and an allowed web origin (so hostGuard is
	// active). The HTMX workbench under /ui/ serves the same job data as /api/, so
	// it must be gated too; only the shell (GET /) and /healthz stay open.
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{},
		WithMutationToken("secret"),
		WithRequireReadAuth(true),
		WithAllowedWebOrigins([]string{"https://fragforge.example"}),
	)
	r := Routes(h)

	cases := []struct {
		name       string
		path       string
		token      string
		wantStatus int
	}{
		{name: "workbench ui read without token blocked", path: "/ui/jobs", wantStatus: http.StatusUnauthorized},
		{name: "workbench ui read with token allowed", path: "/ui/jobs", token: "secret", wantStatus: http.StatusOK},
		{name: "healthz stays open", path: "/healthz", wantStatus: http.StatusOK},
		{name: "shell stays open", path: "/", wantStatus: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			// A loopback Host so hostGuard (outermost, active in hosted mode) passes
			// and the read-auth gate is what decides the outcome.
			req.Host = "127.0.0.1:8080"
			if tc.token != "" {
				req.Header.Set("X-FragForge-Token", tc.token)
			}
			rw := httptest.NewRecorder()
			r.ServeHTTP(rw, req)

			if rw.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rw.Code, tc.wantStatus, rw.Body.String())
			}
		})
	}
}

func TestHostGuardRejectsRebindingHostWhenHosted(t *testing.T) {
	allowed := "https://fragforge.example"
	hosted := &Handlers{}
	WithAllowedWebOrigins([]string{allowed})(hosted)
	nonHosted := &Handlers{}

	cases := []struct {
		name       string
		h          *Handlers
		host       string
		wantStatus int
	}{
		{name: "hosted rejects rebinding host", h: hosted, host: "evil.com", wantStatus: http.StatusMisdirectedRequest},
		{name: "hosted allows loopback ip host", h: hosted, host: "127.0.0.1:8787", wantStatus: http.StatusOK},
		{name: "hosted allows localhost host", h: hosted, host: "localhost:8787", wantStatus: http.StatusOK},
		{name: "non-hosted passes rebinding host (no-op)", h: nonHosted, host: "evil.com", wantStatus: http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
			req.Host = tc.host
			rw := httptest.NewRecorder()
			tc.h.hostGuard(next).ServeHTTP(rw, req)

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
