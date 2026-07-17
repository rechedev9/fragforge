package streamcli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

// TestStreamJourneyChainsStagesMediaFree drives the stream/VOD journey through
// the in-process command layer via runStreamWithService + fakeStreamService: a
// plan preflight, the persisted plan, the reviewed killfeed and Spanish caption
// imports, and the render dry-run. Each hop asserts the {ok, dry_run, executed}
// envelope and that the --out document one stage writes is the literal --plan
// the next stage consumes. It stays media-free (no ffmpeg/ffprobe) because the
// service seam fakes probe/ffmpeg/render, so it always runs. Reviewed killfeed
// events and caption words come from the shared testdata fixtures; from this
// package they live at ../../testdata.
func TestStreamJourneyChainsStagesMediaFree(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")

	ws := t.TempDir()
	events := filepath.Join("..", "..", "testdata", "stream-killfeed-events.json")
	words := filepath.Join("..", "..", "testdata", "stream-caption-words.json")

	editPlan := filepath.Join(ws, "edit-plan.json")
	reviewedPlan := filepath.Join(ws, "reviewed-plan.json")
	finalPlan := filepath.Join(ws, "final-plan.json")

	// The reviewed killfeed fixture confirms cues at 2.75s and 8.625s on
	// clip-001, so the persisted plan must detect those exact cues or the import
	// rejects them as drift.
	planProbe := streamclips.SourceProbe{Width: 1920, Height: 1080, DurationSeconds: 15, VideoCodec: "h264", AudioCodec: "aac"}

	// 1. plan --dry-run validates the clip/crop/caption contract without writing.
	stdout := runStream(t, &fakeStreamService{probe: planProbe},
		"plan", "--input", "stream.mp4", "--out", editPlan,
		"--killfeed-crop", "0.82,0.05,0.17,0.18", "--detect-killfeed",
		"--dry-run", "--format", "json")
	var planDry streamPlanResult
	decodeStreamJSON(t, "stream plan preflight", stdout, &planDry)
	if !planDry.OK || !planDry.DryRun || planDry.Executed {
		t.Fatalf("plan preflight envelope = %#v, want a successful dry run", planDry)
	}
	assertStreamPathMissing(t, editPlan)

	// 2. plan persist writes the edit plan clip-001 carries the detected cues.
	stdout = runStream(t, &fakeStreamService{probe: planProbe, detectedCues: []float64{2.75, 8.625}},
		"plan", "--input", "stream.mp4", "--out", editPlan,
		"--killfeed-crop", "0.82,0.05,0.17,0.18", "--detect-killfeed", "--format", "json")
	var planPersist streamPlanResult
	decodeStreamJSON(t, "stream plan", stdout, &planPersist)
	if !planPersist.OK || planPersist.DryRun || !planPersist.Executed {
		t.Fatalf("plan envelope = %#v, want executed", planPersist)
	}
	assertStreamFileExists(t, editPlan)

	// 3. killfeed import consumes the persisted plan and persists the reviewed
	// factual events; its --plan is the plan step's --out.
	stdout = runStream(t, &fakeStreamService{},
		"killfeed", "--plan", editPlan, "--events", events, "--out", reviewedPlan, "--format", "json")
	var killfeed streamKillfeedResult
	decodeStreamJSON(t, "stream killfeed", stdout, &killfeed)
	if !killfeed.OK || killfeed.DryRun || !killfeed.Executed {
		t.Fatalf("killfeed envelope = %#v, want executed", killfeed)
	}
	if killfeed.CueCount != 2 || killfeed.KillCount != 3 {
		t.Fatalf("killfeed counts = %#v, want 2 cues / 3 kills from the fixture", killfeed)
	}
	assertStreamFileExists(t, reviewedPlan)

	// 4. captions import consumes the reviewed plan and persists the Spanish
	// caption timings; its --plan is the killfeed step's --out.
	stdout = runStream(t, &fakeStreamService{},
		"captions", "--plan", reviewedPlan, "--words", words, "--out", finalPlan, "--format", "json")
	var captions streamCaptionsResult
	decodeStreamJSON(t, "stream captions", stdout, &captions)
	if !captions.OK || captions.DryRun || !captions.Executed {
		t.Fatalf("captions envelope = %#v, want executed", captions)
	}
	if captions.WordCount != 2 || captions.Language != "es" {
		t.Fatalf("captions result = %#v, want 2 reviewed Spanish words", captions)
	}
	assertStreamFileExists(t, finalPlan)

	// 5. render --dry-run consumes the final plan; its --plan is the captions
	// step's --out. Reviewed captions make it credential-free.
	renderDir := filepath.Join(ws, "render")
	stdout = runStream(t, &fakeStreamService{probe: planProbe},
		"render", "--input", "stream.mp4", "--plan", finalPlan, "--out", renderDir, "--dry-run", "--format", "json")
	var render streamRenderResult
	decodeStreamJSON(t, "stream render", stdout, &render)
	if !render.OK || !render.DryRun || render.Executed {
		t.Fatalf("render envelope = %#v, want a successful dry run", render)
	}
}

func runStream(t *testing.T, service streamService, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runStreamWithService(args, &stdout, &stderr, service)
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("stream %v: code = %d, stderr = %q\nstdout: %s", args, code, stderr.String(), stdout.String())
	}
	return stdout.String()
}

func decodeStreamJSON(t *testing.T, source, stdout string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(stdout), into); err != nil {
		t.Fatalf("%s: decode stdout: %v\n%s", source, err, stdout)
	}
}

func assertStreamFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected chained artifact %s: %v", path, err)
	}
}

func assertStreamPathMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("path %s stat error = %v, want not exist", path, err)
	}
}
