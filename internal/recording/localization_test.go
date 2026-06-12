package recording

import (
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestNewSegmentClipLocalizationsDerivesKeysAndLocalPaths(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	workDir := filepath.Join("work", "job")

	got, err := NewSegmentClipLocalizations(id, workDir, RecordingResult{
		Artifacts: []RecordingArtifact{{
			SegmentID: "seg-001",
			Type:      "audio",
			Role:      "raw",
		}, {
			SegmentID: "seg-001",
			Type:      "video",
			Role:      "segment",
			Path:      "stale.mp4",
		}, {
			SegmentID: "seg-002",
			Type:      "video",
			Role:      "segment",
			Path:      "stale-2.mp4",
		}},
	})
	if err != nil {
		t.Fatalf("NewSegmentClipLocalizations error = %v", err)
	}

	prefix := "jobs/11111111-1111-1111-1111-111111111111/recording/segments"
	want := []SegmentClipLocalization{{
		ArtifactIndex: 1,
		SegmentID:     "seg-001",
		Key:           prefix + "/seg-001.mp4",
		LocalPath:     filepath.Join(workDir, "segments", "seg-001.mp4"),
	}, {
		ArtifactIndex: 2,
		SegmentID:     "seg-002",
		Key:           prefix + "/seg-002.mp4",
		LocalPath:     filepath.Join(workDir, "segments", "seg-002.mp4"),
	}}
	if len(got) != len(want) {
		t.Fatalf("localizations len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("localization[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
