package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHeartbeat(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := Heartbeat(context.Background(), NewClient(srv.URL, "tok"), map[string]any{"parser": true}); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if gotAuth != "Bearer tok" {
		t.Errorf("got auth %q, want Bearer tok", gotAuth)
	}
}
