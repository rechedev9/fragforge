package httpapi

import "testing"

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
