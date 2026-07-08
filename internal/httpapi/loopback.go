package httpapi

import "net"

// IsLoopbackAddr reports whether addr ("host:port") binds a loopback host:
// "localhost" or an IP whose IsLoopback is true. A non-loopback host or an
// addr that does not parse as host:port is false. It is the single predicate
// shared by the orchestrator (which gates the mutation token on a non-loopback
// bind) and the agent (which refuses to front its data-plane proxy on a
// non-loopback address).
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
