package recording

import (
	"fmt"
	"math"
)

// ValidateArtifacts returns non-fatal warnings for missing or suspicious
// recorder outputs. The caller still owns deciding whether to fail a job.
func ValidateArtifacts(plan RecordingPlan, artifacts []RecordingArtifact) []string {
	var warnings []string
	for _, a := range artifacts {
		if a.ProbeError != "" {
			warnings = append(warnings, fmt.Sprintf("%s: %s", a.Path, a.ProbeError))
		}
	}

	bySegment := map[string][]RecordingArtifact{}
	rawTakes := map[string]bool{}
	for _, a := range artifacts {
		if a.SegmentID != "" {
			bySegment[a.SegmentID] = append(bySegment[a.SegmentID], a)
		}
		if a.Role == "raw" && a.TakeID != "" {
			rawTakes[a.TakeID] = true
		}
	}
	if len(rawTakes) != len(plan.Segments) {
		warnings = append(warnings, fmt.Sprintf("raw take count = %d, want %d", len(rawTakes), len(plan.Segments)))
	}

	for _, s := range plan.Segments {
		items := bySegment[s.ID]
		if !hasArtifact(items, "raw", "video") {
			warnings = append(warnings, fmt.Sprintf("segment %s missing raw video", s.ID))
		}
		if !hasArtifact(items, "raw", "audio") {
			warnings = append(warnings, fmt.Sprintf("segment %s missing raw audio", s.ID))
		}
		if !hasArtifact(items, "segment", "video") {
			warnings = append(warnings, fmt.Sprintf("segment %s missing muxed clip", s.ID))
		}
		recordStart := EffectiveRecordStartTick(s, plan.Tickrate)
		expected := float64(s.TickEnd-recordStart) / float64(plan.Tickrate)
		for _, a := range items {
			if a.Type != "video" || a.DurationSeconds <= 0 {
				continue
			}
			if math.Abs(a.DurationSeconds-expected) > 0.25 {
				warnings = append(warnings, fmt.Sprintf("segment %s %s video duration %.3fs differs from expected %.3fs", s.ID, a.Role, a.DurationSeconds, expected))
			}
		}
	}
	return warnings
}

func hasArtifact(items []RecordingArtifact, role, typ string) bool {
	for _, a := range items {
		if a.Role == role && a.Type == typ && a.Path != "" && a.ProbeError == "" {
			return true
		}
	}
	return false
}
