package httpapi

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	rateLimiterBucketTTL       = 10 * time.Minute
	rateLimiterMaxBuckets      = 4096
	rateLimiterCleanupInterval = time.Minute
	controlResponseTimeout     = 2 * time.Minute
)

// crossSiteGuard rejects browser-driven cross-site mutation requests. It is a
// defense-in-depth measure against CSRF: non-browser clients (curl, the Next.js
// proxy, server-to-server) send neither Sec-Fetch-Site nor Origin and are
// allowed through, since they cannot be CSRF'd.
func crossSiteGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMutationMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
			// Explicit allow-list: only the user's own same-origin requests and
			// direct user navigations ("none") may mutate. "same-site" (a sibling
			// or subdomain origin) and "cross-site" are rejected, so a same-site
			// attacker origin cannot drive a mutation on an exposed bind.
			if site != "same-origin" && site != "none" {
				writeError(w, http.StatusForbidden, "cross-site request blocked")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r.Host) {
			writeError(w, http.StatusForbidden, "cross-site request blocked")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originMatchesHost reports whether the Origin header's host matches the
// request Host. An unparseable Origin is treated as a mismatch.
func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Host == host
}

// rateLimiter is a per-client-IP token-bucket limiter. Buckets refill lazily on
// access using elapsed time, so there is no background goroutine to own or stop.
type rateLimiter struct {
	rps   float64
	burst float64

	mu      sync.Mutex
	buckets map[string]*tokenBucket
	lastGC  time.Time
}

type tokenBucket struct {
	tokens   float64
	last     time.Time
	lastSeen time.Time
}

// newRateLimiter returns a limiter for the given rate. When rps <= 0 it returns
// nil, signaling a no-op pass-through.
func newRateLimiter(rps float64, burst int) *rateLimiter {
	if rps <= 0 {
		return nil
	}
	if burst < 1 {
		burst = 1
	}
	return &rateLimiter{
		rps:     rps,
		burst:   float64(burst),
		buckets: map[string]*tokenBucket{},
	}
}

// allow reports whether a request from key may proceed, consuming one token.
func (l *rateLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.lastGC.IsZero() || now.Sub(l.lastGC) >= rateLimiterCleanupInterval || len(l.buckets) >= rateLimiterMaxBuckets {
		l.evict(now)
		l.lastGC = now
	}
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= rateLimiterMaxBuckets {
			l.evictOldest()
		}
		l.buckets[key] = &tokenBucket{tokens: l.burst - 1, last: now, lastSeen: now}
		return true
	}
	b.lastSeen = now
	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * l.rps
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *rateLimiter) evict(now time.Time) {
	for key, bucket := range l.buckets {
		if now.Sub(bucket.lastSeen) >= rateLimiterBucketTTL {
			delete(l.buckets, key)
		}
	}
}

func (l *rateLimiter) evictOldest() {
	var oldestKey string
	var oldest time.Time
	for key, bucket := range l.buckets {
		if oldestKey == "" || bucket.lastSeen.Before(oldest) {
			oldestKey = key
			oldest = bucket.lastSeen
		}
	}
	if oldestKey != "" {
		delete(l.buckets, oldestKey)
	}
}

// middleware returns an http middleware that throttles per client IP. A nil
// limiter is a pass-through.
func (l *rateLimiter) middleware(next http.Handler) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := clientIP(r.RemoteAddr)
		if !l.allow(key, time.Now()) {
			w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(l.rps)))
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the host portion of a RemoteAddr, falling back to the raw
// value when it has no port.
func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		// Aggregate IPv6 clients to /64 so rotating privacy addresses do not
		// create one permanent limiter bucket per request.
		masked := ip.Mask(net.CIDRMask(64, 128))
		return masked.String() + "/64"
	}
	return host
}

// retryAfterSeconds suggests how long a throttled client should wait before one
// token refills, with a one-second floor.
func retryAfterSeconds(rps float64) int {
	if rps <= 0 {
		return 1
	}
	secs := int(1.0/rps + 0.5)
	if secs < 1 {
		return 1
	}
	return secs
}

type uploadLimiter struct {
	slots chan struct{}
}

func newUploadLimiter(limit int) *uploadLimiter {
	if limit < 1 {
		return nil
	}
	return &uploadLimiter{slots: make(chan struct{}, limit)}
}

// boundHTTPResources applies a write deadline to ordinary control responses
// and limits multipart request bodies to a small number of simultaneous
// uploads. Media responses and multipart uploads intentionally have no write
// deadline: streams remain client-paced and a large local upload may take
// longer than a control response, while the server-wide read timeout still
// bounds every request body.
func (h *Handlers) boundHTTPResources(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		controller := http.NewResponseController(w)
		multipartUpload := isMultipartUpload(r)
		if !isMediaResponse(r.Method, r.URL.Path) && !multipartUpload {
			_ = controller.SetWriteDeadline(time.Now().Add(controlResponseTimeout))
			defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
		}

		if h.uploadLimiter == nil || !multipartUpload {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case h.uploadLimiter.slots <- struct{}{}:
			defer func() { <-h.uploadLimiter.slots }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "too many concurrent uploads")
		}
	})
}

func isMultipartUpload(r *http.Request) bool {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "multipart/") {
		return false
	}
	if r.Method == http.MethodPost &&
		(r.URL.Path == "/api/jobs" || r.URL.Path == "/api/stream-jobs" || r.URL.Path == "/ui/jobs") {
		return true
	}
	return r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/voice-profiles/")
}

func isMediaResponse(method, requestPath string) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	return strings.HasSuffix(requestPath, "/audio") ||
		strings.HasSuffix(requestPath, "/source") ||
		strings.Contains(requestPath, "/videos/") ||
		strings.Contains(requestPath, "/covers/") ||
		strings.Contains(requestPath, "/captions/") ||
		strings.Contains(requestPath, "/delivery/")
}
