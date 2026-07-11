package mcpserver

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// readStub serves the read-only endpoints with representative DTOs. Render
// variants 404 (none requested), so next_step derives from the job status.
func readStub(t *testing.T) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		caps := tuiclient.Capabilities{}
		caps.Record.Enabled = true
		caps.Render.Enabled = true
		writeJSON(w, http.StatusOK, caps)
	})
	mux.HandleFunc("GET /api/presets", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.PresetList{
			Default: "viral-60-clean",
			Presets: []tuiclient.Preset{{Name: "viral-60-clean", Default: true}},
		})
	})
	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "5" {
			t.Errorf("list_jobs limit query = %q, want 5", got)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"jobs": []tuiclient.Job{{ID: "j1", Status: tuiclient.StatusParsed}},
		})
	})
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.Job{ID: r.PathValue("id"), Status: tuiclient.StatusParsed})
	})
	mux.HandleFunc("GET /api/jobs/{id}/roster", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.RosterResult{
			Players: []tuiclient.PlayerStat{{SteamID64: "76561198000000000", Name: "player", Kills: 30}},
			Match:   tuiclient.MatchInfo{Map: "de_nuke"},
		})
	})
	mux.HandleFunc("GET /api/jobs/{id}/plan", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.Plan{
			SchemaVersion: "1.0",
			Target:        tuiclient.Target{SteamID64: "76561198000000000"},
			Segments:      []tuiclient.Segment{{ID: "seg-001", Round: 1}},
		})
	})
	mux.HandleFunc("GET /api/jobs/{id}/moments", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.MomentsDocument{
			SchemaVersion: "1.0",
			JobID:         r.PathValue("id"),
			Moments:       []tuiclient.Moment{{ID: "m1", SegmentID: "seg-001", Score: 9.5}},
		})
	})
	mux.HandleFunc("GET /api/jobs/{id}/renders/{variant}", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	return mux
}

func TestReadToolsReturnDTOs(t *testing.T) {
	cs := newMCPSession(t, depsFor(t, newStubServer(t, readStub(t))))

	t.Run("get_capabilities", func(t *testing.T) {
		var caps tuiclient.Capabilities
		decodeOK(t, callTool(t, cs, "get_capabilities", nil), &caps)
		if !caps.Record.Enabled || !caps.Render.Enabled {
			t.Errorf("caps = %+v, want record and render enabled", caps)
		}
	})
	t.Run("list_presets", func(t *testing.T) {
		var list tuiclient.PresetList
		decodeOK(t, callTool(t, cs, "list_presets", nil), &list)
		if list.Default != "viral-60-clean" {
			t.Errorf("Default = %q, want viral-60-clean", list.Default)
		}
	})
	t.Run("list_jobs", func(t *testing.T) {
		var out listJobsOutput
		decodeOK(t, callTool(t, cs, "list_jobs", map[string]any{"limit": 5}), &out)
		if len(out.Jobs) != 1 || out.Jobs[0].ID != "j1" {
			t.Errorf("jobs = %+v, want one job j1", out.Jobs)
		}
	})
	t.Run("get_job", func(t *testing.T) {
		var job tuiclient.Job
		decodeOK(t, callTool(t, cs, "get_job", map[string]any{"job_id": "j1"}), &job)
		if job.Status != tuiclient.StatusParsed {
			t.Errorf("Status = %q, want parsed", job.Status)
		}
	})
	t.Run("get_roster", func(t *testing.T) {
		var roster tuiclient.RosterResult
		decodeOK(t, callTool(t, cs, "get_roster", map[string]any{"job_id": "j1"}), &roster)
		if len(roster.Players) != 1 || roster.Players[0].Kills != 30 {
			t.Errorf("players = %+v, want one player with 30 kills", roster.Players)
		}
	})
	t.Run("get_plan", func(t *testing.T) {
		var plan tuiclient.Plan
		decodeOK(t, callTool(t, cs, "get_plan", map[string]any{"job_id": "j1"}), &plan)
		if len(plan.Segments) != 1 || plan.Segments[0].ID != "seg-001" {
			t.Errorf("segments = %+v, want seg-001", plan.Segments)
		}
	})
	t.Run("get_moments", func(t *testing.T) {
		var doc tuiclient.MomentsDocument
		decodeOK(t, callTool(t, cs, "get_moments", map[string]any{"job_id": "j1"}), &doc)
		if len(doc.Moments) != 1 || doc.Moments[0].ID != "m1" {
			t.Errorf("moments = %+v, want m1", doc.Moments)
		}
	})
	t.Run("next_step derives from job status", func(t *testing.T) {
		var ns NextStepResult
		decodeOK(t, callTool(t, cs, "next_step", map[string]any{"job_id": "j1"}), &ns)
		if ns.Step != string(tuiclient.StepRecord) || ns.SuggestedTool != "start_recording" {
			t.Errorf("next_step = %+v, want step=record tool=start_recording", ns)
		}
	})
}

func TestMutateToolsHappyPath(t *testing.T) {
	demo := filepath.Join(t.TempDir(), "match.dem")
	if err := os.WriteFile(demo, []byte("PBDEMS2fake"), 0o600); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		caps := tuiclient.Capabilities{}
		caps.Record.Enabled = true
		caps.Render.Enabled = true
		writeJSON(w, http.StatusOK, caps)
	})
	mux.HandleFunc("POST /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		writeJSON(w, http.StatusCreated, tuiclient.CreateJobResponse{ID: "new", Status: tuiclient.StatusScanning})
	})
	mux.HandleFunc("POST /api/jobs/{id}/parse", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.CreateJobResponse{ID: r.PathValue("id"), Status: tuiclient.StatusParsing})
	})
	mux.HandleFunc("POST /api/jobs/{id}/record", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, tuiclient.EnqueueResponse{ID: r.PathValue("id"), Task: "record:demo"})
	})
	mux.HandleFunc("POST /api/jobs/{id}/compose", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, tuiclient.EnqueueResponse{ID: r.PathValue("id"), Task: "compose"})
	})
	mux.HandleFunc("POST /api/jobs/{id}/renders/{variant}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusAccepted, tuiclient.EnqueueResponse{ID: r.PathValue("id"), Variant: r.PathValue("variant")})
	})
	mux.HandleFunc("GET /api/jobs/{id}/renders/{variant}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.RenderVariantState{
			JobID:   r.PathValue("id"),
			Variant: r.PathValue("variant"),
			Status:  tuiclient.RenderReady,
		})
	})
	mux.HandleFunc("GET /api/jobs/{id}/final", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("MP4DATA-final-bytes"))
	})
	cs := newMCPSession(t, depsFor(t, newStubServer(t, mux)))

	t.Run("create_job", func(t *testing.T) {
		var out createJobOutput
		decodeOK(t, callTool(t, cs, "create_job", map[string]any{"demo_path": demo}), &out)
		if out.ID != "new" || out.PollHintSeconds != pollHintParse {
			t.Errorf("out = %+v, want id=new poll=2", out)
		}
	})
	t.Run("start_parse", func(t *testing.T) {
		var out createJobOutput
		decodeOK(t, callTool(t, cs, "start_parse", map[string]any{"job_id": "j1", "target_steamid": "76561198000000000"}), &out)
		if out.Status != tuiclient.StatusParsing || out.PollHintSeconds != pollHintParse {
			t.Errorf("out = %+v, want status=parsing poll=2", out)
		}
	})
	t.Run("start_recording", func(t *testing.T) {
		var out enqueueOutput
		decodeOK(t, callTool(t, cs, "start_recording", map[string]any{"job_id": "j1"}), &out)
		if out.Task != "record:demo" || out.PollHintSeconds != pollHintMedia {
			t.Errorf("out = %+v, want task=record:demo poll=10", out)
		}
	})
	t.Run("start_compose", func(t *testing.T) {
		var out enqueueOutput
		decodeOK(t, callTool(t, cs, "start_compose", map[string]any{"job_id": "j1"}), &out)
		if out.Task != "compose" || out.PollHintSeconds != pollHintMedia {
			t.Errorf("out = %+v, want task=compose poll=10", out)
		}
	})
	t.Run("start_render", func(t *testing.T) {
		var out enqueueOutput
		decodeOK(t, callTool(t, cs, "start_render", map[string]any{"job_id": "j1", "variant": "viral-60-clean"}), &out)
		if out.Variant != "viral-60-clean" || out.PollHintSeconds != pollHintMedia {
			t.Errorf("out = %+v, want variant=viral-60-clean poll=10", out)
		}
	})
	t.Run("get_render", func(t *testing.T) {
		var state tuiclient.RenderVariantState
		decodeOK(t, callTool(t, cs, "get_render", map[string]any{"job_id": "j1", "variant": "viral-60-clean"}), &state)
		if state.Status != tuiclient.RenderReady {
			t.Errorf("Status = %q, want ready", state.Status)
		}
	})
	t.Run("download_final writes the file and reports bytes", func(t *testing.T) {
		out := filepath.Join(outDir, "final.mp4")
		var res downloadFinalOutput
		decodeOK(t, callTool(t, cs, "download_final", map[string]any{"job_id": "j1", "out_path": out}), &res)
		if res.Path != out {
			t.Errorf("Path = %q, want %q", res.Path, out)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read downloaded file: %v", err)
		}
		if string(data) != "MP4DATA-final-bytes" {
			t.Errorf("file content = %q", data)
		}
		if res.BytesWritten != int64(len(data)) {
			t.Errorf("BytesWritten = %d, want %d", res.BytesWritten, len(data))
		}
	})
}

func TestToolErrorMapping(t *testing.T) {
	t.Run("404 maps to not_found", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "no such job", http.StatusNotFound)
		})
		cs := newMCPSession(t, depsFor(t, newStubServer(t, mux)))
		te := decodeErr(t, callTool(t, cs, "get_job", map[string]any{"job_id": "missing"}))
		if te.Code != codeNotFound || te.Retryable {
			t.Fatalf("got %+v, want not_found non-retryable", te)
		}
	})

	t.Run("409 maps to not_ready with embedded next_step", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/jobs/{id}/plan", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "kill plan not ready"})
		})
		mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, tuiclient.Job{ID: r.PathValue("id"), Status: tuiclient.StatusParsing})
		})
		mux.HandleFunc("GET /api/presets", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, tuiclient.PresetList{Default: "viral-60-clean"})
		})
		mux.HandleFunc("GET /api/jobs/{id}/renders/{variant}", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "not found", http.StatusNotFound)
		})
		cs := newMCPSession(t, depsFor(t, newStubServer(t, mux)))
		te := decodeErr(t, callTool(t, cs, "get_plan", map[string]any{"job_id": "j1"}))
		if te.Code != codeNotReady {
			t.Fatalf("Code = %q, want not_ready", te.Code)
		}
		if te.JobStatus != tuiclient.StatusParsing {
			t.Errorf("JobStatus = %q, want parsing", te.JobStatus)
		}
		if te.NextStep == nil || te.NextStep.Step != string(tuiclient.StepParsing) {
			t.Errorf("NextStep = %+v, want step=parsing", te.NextStep)
		}
	})

	t.Run("unreachable orchestrator maps to orchestrator_unavailable", func(t *testing.T) {
		cs := newMCPSession(t, unavailableDeps())
		te := decodeErr(t, callTool(t, cs, "get_job", map[string]any{"job_id": "j1"}))
		if te.Code != codeOrchestratorUnavailable || !te.Retryable {
			t.Fatalf("got %+v, want orchestrator_unavailable retryable", te)
		}
	})

	t.Run("create_job with a missing local demo fails before hitting the API", func(t *testing.T) {
		var posted atomic.Bool
		mux := http.NewServeMux()
		mux.HandleFunc("POST /api/jobs", func(w http.ResponseWriter, _ *http.Request) {
			posted.Store(true)
			writeJSON(w, http.StatusCreated, tuiclient.CreateJobResponse{ID: "new"})
		})
		cs := newMCPSession(t, depsFor(t, newStubServer(t, mux)))
		te := decodeErr(t, callTool(t, cs, "create_job", map[string]any{"demo_path": "/no/such/file.dem"}))
		if te.Code != codeNotFound {
			t.Fatalf("Code = %q, want not_found", te.Code)
		}
		if posted.Load() {
			t.Error("create_job uploaded despite a missing local demo file")
		}
	})
}

// TestCapabilityGateShortCircuits confirms start_recording and start_render fail
// fast with capability_missing when the stage is not configured, without ever
// issuing the mutate request that would burn GPU minutes or 4xx server-side.
func TestCapabilityGateShortCircuits(t *testing.T) {
	tests := []struct {
		name        string
		tool        string
		args        map[string]any
		mutatePath  string
		enableField func(*tuiclient.Capabilities)
	}{
		{
			name:       "record disabled",
			tool:       "start_recording",
			args:       map[string]any{"job_id": "j1"},
			mutatePath: "POST /api/jobs/{id}/record",
		},
		{
			name:       "render disabled",
			tool:       "start_render",
			args:       map[string]any{"job_id": "j1", "variant": "viral-60-clean"},
			mutatePath: "POST /api/jobs/{id}/renders/{variant}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mutated atomic.Bool
			mux := http.NewServeMux()
			mux.HandleFunc("GET /api/capabilities", func(w http.ResponseWriter, _ *http.Request) {
				// Both stages disabled: the requested one must gate.
				writeJSON(w, http.StatusOK, tuiclient.Capabilities{})
			})
			mux.HandleFunc(tt.mutatePath, func(w http.ResponseWriter, _ *http.Request) {
				mutated.Store(true)
				writeJSON(w, http.StatusAccepted, tuiclient.EnqueueResponse{ID: "j1"})
			})
			cs := newMCPSession(t, depsFor(t, newStubServer(t, mux)))

			te := decodeErr(t, callTool(t, cs, tt.tool, tt.args))
			if te.Code != codeCapabilityMissing || te.Retryable {
				t.Fatalf("got %+v, want capability_missing non-retryable", te)
			}
			if mutated.Load() {
				t.Errorf("%s issued the mutate request despite the missing capability", tt.tool)
			}
		})
	}
}
