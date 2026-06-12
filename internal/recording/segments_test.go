package recording

import "testing"

func TestSegmentIDsReturnsUniqueNonEmptyPlanIDs(t *testing.T) {
	got := SegmentIDs(RecordingResult{
		Plan: RecordingPlan{
			Segments: []RecordingSegment{
				{ID: "seg-001"},
				{ID: ""},
				{ID: "seg-002"},
				{ID: "seg-001"},
			},
		},
	})
	want := []string{"seg-001", "seg-002"}
	if len(got) != len(want) {
		t.Fatalf("SegmentIDs len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("SegmentIDs[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSegmentIDsAllowsEmptyResult(t *testing.T) {
	got := SegmentIDs(RecordingResult{})
	if len(got) != 0 {
		t.Fatalf("SegmentIDs = %#v, want empty", got)
	}
}
