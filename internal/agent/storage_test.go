package agent

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCloudStoragePut(t *testing.T) {
	var uploaded string
	blob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		uploaded = string(b)
		w.WriteHeader(200)
	}))
	defer blob.Close()

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"url":"` + blob.URL + `"}`))
	}))
	defer api.Close()

	s := NewCloudStorage(NewClient(api.URL, "tok"))
	if err := s.Put("jobs/x/roster.json", strings.NewReader(`{"players":[]}`)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if uploaded != `{"players":[]}` {
		t.Errorf("got upload %q", uploaded)
	}
}
