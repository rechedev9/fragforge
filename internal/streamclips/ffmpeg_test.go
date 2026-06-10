package streamclips

import (
	"strings"
	"testing"
)

func TestEditPlanValidationRejectsOutOfBoundsCrop(t *testing.T) {
	plan := DefaultEditPlan()
	plan.FaceCrop = CropRect{X: 0.8, Y: 0, Width: 0.3, Height: 0.2}

	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "within the source frame") {
		t.Fatalf("Validate error = %v, want source frame bounds error", err)
	}
}

func TestEditPlanValidationRejectsInvalidClipRange(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 12, EndSeconds: 10}}

	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "greater than start_seconds") {
		t.Fatalf("Validate error = %v, want range error", err)
	}
}

func TestBuildFFmpegArgsCreatesVerticalStackCommand(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 1.5, EndSeconds: 4.25}}

	args, err := BuildFFmpegArgs("source.mp4", "out.mp4", plan, plan.Clips[0])
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-ss 1.500",
		"-t 2.750",
		"scale=1080:520",
		"scale=1080:1400",
		"vstack=inputs=2",
		"fps=60",
		"-map 0:a?",
		"-crf 18",
		"-movflags +faststart",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
	if got := args[len(args)-1]; got != "out.mp4" {
		t.Fatalf("last arg = %q, want output path", got)
	}
}
