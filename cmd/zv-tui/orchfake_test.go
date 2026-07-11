package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// orchFake is a contract-faithful, in-memory double of the FragForge
// orchestrator HTTP API, used to drive the TUI end to end without a real
// Windows+GPU capture host. It advances job state on the same mutating calls the
// real server accepts, so a teatest run exercises the whole flow: upload -> scan
// -> pick player -> parse -> record -> compose -> render -> download, plus the
// stream-clip flow.
//
// Media stages default to ENABLED (unlike the Linux dev orchestrator) so the E2E
// can cover record/compose/render. Async stages (record/compose/render) complete
// synchronously by default for deterministic tests; call holdInFlight(true) to
// make them park in their in-flight status until release* is called, so a test
// can assert the "recording"/"rendering" states and poll-driven completion.
type orchFake struct {
	mu sync.Mutex

	caps    tuiclient.Capabilities
	presets tuiclient.PresetList

	jobs      map[string]*fakeJob
	streams   map[string]*fakeStream
	seq       int
	nowMillis int64

	// hold parks async stages in their in-flight status until released.
	hold bool
}

type fakeJob struct {
	job     tuiclient.Job
	roster  *tuiclient.RosterResult
	plan    *tuiclient.Plan
	moments *tuiclient.MomentsDocument
	renders map[string]*tuiclient.RenderVariantState
	// pending in-flight stage that hold parks: "record"|"compose"|"render:<variant>".
	pending  string
	uploaded bool
}

type fakeStream struct {
	job     tuiclient.StreamJob
	plan    *tuiclient.StreamEditPlan
	renders map[string]*tuiclient.StreamRenderState
	pending string
}

func newOrchFake() *orchFake {
	f := &orchFake{
		jobs:    map[string]*fakeJob{},
		streams: map[string]*fakeStream{},
	}
	// Everything a capture host would offer, so the E2E can run every stage.
	f.caps.Record = tuiclient.CapabilityGroup{Enabled: true, Tools: []tuiclient.CaptureTool{{Name: "hlae", Configured: true, Accessible: true}}}
	f.caps.Render = tuiclient.CapabilityGroup{Enabled: true, Tools: []tuiclient.CaptureTool{{Name: "ffmpeg", Configured: true, Accessible: true}}}
	f.caps.Compose.Enabled = true
	f.caps.Stream = tuiclient.StreamCapabilities{YtdlpEnabled: true}
	f.presets = tuiclient.PresetList{
		Default: fallbackRenderVariant,
		Presets: []tuiclient.Preset{
			{Name: fallbackRenderVariant, Label: "Viral 60 (clean)", Description: "60fps, no effects", Default: true, FPS: 60, Width: 1080, Height: 1920},
			{Name: "hype-30", Label: "Hype 30", Description: "punchy", FPS: 30, Width: 1080, Height: 1920},
		},
	}
	return f
}

func (f *orchFake) server() *httptest.Server { return httptest.NewServer(f.routes()) }

// seedJob inserts a demo job already at the given status, with roster + plan
// populated, so a test can start partway through the pipeline without driving
// every prior step. Returns the job id.
func (f *orchFake) seedJob(status string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID("job")
	j := &fakeJob{
		job: tuiclient.Job{
			ID: id, Status: status, DemoPath: "seed.dem", TargetSteamID: targetSteamID,
			CreatedAt: f.now(), UpdatedAt: f.now(),
		},
		roster:  demoRoster(),
		plan:    demoPlan(targetSteamID),
		moments: demoMoments(id),
		renders: map[string]*tuiclient.RenderVariantState{},
	}
	if status == tuiclient.StatusFailed {
		j.job.FailureReason = "parser crashed"
	}
	j.job.KillPlan = j.plan
	f.jobs[id] = j
	return id
}

// holdInFlight makes async stages park in their in-flight status (recording /
// composing / rendering) instead of completing immediately.
func (f *orchFake) holdInFlight(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hold = on
}

// release completes whatever stage a held job is parked on, as a real worker
// would: recording -> recorded, composing -> composed, rendering -> ready. A
// later poll then surfaces the new state in the UI.
func (f *orchFake) release(jobID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	j := f.jobs[jobID]
	if j == nil {
		return
	}
	switch {
	case j.pending == "record":
		j.job.Status = tuiclient.StatusRecorded
	case j.pending == "compose":
		j.job.Status = tuiclient.StatusComposed
	case strings.HasPrefix(j.pending, "render:"):
		if st := j.renders[strings.TrimPrefix(j.pending, "render:")]; st != nil {
			st.Status = tuiclient.RenderReady
			st.UpdatedAt = f.now()
		}
	}
	j.pending = ""
	j.job.UpdatedAt = f.now()
}

func (f *orchFake) now() time.Time {
	// Deterministic clock: monotonically increasing, no wall-clock dependency so
	// ordering in the UI ("11h ago" style deltas) is stable across runs.
	f.nowMillis += 1000
	return time.Unix(0, f.nowMillis*int64(time.Millisecond)).UTC()
}

func (f *orchFake) nextID(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%d", prefix, f.seq)
}

func (f *orchFake) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/capabilities", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		writeJSON(w, 200, f.caps)
	})
	mux.HandleFunc("GET /api/presets", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		writeJSON(w, 200, f.presets)
	})

	// ---- demo -> reel --------------------------------------------------------
	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		out := struct {
			Jobs []tuiclient.Job `json:"jobs"`
		}{Jobs: []tuiclient.Job{}}
		for _, j := range f.orderedJobs() {
			out.Jobs = append(out.Jobs, j.job)
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("POST /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		id := f.nextID("job")
		j := &fakeJob{
			job: tuiclient.Job{
				ID:        id,
				Status:    tuiclient.StatusScanned,
				DemoPath:  "uploaded.dem",
				CreatedAt: f.now(),
				UpdatedAt: f.now(),
			},
			roster:  demoRoster(),
			renders: map[string]*tuiclient.RenderVariantState{},
		}
		f.jobs[id] = j
		writeJSON(w, 201, tuiclient.CreateJobResponse{ID: id, Status: j.job.Status})
	})
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		j := f.jobs[r.PathValue("id")]
		if j == nil {
			writeErr(w, 404, "not found")
			return
		}
		writeJSON(w, 200, j.job)
	})
	mux.HandleFunc("GET /api/jobs/{id}/roster", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		j := f.jobs[r.PathValue("id")]
		if j == nil || j.roster == nil {
			writeErr(w, 409, "roster not ready")
			return
		}
		writeJSON(w, 200, *j.roster)
	})
	mux.HandleFunc("POST /api/jobs/{id}/parse", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TargetSteamID string `json:"target_steamid"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		j := f.jobs[r.PathValue("id")]
		if j == nil {
			writeErr(w, 404, "not found")
			return
		}
		j.job.Status = tuiclient.StatusParsed
		j.job.TargetSteamID = body.TargetSteamID
		j.plan = demoPlan(body.TargetSteamID)
		j.job.KillPlan = j.plan
		j.moments = demoMoments(j.job.ID)
		j.job.UpdatedAt = f.now()
		writeJSON(w, 200, tuiclient.CreateJobResponse{ID: j.job.ID, Status: j.job.Status})
	})
	mux.HandleFunc("GET /api/jobs/{id}/plan", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		j := f.jobs[r.PathValue("id")]
		if j == nil || j.plan == nil {
			writeErr(w, 409, "plan not ready")
			return
		}
		writeJSON(w, 200, *j.plan)
	})
	mux.HandleFunc("GET /api/jobs/{id}/moments", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		j := f.jobs[r.PathValue("id")]
		if j == nil || j.moments == nil {
			writeErr(w, 409, "moments not ready")
			return
		}
		writeJSON(w, 200, *j.moments)
	})
	mux.HandleFunc("POST /api/jobs/{id}/record", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		j := f.jobs[r.PathValue("id")]
		if j == nil {
			writeErr(w, 404, "not found")
			return
		}
		if f.hold {
			j.job.Status = tuiclient.StatusRecording
			j.pending = "record"
		} else {
			j.job.Status = tuiclient.StatusRecorded
		}
		j.job.UpdatedAt = f.now()
		writeJSON(w, 202, tuiclient.EnqueueResponse{ID: j.job.ID, Task: "record", Status: j.job.Status})
	})
	mux.HandleFunc("POST /api/jobs/{id}/compose", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		j := f.jobs[r.PathValue("id")]
		if j == nil {
			writeErr(w, 404, "not found")
			return
		}
		if f.hold {
			j.job.Status = tuiclient.StatusComposing
			j.pending = "compose"
		} else {
			j.job.Status = tuiclient.StatusComposed
		}
		j.job.UpdatedAt = f.now()
		writeJSON(w, 202, tuiclient.EnqueueResponse{ID: j.job.ID, Task: "compose", Status: j.job.Status})
	})
	mux.HandleFunc("POST /api/jobs/{id}/renders/{variant}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		j := f.jobs[r.PathValue("id")]
		if j == nil {
			writeErr(w, 404, "not found")
			return
		}
		variant := r.PathValue("variant")
		st := &tuiclient.RenderVariantState{JobID: j.job.ID, Variant: variant, Preset: variant, CreatedAt: f.now(), UpdatedAt: f.now()}
		if f.hold {
			st.Status = tuiclient.RenderRendering
			j.pending = "render:" + variant
		} else {
			st.Status = tuiclient.RenderReady
		}
		j.renders[variant] = st
		writeJSON(w, 202, tuiclient.EnqueueResponse{ID: j.job.ID, Task: "render", Variant: variant, Status: st.Status})
	})
	mux.HandleFunc("GET /api/jobs/{id}/renders/{variant}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		j := f.jobs[r.PathValue("id")]
		if j == nil {
			writeErr(w, 404, "not found")
			return
		}
		st := j.renders[r.PathValue("variant")]
		if st == nil {
			writeErr(w, 404, "variant never requested")
			return
		}
		writeJSON(w, 200, *st)
	})
	mux.HandleFunc("GET /api/jobs/{id}/renders/{variant}/publish", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		j := f.jobs[r.PathValue("id")]
		if j == nil {
			writeErr(w, 404, "not found")
			return
		}
		variant := r.PathValue("variant")
		st := j.renders[variant]
		if st == nil || st.Status != tuiclient.RenderReady {
			writeErr(w, 409, "render not ready")
			return
		}
		writeJSON(w, 200, tuiclient.PublishBoard{
			JobID: j.job.ID, Variant: variant, Status: "ready", UploadReadyRoot: "jobs/" + j.job.ID + "/upload-ready",
			RenderReady: true, Uploaded: j.uploaded, UpdatedAt: f.now(),
			Items: []tuiclient.PublishBoardItem{
				{SegmentID: "seg-1", Status: "ready", VideoReady: true, CoverReady: true, CaptionReady: true},
			},
		})
	})
	mux.HandleFunc("POST /api/jobs/{id}/renders/{variant}/publish/uploaded", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Uploaded bool `json:"uploaded"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		j := f.jobs[r.PathValue("id")]
		if j == nil {
			writeErr(w, 404, "not found")
			return
		}
		j.uploaded = body.Uploaded
		writeJSON(w, 200, map[string]any{"uploaded": body.Uploaded})
	})
	mux.HandleFunc("GET /api/jobs/{id}/final", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		j := f.jobs[r.PathValue("id")]
		composed := j != nil && (j.job.Status == tuiclient.StatusComposed || j.job.Status == tuiclient.StatusDone)
		f.mu.Unlock()
		if !composed {
			writeErr(w, 409, "not composed")
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("FAKE-MP4-BYTES"))
	})

	// ---- stream clips --------------------------------------------------------
	mux.HandleFunc("GET /api/stream-jobs", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		out := struct {
			Jobs []tuiclient.StreamJob `json:"jobs"`
		}{Jobs: []tuiclient.StreamJob{}}
		for _, s := range f.orderedStreams() {
			out.Jobs = append(out.Jobs, s.job)
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("POST /api/stream-jobs", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		id := f.nextID("stream")
		fromURL := strings.HasPrefix(r.Header.Get("Content-Type"), "application/json")
		status := tuiclient.StreamReady
		if fromURL {
			status = tuiclient.StreamAcquiring
		}
		s := &fakeStream{
			job: tuiclient.StreamJob{
				ID:        id,
				Status:    status,
				Title:     "clip",
				Probe:     tuiclient.SourceProbe{Width: 1920, Height: 1080, DurationSeconds: 42},
				CreatedAt: f.now(),
				UpdatedAt: f.now(),
			},
			renders: map[string]*tuiclient.StreamRenderState{},
		}
		f.streams[id] = s
		writeJSON(w, 201, tuiclient.StreamCreateResponse{ID: id, Status: s.job.Status, Probe: s.job.Probe})
	})
	mux.HandleFunc("GET /api/stream-jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		s := f.streams[r.PathValue("id")]
		if s == nil {
			writeErr(w, 404, "not found")
			return
		}
		writeJSON(w, 200, s.job)
	})
	mux.HandleFunc("GET /api/stream-jobs/{id}/edit-plan", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		s := f.streams[r.PathValue("id")]
		if s == nil {
			writeErr(w, 404, "not found")
			return
		}
		if s.plan == nil {
			writeJSON(w, 200, defaultStreamPlan())
			return
		}
		writeJSON(w, 200, *s.plan)
	})
	mux.HandleFunc("PUT /api/stream-jobs/{id}/edit-plan", func(w http.ResponseWriter, r *http.Request) {
		var plan tuiclient.StreamEditPlan
		_ = json.NewDecoder(r.Body).Decode(&plan)
		f.mu.Lock()
		defer f.mu.Unlock()
		s := f.streams[r.PathValue("id")]
		if s == nil {
			writeErr(w, 404, "not found")
			return
		}
		plan.UpdatedAt = f.now()
		s.plan = &plan
		writeJSON(w, 200, plan)
	})
	mux.HandleFunc("POST /api/stream-jobs/{id}/renders/{variant}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		s := f.streams[r.PathValue("id")]
		if s == nil {
			writeErr(w, 404, "not found")
			return
		}
		variant := r.PathValue("variant")
		st := &tuiclient.StreamRenderState{JobID: s.job.ID, Variant: variant, UpdatedAt: f.now()}
		if f.hold {
			st.Status = tuiclient.StreamRendering
			s.job.Status = tuiclient.StreamRendering
			s.pending = "render:" + variant
		} else {
			st.Status = tuiclient.StreamRendered
			s.job.Status = tuiclient.StreamRendered
			st.Videos = []tuiclient.StreamVideoEntry{{ClipID: "c1", Title: "clip", Key: "out.mp4", DurationSeconds: 20}}
		}
		s.renders[variant] = st
		writeJSON(w, 202, tuiclient.EnqueueResponse{ID: s.job.ID, Task: "stream-render", Variant: variant, Status: st.Status})
	})
	mux.HandleFunc("GET /api/stream-jobs/{id}/renders/{variant}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		s := f.streams[r.PathValue("id")]
		if s == nil {
			writeErr(w, 404, "not found")
			return
		}
		st := s.renders[r.PathValue("variant")]
		if st == nil {
			writeErr(w, 404, "variant never requested")
			return
		}
		writeJSON(w, 200, *st)
	})

	return mux
}

// orderedJobs returns jobs newest-first (highest seq first), matching the real
// list ordering the TUI expects.
func (f *orchFake) orderedJobs() []*fakeJob {
	out := make([]*fakeJob, 0, len(f.jobs))
	for _, j := range f.jobs {
		out = append(out, j)
	}
	sortBySeqDesc(out, func(j *fakeJob) string { return j.job.ID })
	return out
}

func (f *orchFake) orderedStreams() []*fakeStream {
	out := make([]*fakeStream, 0, len(f.streams))
	for _, s := range f.streams {
		out = append(out, s)
	}
	sortBySeqDesc(out, func(s *fakeStream) string { return s.job.ID })
	return out
}

// ---- fixtures --------------------------------------------------------------

const targetSteamID = "76561198000000001"

func demoRoster() *tuiclient.RosterResult {
	return &tuiclient.RosterResult{
		Match: tuiclient.MatchInfo{Map: "de_mirage", ScoreCT: 13, ScoreT: 9, Rounds: 22},
		Players: []tuiclient.PlayerStat{
			{SteamID64: targetSteamID, Name: "s1mple", Team: "T", Kills: 30, Deaths: 15, Assists: 4, Headshots: 18, Rounds: 22, ADR: 95.2, HSPct: 60, KAST: 78, Rating: 1.42},
			{SteamID64: "76561198000000002", Name: "ZywOo", Team: "CT", Kills: 26, Deaths: 17, Assists: 6, Headshots: 12, Rounds: 22, ADR: 88.1, HSPct: 46, KAST: 74, Rating: 1.31},
		},
	}
}

func demoPlan(target string) *tuiclient.Plan {
	return &tuiclient.Plan{
		SchemaVersion: "1",
		Demo:          tuiclient.PlanDemo{Path: "uploaded.dem", Map: "de_mirage", Tickrate: 64, DurationTicks: 200000},
		Target:        tuiclient.Target{SteamID64: target, NameInDemo: "s1mple", TeamAtStart: "T"},
		Segments: []tuiclient.Segment{
			{ID: "seg-1", Round: 3, TickStart: 1000, TickEnd: 2000, Kills: []tuiclient.Kill{{Tick: 1500, Weapon: "ak47", Headshot: true, Victim: tuiclient.KillVictim{SteamID64: "76561198000000009", NameInDemo: "bot", TeamAtKill: "CT"}}}},
			{ID: "seg-2", Round: 7, TickStart: 5000, TickEnd: 6200, Kills: []tuiclient.Kill{{Tick: 5500, Weapon: "awp", Headshot: false, Victim: tuiclient.KillVictim{SteamID64: "76561198000000008", NameInDemo: "bot2", TeamAtKill: "CT"}}}},
		},
		Stats: tuiclient.PlanStats{TotalKillsTarget: 30, KillsAfterFilters: 2, SegmentsCreated: 2, DurationSecondsTotal: 34.5},
	}
}

func demoMoments(jobID string) *tuiclient.MomentsDocument {
	return &tuiclient.MomentsDocument{
		SchemaVersion: "1", JobID: jobID,
		Moments: []tuiclient.Moment{
			{ID: "m-1", SegmentID: "seg-1", Round: 3, DurationSeconds: 15, Score: 0.9, ReasonCodes: []string{"headshot"}, Events: tuiclient.MomentEvents{Kills: 1, Headshots: 1}, DefaultVariant: fallbackRenderVariant},
		},
	}
}

func defaultStreamPlan() tuiclient.StreamEditPlan {
	return tuiclient.StreamEditPlan{
		SchemaVersion: "1",
		Variant:       tuiclient.StreamDefaultVariant,
		FaceCrop:      tuiclient.CropRect{X: 0, Y: 0, Width: 0.4, Height: 1},
		GameplayCrop:  tuiclient.CropRect{X: 0.4, Y: 0, Width: 0.6, Height: 1},
		Clips:         []tuiclient.ClipRange{{ID: "c1", StartSeconds: 0, EndSeconds: 20, Title: "clip"}},
	}
}

// ---- helpers ---------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// sortBySeqDesc orders items newest-first by the numeric suffix of their id
// ("job-3" before "job-1"), a stable stand-in for the server's created_at desc.
func sortBySeqDesc[T any](items []T, id func(T) string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && seqOf(id(items[j-1])) < seqOf(id(items[j])); j-- {
			items[j-1], items[j] = items[j], items[j-1]
		}
	}
}

func seqOf(id string) int {
	if i := strings.LastIndexByte(id, '-'); i >= 0 {
		n := 0
		for _, c := range id[i+1:] {
			if c < '0' || c > '9' {
				return 0
			}
			n = n*10 + int(c-'0')
		}
		return n
	}
	return 0
}
