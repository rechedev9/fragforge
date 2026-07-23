package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestRequireReadAuthGatesExposedReads(t *testing.T) {
	cases := []struct {
		name        string
		readAuth    bool
		method      string
		path        string
		token       string
		wantStatus  int
		wantReached bool
	}{
		{name: "exposed api read without token", readAuth: true, path: "/api/jobs", wantStatus: http.StatusUnauthorized},
		{name: "exposed api read with token", readAuth: true, path: "/api/jobs", token: "secret", wantStatus: http.StatusOK, wantReached: true},
		{name: "exposed workbench data without token", readAuth: true, path: "/ui/jobs", wantStatus: http.StatusUnauthorized},
		{name: "exposed workbench data with wrong token", readAuth: true, path: "/ui/jobs", token: "wrong", wantStatus: http.StatusUnauthorized},
		{name: "exposed workbench data with token", readAuth: true, path: "/ui/jobs", token: "secret", wantStatus: http.StatusOK, wantReached: true},
		{name: "exposed workbench head without token", readAuth: true, method: http.MethodHead, path: "/ui/jobs", wantStatus: http.StatusUnauthorized},
		{name: "exposed workbench head with token", readAuth: true, method: http.MethodHead, path: "/ui/jobs", token: "secret", wantStatus: http.StatusOK, wantReached: true},
		{name: "exposed workbench shell stays open", readAuth: true, path: "/", wantStatus: http.StatusOK, wantReached: true},
		{name: "loopback default api read open", readAuth: false, path: "/api/jobs", wantStatus: http.StatusOK, wantReached: true},
		{name: "missing configured capability fails closed", readAuth: true, path: "/api/jobs", wantStatus: http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := "secret"
			if tc.name == "missing configured capability fails closed" {
				token = ""
			}
			h := &Handlers{mutationToken: token, requireReadAuth: tc.readAuth}
			reached := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			})
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			req := httptest.NewRequest(method, tc.path, nil)
			if tc.token != "" {
				req.Header.Set("X-FragForge-Token", tc.token)
			}
			rw := httptest.NewRecorder()
			h.requireMutationToken(next).ServeHTTP(rw, req)

			if rw.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rw.Code, tc.wantStatus, rw.Body.String())
			}
			if reached != tc.wantReached {
				t.Fatalf("handler reached = %v, want %v", reached, tc.wantReached)
			}
		})
	}
}

func TestSessionCapabilityBlocksSameOriginDNSRebindingRequest(t *testing.T) {
	h := &Handlers{mutationToken: "unguessable-session-capability", requireReadAuth: true}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", nil)
	req.Host = "attacker.example:8080"
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.requireMutationToken(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
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

func TestRateLimiterEvictsIdleBucketsAndCapsCardinality(t *testing.T) {
	l := newRateLimiter(1, 1)
	now := time.Now()
	if !l.allow("idle", now) {
		t.Fatal("initial request denied")
	}
	if !l.allow("fresh", now.Add(rateLimiterBucketTTL)) {
		t.Fatal("fresh request denied")
	}
	if _, ok := l.buckets["idle"]; ok {
		t.Fatal("idle bucket survived TTL eviction")
	}

	for i := range rateLimiterMaxBuckets + 100 {
		l.allow("client-"+strconv.Itoa(i), now.Add(rateLimiterBucketTTL+time.Duration(i)*time.Millisecond))
	}
	if got := len(l.buckets); got > rateLimiterMaxBuckets {
		t.Fatalf("bucket count = %d, want <= %d", got, rateLimiterMaxBuckets)
	}
}

func TestClientIPAggregatesIPv6Prefix(t *testing.T) {
	first := clientIP("[2001:db8:abcd:12::1]:1234")
	second := clientIP("[2001:db8:abcd:12:ffff::2]:4321")
	if first != second {
		t.Fatalf("IPv6 keys = %q and %q, want same /64", first, second)
	}
	if first != "2001:db8:abcd:12::/64" {
		t.Fatalf("IPv6 key = %q, want canonical /64", first)
	}
}

func TestUploadLimiterRejectsConcurrentMultipartBody(t *testing.T) {
	h := &Handlers{uploadLimiter: newUploadLimiter(1)}
	started := make(chan struct{})
	release := make(chan struct{})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	handler := h.boundHTTPResources(next)

	firstDone := make(chan int, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs", nil)
		req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		firstDone <- rec.Code
	}()
	<-started

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs", nil)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("concurrency rejection missing Retry-After")
	}
	close(release)
	if status := <-firstDone; status != http.StatusNoContent {
		t.Fatalf("first status = %d, want %d", status, http.StatusNoContent)
	}
}

func TestMultipartUploadRoutesIncludeWorkbench(t *testing.T) {
	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/api/jobs"},
		{method: http.MethodPost, path: "/api/stream-jobs"},
		{method: http.MethodPost, path: "/ui/jobs"},
		{method: http.MethodPut, path: "/api/voice-profiles/default"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
		if !isMultipartUpload(req) {
			t.Errorf("isMultipartUpload(%s %s) = false, want true", tt.method, tt.path)
		}
	}
}

func TestMediaResponsesRemainWriteDeadlineFree(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/api/stream-jobs/id/source", want: true},
		{path: "/api/jobs/id/renders/v/videos/reel.mp4", want: true},
		{path: "/api/stream-jobs/id/renders/v/delivery/clip.mp4", want: true},
		{path: "/api/jobs", want: false},
	}
	for _, tt := range tests {
		if got := isMediaResponse(http.MethodGet, tt.path); got != tt.want {
			t.Errorf("isMediaResponse(GET, %q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}
