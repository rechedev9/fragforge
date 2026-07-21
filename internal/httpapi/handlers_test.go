package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

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
	deleteErr       error
	updateHonorsCtx bool
}

type fakeStreamRepo struct {
	jobs map[uuid.UUID]streamclips.Job
}

type blockingSetStreamRepo struct {
	*fakeStreamRepo
	entered chan struct{}
	release chan struct{}
}

func (r *blockingSetStreamRepo) SetEditPlan(ctx context.Context, id uuid.UUID, plan streamclips.EditPlan) error {
	close(r.entered)
	select {
	case <-r.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return r.fakeStreamRepo.SetEditPlan(ctx, id, plan)
}

func newFakeStreamRepo() *fakeStreamRepo {
	return &fakeStreamRepo{jobs: map[uuid.UUID]streamclips.Job{}}
}

func reviewedDefaultEditPlanJSON(t *testing.T) json.RawMessage {
	t.Helper()
	plan := streamclips.DefaultEditPlan()
	plan.FaceCropReviewed = true
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return raw
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

func (f *fakeStreamRepo) SetAcquired(_ context.Context, id uuid.UUID, probe streamclips.SourceProbe, sha256, discoveredTitle string) error {
	j, ok := f.jobs[id]
	if !ok {
		return streamclips.ErrNotFound
	}
	j.Probe = probe
	j.SourceSHA256 = sha256
	if j.Title == "" {
		j.Title = discoveredTitle
	}
	j.Status = streamclips.StatusReady
	j.FailureReason = ""
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

func (f *fakeRepo) GetMeta(ctx context.Context, id uuid.UUID) (job.Job, error) {
	j, err := f.Get(ctx, id)
	if err != nil {
		return job.Job{}, err
	}
	j.KillPlan = nil
	return j, nil
}

func (f *fakeRepo) GetStatus(ctx context.Context, id uuid.UUID) (job.Status, string, int, error) {
	j, err := f.Get(ctx, id)
	if err != nil {
		return 0, "", 0, err
	}
	segmentCount := 0
	if j.Status == job.StatusRecording && j.KillPlan != nil {
		segmentCount = len(j.KillPlan.Segments)
	}
	return j.Status, j.FailureReason, segmentCount, nil
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
func (f *fakeRepo) ListBySeries(_ context.Context, seriesID string) ([]job.Job, error) {
	jobs := make([]job.Job, 0, len(f.jobs))
	for _, j := range f.jobs {
		if j.SeriesID == seriesID {
			j.KillPlan = nil
			jobs = append(jobs, j)
		}
	}
	sort.Slice(jobs, func(i, k int) bool {
		if jobs[i].CreatedAt.Equal(jobs[k].CreatedAt) {
			return jobs[i].ID.String() < jobs[k].ID.String()
		}
		return jobs[i].CreatedAt.Before(jobs[k].CreatedAt)
	})
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
func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.jobs, id)
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
func (f *fakeStorage) Delete(key string) error {
	delete(f.puts, key)
	return nil
}

// DeleteTree removes every stored key under the given prefix, mirroring the
// recursive delete the local filesystem backend provides.
func (f *fakeStorage) DeleteTree(key string) error {
	prefix := key + "/"
	for k := range f.puts {
		if k == key || strings.HasPrefix(k, prefix) {
			delete(f.puts, k)
		}
	}
	return nil
}

// fakeQueue captures enqueued tasks.
type fakeQueue struct {
	enqueued    []*asynq.Task
	options     [][]asynq.Option
	transitions []func(error) error
	err         error
}

func (q *fakeQueue) Enqueue(t *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return q.enqueue(t, nil, opts...)
}

func (q *fakeQueue) EnqueueWithTransition(t *asynq.Task, transition func(error) error, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	return q.enqueue(t, transition, opts...)
}

func (q *fakeQueue) enqueue(t *asynq.Task, transition func(error) error, opts ...asynq.Option) (*asynq.TaskInfo, error) {
	if transition != nil {
		if err := transition(q.err); err != nil {
			return nil, err
		}
	}
	if q.err != nil {
		return nil, q.err
	}
	q.enqueued = append(q.enqueued, t)
	q.options = append(q.options, opts)
	if transition != nil {
		q.transitions = append(q.transitions, transition)
	}
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

// multipartBodyFields builds a CreateJob upload with a valid demo header, the
// given demo file name, and arbitrary extra form fields (e.g. config,
// series_id). It is used by the series/file-name tests.
func multipartBodyFields(t *testing.T, filename string, demoBytes []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	demoPart, _ := mw.CreateFormFile("demo", filename)
	demoPart.Write(demoMagic)
	demoPart.Write(demoBytes)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
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

func TestSanitizeDemoFileName(t *testing.T) {
	longName := strings.Repeat("a", 200) + ".dem"
	// U+FEFF BOM, U+200B zero-width space, U+202E RTL override: Cf format
	// characters that must be dropped, not just Cc controls. Built from rune
	// values so no invisible characters hide in the source.
	formatCharsName := string(rune(0xFEFF)) + "med" + string(rune(0x200B)) + "io" + string(rune(0x202E)) + "med.dem"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "match.dem", "match.dem"},
		{"windows_path", `C:\replays\game one.dem`, "game one.dem"},
		{"url_path", "uploads/2026/final.dem", "final.dem"},
		{"mixed_separators", `dir/sub\match.dem`, "match.dem"},
		{"control_chars", "clip\t\n\x00.dem", "clip.dem"},
		{"format_chars", formatCharsName, "mediomed.dem"},
		{"over_long", longName, strings.Repeat("a", 128)},
		{"empty", "", ""},
		{"only_separators", `a/b\`, ""},
		{"only_control", "\x00\x01\x02", ""},
		{"whitespace_only", "   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeDemoFileName(tc.in)
			if got != tc.want {
				t.Fatalf("sanitizeDemoFileName(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if len([]rune(got)) > maxDemoFileNameRunes {
				t.Fatalf("sanitizeDemoFileName(%q) = %q, exceeds %d runes", tc.in, got, maxDemoFileNameRunes)
			}
		})
	}
}

func TestCreateJobStoresSeriesIDAndFileName(t *testing.T) {
	repo := newFakeRepo()
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	// Upper-case series id and a Windows path exercise canonicalization and
	// base-name sanitization in one happy-path request.
	series := strings.ToUpper(uuid.NewString())
	fields := map[string]string{
		"config":    `{"target_steamid":"76561198000000000"}`,
		"series_id": series,
	}
	body, ct := multipartBodyFields(t, `C:\replays\game one.dem`, []byte("dem-bytes"), fields)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", ct)
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rw.Code, rw.Body.String())
	}
	var resp struct {
		ID uuid.UUID `json:"id"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	stored, ok := repo.jobs[resp.ID]
	if !ok {
		t.Fatalf("job %s not stored", resp.ID)
	}
	if got, want := stored.SeriesID, strings.ToLower(series); got != want {
		t.Fatalf("SeriesID = %q, want canonical %q", got, want)
	}
	if got, want := stored.DemoFileName, "game one.dem"; got != want {
		t.Fatalf("DemoFileName = %q, want %q", got, want)
	}
}

func TestCreateJobRejectsInvalidSeriesID(t *testing.T) {
	repo := newFakeRepo()
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	fields := map[string]string{
		"config":    `{"target_steamid":"76561198000000000"}`,
		"series_id": "not-a-uuid",
	}
	body, ct := multipartBodyFields(t, "match.dem", []byte("dem-bytes"), fields)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", ct)
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if len(repo.jobs) != 0 {
		t.Fatalf("repo stored %d jobs, want 0 on invalid series_id", len(repo.jobs))
	}
}

func TestCreateJobWithoutSeriesIDLeavesFieldEmpty(t *testing.T) {
	repo := newFakeRepo()
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	body, ct := multipartBody(t, []byte("dem-bytes"), `{"target_steamid":"76561198000000000"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", ct)
	rw := httptest.NewRecorder()

	h.CreateJob(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rw.Code, rw.Body.String())
	}
	for _, j := range repo.jobs {
		if j.SeriesID != "" {
			t.Fatalf("SeriesID = %q, want empty when series_id absent", j.SeriesID)
		}
		// multipartBody uploads as "test.dem", so the name is captured.
		if j.DemoFileName != "test.dem" {
			t.Fatalf("DemoFileName = %q, want test.dem", j.DemoFileName)
		}
	}
}

func TestListJobsBySeries(t *testing.T) {
	repo := newFakeRepo()
	series := uuid.New()
	other := uuid.New()
	base := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

	// Three jobs in the target series, inserted out of creation order so the
	// handler must sort them; one job in another series; one with no series.
	for _, offset := range []int{2, 0, 1} {
		id := uuid.New()
		repo.jobs[id] = job.Job{
			ID:        id,
			Status:    job.StatusQueued,
			SeriesID:  series.String(),
			CreatedAt: base.Add(time.Duration(offset) * time.Minute),
		}
	}
	otherID := uuid.New()
	repo.jobs[otherID] = job.Job{ID: otherID, Status: job.StatusQueued, SeriesID: other.String(), CreatedAt: base}
	loneID := uuid.New()
	repo.jobs[loneID] = job.Job{ID: loneID, Status: job.StatusQueued, CreatedAt: base}

	// Expected upload order is by CreatedAt ascending.
	var want []uuid.UUID
	for _, j := range sortedByCreatedAt(repo.jobs, series.String()) {
		want = append(want, j.ID)
	}

	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})
	r := chi.NewRouter()
	r.Get("/api/jobs", h.ListJobs)

	req := httptest.NewRequest(http.MethodGet, "/api/jobs?series_id="+series.String(), nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	var resp struct {
		Jobs []job.Job `json:"jobs"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Jobs) != len(want) {
		t.Fatalf("got %d jobs, want %d: %+v", len(resp.Jobs), len(want), resp.Jobs)
	}
	for i, j := range resp.Jobs {
		if j.ID != want[i] {
			t.Fatalf("jobs[%d].ID = %s, want %s (order)", i, j.ID, want[i])
		}
		if j.SeriesID != series.String() {
			t.Fatalf("jobs[%d].SeriesID = %q, want %q", i, j.SeriesID, series.String())
		}
	}

	// Invalid series_id is a 400.
	bad := httptest.NewRequest(http.MethodGet, "/api/jobs?series_id=not-a-uuid", nil)
	badRW := httptest.NewRecorder()
	r.ServeHTTP(badRW, bad)
	if badRW.Code != http.StatusBadRequest {
		t.Fatalf("invalid series_id status = %d, want 400; body=%s", badRW.Code, badRW.Body.String())
	}
}

// sortedByCreatedAt returns the target series' jobs ordered by CreatedAt, the
// same order ListBySeries must produce.
func sortedByCreatedAt(jobs map[uuid.UUID]job.Job, seriesID string) []job.Job {
	out := []job.Job{}
	for _, j := range jobs {
		if j.SeriesID == seriesID {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].CreatedAt.Equal(out[k].CreatedAt) {
			return out[i].ID.String() < out[k].ID.String()
		}
		return out[i].CreatedAt.Before(out[k].CreatedAt)
	})
	return out
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
		Format:        renderplan.FormatLandscape16x9,
		KillEffect:    renderplan.KillEffectVelocity,
		Transition:    renderplan.TransitionWhip,
		Intro:         true,
		Outro:         true,
		CoverStrategy: renderplan.CoverStrategyGenerated,
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

func TestPostJobsMarksAcceptedPendingJobFailedWhenQueueDiscardsIt(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	h := NewHandlers(repo, newFakeStorage(), queue)

	body, contentType := multipartBody(t, []byte("dem-bytes"), `{"target_steamid":"76561198000000000"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs", body)
	req.Header.Set("Content-Type", contentType)
	rw := httptest.NewRecorder()
	h.CreateJob(rw, req)

	if rw.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.transitions) != 1 {
		t.Fatalf("queue transitions = %d, want 1", len(queue.transitions))
	}
	if err := queue.transitions[0](errors.New("inline queue task discarded during shutdown")); err != nil {
		t.Fatalf("discard transition error = %v", err)
	}
	for _, j := range repo.jobs {
		if j.Status != job.StatusFailed || !strings.Contains(j.FailureReason, "discarded during shutdown") {
			t.Fatalf("job after discard = status %s, reason %q; want failed discard reason", j.Status, j.FailureReason)
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

func TestStartParseMarksAcceptedPendingJobFailedWhenQueueDiscardsIt(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusScanned, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/parse", h.StartParse)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/parse", strings.NewReader(`{"target_steamid":"76561198000000000"}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.transitions) != 1 {
		t.Fatalf("queue transitions = %d, want 1", len(queue.transitions))
	}
	if err := queue.transitions[0](errors.New("inline queue task discarded during shutdown")); err != nil {
		t.Fatalf("discard transition error = %v", err)
	}
	got := repo.jobs[j.ID]
	if got.Status != job.StatusFailed || !strings.Contains(got.FailureReason, "discarded during shutdown") {
		t.Fatalf("job after discard = status %s, reason %q; want failed discard reason", got.Status, got.FailureReason)
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

func TestStartRecordingAppliesPresetCaptureHUD(t *testing.T) {
	tests := []struct {
		name                     string
		preset                   string
		format                   string
		wantHUD                  string
		wantPortraitSafeKillfeed bool
	}{
		{name: "kill feed vertical", preset: editor.PresetViral60Clean, format: renderplan.FormatShort9x16, wantHUD: "deathnotices", wantPortraitSafeKillfeed: true},
		{name: "kill feed landscape", preset: editor.PresetViral60Clean, format: renderplan.FormatLandscape16x9, wantHUD: "deathnotices"},
		{name: "clean POV", preset: editor.PresetCleanPOV60, format: renderplan.FormatShort9x16, wantHUD: "clean"},
		{name: "full HUD vertical", preset: editor.PresetFullHUD60, format: renderplan.FormatShort9x16, wantHUD: "gameplay", wantPortraitSafeKillfeed: true},
		{name: "full HUD landscape", preset: editor.PresetFullHUD60, format: renderplan.FormatLandscape16x9, wantHUD: "gameplay"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			queue := &fakeQueue{}
			plan := killplan.NewPlan()
			j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
			repo.jobs[j.ID] = j
			h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

			r := chi.NewRouter()
			r.Post("/api/jobs/{id}/record", h.StartRecording)
			body := fmt.Sprintf(`{"preset":%q,"edit":{"format":%q}}`, tc.preset, tc.format)
			req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/record", strings.NewReader(body))
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
			if payload.HUDMode != tc.wantHUD {
				t.Fatalf("HUDMode = %q, want %q", payload.HUDMode, tc.wantHUD)
			}
			if payload.PortraitSafeKillfeed != tc.wantPortraitSafeKillfeed {
				t.Fatalf("PortraitSafeKillfeed = %t, want %t", payload.PortraitSafeKillfeed, tc.wantPortraitSafeKillfeed)
			}
		})
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
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", strings.NewReader(`{"music":"track01","edit":{"format":"landscape-16x9","killEffect":"velocity","transition":"whip","intro":true,"outro":true,"hook_text":true,"kill_counter":true,"cover_strategy":"no-cover","intro_text":"Watch this ace","outro_text":"follow for more"}}`))
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
	if payload.Edit.IntroText != "Watch this ace" || payload.Edit.OutroText != "follow for more" {
		t.Fatalf("edit bookend text = %q / %q, want round-tripped custom text", payload.Edit.IntroText, payload.Edit.OutroText)
	}
	if !payload.Edit.HookText || !payload.Edit.KillCounter {
		t.Fatalf("edit automatic text = hook %v / counter %v, want true / true", payload.Edit.HookText, payload.Edit.KillCounter)
	}
	if payload.Edit.CoverStrategy != renderplan.CoverStrategyNone {
		t.Fatalf("edit cover strategy = %q, want %q", payload.Edit.CoverStrategy, renderplan.CoverStrategyNone)
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

func TestStartRenderVariantRejectsOutOfRangeMusicVolume(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)
	body := `{"music":{"key":"track01","volume":1.5}}`
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0 for rejected volume", len(queue.enqueued))
	}
}

func TestStartRenderVariantThreadsMusicVolume(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)
	body := `{"music":{"key":"track01","volume":0.35}}`
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(queue.enqueued))
	}
	var payload tasks.RenderVariantPayload
	if err := json.Unmarshal(queue.enqueued[0].Payload(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.MusicKey != "track01" || payload.MusicVolume != 0.35 {
		t.Fatalf("music = %q/%v, want track01/0.35", payload.MusicKey, payload.MusicVolume)
	}
}

func TestStartRenderVariantRejectsWhileGuidedGenerateIsActive(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue)
	if err := h.writeGenerateIntent(j.ID, renderplan.GenerateIntent{
		Variant:     editor.PresetViral60Clean,
		Edit:        renderplan.DefaultEditRequest(),
		ActiveRunID: uuid.New(),
	}); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0 while guided generate is active", len(queue.enqueued))
	}
	if _, ok := store.puts[mustRenderVariantStatusKey(j.ID, editor.PresetViral60Clean)]; ok {
		t.Fatal("manual render conflict published a queued render state")
	}
}

func TestStartRenderVariantPreservesReadyStateWhenTaskIsDuplicate(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{err: asynq.ErrDuplicateTask}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue)
	loadout, err := renderplan.LoadoutForVariant(editor.PresetViral60Clean)
	if err != nil {
		t.Fatalf("LoadoutForVariant error = %v", err)
	}
	ready, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:   j.ID,
		Loadout: loadout,
		Status:  renderplan.RenderVariantStatusReady,
	})
	if err != nil {
		t.Fatalf("NewRenderVariantStateForLoadout error = %v", err)
	}
	if err := h.writeRenderVariantState(ready); err != nil {
		t.Fatalf("writeRenderVariantState error = %v", err)
	}

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"status":"ready"`) {
		t.Fatalf("duplicate response did not preserve ready state: %s", rw.Body.String())
	}
	state, ok, err := h.readRenderVariantState(j.ID, editor.PresetViral60Clean)
	if err != nil || !ok {
		t.Fatalf("readRenderVariantState = (%v, %v, %v)", state, ok, err)
	}
	if state.Status != renderplan.RenderVariantStatusReady {
		t.Fatalf("state status = %q, want ready", state.Status)
	}
}

func TestStartRenderVariantMarksStateFailedWhenEnqueueFails(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{err: errors.New("inline queue is full")}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rw.Code, rw.Body.String())
	}
	state, ok, err := h.readRenderVariantState(j.ID, editor.PresetViral60Clean)
	if err != nil || !ok {
		t.Fatalf("readRenderVariantState = (%v, %v, %v)", state, ok, err)
	}
	if state.Status != renderplan.RenderVariantStatusFailed {
		t.Fatalf("state status = %q, want failed", state.Status)
	}
	if state.Error != "enqueue render task: inline queue is full" {
		t.Fatalf("state error = %q", state.Error)
	}
}

func TestStartRenderVariantMarksAcceptedPendingStateFailedWhenQueueDiscardsIt(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue)
	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)

	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.transitions) != 1 {
		t.Fatalf("queue transitions = %d, want 1", len(queue.transitions))
	}
	if err := queue.transitions[0](errors.New("inline queue task discarded during shutdown")); err != nil {
		t.Fatalf("discard transition error = %v", err)
	}
	state, ok, err := h.readRenderVariantState(j.ID, editor.PresetViral60Clean)
	if err != nil || !ok {
		t.Fatalf("readRenderVariantState = (%v, %v, %v)", state, ok, err)
	}
	if state.Status != renderplan.RenderVariantStatusFailed || !strings.Contains(state.Error, "discarded during shutdown") {
		t.Fatalf("state after discard = status %q, error %q; want failed discard reason", state.Status, state.Error)
	}
}

func TestStartRenderVariantRejectsOverlongBookendText(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue)

	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/renders/{variant}", h.StartRenderVariant)
	body := fmt.Sprintf(`{"edit":{"outro_text":"%s"}}`, strings.Repeat("a", 81))
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean", strings.NewReader(body))
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

func TestGetRenderVariantReportsArtifactNames(t *testing.T) {
	// Regression: the render-variant GET must report the reel's real on-disk
	// artifact names so the client stops guessing them from segment ids (which
	// 404'd because the editor writes a single "demo-compilation" compilation).
	// Uses real Local storage because the names come from listing the variant's
	// videos/ and covers/ dirs.
	cases := []struct {
		name       string
		writeFiles bool
		wantVideos string
		wantCovers string
	}{
		{
			name:       "video and cover present are listed",
			writeFiles: true,
			wantVideos: `"videos":["demo-compilation"]`,
			wantCovers: `"covers":["demo-compilation"]`,
		},
		{
			name:       "missing artifact dirs list as empty arrays",
			writeFiles: false,
			wantVideos: `"videos":[]`,
			wantCovers: `"covers":[]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			store, err := storage.NewLocal(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			variant := editor.PresetViral60Clean
			j := job.Job{ID: uuid.New(), Status: job.StatusDone, Rules: rules.Default()}
			repo.jobs[j.ID] = j

			loadout, err := renderplan.LoadoutForVariant(variant)
			if err != nil {
				t.Fatal(err)
			}
			state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
				JobID:   j.ID,
				Loadout: loadout,
				Status:  renderplan.RenderVariantStatusReady,
			})
			if err != nil {
				t.Fatal(err)
			}
			b, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			statusKey, err := artifacts.RenderVariantStatusKey(j.ID, variant)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Put(statusKey, bytes.NewReader(b)); err != nil {
				t.Fatal(err)
			}
			if tc.writeFiles {
				videoKey, err := artifacts.RenderVariantVideoKey(j.ID, variant, "demo-compilation")
				if err != nil {
					t.Fatal(err)
				}
				coverKey, err := artifacts.RenderVariantCoverKey(j.ID, variant, "demo-compilation")
				if err != nil {
					t.Fatal(err)
				}
				for _, key := range []string{videoKey, coverKey} {
					if err := store.Put(key, bytes.NewReader([]byte("artifact"))); err != nil {
						t.Fatal(err)
					}
				}
			}
			h := NewHandlers(repo, store, &fakeQueue{})

			r := chi.NewRouter()
			r.Get("/api/jobs/{id}/renders/{variant}", h.GetRenderVariant)
			req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/"+variant, nil)
			rw := httptest.NewRecorder()
			r.ServeHTTP(rw, req)

			if rw.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
			}
			body := rw.Body.String()
			if !strings.Contains(body, tc.wantVideos) {
				t.Errorf("body = %s\nwant videos %s", body, tc.wantVideos)
			}
			if !strings.Contains(body, tc.wantCovers) {
				t.Errorf("body = %s\nwant covers %s", body, tc.wantCovers)
			}
		})
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
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}/publish", h.GetRenderPublishBoard)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/publish", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	for _, want := range []string{`"status":"ready"`, `"render_ready":true`, `"video_ready":true`, `"cover_ready":true`, `"caption_ready":true`} {
		if !strings.Contains(rw.Body.String(), want) {
			t.Fatalf("body missing %s: %s", want, rw.Body.String())
		}
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

func TestRenderVideoHonorsRangeRequests(t *testing.T) {
	// Regression: the browser <video> element needs Range support (206 +
	// Content-Range) to start playback; the handler used to always 200 with a
	// plain copy. Uses the real Local storage because ranges apply only to
	// seekable readers (*os.File).
	repo := newFakeRepo()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	videoKey, err := artifacts.RenderVariantVideoKey(j.ID, editor.PresetViral60Clean, "seg-001")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(videoKey, bytes.NewReader([]byte("mp4-bytes"))); err != nil {
		t.Fatal(err)
	}
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Get("/api/jobs/{id}/renders/{variant}/videos/{name}", h.GetRenderVideo)
	req := httptest.NewRequest(http.MethodGet, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/videos/seg-001", nil)
	req.Header.Set("Range", "bytes=0-3")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206; body=%s", rw.Code, rw.Body.String())
	}
	if got, want := rw.Body.String(), "mp4-"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
	if got, want := rw.Header().Get("Content-Range"), "bytes 0-3/9"; got != want {
		t.Fatalf("Content-Range = %q, want %q", got, want)
	}
	if got := rw.Header().Get("Content-Type"); got != "video/mp4" {
		t.Fatalf("Content-Type = %q, want video/mp4", got)
	}
}

func TestDeleteRenderVideoRemovesVideoCoverAndCaption(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusDone, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	keys := make([]string, 0, 3)
	for _, derive := range []func(uuid.UUID, string, string) (string, error){
		artifacts.RenderVariantVideoKey,
		artifacts.RenderVariantCoverKey,
		artifacts.RenderVariantCaptionKey,
	} {
		key, err := derive(j.ID, editor.PresetViral60Clean, "seg-001_seg-002")
		if err != nil {
			t.Fatal(err)
		}
		_ = store.Put(key, bytes.NewReader([]byte("artifact-bytes")))
		keys = append(keys, key)
	}
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Delete("/api/jobs/{id}/renders/{variant}/videos/{name}", h.DeleteRenderVideo)
	req := httptest.NewRequest(http.MethodDelete, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/videos/seg-001_seg-002", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rw.Code, rw.Body.String())
	}
	for _, key := range keys {
		if _, ok := store.puts[key]; ok {
			t.Errorf("artifact %q still present after delete", key)
		}
	}

	// Deleting again is idempotent: a retry after a lost response must succeed.
	rw = httptest.NewRecorder()
	r.ServeHTTP(rw, httptest.NewRequest(http.MethodDelete, "/api/jobs/"+j.ID.String()+"/renders/viral-60-clean/videos/seg-001_seg-002", nil))
	if rw.Code != http.StatusNoContent {
		t.Fatalf("repeat delete status = %d, want 204; body=%s", rw.Code, rw.Body.String())
	}
}

func TestDeleteRenderVideoRejectsUnknownVariant(t *testing.T) {
	repo := newFakeRepo()
	j := job.Job{ID: uuid.New(), Status: job.StatusDone, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Delete("/api/jobs/{id}/renders/{variant}/videos/{name}", h.DeleteRenderVideo)
	req := httptest.NewRequest(http.MethodDelete, "/api/jobs/"+j.ID.String()+"/renders/not-a-variant/videos/seg-001", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
}

func TestDeleteJobRemovesJobArtifactsAndDemo(t *testing.T) {
	repo := newFakeRepo()
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	j := job.Job{ID: uuid.New(), Status: job.StatusDone, Rules: rules.Default()}
	repo.jobs[j.ID] = j

	demoKey := "demos/" + j.ID.String() + ".dem"
	artifactKeys := []string{
		"jobs/" + j.ID.String() + "/recording/result.json",
		"jobs/" + j.ID.String() + "/renders/viral-60-clean/video.mp4",
	}
	if err := store.Put(demoKey, bytes.NewReader([]byte("PBDEMS2\x00"))); err != nil {
		t.Fatal(err)
	}
	for _, key := range artifactKeys {
		if err := store.Put(key, bytes.NewReader([]byte("artifact-bytes"))); err != nil {
			t.Fatal(err)
		}
	}
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Delete("/api/jobs/{id}", h.DeleteJob)
	req := httptest.NewRequest(http.MethodDelete, "/api/jobs/"+j.ID.String(), nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rw.Code, rw.Body.String())
	}
	if _, ok := repo.jobs[j.ID]; ok {
		t.Error("job still present in repo after delete")
	}
	for _, key := range append(artifactKeys, demoKey) {
		exists, err := store.Exists(key)
		if err != nil {
			t.Fatalf("Exists(%q) error = %v", key, err)
		}
		if exists {
			t.Errorf("artifact %q still present after delete", key)
		}
	}
	// The whole jobs/<id> tree must be gone, not just the seeded files.
	treeExists, err := store.Exists("jobs/" + j.ID.String())
	if err != nil {
		t.Fatalf("Exists(tree) error = %v", err)
	}
	if treeExists {
		t.Error("job artifact tree still present after delete")
	}

	// A repeat delete after success is a 404: the job is gone.
	rw = httptest.NewRecorder()
	r.ServeHTTP(rw, httptest.NewRequest(http.MethodDelete, "/api/jobs/"+j.ID.String(), nil))
	if rw.Code != http.StatusNotFound {
		t.Fatalf("repeat delete status = %d, want 404; body=%s", rw.Code, rw.Body.String())
	}
}

func TestDeleteJobRejectsInFlightJob(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecording, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	demoKey := "demos/" + j.ID.String() + ".dem"
	_ = store.Put(demoKey, bytes.NewReader([]byte("PBDEMS2\x00")))
	h := NewHandlers(repo, store, &fakeQueue{})

	r := chi.NewRouter()
	r.Delete("/api/jobs/{id}", h.DeleteJob)
	req := httptest.NewRequest(http.MethodDelete, "/api/jobs/"+j.ID.String(), nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
	if _, ok := repo.jobs[j.ID]; !ok {
		t.Error("job removed from repo despite 409")
	}
	if _, ok := store.puts[demoKey]; !ok {
		t.Error("demo removed from storage despite 409")
	}
}

func TestDeleteJobUnknownIDReturns404(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})

	r := chi.NewRouter()
	r.Delete("/api/jobs/{id}", h.DeleteJob)
	req := httptest.NewRequest(http.MethodDelete, "/api/jobs/"+uuid.New().String(), nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rw.Code, rw.Body.String())
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

// TestStreamJobEndpointsReturn501WhenRepositoryNotConfigured guards against a
// nil h.streamRepo (e.g. a deployment mode that never calls
// WithStreamRepository) crashing the handler instead of returning a clear
// error. See streamReady in stream_handlers.go.
func TestStreamJobEndpointsReturn501WhenRepositoryNotConfigured(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})
	r := Routes(h)

	req := httptest.NewRequest(http.MethodGet, "/api/stream-jobs", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusNotImplemented {
		t.Fatalf("list status = %d, want 501; body=%s", rw.Code, rw.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/stream-jobs/"+uuid.New().String(), nil)
	rw = httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusNotImplemented {
		t.Fatalf("get status = %d, want 501; body=%s", rw.Code, rw.Body.String())
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
	plan.FaceCropReviewed = true
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

	req = httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+created.ID+"/renders/"+plan.Variant, nil)
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

func TestPutStreamEditPlanCompletesBeforeWorkerCanClaimSameJob(t *testing.T) {
	id := uuid.New()
	baseRepo := newFakeStreamRepo()
	planA := streamclips.DefaultEditPlan()
	planA.Clips = []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 0, EndSeconds: 2, Title: "plan-a"}}
	planJSON, err := json.Marshal(planA)
	if err != nil {
		t.Fatal(err)
	}
	baseRepo.jobs[id] = streamclips.Job{
		ID: id, Status: streamclips.StatusReady, Probe: streamclips.SourceProbe{DurationSeconds: 10}, EditPlan: planJSON,
	}
	repo := &blockingSetStreamRepo{
		fakeStreamRepo: baseRepo,
		entered:        make(chan struct{}),
		release:        make(chan struct{}),
	}
	locks := streamclips.NewJobLocks()
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{},
		WithStreamRepository(repo), WithStreamJobLocks(locks),
	)
	r := Routes(h)
	planB := planA
	planB.Clips = append([]streamclips.ClipRange(nil), planA.Clips...)
	planB.Clips[0].Title = "plan-b"
	body, err := json.Marshal(planB)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPut, "/api/stream-jobs/"+id.String()+"/edit-plan", bytes.NewReader(body))
		rw := httptest.NewRecorder()
		r.ServeHTTP(rw, req)
		done <- rw
	}()
	<-repo.entered

	claimed := make(chan streamclips.EditPlan, 1)
	go func() {
		release := locks.Lock(id)
		defer release()
		job, _ := repo.Get(context.Background(), id)
		var plan streamclips.EditPlan
		_ = json.Unmarshal(job.EditPlan, &plan)
		claimed <- plan
	}()
	select {
	case <-claimed:
		t.Fatal("worker claim passed an HTTP edit-plan persistence in progress")
	case <-time.After(20 * time.Millisecond):
	}
	close(repo.release)
	rw := <-done
	if rw.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	select {
	case got := <-claimed:
		if got.Clips[0].Title != "plan-b" {
			t.Fatalf("claimed plan title = %q, want plan-b", got.Clips[0].Title)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not claim job after HTTP mutation committed")
	}
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

func TestPutStreamEditPlanRejectsClipPastProbedSourceDuration(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	id := uuid.New()
	streamRepo.jobs[id] = streamclips.Job{
		ID:         id,
		Status:     streamclips.StatusUploaded,
		SourcePath: streamclips.SourceKey(id),
		Probe:      streamclips.SourceProbe{DurationSeconds: 15.15},
	}
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{}, WithStreamRepository(streamRepo))
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 20}}
	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	r.Put("/api/stream-jobs/{id}/edit-plan", h.PutStreamEditPlan)
	req := httptest.NewRequest(http.MethodPut, "/api/stream-jobs/"+id.String()+"/edit-plan", bytes.NewReader(body))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), "exceeds source duration 15.150") {
		t.Fatalf("body = %s, want source-duration error", rw.Body.String())
	}
	if streamRepo.jobs[id].Status != streamclips.StatusUploaded {
		t.Fatalf("job status = %s, want uploaded because invalid plan was not saved", streamRepo.jobs[id].Status)
	}
}

func TestStartStreamRenderAcceptsLegacyTwentySecondPlanWithoutPersistingMigration(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	id := uuid.New()
	plan := streamclips.DefaultEditPlan()
	plan.FaceCropReviewed = true
	plan.Clips = []streamclips.ClipRange{{ID: "legacy", StartSeconds: 0, EndSeconds: 20}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	streamRepo.jobs[id] = streamclips.Job{
		ID:         id,
		Status:     streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(id),
		Probe:      streamclips.SourceProbe{DurationSeconds: 15.15},
		EditPlan:   planJSON,
	}
	h := NewHandlers(newFakeRepo(), store, queue, WithStreamRepository(streamRepo))
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+plan.Variant, nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	var saved streamclips.EditPlan
	if err := json.Unmarshal(streamRepo.jobs[id].EditPlan, &saved); err != nil {
		t.Fatal(err)
	}
	if got, want := saved.Clips[0].EndSeconds, 20.0; got != want {
		t.Fatalf("saved legacy end_seconds = %.2f, want unchanged %.2f", got, want)
	}
	if _, ok := store.puts[streamclips.EditPlanKey(id)]; ok {
		t.Fatal("render start persisted an in-memory legacy migration")
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeRenderStreamClip {
		t.Fatalf("queue = %#v, want one stream render", queue.enqueued)
	}
}

func TestStartStreamRenderRejectsApprovedPlanThatNeedsLegacyMigration(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	queue := &fakeQueue{}
	id := uuid.New()
	plan := streamclips.DefaultEditPlan()
	plan.FaceCropReviewed = true
	plan.Clips = []streamclips.ClipRange{{ID: "legacy", StartSeconds: 0, EndSeconds: 20}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	streamRepo.jobs[id] = streamclips.Job{
		ID:         id,
		Status:     streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(id),
		Probe:      streamclips.SourceProbe{DurationSeconds: 15.15},
		EditPlan:   planJSON,
	}
	h := NewHandlers(newFakeRepo(), newFakeStorage(), queue, WithStreamRepository(streamRepo))
	r := Routes(h)
	body := strings.NewReader(fmt.Sprintf(`{"expected_edit_plan_updated_at":%q}`, plan.UpdatedAt.Format(time.RFC3339Nano)))
	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+plan.Variant, body)
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict || !strings.Contains(rw.Body.String(), "requires migration after approval") {
		t.Fatalf("response = %d %s, want actionable 409", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("approved legacy plan queued %d render tasks, want zero", len(queue.enqueued))
	}
}

func TestStartStreamRenderRejectsBeforePersistingPartialLegacyMigration(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	id := uuid.New()
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{
		{ID: "legacy", StartSeconds: 0, EndSeconds: 20},
		{ID: "custom-overrun", StartSeconds: 0, EndSeconds: 19},
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	streamRepo.jobs[id] = streamclips.Job{
		ID:         id,
		Status:     streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(id),
		Probe:      streamclips.SourceProbe{DurationSeconds: 15.15},
		EditPlan:   planJSON,
	}
	h := NewHandlers(newFakeRepo(), store, queue, WithStreamRepository(streamRepo))
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+plan.Variant, nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	var persisted streamclips.EditPlan
	if err := json.Unmarshal(streamRepo.jobs[id].EditPlan, &persisted); err != nil {
		t.Fatal(err)
	}
	if gotLegacy, gotCustom := persisted.Clips[0].EndSeconds, persisted.Clips[1].EndSeconds; gotLegacy != 20 || gotCustom != 19 {
		t.Fatalf("persisted clip ends = [%.0f %.0f], want unchanged [20 19]", gotLegacy, gotCustom)
	}
	if _, ok := store.puts[streamclips.EditPlanKey(id)]; ok {
		t.Fatal("invalid partially migrated edit-plan artifact was written")
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("invalid plan enqueued work: %#v", queue.enqueued)
	}
}

func TestStartStreamRenderRejectsLegacyPlanWhollyPastEOFWithoutPersisting(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	id := uuid.New()
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "legacy", StartSeconds: 16, EndSeconds: 20}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	streamRepo.jobs[id] = streamclips.Job{
		ID:         id,
		Status:     streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(id),
		Probe:      streamclips.SourceProbe{DurationSeconds: 15.15},
		EditPlan:   planJSON,
	}
	h := NewHandlers(newFakeRepo(), store, queue, WithStreamRepository(streamRepo))
	r := Routes(h)
	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+plan.Variant, nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), "no clips") {
		t.Fatalf("body = %s, want no-clips migration error", rw.Body.String())
	}
	var persisted streamclips.EditPlan
	if err := json.Unmarshal(streamRepo.jobs[id].EditPlan, &persisted); err != nil {
		t.Fatal(err)
	}
	if gotStart, gotEnd := persisted.Clips[0].StartSeconds, persisted.Clips[0].EndSeconds; gotStart != 16 || gotEnd != 20 {
		t.Fatalf("persisted legacy clip = %.2f-%.2f, want unchanged 16-20", gotStart, gotEnd)
	}
	if _, ok := store.puts[streamclips.EditPlanKey(id)]; ok {
		t.Fatal("empty migrated edit-plan artifact was written")
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("empty plan enqueued work: %#v", queue.enqueued)
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

func TestStreamVideoServesCaptionedKeyFromRenderResult(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	id := uuid.New()
	const variant = "streamer-vertical-stack-40-60"
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusRendered, SourcePath: streamclips.SourceKey(id)}

	store := newFakeStorage()
	captionedKey, err := streamclips.RenderVideoKey(id, variant, "clip-1_captioned")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(captionedKey, strings.NewReader("captioned-bytes")); err != nil {
		t.Fatal(err)
	}
	result, err := streamclips.NewRenderResult(id, variant, []streamclips.VideoEntry{{ClipID: "clip-1", Key: captionedKey}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	resultKey, err := streamclips.RenderResultKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(resultKey, bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(newFakeRepo(), store, &fakeQueue{}, WithStreamRepository(streamRepo))
	r := Routes(h)

	req := httptest.NewRequest(http.MethodGet, "/api/stream-jobs/"+id.String()+"/renders/"+variant+"/videos/clip-1", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if got, want := rw.Body.String(), "captioned-bytes"; got != want {
		t.Fatalf("body = %q, want %q (captioned key from render result)", got, want)
	}
}

func TestStreamRenderArtifactsResolveAuthoritativeRevisionState(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	id := uuid.New()
	revisionID := uuid.New()
	const variant = streamclips.VariantStreamer4060
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusRendered, SourcePath: streamclips.SourceKey(id)}

	store := newFakeStorage()
	videoKey, err := streamclips.RenderRevisionVideoKey(id, variant, revisionID, "clip-1_captioned")
	if err != nil {
		t.Fatal(err)
	}
	galleryKey, err := streamclips.RenderRevisionGalleryKey(id, variant, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	resultKey, err := streamclips.RenderRevisionResultKey(id, variant, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(videoKey, strings.NewReader("revision-video")); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(galleryKey, strings.NewReader("revision-gallery")); err != nil {
		t.Fatal(err)
	}
	h := NewHandlers(newFakeRepo(), store, &fakeQueue{}, WithStreamRepository(streamRepo))
	state, err := streamclips.NewRenderState(id, variant, streamclips.StatusRendered, nil, "", []streamclips.VideoEntry{{ClipID: "clip-1", Key: videoKey}})
	if err != nil {
		t.Fatal(err)
	}
	state.ResultKey = resultKey
	state.GalleryKey = galleryKey
	state.ArtifactDir, err = streamclips.RenderRevisionPrefix(id, variant, revisionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.writeStreamRenderState(state); err != nil {
		t.Fatal(err)
	}
	r := Routes(h)

	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/api/stream-jobs/" + id.String() + "/renders/" + variant + "/videos/clip-1", want: "revision-video"},
		{path: "/api/stream-jobs/" + id.String() + "/renders/" + variant + "/gallery", want: "revision-gallery"},
	} {
		req := httptest.NewRequest(http.MethodGet, test.path, nil)
		rw := httptest.NewRecorder()
		r.ServeHTTP(rw, req)
		if rw.Code != http.StatusOK || rw.Body.String() != test.want {
			t.Fatalf("GET %s = %d %q, want 200 %q", test.path, rw.Code, rw.Body.String(), test.want)
		}
	}
}

func TestStreamVideoFallsBackToPlainKeyWithoutRenderResult(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	id := uuid.New()
	const variant = "streamer-vertical-stack-40-60"
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusRendered, SourcePath: streamclips.SourceKey(id)}

	store := newFakeStorage()
	plainKey, err := streamclips.RenderVideoKey(id, variant, "clip-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(plainKey, strings.NewReader("plain-bytes")); err != nil {
		t.Fatal(err)
	}

	h := NewHandlers(newFakeRepo(), store, &fakeQueue{}, WithStreamRepository(streamRepo))
	r := Routes(h)

	req := httptest.NewRequest(http.MethodGet, "/api/stream-jobs/"+id.String()+"/renders/"+variant+"/videos/clip-1", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if got, want := rw.Body.String(), "plain-bytes"; got != want {
		t.Fatalf("body = %q, want %q (conventional key fallback)", got, want)
	}
}

func TestCreateStreamJobFromURLAcquiresAndEnqueues(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	queue := &fakeQueue{}
	h := NewHandlers(newFakeRepo(), newFakeStorage(), queue,
		WithStreamRepository(streamRepo),
		WithCapabilities(Capabilities{YtdlpEnabled: true}),
	)
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs", strings.NewReader(`{"source_url":"https://clips.twitch.tv/SomeSlug","title":"clutch"}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != string(streamclips.StatusAcquiring) {
		t.Fatalf("status = %q, want %q", created.Status, streamclips.StatusAcquiring)
	}
	id, err := uuid.Parse(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, ok := streamRepo.jobs[id]
	if !ok {
		t.Fatal("stream job not created")
	}
	if job.SourceURL != "https://clips.twitch.tv/SomeSlug" {
		t.Fatalf("source url = %q", job.SourceURL)
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeStreamAcquire {
		t.Fatalf("queue = %#v", queue.enqueued)
	}
}

func TestCreateStreamJobFromURLMarksAcceptedPendingJobFailedWhenQueueDiscardsIt(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	queue := &fakeQueue{}
	h := NewHandlers(newFakeRepo(), newFakeStorage(), queue,
		WithStreamRepository(streamRepo),
		WithCapabilities(Capabilities{YtdlpEnabled: true}),
	)
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs", strings.NewReader(`{"source_url":"https://clips.twitch.tv/SomeSlug"}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.transitions) != 1 {
		t.Fatalf("queue transitions = %d, want 1", len(queue.transitions))
	}
	if err := queue.transitions[0](errors.New("inline queue task discarded during shutdown")); err != nil {
		t.Fatalf("discard transition error = %v", err)
	}
	for _, got := range streamRepo.jobs {
		if got.Status != streamclips.StatusFailed || !strings.Contains(got.FailureReason, "discarded during shutdown") {
			t.Fatalf("stream job after discard = status %q, reason %q; want failed discard reason", got.Status, got.FailureReason)
		}
	}
}

func TestCreateStreamJobFromURLRejectsInvalidURL(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{},
		WithStreamRepository(streamRepo),
		WithCapabilities(Capabilities{YtdlpEnabled: true}),
	)
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs", strings.NewReader(`{"source_url":"not-a-url"}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if len(streamRepo.jobs) != 0 {
		t.Fatalf("stream job created for an invalid url: %#v", streamRepo.jobs)
	}
}

func TestCreateStreamJobFromURLRejectsWhenYtdlpMissing(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{}, WithStreamRepository(streamRepo))
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs", strings.NewReader(`{"source_url":"https://clips.twitch.tv/SomeSlug"}`))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
	if len(streamRepo.jobs) != 0 {
		t.Fatalf("stream job created while yt-dlp is unconfigured: %#v", streamRepo.jobs)
	}
}

func TestStartStreamRenderAcceptsRegistryVariantsAndRejectsUnknown(t *testing.T) {
	for _, variant := range streamclips.VariantNames() {
		t.Run(variant, func(t *testing.T) {
			streamRepo := newFakeStreamRepo()
			id := uuid.New()
			plan := streamclips.DefaultEditPlan()
			plan.Variant = variant
			layout, ok := streamclips.VariantByName(variant)
			if !ok {
				t.Fatalf("variant %q missing from registry", variant)
			}
			plan.FaceCropReviewed = !layout.FullFrame
			planJSON, err := json.Marshal(plan)
			if err != nil {
				t.Fatal(err)
			}
			streamRepo.jobs[id] = streamclips.Job{
				ID: id, Status: streamclips.StatusReady,
				SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
			}
			queue := &fakeQueue{}
			h := NewHandlers(newFakeRepo(), newFakeStorage(), queue, WithStreamRepository(streamRepo))
			r := Routes(h)

			req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+variant, nil)
			rw := httptest.NewRecorder()
			r.ServeHTTP(rw, req)

			if rw.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
			}
			if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeRenderStreamClip {
				t.Fatalf("queue = %#v", queue.enqueued)
			}
		})
	}

	t.Run("variant must match edit plan", func(t *testing.T) {
		streamRepo := newFakeStreamRepo()
		id := uuid.New()
		plan := streamclips.DefaultEditPlan()
		planJSON, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		streamRepo.jobs[id] = streamclips.Job{
			ID: id, Status: streamclips.StatusReady,
			SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
		}
		queue := &fakeQueue{}
		h := NewHandlers(newFakeRepo(), newFakeStorage(), queue, WithStreamRepository(streamRepo))
		r := Routes(h)

		req := httptest.NewRequest(
			http.MethodPost,
			"/api/stream-jobs/"+id.String()+"/renders/"+streamclips.VariantStreamerLandscape16x9,
			nil,
		)
		rw := httptest.NewRecorder()
		r.ServeHTTP(rw, req)

		if rw.Code != http.StatusConflict || !strings.Contains(rw.Body.String(), "does not match edit plan variant") {
			t.Fatalf("response = %d %s, want actionable 409", rw.Code, rw.Body.String())
		}
		if len(queue.enqueued) != 0 {
			t.Fatalf("queued mismatched render tasks = %d, want zero", len(queue.enqueued))
		}
	})

	t.Run("facecam crop must be reviewed", func(t *testing.T) {
		streamRepo := newFakeStreamRepo()
		id := uuid.New()
		plan := streamclips.DefaultEditPlan()
		plan.FaceCropReviewed = false
		planJSON, err := json.Marshal(plan)
		if err != nil {
			t.Fatal(err)
		}
		streamRepo.jobs[id] = streamclips.Job{
			ID: id, Status: streamclips.StatusReady,
			SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
		}
		queue := &fakeQueue{}
		h := NewHandlers(newFakeRepo(), newFakeStorage(), queue, WithStreamRepository(streamRepo))
		r := Routes(h)

		req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+plan.Variant, nil)
		rw := httptest.NewRecorder()
		r.ServeHTTP(rw, req)

		if rw.Code != http.StatusConflict || !strings.Contains(rw.Body.String(), "facecam crop requires explicit review") {
			t.Fatalf("response = %d %s, want actionable 409", rw.Code, rw.Body.String())
		}
		if len(queue.enqueued) != 0 {
			t.Fatalf("queued render tasks = %d, want zero", len(queue.enqueued))
		}
	})

	t.Run("unknown variant lists valid names", func(t *testing.T) {
		streamRepo := newFakeStreamRepo()
		id := uuid.New()
		streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id)}
		h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{}, WithStreamRepository(streamRepo))
		r := Routes(h)

		req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/not-a-real-variant", nil)
		rw := httptest.NewRecorder()
		r.ServeHTTP(rw, req)

		if rw.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
		}
		for _, name := range streamclips.VariantNames() {
			if !strings.Contains(rw.Body.String(), name) {
				t.Errorf("error body missing valid variant %q: %s", name, rw.Body.String())
			}
		}
	})
}

func TestStartStreamRenderRequiresTheApprovedEditPlanRevision(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	id := uuid.New()
	plan := streamclips.DefaultEditPlan()
	plan.FaceCropReviewed = true
	plan.UpdatedAt = time.Date(2026, 7, 20, 20, 0, 0, 123, time.UTC)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	streamRepo.jobs[id] = streamclips.Job{
		ID: id, Status: streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
	}
	queue := &fakeQueue{}
	h := NewHandlers(newFakeRepo(), newFakeStorage(), queue, WithStreamRepository(streamRepo))
	r := Routes(h)

	staleBody := strings.NewReader(`{"expected_edit_plan_updated_at":"2026-07-20T19:59:59Z"}`)
	staleRequest := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+plan.Variant, staleBody)
	staleRequest.Header.Set("Content-Type", "application/json")
	staleResponse := httptest.NewRecorder()
	r.ServeHTTP(staleResponse, staleRequest)

	if staleResponse.Code != http.StatusConflict || !strings.Contains(staleResponse.Body.String(), "changed after approval") {
		t.Fatalf("stale response = %d %s, want actionable 409", staleResponse.Code, staleResponse.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("stale revision queued %d render tasks, want zero", len(queue.enqueued))
	}

	currentBody := strings.NewReader(fmt.Sprintf(`{"expected_edit_plan_updated_at":%q}`, plan.UpdatedAt.Format(time.RFC3339Nano)))
	currentRequest := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+plan.Variant, currentBody)
	currentRequest.Header.Set("Content-Type", "application/json")
	currentResponse := httptest.NewRecorder()
	r.ServeHTTP(currentResponse, currentRequest)

	if currentResponse.Code != http.StatusAccepted {
		t.Fatalf("current response = %d %s, want 202", currentResponse.Code, currentResponse.Body.String())
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("current revision queued %d render tasks, want one", len(queue.enqueued))
	}
}

func TestStartStreamRenderAcceptsAnEmptyOptionalJSONBody(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	id := uuid.New()
	plan := streamclips.DefaultEditPlan()
	plan.FaceCropReviewed = true
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	streamRepo.jobs[id] = streamclips.Job{
		ID: id, Status: streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
	}
	queue := &fakeQueue{}
	h := NewHandlers(newFakeRepo(), newFakeStorage(), queue, WithStreamRepository(streamRepo))
	r := Routes(h)
	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+plan.Variant, strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("response = %d %s, want 202", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("empty optional JSON body queued %d render tasks, want one", len(queue.enqueued))
	}
}

func TestStartStreamRenderMarksStateFailedWhenEnqueueFails(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	store := newFakeStorage()
	id := uuid.New()
	variant := streamclips.DefaultVariant().Name
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: reviewedDefaultEditPlanJSON(t)}
	queue := &fakeQueue{err: errors.New("inline queue is full")}
	h := NewHandlers(newFakeRepo(), store, queue, WithStreamRepository(streamRepo))
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+variant, nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rw.Code, rw.Body.String())
	}
	key, err := streamclips.RenderStateKey(id, variant)
	if err != nil {
		t.Fatalf("RenderStateKey error = %v", err)
	}
	raw, ok := store.puts[key]
	if !ok {
		t.Fatal("failed stream render state not written")
	}
	var state streamclips.RenderState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal render state: %v", err)
	}
	if state.Status != streamclips.StatusFailed {
		t.Fatalf("render state status = %q, want failed", state.Status)
	}
	if state.Error != "enqueue render: inline queue is full" {
		t.Fatalf("render state error = %q", state.Error)
	}
}

func TestStartStreamRenderMarksAcceptedPendingStateFailedWhenQueueDiscardsIt(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	store := newFakeStorage()
	id := uuid.New()
	variant := streamclips.DefaultVariant().Name
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: reviewedDefaultEditPlanJSON(t)}
	queue := &fakeQueue{}
	h := NewHandlers(newFakeRepo(), store, queue, WithStreamRepository(streamRepo))
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+variant, nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.transitions) != 1 {
		t.Fatalf("queue transitions = %d, want 1", len(queue.transitions))
	}
	if err := queue.transitions[0](errors.New("inline queue task discarded during shutdown")); err != nil {
		t.Fatalf("discard transition error = %v", err)
	}
	key, err := streamclips.RenderStateKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	var state streamclips.RenderState
	if err := json.Unmarshal(store.puts[key], &state); err != nil {
		t.Fatalf("unmarshal render state: %v", err)
	}
	if state.Status != streamclips.StatusFailed || !strings.Contains(state.Error, "discarded during shutdown") {
		t.Fatalf("state after discard = status %q, error %q; want failed discard reason", state.Status, state.Error)
	}
}

func TestStartStreamRenderKeepsRenderingStateWhenTaskIsDuplicate(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	store := newFakeStorage()
	id := uuid.New()
	variant := streamclips.DefaultVariant().Name
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: reviewedDefaultEditPlanJSON(t)}
	h := NewHandlers(newFakeRepo(), store, &fakeQueue{err: asynq.ErrDuplicateTask}, WithStreamRepository(streamRepo))
	existing, err := streamclips.NewRenderState(id, variant, streamclips.StatusRendering, nil, "", nil)
	if err != nil {
		t.Fatalf("NewRenderState error = %v", err)
	}
	if err := h.writeStreamRenderState(existing); err != nil {
		t.Fatalf("writeStreamRenderState error = %v", err)
	}
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+variant, nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	key, err := streamclips.RenderStateKey(id, variant)
	if err != nil {
		t.Fatalf("RenderStateKey error = %v", err)
	}
	raw, ok := store.puts[key]
	if !ok {
		t.Fatal("rendering stream state not written")
	}
	var state streamclips.RenderState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal render state: %v", err)
	}
	if state.Status != streamclips.StatusRendering || state.Error != "" {
		t.Fatalf("render state = status %q, error %q; want rendering without error", state.Status, state.Error)
	}
}

func TestStartStreamRenderPreservesRenderedStateWhenFinishedTaskIsStillDuplicate(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	store := newFakeStorage()
	id := uuid.New()
	variant := streamclips.DefaultVariant().Name
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusRendered, SourcePath: streamclips.SourceKey(id), EditPlan: reviewedDefaultEditPlanJSON(t)}
	h := NewHandlers(newFakeRepo(), store, &fakeQueue{err: asynq.ErrDuplicateTask}, WithStreamRepository(streamRepo))
	previous, err := streamclips.NewRenderState(id, variant, streamclips.StatusRendered, nil, "", nil)
	if err != nil {
		t.Fatalf("NewRenderState error = %v", err)
	}
	if err := h.writeStreamRenderState(previous); err != nil {
		t.Fatalf("writeStreamRenderState error = %v", err)
	}
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+variant, nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	key, err := streamclips.RenderStateKey(id, variant)
	if err != nil {
		t.Fatalf("RenderStateKey error = %v", err)
	}
	var state streamclips.RenderState
	if err := json.Unmarshal(store.puts[key], &state); err != nil {
		t.Fatalf("unmarshal render state: %v", err)
	}
	if state.Status != streamclips.StatusRendered {
		t.Fatalf("render state status = %q, want rendered", state.Status)
	}
}

func TestStartStreamRenderRejectsUnreviewedCaptionsEvenWithXAI(t *testing.T) {
	streamRepo := newFakeStreamRepo()
	id := uuid.New()
	plan := streamclips.DefaultEditPlan()
	plan.Captions = streamclips.CaptionsPlan{Enabled: true}
	plan.Clips = []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 0, EndSeconds: 2}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON, Probe: streamclips.SourceProbe{AudioCodec: "aac"}}
	queue := &fakeQueue{}
	h := NewHandlers(newFakeRepo(), newFakeStorage(), queue, WithStreamRepository(streamRepo), WithCapabilities(Capabilities{XAIEnabled: true}))
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+streamclips.DefaultVariant().Name, nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued %d tasks, want 0", len(queue.enqueued))
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
