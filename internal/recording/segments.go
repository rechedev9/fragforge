package recording

// SegmentIDs returns the unique non-empty segment IDs from the recording plan
// in result order.
func SegmentIDs(result RecordingResult) []string {
	seen := map[string]bool{}
	var ids []string
	for _, segment := range result.Plan.Segments {
		if segment.ID == "" || seen[segment.ID] {
			continue
		}
		seen[segment.ID] = true
		ids = append(ids, segment.ID)
	}
	return ids
}
