package main

import "testing"

func TestListenGuard(t *testing.T) {
	tests := []struct {
		name        string
		addr        string
		allowPublic bool
		wantErr     bool
	}{
		{name: "loopback ipv4", addr: "127.0.0.1:8090", allowPublic: false, wantErr: false},
		{name: "localhost host", addr: "localhost:8090", allowPublic: false, wantErr: false},
		{name: "loopback ipv6", addr: "[::1]:8090", allowPublic: false, wantErr: false},
		{name: "all interfaces allowed by flag", addr: "0.0.0.0:8090", allowPublic: true, wantErr: false},
		{name: "routable ip allowed by flag", addr: "192.168.1.10:8090", allowPublic: true, wantErr: false},
		{name: "all interfaces blocked", addr: "0.0.0.0:8090", allowPublic: false, wantErr: true},
		{name: "routable ip blocked", addr: "192.168.1.10:8090", allowPublic: false, wantErr: true},
		{name: "empty host binds all blocked", addr: ":8090", allowPublic: false, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := listenGuard(tt.addr, tt.allowPublic)
			if got := err != nil; got != tt.wantErr {
				t.Fatalf("listenGuard(%q, %v) error = %v, want error = %v", tt.addr, tt.allowPublic, err, tt.wantErr)
			}
		})
	}
}
