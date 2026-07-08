package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPair(t *testing.T) {
	var in map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/pair" {
			t.Errorf("got path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["code"] != "ABCD2345" {
			t.Errorf("got code %q", in["code"])
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok", "agentId": "ag1"})
	}))
	defer srv.Close()

	token, id, err := Pair(context.Background(), srv.URL, "ABCD2345", "PC", "lbtok", 8090)
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if token != "tok" || id != "ag1" {
		t.Errorf("got (%q,%q), want (tok,ag1)", token, id)
	}
	if in["loopbackToken"] != "lbtok" {
		t.Errorf("got loopbackToken %v, want lbtok", in["loopbackToken"])
	}
	if port, _ := in["loopbackPort"].(float64); int(port) != 8090 {
		t.Errorf("got loopbackPort %v, want 8090", in["loopbackPort"])
	}
}

func TestGenerateLoopbackToken(t *testing.T) {
	a, err := GenerateLoopbackToken()
	if err != nil {
		t.Fatalf("GenerateLoopbackToken: %v", err)
	}
	b, err := GenerateLoopbackToken()
	if err != nil {
		t.Fatalf("GenerateLoopbackToken: %v", err)
	}
	if a == b {
		t.Errorf("two tokens are equal, want distinct")
	}
	// 32 raw bytes base64url (no padding) decode back to 32 bytes.
	if decoded, err := base64.RawURLEncoding.DecodeString(a); err != nil || len(decoded) != 32 {
		t.Errorf("token %q decoded to %d bytes (err %v), want 32", a, len(decoded), err)
	}
}
