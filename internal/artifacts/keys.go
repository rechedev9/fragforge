// Package artifacts defines durable object-storage keys for job outputs.
package artifacts

import (
	"fmt"
	"path"
	"regexp"

	"github.com/google/uuid"
)

var segmentIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

func JobPrefix(id uuid.UUID) string {
	return path.Join("jobs", id.String())
}

func RecordingResultKey(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "recording", "recording-result.json")
}

func RecordingScriptKey(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "recording", "recording.js")
}

func SegmentClipKey(id uuid.UUID, segmentID string) (string, error) {
	if !segmentIDPattern.MatchString(segmentID) {
		return "", fmt.Errorf("invalid segment id %q", segmentID)
	}
	return path.Join(JobPrefix(id), "recording", "segments", segmentID+".mp4"), nil
}

func CompositionResultKey(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "composition", "composition-result.json")
}

func FinalMP4Key(id uuid.UUID) string {
	return path.Join(JobPrefix(id), "composition", "final.mp4")
}
