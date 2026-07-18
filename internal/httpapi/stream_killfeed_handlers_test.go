package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

// fakeXAIKillfeedServer returns an httptest server that answers the xAI chat
// completions endpoint with a killfeed reply carrying the given kills JSON.
func fakeXAIKillfeedServer(t *testing.T, killsJSON string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer xai_test" {
			t.Errorf("Authorization = %q, want Bearer xai_test", got)
		}
		resp := map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": killsJSON}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func killfeedReadHandlers(t *testing.T, xaiURL, ffmpegPath, xaiKey string) (*Handlers, uuid.UUID) {
	t.Helper()
	streamRepo := newFakeStreamRepo()
	id := uuid.New()
	crop := streamclips.CropRect{X: 0.7, Y: 0.05, Width: 0.28, Height: 0.2}
	plan := streamclips.DefaultEditPlan()
	plan.KillfeedCrop = &crop
	plan.Clips = []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 0, EndSeconds: 5}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	streamRepo.jobs[id] = streamclips.Job{ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON}

	opts := []Option{WithStreamRepository(streamRepo)}
	if ffmpegPath != "" {
		opts = append(opts, WithFFmpegPath(ffmpegPath))
	}
	if xaiKey != "" {
		opts = append(opts, WithXAIKey(xaiKey))
	}
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{}, opts...)
	h.killfeedVisionBaseURL = xaiURL
	h.killfeedFrame = func(context.Context, string, float64) (image.Image, error) {
		return image.NewRGBA(image.Rect(0, 0, 1920, 1080)), nil
	}
	return h, id
}

func encodedNoticeCountFrame(count int) *image.RGBA {
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	frame.SetRGBA(0, 0, color.RGBA{R: uint8(count), A: 255})
	yPositions := []int{73, 109, 143}
	for i := range min(count, len(yPositions)) {
		xStart := 1610 + i*100
		for y := yPositions[i] + 5; y < yPositions[i]+20; y++ {
			for x := xStart; x < xStart+70; x++ {
				frame.SetRGBA(x, y, color.RGBA{R: 230, G: 230, B: 230, A: 255})
			}
		}
	}
	return frame
}

func encodedNoticeRows(frame image.Image, _ *streamclips.CropRect) []streamclips.NoticeRow {
	red, _, _, _ := frame.At(0, 0).RGBA()
	count := int(red >> 8)
	yPositions := []int{73, 109, 143}
	rows := make([]streamclips.NoticeRow, min(count, len(yPositions)))
	for i := range rows {
		rows[i] = streamclips.NoticeRow{X: 1600, Y: yPositions[i], Width: 300, Height: 34}
	}
	return rows
}

func TestReadStreamKillfeedReturnsParsedKills(t *testing.T) {
	weapon := streamclips.WeaponKeys()[0]
	killsJSON := `{"kills":[{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"villain","weapon":"` + weapon + `","headshot":true}]}`
	srv := fakeXAIKillfeedServer(t, killsJSON)

	h, id := killfeedReadHandlers(t, srv.URL, "ffmpeg", "xai_test")
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", strings.NewReader(`{"clip_id":"clip-1","cue_seconds":2}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	var body struct {
		Kills []streamclips.KillfeedKill `json:"kills"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rw.Body.String())
	}
	if len(body.Kills) != 1 {
		t.Fatalf("kills = %d, want 1; body=%s", len(body.Kills), rw.Body.String())
	}
	got := body.Kills[0]
	if got.AttackerName != "hero" || got.VictimName != "villain" || got.Weapon != weapon || !got.Headshot {
		t.Fatalf("kill = %#v, want the parsed hero/villain notice", got)
	}
}

func TestReadStreamKillfeedUsesAppliedEventRowsWithoutChangingSourcePTS(t *testing.T) {
	weapon := streamclips.WeaponKeys()[0]
	var visionCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visionCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer xai_test" {
			t.Errorf("Authorization = %q, want Bearer xai_test", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{
				"content": `{"kills":[{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"victim","weapon":"` + weapon + `"}]}`,
			}}},
		})
	}))
	t.Cleanup(srv.Close)

	h, repo, store, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
	h.xaiAPIKey = "xai_test"
	h.killfeedVisionBaseURL = srv.URL
	// Exact reads consume immutable row artifacts and do not need FFmpeg. A
	// legacy realignment attempt would fail this request.
	h.ffmpegPath = ""
	h.killfeedFrame = func(context.Context, string, float64) (image.Image, error) {
		return nil, errors.New("exact read must not extract a frame")
	}

	const sourcePTS int64 = 1001
	const timeBaseDen int64 = 30000
	cue := float64(sourcePTS) / float64(timeBaseDen)
	event := testKillfeedAnalysisEvent(cue, nil)
	event.EventID = "event-exact"
	event.SourcePTS = sourcePTS
	event.OnsetStartPTS = sourcePTS - 1
	event.OnsetEndPTS = sourcePTS
	event.TimeBase = streamclips.KillfeedTimeBase{Num: 1, Den: timeBaseDen}
	event.SamplePTS = sourcePTS + 100
	event.SampleSeconds = float64(event.SamplePTS) / float64(timeBaseDen)
	nextEvent := event
	nextEvent.EventID = "event-next-frame"
	nextEvent.SourcePTS = sourcePTS + 1
	nextEvent.OnsetStartPTS = sourcePTS
	nextEvent.OnsetEndPTS = sourcePTS + 1
	nextEvent.CueSeconds = float64(sourcePTS+1) / float64(timeBaseDen)
	nextEvent.SamplePTS++
	nextEvent.SampleSeconds = float64(nextEvent.SamplePTS) / float64(timeBaseDen)
	nextEvent.Rows = append([]streamclips.KillfeedRowEvidence(nil), event.Rows...)
	nextEvent.Rows[0].Fingerprint = "row-next-frame"
	state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, []streamclips.KillfeedAnalysisEvent{event, nextEvent})
	if err := h.writeStreamKillfeedState(state); err != nil {
		t.Fatal(err)
	}
	if response := applyKillfeedGeneration(t, h, id, state.GenerationID); response.Code != http.StatusOK {
		t.Fatalf("apply status = %d; body=%s", response.Code, response.Body.String())
	}
	putKillfeedEventRowPNG(t, store, id, state.GenerationID, "clip-1", event.EventID, 0)

	body, err := json.Marshal(readKillfeedRequest{
		ClipID: "clip-1", CueSeconds: cue, EventID: event.EventID, GenerationID: &state.GenerationID,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", bytes.NewReader(body))
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var got readKillfeedResponse
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Aligned || got.CueSeconds != cue || len(got.Events) != 1 || got.Events[0].CueSeconds != cue {
		t.Fatalf("response = %+v, want bit-identical persisted cue %.17g", got, cue)
	}
	if len(got.Kills) != 1 || got.Kills[0].VictimName != "victim" {
		t.Fatalf("kills = %+v, want exact row OCR result", got.Kills)
	}
	if calls := visionCalls.Load(); calls != 1 {
		t.Fatalf("vision calls = %d, want one per persisted row", calls)
	}
	var saved streamclips.EditPlan
	if err := json.Unmarshal(repo.jobs[id].EditPlan, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Clips[0].KillfeedSeconds) != 2 ||
		saved.Clips[0].KillfeedSeconds[0] != cue ||
		saved.Clips[0].KillfeedSeconds[1] != nextEvent.CueSeconds {
		t.Fatalf("applied cues = %v, want adjacent source PTS cues %.17g and %.17g",
			saved.Clips[0].KillfeedSeconds, cue, nextEvent.CueSeconds)
	}
}

func TestReadStreamKillfeedRejectsIdentitylessAppliedEventInsteadOfLegacyAlignment(t *testing.T) {
	h, repo, _, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
	h.xaiAPIKey = "xai_test"
	// The exact event does not need FFmpeg. An accidental legacy fallback would
	// report the missing binary instead of the stable identity error below.
	h.ffmpegPath = ""
	event := testKillfeedAnalysisEvent(0.5, nil)
	state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, []streamclips.KillfeedAnalysisEvent{event})
	if err := h.writeStreamKillfeedState(state); err != nil {
		t.Fatal(err)
	}
	if response := applyKillfeedGeneration(t, h, id, state.GenerationID); response.Code != http.StatusOK {
		t.Fatalf("apply status = %d; body=%s", response.Code, response.Body.String())
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/stream-jobs/"+id.String()+"/killfeed-read",
		strings.NewReader(`{"clip_id":"clip-1","cue_seconds":0.5}`),
	)
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
	var got struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Code != killfeedEventIdentityCode {
		t.Fatalf("code = %q, want %q; body=%s", got.Code, killfeedEventIdentityCode, response.Body.String())
	}
}

func TestReadStreamKillfeedRejectsForeignOrMissingAppliedEventArtifacts(t *testing.T) {
	weapon := streamclips.WeaponKeys()[0]
	srv := fakeXAIKillfeedServer(t, `{"kills":[{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"victim","weapon":"`+weapon+`"}]}`)

	for _, tc := range []struct {
		name      string
		mutate    func(readKillfeedRequest) readKillfeedRequest
		wantCode  string
		putRowPNG bool
		corrupt   bool
	}{
		{
			name: "foreign event id",
			mutate: func(request readKillfeedRequest) readKillfeedRequest {
				request.EventID = "event-foreign"
				return request
			},
			wantCode: killfeedEventStaleCode, putRowPNG: true,
		},
		{
			name: "stale generation",
			mutate: func(request readKillfeedRequest) readKillfeedRequest {
				generationID := uuid.New()
				request.GenerationID = &generationID
				return request
			},
			wantCode: killfeedEventStaleCode, putRowPNG: true,
		},
		{
			name: "missing exact row",
			mutate: func(request readKillfeedRequest) readKillfeedRequest {
				return request
			},
			wantCode: killfeedEventArtifactErrorCode,
		},
		{
			name: "corrupt exact row",
			mutate: func(request readKillfeedRequest) readKillfeedRequest {
				return request
			},
			wantCode: killfeedEventArtifactErrorCode, corrupt: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo, store, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
			h.xaiAPIKey = "xai_test"
			h.killfeedVisionBaseURL = srv.URL
			event := testKillfeedAnalysisEvent(0.5, nil)
			state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, []streamclips.KillfeedAnalysisEvent{event})
			if err := h.writeStreamKillfeedState(state); err != nil {
				t.Fatal(err)
			}
			if response := applyKillfeedGeneration(t, h, id, state.GenerationID); response.Code != http.StatusOK {
				t.Fatalf("apply status = %d; body=%s", response.Code, response.Body.String())
			}
			if tc.putRowPNG {
				putKillfeedEventRowPNG(t, store, id, state.GenerationID, "clip-1", event.EventID, 0)
			}
			if tc.corrupt {
				key, err := streamclips.KillfeedEventRowKey(id, state.GenerationID, "clip-1", event.EventID, 0)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.Put(key, strings.NewReader("not a png")); err != nil {
					t.Fatal(err)
				}
			}
			requestBody := tc.mutate(readKillfeedRequest{
				ClipID: "clip-1", CueSeconds: event.CueSeconds,
				EventID: event.EventID, GenerationID: &state.GenerationID,
			})
			body, err := json.Marshal(requestBody)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", bytes.NewReader(body))
			response := httptest.NewRecorder()
			Routes(h).ServeHTTP(response, request)
			if response.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409; body=%s", response.Code, response.Body.String())
			}
			var errorBody struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &errorBody); err != nil {
				t.Fatal(err)
			}
			if errorBody.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q; body=%s", errorBody.Code, tc.wantCode, response.Body.String())
			}
		})
	}
}

func TestReadStreamKillfeedRejectsEventReplacedDuringVisionRequest(t *testing.T) {
	h, repo, store, _, id, plan := newKillfeedAnalysisHTTPFixture(t)
	h.xaiAPIKey = "xai_test"
	event := testKillfeedAnalysisEvent(0.5, nil)
	state := readyKillfeedAnalysisState(t, repo.jobs[id], plan, []streamclips.KillfeedAnalysisEvent{event})
	if err := h.writeStreamKillfeedState(state); err != nil {
		t.Fatal(err)
	}
	if response := applyKillfeedGeneration(t, h, id, state.GenerationID); response.Code != http.StatusOK {
		t.Fatalf("apply status = %d; body=%s", response.Code, response.Body.String())
	}
	putKillfeedEventRowPNG(t, store, id, state.GenerationID, "clip-1", event.EventID, 0)

	replacementEvent := testKillfeedAnalysisEvent(0.75, nil)
	replacementEvent.EventID = "event-replacement"
	replacement := readyKillfeedAnalysisState(t, repo.jobs[id], plan, []streamclips.KillfeedAnalysisEvent{replacementEvent})
	replacement.Status = streamclips.KillfeedAnalysisApplied
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Simulate another analysis being applied while this request is waiting
		// on xAI. The endpoint must discard the otherwise valid OCR response.
		h.streamPlanMu.Lock()
		defer h.streamPlanMu.Unlock()
		if err := h.writeStreamKillfeedState(replacement); err != nil {
			t.Errorf("write replacement killfeed state: %v", err)
		}
		var nextPlan streamclips.EditPlan
		if err := json.Unmarshal(repo.jobs[id].EditPlan, &nextPlan); err != nil {
			t.Errorf("decode replacement plan: %v", err)
		}
		nextPlan.Clips[0].KillfeedSeconds = []float64{replacementEvent.CueSeconds}
		nextPlan.Clips[0].KillfeedKills = [][]streamclips.KillfeedKill{{}}
		nextPlan.KillfeedAnalysis = &streamclips.KillfeedAnalysisMetadata{
			GenerationID: replacement.GenerationID,
			Fingerprint:  replacement.Fingerprint,
			AppliedAt:    replacement.UpdatedAt,
		}
		if err := repo.SetEditPlan(context.Background(), id, nextPlan); err != nil {
			t.Errorf("save replacement plan: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]any{
				"content": `{"kills":[{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"victim","weapon":"awp"}]}`,
			}}},
		})
	}))
	t.Cleanup(srv.Close)
	h.killfeedVisionBaseURL = srv.URL

	body, err := json.Marshal(readKillfeedRequest{
		ClipID: "clip-1", CueSeconds: event.CueSeconds,
		EventID: event.EventID, GenerationID: &state.GenerationID,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", bytes.NewReader(body))
	response := httptest.NewRecorder()
	Routes(h).ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", response.Code, response.Body.String())
	}
	var errorBody struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &errorBody); err != nil {
		t.Fatal(err)
	}
	if errorBody.Code != killfeedEventStaleCode {
		t.Fatalf("code = %q, want %q; body=%s", errorBody.Code, killfeedEventStaleCode, response.Body.String())
	}
}

func putKillfeedEventRowPNG(
	t *testing.T,
	store *fakeStorage,
	jobID, generationID uuid.UUID,
	clipID, eventID string,
	rowIndex int,
) {
	t.Helper()
	key, err := streamclips.KillfeedEventRowKey(jobID, generationID, clipID, eventID, rowIndex)
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 8, 4))); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(key, bytes.NewReader(encoded.Bytes())); err != nil {
		t.Fatal(err)
	}
}

func TestReadStreamKillfeedAlignsCumulativeSnapshotToNoticeBirths(t *testing.T) {
	weapon := streamclips.WeaponKeys()[0]
	killsJSON := `{"kills":[` +
		`{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"first","weapon":"` + weapon + `"},` +
		`{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"second","weapon":"` + weapon + `"},` +
		`{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"third","weapon":"` + weapon + `"}` +
		`]}`
	srv := fakeXAIKillfeedServer(t, killsJSON)
	h, id := killfeedReadHandlers(t, srv.URL, "ffmpeg", "xai_test")
	h.killfeedFrame = func(_ context.Context, _ string, seconds float64) (image.Image, error) {
		count := 0
		switch {
		case seconds >= 4:
			count = 3
		case seconds >= 2.6:
			count = 2
		case seconds >= 2.5:
			count = 1
		}
		return encodedNoticeCountFrame(count), nil
	}
	h.killfeedNoticeRows = encodedNoticeRows
	h.killfeedTimeline = func(_ context.Context, _ string, start, end float64, crop *streamclips.CropRect) ([]timedKillfeedRows, error) {
		var frames []timedKillfeedRows
		for seconds := start; seconds <= end; seconds += 1.0 / killfeedTimelineFPS {
			frame, err := h.killfeedFrame(context.Background(), "source", seconds)
			if err != nil {
				return nil, err
			}
			rows := h.killfeedNoticeRows(frame, crop)
			frames = append(frames, timedKillfeedRows{
				Seconds:      seconds,
				Bounds:       frame.Bounds(),
				Rows:         rows,
				Fingerprints: fingerprintKillfeedRows(frame, rows),
			})
		}
		return frames, nil
	}
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", strings.NewReader(`{"clip_id":"clip-1","cue_seconds":4.5}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	var body readKillfeedResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rw.Body.String())
	}
	if !body.Aligned {
		t.Fatalf("aligned = false, want true; body=%s", rw.Body.String())
	}
	if len(body.Events) != 3 {
		t.Fatalf("events = %+v, want three notice-birth events", body.Events)
	}
	if math.Abs(body.Events[0].CueSeconds-2.375) > 0.02 || len(body.Events[0].Kills) != 1 || body.Events[0].Kills[0].VictimName != "first" {
		t.Fatalf("first event = %+v, want first kill aligned just before its first stable row", body.Events[0])
	}
	if math.Abs(body.Events[1].CueSeconds-2.5) > 0.02 || len(body.Events[1].Kills) != 1 || body.Events[1].Kills[0].VictimName != "second" {
		t.Fatalf("second event = %+v, want second kill aligned just before its first stable row", body.Events[1])
	}
	if math.Abs(body.Events[2].CueSeconds-3.875) > 0.02 || len(body.Events[2].Kills) != 1 || body.Events[2].Kills[0].VictimName != "third" {
		t.Fatalf("third event = %+v, want only the new third kill just before its first stable row", body.Events[2])
	}
}

func TestAlignKillfeedEventsDoesNotDoubleCountTargetEndpoint(t *testing.T) {
	targetFrame := encodedNoticeCountFrame(2)
	targetRows := encodedNoticeRows(targetFrame, nil)
	targetFingerprints := fingerprintKillfeedRows(targetFrame, targetRows)
	if !distinctKillfeedFingerprints(targetFingerprints) {
		t.Fatal("test target fingerprints are not distinct")
	}
	h := &Handlers{killfeedNoticeRows: encodedNoticeRows}
	h.killfeedTimeline = func(context.Context, string, float64, float64, *streamclips.CropRect) ([]timedKillfeedRows, error) {
		return []timedKillfeedRows{
			{Seconds: 4.25, Bounds: targetFrame.Bounds(), Fingerprints: []killfeedRowFingerprint{targetFingerprints[1]}},
			{Seconds: 4.375, Bounds: targetFrame.Bounds(), Fingerprints: []killfeedRowFingerprint{targetFingerprints[1]}},
			// The first row exists only in this endpoint observation. alignKillfeedEvents
			// separately appends targetFrame at the same timestamp.
			{Seconds: 4.5, Bounds: targetFrame.Bounds(), Rows: targetRows, Fingerprints: targetFingerprints},
		}, nil
	}
	kills := []streamclips.KillfeedKill{
		{AttackerSide: "CT", AttackerName: "hero", VictimSide: "T", VictimName: "first", Weapon: "awp"},
		{AttackerSide: "CT", AttackerName: "hero", VictimSide: "T", VictimName: "second", Weapon: "awp"},
	}
	events, aligned := h.alignKillfeedEvents(
		context.Background(), "source", streamclips.ClipRange{StartSeconds: 0, EndSeconds: 5},
		streamclips.CropRect{X: 0.7, Y: 0.05, Width: 0.28, Height: 0.2}, targetFrame, 4.5, kills,
	)
	if aligned || events != nil {
		t.Fatalf("aligned = %v, events = %+v; want fallback when a non-newest row has only one distinct observation", aligned, events)
	}
}

func TestFallbackKillfeedEventKillsSubtractsRecentEvents(t *testing.T) {
	first := streamclips.KillfeedKill{AttackerSide: "CT", AttackerName: "hero", VictimSide: "T", VictimName: "first", Weapon: "awp"}
	second := streamclips.KillfeedKill{AttackerSide: "CT", AttackerName: "hero", VictimSide: "T", VictimName: "second", Weapon: "awp"}
	clip := streamclips.ClipRange{
		KillfeedSeconds: []float64{2},
		KillfeedKills:   [][]streamclips.KillfeedKill{{first}},
	}

	got := fallbackKillfeedEventKills(clip, 3, []streamclips.KillfeedKill{first, second})
	if len(got) != 1 || got[0] != second {
		t.Fatalf("fallback delta = %+v, want only second kill", got)
	}
}

func TestFindKillfeedRowOnsetIgnoresExpiredOlderOccupant(t *testing.T) {
	bounds := image.Rect(0, 0, 1920, 1080)
	target := streamclips.NoticeRow{X: 1600, Y: 73, Width: 300, Height: 34}
	oldFingerprint := killfeedRowFingerprint{features: 64}
	oldFingerprint.bits[1] = ^uint64(0)
	targetFingerprint := killfeedRowFingerprint{features: 64}
	targetFingerprint.bits[0] = ^uint64(0)
	withFingerprint := func(seconds float64, fingerprint killfeedRowFingerprint) timedKillfeedRows {
		return timedKillfeedRows{
			Seconds: seconds, Bounds: bounds,
			Rows: []streamclips.NoticeRow{target}, Fingerprints: []killfeedRowFingerprint{fingerprint},
		}
	}
	withoutTarget := func(seconds float64) timedKillfeedRows {
		return timedKillfeedRows{Seconds: seconds, Bounds: bounds}
	}
	timeline := []timedKillfeedRows{
		withFingerprint(0, oldFingerprint),
		withFingerprint(0.125, oldFingerprint),
		withoutTarget(0.25),
		withoutTarget(0.375),
		withoutTarget(0.5),
		withoutTarget(0.625),
		withFingerprint(0.75, targetFingerprint),
		withoutTarget(0.875), // one bounded detector miss in the final run
		withFingerprint(1, targetFingerprint),
		withFingerprint(1.125, targetFingerprint),
	}

	got, ok := (&Handlers{}).findKillfeedRowOnset(targetFingerprint, timeline, false, false)
	if !ok {
		t.Fatal("findKillfeedRowOnset ok = false, want final stable occupancy")
	}
	if got != 0.75 {
		t.Fatalf("onset = %.3f, want 0.750 after the older row occupant expired", got)
	}
}

func TestFindKillfeedRowOnsetDistinguishesContinuousSlotReplacement(t *testing.T) {
	bounds := image.Rect(0, 0, 1920, 1080)
	target := streamclips.NoticeRow{X: 1600, Y: 73, Width: 300, Height: 34}
	oldFingerprint := killfeedRowFingerprint{features: 64}
	oldFingerprint.bits[0] = ^uint64(0)
	targetFingerprint := killfeedRowFingerprint{features: 64}
	targetFingerprint.bits[1] = ^uint64(0)
	sample := func(seconds float64, fingerprint killfeedRowFingerprint) timedKillfeedRows {
		return timedKillfeedRows{
			Seconds: seconds, Bounds: bounds,
			Rows: []streamclips.NoticeRow{target}, Fingerprints: []killfeedRowFingerprint{fingerprint},
		}
	}
	timeline := []timedKillfeedRows{
		sample(0, oldFingerprint),
		sample(0.125, oldFingerprint),
		sample(0.25, targetFingerprint),
		sample(0.375, targetFingerprint),
		sample(0.5, targetFingerprint),
	}

	got, ok := (&Handlers{}).findKillfeedRowOnset(targetFingerprint, timeline, false, false)
	if !ok {
		t.Fatal("findKillfeedRowOnset ok = false, want target fingerprint run")
	}
	if got != 0.25 {
		t.Fatalf("onset = %.3f, want 0.250 after continuous slot replacement", got)
	}
}

func TestFindKillfeedRowOnsetTracksNoticeAcrossRowReflow(t *testing.T) {
	bounds := image.Rect(0, 0, 1920, 1080)
	fingerprint := killfeedRowFingerprint{features: 64}
	fingerprint.bits[0] = ^uint64(0)
	sample := func(seconds float64, y int) timedKillfeedRows {
		return timedKillfeedRows{
			Seconds: seconds, Bounds: bounds,
			Rows:         []streamclips.NoticeRow{{X: 1600, Y: y, Width: 300, Height: 34}},
			Fingerprints: []killfeedRowFingerprint{fingerprint},
		}
	}
	timeline := []timedKillfeedRows{
		sample(0, 73),
		sample(0.125, 73),
		sample(0.25, 143),
		sample(0.375, 143),
	}

	got, ok := (&Handlers{}).findKillfeedRowOnset(fingerprint, timeline, false, true)
	if !ok || got != 0 {
		t.Fatalf("onset = %.3f, ok = %v; want birth at 0 before row reflow", got, ok)
	}
}

func TestFindKillfeedRowOnsetAnchorsRepeatedContentToTargetRun(t *testing.T) {
	bounds := image.Rect(0, 0, 1920, 1080)
	fingerprint := killfeedRowFingerprint{features: 64}
	fingerprint.bits[0] = ^uint64(0)
	row := streamclips.NoticeRow{X: 1600, Y: 73, Width: 300, Height: 34}
	matched := func(seconds float64) timedKillfeedRows {
		return timedKillfeedRows{
			Seconds: seconds, Bounds: bounds,
			Rows: []streamclips.NoticeRow{row}, Fingerprints: []killfeedRowFingerprint{fingerprint},
		}
	}
	timeline := []timedKillfeedRows{
		matched(0),
		matched(0.125),
		{Seconds: 0.25, Bounds: bounds},
		{Seconds: 0.75, Bounds: bounds},
		matched(1.5),
		matched(1.625),
		matched(1.75),
	}

	got, ok := (&Handlers{}).findKillfeedRowOnset(fingerprint, timeline, false, false)
	if !ok || got != 1.5 {
		t.Fatalf("onset = %.3f, ok = %v; want target-connected run at 1.500", got, ok)
	}
}

func TestFindKillfeedRowOnsetBridgesBoundedDetectorGap(t *testing.T) {
	bounds := image.Rect(0, 0, 1920, 1080)
	fingerprint := killfeedRowFingerprint{features: 64}
	fingerprint.bits[0] = ^uint64(0)
	row := streamclips.NoticeRow{X: 1600, Y: 73, Width: 300, Height: 34}
	matched := func(seconds float64) timedKillfeedRows {
		return timedKillfeedRows{
			Seconds: seconds, Bounds: bounds,
			Rows: []streamclips.NoticeRow{row}, Fingerprints: []killfeedRowFingerprint{fingerprint},
		}
	}
	timeline := []timedKillfeedRows{matched(0), matched(0.125)}
	for seconds := 0.25; seconds < 1; seconds += 0.125 {
		timeline = append(timeline, timedKillfeedRows{Seconds: seconds, Bounds: bounds})
	}
	timeline = append(timeline, matched(1))

	got, ok := (&Handlers{}).findKillfeedRowOnset(fingerprint, timeline, false, true)
	if !ok || got != 0 {
		t.Fatalf("onset = %.3f, ok = %v; want 0 across bounded detector gap", got, ok)
	}
}

func TestFindKillfeedRowOnsetRejectsLeftCensoredLookbackBoundary(t *testing.T) {
	bounds := image.Rect(0, 0, 1920, 1080)
	fingerprint := killfeedRowFingerprint{features: 64}
	fingerprint.bits[0] = ^uint64(0)
	row := streamclips.NoticeRow{X: 1600, Y: 73, Width: 300, Height: 34}
	matched := func(seconds float64) timedKillfeedRows {
		return timedKillfeedRows{
			Seconds: seconds, Bounds: bounds,
			Rows: []streamclips.NoticeRow{row}, Fingerprints: []killfeedRowFingerprint{fingerprint},
		}
	}
	timeline := []timedKillfeedRows{matched(12), matched(12.125), matched(12.25)}

	if onset, ok := (&Handlers{}).findKillfeedRowOnset(fingerprint, timeline, false, false); ok {
		t.Fatalf("onset = %.3f, ok = true; want fallback for a row already present at the lookback boundary", onset)
	}
	if onset, ok := (&Handlers{}).findKillfeedRowOnset(fingerprint, timeline, false, true); !ok || onset != 12 {
		t.Fatalf("clip-boundary onset = %.3f, ok = %v; want 12.000", onset, ok)
	}
}

func TestReadStreamKillfeedFallsBackWhenVisionAndDetectorCountsDisagree(t *testing.T) {
	weapon := streamclips.WeaponKeys()[0]
	killsJSON := `{"kills":[` +
		`{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"first","weapon":"` + weapon + `"},` +
		`{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"second","weapon":"` + weapon + `"}` +
		`]}`
	srv := fakeXAIKillfeedServer(t, killsJSON)
	h, id := killfeedReadHandlers(t, srv.URL, "ffmpeg", "xai_test")
	h.killfeedNoticeRows = func(image.Image, *streamclips.CropRect) []streamclips.NoticeRow {
		return []streamclips.NoticeRow{{X: 1600, Y: 73, Width: 300, Height: 34}}
	}
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", strings.NewReader(`{"clip_id":"clip-1","cue_seconds":2}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	var body readKillfeedResponse
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rw.Body.String())
	}
	if body.Aligned || len(body.Events) != 1 || body.Events[0].CueSeconds != 2 || len(body.Events[0].Kills) != 2 {
		t.Fatalf("fallback response = %+v, want both kills kept at requested cue", body)
	}
}

func TestReadStreamKillfeedSamplesAfterCueButInsideClip(t *testing.T) {
	h, id := killfeedReadHandlers(t, "", "ffmpeg", "xai_test")
	var sampledAt float64
	h.killfeedFrame = func(_ context.Context, _ string, seconds float64) (image.Image, error) {
		sampledAt = seconds
		return nil, errors.New("stop after recording sample time")
	}
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", strings.NewReader(`{"clip_id":"clip-1","cue_seconds":4.9}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 from the test sentinel; body=%s", rw.Code, rw.Body.String())
	}
	if want := 4.95; math.Abs(sampledAt-want) > 1e-9 {
		t.Fatalf("sampled frame = %.3f, want %.3f so the read cannot drift into the next clip", sampledAt, want)
	}
}

func TestReadStreamKillfeedMissingXAIKeyReturns409Code(t *testing.T) {
	h, id := killfeedReadHandlers(t, "", "ffmpeg", "") // ffmpeg configured, no xAI key
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", strings.NewReader(`{"clip_id":"clip-1","cue_seconds":2}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "xai_key_missing" {
		t.Fatalf("code = %q, want xai_key_missing; body=%s", body.Code, rw.Body.String())
	}
}

func TestReadStreamKillfeedUpstreamFailureReturns502Code(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit exceeded"}`))
	}))
	t.Cleanup(srv.Close)

	h, id := killfeedReadHandlers(t, srv.URL, "ffmpeg", "xai_test")
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", strings.NewReader(`{"clip_id":"clip-1","cue_seconds":2}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rw.Code, rw.Body.String())
	}
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != "xai_request_failed" {
		t.Fatalf("code = %q, want xai_request_failed; body=%s", body.Code, rw.Body.String())
	}
	if !strings.Contains(body.Error, "rate limit exceeded") {
		t.Fatalf("error = %q, want the upstream message surfaced", body.Error)
	}
}

func TestReadStreamKillfeedMissingFFmpegReturns409(t *testing.T) {
	h, id := killfeedReadHandlers(t, "", "", "xai_test") // xAI configured, no ffmpeg
	r := Routes(h)

	req := httptest.NewRequest(http.MethodPost, "/api/stream-jobs/"+id.String()+"/killfeed-read", strings.NewReader(`{"clip_id":"clip-1","cue_seconds":2}`))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rw.Code, rw.Body.String())
	}
}

func TestPreviewStreamKillfeedNoticeReturnsPNG(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})
	r := Routes(h)

	weapon := streamclips.WeaponKeys()[0]
	body := `{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"villain","weapon":"` + weapon + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/stream-killfeed/notice-preview", strings.NewReader(body))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	if got := rw.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content-type = %q, want image/png", got)
	}
	if !strings.HasPrefix(rw.Body.String(), "\x89PNG\r\n\x1a\n") {
		t.Fatalf("body is not a PNG: % x", rw.Body.Bytes()[:min(8, rw.Body.Len())])
	}
}

func TestPreviewStreamKillfeedNoticeRejectsUnknownWeapon(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})
	r := Routes(h)

	body := `{"attacker_side":"CT","attacker_name":"hero","victim_side":"T","victim_name":"villain","weapon":"not-a-real-weapon"}`
	req := httptest.NewRequest(http.MethodPost, "/api/stream-killfeed/notice-preview", strings.NewReader(body))
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rw.Code, rw.Body.String())
	}
}

// A 1080p killfeed crop is only ~150px tall, where the vision model misreads
// player names and weapon icons. The crop must reach the reader enlarged, with
// its aspect ratio and its side-encoding colours intact.
func TestEncodeKillfeedCropPNGEnlargesSmallCrops(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	ctBlue := color.RGBA{R: 0x8a, G: 0xc8, B: 0xff, A: 0xff}
	for y := range 1080 {
		for x := range 1920 {
			frame.Set(x, y, ctBlue)
		}
	}
	crop := streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}

	data, err := encodeKillfeedCropPNG(frame, crop)
	if err != nil {
		t.Fatalf("encodeKillfeedCropPNG error = %v", err)
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode encoded crop: %v", err)
	}

	nativeWidth := int(crop.Width * 1920)
	nativeHeight := int(crop.Height * 1080)
	got := decoded.Bounds()
	if got.Dx() <= nativeWidth {
		t.Fatalf("encoded width = %d, want it enlarged beyond the native %d", got.Dx(), nativeWidth)
	}
	if got.Dx() > killfeedCropTargetWidth*2 {
		t.Fatalf("encoded width = %d, want it capped near %d", got.Dx(), killfeedCropTargetWidth)
	}
	// Enlarging must not distort the notices: a stretched crop would misplace
	// the names the reader pairs with each weapon icon.
	wantRatio := float64(nativeWidth) / float64(nativeHeight)
	gotRatio := float64(got.Dx()) / float64(got.Dy())
	if math.Abs(gotRatio-wantRatio) > 0.01 {
		t.Fatalf("aspect ratio = %.4f, want %.4f", gotRatio, wantRatio)
	}
	// The side of a name is encoded purely in its colour, so enlarging must not
	// blend it toward a neighbouring colour.
	r, g, b, _ := decoded.At(got.Dx()/2, got.Dy()/2).RGBA()
	if uint8(r>>8) != ctBlue.R || uint8(g>>8) != ctBlue.G || uint8(b>>8) != ctBlue.B {
		t.Fatalf("centre pixel = (%d,%d,%d), want the source colour (%d,%d,%d)",
			r>>8, g>>8, b>>8, ctBlue.R, ctBlue.G, ctBlue.B)
	}
}

func TestListStreamKillfeedWeapons(t *testing.T) {
	h := NewHandlers(newFakeRepo(), newFakeStorage(), &fakeQueue{})
	r := Routes(h)

	req := httptest.NewRequest(http.MethodGet, "/api/stream-killfeed/weapons", nil)
	rw := httptest.NewRecorder()
	r.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rw.Code, rw.Body.String())
	}
	var body struct {
		Weapons []string `json:"weapons"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Weapons) == 0 {
		t.Fatalf("weapons = %v, want the non-empty catalog", body.Weapons)
	}
}
