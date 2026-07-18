package streamclips

import "testing"

func TestEditPlanFingerprintTracksReviewedRenderContent(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 1, EndSeconds: 2}}
	first, err := EditPlanFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	plan.Clips[0].CaptionReviewed = true
	second, err := EditPlanFingerprint(plan)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("fingerprint did not change with reviewed render content")
	}
	if got, err := EditPlanFingerprint(plan); err != nil || got != second {
		t.Fatalf("fingerprint = %q, %v; want deterministic %q", got, err, second)
	}
}
