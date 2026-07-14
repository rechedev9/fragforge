package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/storage"
)

func TestGetJobStatusOmitsKillPlanAndPreservesLifecycleFields(t *testing.T) {
	repo := newFakeRepo()
	id := uuid.New()
	plan := killplan.NewPlan()
	plan.Segments = []killplan.Segment{{ID: "seg-001"}}
	repo.jobs[id] = job.Job{
		ID:            id,
		Status:        job.StatusFailed,
		FailureReason: "capture failed",
		KillPlan:      &plan,
	}

	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})
	router := chi.NewRouter()
	router.Get("/api/jobs/{id}", h.GetJob)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/jobs/"+id.String()+"?view=status", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "kill_plan") || strings.Contains(response.Body.String(), "seg-001") {
		t.Fatalf("status response contains kill plan: %s", response.Body.String())
	}
	var got jobStatusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if got.Status != job.StatusFailed || got.FailureReason != "capture failed" {
		t.Fatalf("status response = %+v, want failed/capture failed", got)
	}
}

func TestGetJobStatusReportsCaptureSelectionProgressWithoutKillPlan(t *testing.T) {
	repo := newFakeRepo()
	id := uuid.New()
	repo.jobs[id] = job.Job{ID: id, Status: job.StatusRecording, KillPlan: segmentPlan(4)}
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	writeCaptureSelection(t, store, id, []string{"s2", "s3"})
	writeSegmentClips(t, store, id, "s1", "s2")

	h := NewHandlers(repo, store, &fakeQueue{})
	router := chi.NewRouter()
	router.Get("/api/jobs/{id}", h.GetJob)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/jobs/"+id.String()+"?view=status", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	var got jobStatusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if got.Progress == nil || got.Progress.Done != 1 || got.Progress.Total != 2 {
		t.Fatalf("progress = %+v, want 1/2", got.Progress)
	}
}
