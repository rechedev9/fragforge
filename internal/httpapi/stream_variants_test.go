package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
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
	if len(body.Variants) != 3 {
		t.Fatalf("variants: got %d want 3", len(body.Variants))
	}
	if got := body.Variants[0]; got.Name != body.Default || !got.Default {
		t.Fatalf("first variant: got %+v default %q", got, body.Default)
	}
	if got := body.Variants[2]; got.Name != "streamer-fullframe-nocam" || !got.FullFrame || got.GameOutputHeight != 1920 {
		t.Fatalf("full-frame variant: got %+v", got)
	}
}
