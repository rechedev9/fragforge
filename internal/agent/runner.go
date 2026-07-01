package agent

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/workers"
)

type claimResponse struct {
	Job struct {
		ID string `json:"id"`
	} `json:"job"`
	JobType string `json:"jobType"`
}

// processFunc runs one claimed job. Swapped in tests.
type processFunc func(ctx context.Context, jobType string, demoID uuid.UUID) error

// Run starts the heartbeat and the claim loop until ctx is cancelled.
func Run(ctx context.Context, c *Client) error {
	repo := NewCloudJobRepo(c)
	var store storage.Storage = NewCloudStorage(c)
	pw := workers.NewParserWorker(repo, store)

	proc := func(ctx context.Context, jobType string, demoID uuid.UUID) error {
		switch jobType {
		case "scan":
			return pw.ProcessScanRoster(ctx, demoID)
		case "parse":
			return pw.ProcessParseDemo(ctx, demoID)
		default:
			return nil
		}
	}
	go HeartbeatLoop(ctx, c, map[string]any{"parser": true}, 20*time.Second)
	return loop(ctx, c, proc, 2*time.Second)
}

func loop(ctx context.Context, c *Client, proc processFunc, idle time.Duration) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var out claimResponse
		code, err := c.Do(ctx, "POST", "/api/agent/jobs/claim", nil, &out)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idle):
				continue
			}
		}
		if code == 204 || out.Job.ID == "" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(idle):
				continue
			}
		}
		demoID, perr := uuid.Parse(out.Job.ID)
		if perr != nil {
			continue
		}
		if err := proc(ctx, out.JobType, demoID); err != nil {
			_, _ = c.Do(ctx, "POST", "/api/agent/jobs/"+demoID.String()+"/fail", map[string]string{"error": err.Error()}, nil)
			continue
		}
		_, _ = c.Do(ctx, "POST", "/api/agent/jobs/"+demoID.String()+"/complete", nil, nil)
	}
}
