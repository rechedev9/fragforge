package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/moments"
	"github.com/rechedev9/fragforge/internal/parser"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/tasks"
)

// in-memory fakes -------------------------------------------------------

type fakeRepo struct {
	mu             sync.Mutex
	jobs           map[uuid.UUID]*job.Job
	lastStatusSeen job.Status
}

func newFakeRepo() *fakeRepo { return &fakeRepo{jobs: map[uuid.UUID]*job.Job{}} }

// newFakeJobRepo seeds a fakeRepo with the given jobs, for tests that only
// need to exercise ProcessParseDemo/ProcessScanRoster against a fixed job.
func newFakeJobRepo(jobs ...job.Job) *fakeRepo {
	f := newFakeRepo()
	for i := range jobs {
		j := jobs[i]
		f.jobs[j.ID] = &j
	}
	return f
}

// only returns the single job seeded into the repo, for tests that seed
// exactly one.
func (f *fakeRepo) only() job.Job {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, j := range f.jobs {
		return *j
	}
	return job.Job{}
}

// lastStatus returns the most recent status passed to UpdateStatus.
func (f *fakeRepo) lastStatus() job.Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastStatusSeen
}
func (f *fakeRepo) Get(_ context.Context, id uuid.UUID) (job.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok {
		return job.Job{}, job.ErrNotFound
	}
	return *j, nil
}

// GetMeta mirrors the production lean read: it returns the job without its kill
// plan, so a test fails if the parser or compose worker ever relies on KillPlan
// from the metadata path.
func (f *fakeRepo) GetMeta(_ context.Context, id uuid.UUID) (job.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j, ok := f.jobs[id]
	if !ok {
		return job.Job{}, job.ErrNotFound
	}
	meta := *j
	meta.KillPlan = nil
	return meta, nil
}
func (f *fakeRepo) UpdateStatus(_ context.Context, id uuid.UUID, s job.Status, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastStatusSeen = s
	j := f.jobs[id]
	if j == nil {
		return job.ErrNotFound
	}
	j.Status = s
	j.FailureReason = reason
	return nil
}
func (f *fakeRepo) SetKillPlan(_ context.Context, id uuid.UUID, p killplan.Plan) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[id].KillPlan = &p
	return nil
}

type fakeStorage struct {
	mu    sync.Mutex
	files map[string][]byte
}

func newFakeStorage() *fakeStorage { return &fakeStorage{files: map[string][]byte{}} }
func (f *fakeStorage) Put(key string, r io.Reader) error {
	b, _ := io.ReadAll(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[key] = b
	return nil
}
func (f *fakeStorage) Open(key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.files[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (f *fakeStorage) Exists(key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.files[key]
	return ok, nil
}

// real demo helper ------------------------------------------------------

func loadRealDemo(t *testing.T) []byte {
	t.Helper()
	path := os.Getenv("TEST_DEMO_PATH")
	if path == "" {
		path = filepath.Join("..", "..", "testdata", "lavked-vs-tnc-m2-nuke.dem")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no test demo at %s: %v", path, err)
	}
	return b
}

func TestParserWorkerRunsAgainstRealDemo(t *testing.T) {
	demo := loadRealDemo(t)
	repo := newFakeRepo()
	store := newFakeStorage()

	id := uuid.New()
	repo.jobs[id] = &job.Job{
		ID:            id,
		Status:        job.StatusQueued,
		DemoPath:      "demos/test.dem",
		DemoSHA256:    "fake",
		TargetSteamID: "76561198148986856", // maaryy
		Rules:         rules.Default(),
	}
	_ = store.Put("demos/test.dem", bytes.NewReader(demo))

	w := NewParserWorker(repo, store)

	payload, _ := json.Marshal(tasks.ParseDemoPayload{JobID: id})
	if err := w.HandleParseDemo(context.Background(), asynq.NewTask(tasks.TypeParseDemo, payload)); err != nil {
		t.Fatalf("HandleParseDemo error = %v", err)
	}

	got := repo.jobs[id]
	if got.Status != job.StatusParsed {
		t.Errorf("Status = %v, want StatusParsed", got.Status)
	}
	if got.KillPlan == nil {
		t.Fatal("KillPlan = nil after successful parse")
	}
	if got.KillPlan.Stats.SegmentsCreated == 0 {
		t.Error("SegmentsCreated = 0, expected > 0 (parser regression)")
	}
	t.Logf("SegmentsCreated=%d TotalKillsTarget=%d KillsAfterFilters=%d",
		got.KillPlan.Stats.SegmentsCreated,
		got.KillPlan.Stats.TotalKillsTarget,
		got.KillPlan.Stats.KillsAfterFilters)
}

func TestParserWorkerScansRosterAgainstRealDemo(t *testing.T) {
	demo := loadRealDemo(t)
	repo := newFakeRepo()
	store := newFakeStorage()

	id := uuid.New()
	repo.jobs[id] = &job.Job{
		ID:         id,
		Status:     job.StatusQueued,
		DemoPath:   "demos/test.dem",
		DemoSHA256: "fake",
		Rules:      rules.Default(),
	}
	_ = store.Put("demos/test.dem", bytes.NewReader(demo))

	w := NewParserWorker(repo, store)
	payload, _ := json.Marshal(tasks.ScanRosterPayload{JobID: id})
	if err := w.HandleScanRoster(context.Background(), asynq.NewTask(tasks.TypeScanRoster, payload)); err != nil {
		t.Fatalf("HandleScanRoster error = %v", err)
	}

	if got := repo.jobs[id].Status; got != job.StatusScanned {
		t.Errorf("Status = %v, want StatusScanned", got)
	}
	raw, ok := store.files[artifacts.RosterKey(id)]
	if !ok {
		t.Fatal("roster artifact missing after scan")
	}
	var doc struct {
		Players []parser.PlayerStat `json:"players"`
		Match   parser.MatchInfo    `json:"match"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Players) == 0 {
		t.Fatal("roster has no players, expected > 0 (regression)")
	}
	if doc.Match.Map == "" {
		t.Error("roster artifact match.map is empty, want the demo header's map name")
	}
	if doc.Match.Rounds <= 0 {
		t.Errorf("roster artifact match.rounds = %d, want > 0", doc.Match.Rounds)
	}
}

func TestParserWorkerMarksJobFailedOnUnknownTarget(t *testing.T) {
	demo := loadRealDemo(t)
	repo := newFakeRepo()
	store := newFakeStorage()

	id := uuid.New()
	repo.jobs[id] = &job.Job{
		ID:            id,
		Status:        job.StatusQueued,
		DemoPath:      "demos/test.dem",
		TargetSteamID: "1", // not in demo
		Rules:         rules.Default(),
	}
	_ = store.Put("demos/test.dem", bytes.NewReader(demo))

	w := NewParserWorker(repo, store)
	payload, _ := json.Marshal(tasks.ParseDemoPayload{JobID: id})
	err := w.HandleParseDemo(context.Background(), asynq.NewTask(tasks.TypeParseDemo, payload))
	if err == nil {
		t.Fatal("HandleParseDemo error = nil, want non-nil so Asynq won't retry forever")
	}

	got := repo.jobs[id]
	if got.Status != job.StatusFailed {
		t.Errorf("Status = %v, want StatusFailed", got.Status)
	}
	if got.FailureReason == "" {
		t.Errorf("FailureReason empty, want a message")
	}
}

func TestProcessScanRoster_BadDemoMarksFailed(t *testing.T) {
	repo := newFakeJobRepo(job.Job{ID: uuid.New(), Status: job.StatusQueued, DemoPath: "demos/missing.dem"})
	store, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	w := NewParserWorker(repo, store)

	got := w.ProcessScanRoster(context.Background(), repo.only().ID)
	if got == nil {
		t.Fatalf("got nil error, want failure opening missing demo")
	}
	if repo.lastStatus() != job.StatusFailed {
		t.Errorf("got status %v, want %v", repo.lastStatus(), job.StatusFailed)
	}
}

func TestParserWorkerWritesMomentsArtifact(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	w := NewParserWorker(repo, store)
	id := uuid.New()
	plan := minimalKillPlan()

	key, err := w.writeMoments(id, plan)
	if err != nil {
		t.Fatalf("writeMoments error = %v", err)
	}
	if key != moments.ArtifactKey(id) {
		t.Fatalf("moments key = %q, want %q", key, moments.ArtifactKey(id))
	}
	var doc moments.Document
	if err := json.Unmarshal(store.files[key], &doc); err != nil {
		t.Fatal(err)
	}
	if got, want := doc.JobID, id; got != want {
		t.Fatalf("JobID = %s, want %s", got, want)
	}
	if len(doc.Moments) != 1 {
		t.Fatalf("moments len = %d, want 1", len(doc.Moments))
	}
}
