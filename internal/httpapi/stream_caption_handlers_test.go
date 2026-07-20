package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

func TestStreamCaptionCandidatesPostQueuesDurableGenerationAndGetReturnsIt(t *testing.T) {
	h, _, store, queue, id, _ := newCaptionHTTPFixture(t)
	router := Routes(h)

	request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/captions", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("POST status = %d, want 202; body=%s", response.Code, response.Body.String())
	}
	var queued streamclips.CaptionCandidateState
	if err := json.Unmarshal(response.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	if queued.Status != streamclips.CaptionCandidatesQueued || queued.GenerationID == uuid.Nil || queued.Clips == nil {
		t.Fatalf("queued state = %+v", queued)
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeGenerateStreamCaptions {
		t.Fatalf("queue = %#v, want one caption generation", queue.enqueued)
	}
	if got, err := tasks.StreamCaptionGenerationFromTask(queue.enqueued[0]); err != nil || got != queued.GenerationID {
		t.Fatalf("task generation = %s, %v; want %s", got, err, queued.GenerationID)
	}
	if _, ok := store.puts[streamclips.CaptionCandidatesKey(id)]; !ok {
		t.Fatal("queued caption state was not persisted")
	}

	request = httptest.NewRequest(http.MethodGet, "/api/stream-jobs/"+id.String()+"/captions", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var fetched streamclips.CaptionCandidateState
	if err := json.Unmarshal(response.Body.Bytes(), &fetched); err != nil {
		t.Fatal(err)
	}
	if fetched.GenerationID != queued.GenerationID || fetched.Status != streamclips.CaptionCandidatesQueued || fetched.Clips == nil {
		t.Fatalf("fetched state = %+v, want queued generation %s", fetched, queued.GenerationID)
	}
}

func TestReviewStreamCaptionCandidatesPersistsReviewedWords(t *testing.T) {
	h, repo, _, _, id, plan := newCaptionHTTPFixture(t)
	generationID := writeReviewableCaptionState(t, h, repo.jobs[id], plan, streamclips.CaptionClipReviewRequired)
	body := reviewBody(t, generationID, map[string]any{
		"clip_id": "clip-1",
		"words":   []streamclips.CaptionWord{{Word: "hola", StartSeconds: 0.1, EndSeconds: 0.6}},
	})

	request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/captions/review", bytes.NewReader(body))
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("review status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var saved streamclips.EditPlan
	if err := json.Unmarshal(repo.jobs[id].EditPlan, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.Clips[0].CaptionReviewed || len(saved.Clips[0].CaptionWords) != 1 || saved.Clips[0].CaptionWords[0].Word != "hola" {
		t.Fatalf("saved reviewed clip = %+v", saved.Clips[0])
	}
	state, ok, err := h.readStreamCaptionState(id)
	if err != nil || !ok {
		t.Fatalf("read caption state = ok %v, err %v", ok, err)
	}
	if state.Status != streamclips.CaptionCandidatesReady || state.Clips[0].Status != streamclips.CaptionClipReady {
		t.Fatalf("reviewed state = %+v", state)
	}

	revisedBody := reviewBody(t, generationID, map[string]any{
		"clip_id": "clip-1",
		"words":   []streamclips.CaptionWord{{Word: "buenas", StartSeconds: 0.1, EndSeconds: 0.6}},
	})
	request = httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/captions/review", bytes.NewReader(revisedBody))
	response = httptest.NewRecorder()
	Routes(h).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("revise status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(repo.jobs[id].EditPlan, &saved); err != nil {
		t.Fatal(err)
	}
	if got, want := saved.Clips[0].CaptionWords[0].Word, "buenas"; got != want {
		t.Fatalf("revised word = %q, want %q", got, want)
	}
}

func TestReviewStreamCaptionCandidatesPersistsNoSpeechDecision(t *testing.T) {
	h, repo, _, _, id, plan := newCaptionHTTPFixture(t)
	generationID := writeReviewableCaptionState(t, h, repo.jobs[id], plan, streamclips.CaptionClipNoSpeech)
	body := reviewBody(t, generationID, map[string]any{"clip_id": "clip-1", "no_speech": true})

	request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/captions/review", bytes.NewReader(body))
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("review status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var saved streamclips.EditPlan
	if err := json.Unmarshal(repo.jobs[id].EditPlan, &saved); err != nil {
		t.Fatal(err)
	}
	if !saved.Clips[0].CaptionReviewed || len(saved.Clips[0].CaptionWords) != 0 {
		t.Fatalf("saved no-speech clip = %+v", saved.Clips[0])
	}
}

func TestReviewStreamCaptionCandidatesRejectsReplacedGeneration(t *testing.T) {
	h, repo, _, _, id, plan := newCaptionHTTPFixture(t)
	writeReviewableCaptionState(t, h, repo.jobs[id], plan, streamclips.CaptionClipReviewRequired)
	body := reviewBody(t, uuid.New(), map[string]any{
		"clip_id": "clip-1",
		"words":   []streamclips.CaptionWord{{Word: "viejo", StartSeconds: 0.1, EndSeconds: 0.6}},
	})

	request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/captions/review", bytes.NewReader(body))
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("review status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
}

func TestReviewStreamCaptionCandidatesRejectsStaleClipFingerprint(t *testing.T) {
	h, repo, _, _, id, plan := newCaptionHTTPFixture(t)
	generationID := writeReviewableCaptionState(t, h, repo.jobs[id], plan, streamclips.CaptionClipReviewRequired)
	plan.Clips[0].StartSeconds = 10
	plan.Clips[0].EndSeconds = 12
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	job := repo.jobs[id]
	job.EditPlan = planJSON
	repo.jobs[id] = job
	body := reviewBody(t, generationID, map[string]any{
		"clip_id": "clip-1",
		"words":   []streamclips.CaptionWord{{Word: "stale", StartSeconds: 0.1, EndSeconds: 0.6}},
	})

	request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/captions/review", bytes.NewReader(body))
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("review status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
}

func TestPutStreamEditPlanInvalidatesReviewedCaptionsWhenClipAudioChanges(t *testing.T) {
	h, repo, _, _, id, plan := newCaptionHTTPFixture(t)
	plan.Clips[0].CaptionWords = []streamclips.CaptionWord{{Word: "hola", StartSeconds: 0.1, EndSeconds: 0.6}}
	plan.Clips[0].CaptionReviewed = true
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	job := repo.jobs[id]
	job.EditPlan = planJSON
	repo.jobs[id] = job

	plan.Clips[0].StartSeconds = 10
	plan.Clips[0].EndSeconds = 12
	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/stream-jobs/"+id.String()+"/edit-plan", bytes.NewReader(body))
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var saved streamclips.EditPlan
	if err := json.Unmarshal(repo.jobs[id].EditPlan, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Clips[0].CaptionReviewed || len(saved.Clips[0].CaptionWords) != 0 {
		t.Fatalf("stale review was retained: %+v", saved.Clips[0])
	}
}

func TestStartStreamRenderAcceptsReviewedCaptionsWithoutXAI(t *testing.T) {
	h, repo, _, queue, id, plan := newCaptionHTTPFixture(t)
	plan.Clips[0].CaptionWords = []streamclips.CaptionWord{{Word: "hola", StartSeconds: 0.1, EndSeconds: 0.6}}
	plan.Clips[0].CaptionReviewed = true
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	job := repo.jobs[id]
	job.EditPlan = planJSON
	repo.jobs[id] = job
	// Deliberately omit XAIEnabled: rendering reviewed words is local.
	h.capabilities.XAIEnabled = false

	request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/renders/"+plan.Variant, nil)
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("render status = %d, want 202; body=%s", response.Code, response.Body.String())
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeRenderStreamClip {
		t.Fatalf("queue = %#v", queue.enqueued)
	}
}

func newCaptionHTTPFixture(t *testing.T) (*Handlers, *fakeStreamRepo, *fakeStorage, *fakeQueue, uuid.UUID, streamclips.EditPlan) {
	t.Helper()
	repo := newFakeStreamRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	id := uuid.New()
	plan := streamclips.DefaultEditPlan()
	plan.FaceCropReviewed = true
	plan.Captions = streamclips.CaptionsPlan{Enabled: true, Language: "es"}
	plan.Clips = []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 0, EndSeconds: 2}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo.jobs[id] = streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id),
		SourceSHA256: "source-sha", EditPlan: planJSON,
		Probe: streamclips.SourceProbe{AudioCodec: "aac", DurationSeconds: 30},
	}
	h := NewHandlers(newFakeRepo(), store, queue,
		WithStreamRepository(repo),
		WithCapabilities(Capabilities{XAIEnabled: true}),
	)
	return h, repo, store, queue, id, plan
}

func writeReviewableCaptionState(t *testing.T, h *Handlers, job streamclips.Job, plan streamclips.EditPlan, clipStatus streamclips.CaptionCandidateClipStatus) uuid.UUID {
	t.Helper()
	fingerprint, err := streamclips.CaptionClipFingerprint(job.SourceSHA256, plan.Clips[0])
	if err != nil {
		t.Fatal(err)
	}
	generationID := uuid.New()
	state := streamclips.CaptionCandidateState{
		JobID: job.ID, GenerationID: generationID, Status: streamclips.CaptionCandidatesReviewRequired,
		Clips: []streamclips.CaptionCandidateClip{{
			ClipID: "clip-1", StartSeconds: 0, EndSeconds: 2, Fingerprint: fingerprint,
			Status: clipStatus, CandidateWords: []streamclips.CaptionWord{{Word: "hola", StartSeconds: 0.1, EndSeconds: 0.6}},
		}},
		UpdatedAt: time.Now().UTC(),
	}
	if err := h.writeStreamCaptionState(state); err != nil {
		t.Fatal(err)
	}
	return generationID
}

func reviewBody(t *testing.T, generationID uuid.UUID, clip map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"generation_id": generationID,
		"clips":         []map[string]any{clip},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}
