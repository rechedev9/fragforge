package httpapi

import (
	"path"
	"strings"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/storage"
)

// captureProgressView reports how far a capturing job has advanced: how many of
// its selected segments already have a completed clip on disk. It is attached to
// the job GET response only while it can be computed (see captureProgress).
type captureProgressView struct {
	Stage string `json:"stage"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

// captureProgress derives capture progress for a recording job from durable
// state alone, so the web poll can render a real percent without a side channel.
// Total is the number of segments the job's kill plan holds (the all-kills reel
// is the product default); done is the count of segment MP4s the recorder has
// written under the job's recording/segments dir, clamped to total.
//
// It reports ok=false - so the caller omits progress and the card keeps its
// existing rendering - whenever progress is not meaningful: the job is not
// capturing, it has no kill plan, the storage backend cannot list a directory,
// or no completed segment exists yet (the segments dir is still absent/empty).
// A segment mid-write is briefly counted or missed; the poll tolerates that.
func captureProgress(store storage.Storage, j job.Job) (captureProgressView, bool) {
	if j.Status != job.StatusRecording || j.KillPlan == nil {
		return captureProgressView{}, false
	}
	total := len(j.KillPlan.Segments)
	if total == 0 {
		return captureProgressView{}, false
	}
	lister, ok := store.(renderArtifactLister)
	if !ok {
		return captureProgressView{}, false
	}
	// Resolve the segments directory from the same key builder the recorder
	// writes through, so the two never drift on the on-disk layout.
	ref, err := artifacts.SegmentClipKey(j.ID, artifactNamePlaceholder)
	if err != nil {
		return captureProgressView{}, false
	}
	files, err := lister.List(path.Dir(ref))
	if err != nil {
		return captureProgressView{}, false
	}
	done := 0
	for _, f := range files {
		if strings.EqualFold(path.Ext(f), ".mp4") {
			done++
		}
	}
	if done == 0 {
		return captureProgressView{}, false
	}
	if done > total {
		done = total
	}
	return captureProgressView{Stage: "recording", Done: done, Total: total}, true
}
