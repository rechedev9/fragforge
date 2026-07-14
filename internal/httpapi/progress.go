package httpapi

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/storage"
)

// captureProgressView reports how far a capturing job has advanced: how many of
// its selected segments already have a completed clip on disk. It is attached to
// the job GET response only while it can be computed (see captureProgress).
type captureProgressView struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// captureProgress derives capture progress for a recording job from durable
// state alone, so the web poll can render a real percent without a side channel.
// Progress is scoped to the in-flight reel: the record worker persists the
// ordered segment ids it will capture (the capture-selection artifact), so total
// is that reel's segment count and done counts only the reel's completed clips,
// ignoring stale clips a previous reel of the same job left behind. When no
// selection artifact exists (an older job recorded before this was added), it
// falls back to the whole kill plan and counts every clip under the segments dir.
//
// It reports ok=false - so the caller omits progress and the card keeps its
// existing rendering - whenever progress is not meaningful: the job is not
// capturing, neither a current capture selection nor a kill plan is available,
// the storage backend cannot list a directory, or no completed segment exists
// yet (the segments dir is still absent/empty).
// A segment mid-write is briefly counted or missed; the poll tolerates that.
func captureProgress(store storage.Storage, j job.Job) (captureProgressView, bool) {
	total := 0
	if j.KillPlan != nil {
		total = len(j.KillPlan.Segments)
	}
	return captureProgressWithTotal(store, j.ID, j.Status, total)
}

func captureProgressWithTotal(store storage.Storage, id uuid.UUID, status job.Status, fallbackTotal int) (captureProgressView, bool) {
	if status != job.StatusRecording {
		return captureProgressView{}, false
	}
	// Resolve the segments directory from the same key builder the recorder
	// writes through, so the two never drift on the on-disk layout.
	ref, err := artifacts.SegmentClipKey(id, artifactNamePlaceholder)
	if err != nil {
		return captureProgressView{}, false
	}
	files, ok := listArtifactDir(store, ref)
	if !ok {
		return captureProgressView{}, false
	}

	selection, hasSelection, err := readCaptureSelection(store, id)
	if err != nil {
		return captureProgressView{}, false
	}

	total := 0
	done := 0
	if hasSelection {
		total = len(selection)
		inSelection := make(map[string]bool, len(selection))
		for _, id := range selection {
			inSelection[id] = true
		}
		for _, f := range files {
			if strings.EqualFold(path.Ext(f), ".mp4") && inSelection[strings.TrimSuffix(f, path.Ext(f))] {
				done++
			}
		}
	} else {
		if fallbackTotal == 0 {
			return captureProgressView{}, false
		}
		total = fallbackTotal
		for _, f := range files {
			if strings.EqualFold(path.Ext(f), ".mp4") {
				done++
			}
		}
	}
	if total == 0 || done == 0 {
		return captureProgressView{}, false
	}
	if done > total {
		done = total
	}
	return captureProgressView{Done: done, Total: total}, true
}

// readCaptureSelection reads the ordered segment ids the in-flight record run
// will capture. A missing artifact (an older job) is not an error: hasSelection
// is false and the caller falls back to the whole kill plan.
func readCaptureSelection(store storage.Storage, id uuid.UUID) (ids []string, hasSelection bool, err error) {
	rc, err := store.Open(artifacts.CaptureSelectionKey(id))
	if err != nil {
		if storage.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	defer rc.Close()
	if err := json.NewDecoder(rc).Decode(&ids); err != nil {
		return nil, false, err
	}
	return ids, true, nil
}
