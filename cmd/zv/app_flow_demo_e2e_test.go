package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestDemoJourneyChainsStagesMediaFree drives the demo production journey
// through the REAL delegated binaries at the media-free level: parse --dry-run,
// moments, select preflight then persist, record --dry-run, and shorts render
// --dry-run. Each hop asserts the {ok, dry_run, executed} envelope and that the
// artifact one stage writes is the literal input the next stage consumes. A
// second tier records real placeholder segments with ZV_RECORDER_FAKE and runs
// the render/compose dry runs over them; it is skipped when ffmpeg is absent.
func TestDemoJourneyChainsStagesMediaFree(t *testing.T) {
	t.Parallel()
	exe := buildDelegatedBinaries(t)

	ws := t.TempDir()
	// The shared kill plan fixture is the first Go consumer's input; moments and
	// select both read it, so its absolute path is the chain's anchor.
	killPlan := absPath(t, filepath.Join(repoRoot(t), "testdata", "agent-killplan.json"))
	demo := filepath.Join(ws, "match.dem")
	if err := os.WriteFile(demo, []byte("dummy demo"), 0o600); err != nil {
		t.Fatalf("write demo fixture: %v", err)
	}

	// 1. parse --dry-run validates the demo, target, and --out path without
	// writing the kill plan, and still emits the envelope.
	parseOut := filepath.Join(ws, "parse", "killplan.json")
	stdout, _ := runZVBinarySplit(t, exe, ws, "demo", "parse", "--demo", demo, "--steamid", "76561198377256168", "--out", parseOut, "--dry-run")
	assertDryRunEnvelope(t, "demo parse", stdout)
	assertPathDoesNotExist(t, parseOut)

	// 2. moments ranks candidates from the fixture and writes moments.json. That
	// file is a REVIEW artifact, not a chain input: the next stage (select) reads
	// the same kill plan fixture, not moments.json. Only its input path (the kill
	// plan) is part of the chain.
	momentsOut := filepath.Join(ws, "moments.json")
	stdout, _ = runZVBinarySplit(t, exe, ws, "demo", "moments", "--killplan", killPlan, "--out", momentsOut, "--format", "json")
	var moments demoMomentsResult
	decodeJSON(t, "demo moments", stdout, &moments)
	if !moments.OK || moments.DryRun || !moments.Executed {
		t.Fatalf("moments envelope = %#v, want executed", moments)
	}
	if moments.Input != killPlan {
		t.Fatalf("moments input = %q, want the kill plan fixture %q", moments.Input, killPlan)
	}
	assertFileExists(t, momentsOut)

	// 3a. select preflight validates the ordered selection without writing.
	selectedOut := filepath.Join(ws, "selected-plan.json")
	stdout, _ = runZVBinarySplit(t, exe, ws, "demo", "select", "--killplan", killPlan, "--segments", "seg-001", "--out", selectedOut, "--dry-run", "--format", "json")
	assertDryRunEnvelope(t, "demo select preflight", stdout)
	assertPathDoesNotExist(t, selectedOut)

	// 3b. select persist writes the recorder-ready plan; its input is the same
	// fixture moments consumed, its output is the plan record consumes next.
	stdout, _ = runZVBinarySplit(t, exe, ws, "demo", "select", "--killplan", killPlan, "--segments", "seg-001", "--out", selectedOut, "--format", "json")
	var selection demoSelectionResult
	decodeJSON(t, "demo select", stdout, &selection)
	if !selection.OK || selection.DryRun || !selection.Executed {
		t.Fatalf("select envelope = %#v, want executed", selection)
	}
	if selection.Input != killPlan {
		t.Fatalf("select input = %q, want %q (same fixture moments read)", selection.Input, killPlan)
	}
	assertFileExists(t, selection.Output)
	// select persisted to the path we asked for; record consumes exactly this
	// file next (path identity, not content equality).
	if selection.Output != selectedOut {
		t.Fatalf("select output = %q, want %q", selection.Output, selectedOut)
	}

	// 4. record --dry-run consumes the persisted selection (--killplan is exactly
	// selection.Output) and writes the capture contract plus recording-result.json
	// without launching HLAE/CS2.
	recordingDir := filepath.Join(ws, "recording")
	stdout, _ = runZVBinarySplit(t, exe, ws, "record", "--killplan", selection.Output, "--demo", demo, "--out", recordingDir, "--dry-run", "--format", "json")
	var record recordEnvelope
	decodeJSON(t, "record", stdout, &record)
	if !record.OK || !record.DryRun || record.Executed {
		t.Fatalf("record envelope = %#v, want a successful dry run", record)
	}
	assertFileExists(t, record.ResultPath)
	if record.ResultPath != filepath.Join(recordingDir, "recording-result.json") {
		t.Fatalf("record result path = %q, want %q", record.ResultPath, filepath.Join(recordingDir, "recording-result.json"))
	}

	// 5. shorts render --dry-run consumes the recording result record produced
	// (--recording-result is exactly record.ResultPath) and the selected plan.
	// The editor accepts an empty-artifacts recording result at the media-free
	// level, so the chain completes without any captured media.
	renderDir := filepath.Join(ws, "render")
	publishDir := filepath.Join(ws, "shortslistosparasubir")
	stdout, _ = runZVBinarySplit(t, exe, ws, "shorts", "render",
		"--recording-result", record.ResultPath, "--killplan", selection.Output,
		"--out", renderDir, "--publish-dir", publishDir, "--dry-run", "--format", "json")
	assertDryRunEnvelope(t, "shorts render", stdout)

	t.Run("ffmpeg placeholder tier", func(t *testing.T) {
		ffmpeg, err := exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("ffmpeg not found; skipping placeholder-segment render tier")
		}
		// LookPath is not a capability probe: synthesize a tiny clip first so a
		// build of ffmpeg without libx264/lavfi/drawbox skips (capability missing)
		// via runSyntheticFFmpeg instead of failing the real fake-record stage.
		generateSyntheticSource(t, ffmpeg, filepath.Join(ws, "ffmpeg-probe.mp4"))
		// Record real placeholder segments (no CS2/HLAE) so the render/compose
		// dry runs operate over genuine on-disk MP4 artifacts, not empty ones.
		fakeRecordingDir := filepath.Join(ws, "recording-fake")
		out, _ := runZVBinarySplitWithEnv(t, exe, ws, []string{"ZV_RECORDER_FAKE=1"},
			"record", "--killplan", selection.Output, "--demo", demo, "--out", fakeRecordingDir, "--format", "json")
		var fake recordEnvelope
		decodeJSON(t, "record fake", out, &fake)
		if !fake.OK || fake.DryRun || !fake.Executed {
			t.Fatalf("fake record envelope = %#v, want executed", fake)
		}
		if fake.ArtifactCount == 0 {
			t.Fatalf("fake record artifact count = 0, want placeholder segments: %s", out)
		}
		assertFileExists(t, fake.ResultPath)

		renderRealDir := filepath.Join(ws, "render-real")
		publishRealDir := filepath.Join(ws, "shortslistosparasubir-real")
		out, _ = runZVBinarySplit(t, exe, ws, "shorts", "render",
			"--recording-result", fake.ResultPath, "--killplan", selection.Output,
			"--out", renderRealDir, "--publish-dir", publishRealDir, "--dry-run", "--format", "json")
		assertDryRunEnvelope(t, "shorts render over placeholder segments", out)

		finalOut := filepath.Join(ws, "final.mp4")
		out, _ = runZVBinarySplit(t, exe, ws, "compose", "final",
			"--recording-result", fake.ResultPath, "--out", finalOut, "--dry-run", "--format", "json")
		assertDryRunEnvelope(t, "compose final over placeholder segments", out)
	})
}

// recordEnvelope mirrors the zv-recorder JSON summary fields this suite chains on.
type recordEnvelope struct {
	OK            bool   `json:"ok"`
	DryRun        bool   `json:"dry_run"`
	Executed      bool   `json:"executed"`
	ResultPath    string `json:"result_path"`
	ArtifactCount int    `json:"artifact_count"`
}

func decodeJSON(t *testing.T, source, stdout string, into any) {
	t.Helper()
	if err := json.Unmarshal([]byte(stdout), into); err != nil {
		t.Fatalf("%s: decode stdout: %v\n%s", source, err, stdout)
	}
}

func absPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("abs %s: %v", path, err)
	}
	return abs
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
}
