package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRhythmSyncIndexesSegments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rhythm.json")
	if err := os.WriteFile(path, []byte(`{
		"schema_version":"1.0",
		"segment_sync":[
			{"segment_id":"seg-001","timeline_start_seconds":0.5,"target_kill_time_seconds":1.5},
			{"segment_id":"seg-002","timeline_start_seconds":4.0,"target_kill_time_seconds":5.0}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	sync, err := loadRhythmSync(path)
	if err != nil {
		t.Fatalf("loadRhythmSync returned error: %v", err)
	}
	if got := sync["seg-002"].TimelineStartSeconds; got != 4.0 {
		t.Fatalf("seg-002 timeline start = %.3f, want 4.000", got)
	}
}

func TestLoadRhythmSyncRejectsEmptySegmentSync(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rhythm.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"1.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadRhythmSync(path)
	if err == nil {
		t.Fatal("loadRhythmSync returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "segment_sync") {
		t.Fatalf("error = %v, want segment_sync context", err)
	}
}

func TestLoadRhythmSyncRejectsMissingSegmentID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rhythm.json")
	if err := os.WriteFile(path, []byte(`{
		"schema_version":"1.0",
		"segment_sync":[{"timeline_start_seconds":0.5}]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadRhythmSync(path)
	if err == nil {
		t.Fatal("loadRhythmSync returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "without segment_id") {
		t.Fatalf("error = %v, want missing segment_id context", err)
	}
}
