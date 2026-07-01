package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLoopClaimsProcessesCompletes(t *testing.T) {
	demoID := uuid.New()
	var served atomic.Bool
	var completed atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/agent/jobs/claim":
			if served.Swap(true) {
				w.WriteHeader(204)
				return
			}
			_, _ = w.Write([]byte(`{"job":{"id":"` + demoID.String() + `"},"jobType":"scan"}`))
		case r.URL.Path == "/api/agent/jobs/"+demoID.String()+"/complete":
			completed.Store(true)
			w.WriteHeader(204)
		default:
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	var gotType string
	proc := func(ctx context.Context, jobType string, id uuid.UUID) error {
		gotType = jobType
		return nil
	}
	_ = loop(ctx, NewClient(srv.URL, "tok"), proc, 10*time.Millisecond)

	if gotType != "scan" {
		t.Errorf("got jobType %q, want scan", gotType)
	}
	if !completed.Load() {
		t.Errorf("job was not completed")
	}
}
