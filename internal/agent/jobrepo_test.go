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
