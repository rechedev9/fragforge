package httpapi

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/artifacts"
	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/storage"
)

// segmentPlan builds a kill plan with n segments (s1..sn) for progress tests.
func segmentPlan(n int) *killplan.Plan {
	plan := &killplan.Plan{}
	for i := 1; i <= n; i++ {
		plan.Segments = append(plan.Segments, killplan.Segment{ID: "s" + string(rune('0'+i))})
	}
	return plan
}

// writeSegmentClips writes size-1 MP4 blobs for the given segment ids so the
// dir listing sees completed clips, mirroring what the recorder uploads.
func writeSegmentClips(t *testing.T, store storage.Storage, id uuid.UUID, segmentIDs ...string) {
	t.Helper()
	for _, sid := range segmentIDs {
		key, err := artifacts.SegmentClipKey(id, sid)
		if err != nil {
			t.Fatalf("SegmentClipKey(%q): %v", sid, err)
		}
		if err := store.Put(key, bytes.NewReader([]byte("x"))); err != nil {
			t.Fatalf("put segment clip %q: %v", sid, err)
		}
	}
}

// writeCaptureSelection persists the reel's segment-id selection, mirroring what
// the record worker writes at the start of a capture.
func writeCaptureSelection(t *testing.T, store storage.Storage, id uuid.UUID, segmentIDs []string) {
	t.Helper()
	b, err := json.Marshal(segmentIDs)
	if err != nil {
		t.Fatalf("marshal selection: %v", err)
	}
	if err := store.Put(artifacts.CaptureSelectionKey(id), bytes.NewReader(b)); err != nil {
		t.Fatalf("put capture selection: %v", err)
	}
}

func TestCaptureProgress(t *testing.T) {
	tests := []struct {
		name      string
		status    job.Status
		plan      *killplan.Plan
		clips     []string
		selection []string // nil = no selection artifact (fall back to full plan)
		wantOK    bool
		wantDone  int
		wantTotal int
	}{
		{
			name:      "two of four recorded",
			status:    job.StatusRecording,
			plan:      segmentPlan(4),
			clips:     []string{"s1", "s2"},
			wantOK:    true,
			wantDone:  2,
			wantTotal: 4,
		},
		{
			name:   "no segments dir yet omits progress",
			status: job.StatusRecording,
			plan:   segmentPlan(4),
			clips:  nil,
			wantOK: false,
		},
		{
			name:   "not recording omits progress",
			status: job.StatusRecorded,
			plan:   segmentPlan(4),
			clips:  []string{"s1", "s2"},
			wantOK: false,
		},
		{
			name:   "no kill plan omits progress",
			status: job.StatusRecording,
			plan:   nil,
			clips:  nil,
			wantOK: false,
		},
		{
			name:      "extra clips clamp done to total",
			status:    job.StatusRecording,
			plan:      segmentPlan(2),
			clips:     []string{"s1", "s2", "s3"},
			wantOK:    true,
			wantDone:  2,
			wantTotal: 2,
		},
		{
			name:      "all recorded reports full",
			status:    job.StatusRecording,
			plan:      segmentPlan(3),
			clips:     []string{"s1", "s2", "s3"},
			wantOK:    true,
			wantDone:  3,
			wantTotal: 3,
		},
		{
			// The reel selects s2,s3 out of a 4-segment plan; s1 is a stale clip
			// from a previous reel and must not be counted, and total is the
			// selection size (2), not the plan size (4).
			name:      "selection scopes total and ignores stale clips",
			status:    job.StatusRecording,
			plan:      segmentPlan(4),
			clips:     []string{"s1", "s2"},
			selection: []string{"s2", "s3"},
			wantOK:    true,
			wantDone:  1,
			wantTotal: 2,
		},
		{
			name:      "selection fully recorded reports full",
			status:    job.StatusRecording,
			plan:      segmentPlan(4),
			clips:     []string{"s2", "s3"},
			selection: []string{"s2", "s3"},
			wantOK:    true,
			wantDone:  2,
			wantTotal: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := storage.NewLocal(t.TempDir())
			if err != nil {
				t.Fatalf("NewLocal: %v", err)
			}
			jobID := uuid.New()
			if tt.clips != nil {
				writeSegmentClips(t, store, jobID, tt.clips...)
			}
			if tt.selection != nil {
				writeCaptureSelection(t, store, jobID, tt.selection)
			}
			j := job.Job{ID: jobID, Status: tt.status, KillPlan: tt.plan}

			got, ok := captureProgress(store, j)
			if ok != tt.wantOK {
				t.Fatalf("captureProgress ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got.Done != tt.wantDone {
				t.Errorf("done = %d, want %d", got.Done, tt.wantDone)
			}
			if got.Total != tt.wantTotal {
				t.Errorf("total = %d, want %d", got.Total, tt.wantTotal)
			}
		})
	}
}
