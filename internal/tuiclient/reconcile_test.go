package tuiclient

import "testing"

func TestNextStepFromJobStatus(t *testing.T) {
	cases := map[string]Step{
		StatusQueued:    StepScanning,
		StatusScanning:  StepScanning,
		StatusScanned:   StepPickTarget,
		StatusParsing:   StepParsing,
		StatusParsed:    StepRecord,
		StatusRecording: StepRecording,
		StatusRecorded:  StepRender,
		StatusComposing: StepComposing,
		StatusComposed:  StepRender,
		StatusDone:      StepRender,
		StatusFailed:    StepRetry,
		"bogus":         StepWait,
	}
	for status, want := range cases {
		if got := NextStep(status, nil); got != want {
			t.Errorf("NextStep(%q, nil) = %q, want %q", status, got, want)
		}
	}
}

func TestNextStepRenderDrivesView(t *testing.T) {
	// A ready render is terminal regardless of the (composed) job status.
	if got := NextStep(StatusComposed, &RenderVariantState{Status: RenderReady}); got != StepReady {
		t.Errorf("ready render: got %q, want %q", got, StepReady)
	}
	// An in-flight render shows rendering.
	for _, rs := range []string{RenderQueued, RenderRendering} {
		if got := NextStep(StatusRecorded, &RenderVariantState{Status: rs}); got != StepRendering {
			t.Errorf("render %q: got %q, want %q", rs, got, StepRendering)
		}
	}
	// A failed render falls through to the job status so the operator can retry
	// the underlying stage.
	if got := NextStep(StatusRecorded, &RenderVariantState{Status: RenderFailed}); got != StepRender {
		t.Errorf("failed render on recorded job: got %q, want %q", got, StepRender)
	}
}

func TestStepActionable(t *testing.T) {
	actionable := map[Step]bool{
		StepPickTarget: true, StepRecord: true, StepRender: true, StepRetry: true,
		StepScanning: false, StepParsing: false, StepRecording: false,
		StepComposing: false, StepRendering: false, StepReady: false, StepWait: false,
	}
	for step, want := range actionable {
		if got := step.Actionable(); got != want {
			t.Errorf("%q.Actionable() = %v, want %v", step, got, want)
		}
	}
}

func TestNextStreamStep(t *testing.T) {
	cases := map[string]StreamStep{
		StreamAcquiring: StreamStepAcquiring,
		StreamUploaded:  StreamStepUploading,
		StreamReady:     StreamStepEdit,
		StreamRendering: StreamStepRendering,
		StreamRendered:  StreamStepReady,
		StreamFailed:    StreamStepRetry,
		"bogus":         StreamStepWait,
	}
	for status, want := range cases {
		if got := NextStreamStep(status); got != want {
			t.Errorf("NextStreamStep(%q) = %q, want %q", status, got, want)
		}
	}
}
