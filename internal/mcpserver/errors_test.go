package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// TestClassifyNotReadyEmbedsNextStep verifies the richest error path: a 409
// stage precondition maps to not_ready and, given a job id, embeds the current
// job status plus the reconciler's next_step so the agent self-corrects without
// another round-trip.
func TestClassifyNotReadyEmbedsNextStep(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.Job{ID: r.PathValue("id"), Status: tuiclient.StatusParsing})
	})
	mux.HandleFunc("GET /api/presets", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, tuiclient.PresetList{Default: "viral-60-clean"})
	})
	mux.HandleFunc("GET /api/jobs/{id}/renders/{variant}", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	d := depsFor(t, newStubServer(t, mux))

	err := &tuiclient.APIError{StatusCode: http.StatusConflict, Message: "kill plan not ready", Method: "GET", Path: "/plan"}
	te := classify(context.Background(), d, err, "j1")

	if te.Code != codeNotReady {
		t.Fatalf("Code = %q, want %q", te.Code, codeNotReady)
	}
	if te.StatusCode != http.StatusConflict {
		t.Errorf("StatusCode = %d, want 409", te.StatusCode)
	}
	if !te.Retryable {
		t.Error("not_ready should be retryable")
	}
	if te.JobStatus != tuiclient.StatusParsing {
		t.Errorf("JobStatus = %q, want %q", te.JobStatus, tuiclient.StatusParsing)
	}
	if te.NextStep == nil {
		t.Fatal("NextStep not embedded")
	}
	if te.NextStep.Step != string(tuiclient.StepParsing) {
		t.Errorf("NextStep.Step = %q, want %q", te.NextStep.Step, tuiclient.StepParsing)
	}
	if te.PollHint != pollHintParse {
		t.Errorf("PollHint = %d, want %d", te.PollHint, pollHintParse)
	}
}

// TestClassifyNotReadyWithoutJobIDSkipsEmbedding confirms the embedding is
// best-effort: with no job id, not_ready is still returned but without a
// next_step (the reconciler needs a job to derive one).
func TestClassifyNotReadyWithoutJobID(t *testing.T) {
	err := &tuiclient.APIError{StatusCode: http.StatusConflict, Message: "not ready"}
	te := classify(context.Background(), unavailableDeps(), err, "")
	if te.Code != codeNotReady {
		t.Fatalf("Code = %q, want %q", te.Code, codeNotReady)
	}
	if te.NextStep != nil {
		t.Errorf("NextStep = %+v, want nil without a job id", te.NextStep)
	}
}

// TestClassifyByStatus tables the remaining status-driven mappings.
func TestClassifyByStatus(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantCode      string
		wantStatus    int
		wantRetryable bool
	}{
		{"404 not found", &tuiclient.APIError{StatusCode: http.StatusNotFound, Message: "gone"}, codeNotFound, http.StatusNotFound, false},
		{"400 client error", &tuiclient.APIError{StatusCode: http.StatusBadRequest, Message: "bad"}, codeAPIError, http.StatusBadRequest, false},
		{"500 server error retryable", &tuiclient.APIError{StatusCode: http.StatusInternalServerError, Message: "boom"}, codeAPIError, http.StatusInternalServerError, true},
		{"503 server error retryable", &tuiclient.APIError{StatusCode: http.StatusServiceUnavailable, Message: "later"}, codeAPIError, http.StatusServiceUnavailable, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			te := classify(context.Background(), unavailableDeps(), tt.err, "")
			if te.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", te.Code, tt.wantCode)
			}
			if te.StatusCode != tt.wantStatus {
				t.Errorf("StatusCode = %d, want %d", te.StatusCode, tt.wantStatus)
			}
			if te.Retryable != tt.wantRetryable {
				t.Errorf("Retryable = %v, want %v", te.Retryable, tt.wantRetryable)
			}
		})
	}
}

// TestClassifyTransportFailureIsUnavailable confirms a non-APIError (a transport
// failure, which on loopback means the orchestrator went away) maps to
// orchestrator_unavailable, retryable, naming the base URL.
func TestClassifyTransportFailureIsUnavailable(t *testing.T) {
	d := unavailableDeps()
	err := errors.New("dial tcp 127.0.0.1:1: connect: connection refused")
	te := classify(context.Background(), d, err, "")
	if te.Code != codeOrchestratorUnavailable {
		t.Fatalf("Code = %q, want %q", te.Code, codeOrchestratorUnavailable)
	}
	if !te.Retryable {
		t.Error("orchestrator_unavailable should be retryable")
	}
	if !strings.Contains(te.Message, d.client.BaseURL()) {
		t.Errorf("Message %q should name the base URL %q", te.Message, d.client.BaseURL())
	}
}

// TestToolErrorHelpers checks the standalone constructors used off the classify
// path, including that Error() emits the JSON body agents read.
func TestToolErrorHelpers(t *testing.T) {
	t.Run("capability missing is not retryable and names the stage", func(t *testing.T) {
		te := capabilityMissingError("record")
		if te.Code != codeCapabilityMissing || te.Retryable {
			t.Fatalf("got %+v, want capability_missing, retryable=false", te)
		}
		if !strings.Contains(te.Message, "record") {
			t.Errorf("message %q should name the record stage", te.Message)
		}
	})
	t.Run("local not found is a 404 not_found", func(t *testing.T) {
		te := localNotFoundError("demo missing")
		if te.Code != codeNotFound || te.StatusCode != http.StatusNotFound || te.Retryable {
			t.Fatalf("got %+v, want not_found 404 non-retryable", te)
		}
	})
	t.Run("Error emits the structured JSON body", func(t *testing.T) {
		te := unavailableError("http://127.0.0.1:8080")
		var round toolError
		if err := json.Unmarshal([]byte(te.Error()), &round); err != nil {
			t.Fatalf("Error() is not valid JSON: %v", err)
		}
		if round.Code != codeOrchestratorUnavailable {
			t.Errorf("round-tripped Code = %q, want %q", round.Code, codeOrchestratorUnavailable)
		}
	})
}
