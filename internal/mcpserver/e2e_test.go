package mcpserver

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// fakeOrchestrator is a stateful in-memory stand-in for the orchestrator's
// demo->reel state machine, enough to drive the create -> scan -> pick-target ->
// parse -> plan walk that the spec's E2E exercises. A job advances one stage per
// couple of GET polls, so next_step-driven polling behaves as it would against
// real async workers. Capture stages are disabled, matching a Linux host.
type fakeOrchestrator struct {
	mu   sync.Mutex
	jobs map[string]*fakeJob
}

type fakeJob struct {
	status string
	polls  int
}

func newFakeOrchestrator() *fakeOrchestrator {
	return &fakeOrchestrator{jobs: map[string]*fakeJob{}}
}

// getAdvance returns the job, advancing scanning->scanned and parsing->parsed
// after a second observation so polling loops iterate at least once.
func (o *fakeOrchestrator) getAdvance(id string) (*fakeJob, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	j, ok := o.jobs[id]
	if !ok {
		return nil, false
	}
	j.polls++
	if j.polls >= 2 {
		switch j.status {
		case tuiclient.StatusScanning:
			j.status = tuiclient.StatusScanned
		case tuiclient.StatusParsing:
			j.status = tuiclient.StatusParsed
		}
	}
	return j, true
}

func (o *fakeOrchestrator) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		o.mu.Lock()
		id := "job-1"
		o.jobs[id] = &fakeJob{status: tuiclient.StatusScanning}
		o.mu.Unlock()
		writeJSON(w, http.StatusCreated, tuiclient.CreateJobResponse{ID: id, Status: tuiclient.StatusScanning})
	})
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		j, ok := o.getAdvance(r.PathValue("id"))
		if !ok {
			http.Error(w, "no such job", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, tuiclient.Job{ID: r.PathValue("id"), Status: j.status})
	})
	mux.HandleFunc("GET /api/jobs/{id}/roster", func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		j, ok := o.jobs[r.PathValue("id")]
		o.mu.Unlock()
		if !ok || j.status == tuiclient.StatusScanning {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "roster not ready"})
			return
		}
		writeJSON(w, http.StatusOK, tuiclient.RosterResult{
			Players: []tuiclient.PlayerStat{{SteamID64: "76561198000000000", Name: "ace", Kills: 30, Rating: 1.4}},
			Match:   tuiclient.MatchInfo{Map: "de_nuke", Rounds: 24},
		})
	})
	mux.HandleFunc("POST /api/jobs/{id}/parse", func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		j, ok := o.jobs[r.PathValue("id")]
		if ok {
			j.status = tuiclient.StatusParsing
			j.polls = 0
		}
		o.mu.Unlock()
		if !ok {
			http.Error(w, "no such job", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, tuiclient.CreateJobResponse{ID: r.PathValue("id"), Status: tuiclient.StatusParsing})
	})
	mux.HandleFunc("GET /api/jobs/{id}/plan", func(w http.ResponseWriter, r *http.Request) {
		o.mu.Lock()
		j, ok := o.jobs[r.PathValue("id")]
		o.mu.Unlock()
		if !ok || j.status != tuiclient.StatusParsed {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "kill plan not ready"})
			return
		}
		writeJSON(w, http.StatusOK, tuiclient.Plan{
			SchemaVersion: "1.0",
			Target:        tuiclient.Target{SteamID64: "76561198000000000", NameInDemo: "ace"},
			Segments:      []tuiclient.Segment{{ID: "seg-001", Round: 1}, {ID: "seg-002", Round: 5}},
		})
	})
	mux.HandleFunc("GET /api/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		// Linux host: no capture tools configured.
		writeJSON(w, http.StatusOK, tuiclient.Capabilities{})
	})
	mux.HandleFunc("GET /api/presets", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.PresetList{Default: "viral-60-clean"})
	})
	mux.HandleFunc("GET /api/jobs/{id}/renders/{variant}", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	_ = t
	return mux
}

// TestE2EPipelineWalk drives the read/mutate tools end to end through a real MCP
// client and server (in-memory stdio-equivalent transport, the production
// registerTools path) against the stateful orchestrator simulator: upload ->
// roster -> parse -> plan, then confirms the capability gate stops record/render
// on a capture-less host. It needs no committed .dem, so it runs everywhere; the
// real zv-serve + fixture-.dem parse path is covered by the orchestrator's own
// package tests.
func TestE2EPipelineWalk(t *testing.T) {
	mux := http.NewServeMux()
	orch := newFakeOrchestrator()
	mux.Handle("/", orch.handler(t))
	cs := newMCPSession(t, depsFor(t, newStubServer(t, mux)))

	demo := filepath.Join(t.TempDir(), "match.dem")
	if err := os.WriteFile(demo, []byte("PBDEMS2fake-demo-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 1. Upload the demo, starting the roster-scan flow (no target yet).
	var created createJobOutput
	decodeOK(t, callTool(t, cs, "create_job", map[string]any{"demo_path": demo}), &created)
	if created.ID == "" || created.PollHintSeconds != pollHintParse {
		t.Fatalf("create_job = %+v, want a job id and poll hint 2", created)
	}
	jobID := created.ID

	// 2. Poll next_step until the scan finishes and a target pick is due.
	awaitStep(t, cs, jobID, string(tuiclient.StepPickTarget))

	// 3. Inspect the roster and pick the top fragger.
	var roster tuiclient.RosterResult
	decodeOK(t, callTool(t, cs, "get_roster", map[string]any{"job_id": jobID}), &roster)
	if len(roster.Players) == 0 {
		t.Fatal("roster has no players to pick from")
	}
	target := roster.Players[0].SteamID64

	// 4. Assign the target and start parsing.
	var parse createJobOutput
	decodeOK(t, callTool(t, cs, "start_parse", map[string]any{"job_id": jobID, "target_steamid": target}), &parse)
	if parse.Status != tuiclient.StatusParsing {
		t.Fatalf("start_parse status = %q, want parsing", parse.Status)
	}

	// 5. Poll next_step until parsing finishes and recording is due.
	awaitStep(t, cs, jobID, string(tuiclient.StepRecord))

	// 6. Read the kill plan.
	var plan tuiclient.Plan
	decodeOK(t, callTool(t, cs, "get_plan", map[string]any{"job_id": jobID}), &plan)
	if len(plan.Segments) == 0 {
		t.Fatal("kill plan has no segments")
	}

	// 7. On a capture-less host, record/render are gated with capability_missing.
	var caps tuiclient.Capabilities
	decodeOK(t, callTool(t, cs, "get_capabilities", nil), &caps)
	if caps.Record.Enabled || caps.Render.Enabled {
		t.Fatal("expected record/render disabled on a Linux capture-less host")
	}
	recErr := decodeErr(t, callTool(t, cs, "start_recording", map[string]any{"job_id": jobID}))
	if recErr.Code != codeCapabilityMissing {
		t.Errorf("start_recording code = %q, want capability_missing", recErr.Code)
	}
	renErr := decodeErr(t, callTool(t, cs, "start_render", map[string]any{"job_id": jobID, "variant": "viral-60-clean"}))
	if renErr.Code != codeCapabilityMissing {
		t.Errorf("start_render code = %q, want capability_missing", renErr.Code)
	}
}

// awaitStep polls next_step until it reports wantStep, bounded by a deadline so
// a stuck state machine fails the test instead of hanging.
func awaitStep(t *testing.T, cs *mcp.ClientSession, jobID, wantStep string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var ns NextStepResult
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "next_step",
			Arguments: map[string]any{"job_id": jobID},
		})
		if err != nil {
			t.Fatalf("next_step: %v", err)
		}
		decodeOK(t, res, &ns)
		if ns.Step == wantStep {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach step %q within deadline", jobID, wantStep)
}
