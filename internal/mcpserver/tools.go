package mcpserver

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rechedev9/fragforge/internal/tuiclient"
)

// ---- input schemas ---------------------------------------------------------
//
// One request struct per tool; the SDK infers the JSON input schema from the
// json/jsonschema tags. Empty-input tools use emptyInput (an empty object).

type emptyInput struct{}

type listJobsInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"max jobs to return; 0 means the server default (50)"`
}

type jobIDInput struct {
	JobID string `json:"job_id" jsonschema:"the job id"`
}

type nextStepInput struct {
	JobID   string `json:"job_id" jsonschema:"the job id"`
	Variant string `json:"variant,omitempty" jsonschema:"render variant to check; empty uses the default preset"`
}

type createJobInput struct {
	DemoPath      string `json:"demo_path" jsonschema:"local filesystem path to a .dem readable by this process"`
	TargetSteamID string `json:"target_steamid,omitempty" jsonschema:"SteamID64 to feature; empty starts the roster-scan flow"`
}

type startParseInput struct {
	JobID         string `json:"job_id" jsonschema:"the job id"`
	TargetSteamID string `json:"target_steamid" jsonschema:"SteamID64 of the player to feature"`
}

type startRecordingInput struct {
	JobID      string   `json:"job_id" jsonschema:"the job id"`
	Preset     string   `json:"preset,omitempty" jsonschema:"recording preset; empty uses the server default"`
	SegmentIDs []string `json:"segment_ids,omitempty" jsonschema:"specific plan segment ids; empty records all"`
}

type startRenderInput struct {
	JobID   string `json:"job_id" jsonschema:"the job id"`
	Variant string `json:"variant" jsonschema:"render variant name from list_presets"`
}

type downloadFinalInput struct {
	JobID   string `json:"job_id" jsonschema:"the job id"`
	OutPath string `json:"out_path" jsonschema:"local path to write the .mp4"`
}

// ---- output schemas --------------------------------------------------------
//
// Outputs reuse the tuiclient DTOs so the MCP surface never forks from the TUI.
// Slice- and pipeline-metadata-carrying outputs are wrapped in a struct (the SDK
// requires a struct/map output, and the poll_hint_seconds field pairs an enqueue
// snapshot with a pacing hint).

type listJobsOutput struct {
	Jobs []tuiclient.Job `json:"jobs"`
}

type createJobOutput struct {
	tuiclient.CreateJobResponse
	PollHintSeconds int `json:"poll_hint_seconds"`
}

type enqueueOutput struct {
	tuiclient.EnqueueResponse
	PollHintSeconds int `json:"poll_hint_seconds"`
}

type downloadFinalOutput struct {
	Path         string `json:"path"`
	BytesWritten int64  `json:"bytes_written"`
}

// registerTools installs every tool on srv. Each handler re-probes the
// orchestrator first, then calls one tuiclient method, then classifies any
// error into a structured toolError.
func registerTools(srv *mcp.Server, d deps) {
	registerReadTools(srv, d)
	registerMutateTools(srv, d)
}

func registerReadTools(srv *mcp.Server, d deps) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_capabilities",
		Description: "Reports which stages (record/render/compose/stream) this orchestrator has configured; call before offering record or render.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, tuiclient.Capabilities, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, tuiclient.Capabilities{}, *te
		}
		caps, err := d.client.Capabilities(ctx)
		if err != nil {
			return nil, tuiclient.Capabilities{}, classify(ctx, d, err, "")
		}
		return nil, caps, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_presets",
		Description: "Lists render variants and recording presets available on this orchestrator.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ emptyInput) (*mcp.CallToolResult, tuiclient.PresetList, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, tuiclient.PresetList{}, *te
		}
		list, err := d.client.Presets(ctx)
		if err != nil {
			return nil, tuiclient.PresetList{}, classify(ctx, d, err, "")
		}
		return nil, list, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_jobs",
		Description: "Returns a summary list of recent jobs, newest first.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listJobsInput) (*mcp.CallToolResult, listJobsOutput, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, listJobsOutput{}, *te
		}
		jobs, err := d.client.ListJobs(ctx, in.Limit)
		if err != nil {
			return nil, listJobsOutput{}, classify(ctx, d, err, "")
		}
		return nil, listJobsOutput{Jobs: jobs}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_job",
		Description: "Returns full status for one job by id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in jobIDInput) (*mcp.CallToolResult, tuiclient.Job, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, tuiclient.Job{}, *te
		}
		job, err := d.client.GetJob(ctx, in.JobID)
		if err != nil {
			return nil, tuiclient.Job{}, classify(ctx, d, err, in.JobID)
		}
		return nil, job, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_roster",
		Description: "Returns the demo player table (K/D/A, HS%, ADR, rating) for target selection; requires the roster scan to have finished (status scanned or later).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in jobIDInput) (*mcp.CallToolResult, tuiclient.RosterResult, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, tuiclient.RosterResult{}, *te
		}
		roster, err := d.client.GetRoster(ctx, in.JobID)
		if err != nil {
			return nil, tuiclient.RosterResult{}, classify(ctx, d, err, in.JobID)
		}
		return nil, roster, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_plan",
		Description: "Returns the kill plan; requires the job to be parsed (status parsed or later).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in jobIDInput) (*mcp.CallToolResult, tuiclient.Plan, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, tuiclient.Plan{}, *te
		}
		plan, err := d.client.GetPlan(ctx, in.JobID)
		if err != nil {
			return nil, tuiclient.Plan{}, classify(ctx, d, err, in.JobID)
		}
		return nil, plan, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_moments",
		Description: "Returns scored clip candidates; requires the plan to be ready.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in jobIDInput) (*mcp.CallToolResult, tuiclient.MomentsDocument, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, tuiclient.MomentsDocument{}, *te
		}
		doc, err := d.client.GetMoments(ctx, in.JobID)
		if err != nil {
			return nil, tuiclient.MomentsDocument{}, classify(ctx, d, err, in.JobID)
		}
		return nil, doc, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "next_step",
		Description: "Returns the single recommended next action for a job (step, whether it is actionable, the suggested tool and args), porting the TUI reconciler so agents loop next_step -> act -> poll.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in nextStepInput) (*mcp.CallToolResult, NextStepResult, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, NextStepResult{}, *te
		}
		ns, te := computeNextStep(ctx, d, in.JobID, in.Variant)
		if te != nil {
			return nil, NextStepResult{}, *te
		}
		return nil, ns, nil
	})
}

func registerMutateTools(srv *mcp.Server, d deps) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_job",
		Description: "Uploads a local .dem and creates a job; an empty target_steamid starts the roster-scan flow so you can pick a player with get_roster + start_parse.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in createJobInput) (*mcp.CallToolResult, createJobOutput, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, createJobOutput{}, *te
		}
		if _, err := os.Stat(in.DemoPath); err != nil {
			return nil, createJobOutput{}, localNotFoundError(fmt.Sprintf("demo file not found or unreadable at %s: %v", in.DemoPath, err))
		}
		resp, err := d.client.CreateJob(ctx, in.DemoPath, in.TargetSteamID)
		if err != nil {
			return nil, createJobOutput{}, classify(ctx, d, err, "")
		}
		return nil, createJobOutput{CreateJobResponse: resp, PollHintSeconds: pollHintParse}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "start_parse",
		Description: "Assigns a target player to a scanned job and starts parsing; requires status scanned.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in startParseInput) (*mcp.CallToolResult, createJobOutput, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, createJobOutput{}, *te
		}
		resp, err := d.client.StartParse(ctx, in.JobID, in.TargetSteamID)
		if err != nil {
			return nil, createJobOutput{}, classify(ctx, d, err, in.JobID)
		}
		return nil, createJobOutput{CreateJobResponse: resp, PollHintSeconds: pollHintParse}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "start_recording",
		Description: "Starts recording gameplay for a parsed job; requires status parsed and the record capability (checked client-side, fails fast as capability_missing); never auto-retried because it burns GPU minutes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in startRecordingInput) (*mcp.CallToolResult, enqueueOutput, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, enqueueOutput{}, *te
		}
		if te := d.requireCapability(ctx, capabilityRecord); te != nil {
			return nil, enqueueOutput{}, *te
		}
		resp, err := d.client.StartRecording(ctx, in.JobID, in.Preset, in.SegmentIDs)
		if err != nil {
			return nil, enqueueOutput{}, classify(ctx, d, err, in.JobID)
		}
		return nil, enqueueOutput{EnqueueResponse: resp, PollHintSeconds: pollHintMedia}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "start_compose",
		Description: "Composes the final video from recorded segments; requires status recorded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in jobIDInput) (*mcp.CallToolResult, enqueueOutput, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, enqueueOutput{}, *te
		}
		resp, err := d.client.StartCompose(ctx, in.JobID)
		if err != nil {
			return nil, enqueueOutput{}, classify(ctx, d, err, in.JobID)
		}
		return nil, enqueueOutput{EnqueueResponse: resp, PollHintSeconds: pollHintMedia}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "start_render",
		Description: "Starts rendering a Short variant; requires the composed job and the render capability (checked client-side, capability_missing on miss).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in startRenderInput) (*mcp.CallToolResult, enqueueOutput, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, enqueueOutput{}, *te
		}
		if te := d.requireCapability(ctx, capabilityRender); te != nil {
			return nil, enqueueOutput{}, *te
		}
		resp, err := d.client.StartRenderVariant(ctx, in.JobID, in.Variant)
		if err != nil {
			return nil, enqueueOutput{}, classify(ctx, d, err, in.JobID)
		}
		return nil, enqueueOutput{EnqueueResponse: resp, PollHintSeconds: pollHintMedia}, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_render",
		Description: "Returns the status of one render variant; not_found until start_render has been called for that variant.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in startRenderInput) (*mcp.CallToolResult, tuiclient.RenderVariantState, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, tuiclient.RenderVariantState{}, *te
		}
		state, err := d.client.GetRenderVariant(ctx, in.JobID, in.Variant)
		if err != nil {
			return nil, tuiclient.RenderVariantState{}, classify(ctx, d, err, in.JobID)
		}
		return nil, state, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "download_final",
		Description: "Downloads the composed MP4 to a local out_path and returns the path (never the bytes); requires status composed or done.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in downloadFinalInput) (*mcp.CallToolResult, downloadFinalOutput, error) {
		if te := d.requireOrchestrator(ctx); te != nil {
			return nil, downloadFinalOutput{}, *te
		}
		return downloadFinal(ctx, d, in)
	})
}

// downloadFinal owns the output file: it creates it, streams the MP4 through a
// counting writer, and reports the path plus bytes written. A partial file from
// a mid-stream failure is removed so a retry starts clean.
func downloadFinal(ctx context.Context, d deps, in downloadFinalInput) (*mcp.CallToolResult, downloadFinalOutput, error) {
	f, err := os.Create(in.OutPath) // #nosec G304 -- operator-chosen output path
	if err != nil {
		return nil, downloadFinalOutput{}, localNotFoundError(fmt.Sprintf("cannot create output file %s: %v", in.OutPath, err))
	}
	counter := &countingWriter{w: f}
	streamErr := d.client.DownloadFinal(ctx, in.JobID, counter)
	closeErr := f.Close()
	if streamErr != nil {
		_ = os.Remove(in.OutPath)
		return nil, downloadFinalOutput{}, classify(ctx, d, streamErr, in.JobID)
	}
	if closeErr != nil {
		return nil, downloadFinalOutput{}, localNotFoundError(fmt.Sprintf("cannot finalize output file %s: %v", in.OutPath, closeErr))
	}
	return nil, downloadFinalOutput{Path: in.OutPath, BytesWritten: counter.n}, nil
}

// countingWriter counts bytes copied to the underlying writer, so download_final
// can report bytes_written without buffering the MP4 in memory.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
