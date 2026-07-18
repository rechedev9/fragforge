package streamkillfeed

import (
	"context"
	"testing"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

func TestDecodeRefinementFramesFindsNativePredecessorAcrossPresentationGap(t *testing.T) {
	t.Parallel()

	timeBase := TimeBase{Num: 1, Den: 1000}
	notice := testObservedRow(0, testFingerprint(0))
	sourceFrames := []frameObservation{
		testFrame(1000, timeBase),
		testFrame(12000, timeBase, notice),
		testFrame(12400, timeBase, notice),
	}
	request := decodeRequest{
		sourcePath:   "vfr-source.mp4",
		startSeconds: 11.75,
		endSeconds:   12.5,
	}
	var requestedStarts []float64
	decode := func(
		_ context.Context,
		request decodeRequest,
		_ streamclips.CropRect,
	) ([]frameObservation, error) {
		requestedStarts = append(requestedStarts, request.startSeconds)
		var frames []frameObservation
		for _, frame := range sourceFrames {
			if frame.seconds >= request.startSeconds && frame.seconds < request.endSeconds {
				frames = append(frames, frame)
			}
		}
		return frames, nil
	}

	frames, err := decodeRefinementFrames(
		context.Background(),
		request,
		streamclips.CropRect{},
		decode,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(frames), 3; got != want {
		t.Fatalf("frame count = %d, want %d: %+v", got, want, frames)
	}
	for i, want := range []int64{1000, 12000, 12400} {
		if got := frames[i].pts; got != want {
			t.Errorf("frame %d PTS = %d, want exact native PTS %d", i, got, want)
		}
	}
	if got := requestedStarts[len(requestedStarts)-1]; got > 1 {
		t.Errorf("search stopped at %.9f, want it widened through predecessor at 1s", got)
	}

	events, err := detectFrameEvents(frames, streamclips.ClipRange{
		ID:           "clip-vfr-gap",
		StartSeconds: 11,
		EndSeconds:   13,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d: %+v", got, want, events)
	}
	if got, want := events[0].OnsetStartPTS, int64(1000); got != want {
		t.Errorf("OnsetStartPTS = %d, want native predecessor PTS %d", got, want)
	}
	if got, want := events[0].SourcePTS, int64(12000); got != want {
		t.Errorf("SourcePTS = %d, want native onset PTS %d", got, want)
	}
	if got, want := events[0].Mode, ModeAlignedFrame; got != want {
		t.Errorf("Mode = %q, want %q", got, want)
	}
}
