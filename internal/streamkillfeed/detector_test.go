package streamkillfeed

import (
	"testing"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

func TestDetectFrameEventsOneFrameKill(t *testing.T) {
	t.Parallel()
	timeBase := TimeBase{Num: 1, Den: 1000}
	notice := testObservedRow(0, testFingerprint(0))
	frames := []frameObservation{
		testFrame(900, timeBase),
		testFrame(1000, timeBase, notice),
		testFrame(1033, timeBase),
	}

	events, err := detectFrameEvents(frames, testClip())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	event := events[0]
	if got, want := event.SourcePTS, int64(1000); got != want {
		t.Errorf("SourcePTS = %d, want %d", got, want)
	}
	if got, want := event.OnsetStartPTS, int64(900); got != want {
		t.Errorf("OnsetStartPTS = %d, want %d", got, want)
	}
	if got, want := event.SamplePTS, int64(1000); got != want {
		t.Errorf("SamplePTS = %d, want transient onset %d", got, want)
	}
	if got, want := event.Mode, ModeAlignedFrame; got != want {
		t.Errorf("Mode = %q, want %q", got, want)
	}
}

func TestDetectFrameEventsFastSameCountReplacement(t *testing.T) {
	t.Parallel()
	timeBase := TimeBase{Num: 1, Den: 1000}
	oldNotice := testObservedRow(0, testFingerprint(0))
	newNotice := testObservedRow(0, testFingerprint(2))
	frames := []frameObservation{
		testFrame(900, timeBase, oldNotice),
		testFrame(967, timeBase, oldNotice),
		testFrame(1000, timeBase, newNotice),
		testFrame(1033, timeBase, newNotice),
		testFrame(1400, timeBase, newNotice),
	}

	events, err := detectFrameEvents(frames, testClip())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	event := events[0]
	if got, want := event.SourcePTS, int64(1000); got != want {
		t.Errorf("SourcePTS = %d, want replacement PTS %d", got, want)
	}
	if got, want := event.SamplePTS, int64(1400); got != want {
		t.Errorf("SamplePTS = %d, want first settled frame %d", got, want)
	}
	if got, want := event.Rows[0].Fingerprint, fingerprintKey(newNotice.fingerprint); got != want {
		t.Errorf("Fingerprint = %q, want replacement fingerprint %q", got, want)
	}
}

func TestDetectFrameEventsSameFrameBurst(t *testing.T) {
	t.Parallel()
	timeBase := TimeBase{Num: 1, Den: 1000}
	first := testObservedRow(0, testFingerprint(0))
	second := testObservedRow(1, testFingerprint(2))
	frames := []frameObservation{
		testFrame(967, timeBase),
		testFrame(1000, timeBase, first, second),
		testFrame(1400, timeBase, first, second),
	}

	events, err := detectFrameEvents(frames, testClip())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	event := events[0]
	if got, want := event.Mode, ModeBurst; got != want {
		t.Errorf("Mode = %q, want %q", got, want)
	}
	if got, want := len(event.Rows), 2; got != want {
		t.Errorf("row count = %d, want %d", got, want)
	}
	if got, want := event.SourcePTS, int64(1000); got != want {
		t.Errorf("SourcePTS = %d, want %d", got, want)
	}
}

func TestDetectFrameEventsPreexistingNotice(t *testing.T) {
	t.Parallel()
	timeBase := TimeBase{Num: 1, Den: 1000}
	notice := testObservedRow(0, testFingerprint(0))
	frames := []frameObservation{
		testFrame(800, timeBase),
		testFrame(900, timeBase, notice),
		testFrame(1000, timeBase, notice),
		testFrame(1100, timeBase, notice),
	}

	events, err := detectFrameEvents(frames, testClip())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(events); got != 0 {
		t.Fatalf("event count = %d, want no fabricated clip-boundary event: %+v", got, events)
	}
}

func TestDetectFrameEventsNeverMergesAdjacentPTS(t *testing.T) {
	t.Parallel()
	timeBase := TimeBase{Num: 1, Den: 1000}
	first := testObservedRow(0, testFingerprint(0))
	second := testObservedRow(0, testFingerprint(2))
	frames := []frameObservation{
		testFrame(999, timeBase),
		testFrame(1000, timeBase, first),
		testFrame(1001, timeBase, second),
	}

	events, err := detectFrameEvents(frames, testClip())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 2; got != want {
		t.Fatalf("event count = %d, want %d distinct PTS events", got, want)
	}
	if events[0].SourcePTS == events[1].SourcePTS {
		t.Fatalf("adjacent events unexpectedly share PTS %d", events[0].SourcePTS)
	}
}

func TestDetectFrameEventsCollapsesNearDuplicateJitterBirths(t *testing.T) {
	t.Parallel()
	timeBase := TimeBase{Num: 1, Den: 1000}
	var firstFingerprint rowFingerprint
	firstFingerprint.bits[0] = ^uint64(0)
	firstFingerprint.bits[1] = ^uint64(0)
	firstFingerprint.features = 128
	var jitterFingerprint rowFingerprint
	jitterFingerprint.bits[0] = ^uint64(0)
	jitterFingerprint.bits[1] = uint64(1)<<38 - 1
	jitterFingerprint.bits[2] = uint64(1)<<26 - 1
	jitterFingerprint.features = 128
	frames := []frameObservation{
		testFrame(1900, timeBase),
		testFrame(1920, timeBase, testObservedRow(0, firstFingerprint)),
		testFrame(1950, timeBase, testObservedRow(0, jitterFingerprint)),
	}
	clip := streamclips.ClipRange{ID: "clip-1", StartSeconds: 1, EndSeconds: 3}
	events, err := detectFrameEvents(frames, clip)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d jitter-collapsed event", got, want)
	}
}

func TestDetectFrameEventsCollapsesJitterThatReturnsToOriginalFingerprint(t *testing.T) {
	t.Parallel()
	timeBase := TimeBase{Num: 1, Den: 1000}
	var firstFingerprint rowFingerprint
	firstFingerprint.bits[0] = ^uint64(0)
	firstFingerprint.bits[1] = ^uint64(0)
	firstFingerprint.features = 128
	var jitterFingerprint rowFingerprint
	jitterFingerprint.bits[0] = ^uint64(0)
	jitterFingerprint.bits[1] = uint64(1)<<38 - 1
	jitterFingerprint.bits[2] = uint64(1)<<26 - 1
	jitterFingerprint.features = 128
	frames := []frameObservation{
		testFrame(1900, timeBase),
		testFrame(1920, timeBase, testObservedRow(0, firstFingerprint)),
		testFrame(1950, timeBase, testObservedRow(0, jitterFingerprint)),
		testFrame(1980, timeBase, testObservedRow(0, firstFingerprint)),
	}
	clip := streamclips.ClipRange{ID: "clip-1", StartSeconds: 1, EndSeconds: 3}
	events, err := detectFrameEvents(frames, clip)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d A-B-A jitter-collapsed event", got, want)
	}
}

func TestDetectFrameEventsCollapsesJitterAcrossInterveningEvent(t *testing.T) {
	t.Parallel()
	timeBase := TimeBase{Num: 1, Den: 1000}
	var firstFingerprint rowFingerprint
	firstFingerprint.bits[0] = ^uint64(0)
	firstFingerprint.bits[1] = ^uint64(0)
	firstFingerprint.features = 128
	var jitterFingerprint rowFingerprint
	jitterFingerprint.bits[0] = ^uint64(0)
	jitterFingerprint.bits[1] = uint64(1)<<38 - 1
	jitterFingerprint.bits[2] = uint64(1)<<26 - 1
	jitterFingerprint.features = 128
	other := testFingerprint(8)
	frames := []frameObservation{
		testFrame(1900, timeBase),
		testFrame(1920, timeBase, testObservedRow(0, firstFingerprint)),
		testFrame(1940, timeBase, testObservedRow(0, firstFingerprint), testObservedRow(1, other)),
		testFrame(1960, timeBase, testObservedRow(0, jitterFingerprint), testObservedRow(1, other)),
	}
	clip := streamclips.ClipRange{ID: "clip-1", StartSeconds: 1, EndSeconds: 3}
	events, err := detectFrameEvents(frames, clip)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 2; got != want {
		t.Fatalf("event count = %d, want %d distinct events with repeated A jitter collapsed", got, want)
	}
}

func TestDetectFrameEventsClampsSampleBeforeClipEnd(t *testing.T) {
	t.Parallel()
	timeBase := TimeBase{Num: 1, Den: 1000}
	notice := testObservedRow(0, testFingerprint(0))
	frames := []frameObservation{
		testFrame(1850, timeBase),
		testFrame(1900, timeBase, notice),
		testFrame(1950, timeBase, notice),
		testFrame(2000, timeBase, notice),
		testFrame(2250, timeBase, notice),
	}

	events, err := detectFrameEvents(frames, testClip())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(events), 1; got != want {
		t.Fatalf("event count = %d, want %d", got, want)
	}
	if got, want := events[0].SamplePTS, int64(1950); got != want {
		t.Errorf("SamplePTS = %d, want last matching frame inside clip %d", got, want)
	}
	if got := events[0].SampleSeconds; got >= testClip().EndSeconds {
		t.Errorf("SampleSeconds = %.6f, want < clip end %.6f", got, testClip().EndSeconds)
	}
}

func TestRelativeVideoSecondsSubtractsNonZeroStartTime(t *testing.T) {
	t.Parallel()
	timeBase := TimeBase{Num: 1, Den: 30000}
	if got, want := relativeVideoSeconds(180000, timeBase, 5), 1.0; got != want {
		t.Errorf("relativeVideoSeconds() = %.12f, want %.12f", got, want)
	}
}

func testClip() streamclips.ClipRange {
	return streamclips.ClipRange{
		ID:           "clip-1",
		StartSeconds: 1,
		EndSeconds:   2,
	}
}

func testFrame(pts int64, timeBase TimeBase, rows ...observedRow) frameObservation {
	return frameObservation{
		pts:      pts,
		timeBase: timeBase,
		seconds:  timeBase.Seconds(pts),
		rows:     rows,
	}
}

func testObservedRow(index int, fingerprint rowFingerprint) observedRow {
	return observedRow{
		index:       index,
		bounds:      streamclips.NoticeRow{X: 100, Y: 20 + index*40, Width: 220, Height: 36},
		fingerprint: fingerprint,
	}
}

func testFingerprint(word int) rowFingerprint {
	var fingerprint rowFingerprint
	fingerprint.bits[word] = ^uint64(0)
	fingerprint.features = 64
	return fingerprint
}
