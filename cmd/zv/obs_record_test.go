package main

import (
	"testing"

	"github.com/rechedev9/fragforge/internal/obs"
)

func TestShortStageClass(t *testing.T) {
	cases := []struct {
		binary    string
		code      int
		wantStage string
		wantClass string
	}{
		{"zv-parser", 3, obs.StageParse, "file_error"},
		{"zv-parser", 4, obs.StageParse, "corrupt"},
		{"zv-parser", 5, obs.StageParse, "target_not_found"},
		{"zv-parser", 1, obs.StageParse, "parse_failed"},
		{"zv-recorder", 1, obs.StageRecord, "record_failed"},
		{"zv-rhythm", 1, obs.StageRender, "rhythm_failed"},
		{"zv-editor", 1, obs.StageRender, "render_failed"},
		{"unknown-bin", 1, "short", "stage_failed"},
	}
	for _, c := range cases {
		t.Run(c.binary, func(t *testing.T) {
			gotStage, gotClass := shortStageClass(c.binary, c.code)
			if gotStage != c.wantStage || gotClass != c.wantClass {
				t.Errorf("shortStageClass(%q,%d): got (%q,%q) want (%q,%q)",
					c.binary, c.code, gotStage, gotClass, c.wantStage, c.wantClass)
			}
		})
	}
}

// TestRecordShortFailureWritesJournal verifies the best-effort recorder appends
// a journal line. TestMain points ZV_DATA_DIR at a temp dir, so this writes
// there rather than into the source tree.
func TestRecordShortFailureWritesJournal(t *testing.T) {
	demo := "TestRecordShortFailureWritesJournal-unique.dem"
	recordShortFailure(shortStage{label: "parsing demo", binary: "zv-parser", args: nil}, 5, demo)

	rec := obs.Default()
	if rec == nil {
		t.Fatal("obs.Default returned nil")
	}
	events, err := readEvents(rec.JournalPath())
	if err != nil {
		t.Fatalf("readEvents: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Demo == demo {
			found = true
			if ev.Class != "target_not_found" || ev.Stage != obs.StageParse {
				t.Errorf("event: got stage=%q class=%q want parse/target_not_found", ev.Stage, ev.Class)
			}
			if ev.ExitCode != 5 {
				t.Errorf("exit code: got %d want 5", ev.ExitCode)
			}
		}
	}
	if !found {
		t.Errorf("journal %s has no event for demo %q", rec.JournalPath(), demo)
	}
}
