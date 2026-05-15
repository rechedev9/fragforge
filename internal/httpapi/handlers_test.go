package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/reche/zackvideo/internal/artifacts"
	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
	"github.com/reche/zackvideo/internal/tasks"
)

// fakeRepo implements JobRepository for tests.
type fakeRepo struct {
	jobs map[uuid.UUID]job.Job
}

func newFakeRepo() *fakeRepo { return &fakeRepo{jobs: map[uuid.UUID]job.Job{}} }
func (f *fakeRepo) Create(_ context.Context, j *job.Job) error {
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	f.jobs[j.ID] = *j
	return nil
}
func (f *fakeRepo) Get(_ context.Context, id uuid.UUID) (job.Job, error) {
	j, ok := f.jobs[id]
	if !ok {
		return job.Job{}, job.ErrNotFound
	}
	return j, nil
}

// fakeStorage records every Put call.
type fakeStorage struct {
	puts map[string][]byte
}

func newFakeStorage() *fakeStorage { return &fakeStorage{puts: map[string][]byte{}} }
func (f *fakeStorage) Put(key string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.puts[key] = b
	return nil
}
func (f *fakeStorage) Open(key string) (io.ReadCloser, error) {
	b, ok := f.puts[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// fakeQueue captures enqueued tasks.
type fakeQueue struct {
	enqueued []*asynq.Task
}

func (q *fakeQueue) Enqueue(t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.enqueued = append(q.enqueued, t)
	return &asynq.TaskInfo{ID: "x"}, nil
}

func multipartBody(t *testing.T, demoBytes []byte, configJSON string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	demoPart, _ := mw.CreateFormFile("demo", "test.dem")
	demoPart.Write(demoBytes)
	mw.WriteField("config", configJSON)
	mw.Close()
	return body, mw.FormDataContentType()
}

func TestPostJobsCreatesJobAndEnqueues(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	h := NewHandlers(repo, store, queue)

	body, ct := multipartBody(t, []byte("dem-bytes"), `{"target_steamid":"76561198000000000"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", ct)
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rw.Code, rw.Body.String())
	}
	var resp struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rw.Body.Bytes(), &resp)
	if resp.Status != "queued" {
		t.Errorf("status = %q, want queued", resp.Status)
	}
	if len(repo.jobs) != 1 {
		t.Errorf("repo has %d jobs, want 1", len(repo.jobs))
	}
	if len(store.puts) != 1 {
		t.Errorf("storage has %d puts, want 1", len(store.puts))
	}
	if len(queue.enqueued) != 1 {
		t.Errorf("queue has %d tasks, want 1", len(queue.enqueued))
	}
}

func TestPostJobsRejectsMissingDemo(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	mw.WriteField("config", `{"target_steamid":"76561198000000000"}`)
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestPostJobsRejectsInvalidSteamID(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})

	body, ct := multipartBody(t, []byte("x"), `{"target_steamid":"not-a-number"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", ct)
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestGetJobReturnsJob(t *testing.T) {
	repo := newFakeRepo()
	j := job.Job{
		ID:            uuid.New(),
		Status:        job.StatusQueued,
		DemoPath:      "demos/x.dem",
		DemoSHA256:    "abc",
		TargetSteamID: "76561198000000000",
		Rules:         rules.Default(),
	}
	repo.jobs[j.ID] = j

	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}", h.GetJob)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String(), nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}
	var got struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(rw.Body.Bytes(), &got)
	if got.ID != j.ID.String() {
		t.Errorf("id = %q, want %q", got.ID, j.ID.String())
	}
	if got.Status != "queued" {
		t.Errorf("status = %q, want queued", got.Status)
	}
}

func TestGetJobReturns404WhenMissing(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})
	r := chi.NewRouter()
	r.Get("/api/jobs/{id}", h.GetJob)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+uuid.New().String(), nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rw.Code)
	}
}

func TestGetJobReturns400OnInvalidUUID(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})
	r := chi.NewRouter()
	r.Get("/api/jobs/{id}", h.GetJob)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/not-a-uuid", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rw.Code)
	}
}

func TestGetPlanReturns409WhenJobNotParsed(t *testing.T) {
	repo := newFakeRepo()
	j := job.Job{ID: uuid.New(), Status: job.StatusQueued, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/plan", h.GetPlan)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/plan", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (not yet ready)", rw.Code)
	}
}

func TestGetPlanReturnsPlanWhenReady(t *testing.T) {
	repo := newFakeRepo()
	plan := killplan.NewPlan()
	plan.Demo.Map = "de_inferno"
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/plan", h.GetPlan)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/plan", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}
	if !strings.Contains(rw.Body.String(), "de_inferno") {
		t.Errorf("body does not include map: %s", rw.Body.String())
	}
}

func TestStartRecordingEnqueuesRecordTaskWhenParsed(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/record", h.StartRecording)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/record", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(queue.enqueued))
	}
	if queue.enqueued[0].Type() != tasks.TypeRecordDemo {
		t.Fatalf("task type = %q, want %q", queue.enqueued[0].Type(), tasks.TypeRecordDemo)
	}
}

func TestStartRecordingRejectsJobWithoutPlan(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/record", h.StartRecording)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/record", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rw.Code)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
}

func TestStartCompositionEnqueuesComposeTaskWhenRecorded(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/compose", h.StartComposition)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/compose", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(queue.enqueued))
	}
	if queue.enqueued[0].Type() != tasks.TypeComposeFinal {
		t.Fatalf("task type = %q, want %q", queue.enqueued[0].Type(), tasks.TypeComposeFinal)
	}
}

func TestStartCompositionRejectsWrongStatus(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/compose", h.StartComposition)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/compose", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rw.Code)
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
}

func TestGetFinalStreamsFinalArtifactWhenComposed(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusComposed, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	_ = store.Put(artifacts.FinalMP4Key(j.ID), bytes.NewReader([]byte("mp4-bytes")))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/final", h.GetFinal)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/final", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if got := rw.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("Content-Type = %q, want video/mp4", got)
	}
	if rw.Body.String() != "mp4-bytes" {
		t.Fatalf("body = %q, want mp4-bytes", rw.Body.String())
	}
}

func TestGetFinalReturns409BeforeComposed(t *testing.T) {
	repo := newFakeRepo()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/final", h.GetFinal)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/final", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rw.Code)
	}
}

func TestGetFinalReturns404WhenArtifactMissing(t *testing.T) {
	repo := newFakeRepo()
	j := job.Job{ID: uuid.New(), Status: job.StatusComposed, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/final", h.GetFinal)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/final", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rw.Code)
	}
}
