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
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/composition"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/moments"
	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
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
func (f *fakeRepo) SetParseInputs(_ context.Context, id uuid.UUID, steamID string, r rules.Rules) error {
	j, ok := f.jobs[id]
	if !ok {
		return job.ErrNotFound
	}
	if j.Status != job.StatusScanned && j.Status != job.StatusParsed {
		return job.ErrConflict
	}
	j.TargetSteamID = steamID
	j.Rules = r
	j.Status = job.StatusParsing
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

// demoMagic is the CS2 (Source 2) demo header CreateJob validates against.
var demoMagic = []byte("PBDEMS2\x00")

// multipartBody builds a CreateJob upload whose demo bytes start with a valid
// CS2 demo header, so it exercises the happy path. Tests that need an invalid
// header build their own body.
func multipartBody(t *testing.T, demoBytes []byte, configJSON string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	demoPart, _ := mw.CreateFormFile("demo", "test.dem")
	demoPart.Write(demoMagic)
	demoPart.Write(demoBytes)
	mw.WriteField("config", configJSON)
	mw.Close()
	return body, mw.FormDataContentType()
}

// multipartBodyRaw builds a CreateJob upload with exactly the given demo bytes,
// for tests that assert on the magic-byte validation itself.
func multipartBodyRaw(t *testing.T, demoBytes []byte, configJSON string) (*bytes.Buffer, string) {
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

func TestPostJobsRemovesMultipartTempFiles(t *testing.T) {
	withIsolatedTempDir(t)
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	h := NewHandlers(repo, store, queue)

	body, ct := multipartBody(t, bytes.Repeat([]byte("d"), multipartMemBudget+1), `{"target_steamid":"76561198000000000"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", ct)
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rw.Code, rw.Body.String())
	}
	assertMultipartTempDirEmpty(t)
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
	if !strings.Contains(rw.Body.String(), editor.PresetViral60Clean) {
		t.Fatalf("body missing loadout: %s", rw.Body.String())
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
	if resp.Default != editor.PresetViral60Clean {
		t.Fatalf("default = %q, want %q", resp.Default, editor.PresetViral60Clean)
	}
	if got, want := len(resp.Presets), len(editor.PresetNames()); got != want {
		t.Fatalf("presets = %d, want %d", got, want)
	}
	first := resp.Presets[0]
	if first.Name != editor.PresetViral60Clean || !first.Default || first.Description == "" {
		t.Fatalf("first preset = %#v, want default %s", first, editor.PresetViral60Clean)
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
	for _, want := range []string{"FragForge Workbench", "Mutation token", "workbench-shell", "HTMX", `hx-post="/ui/jobs"`, `hx-get="/ui/jobs"`, `hx-get="/ui/workspace"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("workbench missing %q", want)
		}
	}
	if !strings.Contains(body, `"X-FragForge-Token"`) {
		t.Fatalf("workbench missing mutation token header")
	}
	if strings.Contains(body, "X-ZackVideo-Token") {
		t.Fatalf("workbench uses stale mutation token header")
	}
	if strings.Contains(body, "WORKBENCH_HTMX") || strings.Contains(body, "WORKBENCH_CSS") {
		t.Fatalf("workbench contains unreplaced template markers")
	}
	if strings.Contains(body, "type JobStatus") || strings.Contains(body, "interface AppState") {
		t.Fatalf("workbench still embeds the old TypeScript app")
	}
}

func TestWorkbenchWorkspaceOnboardsAndDeepLinksSelectedJob(t *testing.T) {
	repo := newFakeRepo()
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})
	r := Routes(h)

	req := httptest.NewRequest(http.MethodGet, "/ui/workspace", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("onboarding status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	for _, want := range []string{"Start here", "Ready for local run", "No Node server required", "shortslistosparasubir"} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Fatalf("onboarding missing %q: %s", want, rw.Body.String())
		}
	}

	j := job.Job{ID: uuid.New(), Status: job.StatusScanned, DemoPath: "demos/deep.dem", Rules: rules.Default()}
	repo.jobs[j.ID] = j
	req = httptest.NewRequest(http.MethodGet, "/ui/workspace", nil)
	req.Header.Set("HX-Current-URL", "http://127.0.0.1:8080/?job="+j.ID.String())
	rw = httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("deep-link status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	for _, want := range []string{j.ID.String(), `hx-swap-oob="true"`, "Choose the POV to clip", "deep.dem"} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Fatalf("deep-link workspace missing %q: %s", want, rw.Body.String())
		}
	}
}

func TestWorkbenchHTMXFragmentsExposeLocalFlow(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	plan := killplan.NewPlan()
	plan.Demo.Map = "de_mirage"
	plan.Demo.Tickrate = 64
	plan.Target.NameInDemo = "MartinezSa"
	plan.Segments = []killplan.Segment{{
		ID:        "seg-001",
		Round:     4,
		TickStart: 640,
		TickEnd:   1280,
		Kills: []killplan.Kill{{
			Weapon:   "ak47",
			Headshot: true,
			Victim:   killplan.Player{NameInDemo: "alex"},
		}},
	}}
	j := job.Job{
		ID:            uuid.New(),
		Status:        job.StatusRecorded,
		DemoPath:      "demos/local.dem",
		TargetSteamID: "76561198000000000",
		Rules:         rules.Default(),
		KillPlan:      &plan,
	}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, &fakeQueue{})
	r := Routes(h)

	req := httptest.NewRequest(http.MethodGet, "/ui/jobs?selected="+j.ID.String(), nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("jobs status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	for _, want := range []string{j.ID.String(), `hx-get="/ui/jobs/` + j.ID.String(), `aria-selected="true"`} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Fatalf("jobs fragment missing %q: %s", want, rw.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/ui/jobs/"+j.ID.String(), nil)
	rw = httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("job status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	for _, want := range []string{
		"Generate short",
		"Choose your short",
		`hx-post="/ui/jobs/` + j.ID.String() + `/generate"`,
		"Kill Feed", "Clean POV", "Full HUD",
		"short-9x16", "landscape-16x9", "Punch-in",
		"de_mirage", "MartinezSa",
	} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Fatalf("job fragment missing %q: %s", want, rw.Body.String())
		}
	}
}

func TestWorkbenchCreateJobWithTargetEnqueuesParse(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	h := NewHandlers(repo, newFakeStorage(), queue)
	r := Routes(h)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	demoPart, _ := mw.CreateFormFile("demo", "target.dem")
	demoPart.Write(demoMagic)
	demoPart.Write([]byte("dem-bytes"))
	mw.WriteField("target_steamid", "76561198000000000")
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/ui/jobs", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rw.Code, rw.Body.String())
	}
	if got := rw.Header().Get("HX-Redirect"); !strings.HasPrefix(got, "/?job=") {
		t.Fatalf("HX-Redirect = %q, want job redirect", got)
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeParseDemo {
		t.Fatalf("queue = %#v, want parse task", queue.enqueued)
	}
	for _, j := range repo.jobs {
		if j.TargetSteamID != "76561198000000000" {
			t.Fatalf("TargetSteamID = %q, want submitted target", j.TargetSteamID)
		}
	}
}

func TestWorkbenchRenderFormEnqueuesEditOptions(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	plan.Segments = []killplan.Segment{{ID: "seg-001", TickStart: 1, TickEnd: 2}}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)
	r := Routes(h)

	body := strings.NewReader("variant=viral-60-clean&music=synth-one&format=landscape-16x9&kill_effect=velocity&transition=whip&intro=on&outro=on")
	req := httptest.NewRequest(http.MethodPost, "/ui/jobs/"+j.ID.String()+"/render", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeRenderVariant {
		t.Fatalf("queue = %#v, want one render task", queue.enqueued)
	}
	var payload tasks.RenderVariantPayload
	if err := json.Unmarshal(queue.enqueued[0].Payload(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Variant != editor.PresetViral60Clean || payload.MusicKey != "synth-one" {
		t.Fatalf("payload variant/music = %q/%q", payload.Variant, payload.MusicKey)
	}
	wantEdit := renderplan.EditRequest{
		Format:     renderplan.FormatLandscape16x9,
		KillEffect: renderplan.KillEffectVelocity,
		Transition: renderplan.TransitionWhip,
		Intro:      true,
		Outro:      true,
	}
	if payload.Edit != wantEdit {
		t.Fatalf("edit = %#v, want %#v", payload.Edit, wantEdit)
	}
	if !strings.Contains(rw.Body.String(), `Queued for render`) {
		t.Fatalf("fragment missing queued render state: %s", rw.Body.String())
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

func TestPostJobsValidatesDemoMagicBytes(t *testing.T) {
	cases := []struct {
		name       string
		demo       []byte
		wantStatus int
	}{
		{name: "cs2 source2 demo", demo: []byte("PBDEMS2\x00rest-of-demo"), wantStatus: http.StatusCreated},
		{name: "legacy gotv demo", demo: []byte("HL2DEMO\x00rest-of-demo"), wantStatus: http.StatusCreated},
		{name: "not a demo", demo: []byte("just some bytes"), wantStatus: http.StatusBadRequest},
		{name: "short non-demo body", demo: []byte("PB2"), wantStatus: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			store := newFakeStorage()
			queue := &fakeQueue{}
			h := NewHandlers(repo, store, queue)

			body, ct := multipartBodyRaw(t, tc.demo, `{"target_steamid":"76561198000000000"}`)
			req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
			req.Header.Set("Content-Type", ct)
			rw := httptest.NewRecorder()

			h.CreateJob(rw, req)

			if rw.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rw.Code, tc.wantStatus, rw.Body.String())
			}
			if tc.wantStatus == http.StatusBadRequest {
				if !strings.Contains(rw.Body.String(), "not a CS2 demo") {
					t.Fatalf("body = %s, want not-a-demo error", rw.Body.String())
				}
				if len(store.puts) != 0 {
					t.Fatalf("storage puts = %d, want 0 (must reject before Put)", len(store.puts))
				}
				return
			}
			// The full demo bytes (header included) must reach storage intact.
			for _, stored := range store.puts {
				if !bytes.Equal(stored, tc.demo) {
					t.Fatalf("stored demo = %q, want full bytes %q", stored, tc.demo)
				}
			}
		})
	}
}

func TestPostJobsWithTargetEnqueuesParse(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	h := NewHandlers(repo, newFakeStorage(), queue)

	body, ct := multipartBody(t, []byte("dem-bytes"), `{"target_steamid":"76561198000000000"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", ct)
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeParseDemo {
		t.Fatalf("queue = %#v, want one parse task", queue.enqueued)
	}
}

func TestPostJobsWithoutTargetEnqueuesScan(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	h := NewHandlers(repo, newFakeStorage(), queue)

	body, ct := multipartBody(t, []byte("dem-bytes"), ``)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", ct)
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeScanRoster {
		t.Fatalf("queue = %#v, want one scan task", queue.enqueued)
	}
	for _, j := range repo.jobs {
		if j.TargetSteamID != "" {
			t.Fatalf("TargetSteamID = %q, want empty for scan-first job", j.TargetSteamID)
		}
	}
}

func TestGetRosterReturns409BeforeScan(t *testing.T) {
	repo := newFakeRepo()
	j := job.Job{ID: uuid.New(), Status: job.StatusScanning, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/roster", h.GetRoster)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/roster", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), "roster not ready") {
		t.Fatalf("body = %s, want roster-not-ready", rw.Body.String())
	}
}

func TestGetRosterReturnsPlayersAfterScan(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusScanned, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	_ = store.Put(artifacts.RosterKey(j.ID), bytes.NewReader([]byte(`{"players":[{"steamid64":"765","name":"kekO","team":"CT","kills":24,"deaths":14,"assists":5}]}`)))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/roster", h.GetRoster)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/roster", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if got := rw.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	for _, want := range []string{`"players"`, `"kekO"`, `"kills":24`} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Fatalf("body missing %s: %s", want, rw.Body.String())
		}
	}
}

func TestStartParseAcceptsScannedJob(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusScanned, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/parse", h.StartParse)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/parse", strings.NewReader(`{"target_steamid":"76561198000000000"}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"status":"parsing"`) {
		t.Fatalf("body missing parsing status: %s", rw.Body.String())
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeParseDemo {
		t.Fatalf("queue = %#v, want one parse task", queue.enqueued)
	}
	if got := repo.jobs[j.ID].TargetSteamID; got != "76561198000000000" {
		t.Fatalf("TargetSteamID = %q, want persisted target", got)
	}
}

func TestStartParseRejectsNonUintTarget(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusScanned, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/parse", h.StartParse)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/parse", strings.NewReader(`{"target_steamid":"not-a-number"}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
}

func TestStartParseRejectsWrongState(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusQueued, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/parse", h.StartParse)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/parse", strings.NewReader(`{"target_steamid":"76561198000000000"}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
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
	h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

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
	if len(queue.options) != 1 || !hasAsynqOption(queue.options[0], "Unique(") {
		t.Fatalf("enqueue options = %#v, want Unique option for dedup", queue.options)
	}
}

func TestStartRecordingAppliesPresetHUD(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/record", h.StartRecording)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/record", strings.NewReader(`{"preset":"clean-pov-60"}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(queue.enqueued))
	}
	var payload tasks.RecordDemoPayload
	if err := json.Unmarshal(queue.enqueued[0].Payload(), &payload); err != nil {
		t.Fatalf("unmarshal record payload: %v", err)
	}
	// clean-pov-60 records HUD-less, so its preset resolves to the "clean" HUD.
	if payload.HUDMode != "clean" {
		t.Fatalf("HUDMode = %q, want clean", payload.HUDMode)
	}
}

func TestStartRecordingRejectsUnknownPreset(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/record", h.StartRecording)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/record", strings.NewReader(`{"preset":"no-such-preset"}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown preset; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
}

func TestStartRecordingPassesSelectedSegmentIDs(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	plan.Segments = []killplan.Segment{{ID: "seg-001"}, {ID: "seg-002"}}
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/record", h.StartRecording)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/record", strings.NewReader(`{"segment_ids":["seg-002"]}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	var payload tasks.RecordDemoPayload
	if err := json.Unmarshal(queue.enqueued[0].Payload(), &payload); err != nil {
		t.Fatalf("unmarshal record payload: %v", err)
	}
	if len(payload.SegmentIDs) != 1 || payload.SegmentIDs[0] != "seg-002" {
		t.Fatalf("SegmentIDs = %v, want [seg-002]", payload.SegmentIDs)
	}
}

func TestStartRecordingRejectsUnknownSegmentID(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	plan.Segments = []killplan.Segment{{ID: "seg-001"}}
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/record", h.StartRecording)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/record", strings.NewReader(`{"segment_ids":["seg-001","seg-999"]}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown segment id; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
}

func TestStartRecordingRejectsJobWithoutPlan(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

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
	h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

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

func TestStartRecordingAllowsRetryWhenFailed(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	// A capture that failed (CS2 crash) keeps its kill plan; the user retries.
	j := job.Job{ID: uuid.New(), Status: job.StatusFailed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

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

func TestStartRecordingRejectsFailedJobWithoutPlan(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	// Failed before it was ever parsed: no kill plan, so re-record stays rejected.
	j := job.Job{ID: uuid.New(), Status: job.StatusFailed, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

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
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", strings.NewReader(`{"music":"track01","edit":{"format":"landscape-16x9","killEffect":"velocity","transition":"whip","intro":true,"outro":true}}`))
	req.Header.Set("Content-Type", "application/json")
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
	if payload.JobID != j.ID || payload.Variant != editor.PresetViral60Clean {
		t.Fatalf("payload = %#v, want job %s variant %s", payload, j.ID, editor.PresetViral60Clean)
	}
	if payload.MusicKey != "track01" {
		t.Fatalf("music key = %q, want track01", payload.MusicKey)
	}
	if payload.Edit.Format != renderplan.FormatLandscape16x9 || payload.Edit.KillEffect != renderplan.KillEffectVelocity || payload.Edit.Transition != renderplan.TransitionWhip || !payload.Edit.Intro || !payload.Edit.Outro {
		t.Fatalf("edit payload = %#v", payload.Edit)
	}
	if len(queue.options) != 1 || !hasAsynqOption(queue.options[0], "Unique(") {
		t.Fatalf("enqueue options = %#v, want Unique option", queue.options)
	}
	statusKey, err := artifacts.RenderVariantStatusKey(j.ID, editor.PresetViral60Clean)
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
	loadout, err := renderplan.LoadoutForVariant(editor.PresetViral60Clean)
	if err != nil {
		t.Fatal(err)
	}
	state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:   j.ID,
		Loadout: loadout,
		Status:  renderplan.RenderVariantStatusQueued,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	statusKey, err := artifacts.RenderVariantStatusKey(j.ID, editor.PresetViral60Clean)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(statusKey, bytes.NewReader(b))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}", h.GetRenderVariant)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", nil)
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
		{name: "registered preset", variant: editor.PresetViral60Clean, wantStatus: http.StatusAccepted},
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
				if !strings.Contains(rw.Body.String(), editor.PresetViral60Clean) {
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
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", nil)
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
	key, err := artifacts.RenderVariantResultKey(j.ID, editor.PresetViral60Clean)
	if err != nil {
		t.Fatal(err)
	}
	result := editor.Result{Preset: editor.PresetViral60Clean}
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(key, bytes.NewReader(b))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}", h.GetRenderVariant)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", nil)
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
	_ = store.Put(moments.ArtifactKey(j.ID), bytes.NewReader([]byte(`{"schema_version":"stored"}`)))
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
	variant := editor.PresetViral60Clean
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
	uploadedKey, err := renderplan.RenderVariantUploadStatusKey(j.ID, variant)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(uploadedKey, bytes.NewReader([]byte(`{"uploaded":true}`)))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}/publish", h.GetRenderPublishBoard)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/publish", nil)
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
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/publish/uploaded", strings.NewReader(`{"uploaded":true}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	key, err := renderplan.RenderVariantUploadStatusKey(j.ID, editor.PresetViral60Clean)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(storeBytes(t, store, key)), `"uploaded": true`) {
		t.Fatalf("uploaded marker = %s", storeBytes(t, store, key))
	}
}

func TestSetRenderUploadedRejectsLargeJSONBody(t *testing.T) {
	repo := newFakeRepo()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}/publish/uploaded", h.SetRenderUploaded)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/publish/uploaded", strings.NewReader(`{`+strings.Repeat(" ", maxJSONBodyBytes+1)))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rw.Code, rw.Body.String())
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
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/agent/captions", nil)
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
	key, err := artifacts.RenderVariantAgentResultKey(j.ID, editor.PresetViral60Clean, renderplan.AgentKindCaptionCandidates)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(key, bytes.NewReader([]byte(`{"status":"ready","titles":["t1"]}`)))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}/agent/captions", h.GetCaptionAgent)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/agent/captions", nil)
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
	variant := editor.PresetViral60Clean
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
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/quality", nil)
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
	variant := editor.PresetViral60Clean
	videoKey, err := artifacts.RenderVariantVideoKey(j.ID, variant, "seg-001")
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(videoKey, bytes.NewReader([]byte("mp4-bytes")))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}/videos/{name}", h.GetRenderVideo)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/videos/seg-001", nil)
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
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/videos/seg-001.mp4", nil)
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
	variant := editor.PresetViral60Clean
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
		"/api/jobs/" + j.ID.String() + "/renders/viral-60-clean/pack",
		"/api/jobs/" + j.ID.String() + "/renders/viral-60-clean/edit-document",
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
	_ = store.Put(composition.FinalArtifactKey(j.ID), bytes.NewReader([]byte("mp4-bytes")))
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
	_ = store.Put(moments.ArtifactKey(j.ID), bytes.NewReader([]byte(`{"schema_version":"1.0","moments":[{"id":"mom-001","player":"MartinezSa"}]}`)))
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
			req.Header.Set("X-FragForge-Token", "secret")
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
		{"/", "FragForge Workbench"},
		{"/api/jobs", j.ID.String()},
		{"/api/loadouts", editor.PresetViral60Clean},
		{"/api/jobs/" + j.ID.String() + "/moments", "MartinezSa"},
	} {
		if body := get(check.path); !strings.Contains(body, check.want) {
			t.Fatalf("GET %s body missing %q: %s", check.path, check.want, body)
		}
	}

	renderPath := "/api/jobs/" + j.ID.String() + "/renders/viral-60-clean"
	post(renderPath, "", false, http.StatusUnauthorized)
	post(renderPath, "", true, http.StatusAccepted)
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeRenderVariant {
		t.Fatalf("queue after render = %#v", queue.enqueued)
	}
	if body := get(renderPath); !strings.Contains(body, `"status":"queued"`) {
		t.Fatalf("render state missing queued: %s", body)
	}

	resultKey, err := artifacts.RenderVariantResultKey(j.ID, editor.PresetViral60Clean)
	if err != nil {
		t.Fatal(err)
	}
	result := editor.Result{
		Preset: editor.PresetViral60Clean,
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
		key, err := keyFn(j.ID, editor.PresetViral60Clean)
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
		key, err := keyFn(j.ID, editor.PresetViral60Clean, "seg-001")
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
	agentKey, err := artifacts.RenderVariantAgentResultKey(j.ID, editor.PresetViral60Clean, renderplan.AgentKindCaptionCandidates)
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

func TestStreamJobRemovesMultipartTempFiles(t *testing.T) {
	withIsolatedTempDir(t)
	streamRepo := newFakeStreamRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	h := NewHandlers(newFakeRepo(), store, queue, WithStreamRepository(streamRepo))

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	videoPart, _ := mw.CreateFormFile("video", "stream.mp4")
	_, _ = videoPart.Write(bytes.Repeat([]byte("m"), multipartMemBudget+1))
	_ = mw.WriteField("config", `{"title":"match stream"}`)
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rw := httptest.NewRecorder()

	h.CreateStreamJob(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rw.Code, rw.Body.String())
	}
	assertMultipartTempDirEmpty(t)
}

func TestPutStreamEditPlanRejectsLargeJSONBody(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	id := uuid.New()
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusUploaded, SourcePath: streamclips.SourceKey(id)}
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{}, WithStreamRepository(streamRepo))

	r := chi.NewRouter()
	r.Put("/api/stream-jobs/{id}/edit-plan", h.PutStreamEditPlan)
	req := httptest.NewRequest(http.MethodPut, "/api/stream-jobs/"+id.String()+"/edit-plan", strings.NewReader(`{`+strings.Repeat(" ", maxJSONBodyBytes+1)))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%s", rw.Code, rw.Body.String())
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

func withIsolatedTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
	return dir
}

func assertMultipartTempDirEmpty(t *testing.T) {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "multipart-") || strings.HasPrefix(entry.Name(), "zv-stream-upload-") {
			t.Fatalf("temporary upload file still exists: %s", filepath.Join(os.TempDir(), entry.Name()))
		}
	}
}
