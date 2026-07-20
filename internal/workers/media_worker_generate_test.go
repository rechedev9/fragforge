package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/tasks"
)

type fakeEnqueuer struct {
	mu    sync.Mutex
	tasks []*asynq.Task
	err   error
}

type failingRecordGetRepo struct {
	*fakeRepo
	err error
}

type failingRecordGetAndStatusRepo struct {
	*failingRecordGetRepo
	statusErr error
}

func (r *failingRecordGetAndStatusRepo) UpdateStatus(context.Context, uuid.UUID, job.Status, string) error {
	return r.statusErr
}

type failOncePutStorage struct {
	*fakeStorage
	key   string
	err   error
	armed bool
}

type failNPutStorage struct {
	*fakeStorage
	key       string
	err       error
	remaining int
}

func (s *failNPutStorage) Put(key string, r io.Reader) error {
	if key == s.key && s.remaining > 0 {
		s.remaining--
		return s.err
	}
	return s.fakeStorage.Put(key, r)
}

func (s *failOncePutStorage) Put(key string, r io.Reader) error {
	if key == s.key && s.armed {
		s.armed = false
		return s.err
	}
	return s.fakeStorage.Put(key, r)
}

func (r *failingRecordGetRepo) Get(context.Context, uuid.UUID) (job.Job, error) {
	return job.Job{}, r.err
}

func (e *fakeEnqueuer) Enqueue(t *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	return e.enqueue(t, nil)
}

func (e *fakeEnqueuer) EnqueueWithTransition(t *asynq.Task, transition func(error) error, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	return e.enqueue(t, transition)
}

func (e *fakeEnqueuer) enqueue(t *asynq.Task, transition func(error) error) (*asynq.TaskInfo, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if transition != nil {
		if err := transition(e.err); err != nil {
			return nil, err
		}
	}
	if e.err != nil {
		return nil, e.err
	}
	e.tasks = append(e.tasks, t)
	return &asynq.TaskInfo{ID: "x"}, nil
}

// recordRunnerWithSegment returns a runner that materializes one segment clip
// and a recording result, mirroring the happy path of the recorder CLI.
func recordRunnerWithSegment(t *testing.T) *fakeRunner {
	t.Helper()
	return &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		outDir := argValue(args, "--out")
		if outDir == "" {
			t.Fatal("runner args missing --out")
		}
		scriptPath := filepath.Join(outDir, "recording.js")
		segmentPath := filepath.Join(outDir, "segments", "seg-001.mp4")
		if err := os.MkdirAll(filepath.Dir(segmentPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(scriptPath, []byte("script"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(segmentPath, []byte("clip"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := writeJSONFile(filepath.Join(outDir, "recording-result.json"), recordingResultWithSegment(scriptPath, segmentPath)); err != nil {
			t.Fatal(err)
		}
		return []byte("recorded"), nil
	}}
}

func parsedRecordJob(store *fakeStorage) (*fakeRepo, uuid.UUID) {
	repo := newFakeRepo()
	id := uuid.New()
	plan := minimalKillPlan()
	repo.jobs[id] = &job.Job{
		ID:       id,
		Status:   job.StatusParsed,
		DemoPath: "demos/test.dem",
		Rules:    rules.Default(),
		KillPlan: &plan,
	}
	_ = store.Put("demos/test.dem", bytes.NewReader([]byte("demo")))
	return repo, id
}

func newRecordWorkerForTest(repo *fakeRepo, store *fakeStorage, t *testing.T) *RecordWorker {
	t.Helper()
	w := NewRecordWorker(repo, store, RecordWorkerConfig{
		WorkDir:      t.TempDir(),
		RecorderPath: "zv-recorder",
		HLAEPath:     "HLAE.exe",
		CS2Path:      "cs2.exe",
	})
	w.runner = recordRunnerWithSegment(t)
	return w
}

func generateRecordTask(t *testing.T, id uuid.UUID, intent renderplan.GenerateIntent) *asynq.Task {
	t.Helper()
	task, err := tasks.NewGenerateRecordDemoTask(id, "", nil, false, intent)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestRecordWorkerChainsRenderFromGenerateIntent(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	edit := renderplan.EditRequest{
		Format:        renderplan.FormatShort9x16,
		KillEffect:    renderplan.KillEffectVelocity,
		Transition:    renderplan.TransitionWhip,
		Intro:         true,
		CoverStrategy: renderplan.CoverStrategyGenerated,
	}
	intent := renderplan.GenerateIntent{
		Variant:     editor.PresetCleanPOV60,
		MusicKey:    "phonk-01",
		Edit:        edit,
		ActiveRunID: uuid.New(),
	}
	putJSON(t, store, artifacts.GenerateIntentKey(id), intent)
	enq := &fakeEnqueuer{}
	w := newRecordWorkerForTest(repo, store, t)
	w.UseEnqueuer(enq)

	if err := w.HandleRecordDemo(context.Background(), generateRecordTask(t, id, intent)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusRecorded {
		t.Fatalf("Status = %s, want recorded", repo.jobs[id].Status)
	}
	if len(enq.tasks) != 1 {
		t.Fatalf("chained tasks = %d, want 1", len(enq.tasks))
	}
	if got := enq.tasks[0].Type(); got != tasks.TypeRenderVariant {
		t.Fatalf("chained task type = %q, want %q", got, tasks.TypeRenderVariant)
	}
	var payload tasks.RenderVariantPayload
	if err := json.Unmarshal(enq.tasks[0].Payload(), &payload); err != nil {
		t.Fatalf("unmarshal render payload: %v", err)
	}
	if payload.Variant != editor.PresetCleanPOV60 || payload.MusicKey != "phonk-01" || payload.Edit != edit {
		t.Fatalf("render payload = %#v, want variant=%s music=phonk-01 edit=%#v", payload, editor.PresetCleanPOV60, edit)
	}

	// The queued render state is surfaced immediately so the UI does not flash
	// back to "not started" while the render worker spins up.
	raw, ok := store.files[mustRenderVariantStatusKey(t, id, editor.PresetCleanPOV60)]
	if !ok {
		t.Fatal("queued render state not written")
	}
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal render state: %v", err)
	}
	if state.Status != renderplan.RenderVariantStatusQueued {
		t.Fatalf("render state status = %q, want queued", state.Status)
	}
	var completed renderplan.GenerateIntent
	completedRaw := store.files[artifacts.GenerateIntentKey(id)]
	if err := json.Unmarshal(completedRaw, &completed); err != nil {
		t.Fatalf("unmarshal completed intent %q: %v", completedRaw, err)
	}
	if completed.ActiveRunID != uuid.Nil {
		t.Fatalf("active run after render admission = %s, want cleared", completed.ActiveRunID)
	}
}

func TestRecordWorkerMarksGuidedGenerateFailedWhenJobLoadFails(t *testing.T) {
	store := newFakeStorage()
	base, id := parsedRecordJob(store)
	wantErr := errors.New("sqlite read failed")
	repo := &failingRecordGetRepo{fakeRepo: base, err: wantErr}
	w := NewRecordWorker(repo, store, RecordWorkerConfig{})
	intent := renderplan.GenerateIntent{
		Variant:     editor.PresetViral60Clean,
		Edit:        renderplan.DefaultEditRequest(),
		ActiveRunID: uuid.New(),
	}
	putJSON(t, store, artifacts.GenerateIntentKey(id), intent)

	err := w.HandleRecordDemo(context.Background(), generateRecordTask(t, id, intent))
	if !errors.Is(err, wantErr) {
		t.Fatalf("HandleRecordDemo error = %v, want %v", err, wantErr)
	}
	got := base.jobs[id]
	if got.Status != job.StatusFailed || !strings.Contains(got.FailureReason, wantErr.Error()) {
		t.Fatalf("job after load failure = status %s reason %q; want failed load error", got.Status, got.FailureReason)
	}
}

func TestRecordWorkerKeepsRecoveryMarkerWhenTerminalFailureDoesNotPersist(t *testing.T) {
	store := newFakeStorage()
	base, id := parsedRecordJob(store)
	loadErr := errors.New("sqlite read failed")
	statusErr := errors.New("sqlite write failed")
	repo := &failingRecordGetAndStatusRepo{
		failingRecordGetRepo: &failingRecordGetRepo{fakeRepo: base, err: loadErr},
		statusErr:            statusErr,
	}
	w := NewRecordWorker(repo, store, RecordWorkerConfig{})
	intent := renderplan.GenerateIntent{
		Variant:     editor.PresetViral60Clean,
		Edit:        renderplan.DefaultEditRequest(),
		ActiveRunID: uuid.New(),
	}
	putJSON(t, store, artifacts.GenerateIntentKey(id), intent)

	err := w.HandleRecordDemo(context.Background(), generateRecordTask(t, id, intent))
	if !errors.Is(err, loadErr) {
		t.Fatalf("HandleRecordDemo error = %v, want %v", err, loadErr)
	}
	var current renderplan.GenerateIntent
	if err := json.Unmarshal(store.files[artifacts.GenerateIntentKey(id)], &current); err != nil {
		t.Fatalf("unmarshal current intent: %v", err)
	}
	if current.ActiveRunID != intent.ActiveRunID {
		t.Fatalf("ActiveRunID = %s, want recovery marker %s", current.ActiveRunID, intent.ActiveRunID)
	}
	if got := base.jobs[id].Status; got != job.StatusParsed {
		t.Fatalf("job status = %s, want parsed after injected failure", got)
	}
}

func TestRecordWorkerOlderCaptureDoesNotClearNewerActiveRun(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	oldIntent := renderplan.GenerateIntent{
		Variant:     editor.PresetViral60Clean,
		Edit:        renderplan.DefaultEditRequest(),
		ActiveRunID: uuid.New(),
	}
	newIntent := oldIntent
	newIntent.ActiveRunID = uuid.New()
	putJSON(t, store, artifacts.GenerateIntentKey(id), newIntent)
	w := newRecordWorkerForTest(repo, store, t)
	enqueuer := &fakeEnqueuer{}
	w.UseEnqueuer(enqueuer)

	if err := w.HandleRecordDemo(context.Background(), generateRecordTask(t, id, oldIntent)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	var got renderplan.GenerateIntent
	if err := json.Unmarshal(store.files[artifacts.GenerateIntentKey(id)], &got); err != nil {
		t.Fatalf("unmarshal current intent: %v", err)
	}
	if got.ActiveRunID != newIntent.ActiveRunID {
		t.Fatalf("active run = %s, want newer %s preserved", got.ActiveRunID, newIntent.ActiveRunID)
	}
	if len(enqueuer.tasks) != 0 {
		t.Fatalf("stale capture enqueued %d render task(s), want 0", len(enqueuer.tasks))
	}
	if _, ok := store.files[mustRenderVariantStatusKey(t, id, oldIntent.Variant)]; ok {
		t.Fatal("stale capture overwrote the newer run's render state")
	}
}

func TestRecordWorkerMarksChainedRenderFailedWhenEnqueueFails(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	intent := renderplan.GenerateIntent{
		Variant:     editor.PresetViral60Clean,
		Edit:        renderplan.DefaultEditRequest(),
		ActiveRunID: uuid.New(),
	}
	putJSON(t, store, artifacts.GenerateIntentKey(id), intent)
	enqueueErr := errors.New("inline queue is full")
	w := newRecordWorkerForTest(repo, store, t)
	w.UseEnqueuer(&fakeEnqueuer{err: enqueueErr})

	if err := w.HandleRecordDemo(context.Background(), generateRecordTask(t, id, intent)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusRecorded {
		t.Fatalf("Status = %s, want recorded", repo.jobs[id].Status)
	}
	raw, ok := store.files[mustRenderVariantStatusKey(t, id, intent.Variant)]
	if !ok {
		t.Fatal("failed render state not written")
	}
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal render state: %v", err)
	}
	if state.Status != renderplan.RenderVariantStatusFailed {
		t.Fatalf("render state status = %q, want failed", state.Status)
	}
	if state.Error != "enqueue render: "+enqueueErr.Error() {
		t.Fatalf("render state error = %q", state.Error)
	}
	var completed renderplan.GenerateIntent
	completedRaw := store.files[artifacts.GenerateIntentKey(id)]
	if err := json.Unmarshal(completedRaw, &completed); err != nil {
		t.Fatalf("unmarshal completed intent %q: %v", completedRaw, err)
	}
	if completed.ActiveRunID != uuid.Nil {
		t.Fatalf("active run after rejected render admission = %s, want cleared", completed.ActiveRunID)
	}
}

func TestRecordWorkerDoesNotStrandQueuedStateWhenAcceptedTransitionFails(t *testing.T) {
	baseStore := newFakeStorage()
	repo, id := parsedRecordJob(baseStore)
	intent := renderplan.GenerateIntent{
		Variant:     editor.PresetViral60Clean,
		Edit:        renderplan.DefaultEditRequest(),
		ActiveRunID: uuid.New(),
	}
	putJSON(t, baseStore, artifacts.GenerateIntentKey(id), intent)
	writeErr := errors.New("intent completion write failed")
	store := &failOncePutStorage{
		fakeStorage: baseStore,
		key:         artifacts.GenerateIntentKey(id),
		err:         writeErr,
		armed:       true,
	}
	w := NewRecordWorker(repo, store, RecordWorkerConfig{
		WorkDir:      t.TempDir(),
		RecorderPath: "zv-recorder",
		HLAEPath:     "HLAE.exe",
		CS2Path:      "cs2.exe",
	})
	w.runner = recordRunnerWithSegment(t)
	enqueuer := &fakeEnqueuer{}
	w.UseEnqueuer(enqueuer)

	if err := w.HandleRecordDemo(context.Background(), generateRecordTask(t, id, intent)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if len(enqueuer.tasks) != 0 {
		t.Fatalf("accepted tasks = %d, want transition failure to reject handoff", len(enqueuer.tasks))
	}
	raw := baseStore.files[mustRenderVariantStatusKey(t, id, intent.Variant)]
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal render state: %v", err)
	}
	if state.Status != renderplan.RenderVariantStatusFailed {
		t.Fatalf("render state status = %q, want failed", state.Status)
	}
	if !strings.Contains(state.Error, writeErr.Error()) {
		t.Fatalf("render state error = %q, want %q", state.Error, writeErr)
	}
	var completed renderplan.GenerateIntent
	if err := json.Unmarshal(baseStore.files[artifacts.GenerateIntentKey(id)], &completed); err != nil {
		t.Fatalf("unmarshal completed intent: %v", err)
	}
	if completed.ActiveRunID != uuid.Nil {
		t.Fatalf("active run after failed handoff = %s, want cleared", completed.ActiveRunID)
	}
}

func TestRecordWorkerKeepsMarkerWhenQueuedAndFailedStateWritesBothFail(t *testing.T) {
	baseStore := newFakeStorage()
	repo, id := parsedRecordJob(baseStore)
	intent := renderplan.GenerateIntent{
		Variant:     editor.PresetViral60Clean,
		Edit:        renderplan.DefaultEditRequest(),
		ActiveRunID: uuid.New(),
	}
	putJSON(t, baseStore, artifacts.GenerateIntentKey(id), intent)
	stateKey := mustRenderVariantStatusKey(t, id, intent.Variant)
	store := &failNPutStorage{
		fakeStorage: baseStore,
		key:         stateKey,
		err:         errors.New("render state storage unavailable"),
		remaining:   2,
	}
	w := NewRecordWorker(repo, store, RecordWorkerConfig{
		WorkDir:      t.TempDir(),
		RecorderPath: "zv-recorder",
		HLAEPath:     "HLAE.exe",
		CS2Path:      "cs2.exe",
	})
	w.runner = recordRunnerWithSegment(t)
	enqueuer := &fakeEnqueuer{}
	w.UseEnqueuer(enqueuer)

	if err := w.HandleRecordDemo(context.Background(), generateRecordTask(t, id, intent)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if len(enqueuer.tasks) != 0 {
		t.Fatalf("accepted tasks = %d, want 0 after transition failure", len(enqueuer.tasks))
	}
	if _, ok := baseStore.files[stateKey]; ok {
		t.Fatal("render state unexpectedly persisted despite two injected failures")
	}
	var current renderplan.GenerateIntent
	if err := json.Unmarshal(baseStore.files[artifacts.GenerateIntentKey(id)], &current); err != nil {
		t.Fatalf("unmarshal current intent: %v", err)
	}
	if current.ActiveRunID != intent.ActiveRunID {
		t.Fatalf("ActiveRunID = %s, want recovery marker %s", current.ActiveRunID, intent.ActiveRunID)
	}
}

func TestRecordWorkerKeepsChainedRenderQueuedWhenTaskIsDuplicate(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	intent := renderplan.GenerateIntent{
		Variant: editor.PresetViral60Clean,
		Edit:    renderplan.DefaultEditRequest(),
	}
	w := newRecordWorkerForTest(repo, store, t)
	if err := w.writeQueuedRenderState(id, intent.Variant); err != nil {
		t.Fatalf("writeQueuedRenderState error = %v", err)
	}
	w.UseEnqueuer(&fakeEnqueuer{err: asynq.ErrDuplicateTask})

	if err := w.HandleRecordDemo(context.Background(), generateRecordTask(t, id, intent)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	raw, ok := store.files[mustRenderVariantStatusKey(t, id, intent.Variant)]
	if !ok {
		t.Fatal("queued render state not written")
	}
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal render state: %v", err)
	}
	if state.Status != renderplan.RenderVariantStatusQueued || state.Error != "" {
		t.Fatalf("render state = status %q, error %q; want queued without error", state.Status, state.Error)
	}
}

func TestRecordWorkerPreservesReadyStateWhenFinishedTaskIsStillDuplicate(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	intent := renderplan.GenerateIntent{
		Variant: editor.PresetViral60Clean,
		Edit:    renderplan.DefaultEditRequest(),
	}
	loadout, err := renderplan.LoadoutForVariant(intent.Variant)
	if err != nil {
		t.Fatalf("LoadoutForVariant error = %v", err)
	}
	ready, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:   id,
		Loadout: loadout,
		Status:  renderplan.RenderVariantStatusReady,
	})
	if err != nil {
		t.Fatalf("NewRenderVariantStateForLoadout error = %v", err)
	}
	putJSON(t, store, mustRenderVariantStatusKey(t, id, intent.Variant), ready)

	w := newRecordWorkerForTest(repo, store, t)
	w.UseEnqueuer(&fakeEnqueuer{err: asynq.ErrDuplicateTask})
	if err := w.HandleRecordDemo(context.Background(), generateRecordTask(t, id, intent)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}

	raw := store.files[mustRenderVariantStatusKey(t, id, intent.Variant)]
	var state renderplan.RenderVariantState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal render state: %v", err)
	}
	if state.Status != renderplan.RenderVariantStatusReady {
		t.Fatalf("render state status = %q, want ready", state.Status)
	}
}

func TestRecordWorkerWithoutIntentDoesNotChain(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	enq := &fakeEnqueuer{}
	w := newRecordWorkerForTest(repo, store, t)
	w.UseEnqueuer(enq)

	if err := w.HandleRecordDemo(context.Background(), recordTask(t, id)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusRecorded {
		t.Fatalf("Status = %s, want recorded", repo.jobs[id].Status)
	}
	if len(enq.tasks) != 0 {
		t.Fatalf("chained tasks = %d, want 0 without an intent", len(enq.tasks))
	}
}

func TestRecordWorkerPlainRecordIgnoresStaleGenerateArtifact(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	putJSON(t, store, artifacts.GenerateIntentKey(id), renderplan.GenerateIntent{
		Variant: editor.PresetViral60Clean,
		Edit:    renderplan.DefaultEditRequest(),
	})
	enq := &fakeEnqueuer{}
	w := newRecordWorkerForTest(repo, store, t)
	w.UseEnqueuer(enq)

	if err := w.HandleRecordDemo(context.Background(), recordTask(t, id)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if len(enq.tasks) != 0 {
		t.Fatalf("chained tasks = %d, want 0 for plain record with stale artifact", len(enq.tasks))
	}
}

func TestRecordWorkerNilEnqueuerSkipsChaining(t *testing.T) {
	store := newFakeStorage()
	repo, id := parsedRecordJob(store)
	intent := renderplan.GenerateIntent{
		Variant: editor.PresetViral60Clean,
		Edit:    renderplan.DefaultEditRequest(),
	}
	// No UseEnqueuer: a worker built without a queue must skip chaining cleanly.
	w := newRecordWorkerForTest(repo, store, t)

	if err := w.HandleRecordDemo(context.Background(), generateRecordTask(t, id, intent)); err != nil {
		t.Fatalf("HandleRecordDemo error = %v", err)
	}
	if repo.jobs[id].Status != job.StatusRecorded {
		t.Fatalf("Status = %s, want recorded", repo.jobs[id].Status)
	}
}
