package tuiclient

// Step is the primary next action available on a demo->reel job, derived from
// the job status and (optionally) the default render variant's state. It mirrors
// web/lib/api/reel-reconcile.ts deriveReelView, extended to cover the scan /
// target-pick and record / compose stages the TUI also drives. The derivation is
// idempotent: it reads server truth and names one action, so re-deriving after a
// reload never double-drives the pipeline.
type Step string

const (
	StepScanning   Step = "scanning"    // queued/scanning: roster scan running
	StepPickTarget Step = "pick-target" // scanned: choose the player to feature
	StepParsing    Step = "parsing"     // parse running
	StepRecord     Step = "record"      // parsed: record segments (HLAE/CS2)
	StepRecording  Step = "recording"   // recording running
	StepRender     Step = "render"      // recorded/composed/done: render a Short
	StepRendering  Step = "rendering"   // a render variant is queued/rendering
	StepComposing  Step = "composing"   // final composition running
	StepReady      Step = "ready"       // a render variant is ready
	StepRetry      Step = "retry"       // failed: retry recording
	StepWait       Step = "wait"        // transient/unknown: keep polling
)

// NextStep derives the primary next action for a demo->reel job. render may be
// nil when no variant has been requested. A ready or in-flight render drives the
// view (matching the web); a failed render falls through to the job status so
// the operator can retry.
func NextStep(jobStatus string, render *RenderVariantState) Step {
	// A finished render is terminal, even if the job later flags an error.
	if render != nil && render.Status == RenderReady {
		return StepReady
	}
	// A job-level failure takes precedence over a stale in-flight render, so a
	// job that fails while a render state is still queued/rendering surfaces the
	// failure instead of hanging on "rendering" (matches deriveReelView order).
	if jobStatus == StatusFailed {
		return StepRetry
	}
	if render != nil && (render.Status == RenderQueued || render.Status == RenderRendering) {
		return StepRendering
	}
	switch jobStatus {
	case StatusQueued, StatusScanning:
		return StepScanning
	case StatusScanned:
		return StepPickTarget
	case StatusParsing:
		return StepParsing
	case StatusParsed:
		return StepRecord
	case StatusRecording:
		return StepRecording
	case StatusComposing:
		return StepComposing
	case StatusRecorded, StatusComposed, StatusDone:
		return StepRender
	default:
		return StepWait
	}
}

// Actionable reports whether a step is one the operator triggers (as opposed to
// a server stage the TUI only waits on).
func (s Step) Actionable() bool {
	switch s {
	case StepPickTarget, StepRecord, StepRender, StepRetry:
		return true
	default:
		return false
	}
}

// Label is a short human description of a step, for the status bar.
func (s Step) Label() string {
	switch s {
	case StepScanning:
		return "scanning demo roster"
	case StepPickTarget:
		return "pick a player to feature"
	case StepParsing:
		return "parsing kill plan"
	case StepRecord:
		return "ready to record"
	case StepRecording:
		return "recording gameplay"
	case StepRender:
		return "ready to render a Short"
	case StepRendering:
		return "rendering Short"
	case StepComposing:
		return "composing final video"
	case StepReady:
		return "reel ready"
	case StepRetry:
		return "failed - retry recording"
	default:
		return "working"
	}
}

// StreamStep is the primary next action on a stream-clip job.
type StreamStep string

const (
	StreamStepAcquiring StreamStep = "acquiring" // downloading the source
	StreamStepUploading StreamStep = "uploading" // probing an upload
	StreamStepEdit      StreamStep = "edit"      // ready: adjust plan / render
	StreamStepRendering StreamStep = "rendering" // render running
	StreamStepReady     StreamStep = "ready"     // rendered clips available
	StreamStepRetry     StreamStep = "retry"     // failed
	StreamStepWait      StreamStep = "wait"
)

// NextStreamStep derives the primary next action for a stream-clip job.
func NextStreamStep(status string) StreamStep {
	switch status {
	case StreamAcquiring:
		return StreamStepAcquiring
	case StreamUploaded:
		return StreamStepUploading
	case StreamReady:
		return StreamStepEdit
	case StreamRendering:
		return StreamStepRendering
	case StreamRendered:
		return StreamStepReady
	case StreamFailed:
		return StreamStepRetry
	default:
		return StreamStepWait
	}
}
