package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
)

// CloudJobRepo implements workers.JobRepository against the cloud control-plane.
type CloudJobRepo struct {
	c *Client
}

func NewCloudJobRepo(c *Client) *CloudJobRepo { return &CloudJobRepo{c: c} }

func (r *CloudJobRepo) GetMeta(ctx context.Context, id uuid.UUID) (job.Job, error) {
	var j job.Job
	if _, err := r.c.Do(ctx, "GET", "/api/agent/jobs/"+id.String(), nil, &j); err != nil {
		return job.Job{}, err
	}
	return j, nil
}

func (r *CloudJobRepo) UpdateStatus(ctx context.Context, id uuid.UUID, s job.Status, failureReason string) error {
	body := map[string]any{"status": int(s), "failure_reason": failureReason}
	_, err := r.c.Do(ctx, "POST", "/api/agent/jobs/"+id.String()+"/status", body, nil)
	return err
}

func (r *CloudJobRepo) SetKillPlan(ctx context.Context, id uuid.UUID, plan killplan.Plan) error {
	_, err := r.c.Do(ctx, "POST", "/api/agent/jobs/"+id.String()+"/killplan", map[string]any{"kill_plan": plan}, nil)
	return err
}
