package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/obs"
)

func TestHealthHandler(t *testing.T) {
	h := &Handlers{}
	rr := httptest.NewRecorder()
	h.Health(rr, httptest.NewRequest("GET", "/healthz", nil))
	if rr.Code != 200 {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"status":"ok"`) {
		t.Errorf("body: got %q", rr.Body.String())
	}
}

func TestMetricsHandlerServesCounters(t *testing.T) {
	t.Setenv("ZV_DATA_DIR", t.TempDir())
	rec, err := obs.New(obs.DefaultDir())
	if err != nil {
		t.Fatalf("obs.New: %v", err)
	}
	if err := rec.RecordError(obs.Event{Stage: obs.StageHTTP, Class: "boom", Message: "x"}); err != nil {
		t.Fatalf("RecordError: %v", err)
	}

	h := &Handlers{}
	rr := httptest.NewRecorder()
	h.Metrics(rr, httptest.NewRequest("GET", "/metrics", nil))
	if rr.Code != 200 {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `fragforge_errors_total{class="boom",stage="http"} 1`) {
		t.Errorf("metrics body missing seeded counter:\n%s", body)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type: got %q", ct)
	}
}
