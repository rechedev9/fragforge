package editor

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/rechedev9/fragforge/internal/rhythm"
)

func loadRhythmSync(path string) (map[string]rhythm.SegmentSync, error) {
	if path == "" {
		return nil, nil
	}
	// #nosec G304 -- rhythm path is an explicit local CLI/config input.
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rhythm json: %w", err)
	}
	var analysis rhythm.Analysis
	if err := json.Unmarshal(b, &analysis); err != nil {
		return nil, fmt.Errorf("decode rhythm json: %w", err)
	}
	if len(analysis.SegmentSync) == 0 {
		return nil, fmt.Errorf("rhythm json has no segment_sync entries")
	}
	indexed := make(map[string]rhythm.SegmentSync, len(analysis.SegmentSync))
	for _, entry := range analysis.SegmentSync {
		if entry.SegmentID == "" {
			return nil, fmt.Errorf("rhythm json contains segment_sync entry without segment_id")
		}
		indexed[entry.SegmentID] = entry
	}
	return indexed, nil
}
