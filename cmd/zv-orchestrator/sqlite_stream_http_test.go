package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rechedev9/fragforge/internal/httpapi"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

// TestPostStreamJobsUnderSQLiteMode is the regression test for the desktop
// bug: FragForge Studio runs the orchestrator with ZV_DATABASE_URL=sqlite,
// and main.go's sqlite branch left streamRepo nil (only the memory and
// postgres branches assigned it), so POST /api/stream-jobs 500'd for every
// desktop user. This assembles the same building blocks as that sqlite
// branch -- a sqlite job repository plus a sqlite stream job repository
// sharing its *sql.DB -- and drives the real HTTP handler the way the
// desktop UI does: a multipart upload with a "video" field.
func TestPostStreamJobsUnderSQLiteMode(t *testing.T) {
	dataDir := t.TempDir()
	store, err := storage.NewLocal(dataDir)
	if err != nil {
		t.Fatalf("storage.NewLocal: %v", err)
	}

	jobRepo, err := newSQLiteJobRepository(filepath.Join(dataDir, "jobs.db"))
	if err != nil {
		t.Fatalf("newSQLiteJobRepository: %v", err)
	}
	defer func() { _ = jobRepo.Close() }()

	streamRepo, err := newSQLiteStreamJobRepository(jobRepo.db)
	if err != nil {
		t.Fatalf("newSQLiteStreamJobRepository: %v", err)
	}

	queue := newInlineQueue(map[string]taskHandler{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue.Start(ctx)

	handlers := httpapi.NewHandlers(jobRepo, store, queue, httpapi.WithStreamRepository(streamRepo))
	srv := httptest.NewServer(httpapi.Routes(handlers))
	defer srv.Close()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, err := mw.CreateFormFile("video", "source.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("fake-mp4-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("config", `{"title":"sqlite desktop upload"}`); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/stream-jobs", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create stream job status = %d, want 201; body = %s", resp.StatusCode, respBody)
	}

	var created struct {
		ID     string             `json:"id"`
		Status streamclips.Status `json:"status"`
	}
	if err := json.Unmarshal(respBody, &created); err != nil {
		t.Fatalf("decode create response: %v\nbody = %s", err, respBody)
	}
	if created.Status != streamclips.StatusUploaded {
		t.Fatalf("status = %s, want uploaded", created.Status)
	}

	// GET must also work end to end under sqlite mode.
	getResp, err := srv.Client().Get(srv.URL + "/api/stream-jobs/" + created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	getBody, _ := io.ReadAll(getResp.Body)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get stream job status = %d, want 200; body = %s", getResp.StatusCode, getBody)
	}
}
