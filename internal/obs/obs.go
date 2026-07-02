// Package obs provides local, dependency-free observability for the FragForge
// pipeline: a structured error journal (newline-delimited JSON) plus counters
// exported in the Prometheus text exposition format.
//
// It exists so pipeline failures are recorded in one place that an operator (or
// an agent) can inspect without standing up Postgres, Redis, or a real
// Prometheus server. When a Prometheus server is available it can scrape the
// orchestrator's /metrics endpoint, which serves the same counters.
//
// The error journal is the authoritative record: every RecordError appends one
// line under O_APPEND, so concurrent writers (even across processes) never lose
// an event. The counters are a convenience derived from a load-modify-write of
// a small file; within one process the Recorder mutex serializes them, but two
// processes writing the same directory concurrently can lose a counter
// increment (the journal still has the event). Share one Recorder per process
// (see Default) and treat counts as approximate across processes.
package obs

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Pipeline stage labels used when recording events. They double as the `stage`
// metric label, so keep them short and stable.
const (
	StageParse   = "parse"
	StageRecord  = "record"
	StageRender  = "render"
	StageCompose = "compose"
	StageBatch   = "batch"
	StageHTTP    = "http"
	StageWorker  = "worker"

	// StageStreamAcquire labels failures downloading a stream job's source
	// video by URL (the AcquireWorker), so they are distinguishable from the
	// rest of the "worker" stage in the journal and metrics.
	StageStreamAcquire = "stream_acquire"
)

// Metric names. HELP text for each lives in metricHelp.
const (
	metricStageRuns = "fragforge_stage_runs_total"
	metricErrors    = "fragforge_errors_total"
)

var metricHelp = map[string]string{
	metricStageRuns: "Total pipeline stage runs by stage and result.",
	metricErrors:    "Total pipeline errors by stage and error class.",
}

// Event is one recorded pipeline error. It is appended to the journal as a
// single JSON line.
type Event struct {
	Time     time.Time `json:"time"`
	Stage    string    `json:"stage"`
	Class    string    `json:"class"`
	Message  string    `json:"message"`
	Demo     string    `json:"demo,omitempty"`
	Target   string    `json:"target_steamid,omitempty"`
	ExitCode int       `json:"exit_code,omitempty"`
}

// Recorder accumulates Prometheus-style counters and appends error events to a
// JSONL journal under dir. It is safe for concurrent use.
type Recorder struct {
	mu       sync.Mutex
	dir      string
	counters map[string]int64 // series key ("name{sorted,labels}") -> value
}

// DefaultDir returns the observability directory: $ZV_DATA_DIR/obs, defaulting
// to data/obs when ZV_DATA_DIR is unset.
func DefaultDir() string {
	base := os.Getenv("ZV_DATA_DIR")
	if base == "" {
		base = "data"
	}
	return filepath.Join(base, "obs")
}

// New opens (or creates) a Recorder rooted at dir, loading any counters
// persisted by a previous process so counts accumulate across CLI invocations.
func New(dir string) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create obs dir: %w", err)
	}
	r := &Recorder{dir: dir, counters: map[string]int64{}}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

var (
	defaultOnce sync.Once
	defaultRec  *Recorder
)

// Default returns a process-wide Recorder rooted at DefaultDir, created once.
// Best-effort failure paths (the worker and `zv short`) share it so the
// Recorder mutex serializes their writes within the process. It returns nil if
// the recorder could not be created, so callers must nil-check before use.
func Default() *Recorder {
	defaultOnce.Do(func() {
		if rec, err := New(DefaultDir()); err == nil {
			defaultRec = rec
		}
	})
	return defaultRec
}

// Reset clears all counters and removes the persisted metrics files. The
// journal is left untouched; clear it separately when starting a fresh run.
func (r *Recorder) Reset() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters = map[string]int64{}
	for _, p := range []string{r.metricsJSONPath(), r.MetricsPromPath()} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("reset metrics: %w", err)
		}
	}
	return nil
}

// JournalPath is the newline-delimited JSON error journal.
func (r *Recorder) JournalPath() string { return filepath.Join(r.dir, "journal.jsonl") }

// MetricsPromPath is the Prometheus text exposition snapshot.
func (r *Recorder) MetricsPromPath() string { return filepath.Join(r.dir, "metrics.prom") }

func (r *Recorder) metricsJSONPath() string { return filepath.Join(r.dir, "metrics.json") }

// RecordSuccess counts one successful run of stage.
func (r *Recorder) RecordSuccess(stage string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inc(metricStageRuns, map[string]string{"stage": stage, "result": "ok"})
	return r.flushLocked()
}

// RecordError counts one failed run of ev.Stage, increments the error counter
// for (stage, class), and appends ev to the journal. A zero ev.Time is set to
// the current time.
func (r *Recorder) RecordError(ev Event) error {
	if ev.Time.IsZero() {
		ev.Time = time.Now().UTC()
	}
	if ev.Stage == "" {
		ev.Stage = "unknown"
	}
	if ev.Class == "" {
		ev.Class = "unknown"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inc(metricStageRuns, map[string]string{"stage": ev.Stage, "result": "error"})
	r.inc(metricErrors, map[string]string{"stage": ev.Stage, "class": ev.Class})
	if err := r.appendJournal(ev); err != nil {
		return err
	}
	return r.flushLocked()
}

// Metric is a single counter series for export.
type Metric struct {
	Name   string
	Labels map[string]string
	Value  int64
}

// Snapshot returns the current counters sorted by series key.
func (r *Recorder) Snapshot() []Metric {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotLocked()
}

func (r *Recorder) snapshotLocked() []Metric {
	keys := make([]string, 0, len(r.counters))
	for k := range r.counters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Metric, 0, len(keys))
	for _, k := range keys {
		name, labels := parseSeriesKey(k)
		out = append(out, Metric{Name: name, Labels: labels, Value: r.counters[k]})
	}
	return out
}

func (r *Recorder) inc(name string, labels map[string]string) {
	r.counters[seriesKey(name, labels)]++
}

func (r *Recorder) appendJournal(ev Event) error {
	b, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	f, err := os.OpenFile(r.JournalPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open journal: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("append journal: %w", err)
	}
	return nil
}

// flushLocked persists counters as both metrics.json (for reload) and
// metrics.prom (for humans and Prometheus). The caller holds r.mu.
func (r *Recorder) flushLocked() error {
	jb, err := json.MarshalIndent(r.counters, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal counters: %w", err)
	}
	if err := writeFileAtomic(r.metricsJSONPath(), append(jb, '\n')); err != nil {
		return fmt.Errorf("write metrics json: %w", err)
	}
	var b strings.Builder
	WritePrometheus(&b, r.snapshotLocked())
	if err := writeFileAtomic(r.MetricsPromPath(), []byte(b.String())); err != nil {
		return fmt.Errorf("write metrics prom: %w", err)
	}
	return nil
}

// writeFileAtomic writes data to a temp file in the target's directory and
// renames it over path, so a concurrent reader never observes a torn file.
// os.Rename replaces the destination atomically on the platforms FragForge
// targets (including Windows, via MoveFileEx).
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".obs-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func (r *Recorder) load() error {
	b, err := os.ReadFile(r.metricsJSONPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read metrics json: %w", err)
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, &r.counters); err != nil {
		return fmt.Errorf("parse metrics json: %w", err)
	}
	return nil
}

// WritePrometheus renders metrics in the Prometheus text exposition format,
// grouping series by metric name with HELP and TYPE headers.
func WritePrometheus(w io.Writer, metrics []Metric) {
	byName := map[string][]Metric{}
	var names []string
	for _, m := range metrics {
		if _, ok := byName[m.Name]; !ok {
			names = append(names, m.Name)
		}
		byName[m.Name] = append(byName[m.Name], m)
	}
	sort.Strings(names)
	for _, name := range names {
		if help := metricHelp[name]; help != "" {
			fmt.Fprintf(w, "# HELP %s %s\n", name, help)
		}
		fmt.Fprintf(w, "# TYPE %s counter\n", name)
		for _, m := range byName[name] {
			fmt.Fprintf(w, "%s %d\n", seriesKey(m.Name, m.Labels), m.Value)
		}
	}
}

// seriesKey renders "name{label="v",...}" with labels sorted for determinism.
func seriesKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s=%q", k, labels[k])
	}
	b.WriteByte('}')
	return b.String()
}

// parseSeriesKey is the inverse of seriesKey for the values obs itself writes.
func parseSeriesKey(key string) (string, map[string]string) {
	name, rest, ok := strings.Cut(key, "{")
	if !ok {
		return key, nil
	}
	body := strings.TrimSuffix(rest, "}")
	labels := map[string]string{}
	for _, pair := range splitTopLevel(body) {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		labels[k] = strings.Trim(v, `"`)
	}
	return name, labels
}

// splitTopLevel splits on commas that are not inside quotes.
func splitTopLevel(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"':
			inQuote = !inQuote
			cur.WriteByte(c)
		case c == ',' && !inQuote:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
