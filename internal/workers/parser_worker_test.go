package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/reche/zackvideo/internal/job"
	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
	"github.com/reche/zackvideo/internal/tasks"
)

// in-memory fakes -------------------------------------------------------

type fakeRepo struct {
	jobs map[uuid.UUID]*job.Job
}

func newFakeRepo() *fakeRepo { return &fakeRepo{jobs: map[uuid.UUID]*job.Job{}} }
func (f *fakeRepo) Get(_ context.Context, id uuid.UUID) (job.Job, error) {
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
	j, ok := f.jobs[id]
	if !ok {
		return job.Job{}, job.ErrNotFound
	}
	meta := *j
	meta.KillPlan = nil
	return meta, nil
}
func (f *fakeRepo) UpdateStatus(_ context.Context, id uuid.UUID, s job.Status, reason string) error {
	j := f.jobs[id]
	if j == nil {
		return job.ErrNotFound
	}
	j.Status = s
	j.FailureReason = reason
	return nil
}
func (f *fakeRepo) SetKillPlan(_ context.Context, id uuid.UUID, p killplan.Plan) error {
	f.jobs[id].KillPlan = &p
	return nil
}

type fakeStorage struct{ files map[string][]byte }

func newFakeStorage() *fakeStorage { return &fakeStorage{files: map[string][]byte{}} }
func (f *fakeStorage) Put(key string, r io.Reader) error {
	b, _ := io.ReadAll(r)
	f.files[key] = b
	return nil
}
func (f *fakeStorage) Open(key string) (io.ReadCloser, error) {
	b, ok := f.files[key]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (f *fakeStorage) Exists(key string) (bool, error) {
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
