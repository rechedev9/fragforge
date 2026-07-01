package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/rechedev9/fragforge/internal/job"
)

func decodeJSON(r *http.Request, out any) error {
	return json.NewDecoder(r.Body).Decode(out)
}

func TestCloudJobRepoUpdateStatus(t *testing.T) {
	id := uuid.New()
	var gotPath string
	var gotStatus float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var in map[string]any
		_ = decodeJSON(r, &in)
		gotStatus, _ = in["status"].(float64)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	repo := NewCloudJobRepo(NewClient(srv.URL, "tok"))
	if err := repo.UpdateStatus(context.Background(), id, job.StatusScanning, ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if gotPath != "/api/agent/jobs/"+id.String()+"/status" {
		t.Errorf("got path %s", gotPath)
	}
	if int(gotStatus) != int(job.StatusScanning) {
		t.Errorf("got status %v, want %v", gotStatus, int(job.StatusScanning))
	}
}

// TestCloudJobRepoGetMetaDecodesDTO locks the wire shape the cloud control-plane
// sends back from GET /api/agent/jobs/<id>: status must be the canonical string
// name ("queued"), not the wire int, or job.Job's UnmarshalJSON fails and every
// claimed job immediately gets /fail'd before demo_path is ever read.
func TestCloudJobRepoGetMetaDecodesDTO(t *testing.T) {
	id := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + id.String() + `","status":"queued","demo_path":"demos/u/d.dem","demo_sha256":"ab","target_steamid":"","rules":{}}`))
	}))
	defer srv.Close()

	repo := NewCloudJobRepo(NewClient(srv.URL, "tok"))
	got, err := repo.GetMeta(context.Background(), id)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if got.DemoPath != "demos/u/d.dem" {
		t.Errorf("got DemoPath %q, want %q", got.DemoPath, "demos/u/d.dem")
	}
	if got.Status != job.StatusQueued {
		t.Errorf("got Status %v, want %v", got.Status, job.StatusQueued)
	}
}
