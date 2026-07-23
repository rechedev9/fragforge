package httpapi

import (
	"net"
	"net/http"
	"strings"
)

// IsLoopbackAddr reports whether addr ("host:port") binds a loopback host:
// "localhost" or an IP whose IsLoopback is true. A non-loopback host or an
// addr that does not parse as host:port is false. It is the single predicate
// shared by the orchestrator and agent, both of which reject remote cleartext
// authorities even when a bearer capability is configured.
func IsLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// RequireAuthority rejects Host values that do not identify the listener.
// Loopback listeners accept localhost and explicit loopback IP literals on the
// actual listener port. A DNS name is never accepted, even if it currently
// resolves to loopback, because that would reintroduce DNS-rebinding trust.
func RequireAuthority(listener net.Addr, next http.Handler) http.Handler {
	listenerHost, listenerPort, err := net.SplitHostPort(listener.String())
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, http.StatusServiceUnavailable, "listener authority is invalid")
		})
	}
	listenerIP := net.ParseIP(strings.TrimSpace(listenerHost))
	listenerLoopback := listenerIP != nil && listenerIP.IsLoopback()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, port, splitErr := net.SplitHostPort(r.Host)
		if splitErr != nil || port != listenerPort || !authorityHostAllowed(host, listenerHost, listenerLoopback) {
			writeError(w, http.StatusMisdirectedRequest, "request authority does not match listener")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func authorityHostAllowed(host, listenerHost string, listenerLoopback bool) bool {
	host = strings.TrimSpace(host)
	if !listenerLoopback {
		return strings.EqualFold(host, listenerHost)
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
