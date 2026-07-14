package httpapi

import (
	"context"
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

// fakeXAIKillfeedServer returns an httptest server that answers the xAI chat
// completions endpoint with a killfeed reply carrying the given kills JSON.
func fakeXAIKillfeedServer(t *testing.T, killsJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer xai_test" {
			t.Errorf("Authorization = %q, want Bearer xai_test", got)
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": killsJSON}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func killfeedReadHandlers(t *testing.T, xaiURL, ffmpegPath, xaiKey string) (*Handlers, uuid.UUID) {
	t.Helper()
	streamRepo := newFakeStreamRepo()
	id := uuid.New()
	crop := streamclips.CropRect{X: 0.7, Y: 0.05, Width: 0.28, Height: 0.2}
	plan := streamclips.DefaultEditPlan()
	plan.KillfeedCrop = &crop
	plan.Clips = []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 0, EndSeconds: 5}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON}

	opts := []Option{WithStreamRepository(streamRepo)}
	if ffmpegPath != "" {
		opts = append(opts, WithFFmpegPath(ffmpegPath))
	}
	if xaiKey != "" {
		opts = append(opts, WithXAIKey(xaiKey))
	}
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{}, opts...)
	h.killfeedVisionBaseURL = xaiURL
	h.killfeedFrame = func(context.Context, string, float64) (image.Image, error) {
		return image.NewRGBA(image.Rect(0, 0, 1920, 1080)), nil
	}
	return h, id
}

func TestReadStreamKillfeedReturnsParsedKills(t *testing.T) {
	weapon := streamclips.WeaponKeys()[0]
	killsJSON := `{"kills":[{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"villain","weapon":"` + weapon + `","headshot":true}]}`
	srv := fakeXAIKillfeedServer(t, killsJSON)

	h, id := killfeedReadHandlers(t, srv.URL, "ffmpeg", "xai_test")
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", strings.NewReader(`{"clip_id":"clip-1","cue_seconds":2}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	var body struct {
		Kills []streamclips.KillfeedKill `json:"kills"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rw.Body.String())
	}
	if len(body.Kills) != 1 {
		t.Fatalf("kills = %d, want 1; body=%s", len(body.Kills), rw.Body.String())
	}
	got := body.Kills[0]
	if got.AttackerName != "hero" || got.VictimName != "villain" || got.Weapon != weapon || !got.Headshot {
		t.Fatalf("kill = %#v, want the parsed hero/villain notice", got)
	}
}

func TestReadStreamKillfeedMissingXAIKeyReturns409Code(t *testing.T) {
	h, id := killfeedReadHandlers(t, "", "ffmpeg", "") // ffmpeg configured, no xAI key
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", strings.NewReader(`{"clip_id":"clip-1","cue_seconds":2}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "xai_key_missing" {
		t.Fatalf("code = %q, want xai_key_missing; body=%s", body.Code, rw.Body.String())
	}
}

func TestReadStreamKillfeedUpstreamFailureReturns502Code(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
	}))
	t.Cleanup(srv.Close)

	h, id := killfeedReadHandlers(t, srv.URL, "ffmpeg", "xai_test")
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", strings.NewReader(`{"clip_id":"clip-1","cue_seconds":2}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rw.Code, rw.Body.String())
	}
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "xai_request_failed" {
		t.Fatalf("code = %q, want xai_request_failed; body=%s", body.Code, rw.Body.String())
	}
	if !strings.Contains(body.Error, "rate limit exceeded") {
		t.Fatalf("error = %q, want the upstream message surfaced", body.Error)
	}
}

func TestReadStreamKillfeedMissingFFmpegReturns409(t *testing.T) {
	h, id := killfeedReadHandlers(t, "", "", "xai_test") // xAI configured, no ffmpeg
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", strings.NewReader(`{"clip_id":"clip-1","cue_seconds":2}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
}

func TestPreviewStreamKillfeedNoticeReturnsPNG(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})
	r := Routes(h)

	weapon := streamclips.WeaponKeys()[0]
	body := `{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"villain","weapon":"` + weapon + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/stream-killfeed/notice-preview", strings.NewReader(body))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if got := rw.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type = %q, want image/png", got)
	}
	if !strings.HasPrefix(rw.Body.String(), "\x89PNG\r\n\x1a\n") {
		t.Fatalf("body is not a PNG: % x", rw.Body.Bytes()[:min(8, rw.Body.Len())])
	}
}

func TestPreviewStreamKillfeedNoticeRejectsUnknownWeapon(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})
	r := Routes(h)

	body := `{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"villain","weapon":"not-a-real-weapon"}`
	req := httptest.NewRequest(http.MethodPost, "/api/stream-killfeed/notice-preview", strings.NewReader(body))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
}

func TestListStreamKillfeedWeapons(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})
	r := Routes(h)

	req := httptest.NewRequest(http.MethodGet, "/api/stream-killfeed/weapons", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	var body struct {
		Weapons []string `json:"weapons"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Weapons) == 0 {
		t.Fatalf("weapons = %v, want the non-empty catalog", body.Weapons)
	}
}
