package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
)

func TestStreamKillfeedPostQueuesDurableGenerationWithoutXAIAndGetReturnsIt(t *testing.T) {
	h, _, store, queue, id, _ := newKillfeedAnalysisHTTPFixture(t)
	router := Routes(h)

	request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("POST status = %d, want 202; body=%s", response.Code, response.Body.String())
	}
	var queued streamclips.KillfeedAnalysisState
	if err := json.Unmarshal(response.Body.Bytes(), &queued); err != nil {
		t.Fatal(err)
	}
	if queued.Status != streamclips.KillfeedAnalysisQueued || queued.GenerationID == uuid.Nil || queued.Fingerprint == "" {
		t.Fatalf("queued state = %+v", queued)
	}
	if len(queued.Clips) != 1 || queued.Clips[0].Events == nil {
		t.Fatalf("queued clips = %+v, want one clip with an empty event list", queued.Clips)
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeGenerateStreamKillfeed {
		t.Fatalf("queue = %#v, want one killfeed generation", queue.enqueued)
	}
	if got, err := tasks.StreamKillfeedGenerationFromTask(queue.enqueued[0]); err != nil || got != queued.GenerationID {
		t.Fatalf("task generation = %s, %v; want %s", got, err, queued.GenerationID)
	}
	var payload tasks.GenerateStreamKillfeedPayload
	if err := json.Unmarshal(queue.enqueued[0].Payload(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.JobID != id || payload.GenerationID != queued.GenerationID {
		t.Fatalf("task payload = %+v", payload)
	}
	if !hasAsynqOption(queue.options[0], "MaxRetry(0)") {
		t.Fatalf("queue options = %#v, want MaxRetry(0)", queue.options[0])
	}
	if _, ok := store.puts[streamclips.KillfeedAnalysisKey(id)]; !ok {
		t.Fatal("active killfeed analysis pointer was not persisted")
	}
	generationKey, err := streamclips.KillfeedAnalysisGenerationKey(id, queued.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.puts[generationKey]; !ok {
		t.Fatal("killfeed generation artifact was not persisted")
	}

	request = httptest.NewRequest(http.MethodGet, "/api/stream-jobs/"+id.String()+"/killfeed", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var fetched streamclips.KillfeedAnalysisState
	if err := json.Unmarshal(response.Body.Bytes(), &fetched); err != nil {
		t.Fatal(err)
	}
	if fetched.GenerationID != queued.GenerationID || fetched.Status != streamclips.KillfeedAnalysisQueued {
		t.Fatalf("fetched state = %+v, want queued generation %s", fetched, queued.GenerationID)
	}
}

func TestStartStreamKillfeedAnalysisRequiresFFmpegAndCrop(t *testing.T) {
	t.Run("ffmpeg", func(t *testing.T) {
		h, _, _, queue, id, _ := newKillfeedAnalysisHTTPFixture(t)
		h.ffmpegPath = ""
		response := httptest.NewRecorder()
		Routes(h).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed", nil))
		if response.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body=%s", response.Code, response.Body.String())
		}
		if len(queue.enqueued) != 0 {
			t.Fatalf("queue = %#v, want no task", queue.enqueued)
		}
	})

	t.Run("crop", func(t *testing.T) {
		h, repo, _, queue, id, plan := newKillfeedAnalysisHTTPFixture(t)
		plan.KillfeedCrop = nil
		setStreamPlan(t, repo, id, plan)
		response := httptest.NewRecorder()
		Routes(h).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed", nil))
		if response.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body=%s", response.Code, response.Body.String())
		}
		if len(queue.enqueued) != 0 {
			t.Fatalf("queue = %#v, want no task", queue.enqueued)
		}
	})

	t.Run("ready job", func(t *testing.T) {
		h, repo, _, queue, id, _ := newKillfeedAnalysisHTTPFixture(t)
		job := repo.jobs[id]
		job.Status = streamclips.StatusUploaded
		repo.jobs[id] = job
		response := httptest.NewRecorder()
		Routes(h).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed", nil))
		if response.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body=%s", response.Code, response.Body.String())
		}
		if len(queue.enqueued) != 0 {
			t.Fatalf("queue = %#v, want no task", queue.enqueued)
		}
	})
}

func TestStartStreamKillfeedAnalysisPersistsFailedStateWhenEnqueueFails(t *testing.T) {
	h, _, _, queue, id, _ := newKillfeedAnalysisHTTPFixture(t)
	queue.err = errors.New("queue unavailable")
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", response.Code, response.Body.String())
	}
	state, exists, err := h.readStreamKillfeedState(id)
	if err != nil || !exists {
		t.Fatalf("read failed state = exists %v, err %v", exists, err)
	}
	if state.Status != streamclips.KillfeedAnalysisFailed || state.GenerationID == uuid.Nil || state.Error == "" {
		t.Fatalf("failed state = %+v", state)
	}
}

func TestApplyStreamKillfeedAnalysisCopiesEventsAndMarksGenerationApplied(t *testing.T) {
	h, repo, store, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
	state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, []streamclips.KillfeedAnalysisEvent{
		testKillfeedAnalysisEvent(0.5, nil),
	})
	if err := h.writeStreamKillfeedState(state); err != nil {
		t.Fatal(err)
	}

	response := applyKillfeedGeneration(t, h, id, state.GenerationID)
	if response.Code != http.StatusOK {
		t.Fatalf("apply status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var saved streamclips.EditPlan
	if err := json.Unmarshal(repo.jobs[id].EditPlan, &saved); err != nil {
		t.Fatal(err)
	}
	if got, want := saved.Clips[0].KillfeedSeconds, []float64{0.5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("killfeed cues = %v, want %v", got, want)
	}
	if len(saved.Clips[0].KillfeedKills) != 1 || len(saved.Clips[0].KillfeedKills[0]) != 0 {
		t.Fatalf("killfeed kills = %#v, want one valid empty-kill event", saved.Clips[0].KillfeedKills)
	}
	provenance, ok := saved.Clips[0].KillfeedProvenanceAt(0.5)
	if !ok || provenance.Origin != streamclips.KillfeedCueAutomatic ||
		provenance.EventID != state.Clips[0].Events[0].EventID {
		t.Fatalf("killfeed provenance = %#v / %v, want applied automatic event", provenance, ok)
	}
	if saved.KillfeedAnalysis == nil || saved.KillfeedAnalysis.GenerationID != state.GenerationID ||
		saved.KillfeedAnalysis.Fingerprint != state.Fingerprint || saved.KillfeedAnalysis.AppliedAt.IsZero() {
		t.Fatalf("killfeed metadata = %+v", saved.KillfeedAnalysis)
	}
	if _, ok := store.puts[streamclips.EditPlanKey(id)]; !ok {
		t.Fatal("applied edit-plan artifact was not written")
	}
	applied, ok, err := h.readStreamKillfeedState(id)
	if err != nil || !ok {
		t.Fatalf("read applied state = ok %v, err %v", ok, err)
	}
	if applied.Status != streamclips.KillfeedAnalysisApplied {
		t.Fatalf("state status = %q, want applied", applied.Status)
	}
}

func TestApplyStreamKillfeedAnalysisRepairsStaleActivePointerIdempotently(t *testing.T) {
	h, repo, store, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
	state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, []streamclips.KillfeedAnalysisEvent{
		testKillfeedAnalysisEvent(0.5, nil),
	})
	if err := h.writeStreamKillfeedState(state); err != nil {
		t.Fatal(err)
	}
	if response := applyKillfeedGeneration(t, h, id, state.GenerationID); response.Code != http.StatusOK {
		t.Fatalf("first apply status = %d; body=%s", response.Code, response.Body.String())
	}
	var enriched streamclips.EditPlan
	if err := json.Unmarshal(repo.jobs[id].EditPlan, &enriched); err != nil {
		t.Fatal(err)
	}
	enriched.Clips[0].KillfeedKills = [][]streamclips.KillfeedKill{{{
		AttackerSide: "CT", AttackerName: "ocr-name", VictimSide: "T",
		VictimName: "ocr-victim", Weapon: "ak47",
	}}}
	enriched.Clips[0].KillfeedSeconds = append(enriched.Clips[0].KillfeedSeconds, 1.5)
	enriched.Clips[0].KillfeedKills = append(enriched.Clips[0].KillfeedKills, []streamclips.KillfeedKill{{
		AttackerSide: "T", AttackerName: "manual-name", VictimSide: "CT",
		VictimName: "manual-victim", Weapon: "awp",
	}})
	setStreamPlan(t, repo, id, enriched)
	generationKey, err := streamclips.KillfeedAnalysisGenerationKey(id, state.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	generationBefore := append([]byte(nil), store.puts[generationKey]...)

	// Simulate the recoverable split-write case: the generation Put succeeded
	// as applied, but the subsequent active-pointer Put did not. GET and Apply
	// resolve the authoritative generation selected by this stale pointer.
	stalePointer := state
	stalePointer.Status = streamclips.KillfeedAnalysisReady
	stalePointerJSON, err := json.Marshal(stalePointer)
	if err != nil {
		t.Fatal(err)
	}
	store.puts[streamclips.KillfeedAnalysisKey(id)] = stalePointerJSON

	response := applyKillfeedGeneration(t, h, id, state.GenerationID)
	if response.Code != http.StatusOK {
		t.Fatalf("repair apply status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var repaired streamclips.KillfeedAnalysisState
	if err := json.Unmarshal(store.puts[streamclips.KillfeedAnalysisKey(id)], &repaired); err != nil {
		t.Fatal(err)
	}
	if repaired.Status != streamclips.KillfeedAnalysisApplied || repaired.GenerationID != state.GenerationID {
		t.Fatalf("repaired pointer = %s/%s, want applied/%s", repaired.GenerationID, repaired.Status, state.GenerationID)
	}
	var gotPlan streamclips.EditPlan
	if err := json.Unmarshal(repo.jobs[id].EditPlan, &gotPlan); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotPlan, enriched) {
		t.Fatalf("re-apply replaced reviewed/manual plan\ngot:  %+v\nwant: %+v", gotPlan, enriched)
	}
	if got := store.puts[generationKey]; !bytes.Equal(got, generationBefore) {
		t.Fatal("re-apply rewrote the authoritative generation instead of only repairing its pointer")
	}
}

func TestReapplyStreamKillfeedAnalysisRejectsPlanMissingAnExactEvent(t *testing.T) {
	h, repo, _, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
	state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, []streamclips.KillfeedAnalysisEvent{
		testKillfeedAnalysisEvent(0.5, nil),
	})
	if err := h.writeStreamKillfeedState(state); err != nil {
		t.Fatal(err)
	}
	if response := applyKillfeedGeneration(t, h, id, state.GenerationID); response.Code != http.StatusOK {
		t.Fatalf("first apply status = %d; body=%s", response.Code, response.Body.String())
	}
	var edited streamclips.EditPlan
	if err := json.Unmarshal(repo.jobs[id].EditPlan, &edited); err != nil {
		t.Fatal(err)
	}
	edited.Clips[0].KillfeedSeconds = nil
	edited.Clips[0].KillfeedKills = nil
	setStreamPlan(t, repo, id, edited)

	response := applyKillfeedGeneration(t, h, id, state.GenerationID)
	if response.Code != http.StatusConflict {
		t.Fatalf("re-apply status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte("missing exact analyzed cue")) {
		t.Fatalf("re-apply error is not actionable: %s", response.Body.String())
	}
}

func TestApplyStreamKillfeedAnalysisRejectsReplacedOrStaleGeneration(t *testing.T) {
	t.Run("replaced", func(t *testing.T) {
		h, repo, _, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
		state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, nil)
		if err := h.writeStreamKillfeedState(state); err != nil {
			t.Fatal(err)
		}
		response := applyKillfeedGeneration(t, h, id, uuid.New())
		if response.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("stale fingerprint", func(t *testing.T) {
		h, repo, _, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
		state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, nil)
		if err := h.writeStreamKillfeedState(state); err != nil {
			t.Fatal(err)
		}
		plan.Clips[0].EndSeconds = 3
		setStreamPlan(t, repo, id, plan)
		response := applyKillfeedGeneration(t, h, id, state.GenerationID)
		if response.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("not ready", func(t *testing.T) {
		h, repo, _, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
		state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, nil)
		state.Status = streamclips.KillfeedAnalysisReviewRequired
		if err := h.writeStreamKillfeedState(state); err != nil {
			t.Fatal(err)
		}
		response := applyKillfeedGeneration(t, h, id, state.GenerationID)
		if response.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body=%s", response.Code, response.Body.String())
		}
	})
}

func TestPutStreamEditPlanInvalidatesAppliedKillfeedWhenCropOrBoundsChange(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*streamclips.EditPlan)
	}{
		{name: "crop", mutate: func(plan *streamclips.EditPlan) { plan.KillfeedCrop.X -= 0.05 }},
		{name: "bounds", mutate: func(plan *streamclips.EditPlan) { plan.Clips[0].EndSeconds = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo, _, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
			state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, []streamclips.KillfeedAnalysisEvent{
				testKillfeedAnalysisEvent(0.5, nil),
			})
			if err := h.writeStreamKillfeedState(state); err != nil {
				t.Fatal(err)
			}
			if response := applyKillfeedGeneration(t, h, id, state.GenerationID); response.Code != http.StatusOK {
				t.Fatalf("apply status = %d; body=%s", response.Code, response.Body.String())
			}
			var submitted streamclips.EditPlan
			if err := json.Unmarshal(repo.jobs[id].EditPlan, &submitted); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&submitted)
			body, err := json.Marshal(submitted)
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
			if saved.KillfeedAnalysis != nil || len(saved.Clips[0].KillfeedSeconds) != 0 || len(saved.Clips[0].KillfeedKills) != 0 {
				t.Fatalf("stale generated killfeed survived: metadata=%+v clip=%+v", saved.KillfeedAnalysis, saved.Clips[0])
			}
		})
	}
}

func TestStartStreamRenderRequiresCurrentAppliedKillfeedAndAcceptsAppliedZeroEvents(t *testing.T) {
	h, repo, _, queue, id, plan := newKillfeedAnalysisHTTPFixture(t)
	renderPath := "/api/stream-jobs/" + id.String() + "/renders/" + plan.Variant

	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, httptest.NewRequest(http.MethodPost, renderPath, nil))
	if response.Code != http.StatusConflict {
		t.Fatalf("render before analysis status = %d, want 409; body=%s", response.Code, response.Body.String())
	}

	state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, []streamclips.KillfeedAnalysisEvent{})
	if err := h.writeStreamKillfeedState(state); err != nil {
		t.Fatal(err)
	}
	if response = applyKillfeedGeneration(t, h, id, state.GenerationID); response.Code != http.StatusOK {
		t.Fatalf("zero-event apply status = %d; body=%s", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	Routes(h).ServeHTTP(response, httptest.NewRequest(http.MethodPost, renderPath, nil))
	if response.Code != http.StatusAccepted {
		t.Fatalf("render after zero-event apply status = %d, want 202; body=%s", response.Code, response.Body.String())
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0].Type() != tasks.TypeRenderStreamClip {
		t.Fatalf("queue = %#v, want one render", queue.enqueued)
	}
}

func TestStartStreamRenderRejectsUnresolvedManualKillfeedCueBeforeEnqueue(t *testing.T) {
	h, repo, _, queue, id, plan := newKillfeedAnalysisHTTPFixture(t)
	state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, []streamclips.KillfeedAnalysisEvent{
		testKillfeedAnalysisEvent(0.5, nil),
	})
	if err := h.writeStreamKillfeedState(state); err != nil {
		t.Fatal(err)
	}
	if response := applyKillfeedGeneration(t, h, id, state.GenerationID); response.Code != http.StatusOK {
		t.Fatalf("apply status = %d; body=%s", response.Code, response.Body.String())
	}
	var saved streamclips.EditPlan
	if err := json.Unmarshal(repo.jobs[id].EditPlan, &saved); err != nil {
		t.Fatal(err)
	}
	saved.Clips[0].KillfeedSeconds = append(saved.Clips[0].KillfeedSeconds, 1.25)
	saved.Clips[0].KillfeedKills = append(saved.Clips[0].KillfeedKills, []streamclips.KillfeedKill{})
	setStreamPlan(t, repo, id, saved)

	renderPath := "/api/stream-jobs/" + id.String() + "/renders/" + plan.Variant
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, httptest.NewRequest(http.MethodPost, renderPath, nil))
	if response.Code != http.StatusConflict {
		t.Fatalf("render status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte("no exact captured event and no reviewed kills")) {
		t.Fatalf("render error is not actionable: %s", response.Body.String())
	}
	if len(queue.enqueued) != 0 {
		t.Fatalf("queue = %#v, want no render task", queue.enqueued)
	}
}

func TestStartStreamRenderRejectsAppliedPlanAfterNewGenerationBecomesCurrent(t *testing.T) {
	h, repo, _, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
	applied := readyKillfeedAnalysisState(t, repo.jobs[id], plan, nil)
	if err := h.writeStreamKillfeedState(applied); err != nil {
		t.Fatal(err)
	}
	if response := applyKillfeedGeneration(t, h, id, applied.GenerationID); response.Code != http.StatusOK {
		t.Fatalf("apply status = %d; body=%s", response.Code, response.Body.String())
	}

	current := readyKillfeedAnalysisState(t, repo.jobs[id], plan, nil)
	current.Status = streamclips.KillfeedAnalysisQueued
	if err := h.writeStreamKillfeedState(current); err != nil {
		t.Fatal(err)
	}
	renderPath := "/api/stream-jobs/" + id.String() + "/renders/" + plan.Variant
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, httptest.NewRequest(http.MethodPost, renderPath, nil))
	if response.Code != http.StatusConflict {
		t.Fatalf("render status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
}

func newKillfeedAnalysisHTTPFixture(t *testing.T) (*Handlers, *fakeStreamRepo, *fakeStorage, *fakeQueue, uuid.UUID, streamclips.EditPlan) {
	t.Helper()
	repo := newFakeStreamRepo()
	store := newFakeStorage()
	queue := &fakeQueue{}
	id := uuid.New()
	plan := streamclips.DefaultEditPlan()
	plan.KillfeedCrop = &streamclips.CropRect{X: 0.7, Y: 0.05, Width: 0.25, Height: 0.2}
	plan.Clips = []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 0, EndSeconds: 2}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo.jobs[id] = streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id),
		SourceSHA256: "source-sha", EditPlan: planJSON,
		Probe: streamclips.SourceProbe{VideoCodec: "h264", DurationSeconds: 30},
	}
	h := NewHandlers(newFakeRepo(), store, queue,
		WithStreamRepository(repo),
		WithFFmpegPath("ffmpeg"),
	)
	// No xAI key: temporal analysis and exact captured-row rendering still work.
	return h, repo, store, queue, id, plan
}

func readyKillfeedAnalysisState(t *testing.T, job streamclips.Job, plan streamclips.EditPlan, events []streamclips.KillfeedAnalysisEvent) streamclips.KillfeedAnalysisState {
	t.Helper()
	fingerprint, err := streamclips.KillfeedAnalysisFingerprint(job.SourceSHA256, *plan.KillfeedCrop, plan.Clips)
	if err != nil {
		t.Fatal(err)
	}
	if events == nil {
		events = []streamclips.KillfeedAnalysisEvent{}
	}
	return streamclips.KillfeedAnalysisState{
		JobID: job.ID, GenerationID: uuid.New(), Status: streamclips.KillfeedAnalysisReady,
		SourceSHA256: job.SourceSHA256, KillfeedCrop: *plan.KillfeedCrop, Fingerprint: fingerprint,
		Clips: []streamclips.KillfeedAnalysisClip{{
			ClipID: plan.Clips[0].ID, StartSeconds: plan.Clips[0].StartSeconds,
			EndSeconds: plan.Clips[0].EndSeconds, Events: events,
		}},
		UpdatedAt: time.Now().UTC(),
	}
}

func testKillfeedAnalysisEvent(cue float64, kills []streamclips.KillfeedKill) streamclips.KillfeedAnalysisEvent {
	pts := int64(cue * 1000)
	samplePTS := pts + 350
	if kills == nil {
		kills = []streamclips.KillfeedKill{}
	}
	return streamclips.KillfeedAnalysisEvent{
		EventID: "event-1", SourcePTS: pts, TimeBase: streamclips.KillfeedTimeBase{Num: 1, Den: 1000},
		CueSeconds: cue, OnsetStartPTS: pts - 1, OnsetEndPTS: pts,
		SamplePTS: samplePTS, SampleSeconds: cue + 0.35, Mode: streamclips.KillfeedEventAlignedFrame,
		Rows: []streamclips.KillfeedRowEvidence{{
			OnsetRowIndex: 0, SampleRowIndex: 0, Fingerprint: "row-1",
			OnsetBounds:  streamclips.NoticeRow{X: 10, Y: 10, Width: 100, Height: 30},
			SampleBounds: streamclips.NoticeRow{X: 10, Y: 10, Width: 100, Height: 30},
		}},
		Kills: kills,
	}
}

func applyKillfeedGeneration(t *testing.T, h *Handlers, id, generationID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(applyStreamKillfeedRequest{GenerationID: generationID})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed/apply", bytes.NewReader(body))
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, request)
	return response
}

func setStreamPlan(t *testing.T, repo *fakeStreamRepo, id uuid.UUID, plan streamclips.EditPlan) {
	t.Helper()
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	job := repo.jobs[id]
	job.EditPlan = b
	repo.jobs[id] = job
}
