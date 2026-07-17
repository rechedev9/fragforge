package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

func TestListStreamVariantsUsesRegistryOrderAndDefault(t *testing.T) {
	h := &Handlers{}
	rr := httptest.NewRecorder()
	h.ListStreamVariants(rr, httptest.NewRequest("GET", "/api/stream-variants", nil))
	if rr.Code != 200 {
		t.Fatalf("status: got %d want 200", rr.Code)
	}
	var body struct {
		Default  string                 `json:"default"`
		Variants []streamVariantSummary `json:"variants"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantNames := streamclips.VariantNames()
	if len(body.Variants) != len(wantNames) {
		t.Fatalf("variants: got %d want %d", len(body.Variants), len(wantNames))
	}
	if got := body.Variants[0]; got.Name != body.Default || !got.Default {
		t.Fatalf("first variant: got %+v default %q", got, body.Default)
	}
	if got := body.Variants[2]; got.Name != "streamer-fullframe-nocam" || !got.FullFrame || got.GameOutputHeight != 1920 {
		t.Fatalf("full-frame variant: got %+v", got)
	}
	if got := body.Variants[3]; got.Name != streamclips.VariantStreamerLandscape16x9 || !got.FullFrame || got.OutputWidth != 1920 || got.GameOutputHeight != 1080 {
		t.Fatalf("landscape variant: got %+v", got)
	}
}
