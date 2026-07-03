package tuiclient

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
)

// StreamCreateResponse is the body of POST /api/stream-jobs.
type StreamCreateResponse struct {
	ID     string      `json:"id"`
	Status string      `json:"status"`
	Probe  SourceProbe `json:"probe"`
}

// ListStreamJobs returns the most recent stream-clip jobs, newest first.
func (c *Client) ListStreamJobs(ctx context.Context, limit int) ([]StreamJob, error) {
	if limit <= 0 {
		limit = 50
	}
	var out struct {
		Jobs []StreamJob `json:"jobs"`
	}
	if err := c.getJSON(ctx, "/api/stream-jobs"+query(map[string]string{"limit": strconv.Itoa(limit)}), &out); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

// GetStreamJob returns one stream-clip job.
func (c *Client) GetStreamJob(ctx context.Context, id string) (StreamJob, error) {
	var job StreamJob
	err := c.getJSON(ctx, "/api/stream-jobs/"+id, &job)
	return job, err
}

// CreateStreamJobUpload uploads a streamer MP4 file. The job starts in
// "uploaded" and becomes "ready" once probed.
func (c *Client) CreateStreamJobUpload(ctx context.Context, videoPath, title string) (StreamCreateResponse, error) {
	config := "{}"
	if title != "" {
		config = fmt.Sprintf(`{"title":%q}`, title)
	}
	var out StreamCreateResponse
	err := c.uploadMultipart(ctx, "/api/stream-jobs", "video", videoPath, config, &out)
	return out, err
}

// CreateStreamJobFromURL creates a stream-clip job that downloads its source
// from a URL (requires yt-dlp on the orchestrator host). The job starts in
// "acquiring".
func (c *Client) CreateStreamJobFromURL(ctx context.Context, sourceURL, title string) (StreamCreateResponse, error) {
	body := map[string]string{"source_url": sourceURL}
	if title != "" {
		body["title"] = title
	}
	var out StreamCreateResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/stream-jobs", body, &out)
	return out, err
}

// GetStreamEditPlan returns the job's edit plan (or the default plan when none
// has been saved yet).
func (c *Client) GetStreamEditPlan(ctx context.Context, id string) (StreamEditPlan, error) {
	var plan StreamEditPlan
	err := c.getJSON(ctx, "/api/stream-jobs/"+id+"/edit-plan", &plan)
	return plan, err
}

// PutStreamEditPlan saves an edit plan and returns the normalized result.
func (c *Client) PutStreamEditPlan(ctx context.Context, id string, plan StreamEditPlan) (StreamEditPlan, error) {
	var out StreamEditPlan
	err := c.doJSON(ctx, http.MethodPut, "/api/stream-jobs/"+id+"/edit-plan", plan, &out)
	return out, err
}

// StartStreamRender enqueues a stream render. Valid only when the job is "ready"
// or "rendered"; the default variant is StreamDefaultVariant.
func (c *Client) StartStreamRender(ctx context.Context, id, variant string) (EnqueueResponse, error) {
	if variant == "" {
		variant = StreamDefaultVariant
	}
	var out EnqueueResponse
	err := c.doJSON(ctx, http.MethodPost, "/api/stream-jobs/"+id+"/renders/"+variant, nil, &out)
	return out, err
}

// GetStreamRender returns the render state of a stream job variant.
func (c *Client) GetStreamRender(ctx context.Context, id, variant string) (StreamRenderState, error) {
	if variant == "" {
		variant = StreamDefaultVariant
	}
	var state StreamRenderState
	err := c.getJSON(ctx, "/api/stream-jobs/"+id+"/renders/"+variant, &state)
	return state, err
}
