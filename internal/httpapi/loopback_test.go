package httpapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsLoopbackAddr(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{"loopback ipv4 default", "127.0.0.1:8090", true},
		{"loopback ipv4 dynamic", "127.0.0.1:0", true},
		{"loopback ipv4 range", "127.0.0.5:8080", true},
		{"localhost", "localhost:8080", true},
		{"loopback ipv6", "[::1]:8080", true},
		{"non-loopback ipv4", "0.0.0.0:8080", false},
		{"lan ip", "192.168.1.5:8080", false},
		{"public ip", "8.8.8.8:80", false},
		{"missing port", "127.0.0.1", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLoopbackAddr(tt.addr); got != tt.want {
				t.Errorf("IsLoopbackAddr(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestRequireAuthorityRejectsDNSRebindingHost(t *testing.T) {
	listener := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := RequireAuthority(listener, next)

	tests := []struct {
		name       string
		host       string
		wantStatus int
	}{
		{name: "actual authority", host: "127.0.0.1:8080", wantStatus: http.StatusNoContent},
		{name: "explicit localhost", host: "localhost:8080", wantStatus: http.StatusNoContent},
		{name: "other loopback literal", host: "127.0.0.2:8080", wantStatus: http.StatusNoContent},
		{name: "attacker DNS name", host: "attacker.example:8080", wantStatus: http.StatusMisdirectedRequest},
		{name: "wrong port", host: "127.0.0.1:9090", wantStatus: http.StatusMisdirectedRequest},
		{name: "missing port", host: "127.0.0.1", wantStatus: http.StatusMisdirectedRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/api/jobs", nil)
			req.Host = tt.host
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestRequireAuthorityUsesActualDynamicListenerPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	handler := RequireAuthority(listener.Addr(), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = listener.Addr().String()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
}
