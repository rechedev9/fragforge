package tuiclient

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// CreateJobResponse is the body of POST /api/jobs and /parse.
type CreateJobResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// EnqueueResponse is the body of the record/compose/render enqueue endpoints.
type EnqueueResponse struct {
	ID        string `json:"id"`
	Task      string `json:"task,omitempty"`
	Status    string `json:"status,omitempty"`
	Variant   string `json:"variant,omitempty"`
	Duplicate bool   `json:"duplicate,omitempty"`
}

// ListJobs returns the most recent jobs (kill_plan omitted), newest first.
func (c *Client) ListJobs(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 50
	}
	var out struct {
		Jobs []Job `json:"jobs"`
	}
	if err := c.getJSON(ctx, "/api/jobs"+query(map[string]string{"limit": strconv.Itoa(limit)}), &out); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

// GetJob returns one job, including its kill plan when parsed.
func (c *Client) GetJob(ctx context.Context, id string) (Job, error) {
	var job Job
	err := c.getJSON(ctx, "/api/jobs/"+id, &job)
	return job, err
}

// CreateJob uploads a .dem file. When targetSteamID is empty the orchestrator
// runs the roster scan flow (job ends in "scanned", awaiting a target pick);
// when set, it parses straight to a kill plan.
func (c *Client) CreateJob(ctx context.Context, demoPath, targetSteamID string) (CreateJobResponse, error) {
	config := configJSON(map[string]string{"target_steamid": targetSteamID})
	var out CreateJobResponse
	err := c.uploadMultipart(ctx, "/api/jobs", "demo", demoPath, config, &out)
	return out, err
}

// StartParse assigns a target player to a scanned (or re-parses a parsed) job
// and enqueues parsing.
func (c *Client) StartParse(ctx context.Context, id, targetSteamID string) (CreateJobResponse, error) {
	body := map[string]string{"target_steamid": targetSteamID}
	var out CreateJobResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/jobs/"+id+"/parse", body, &out)
	return out, err
}

// GetPlan returns the kill plan, or a 409 APIError (IsNotReady) if not parsed.
func (c *Client) GetPlan(ctx context.Context, id string) (Plan, error) {
	var plan Plan
	err := c.getJSON(ctx, "/api/jobs/"+id+"/plan", &plan)
	return plan, err
}

// GetRoster returns the roster scan, or a 409 APIError (IsNotReady) if the scan
// has not finished.
func (c *Client) GetRoster(ctx context.Context, id string) (RosterResult, error) {
	var roster RosterResult
	err := c.getJSON(ctx, "/api/jobs/"+id+"/roster", &roster)
	return roster, err
}

// GetMoments returns the scored moments, or a 409 APIError if not ready.
func (c *Client) GetMoments(ctx context.Context, id string) (MomentsDocument, error) {
	var doc MomentsDocument
	err := c.getJSON(ctx, "/api/jobs/"+id+"/moments", &doc)
	return doc, err
}

// StartRecording enqueues HLAE/CS2 recording. An empty preset uses the default;
// empty segmentIDs records every segment in the plan.
func (c *Client) StartRecording(ctx context.Context, id, preset string, segmentIDs []string) (EnqueueResponse, error) {
	body := map[string]any{}
	if preset != "" {
		body["preset"] = preset
	}
	if len(segmentIDs) > 0 {
		body["segment_ids"] = segmentIDs
	}
	var out EnqueueResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/jobs/"+id+"/record", body, &out)
	return out, err
}

// StartCompose enqueues the final concat/composition of the recorded segments.
func (c *Client) StartCompose(ctx context.Context, id string) (EnqueueResponse, error) {
	var out EnqueueResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/jobs/"+id+"/compose", nil, &out)
	return out, err
}

// StartRenderVariant enqueues rendering of a named preset variant into a
// publish-ready Short.
func (c *Client) StartRenderVariant(ctx context.Context, id, variant string) (EnqueueResponse, error) {
	var out EnqueueResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/jobs/"+id+"/renders/"+variant, map[string]any{}, &out)
	return out, err
}

// GetRenderVariant returns the state of one render variant, or a 404 APIError
// (IsNotFound) when it has never been requested.
func (c *Client) GetRenderVariant(ctx context.Context, id, variant string) (RenderVariantState, error) {
	var state RenderVariantState
	err := c.getJSON(ctx, "/api/jobs/"+id+"/renders/"+variant, &state)
	return state, err
}

// GetRenderPublishBoard returns the publish board for a render variant: the
// upload-readiness of its artifacts and whether it has been marked uploaded.
func (c *Client) GetRenderPublishBoard(ctx context.Context, id, variant string) (PublishBoard, error) {
	var board PublishBoard
	err := c.getJSON(ctx, "/api/jobs/"+id+"/renders/"+variant+"/publish", &board)
	return board, err
}

// SetRenderUploaded records that a render variant has (or has not) been uploaded
// to the operator's channels.
func (c *Client) SetRenderUploaded(ctx context.Context, id, variant string, uploaded bool) error {
	body := map[string]bool{"uploaded": uploaded}
	return c.doJSON(ctx, http.MethodPost, "/api/jobs/"+id+"/renders/"+variant+"/publish/uploaded", body, nil)
}

// DownloadFinal streams the composed MP4 to dst. Returns a 409 APIError when the
// job has not been composed yet.
func (c *Client) DownloadFinal(ctx context.Context, id string, dst io.Writer) error {
	_, err := c.stream(ctx, "/api/jobs/"+id+"/final", dst)
	return err
}

// Capabilities reports which media stages (record/render/compose/stream) are
// configured on the orchestrator host.
func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	var caps Capabilities
	err := c.getJSON(ctx, "/api/capabilities", &caps)
	return caps, err
}

// Presets returns the render preset registry.
func (c *Client) Presets(ctx context.Context) (PresetList, error) {
	var list PresetList
	err := c.getJSON(ctx, "/api/presets", &list)
	return list, err
}

// uploadMultipart posts a multipart form with a single file part plus a
// "config" JSON field, the shape both POST /api/jobs and POST /api/stream-jobs
// accept, decoding the JSON response into out.
func (c *Client) uploadMultipart(ctx context.Context, path, fileField, filePath, config string, out any) error {
	f, err := os.Open(filePath) // #nosec G304 -- operator-chosen local upload path
	if err != nil {
		return fmt.Errorf("open %s: %w", filePath, err)
	}
	defer f.Close()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)

	// Build the request before starting the writer goroutine: if the request
	// fails to build, close the pipe and return without a goroutine that would
	// otherwise block forever writing to a body nobody reads.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, pr)
	if err != nil {
		_ = pw.Close()
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	go func() {
		var werr error
		defer func() { _ = pw.CloseWithError(werr) }()
		if config != "" {
			if werr = mw.WriteField("config", config); werr != nil {
				return
			}
		}
		part, err := mw.CreateFormFile(fileField, filepath.Base(filePath))
		if err != nil {
			werr = err
			return
		}
		if _, werr = io.Copy(part, f); werr != nil {
			return
		}
		werr = mw.Close()
	}()

	return c.send(req, http.MethodPost, path, out)
}
