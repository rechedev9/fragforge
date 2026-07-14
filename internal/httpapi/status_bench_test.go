package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
)

var benchmarkStatusResponseSize int

func BenchmarkGetJobFullStatusPayload(b *testing.B) {
	repo := newFakeRepo()
	j := benchmarkStatusJob()
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})
	router := chi.NewRouter()
	router.Get("/api/jobs/{id}", h.GetJob)
	request := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String(), nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			b.Fatalf("status = %d, want 200", response.Code)
		}
		benchmarkStatusResponseSize = response.Body.Len()
	}
}

func BenchmarkGetJobStatusMinimalPayload(b *testing.B) {
	repo := newFakeRepo()
	j := benchmarkStatusJob()
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})
	router := chi.NewRouter()
	router.Get("/api/jobs/{id}", h.GetJob)
	request := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"?view=status", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			b.Fatalf("status = %d, want 200", response.Code)
		}
		benchmarkStatusResponseSize = response.Body.Len()
	}
}

func benchmarkStatusJob() job.Job {
	plan := killplan.NewPlan()
	plan.Segments = make([]killplan.Segment, 240)
	for i := range plan.Segments {
		segment := &plan.Segments[i]
		segment.ID = killplan.FormatSegmentID(i + 1)
		segment.Round = i/8 + 1
		segment.TickStart = i * 640
		segment.TickEnd = segment.TickStart + 512
		segment.Kills = make([]killplan.Kill, 8)
		for k := range segment.Kills {
			segment.Kills[k] = killplan.Kill{
				Tick:     segment.TickStart + k*64,
				Weapon:   "ak47",
				Headshot: k%2 == 0,
				Victim: killplan.Player{
					SteamID64:  fmt.Sprintf("76561198%09d", i*8+k),
					NameInDemo: fmt.Sprintf("player-%04d", i*8+k),
					TeamAtKill: "CT",
				},
			}
		}
	}
	return job.Job{
		ID:       uuid.MustParse("81818181-8181-8181-8181-818181818181"),
		Status:   job.StatusParsed,
		KillPlan: &plan,
	}
}
