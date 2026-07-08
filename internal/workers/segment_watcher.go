package workers

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/storage"
)

// segmentWatchInterval is how often the record worker scans the recorder's
// segments dir for newly finished clips during a capture. One dir listing per
// tick, so the cost is negligible next to a minutes-long HLAE run.
const segmentWatchInterval = 2 * time.Second

// segmentClipWatcher uploads each finished segment MP4 from the recorder's
// work dir to its durable storage key while the recorder subprocess is still
// running, so GET /api/jobs/{id} reports live capture progress instead of
// jumping from nothing to done. Strictly best-effort: it never fails the
// record task, and the post-run uploadRecordingOutputs batch re-uploads every
// clip, overwriting whatever the watcher wrote — the batch stays the source of
// truth and reconciles anything the watcher missed or half-read.
type segmentClipWatcher struct {
	store storage.Storage
	jobID uuid.UUID
	dir   string
	// sizes holds each clip's size at the previous tick; a clip uploads only
	// once its non-zero size is unchanged across two consecutive ticks. The
	// recorder publishes mid-run clips atomically (temp name + rename), and this
	// two-tick guard additionally covers writers that write in place (the fake
	// recorder, the recorder's end-of-run mux), so a still-growing file is never
	// counted as done.
	sizes map[string]int64
	// uploaded marks clips already pushed so a later tick never re-uploads them.
	uploaded map[string]bool
}

func newSegmentClipWatcher(store storage.Storage, jobID uuid.UUID, dir string) *segmentClipWatcher {
	return &segmentClipWatcher{
		store:    store,
		jobID:    jobID,
		dir:      dir,
		sizes:    map[string]int64{},
		uploaded: map[string]bool{},
	}
}

// watch ticks until ctx is cancelled. Its owner is RecordWorker.record, which
// cancels it as soon as the recorder subprocess exits and waits for it to stop,
// so the goroutine never outlives its task.
func (w *segmentClipWatcher) watch(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick()
		}
	}
}

// tick scans the segments dir once and uploads every stable, not-yet-uploaded
// clip. A missing dir means no segment has finished yet; upload errors are
// logged and retried next tick. Progress is observed by the job poll counting
// the uploaded clips, so tick has nothing to return.
func (w *segmentClipWatcher) tick() {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return // dir appears with the first finished segment
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.EqualFold(filepath.Ext(name), ".mp4") {
			continue
		}
		segmentID := strings.TrimSuffix(name, filepath.Ext(name))
		if w.uploaded[segmentID] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		size := info.Size()
		previous, seen := w.sizes[name]
		w.sizes[name] = size
		if size == 0 || !seen || previous != size {
			continue // first sighting or still growing; re-check next tick
		}
		key, err := artifacts.SegmentClipKey(w.jobID, segmentID)
		if err != nil {
			w.uploaded[segmentID] = true // invalid id can never become uploadable
			continue
		}
		if err := uploadFile(w.store, key, filepath.Join(w.dir, name)); err != nil {
			logWorkerError(w.jobID, "upload segment clip "+segmentID, err)
			continue
		}
		w.uploaded[segmentID] = true
	}
}
