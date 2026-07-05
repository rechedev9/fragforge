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
	"time"
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

// TestDefaultClientHasNoWholeExchangeTimeout pins that the nil-HTTPClient
// default carries connection-phase bounds but no Client.Timeout, so a large
// body transfer is never capped mid-flight. It also checks that an injected
// client is used verbatim.
func TestDefaultClientHasNoWholeExchangeTimeout(t *testing.T) {
	c := New(Config{BaseURL: "http://example.invalid"})
	if c.hc.Timeout != 0 {
		t.Fatalf("default client Timeout = %v, want 0 (no whole-exchange cap)", c.hc.Timeout)
	}
	tr, ok := c.hc.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("default transport = %T, want *http.Transport", c.hc.Transport)
	}
	if tr.ResponseHeaderTimeout != defaultResponseHeaderTimeout {
		t.Fatalf("ResponseHeaderTimeout = %v, want %v", tr.ResponseHeaderTimeout, defaultResponseHeaderTimeout)
	}
	if tr.DialContext == nil {
		t.Fatal("DialContext is nil, want a dialer with a connect timeout")
	}

	custom := &http.Client{}
	c2 := New(Config{BaseURL: "http://example.invalid", HTTPClient: custom})
	if c2.hc != custom {
		t.Fatal("injected HTTPClient was not used verbatim")
	}
}

// TestDownloadFinalHonorsContextDeadlineWhenStalled exercises the real default
// client (not srv.Client()) against a server that flushes a few bytes then
// stalls. With no Client.Timeout, only the caller context bounds the transfer;
// the call must fail promptly when that context expires.
func TestDownloadFinalHonorsContextDeadlineWhenStalled(t *testing.T) {
	// srvDone is closed by the deferred call when the test returns, before the
	// srv.Close cleanup runs, so the handler always unblocks and Close never
	// hangs, even if Go does not detect the client disconnect on its own.
	srvDone := make(chan struct{})
	defer close(srvDone)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PARTIAL"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Stall until the client cancels (or the test ends).
		select {
		case <-r.Context().Done():
		case <-srvDone:
		}
	}))
	t.Cleanup(srv.Close)

	c := New(Config{BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := c.DownloadFinal(ctx, "id", io.Discard)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected an error when the transfer stalls past the context deadline")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("stalled download took %v to fail, want prompt cancellation", elapsed)
	}
}

// TestCreateJobHonorsContextDeadlineWhenServerStalls does the same for the
// upload path: the server never reads the body to completion, so only the
// caller context can end the exchange. This also drives the uploadMultipart
// pipe-writer goroutine cleanup under cancellation.
func TestCreateJobHonorsContextDeadlineWhenServerStalls(t *testing.T) {
	dir := t.TempDir()
	demo := filepath.Join(dir, "match.dem")
	if err := os.WriteFile(demo, []byte("PBDEMS2fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	// srvDone unblocks the handler when the test returns (deferred, so it runs
	// before the srv.Close cleanup). On the upload path the server never reads
	// the body, so it may not detect the client disconnect and cancel
	// r.Context(); without this, srv.Close would hang.
	srvDone := make(chan struct{})
	defer close(srvDone)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Do not read the body to completion; stall until the client cancels
		// (or the test ends).
		select {
		case <-r.Context().Done():
		case <-srvDone:
		}
	}))
	t.Cleanup(srv.Close)

	c := New(Config{BaseURL: srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.CreateJob(ctx, demo, "")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected an error when the upload stalls past the context deadline")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("stalled upload took %v to fail, want prompt cancellation", elapsed)
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
