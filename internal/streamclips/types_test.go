package streamclips

import (
	"strings"
	"testing"
)

func TestClampEditPlanToDuration(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 59, Title: "long"}}

	got, warnings, err := ClampEditPlanToDuration(plan, 19.55)
	if err != nil {
		t.Fatalf("ClampEditPlanToDuration error = %v", err)
	}
	if got.Clips[0].EndSeconds != 19.55 {
		t.Fatalf("end = %v, want 19.55", got.Clips[0].EndSeconds)
	}
	if plan.Clips[0].EndSeconds != 59 {
		t.Fatalf("input plan mutated: end = %v, want 59", plan.Clips[0].EndSeconds)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "clamped") {
		t.Fatalf("warnings = %v, want clamp warning", warnings)
	}
}

func TestClampEditPlanToDurationRejectsClipStartingAtEOF(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 20, EndSeconds: 21}}
	if _, _, err := ClampEditPlanToDuration(plan, 20); err == nil {
		t.Fatal("ClampEditPlanToDuration returned nil error for clip starting at EOF")
	}
}

func TestClampEditPlanToDurationLeavesUnknownDurationUnchanged(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 59}}
	got, warnings, err := ClampEditPlanToDuration(plan, 0)
	if err != nil || len(warnings) != 0 || got.Clips[0].EndSeconds != 59 {
		t.Fatalf("got plan=%+v warnings=%v err=%v, want unchanged", got, warnings, err)
	}
}

func TestNormalizeEditPlanUsesMultilingualCaptionDetection(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Captions = CaptionsPlan{Enabled: true, Language: "es"}
	got := NormalizeEditPlan(plan)
	if got.Captions.Language != "auto" {
		t.Fatalf("caption language = %q, want auto", got.Captions.Language)
	}
}
