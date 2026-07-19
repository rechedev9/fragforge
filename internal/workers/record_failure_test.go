package workers

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/job"
	"github.com/rechedev9/fragforge/internal/recording"
	"github.com/rechedev9/fragforge/internal/rules"
)

// observedRecorderError is the real multi-line recorder failure seen when CS2
// cannot replay a demo recorded on an older build.
const observedRecorderError = `C:\...\zv-recorder.exe failed: exit status 1: 2026/07/19 13:44:07 windowed capture: patched c:\program files (x86)\steam\userdata\50084006\730\local\cfg\cs2_video.txt (fullscreen/borderless off for this run)
error: cs2 demo playback failed with NETWORK_DISCONNECT_MESSAGE_PARSE_ERROR; check console log "..."`

func TestRecordFailureReason(t *testing.T) {
	segment := recording.RecordingArtifact{SegmentID: "seg-001", Role: "segment", Type: "video", Path: "segments/seg-001.mp4"}
	requested16 := make([]string, 16)
	for i := range requested16 {
		requested16[i] = "seg"
	}

	cases := []struct {
		name      string
		err       error
		result    recording.RecordingResult
		requested []string
		want      string
	}{
		{
			name:      "incompatible demo with captured segments",
			err:       errors.New(observedRecorderError),
			result:    recording.RecordingResult{Artifacts: []recording.RecordingArtifact{segment}},
			requested: requested16,
			want:      "demo_incompatible: cs2 cannot replay this demo (it was recorded on an older cs2 build); captured 1/16 segments before the failure",
		},
		{
			name:      "incompatible demo with no captured segments",
			err:       errors.New(observedRecorderError),
			result:    recording.RecordingResult{},
			requested: requested16,
			want:      "demo_incompatible: cs2 cannot replay this demo (it was recorded on an older cs2 build)",
		},
		{
			name:      "generic failure uses last error line",
			err:       errors.New("zv-recorder.exe failed: exit status 1: some noise\nerror: first problem\nmore noise\nerror: hlae launch failed"),
			requested: []string{"seg-001"},
			want:      "recorder failed: hlae launch failed",
		},
		{
			name:      "no marker and no error line passes through unchanged",
			err:       errors.New("zv-recorder.exe failed: exit status 1: unexpected log noise only"),
			requested: []string{"seg-001"},
			want:      "zv-recorder.exe failed: exit status 1: unexpected log noise only",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := recordFailureReason(tc.err, tc.result, tc.requested); got != tc.want {
				t.Fatalf("recordFailureReason() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNewRecordFailureUnwrapsOriginal(t *testing.T) {
	orig := errors.New(observedRecorderError)
	failure := newRecordFailure(orig, recording.RecordingResult{}, []string{"seg-001"})

	if got := failure.Error(); !strings.HasPrefix(got, demoIncompatiblePrefix) {
		t.Fatalf("Error() = %q, want prefix %q", got, demoIncompatiblePrefix)
	}
	if !errors.Is(failure, orig) {
		t.Fatalf("errors.Is(failure, orig) = false, want the original error reachable via Unwrap")
	}
}

func TestRecordWorkerFailsWithConciseIncompatibleReason(t *testing.T) {
	repo := newFakeRepo()
	store := newFakeStorage()
	id := uuid.New()
	plan := minimalKillPlan()
	repo.jobs[id] = &job.Job{
		ID:       id,
		Status:   job.StatusParsed,
		DemoPath: "demos/test.dem",
		Rules:    rules.Default(),
		KillPlan: &plan,
	}
	_ = store.Put("demos/test.dem", bytes.NewReader([]byte("demo")))

	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		outDir := argValue(args, "--out")
		scriptPath := filepath.Join(outDir, "recording.js")
		segmentPath := filepath.Join(outDir, "segments", "seg-001.mp4")
		if err := os.MkdirAll(filepath.Dir(segmentPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(scriptPath, []byte("script"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(segmentPath, []byte("clip"), 0o644); err != nil {
			t.Fatal(err)
		}
		result := recordingResultWithSegment(scriptPath, segmentPath)
		if err := writeJSONFile(filepath.Join(outDir, "recording-result.json"), result); err != nil {
			t.Fatal(err)
		}
		return []byte(observedRecorderError), errors.New(observedRecorderError)
	}}
	w := NewRecordWorker(repo, store, RecordWorkerConfig{
		WorkDir:      t.TempDir(),
		RecorderPath: "zv-recorder",
		HLAEPath:     "HLAE.exe",
		CS2Path:      "cs2.exe",
	})
	w.runner = runner

	err := w.HandleRecordDemo(context.Background(), recordTask(t, id))
	if err == nil {
		t.Fatal("HandleRecordDemo error = nil, want failure")
	}

	got := repo.jobs[id]
	if got.Status != job.StatusFailed {
		t.Fatalf("Status = %s, want failed", got.Status)
	}
	if !strings.HasPrefix(got.FailureReason, demoIncompatiblePrefix) {
		t.Fatalf("FailureReason = %q, want prefix %q", got.FailureReason, demoIncompatiblePrefix)
	}
}
