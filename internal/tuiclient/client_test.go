package tuiclient

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(Config{BaseURL: srv.URL, Token: "secret", HTTPClient: srv.Client()})
}

func TestListJobsSendsTokenAndParses(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(tokenHeader); got != "secret" {
			t.Errorf("token header = %q, want secret", got)
		}
		if r.URL.Path != "/api/jobs" || r.URL.Query().Get("limit") != "10" {
			t.Errorf("unexpected request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jobs": []map[string]any{{"id": "abc", "status": "parsed", "target_steamid": "76561"}},
		})
	})
	jobs, err := c.ListJobs(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "abc" || jobs[0].Status != StatusParsed {
		t.Fatalf("unexpected jobs: %+v", jobs)
	}
}

func TestGetPlanNotReadyIsConflict(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "kill plan not ready"})
	})
	_, err := c.GetPlan(context.Background(), "id")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotReady(err) {
		t.Fatalf("IsNotReady = false for %v", err)
	}
	if StatusCode(err) != http.StatusConflict {
		t.Fatalf("StatusCode = %d", StatusCode(err))
	}
}

func TestCapabilitiesParsesXAIStreamBackend(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stream": map[string]any{"xai_enabled": true},
		})
	})

	got, err := c.Capabilities(context.Background())
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if !got.Stream.XAIEnabled {
		t.Fatal("Stream.XAIEnabled = false, want true")
	}
}

func TestGetRenderVariantNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	_, err := c.GetRenderVariant(context.Background(), "id", "viral-60-clean")
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false for %v", err)
	}
}

func TestStartRecordingSendsPresetAndSegments(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/jobs/j1/record" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Preset     string   `json:"preset"`
			SegmentIDs []string `json:"segment_ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Preset != "viral-60-clean" || len(body.SegmentIDs) != 2 {
			t.Errorf("unexpected body: %+v", body)
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "j1", "task": "record:demo"})
	})
	resp, err := c.StartRecording(context.Background(), "j1", "viral-60-clean", []string{"seg-001", "seg-004"})
	if err != nil {
		t.Fatalf("StartRecording: %v", err)
	}
	if resp.Task != "record:demo" {
		t.Fatalf("unexpected resp: %+v", resp)
	}
}

func TestCreateJobUploadsMultipart(t *testing.T) {
	dir := t.TempDir()
	demo := filepath.Join(dir, "match.dem")
	if err := os.WriteFile(demo, []byte("PBDEMS2fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" {
			t.Fatalf("content-type = %q (%v)", r.Header.Get("Content-Type"), err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		var sawDemo, sawConfig bool
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("next part: %v", err)
			}
			switch part.FormName() {
			case "demo":
				if part.FileName() != "match.dem" {
					t.Errorf("filename = %q", part.FileName())
				}
				sawDemo = true
			case "config":
				b, _ := io.ReadAll(part)
				if string(b) != `{"target_steamid":"76561198000000000"}` {
					t.Errorf("config = %q", b)
				}
				sawConfig = true
			}
		}
		if !sawDemo || !sawConfig {
			t.Errorf("missing parts: demo=%v config=%v", sawDemo, sawConfig)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "new", "status": "scanning"})
	})
	resp, err := c.CreateJob(context.Background(), demo, "76561198000000000")
	if err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if resp.ID != "new" {
		t.Fatalf("unexpected resp: %+v", resp)
	}
}

func TestNoTokenOmitsHeader(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header[tokenHeader]; ok {
			t.Errorf("token header present when unset")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jobs": []any{}})
	})
	c.token = "" // simulate a loopback orchestrator with no token
	if _, err := c.ListJobs(context.Background(), 0); err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
}
