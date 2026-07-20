package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/renderplan"
	"github.com/rechedev9/fragforge/internal/rules"
	"github.com/rechedev9/fragforge/internal/tasks"
)

func generateRouter(h *Handlers) chi.Router {
	r := chi.NewRouter()
	r.Post("/api/jobs/{id}/generate", h.StartGenerate)
	return r
}

func postGenerate(t *testing.T, h *Handlers, id uuid.UUID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/"+id.String()+"/generate", strings.NewReader(body))
	rw := httptest.NewRecorder()
	generateRouter(h).ServeHTTP(rw, req)
	return rw
}

type failingGenerateUpdateRepo struct {
	*fakeRepo
	err error
}

func (r failingGenerateUpdateRepo) UpdateStatus(context.Context, uuid.UUID, job.Status, string) error {
	return r.err
}

func TestStartGenerateEnqueuesRecordAndWritesIntent(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	rw := postGenerate(t, h, j.ID, `{"preset":"clean-pov-60","music":"phonk-01","edit":{"format":"short-9x16","killEffect":"velocity","transition":"whip","intro":true}}`)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}

	// Exactly one task is enqueued: the recording. The render is chained later by
	// the record worker, not enqueued here.
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(queue.enqueued))
	}
	if got := queue.enqueued[0].Type(); got != tasks.TypeRecordDemo {
		t.Fatalf("task type = %q, want %q", got, tasks.TypeRecordDemo)
	}
	var payload tasks.RecordDemoPayload
	if err := json.Unmarshal(queue.enqueued[0].Payload(), &payload); err != nil {
		t.Fatalf("unmarshal record payload: %v", err)
	}
	// clean-pov-60 records HUD-less, so it resolves to the "clean" HUD.
	if payload.HUDMode != "clean" {
		t.Fatalf("HUDMode = %q, want clean", payload.HUDMode)
	}
	if len(queue.options) != 1 || !hasAsynqOption(queue.options[0], "Unique(") {
		t.Fatalf("enqueue options = %#v, want Unique option for dedup", queue.options)
	}

	// The intent is persisted so the record worker can chain the render.
	raw, ok := store.puts[artifacts.GenerateIntentKey(j.ID)]
	if !ok {
		t.Fatalf("generate intent not written; puts=%v", keysOf(store))
	}
	var intent renderplan.GenerateIntent
	if err := json.Unmarshal(raw, &intent); err != nil {
		t.Fatalf("unmarshal intent: %v", err)
	}
	want := renderplan.GenerateIntent{
		Variant:  "clean-pov-60",
		MusicKey: "phonk-01",
		Edit: renderplan.EditRequest{
			Format:        renderplan.FormatShort9x16,
			KillEffect:    renderplan.KillEffectVelocity,
			Transition:    renderplan.TransitionWhip,
			Intro:         true,
			CoverStrategy: renderplan.CoverStrategyGenerated,
		},
	}
	if intent.ActiveRunID == uuid.Nil || intent.AcceptedAt.IsZero() {
		t.Fatalf("active generate marker = run %s accepted %s, want populated", intent.ActiveRunID, intent.AcceptedAt)
	}
	want.ActiveRunID = intent.ActiveRunID
	want.AcceptedAt = intent.AcceptedAt
	if intent != want {
		t.Fatalf("intent = %#v, want %#v", intent, want)
	}
	taskIntent, ok, err := tasks.GenerateIntentFromTask(queue.enqueued[0])
	if err != nil || !ok {
		t.Fatalf("GenerateIntentFromTask = (%#v, %v, %v)", taskIntent, ok, err)
	}
	if taskIntent != want {
		t.Fatalf("task intent = %#v, want %#v", taskIntent, want)
	}
}

func TestStartGenerateDuplicatePreservesAcceptedIntent(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{err: asynq.ErrDuplicateTask}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))
	existing := renderplan.GenerateIntent{
		Variant:  editor.PresetCleanPOV60,
		MusicKey: "first-track",
		Edit:     renderplan.DefaultEditRequest(),
	}
	if err := h.writeGenerateIntent(j.ID, existing); err != nil {
		t.Fatalf("writeGenerateIntent error = %v", err)
	}

	rw := postGenerate(t, h, j.ID, `{"preset":"clean-pov-60","music":"second-track","edit":{"intro":true}}`)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"duplicate":true`) {
		t.Fatalf("body missing duplicate marker: %s", rw.Body.String())
	}
	got, ok, err := h.readGenerateIntent(j.ID)
	if err != nil || !ok {
		t.Fatalf("readGenerateIntent = (%#v, %v, %v)", got, ok, err)
	}
	if got != existing {
		t.Fatalf("intent = %#v, want preserved %#v", got, existing)
	}
}

func TestStartGenerateEnqueueFailureDoesNotPublishIntent(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{err: errors.New("inline queue is full")}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	rw := postGenerate(t, h, j.ID, `{"preset":"viral-60-clean"}`)
	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rw.Code, rw.Body.String())
	}
	if _, ok := store.puts[artifacts.GenerateIntentKey(j.ID)]; ok {
		t.Fatal("intent published for rejected generate task")
	}
}

func TestStartGenerateMarksAcceptedPendingJobFailedWhenQueueDiscardsIt(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	rw := postGenerate(t, h, j.ID, `{"preset":"viral-60-clean"}`)
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
	intent, ok, err := h.readGenerateIntent(j.ID)
	if err != nil || !ok {
		t.Fatalf("readGenerateIntent = (%#v, %v, %v)", intent, ok, err)
	}
	if intent.ActiveRunID != uuid.Nil {
		t.Fatalf("active run after discard = %s, want cleared", intent.ActiveRunID)
	}
}

func TestStartGenerateDiscardKeepsRecoveryMarkerWhenFailureStatusDoesNotPersist(t *testing.T) {
	base := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	base.jobs[j.ID] = j
	repo := failingGenerateUpdateRepo{fakeRepo: base, err: errors.New("sqlite write failed")}
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	rw := postGenerate(t, h, j.ID, `{"preset":"viral-60-clean"}`)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	if got := len(queue.transitions); got != 1 {
		t.Fatalf("transitions = %d, want 1", got)
	}
	if err := queue.transitions[0](errors.New("shutdown discard")); err == nil {
		t.Fatal("discard transition error = nil, want durable status failure")
	}
	intent, ok, err := h.readGenerateIntent(j.ID)
	if err != nil || !ok {
		t.Fatalf("readGenerateIntent = (%#v, %v, %v)", intent, ok, err)
	}
	if intent.ActiveRunID == uuid.Nil {
		t.Fatal("discard cleared ActiveRunID without persisting failed job status")
	}
	if got := base.jobs[j.ID].Status; got != job.StatusParsed {
		t.Fatalf("job status = %s, want parsed after injected write failure", got)
	}
}

func TestStartGenerateRejectsOverlappingCaptureBeforeItCanReplaceIntent(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	firstResponse := postGenerate(t, h, j.ID, `{"preset":"clean-pov-60","music":"first-track"}`)
	if firstResponse.Code != http.StatusAccepted {
		t.Fatalf("first status = %d, want 202; body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	secondResponse := postGenerate(t, h, j.ID, `{"preset":"viral-60-clean","music":"second-track"}`)
	if secondResponse.Code != http.StatusConflict {
		t.Fatalf("second status = %d, want 409; body=%s", secondResponse.Code, secondResponse.Body.String())
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want only the first capture", len(queue.enqueued))
	}

	first, ok, err := tasks.GenerateIntentFromTask(queue.enqueued[0])
	if err != nil || !ok {
		t.Fatalf("first task intent = (%#v, %v, %v)", first, ok, err)
	}
	current, ok, err := h.readGenerateIntent(j.ID)
	if err != nil || !ok {
		t.Fatalf("current intent = (%#v, %v, %v)", current, ok, err)
	}
	if first.MusicKey != "first-track" || current.ActiveRunID != first.ActiveRunID || current.MusicKey != first.MusicKey {
		t.Fatalf("active intent changed after rejected overlap: task=%+v current=%+v", first, current)
	}
}

func TestStartGenerateRejectsWhileRenderStateIsActive(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))
	loadout, err := renderplan.LoadoutForVariant(editor.PresetViral60Clean)
	if err != nil {
		t.Fatal(err)
	}
	state, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:   j.ID,
		Loadout: loadout,
		Status:  renderplan.RenderVariantStatusRendering,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.writeRenderVariantState(state); err != nil {
		t.Fatal(err)
	}

	rw := postGenerate(t, h, j.ID, `{"preset":"viral-60-clean"}`)
	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0 while render is active", len(queue.enqueued))
	}
	if _, ok := store.puts[artifacts.GenerateIntentKey(j.ID)]; ok {
		t.Fatal("active render rejection published a generate intent")
	}
}

func TestStartGenerateVerticalKillfeedRequestsPortraitSafeCapture(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	rw := postGenerate(t, h, j.ID, `{"preset":"viral-60-clean","edit":{"format":"short-9x16"}}`)
	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	var payload tasks.RecordDemoPayload
	if err := json.Unmarshal(queue.enqueued[0].Payload(), &payload); err != nil {
		t.Fatalf("unmarshal record payload: %v", err)
	}
	if payload.HUDMode != "deathnotices" || !payload.PortraitSafeKillfeed {
		t.Fatalf("record payload = %#v, want portrait-safe deathnotices", payload)
	}
}

func TestStartGenerateRoundTripsBookendText(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	rw := postGenerate(t, h, j.ID, `{"preset":"clean-pov-60","edit":{"format":"short-9x16","killEffect":"velocity","transition":"whip","intro":true,"outro":true,"hook_text":true,"kill_counter":true,"intro_text":"Watch this ace","outro_text":"follow for more"}}`)

	if rw.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rw.Code, rw.Body.String())
	}
	raw, ok := store.puts[artifacts.GenerateIntentKey(j.ID)]
	if !ok {
		t.Fatalf("generate intent not written; puts=%v", keysOf(store))
	}
	var intent renderplan.GenerateIntent
	if err := json.Unmarshal(raw, &intent); err != nil {
		t.Fatalf("unmarshal intent: %v", err)
	}
	if intent.Edit.IntroText != "Watch this ace" || intent.Edit.OutroText != "follow for more" {
		t.Fatalf("edit bookend text = %q / %q, want round-tripped custom text", intent.Edit.IntroText, intent.Edit.OutroText)
	}
	if !intent.Edit.HookText || !intent.Edit.KillCounter {
		t.Fatalf("edit automatic text = hook %v / counter %v, want true / true", intent.Edit.HookText, intent.Edit.KillCounter)
	}
}

func TestStartGenerateRejectsOverlongBookendText(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	body := fmt.Sprintf(`{"preset":"viral-60-clean","edit":{"intro_text":"%s"}}`, strings.Repeat("a", 81))
	rw := postGenerate(t, h, j.ID, body)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
}

func TestStartGenerateRejectsUnknownPreset(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	rw := postGenerate(t, h, j.ID, `{"preset":"no-such-preset"}`)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
	if _, ok := store.puts[artifacts.GenerateIntentKey(j.ID)]; ok {
		t.Fatal("intent written for a rejected request")
	}
}

func TestStartGenerateRejectsUnknownSegmentID(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	plan.Segments = []killplan.Segment{{ID: "seg-001"}}
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	rw := postGenerate(t, h, j.ID, `{"preset":"viral-60-clean","segment_ids":["seg-404"]}`)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for unknown segment id; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
	if _, ok := store.puts[artifacts.GenerateIntentKey(j.ID)]; ok {
		t.Fatal("intent written for a rejected request")
	}
}

func TestStartGenerateRejectsJobNotReady(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	// A roster-scanned job has no kill plan yet, so it cannot record.
	j := job.Job{ID: uuid.New(), Status: job.StatusScanned, Rules: rules.Default()}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	rw := postGenerate(t, h, j.ID, `{"preset":"viral-60-clean"}`)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
}

func TestStartGenerateRejectsInvalidEdit(t *testing.T) {
	repo := newFakeRepo()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, newFakeStorage(), queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	rw := postGenerate(t, h, j.ID, `{"preset":"viral-60-clean","edit":{"killEffect":"glitch"}}`)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
}

func TestStartGenerateRejectsBadMusicKey(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))

	rw := postGenerate(t, h, j.ID, `{"preset":"viral-60-clean","music":"../evil"}`)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %d, want 0", len(queue.enqueued))
	}
	if _, ok := store.puts[artifacts.GenerateIntentKey(j.ID)]; ok {
		t.Fatal("intent written for a rejected request")
	}
}

func keysOf(s *fakeStorage) []string {
	out := make([]string, 0, len(s.puts))
	for k := range s.puts {
		out = append(out, k)
	}
	return out
}

func TestWorkbenchGenerateAdapterEnqueuesAndShowsProgress(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	plan.Demo.Map = "de_nuke"
	plan.Segments = []killplan.Segment{{ID: "seg-001", TickStart: 1, TickEnd: 2}}
	j := job.Job{ID: uuid.New(), Status: job.StatusParsed, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))
	r := Routes(h)

	form := "preset=clean-pov-60&music=&format=short-9x16&kill_effect=punch-in&transition=flash&intro=on"
	req := httptest.NewRequest(http.MethodPost, "/ui/jobs/"+j.ID.String()+"/generate", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeRecordDemo {
		t.Fatalf("queue = %#v, want one record task", queue.enqueued)
	}
	var payload tasks.RecordDemoPayload
	if err := json.Unmarshal(queue.enqueued[0].Payload(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.HUDMode != "clean" {
		t.Fatalf("HUDMode = %q, want clean for clean-pov-60", payload.HUDMode)
	}
	if _, ok := store.puts[artifacts.GenerateIntentKey(j.ID)]; !ok {
		t.Fatal("generate intent not written")
	}
	// The returned fragment shows the unified generating state and self-polls.
	body := rw.Body.String()
	for _, want := range []string{"Starting capture", `hx-trigger="every 3s"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("fragment missing %q: %s", want, body)
		}
	}
}

func TestWorkbenchNewGenerateIgnoresReadyStateFromPriorRun(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	plan := killplan.NewPlan()
	plan.Segments = []killplan.Segment{{ID: "seg-001", TickStart: 1, TickEnd: 2}}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j
	h := NewHandlers(repo, store, queue, WithCapabilities(Capabilities{RecordEnabled: true}))
	loadout, err := renderplan.LoadoutForVariant(editor.PresetViral60Clean)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := renderplan.NewRenderVariantStateForLoadout(renderplan.NewRenderVariantStateForLoadoutOptions{
		JobID:   j.ID,
		Loadout: loadout,
		Status:  renderplan.RenderVariantStatusReady,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.writeRenderVariantState(ready); err != nil {
		t.Fatal(err)
	}
	r := Routes(h)

	form := "preset=viral-60-clean&format=short-9x16&kill_effect=punch-in&transition=flash"
	req := httptest.NewRequest(http.MethodPost, "/ui/jobs/"+j.ID.String()+"/generate", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), "Starting capture") {
		t.Fatalf("new run did not replace prior ready phase: %s", rw.Body.String())
	}
	if strings.Contains(rw.Body.String(), "Short ready") {
		t.Fatalf("new run exposed prior ready state: %s", rw.Body.String())
	}
}

func TestWorkbenchShowsInlinePreviewWhenReady(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	plan := killplan.NewPlan()
	plan.Segments = []killplan.Segment{{ID: "seg-001", TickStart: 1, TickEnd: 2}}
	j := job.Job{ID: uuid.New(), Status: job.StatusRecorded, Rules: rules.Default(), KillPlan: &plan}
	repo.jobs[j.ID] = j

	// The persisted choice points the view at the generated variant, and a
	// finished render result makes it "ready" and lists the short to preview.
	intentBytes, err := json.Marshal(renderplan.GenerateIntent{Variant: editor.PresetViral60Clean, Edit: renderplan.DefaultEditRequest()})
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(artifacts.GenerateIntentKey(j.ID), bytes.NewReader(intentBytes))

	resultKey, err := artifacts.RenderVariantResultKey(j.ID, editor.PresetViral60Clean)
	if err != nil {
		t.Fatal(err)
	}
	result := editor.Result{
		Preset: editor.PresetViral60Clean,
		Shorts: []editor.ShortResult{{SegmentID: "seg-001", Title: "Ace on Nuke", DurationSeconds: 14, CoverPath: "cover.jpg"}},
	}
	resultBytes, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Put(resultKey, bytes.NewReader(resultBytes))

	h := NewHandlers(repo, store, &fakeQueue{})
	r := Routes(h)
	req := httptest.NewRequest(http.MethodGet, "/ui/jobs/"+j.ID.String(), nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	body := rw.Body.String()
	videoSrc := "/api/jobs/" + j.ID.String() + "/renders/" + editor.PresetViral60Clean + "/videos/seg-001"
	for _, want := range []string{"<video", videoSrc, "Ace on Nuke", "Download", "Short ready"} {
		if !strings.Contains(body, want) {
			t.Fatalf("preview missing %q: %s", want, body)
		}
	}
}
