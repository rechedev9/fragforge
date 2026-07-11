package mcpserver

import (
	"context"
	"net/http"
	"testing"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// TestNewNextStepResultMapping locks the Step -> agent-suggestion table (spec
// section "next_step is the agent's steering wheel"): every reconciler step maps
// to a stable suggested tool, args hint, actionable flag, label, and poll hint.
func TestNewNextStepResultMapping(t *testing.T) {
	tests := []struct {
		step         tuiclient.Step
		wantActable  bool
		wantLabel    string
		wantTool     string
		wantArgsHint string
		wantPollHint int
	}{
		{tuiclient.StepScanning, false, "scanning demo roster", "get_job", "poll get_job until status is scanned", pollHintParse},
		{tuiclient.StepPickTarget, true, "pick a player to feature", "get_roster", "inspect the roster, then start_parse with target_steamid", 0},
		{tuiclient.StepParsing, false, "parsing kill plan", "get_job", "poll get_job until status is parsed", pollHintParse},
		{tuiclient.StepRecord, true, "ready to record", "start_recording", "check get_capabilities, then start_recording (optional preset, segment_ids)", 0},
		{tuiclient.StepRecording, false, "recording gameplay", "get_job", "poll get_job until status is recorded", pollHintMedia},
		{tuiclient.StepRender, true, "ready to render a Short", "start_render", "pick a variant from list_presets, then start_render", 0},
		{tuiclient.StepRendering, false, "rendering Short", "get_render", "poll get_render for the variant until status is ready", pollHintMedia},
		{tuiclient.StepComposing, false, "composing final video", "get_job", "poll get_job until status is composed", pollHintMedia},
		// The reconciler's Actionable() set is pick-target/record/render/retry;
		// "ready" is a terminal state, so it is not flagged actionable even though
		// download_final is the suggested tool. next_step ports that faithfully.
		{tuiclient.StepReady, false, "reel ready", "download_final", "download_final with a local out_path", 0},
		{tuiclient.StepRetry, true, "failed - retry recording", "get_job", "job failed; read failure_reason, then re-run the failed stage (e.g. start_recording)", 0},
		{tuiclient.StepWait, false, "working", "get_job", "poll get_job", pollHintDefault},
	}
	for _, tt := range tests {
		t.Run(string(tt.step), func(t *testing.T) {
			got := newNextStepResult(tt.step)
			if got.Step != string(tt.step) {
				t.Errorf("Step = %q, want %q", got.Step, tt.step)
			}
			if got.Actionable != tt.wantActable {
				t.Errorf("Actionable = %v, want %v", got.Actionable, tt.wantActable)
			}
			if got.Label != tt.wantLabel {
				t.Errorf("Label = %q, want %q", got.Label, tt.wantLabel)
			}
			if got.SuggestedTool != tt.wantTool {
				t.Errorf("SuggestedTool = %q, want %q", got.SuggestedTool, tt.wantTool)
			}
			if got.SuggestedArgsHint != tt.wantArgsHint {
				t.Errorf("SuggestedArgsHint = %q, want %q", got.SuggestedArgsHint, tt.wantArgsHint)
			}
			if got.PollHintSeconds != tt.wantPollHint {
				t.Errorf("PollHintSeconds = %d, want %d", got.PollHintSeconds, tt.wantPollHint)
			}
		})
	}
}

// TestComputeNextStepRenderReadyOverridesFailedJob verifies the reconciler
// precedence port: a finished render is terminal even when the job later flags
// failed, so next_step points at download_final, not retry.
func TestComputeNextStepRenderReadyOverridesFailedJob(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.Job{ID: r.PathValue("id"), Status: tuiclient.StatusFailed})
	})
	mux.HandleFunc("GET /api/jobs/{id}/renders/{variant}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.RenderVariantState{Variant: r.PathValue("variant"), Status: tuiclient.RenderReady})
	})
	d := depsFor(t, newStubServer(t, mux))

	got, te := computeNextStep(context.Background(), d, "j1", "viral-60-clean")
	if te != nil {
		t.Fatalf("computeNextStep: %v", te)
	}
	if got.Step != string(tuiclient.StepReady) || got.SuggestedTool != "download_final" {
		t.Fatalf("got %+v, want step=ready tool=download_final", got)
	}
}

// TestComputeNextStepRenderNotFoundIsNil confirms a 404 render (never requested)
// is treated as no in-flight variant, so the job status drives the step.
func TestComputeNextStepRenderNotFoundIsNil(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.Job{ID: r.PathValue("id"), Status: tuiclient.StatusParsed})
	})
	mux.HandleFunc("GET /api/jobs/{id}/renders/{variant}", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	d := depsFor(t, newStubServer(t, mux))

	got, te := computeNextStep(context.Background(), d, "j1", "viral-60-clean")
	if te != nil {
		t.Fatalf("computeNextStep: %v", te)
	}
	if got.Step != string(tuiclient.StepRecord) {
		t.Fatalf("Step = %q, want %q", got.Step, tuiclient.StepRecord)
	}
}

// TestComputeNextStepResolvesDefaultVariant confirms an empty variant resolves
// through the preset registry's default before probing the render state.
func TestComputeNextStepResolvesDefaultVariant(t *testing.T) {
	var sawPresets, sawVariant string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.Job{ID: r.PathValue("id"), Status: tuiclient.StatusRecorded})
	})
	mux.HandleFunc("GET /api/presets", func(w http.ResponseWriter, r *http.Request) {
		sawPresets = r.URL.Path
		writeJSON(w, http.StatusOK, tuiclient.PresetList{Default: "viral-60-clean"})
	})
	mux.HandleFunc("GET /api/jobs/{id}/renders/{variant}", func(w http.ResponseWriter, r *http.Request) {
		sawVariant = r.PathValue("variant")
		http.Error(w, "not found", http.StatusNotFound)
	})
	d := depsFor(t, newStubServer(t, mux))

	got, te := computeNextStep(context.Background(), d, "j1", "")
	if te != nil {
		t.Fatalf("computeNextStep: %v", te)
	}
	if sawPresets != "/api/presets" {
		t.Errorf("presets endpoint not consulted for default variant")
	}
	if sawVariant != "viral-60-clean" {
		t.Errorf("render variant probed = %q, want the resolved default viral-60-clean", sawVariant)
	}
	if got.Step != string(tuiclient.StepRender) {
		t.Errorf("Step = %q, want %q", got.Step, tuiclient.StepRender)
	}
}

// TestComputeNextStepJobErrorClassified surfaces a GetJob failure as a
// classified toolError rather than a bare next-step result.
func TestComputeNextStepJobErrorClassified(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no such job", http.StatusNotFound)
	})
	d := depsFor(t, newStubServer(t, mux))

	_, te := computeNextStep(context.Background(), d, "missing", "viral-60-clean")
	if te == nil {
		t.Fatal("expected a toolError for a missing job")
	}
	if te.Code != codeNotFound {
		t.Errorf("Code = %q, want %q", te.Code, codeNotFound)
	}
}
