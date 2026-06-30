package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
			Format:     renderplan.FormatShort9x16,
			KillEffect: renderplan.KillEffectVelocity,
			Transition: renderplan.TransitionWhip,
			Intro:      true,
		},
	}
	if intent != want {
		t.Fatalf("intent = %#v, want %#v", intent, want)
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
