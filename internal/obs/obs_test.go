package obs

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRecordErrorWritesJournalAndCounters(t *testing.T) {
	dir := t.TempDir()
	r, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if runtime.GOOS != "windows" {
		if info, statErr := os.Stat(dir); statErr != nil {
			t.Fatalf("stat obs dir: %v", statErr)
		} else if got, want := info.Mode().Perm(), os.FileMode(0o700); got != want {
			t.Errorf("obs dir permissions: got %o want %o", got, want)
		}
	}

	if err := r.RecordError(Event{Stage: StageParse, Class: "target_not_found", Message: "no such player", Demo: "a.dem", Target: "76561198000000000"}); err != nil {
		t.Fatalf("RecordError: %v", err)
	}
	if err := r.RecordError(Event{Stage: StageParse, Class: "target_not_found", Message: "again", Demo: "b.dem"}); err != nil {
		t.Fatalf("RecordError: %v", err)
	}
	if err := r.RecordSuccess(StageParse); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}

	events := readJournal(t, r.JournalPath())
	if got, want := len(events), 2; got != want {
		t.Fatalf("journal lines: got %d want %d", got, want)
	}
	if events[0].Demo != "a.dem" || events[0].Class != "target_not_found" {
		t.Errorf("first event: got %+v", events[0])
	}
	if events[0].Time.IsZero() {
		t.Errorf("first event time not set: %+v", events[0])
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(r.JournalPath()); err != nil {
			t.Fatalf("stat journal: %v", err)
		} else if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
			t.Errorf("journal permissions: got %o want %o", got, want)
		}
	}

	want := map[string]int64{
		`fragforge_errors_total{class="target_not_found",stage="parse"}`: 2,
		`fragforge_stage_runs_total{result="error",stage="parse"}`:       2,
		`fragforge_stage_runs_total{result="ok",stage="parse"}`:          1,
	}
	got := counterMap(r.Snapshot())
	for k, v := range want {
		if got[k] != v {
			t.Errorf("counter %s: got %d want %d", k, got[k], v)
		}
	}
}

func TestRecordErrorRestrictsExistingJournalPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose POSIX permission bits")
	}
	dir := t.TempDir()
	journalPath := filepath.Join(dir, "journal.jsonl")
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatalf("seed journal: %v", err)
	}
	r, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.RecordError(Event{Stage: StageParse, Class: "test", Message: "test"}); err != nil {
		t.Fatalf("RecordError: %v", err)
	}
	info, err := os.Stat(journalPath)
	if err != nil {
		t.Fatalf("stat journal: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Errorf("journal permissions: got %o want %o", got, want)
	}
}

func TestRecordErrorDefaultsStageAndClass(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.RecordError(Event{Message: "boom"}); err != nil {
		t.Fatalf("RecordError: %v", err)
	}
	got := counterMap(r.Snapshot())
	if got[`fragforge_errors_total{class="unknown",stage="unknown"}`] != 1 {
		t.Errorf("expected unknown/unknown counter, got %v", got)
	}
}

func TestCountersPersistAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	r1, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r1.RecordError(Event{Stage: StageRender, Class: "ffmpeg_failed", Message: "x"}); err != nil {
		t.Fatalf("RecordError: %v", err)
	}

	r2, err := New(dir)
	if err != nil {
		t.Fatalf("re-New: %v", err)
	}
	if err := r2.RecordError(Event{Stage: StageRender, Class: "ffmpeg_failed", Message: "y"}); err != nil {
		t.Fatalf("RecordError: %v", err)
	}
	got := counterMap(r2.Snapshot())
	if got[`fragforge_errors_total{class="ffmpeg_failed",stage="render"}`] != 2 {
		t.Errorf("counter did not accumulate across reopen: %v", got)
	}
}

func TestWritePrometheusFormat(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.RecordError(Event{Stage: StageParse, Class: "corrupt", Message: "x"}); err != nil {
		t.Fatalf("RecordError: %v", err)
	}
	var b strings.Builder
	WritePrometheus(&b, r.Snapshot())
	out := b.String()
	for _, want := range []string{
		"# HELP fragforge_errors_total",
		"# TYPE fragforge_errors_total counter",
		`fragforge_errors_total{class="corrupt",stage="parse"} 1`,
		"# TYPE fragforge_stage_runs_total counter",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prometheus output missing %q\n---\n%s", want, out)
		}
	}
}

func TestMetricsPromFileWritten(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := r.RecordSuccess(StageCompose); err != nil {
		t.Fatalf("RecordSuccess: %v", err)
	}
	b, err := os.ReadFile(r.MetricsPromPath())
	if err != nil {
		t.Fatalf("read prom file: %v", err)
	}
	if !strings.Contains(string(b), `fragforge_stage_runs_total{result="ok",stage="compose"} 1`) {
		t.Errorf("prom file missing expected series:\n%s", b)
	}
}

func readJournal(t *testing.T, path string) []Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	defer f.Close()
	var events []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("unmarshal journal line %q: %v", line, err)
		}
		events = append(events, ev)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan journal: %v", err)
	}
	return events
}

func counterMap(metrics []Metric) map[string]int64 {
	m := map[string]int64{}
	for _, metric := range metrics {
		m[seriesKey(metric.Name, metric.Labels)] = metric.Value
	}
	return m
}
