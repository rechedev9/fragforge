package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHeartbeat(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := Heartbeat(context.Background(), NewClient(srv.URL, "tok"), map[string]any{"parser": true}, "lbtok", 8090); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("got auth %q, want Bearer tok", gotAuth)
	}
	if gotBody["loopbackToken"] != "lbtok" {
		t.Errorf("got loopbackToken %v, want lbtok", gotBody["loopbackToken"])
	}
	if port, _ := gotBody["loopbackPort"].(float64); int(port) != 8090 {
		t.Errorf("got loopbackPort %v, want 8090", gotBody["loopbackPort"])
	}
}
