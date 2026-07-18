package streamclips

import "testing"

func TestNormalizeEditPlanKeepsAutomaticProvenanceBoundToCue(t *testing.T) {
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &CropRect{X: 0.7, Y: 0.02, Width: 0.28, Height: 0.2}
	plan.Clips = []ClipRange{{
		ID:              "clip-001",
		StartSeconds:    0,
		EndSeconds:      3,
		KillfeedSeconds: []float64{2, 1},
		KillfeedKills: [][]KillfeedKill{
			{{AttackerSide: "CT", AttackerName: "two", VictimSide: "T", VictimName: "victim", Weapon: "ak47"}},
			{{AttackerSide: "CT", AttackerName: "one", VictimSide: "T", VictimName: "victim", Weapon: "ak47"}},
		},
		KillfeedCueProvenance: []KillfeedCueProvenance{
			{CueSeconds: 2, Origin: KillfeedCueAutomatic, EventID: "event-two"},
			{CueSeconds: 1, Origin: KillfeedCueManual},
			{CueSeconds: 2.5, Origin: KillfeedCueAutomatic, EventID: "stale-event"},
		},
	}}

	got := NormalizeEditPlan(plan).Clips[0]
	if len(got.KillfeedCueProvenance) != 2 {
		t.Fatalf("provenance = %#v, want only live cues", got.KillfeedCueProvenance)
	}
	manual, ok := got.KillfeedProvenanceAt(1)
	if !ok || manual.Origin != KillfeedCueManual {
		t.Fatalf("cue 1 provenance = %#v / %v, want manual", manual, ok)
	}
	automatic, ok := got.KillfeedProvenanceAt(2)
	if !ok || automatic.Origin != KillfeedCueAutomatic || automatic.EventID != "event-two" {
		t.Fatalf("cue 2 provenance = %#v / %v, want automatic event-two", automatic, ok)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("normalized clip validation: %v", err)
	}
}

func TestClipRangeRejectsManualCueWithAutomaticEventID(t *testing.T) {
	clip := ClipRange{
		ID:              "clip-001",
		StartSeconds:    0,
		EndSeconds:      2,
		KillfeedSeconds: []float64{1},
		KillfeedCueProvenance: []KillfeedCueProvenance{{
			CueSeconds: 1, Origin: KillfeedCueManual, EventID: "automatic-event",
		}},
	}
	if err := clip.Validate(); err == nil {
		t.Fatal("manual cue with automatic event id unexpectedly validated")
	}
}
