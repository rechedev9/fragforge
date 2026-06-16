package httpapi

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
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
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

// newRateLimiter returns a limiter for the given rate. When rps <= 0 it returns
// nil, signaling a no-op pass-through.
func newRateLimiter(rps float64, burst int) *rateLimiter {
	if rps <= 0 {
		return nil
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

	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &tokenBucket{tokens: l.burst - 1, last: now}
		return true
	}
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
