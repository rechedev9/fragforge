package workers

import (
	"fmt"
	"strings"

	"github.com/rechedev9/fragforge/internal/recording"
)

// demoIncompatiblePrefix is the stable, machine-readable marker the web UI
// matches on to explain that CS2 cannot replay a demo recorded on an older
// build. Keep it exactly in sync with the frontend.
const demoIncompatiblePrefix = "demo_incompatible:"

// networkDisconnectMarker is the CS2 playback error substring that is stable
// across both the old and the new zv-recorder wording.
const networkDisconnectMarker = "NETWORK_DISCONNECT_MESSAGE_PARSE_ERROR"

// recordFailure carries a concise, user-facing job failure reason while still
// wrapping the original noisy recorder error so logs and tests can unwrap the
// full chain.
type recordFailure struct {
	reason string
	err    error
}

func (f *recordFailure) Error() string { return f.reason }

func (f *recordFailure) Unwrap() error { return f.err }

// newRecordFailure wraps a recorder run error with a concise reason derived
// from its text, the decoded (possibly zero) recording result, and the segment
// ids this reel requested.
func newRecordFailure(runErr error, result recording.RecordingResult, requested []string) error {
	return &recordFailure{reason: recordFailureReason(runErr, result, requested), err: runErr}
}

// recordFailureReason condenses a noisy recorder run error into a concise
// reason. An incompatible-demo failure (keyed on the stable CS2 marker) becomes
// the demo_incompatible: prefix plus an optional captured-progress suffix; any
// other failure is reduced to its last "error: " line, falling back to the
// original text when there is none.
func recordFailureReason(runErr error, result recording.RecordingResult, requested []string) string {
	text := runErr.Error()
	if strings.Contains(text, networkDisconnectMarker) {
		reason := demoIncompatiblePrefix + " cs2 cannot replay this demo (it was recorded on an older cs2 build)"
		if captured := capturedSegmentCount(result); captured > 0 {
			reason += fmt.Sprintf("; captured %d/%d segments before the failure", captured, len(requested))
		}
		return reason
	}
	if line, ok := lastErrorLine(text); ok {
		return "recorder failed: " + line
	}
	return text
}

// capturedSegmentCount counts the distinct segment ids that produced a segment
// video artifact, i.e. the reels the recorder finished before it failed.
func capturedSegmentCount(result recording.RecordingResult) int {
	seen := map[string]struct{}{}
	for _, a := range result.Artifacts {
		if a.Role == "segment" && a.SegmentID != "" {
			seen[a.SegmentID] = struct{}{}
		}
	}
	return len(seen)
}

// lastErrorLine returns the last line beginning with "error: " with that prefix
// stripped, reporting whether such a line existed.
func lastErrorLine(text string) (string, bool) {
	line, ok := "", false
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if rest, cut := strings.CutPrefix(l, "error: "); cut {
			line, ok = strings.TrimSpace(rest), true
		}
	}
	return line, ok
}
