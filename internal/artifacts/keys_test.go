package artifacts

import (
	"testing"

	"github.com/google/uuid"
)

func TestKeysUseStableJobLayout(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	segmentKey, err := SegmentClipKey(id, "s1")
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		JobPrefix(id):            "jobs/11111111-1111-1111-1111-111111111111",
		RecordingResultKey(id):   "jobs/11111111-1111-1111-1111-111111111111/recording/recording-result.json",
		RecordingScriptKey(id):   "jobs/11111111-1111-1111-1111-111111111111/recording/recording.js",
		segmentKey:               "jobs/11111111-1111-1111-1111-111111111111/recording/segments/s1.mp4",
		CompositionResultKey(id): "jobs/11111111-1111-1111-1111-111111111111/composition/composition-result.json",
		FinalMP4Key(id):          "jobs/11111111-1111-1111-1111-111111111111/composition/final.mp4",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("key = %q, want %q", got, want)
		}
	}
}

func TestSegmentClipKeyRejectsPathLikeIDs(t *testing.T) {
	id := uuid.New()
	for _, segmentID := range []string{"", "../x", "x/y", `x\y`, "-bad"} {
		if _, err := SegmentClipKey(id, segmentID); err == nil {
			t.Fatalf("SegmentClipKey(%q) error = nil, want error", segmentID)
		}
	}
}
