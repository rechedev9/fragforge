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
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/reche/zackvideo/internal/artifacts"
	"github.com/reche/zackvideo/internal/editor"
	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/recording"
	"github.com/reche/zackvideo/internal/renderplan"
	"github.com/reche/zackvideo/internal/rules"
	"github.com/reche/zackvideo/internal/storage"
	"github.com/reche/zackvideo/internal/streamclips"
	"github.com/reche/zackvideo/internal/tasks"
)

// fakeRepo implements JobRepository for tests.
type fakeRepo struct {
	jobs            map[uuid.UUID]job.Job
	getErr          error
	updateHonorsCtx bool
}

type fakeStreamRepo struct {
	jobs map[uuid.UUID]streamclips.Job
}

func newFakeStreamRepo() *fakeStreamRepo {
	return &fakeStreamRepo{jobs: map[uuid.UUID]streamclips.Job{}}
}

func (f *fakeStreamRepo) Create(_ context.Context, j *streamclips.Job) error {
	f.jobs[j.ID] = *j
	return nil
}

func (f *fakeStreamRepo) Get(_ context.Context, id uuid.UUID) (streamclips.Job, error) {
	j, ok := f.jobs[id]
	if !ok {
		return streamclips.Job{}, streamclips.ErrNotFound
	}
	return j, nil
}

func (f *fakeStreamRepo) List(_ context.Context, limit int) ([]streamclips.Job, error) {
	jobs := make([]streamclips.Job, 0, len(f.jobs))
	for _, j := range f.jobs {
		jobs = append(jobs, j)
		if len(jobs) == limit {
			break
		}
	}
	return jobs, nil
}

func (f *fakeStreamRepo) UpdateStatus(_ context.Context, id uuid.UUID, s streamclips.Status, reason string) error {
	j, ok := f.jobs[id]
	if !ok {
		return streamclips.ErrNotFound
	}
	j.Status = s
	j.FailureReason = reason
	f.jobs[id] = j
	return nil
}

func (f *fakeStreamRepo) SetEditPlan(_ context.Context, id uuid.UUID, plan streamclips.EditPlan) error {
	j, ok := f.jobs[id]
	if !ok {
		return streamclips.ErrNotFound
	}
	b, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	j.EditPlan = b
	j.Status = streamclips.StatusReady
	f.jobs[id] = j
	return nil
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
	if f.getErr != nil {
		return job.Job{}, f.getErr
	}
	j, ok := f.jobs[id]
	if !ok {
		return job.Job{}, job.ErrNotFound
	}
	return j, nil
}
func (f *fakeRepo) List(_ context.Context, limit int) ([]job.Job, error) {
	jobs := make([]job.Job, 0, len(f.jobs))
	for _, j := range f.jobs {
		j.KillPlan = nil
		jobs = append(jobs, j)
		if len(jobs) == limit {
			break
		}
	}
	return jobs, nil
}
func (f *fakeRepo) UpdateStatus(ctx context.Context, id uuid.UUID, s job.Status, reason string) error {
	if f.updateHonorsCtx {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	j, ok := f.jobs[id]
	if !ok {
		return job.ErrNotFound
	}
	j.Status = s
	j.FailureReason = reason
	f.jobs[id] = j
	return nil
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
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (f *fakeStorage) Exists(key string) (bool, error) {
	_, ok := f.puts[key]
	return ok, nil
}

// fakeQueue captures enqueued tasks.
type fakeQueue struct {
	enqueued []*asynq.Task
	options  [][]asynq.Option
	err      error
}

func (q *fakeQueue) Enqueue(t *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if q.err != nil {
		return nil, q.err
	}
	q.enqueued = append(q.enqueued, t)
	q.options = append(q.options, opts)
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

func TestListJobsReturnsRecentJobsWithoutKillPlan(t *testing.T) {
	repo := newFakeRepo()
	id := uuid.New()
	plan := killplan.NewPlan()
	repo.jobs[id] = job.Job{ID: id, Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs", h.ListJobs)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs?limit=10", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), id.String()) {
		t.Fatalf("body missing job id: %s", rw.Body.String())
	}
	if strings.Contains(rw.Body.String(), "kill_plan") {
		t.Fatalf("list response should not include kill_plan: %s", rw.Body.String())
	}
}

func TestListLoadoutsReturnsCatalog(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/loadouts", h.ListLoadouts)
	req := httptest.NewRequest(http.MethodGet, "/api/loadouts", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), editor.PresetShortNaturalHQ2Full) {
		t.Fatalf("body missing loadout: %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), editor.PresetViral60) {
		t.Fatalf("body missing default loadout: %s", rw.Body.String())
	}
}

func TestListPresetsReturnsRegistry(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/presets", h.ListPresets)
	req := httptest.NewRequest(http.MethodGet, "/api/presets", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	var resp struct {
		Default string `json:"default"`
		Presets []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Default     bool   `json:"default"`
			FPS         int    `json:"fps"`
			Width       int    `json:"width"`
			Height      int    `json:"height"`
		} `json:"presets"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Default != editor.PresetViral60 {
		t.Fatalf("default = %q, want %q", resp.Default, editor.PresetViral60)
	}
	if got, want := len(resp.Presets), len(editor.PresetNames()); got != want {
		t.Fatalf("presets = %d, want %d", got, want)
	}
	first := resp.Presets[0]
	if first.Name != editor.PresetViral60 || !first.Default || first.Description == "" {
		t.Fatalf("first preset = %#v, want default %s", first, editor.PresetViral60)
	}
	if first.FPS != 60 || first.Width != 1080 || first.Height != 1920 {
		t.Fatalf("first preset geometry = %#v, want 1080x1920@60", first)
	}
}

func TestWorkbenchServesLocalApp(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/", h.Workbench)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rw.Code)
	}
	body := rw.Body.String()
	for _, want := range []string{"ZackVideo Workbench", "Mutation token", "workbench-shell", "APPROVE_RECORDING", "/api/jobs", "/api/loadouts", "/agent/captions"} {
		if !strings.Contains(body, want) {
			t.Fatalf("workbench missing %q", want)
		}
	}
}

func TestPostJobsMarksJobFailedWhenEnqueueFails(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{err: errors.New("redis down")}
	h := NewHandlers(repo, store, queue)

	body, ct := multipartBody(t, []byte("dem-bytes"), `{"target_steamid":"76561198000000000"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", ct)
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rw.Code, rw.Body.String())
	}
	if len(repo.jobs) != 1 {
		t.Fatalf("repo jobs = %d, want 1", len(repo.jobs))
	}
	for _, j := range repo.jobs {
		if j.Status != job.StatusFailed {
			t.Fatalf("job status = %s, want failed (must not be stranded in queued with no task)", j.Status)
		}
	}
}

func TestPostJobsFailedWriteSurvivesCancelledRequestContext(t *testing.T) {
	repo := newFakeRepo()
	repo.updateHonorsCtx = true // mimic pgxpool: refuse a cancelled context
	store := newFakeStorage()
	queue := &fakeQueue{err: errors.New("redis down")}
	h := NewHandlers(repo, store, queue)

	body, ct := multipartBody(t, []byte("dem-bytes"), `{"target_steamid":"76561198000000000"}`)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body).WithContext(ctx)
	req.Header.Set("Content-Type", ct)
	cancel() // client disconnect / proxy deadline before the handler finishes
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)

	if len(repo.jobs) != 1 {
		t.Fatalf("repo jobs = %d, want 1", len(repo.jobs))
	}
	for _, j := range repo.jobs {
		if j.Status != job.StatusFailed {
			t.Fatalf("job status = %s, want failed (compensating write must survive a cancelled request context)", j.Status)
		}
	}
}

func TestGetJobHidesInternalErrorDetails(t *testing.T) {
	repo := newFakeRepo()
	repo.getErr = errors.New(`pq: relation "jobs" does not exist [secret-schema]`)
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}", h.GetJob)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+uuid.New().String(), nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rw.Code)
	}
	if strings.Contains(rw.Body.String(), "secret-schema") {
		t.Fatalf("response leaked internal error detail: %s", rw.Body.String())
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

func TestGetMomentsReturnsDerivedMomentDocument(t *testing.T) {
	repo := newFakeRepo()
	plan := killplan.NewPlan()
	plan.Demo.Tickrate = 64
	plan.Segments = []killplan.Segment{{
		ID:        "seg-001",
		Round:     5,
		TickStart: 64,
		TickEnd:   128,
		Kills: []killplan.Kill{{
			Tick:     80,
			Weapon:   "weapon_awp",
			Headshot: true,
		}},
	}}
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/moments", h.GetMoments)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/moments", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	for _, want := range []string{`"schema_version":"1.0"`, `"segment_id":"seg-001"`, `"awp"`, `"headshot"`} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Fatalf("body missing %s: %s", want, rw.Body.String())
		}
	}
}

func TestGetMomentsReturns409WhenJobNotParsed(t *testing.T) {
	repo := newFakeRepo()
	j := job.Job{ID: uuid.New(), Status: job.StatusQueued, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/moments", h.GetMoments)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/moments", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rw.Code)
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

func TestStartRecordingAllowsIdempotentRetryWhenRecorded(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
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

func TestStartCompositionAllowsIdempotentRetryWhenComposed(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusComposed, Rules: rules.Default()}
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

func TestStartRenderVariantEnqueuesRenderTaskWhenRecorded(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/natural-hq2-full", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(queue.enqueued))
	}
	if queue.enqueued[0].Type() != tasks.TypeRenderVariant {
		t.Fatalf("task type = %q, want %q", queue.enqueued[0].Type(), tasks.TypeRenderVariant)
	}
	var payload tasks.RenderVariantPayload
	if err := json.Unmarshal(queue.enqueued[0].Payload(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.JobID != j.ID || payload.Variant != editor.PresetShortNaturalHQ2Full {
		t.Fatalf("payload = %#v, want job %s variant %s", payload, j.ID, editor.PresetShortNaturalHQ2Full)
	}
	if len(queue.options) != 1 || !hasAsynqOption(queue.options[0], "Unique(") {
		t.Fatalf("enqueue options = %#v, want Unique option", queue.options)
	}
	statusKey, err := artifacts.RenderVariantStatusKey(j.ID, editor.PresetShortNaturalHQ2Full)
	if err != nil {
		t.Fatal(err)
	}
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(storeBytes(t, h.storage, statusKey), &state); err != nil {
		t.Fatal(err)
	}
	if got, want := state.Status, renderplan.RenderVariantStatusQueued; got != want {
		t.Fatalf("state status = %q, want %q", got, want)
	}
}

func TestGetRenderVariantReturnsQueuedState(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	loadout, err := renderplan.LoadoutForVariant(editor.PresetShortNaturalHQ2Full)
	if err != nil {
		t.Fatal(err)
	}
	state, err := newRenderVariantState(j.ID, loadout, renderplan.RenderVariantStatusQueued, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	statusKey, err := artifacts.RenderVariantStatusKey(j.ID, editor.PresetShortNaturalHQ2Full)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(statusKey, bytes.NewReader(b))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}", h.GetRenderVariant)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/natural-hq2-full", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"status":"queued"`) {
		t.Fatalf("body missing queued state: %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), "edit-document.json") {
		t.Fatalf("body missing artifact keys: %s", rw.Body.String())
	}
}

func TestStartRenderVariantRejectsUnsafeVariant(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/bad.mp4", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
}

func TestStartRenderVariantValidatesAgainstPresetRegistry(t *testing.T) {
	cases := []struct {
		name       string
		variant    string
		wantStatus int
	}{
		{name: "default viral preset", variant: editor.PresetViral60, wantStatus: http.StatusAccepted},
		{name: "known natural preset", variant: editor.PresetShortNaturalHQ2Full, wantStatus: http.StatusAccepted},
		{name: "unknown preset", variant: "made-up-preset", wantStatus: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			queue := &fakeQueue{}
			j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
			repo.jobs[j.ID] = j
			h := NewHandlers(repo, newFakeStorage(), queue)

			r := chi.NewRouter()
			r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)
			req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/"+tc.variant, nil)
			rw := httptest.NewRecorder()
			r.ServeHTTP(rw, req)

			if rw.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rw.Code, tc.wantStatus, rw.Body.String())
			}
			if tc.wantStatus != http.StatusAccepted {
				if len(queue.enqueued) != 0 {
					t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
				}
				if !strings.Contains(rw.Body.String(), editor.PresetViral60) {
					t.Fatalf("error body should list valid presets: %s", rw.Body.String())
				}
				return
			}
			if len(queue.enqueued) != 1 {
				t.Fatalf("enqueued = %d, want 1", len(queue.enqueued))
			}
		})
	}
}

func TestStartRenderVariantRejectsWrongStatus(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/natural-hq2-full", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
}

func TestGetRenderVariantReturnsReadyArtifactStatus(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	key, err := artifacts.RenderVariantResultKey(j.ID, editor.PresetShortNaturalHQ2Full)
	if err != nil {
		t.Fatal(err)
	}
	result := editor.Result{Preset: editor.PresetShortNaturalHQ2Full}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(key, bytes.NewReader(b))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}", h.GetRenderVariant)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/natural-hq2-full", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"status":"ready"`) {
		t.Fatalf("body missing ready status: %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"job_id"`) {
		t.Fatalf("body missing RenderVariantState job_id: %s", rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), "edit-document.json") {
		t.Fatalf("body missing state artifact keys: %s", rw.Body.String())
	}
}

func TestGetMomentsPrefersStoredArtifact(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	_ = store.Put(artifacts.MomentsKey(j.ID), bytes.NewReader([]byte(`{"schema_version":"stored"}`)))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/moments", h.GetMoments)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/moments", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"schema_version":"stored"`) {
		t.Fatalf("body = %s, want stored artifact", rw.Body.String())
	}
}

func TestRoutesRequireMutationTokenForPostsWhenConfigured(t *testing.T) {
	repo := newFakeRepo()
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{}, WithMutationToken("secret"))
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/jobs", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rw.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/loadouts", nil)
	rw = httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rw.Code)
	}
}

func TestGetRenderPublishBoardReturnsReadyStatus(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	variant := editor.PresetShortNaturalHQ2Full
	resultKey, err := artifacts.RenderVariantResultKey(j.ID, variant)
	if err != nil {
		t.Fatal(err)
	}
	result := editor.Result{
		Preset: variant,
		Shorts: []editor.ShortResult{{
			SegmentID: "seg-001",
		}},
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(resultKey, bytes.NewReader(b))
	for _, keyFn := range []func(uuid.UUID, string) (string, error){
		artifacts.RenderVariantPackManifestKey,
		artifacts.RenderVariantGalleryKey,
		artifacts.RenderVariantPublishSummaryKey,
	} {
		key, err := keyFn(j.ID, variant)
		if err != nil {
			t.Fatal(err)
		}
		_ = store.Put(key, bytes.NewReader([]byte("artifact")))
	}
	videoKey, err := artifacts.RenderVariantVideoKey(j.ID, variant, "seg-001")
	if err != nil {
		t.Fatal(err)
	}
	coverKey, err := artifacts.RenderVariantCoverKey(j.ID, variant, "seg-001")
	if err != nil {
		t.Fatal(err)
	}
	captionKey, err := artifacts.RenderVariantCaptionKey(j.ID, variant, "seg-001")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{videoKey, coverKey, captionKey} {
		_ = store.Put(key, bytes.NewReader([]byte("artifact")))
	}
	uploadedKey, err := artifacts.RenderVariantUploadStatusKey(j.ID, variant)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(uploadedKey, bytes.NewReader([]byte(`{"uploaded":true}`)))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}/publish", h.GetRenderPublishBoard)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/natural-hq2-full/publish", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	for _, want := range []string{`"status":"uploaded"`, `"uploaded":true`, `"render_ready":true`, `"video_ready":true`, `"cover_ready":true`, `"caption_ready":true`} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Fatalf("body missing %s: %s", want, rw.Body.String())
		}
	}
}

func TestSetRenderUploadedWritesLocalMarker(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}/publish/uploaded", h.SetRenderUploaded)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/natural-hq2-full/publish/uploaded", strings.NewReader(`{"uploaded":true}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	key, err := artifacts.RenderVariantUploadStatusKey(j.ID, editor.PresetShortNaturalHQ2Full)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(storeBytes(t, store, key)), `"uploaded": true`) {
		t.Fatalf("uploaded marker = %s", storeBytes(t, store, key))
	}
}

func TestStartCaptionAgentEnqueuesCodexTask(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}/agent/captions", h.StartCaptionAgent)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/natural-hq2-full/agent/captions", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeCodexAgent {
		t.Fatalf("queue = %#v", queue.enqueued)
	}
	if !strings.Contains(rw.Body.String(), "caption-candidates") {
		t.Fatalf("body missing agent kind: %s", rw.Body.String())
	}
}

func TestGetCaptionAgentStreamsStoredResult(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	key, err := artifacts.RenderVariantAgentResultKey(j.ID, editor.PresetShortNaturalHQ2Full, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(key, bytes.NewReader([]byte(`{"status":"ready","titles":["t1"]}`)))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}/agent/captions", h.GetCaptionAgent)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/natural-hq2-full/agent/captions", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"titles":["t1"]`) {
		t.Fatalf("body = %s", rw.Body.String())
	}
}

func TestGetRenderQualityReturnsReadyReport(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	variant := editor.PresetShortNaturalHQ2Full
	resultKey, err := artifacts.RenderVariantResultKey(j.ID, variant)
	if err != nil {
		t.Fatal(err)
	}
	result := editor.Result{
		Preset: variant,
		Shorts: []editor.ShortResult{{
			SegmentID: "seg-001",
			PublishArtifact: recording.RecordingArtifact{
				SizeBytes:       10,
				Width:           1080,
				Height:          1920,
				DurationSeconds: 30,
				Codec:           "h264",
			},
		}},
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(resultKey, bytes.NewReader(b))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}/quality", h.GetRenderQuality)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/natural-hq2-full/quality", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	for _, want := range []string{`"status":"ready"`, `"video_width":1080`, `"video_height":1920`, `"video_codec":"h264"`} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Fatalf("body missing %s: %s", want, rw.Body.String())
		}
	}
}

func TestRenderArtifactRoutesStreamKnownArtifacts(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	variant := editor.PresetShortNaturalHQ2Full
	videoKey, err := artifacts.RenderVariantVideoKey(j.ID, variant, "seg-001")
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(videoKey, bytes.NewReader([]byte("mp4-bytes")))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}/videos/{name}", h.GetRenderVideo)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/natural-hq2-full/videos/seg-001", nil)
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

func TestRenderArtifactRoutesRejectUnsafeArtifactName(t *testing.T) {
	repo := newFakeRepo()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}/videos/{name}", h.GetRenderVideo)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/natural-hq2-full/videos/seg-001.mp4", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
}

func TestRenderPackAndEditDocumentRoutesStreamJSON(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	variant := editor.PresetShortNaturalHQ2Full
	packKey, err := artifacts.RenderVariantPackManifestKey(j.ID, variant)
	if err != nil {
		t.Fatal(err)
	}
	editKey, err := artifacts.RenderVariantEditDocumentKey(j.ID, variant)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(packKey, bytes.NewReader([]byte(`{"items":[]}`)))
	_ = store.Put(editKey, bytes.NewReader([]byte(`{"schema_version":"1.0"}`)))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}/pack", h.GetRenderPack)
	r.Get("/api/jobs/{id}/renders/{variant}/edit-document", h.GetRenderEditDocument)
	for _, path := range []string{
		"/api/jobs/" + j.ID.String() + "/renders/natural-hq2-full/pack",
		"/api/jobs/" + j.ID.String() + "/renders/natural-hq2-full/edit-document",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rw := httptest.NewRecorder()
		r.ServeHTTP(rw, req)
		if rw.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200; body=%s", path, rw.Code, rw.Body.String())
		}
		if got := rw.Header().Get("Content-Type"); got != "application/json" {
			t.Fatalf("%s Content-Type = %q, want application/json", path, got)
		}
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

func TestWorkbenchLocalProductFlowEndToEnd(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	plan.Demo.Map = "de_ancient"
	plan.Demo.Tickrate = 64
	plan.Target.NameInDemo = "MartinezSa"
	plan.Segments = []killplan.Segment{{
		ID:        "seg-001",
		Round:     2,
		TickStart: 640,
		TickEnd:   1280,
		Kills: []killplan.Kill{{
			Weapon:   "ak47",
			Headshot: true,
			Victim:   killplan.Player{NameInDemo: "alex"},
		}},
	}}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, DemoPath: "demos/demo.dem", TargetSteamID: "76561198000000000", Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	_ = store.Put(artifacts.MomentsKey(j.ID), bytes.NewReader([]byte(`{"schema_version":"1.0","moments":[{"id":"mom-001","player":"MartinezSa"}]}`)))
	h := NewHandlers(repo, store, queue, WithMutationToken("secret"))
	r := Routes(h)

	get := func(path string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rw := httptest.NewRecorder()
		r.ServeHTTP(rw, req)
		if rw.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200; body=%s", path, rw.Code, rw.Body.String())
		}
		return rw.Body.String()
	}
	post := func(path, body string, token bool, want int) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token {
			req.Header.Set("X-ZackVideo-Token", "secret")
		}
		rw := httptest.NewRecorder()
		r.ServeHTTP(rw, req)
		if rw.Code != want {
			t.Fatalf("POST %s status = %d, want %d; body=%s", path, rw.Code, want, rw.Body.String())
		}
		return rw.Body.String()
	}

	for _, check := range []struct {
		path string
		want string
	}{
		{"/", "ZackVideo Workbench"},
		{"/api/jobs", j.ID.String()},
		{"/api/loadouts", editor.PresetShortNaturalHQ2Full},
		{"/api/jobs/" + j.ID.String() + "/moments", "MartinezSa"},
	} {
		if body := get(check.path); !strings.Contains(body, check.want) {
			t.Fatalf("GET %s body missing %q: %s", check.path, check.want, body)
		}
	}

	renderPath := "/api/jobs/" + j.ID.String() + "/renders/natural-hq2-full"
	post(renderPath, "", false, http.StatusUnauthorized)
	post(renderPath, "", true, http.StatusAccepted)
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeRenderVariant {
		t.Fatalf("queue after render = %#v", queue.enqueued)
	}
	if body := get(renderPath); !strings.Contains(body, `"status":"queued"`) {
		t.Fatalf("render state missing queued: %s", body)
	}

	resultKey, err := artifacts.RenderVariantResultKey(j.ID, editor.PresetShortNaturalHQ2Full)
	if err != nil {
		t.Fatal(err)
	}
	result := editor.Result{
		Preset: editor.PresetShortNaturalHQ2Full,
		Shorts: []editor.ShortResult{{
			SegmentID: "seg-001",
			PublishArtifact: recording.RecordingArtifact{
				SizeBytes:       123,
				Width:           1080,
				Height:          1920,
				DurationSeconds: 15,
				Codec:           "h264",
			},
		}},
	}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(resultKey, bytes.NewReader(b))
	for _, keyFn := range []func(uuid.UUID, string) (string, error){
		artifacts.RenderVariantPackManifestKey,
		artifacts.RenderVariantGalleryKey,
		artifacts.RenderVariantPublishSummaryKey,
	} {
		key, err := keyFn(j.ID, editor.PresetShortNaturalHQ2Full)
		if err != nil {
			t.Fatal(err)
		}
		_ = store.Put(key, bytes.NewReader([]byte("artifact")))
	}
	for _, keyFn := range []func(uuid.UUID, string, string) (string, error){
		artifacts.RenderVariantVideoKey,
		artifacts.RenderVariantCoverKey,
		artifacts.RenderVariantCaptionKey,
	} {
		key, err := keyFn(j.ID, editor.PresetShortNaturalHQ2Full, "seg-001")
		if err != nil {
			t.Fatal(err)
		}
		_ = store.Put(key, bytes.NewReader([]byte("artifact")))
	}
	publishPath := renderPath + "/publish"
	if body := get(publishPath); !strings.Contains(body, `"status":"ready"`) {
		t.Fatalf("publish board missing ready: %s", body)
	}
	post(publishPath+"/uploaded", `{"uploaded":true}`, true, http.StatusOK)
	if body := get(publishPath); !strings.Contains(body, `"status":"uploaded"`) {
		t.Fatalf("publish board missing uploaded: %s", body)
	}
	if body := get(renderPath + "/quality"); !strings.Contains(body, `"status":"ready"`) || !strings.Contains(body, `"video_codec":"h264"`) {
		t.Fatalf("quality body missing ready codec: %s", body)
	}

	agentPath := renderPath + "/agent/captions"
	post(agentPath, "", true, http.StatusAccepted)
	if len(queue.enqueued) != 2 || queue.enqueued[1].Type() != tasks.TypeCodexAgent {
		t.Fatalf("queue after agent = %#v", queue.enqueued)
	}
	agentKey, err := artifacts.RenderVariantAgentResultKey(j.ID, editor.PresetShortNaturalHQ2Full, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(agentKey, bytes.NewReader([]byte(`{"status":"ready","titles":["t1"],"captions":["c1"]}`)))
	if body := get(agentPath); !strings.Contains(body, `"titles":["t1"]`) {
		t.Fatalf("agent result missing title: %s", body)
	}
}

func TestStreamJobFlowSavesPlanAndEnqueuesRender(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	h := NewHandlers(newFakeRepo(), store, queue, WithStreamRepository(streamRepo))
	r := Routes(h)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	videoPart, _ := mw.CreateFormFile("video", "stream.mp4")
	_, _ = videoPart.Write([]byte("mp4-bytes"))
	_ = mw.WriteField("config", `{"title":"match stream"}`)
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body=%s", rw.Code, rw.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, err := uuid.Parse(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.puts[streamclips.SourceKey(id)]; !ok {
		t.Fatalf("storage missing stream source")
	}

	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 1, EndSeconds: 3, Title: "one"}}
	planBody, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPut, "/api/stream-jobs/"+created.ID+"/edit-plan", bytes.NewReader(planBody))
	req.Header.Set("Content-Type", "application/json")
	rw = httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("plan status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if streamRepo.jobs[id].Status != streamclips.StatusReady {
		t.Fatalf("stream status = %s, want ready", streamRepo.jobs[id].Status)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+created.ID+"/renders/streamer-vertical-stack", nil)
	rw = httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("render status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeRenderStreamClip {
		t.Fatalf("queue = %#v", queue.enqueued)
	}
}

func TestStreamVideoRejectsUnsafeClipID(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	id := uuid.New()
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusRendered, SourcePath: streamclips.SourceKey(id)}
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{}, WithStreamRepository(streamRepo))
	r := Routes(h)

	req := httptest.NewRequest(http.MethodGet, "/api/stream-jobs/"+id.String()+"/renders/streamer-vertical-stack/videos/bad.mp4", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
}

func storeBytes(t *testing.T, store storage.Storage, key string) []byte {
	t.Helper()
	rc, err := store.Open(key)
	if err != nil {
		t.Fatalf("open %s: %v", key, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return b
}

func hasAsynqOption(opts []asynq.Option, prefix string) bool {
	for _, opt := range opts {
		if strings.HasPrefix(opt.String(), prefix) {
			return true
		}
	}
	return false
}
