package batch

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/obs"
	"github.com/rechedev9/fragforge/internal/parser"
)

func TestIsDemo(t *testing.T) {
	cases := map[string]bool{
		"a.dem":        true,
		"A.DEM":        true,
		"dir/b.Dem":    true,
		"c.demo":       false,
		"d.txt":        false,
		"noext":        false,
		"e.dem.txt":    false,
		"f.dem.backup": false,
	}
	for in, want := range cases {
		if got := isDemo(in); got != want {
			t.Errorf("isDemo(%q): got %v want %v", in, got, want)
		}
	}
}

func TestFindDemos(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.dem")
	write("b.dem")
	write("notes.txt")
	write("sub/c.dem")

	flat, err := findDemos(root, false)
	if err != nil {
		t.Fatalf("findDemos flat: %v", err)
	}
	if got, want := len(flat), 2; got != want {
		t.Errorf("flat demos: got %d want %d (%v)", got, want, flat)
	}

	deep, err := findDemos(root, true)
	if err != nil {
		t.Fatalf("findDemos recursive: %v", err)
	}
	if got, want := len(deep), 3; got != want {
		t.Errorf("recursive demos: got %d want %d (%v)", got, want, deep)
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"panic", errParsePanic, "panic"},
		{"no_target", errNoTarget, "no_target"},
		{"target_not_found", parser.ErrTargetNotFound, "target_not_found"},
		{"canceled", context.Canceled, "canceled"},
		{"deadline", context.DeadlineExceeded, "canceled"},
		{"missing", os.ErrNotExist, "file_not_found"},
		{"perm", os.ErrPermission, "file_permission"},
		{"other", errors.New("boom"), "corrupt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, msg := classify(c.err)
			if got != c.want {
				t.Errorf("classify(%v): got class %q want %q", c.err, got, c.want)
			}
			if msg == "" {
				t.Error("classify returned empty message")
			}
		})
	}
}

func TestRunNoDemos(t *testing.T) {
	rec := newRecorder(t)
	_, err := Run(context.Background(), Options{Dir: t.TempDir()}, rec, nil)
	if err == nil {
		t.Fatal("expected error for empty directory")
	}
}

// TestRunRecordsCorruptDemo feeds a garbage .dem so the parser fails (or
// panics), and asserts the failure is recorded in the obs journal rather than
// crashing the run.
func TestRunRecordsCorruptDemo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "garbage.dem"), []byte("not a real demo file at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := newRecorder(t)
	// Pass a SteamID so the run skips roster auto-detection and goes straight to
	// the kill-plan parse, which must fail on garbage input.
	sum, err := Run(context.Background(), Options{Dir: dir, SteamID: "76561198000000000"}, rec, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Total != 1 || sum.Failed != 1 || sum.OK != 0 {
		t.Fatalf("summary: got %+v want 1 total / 1 failed", sum)
	}
	if sum.Results[0].OK {
		t.Errorf("expected demo to fail, got %+v", sum.Results[0])
	}

	events := readJournalT(t, rec.JournalPath())
	if len(events) != 1 {
		t.Fatalf("journal: got %d events want 1", len(events))
	}
	if events[0].Stage != obs.StageParse {
		t.Errorf("event stage: got %q want %q", events[0].Stage, obs.StageParse)
	}
	if events[0].Class == "" {
		t.Errorf("event missing class: %+v", events[0])
	}

	if v := counterValue(rec, "fragforge_stage_runs_total", map[string]string{"result": "error", "stage": "parse"}); v != 1 {
		t.Errorf("expected one recorded parse error, got %d", v)
	}
}

func TestDefaultJobsAtLeastOne(t *testing.T) {
	if got := defaultJobs(); got < 1 {
		t.Errorf("defaultJobs: got %d want >= 1", got)
	}
}

func TestRecoverParse(t *testing.T) {
	t.Run("panic becomes errParsePanic", func(t *testing.T) {
		err := recoverParse(func() error { panic("boom") })
		if !errors.Is(err, errParsePanic) {
			t.Fatalf("got %v, want errParsePanic", err)
		}
		if class, _ := classify(err); class != "panic" {
			t.Errorf("classify: got %q want panic", class)
		}
	})
	t.Run("normal error passes through", func(t *testing.T) {
		want := errors.New("plain")
		if got := recoverParse(func() error { return want }); got != want {
			t.Errorf("got %v want %v", got, want)
		}
	})
	t.Run("nil stays nil", func(t *testing.T) {
		if got := recoverParse(func() error { return nil }); got != nil {
			t.Errorf("got %v want nil", got)
		}
	})
}

// TestRunAutoTargetRecordsBadDemo drives the default auto-target path (empty
// SteamID -> topFragger, which parses in its own goroutine) with a garbage demo
// and asserts the failure is recorded rather than crashing the run. This covers
// the goroutine panic-recovery path the SteamID test deliberately skips.
func TestRunAutoTargetRecordsBadDemo(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "garbage.dem"), []byte("HL2DEMO\x00 not really a demo"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := newRecorder(t)
	sum, err := Run(context.Background(), Options{Dir: dir}, rec, nil) // empty SteamID -> auto-target
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if sum.Total != 1 || sum.Failed != 1 {
		t.Fatalf("summary: got %+v want 1 total / 1 failed", sum)
	}
	events := readJournalT(t, rec.JournalPath())
	if len(events) != 1 || events[0].Stage != obs.StageParse {
		t.Fatalf("journal: got %+v want one parse-stage event", events)
	}
}

func TestRecorderResetClearsCounters(t *testing.T) {
	rec := newRecorder(t)
	if err := rec.RecordError(obs.Event{Stage: obs.StageParse, Class: "corrupt", Message: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := rec.Reset(); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if got := rec.Snapshot(); len(got) != 0 {
		t.Errorf("after Reset: got %d counters want 0", len(got))
	}
}

func newRecorder(t *testing.T) *obs.Recorder {
	t.Helper()
	rec, err := obs.New(t.TempDir())
	if err != nil {
		t.Fatalf("obs.New: %v", err)
	}
	return rec
}

// counterValue returns the value of the metric series matching name and labels,
// or 0 if no such series exists.
func counterValue(rec *obs.Recorder, name string, labels map[string]string) int64 {
	for _, m := range rec.Snapshot() {
		if m.Name != name || len(m.Labels) != len(labels) {
			continue
		}
		match := true
		for k, v := range labels {
			if m.Labels[k] != v {
				match = false
				break
			}
		}
		if match {
			return m.Value
		}
	}
	return 0
}

func readJournalT(t *testing.T, path string) []obs.Event {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	var events []obs.Event
	for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var ev obs.Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshal %q: %v", line, err)
		}
		events = append(events, ev)
	}
	return events
}
