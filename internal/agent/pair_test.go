package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPair(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/pair" {
			t.Errorf("got path %s", r.URL.Path)
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["code"] != "ABCD2345" {
			t.Errorf("got code %q", in["code"])
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": "tok", "agentId": "ag1"})
	}))
	defer srv.Close()

	token, id, err := Pair(context.Background(), srv.URL, "ABCD2345", "PC")
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if token != "tok" || id != "ag1" {
		t.Errorf("got (%q,%q), want (tok,ag1)", token, id)
	}
}
