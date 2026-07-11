package mcpserver

import (
	"context"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// NextStepResult is the agent-facing projection of the TUI reconciler
// (internal/tuiclient/reconcile.go). It names the single recommended next action
// for a job plus the tool an agent should call to take it, so an agent can loop
// next_step -> act -> poll without re-deriving the job state machine.
type NextStepResult struct {
	Step              string `json:"step"`
	Actionable        bool   `json:"actionable"`
	Label             string `json:"label"`
	SuggestedTool     string `json:"suggested_tool"`
	SuggestedArgsHint string `json:"suggested_args_hint"`
	PollHintSeconds   int    `json:"poll_hint_seconds,omitempty"`
}

// computeNextStep fetches the job plus one render variant and runs the reconciler
// to name the next action. An empty variant resolves to the default preset,
// because the reconciler needs a specific variant's state and a job has no
// intrinsic "current" variant. It returns a *toolError (nil on success) so the
// caller can surface classified failures unchanged.
func computeNextStep(ctx context.Context, d deps, jobID, variant string) (NextStepResult, *toolError) {
	job, err := d.client.GetJob(ctx, jobID)
	if err != nil {
		te := classify(ctx, d, err, "")
		return NextStepResult{}, &te
	}

	if variant == "" {
		if presets, perr := d.client.Presets(ctx); perr == nil {
			variant = presets.Default
		}
	}

	var render *tuiclient.RenderVariantState
	if variant != "" {
		state, rerr := d.client.GetRenderVariant(ctx, jobID, variant)
		switch {
		case rerr == nil:
			render = &state
		case tuiclient.IsNotFound(rerr):
			// Never requested for this job: the reconciler treats a nil render as
			// "no variant in flight".
			render = nil
		default:
			te := classify(ctx, d, rerr, "")
			return NextStepResult{}, &te
		}
	}

	return newNextStepResult(tuiclient.NextStep(job.Status, render)), nil
}

// newNextStepResult maps a reconciler Step onto the agent-facing suggestion. The
// reconciler has no notion of a suggested tool, so that mapping is synthesized
// here (stream steps are out of scope per the spec and never reach this).
func newNextStepResult(step tuiclient.Step) NextStepResult {
	r := NextStepResult{
		Step:       string(step),
		Actionable: step.Actionable(),
		Label:      step.Label(),
	}
	switch step {
	case tuiclient.StepScanning:
		r.SuggestedTool = "get_job"
		r.SuggestedArgsHint = "poll get_job until status is scanned"
		r.PollHintSeconds = pollHintParse
	case tuiclient.StepPickTarget:
		r.SuggestedTool = "get_roster"
		r.SuggestedArgsHint = "inspect the roster, then start_parse with target_steamid"
	case tuiclient.StepParsing:
		r.SuggestedTool = "get_job"
		r.SuggestedArgsHint = "poll get_job until status is parsed"
		r.PollHintSeconds = pollHintParse
	case tuiclient.StepRecord:
		r.SuggestedTool = "start_recording"
		r.SuggestedArgsHint = "check get_capabilities, then start_recording (optional preset, segment_ids)"
	case tuiclient.StepRecording:
		r.SuggestedTool = "get_job"
		r.SuggestedArgsHint = "poll get_job until status is recorded"
		r.PollHintSeconds = pollHintMedia
	case tuiclient.StepRender:
		r.SuggestedTool = "start_render"
		r.SuggestedArgsHint = "pick a variant from list_presets, then start_render"
	case tuiclient.StepRendering:
		r.SuggestedTool = "get_render"
		r.SuggestedArgsHint = "poll get_render for the variant until status is ready"
		r.PollHintSeconds = pollHintMedia
	case tuiclient.StepComposing:
		r.SuggestedTool = "get_job"
		r.SuggestedArgsHint = "poll get_job until status is composed"
		r.PollHintSeconds = pollHintMedia
	case tuiclient.StepReady:
		r.SuggestedTool = "download_final"
		r.SuggestedArgsHint = "download_final with a local out_path"
	case tuiclient.StepRetry:
		r.SuggestedTool = "get_job"
		r.SuggestedArgsHint = "job failed; read failure_reason, then re-run the failed stage (e.g. start_recording)"
	default:
		r.SuggestedTool = "get_job"
		r.SuggestedArgsHint = "poll get_job"
		r.PollHintSeconds = pollHintDefault
	}
	return r
}
