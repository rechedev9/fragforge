package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// Tool error codes. Every failed tool call returns one of these as a structured
// body so an agent can branch on it (see the zv mcp spec error model).
const (
	codeOrchestratorUnavailable = "orchestrator_unavailable"
	codeNotReady                = "not_ready"
	codeNotFound                = "not_found"
	codeCapabilityMissing       = "capability_missing"
	codeAPIError                = "api_error"
)

// toolError is the structured body of a failed tool call. It implements error so
// a handler can return it directly: the MCP SDK packs a returned error into an
// isError result with Error() as the text content, which is exactly the
// machine-readable JSON body we want the model to see (the SDK skips output
// marshaling on the error path, so no stray structured content is attached).
type toolError struct {
	StatusCode int             `json:"status_code"`
	Code       string          `json:"code"`
	Message    string          `json:"message"`
	Retryable  bool            `json:"retryable"`
	JobStatus  string          `json:"job_status,omitempty"`
	NextStep   *NextStepResult `json:"next_step,omitempty"`
	PollHint   int             `json:"poll_hint_seconds,omitempty"`
}

func (te toolError) Error() string {
	b, err := json.Marshal(te)
	if err != nil {
		return te.Message
	}
	return string(b)
}

// unavailableError is returned when the orchestrator is not reachable, either at
// the pre-flight healthz probe or as a transport failure mid-call. It is
// retryable because MCP clients launch the server eagerly, before the desktop
// app is necessarily up.
func unavailableError(url string) toolError {
	return toolError{
		StatusCode: http.StatusServiceUnavailable,
		Code:       codeOrchestratorUnavailable,
		Message:    fmt.Sprintf("FragForge Studio (or `zv serve`) is not reachable at %s; launch it and retry", url),
		Retryable:  true,
	}
}

// capabilityMissingError is returned before any API call when a stage
// (record/render) is not configured on the orchestrator host. It is not
// retryable: the fix is a capture-configured Windows+GPU machine, not time.
func capabilityMissingError(stage string) toolError {
	return toolError{
		Code:      codeCapabilityMissing,
		Message:   fmt.Sprintf("the %s stage is not configured on this orchestrator host; it needs a capture-capable Windows+GPU machine (check get_capabilities)", stage),
		Retryable: false,
	}
}

// localNotFoundError is returned when an agent-supplied local filesystem path is
// missing or unusable (a demo to upload, an output path to write). It is not an
// orchestrator condition, so it is reported distinctly from a transport failure.
func localNotFoundError(msg string) toolError {
	return toolError{
		StatusCode: http.StatusNotFound,
		Code:       codeNotFound,
		Message:    msg,
		Retryable:  false,
	}
}

// classify maps a tuiclient error onto a toolError. When jobID is non-empty and
// the error is a 409 stage precondition, it embeds the current job status and
// the next_step result so the agent self-corrects without another round-trip.
func classify(ctx context.Context, d deps, err error, jobID string) toolError {
	switch {
	case tuiclient.IsNotReady(err):
		te := toolError{
			StatusCode: http.StatusConflict,
			Code:       codeNotReady,
			Message:    err.Error(),
			Retryable:  true,
		}
		if jobID != "" {
			if job, jerr := d.client.GetJob(ctx, jobID); jerr == nil {
				te.JobStatus = job.Status
			}
			// computeNextStep guards against re-entering classify with a jobID
			// (it classifies its own GET errors with an empty id), so embedding
			// next_step for this job here cannot recurse.
			if ns, nerr := computeNextStep(ctx, d, jobID, ""); nerr == nil {
				te.NextStep = &ns
				te.PollHint = ns.PollHintSeconds
			}
		}
		return te
	case tuiclient.IsNotFound(err):
		return toolError{
			StatusCode: http.StatusNotFound,
			Code:       codeNotFound,
			Message:    err.Error(),
			Retryable:  false,
		}
	default:
		// A real API response carries a status code; anything else is a transport
		// failure, which on loopback means the orchestrator went away.
		if code := tuiclient.StatusCode(err); code != 0 {
			return toolError{
				StatusCode: code,
				Code:       codeAPIError,
				Message:    err.Error(),
				Retryable:  code >= 500,
			}
		}
		return unavailableError(d.client.BaseURL())
	}
}
