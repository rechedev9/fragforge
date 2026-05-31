# Zack Replay Shorts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce one 24 FPS, music-synced, Lua-enhanced Short per Zack demo, including all Zack kills from each demo.

**Architecture:** Extend `internal/editor` so Shorts can opt into output FPS, external music, rhythm JSON validation, and one-per-demo compilation mode. Keep `cmd/zv-editor` thin by adding flags that pass through to `editor.Config`; keep production commands as a repeatable local workflow under `data/runs/` without committing media outputs.

**Tech Stack:** Go 1.26, FFmpeg argument slices, existing `internal/rhythm` JSON schema, existing Lua effects engine, HLAE/CS2 capture via existing recorder.

**Spec:** `docs/superpowers/specs/2026-05-29-zack-replay-shorts-design.md`

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/editor/types.go` | Add config, manifest, and short metadata fields for `music_path`, `rhythm_path`, `output_fps`, and compilation parts. |
| `internal/editor/run.go` | Resolve and validate new config paths; preserve defaults when fields are omitted. |
| `internal/editor/manifest.go` | Copy new controls into manifests and build one compilation short when enabled. |
| `internal/editor/filter.go` | Replace hard-coded `fps=60` with a helper that uses `ShortEdit.OutputFPS`. |
| `internal/editor/ffmpeg.go` | Build FFmpeg commands for music-forward audio and compilation shorts. |
| `internal/editor/rhythm_sync.go` | Load and validate rhythm JSON, then map `segment_sync` by segment ID. |
| `internal/editor/manifest_test.go` | Regression tests for config propagation, FPS, music FFmpeg args, rhythm validation, and compilation manifest shape. |
| `cmd/zv-editor/main.go` | Add CLI flags: `--music`, `--rhythm`, `--fps`, `--compile-segments`. |
| `cmd/zv/command_validation.go` | Allow the new `zv shorts render` value flags and bool flag. |
| `cmd/zv/app_workflows_e2e_test.go` | Extend wrapper validation coverage for the new flags. |
| `docs/superpowers/specs/2026-05-29-zack-replay-shorts-design.md` | Already written; no implementation edits expected. |

Conventions to preserve:

- FFmpeg commands remain `[]string`, never shell-concatenated.
- Defaults must keep current behavior when new flags are omitted.
- Unit tests must not invoke HLAE, CS2, or long FFmpeg renders.
- Generated audio/video artifacts stay under `data/` and are not committed.

---

### Task 1: Add Editor Config And Manifest Controls

**Files:**
- Modify: `internal/editor/types.go`
- Modify: `internal/editor/run.go`
- Modify: `internal/editor/manifest.go`
- Test: `internal/editor/manifest_test.go`

- [ ] **Step 1: Write the failing test**

Add this test near `TestBuildManifestUsesVideoEncodingOptions` in `internal/editor/manifest_test.go`:

```go
func TestBuildManifestCarriesMusicRhythmAndFPSOptions(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.MusicPath = filepath.Join(dir, "music", "trap-130.mp3")
	opts.RhythmPath = filepath.Join(dir, "rhythm.json")
	opts.OutputFPS = 24

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.MusicPath != opts.MusicPath {
		t.Fatalf("manifest music path = %q, want %q", manifest.MusicPath, opts.MusicPath)
	}
	if manifest.RhythmPath != opts.RhythmPath {
		t.Fatalf("manifest rhythm path = %q, want %q", manifest.RhythmPath, opts.RhythmPath)
	}
	if manifest.OutputFPS != 24 {
		t.Fatalf("manifest output fps = %d, want 24", manifest.OutputFPS)
	}
	first := manifest.Shorts[0]
	if first.MusicPath != opts.MusicPath || first.RhythmPath != opts.RhythmPath || first.OutputFPS != 24 {
		t.Fatalf("short render controls = %#v", first)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/editor -run TestBuildManifestCarriesMusicRhythmAndFPSOptions -v`

Expected: FAIL with compile errors for missing `MusicPath`, `RhythmPath`, or `OutputFPS` fields.

- [ ] **Step 3: Add the fields and propagation**

In `internal/editor/types.go`, add these fields to `Config` after `VideoPreset string`:

```go
	MusicPath  string
	RhythmPath string
	OutputFPS  int
```

Add the same fields to `ManifestOptions` after `VideoPreset string`:

```go
	MusicPath  string
	RhythmPath string
	OutputFPS  int
```

Add these JSON fields to `Manifest` after `VideoPreset int/string` metadata:

```go
	MusicPath string `json:"music_path,omitempty"`
	RhythmPath string `json:"rhythm_path,omitempty"`
	OutputFPS int    `json:"output_fps,omitempty"`
```

Add these JSON fields to `ShortEdit` after `VideoPreset string`:

```go
	MusicPath string `json:"music_path,omitempty"`
	RhythmPath string `json:"rhythm_path,omitempty"`
	OutputFPS int    `json:"output_fps,omitempty"`
```

In `internal/editor/run.go`, resolve paths after `effectsPath` resolution:

```go
	musicPath := cfg.MusicPath
	if musicPath != "" {
		musicPath, err = filepath.Abs(musicPath)
		if err != nil {
			return Result{}, fmt.Errorf("resolve music path: %w", err)
		}
	}
	rhythmPath := cfg.RhythmPath
	if rhythmPath != "" {
		rhythmPath, err = filepath.Abs(rhythmPath)
		if err != nil {
			return Result{}, fmt.Errorf("resolve rhythm path: %w", err)
		}
	}
```

Pass the resolved values into `ManifestOptions`:

```go
		MusicPath:          musicPath,
		RhythmPath:         rhythmPath,
		OutputFPS:          cfg.OutputFPS,
```

In `Config.validate`, add:

```go
	if c.OutputFPS < 0 {
		return fmt.Errorf("output fps must be >= 0")
	}
	if c.RhythmPath != "" && c.MusicPath == "" {
		return fmt.Errorf("rhythm path requires music path")
	}
```

In `internal/editor/manifest.go`, set manifest metadata inside the `Manifest{}` literal:

```go
		MusicPath:         opts.MusicPath,
		RhythmPath:        opts.RhythmPath,
		OutputFPS:         opts.OutputFPS,
```

Set the same fields inside the `ShortEdit{}` literal:

```go
			MusicPath:         opts.MusicPath,
			RhythmPath:        opts.RhythmPath,
			OutputFPS:         opts.OutputFPS,
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/editor -run TestBuildManifestCarriesMusicRhythmAndFPSOptions -v`

Expected: PASS.

- [ ] **Step 5: Commit the work unit**

```bash
git add internal/editor/types.go internal/editor/run.go internal/editor/manifest.go internal/editor/manifest_test.go
git commit -m "feat(editor): carry music rhythm and fps options"
```

---

### Task 2: Make Output FPS Configurable

**Files:**
- Modify: `internal/editor/filter.go`
- Test: `internal/editor/manifest_test.go`

- [ ] **Step 1: Write the failing tests**

Add these tests after `TestBuildFFmpegCommandKeepsPathsAsArgs`:

```go
func TestBuildFFmpegCommandUsesConfiguredOutputFPS(t *testing.T) {
	short := ShortEdit{Input: "clip.mp4", Output: "short.mp4", OutputFPS: 24}
	command := BuildFFmpegCommand("ffmpeg", short)
	filter := argAfter(command, "-vf")
	if !strings.Contains(filter, "fps=24") {
		t.Fatalf("filter missing fps=24:\n%s", filter)
	}
	if strings.Contains(filter, "fps=60") {
		t.Fatalf("filter still contains fps=60:\n%s", filter)
	}
}

func TestBuildFFmpegCommandKeepsDefaultFPS(t *testing.T) {
	short := ShortEdit{Input: "clip.mp4", Output: "short.mp4"}
	command := BuildFFmpegCommand("ffmpeg", short)
	filter := argAfter(command, "-vf")
	if !strings.Contains(filter, "fps=60") {
		t.Fatalf("filter missing default fps=60:\n%s", filter)
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/editor -run 'TestBuildFFmpegCommand(UsesConfiguredOutputFPS|KeepsDefaultFPS)' -v`

Expected: FAIL because `OutputFPS` is ignored and filters still contain `fps=60`.

- [ ] **Step 3: Implement the FPS helper**

In `internal/editor/filter.go`, add this helper near `VideoFilter`:

```go
func outputFPSFilter(short ShortEdit) string {
	fps := short.OutputFPS
	if fps <= 0 {
		fps = 60
	}
	return fmt.Sprintf("fps=%d", fps)
}
```

Replace each hard-coded `"fps=60"` in `VideoFilter`, `ViralSquareFilter`, `SmokeLineupSlowMotionFilter`, and `PremiumPlayerFilter` with `outputFPSFilter(short)`.

For the smoke concat line, change:

```go
	fmt.Sprintf("%sconcat=n=%d:v=1:a=0,fps=60,format=yuv420p[v]", strings.Join(videoParts, ""), len(videoParts)),
```

to:

```go
	fmt.Sprintf("%sconcat=n=%d:v=1:a=0,%s,format=yuv420p[v]", strings.Join(videoParts, ""), len(videoParts), outputFPSFilter(short)),
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/editor -run 'TestBuildFFmpegCommand(UsesConfiguredOutputFPS|KeepsDefaultFPS)' -v`

Expected: PASS.

- [ ] **Step 5: Commit the work unit**

```bash
git add internal/editor/filter.go internal/editor/manifest_test.go
git commit -m "feat(editor): support configurable output fps"
```

---

### Task 3: Add Music-Forward FFmpeg Audio Mapping

**Files:**
- Modify: `internal/editor/ffmpeg.go`
- Test: `internal/editor/manifest_test.go`

- [ ] **Step 1: Write the failing test**

Add this test after the FPS tests:

```go
func TestBuildFFmpegCommandMixesExternalMusic(t *testing.T) {
	short := ShortEdit{
		Input:     "clip.mp4",
		Output:    "short.mp4",
		MusicPath: "music/trap-130.mp3",
		OutputFPS: 24,
	}
	command := BuildFFmpegCommand("ffmpeg", short)

	if !containsArg(command, "music/trap-130.mp3") {
		t.Fatalf("command missing music input: %#v", command)
	}
	filter := argAfter(command, "-filter_complex")
	for _, want := range []string{
		"[0:v]",
		"fps=24",
		"[0:a]volume=0.20[game]",
		"[1:a]volume=1.00[music]",
		"[game][music]amix=inputs=2:duration=first:dropout_transition=0[a]",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("music filter missing %q:\n%s", want, filter)
		}
	}
	if got := firstMapAfter(command, "[v]"); got != "[v]" {
		t.Fatalf("video map = %q, want [v]", got)
	}
	if !containsMap(command, "[a]") {
		t.Fatalf("command missing [a] map: %#v", command)
	}
}

func firstMapAfter(args []string, want string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-map" && args[i+1] == want {
			return args[i+1]
		}
	}
	return ""
}

func containsMap(args []string, want string) bool {
	return firstMapAfter(args, want) == want
}
```

- [ ] **Step 2: Run the test to verify failure**

Run: `go test ./internal/editor -run TestBuildFFmpegCommandMixesExternalMusic -v`

Expected: FAIL because `MusicPath` is ignored and no `-filter_complex` exists.

- [ ] **Step 3: Implement music command path**

In `internal/editor/ffmpeg.go`, change `BuildFFmpegCommand` so the normal path checks music before building the simple `-vf` command:

```go
	if short.MusicPath != "" {
		return BuildMusicFFmpegCommand(ffmpegPath, short)
	}
```

Add this function below `BuildViralSquareFFmpegCommand`:

```go
func BuildMusicFFmpegCommand(ffmpegPath string, short ShortEdit) []string {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	filter := fmt.Sprintf(
		"[0:v]%s[v];[0:a]volume=0.20[game];[1:a]volume=1.00[music];[game][music]amix=inputs=2:duration=first:dropout_transition=0[a]",
		VideoFilter(short),
	)
	command := []string{
		ffmpegPath,
		"-y",
		"-v", "error",
		"-i", short.Input,
		"-stream_loop", "-1",
		"-i", short.MusicPath,
		"-filter_complex", filter,
		"-map", "[v]",
		"-map", "[a]",
		"-c:v", "libx264",
		"-preset", videoPresetForCommand(short.VideoPreset),
		"-crf", fmt.Sprintf("%d", videoCRFForCommand(short.VideoCRF)),
	}
	command = appendVideoEncodeArgs(command, short)
	command = appendAudioCodecArgs(command)
	return append(command,
		"-movflags", "+faststart",
		"-shortest",
		short.Output,
	)
}
```

- [ ] **Step 4: Run the test to verify pass**

Run: `go test ./internal/editor -run TestBuildFFmpegCommandMixesExternalMusic -v`

Expected: PASS.

- [ ] **Step 5: Commit the work unit**

```bash
git add internal/editor/ffmpeg.go internal/editor/manifest_test.go
git commit -m "feat(editor): mix external music into shorts"
```

---

### Task 4: Load And Validate Rhythm JSON

**Files:**
- Create: `internal/editor/rhythm_sync.go`
- Test: `internal/editor/rhythm_sync_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/editor/rhythm_sync_test.go`:

```go
package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRhythmSyncIndexesSegments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rhythm.json")
	if err := os.WriteFile(path, []byte(`{
		"schema_version": "1.0",
		"segment_sync": [
			{"segment_id":"seg-001","timeline_start_seconds":0.5,"target_kill_time_seconds":1.5},
			{"segment_id":"seg-002","timeline_start_seconds":4.0,"target_kill_time_seconds":5.0}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	sync, err := loadRhythmSync(path)
	if err != nil {
		t.Fatalf("loadRhythmSync returned error: %v", err)
	}
	if got := sync["seg-002"].TimelineStartSeconds; got != 4.0 {
		t.Fatalf("seg-002 timeline start = %.3f, want 4.000", got)
	}
}

func TestLoadRhythmSyncRejectsEmptySegmentSync(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rhythm.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"1.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadRhythmSync(path)
	if err == nil {
		t.Fatal("loadRhythmSync returned nil error, want error")
	}
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/editor -run TestLoadRhythmSync -v`

Expected: FAIL with `undefined: loadRhythmSync`.

- [ ] **Step 3: Implement rhythm sync loader**

Create `internal/editor/rhythm_sync.go`:

```go
package editor

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/reche/zackvideo/internal/rhythm"
)

func loadRhythmSync(path string) (map[string]rhythm.SegmentSync, error) {
	if path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read rhythm json: %w", err)
	}
	var analysis rhythm.Analysis
	if err := json.Unmarshal(b, &analysis); err != nil {
		return nil, fmt.Errorf("decode rhythm json: %w", err)
	}
	if len(analysis.SegmentSync) == 0 {
		return nil, fmt.Errorf("rhythm json has no segment_sync entries")
	}
	indexed := make(map[string]rhythm.SegmentSync, len(analysis.SegmentSync))
	for _, entry := range analysis.SegmentSync {
		if entry.SegmentID == "" {
			return nil, fmt.Errorf("rhythm json contains segment_sync entry without segment_id")
		}
		indexed[entry.SegmentID] = entry
	}
	return indexed, nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/editor -run TestLoadRhythmSync -v`

Expected: PASS.

- [ ] **Step 5: Commit the work unit**

```bash
git add internal/editor/rhythm_sync.go internal/editor/rhythm_sync_test.go
git commit -m "feat(editor): validate rhythm sync input"
```

---

### Task 5: Add Per-Demo Compilation Manifest Mode

**Files:**
- Modify: `internal/editor/types.go`
- Modify: `internal/editor/manifest.go`
- Test: `internal/editor/manifest_test.go`

- [ ] **Step 1: Write the failing test**

Add this test near other manifest construction tests:

```go
func TestBuildManifestCompileSegmentsCreatesOneShort(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.CompileSegments = true
	opts.MusicPath = filepath.Join(dir, "music", "trap-130.mp3")
	opts.OutputFPS = 24

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if !manifest.CompileSegments {
		t.Fatal("manifest compile_segments = false, want true")
	}
	if len(manifest.Shorts) != 1 {
		t.Fatalf("short count = %d, want 1", len(manifest.Shorts))
	}
	short := manifest.Shorts[0]
	if len(short.Parts) != 2 {
		t.Fatalf("short parts = %d, want 2", len(short.Parts))
	}
	if got := short.Parts[0].SegmentID; got != "seg-001" {
		t.Fatalf("part[0] segment = %q, want seg-001", got)
	}
	if got := short.Parts[1].SegmentID; got != "seg-002" {
		t.Fatalf("part[1] segment = %q, want seg-002", got)
	}
	if short.OutputFPS != 24 || short.MusicPath != opts.MusicPath {
		t.Fatalf("compiled short controls = %#v", short)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/editor -run TestBuildManifestCompileSegmentsCreatesOneShort -v`

Expected: FAIL with missing `CompileSegments` and `Parts` fields.

- [ ] **Step 3: Add compilation data types**

In `internal/editor/types.go`, add to `Config` and `ManifestOptions`:

```go
	CompileSegments bool
```

Add to `Manifest`:

```go
	CompileSegments bool `json:"compile_segments,omitempty"`
```

Add to `ShortEdit`:

```go
	Parts []ShortPart `json:"parts,omitempty"`
```

Add this type after `ShortEdit`:

```go
type ShortPart struct {
	SegmentID            string                      `json:"segment_id"`
	Input                string                      `json:"input"`
	SourceArtifact       recording.RecordingArtifact `json:"source_artifact,omitempty"`
	DurationSeconds      float64                     `json:"duration_seconds,omitempty"`
	TimelineStartSeconds float64                     `json:"timeline_start_seconds,omitempty"`
	GapBeforeSeconds     float64                     `json:"gap_before_seconds,omitempty"`
	Kills                []KillCue                   `json:"kills,omitempty"`
}
```

- [ ] **Step 4: Build one compiled short from selected segments**

In `internal/editor/manifest.go`, after segment availability checks and before the existing `for i, segment := range result.Plan.Segments` loop, add:

```go
	if opts.CompileSegments {
		compiled, err := buildCompiledShort(result, opts, clipBySegment, selected, player, mapName, promptDir, logDir, effectsSource, videoCRF, videoPreset, hqFilters, audioNormalize, temporalSmoothing)
		if err != nil {
			return Manifest{Warnings: manifest.Warnings}, err
		}
		manifest.CompileSegments = true
		manifest.Shorts = append(manifest.Shorts, compiled)
		if len(manifest.Shorts) == 0 {
			manifest.Warnings = append(manifest.Warnings, "segment selection produced no shorts")
		}
		if err := applyEffectsToManifest(&manifest, effectsSource, opts.FFmpegPath); err != nil {
			return manifest, err
		}
		return manifest, nil
	}
```

Create `buildCompiledShort` in the same file. It should:

- Iterate `result.Plan.Segments` in order.
- Respect `selected` and `opts.Limit` exactly like the existing loop.
- Use `clipBySegment[segment.ID]` for each part.
- Set `ShortEdit.SegmentID` to `demo-compilation`.
- Set `ShortEdit.Output` to `short-001-demo-compilation.mp4`.
- Set `ShortEdit.PublishPath` to `01_demo-compilation_<player>_<map>_<killcount>k.mp4` through `safeFilenameToken` pieces.
- Append each part's kills using `killCues(segment, result.Plan.Tickrate)`.

- [ ] **Step 5: Run test to verify pass**

Run: `go test ./internal/editor -run TestBuildManifestCompileSegmentsCreatesOneShort -v`

Expected: PASS.

- [ ] **Step 6: Commit the work unit**

```bash
git add internal/editor/types.go internal/editor/manifest.go internal/editor/manifest_test.go
git commit -m "feat(editor): build per-demo compilation manifest"
```

---

### Task 6: Build Compilation FFmpeg Command

**Files:**
- Modify: `internal/editor/ffmpeg.go`
- Test: `internal/editor/manifest_test.go`

- [ ] **Step 1: Write the failing test**

Add this test after `TestBuildManifestCompileSegmentsCreatesOneShort`:

```go
func TestBuildFFmpegCommandForCompilationShort(t *testing.T) {
	short := ShortEdit{
		Output:    "compiled.mp4",
		MusicPath: "music/trap-130.mp3",
		OutputFPS: 24,
		Parts: []ShortPart{
			{SegmentID: "seg-001", Input: "seg-001.mp4", DurationSeconds: 4.0, GapBeforeSeconds: 0.5},
			{SegmentID: "seg-002", Input: "seg-002.mp4", DurationSeconds: 3.0, GapBeforeSeconds: 0.0},
		},
	}
	command := BuildFFmpegCommand("ffmpeg", short)
	filter := argAfter(command, "-filter_complex")

	for _, want := range []string{
		"[0:v]",
		"[1:v]",
		"concat=n=2:v=1:a=1",
		"fps=24",
		"[2:a]volume=1.00[music]",
		"amix=inputs=2:duration=first:dropout_transition=0[a]",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("compilation filter missing %q:\n%s", want, filter)
		}
	}
	if !containsArg(command, "seg-001.mp4") || !containsArg(command, "seg-002.mp4") || !containsArg(command, "music/trap-130.mp3") {
		t.Fatalf("command missing expected inputs: %#v", command)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/editor -run TestBuildFFmpegCommandForCompilationShort -v`

Expected: FAIL because `Parts` is ignored.

- [ ] **Step 3: Implement compilation command**

In `BuildFFmpegCommand`, add this check before `short.PlayerImage`:

```go
	if len(short.Parts) > 0 {
		return BuildCompilationFFmpegCommand(ffmpegPath, short)
	}
```

Add `BuildCompilationFFmpegCommand` in `internal/editor/ffmpeg.go`. It must:

- Add each part input with `-i <part.Input>`.
- Add music as the last input when `short.MusicPath != ""` using `-stream_loop -1 -i <music>`.
- Build one `filter_complex` with per-part video and audio chains, `concat=n=<parts>:v=1:a=1[gamev][gamea]`, output FPS formatting, and music-forward `amix`.
- Map `[v]` and `[a]`.
- Use `libx264`, configured preset/CRF, AAC `192k`, `+faststart`, and `-shortest`.

The first implementation should keep gap handling simple: use `GapBeforeSeconds` only after Task 7 wires rhythm timings. In this task, include all parts in order without inserted black gaps.

- [ ] **Step 4: Run test to verify pass**

Run: `go test ./internal/editor -run TestBuildFFmpegCommandForCompilationShort -v`

Expected: PASS.

- [ ] **Step 5: Commit the work unit**

```bash
git add internal/editor/ffmpeg.go internal/editor/manifest_test.go
git commit -m "feat(editor): render compilation shorts"
```

---

### Task 7: Apply Rhythm Sync To Compilation Parts

**Files:**
- Modify: `internal/editor/manifest.go`
- Modify: `internal/editor/ffmpeg.go`
- Test: `internal/editor/manifest_test.go`

- [ ] **Step 1: Write the failing test**

Add this test near `TestBuildManifestCompileSegmentsCreatesOneShort`:

```go
func TestBuildManifestAppliesRhythmSyncToCompiledParts(t *testing.T) {
	dir := t.TempDir()
	rhythmPath := filepath.Join(dir, "rhythm.json")
	if err := os.WriteFile(rhythmPath, []byte(`{
		"schema_version":"1.0",
		"segment_sync":[
			{"segment_id":"seg-001","timeline_start_seconds":0.5,"gap_before_seconds":0.5},
			{"segment_id":"seg-002","timeline_start_seconds":8.0,"gap_before_seconds":1.0}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.CompileSegments = true
	opts.MusicPath = filepath.Join(dir, "music", "trap-130.mp3")
	opts.RhythmPath = rhythmPath

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	parts := manifest.Shorts[0].Parts
	if got := parts[0].GapBeforeSeconds; got != 0.5 {
		t.Fatalf("part[0] gap = %.3f, want 0.500", got)
	}
	if got := parts[1].TimelineStartSeconds; got != 8.0 {
		t.Fatalf("part[1] timeline start = %.3f, want 8.000", got)
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/editor -run TestBuildManifestAppliesRhythmSyncToCompiledParts -v`

Expected: FAIL because rhythm JSON is not loaded by manifest construction.

- [ ] **Step 3: Load rhythm sync in manifest construction**

In `buildManifest`, before building compilation shorts, add:

```go
	rhythmSync, err := loadRhythmSync(opts.RhythmPath)
	if err != nil {
		return Manifest{Warnings: warnings}, err
	}
```

Pass `rhythmSync` into `buildCompiledShort`. In each `ShortPart`, when `entry, ok := rhythmSync[segment.ID]`, set:

```go
	part.TimelineStartSeconds = entry.TimelineStartSeconds
	part.GapBeforeSeconds = entry.GapBeforeSeconds
```

- [ ] **Step 4: Add FFmpeg gap insertion test**

Extend `TestBuildFFmpegCommandForCompilationShort` to assert:

```go
	if !strings.Contains(filter, "color=c=black:s=1080x1920:r=24:d=0.500") {
		t.Fatalf("compilation filter missing black gap:\n%s", filter)
	}
	if !strings.Contains(filter, "anullsrc=channel_layout=stereo:sample_rate=48000:d=0.500") {
		t.Fatalf("compilation filter missing audio gap:\n%s", filter)
	}
```

- [ ] **Step 5: Implement FFmpeg gap insertion**

In `BuildCompilationFFmpegCommand`, when a part has `GapBeforeSeconds > 0`, insert a generated black video source and silent audio source before that part in the concat inputs:

```go
	gapVideo := fmt.Sprintf("color=c=black:s=1080x1920:r=%d:d=%.3f", outputFPS(short), part.GapBeforeSeconds)
	gapAudio := fmt.Sprintf("anullsrc=channel_layout=stereo:sample_rate=48000:d=%.3f", part.GapBeforeSeconds)
```

Feed those generated labels into the same concat list before the part labels.

- [ ] **Step 6: Run tests to verify pass**

Run: `go test ./internal/editor -run 'Test(BuildManifestAppliesRhythmSyncToCompiledParts|BuildFFmpegCommandForCompilationShort)' -v`

Expected: PASS.

- [ ] **Step 7: Commit the work unit**

```bash
git add internal/editor/manifest.go internal/editor/ffmpeg.go internal/editor/manifest_test.go
git commit -m "feat(editor): align compilation parts to rhythm sync"
```

---

### Task 8: Add CLI And Wrapper Validation Flags

**Files:**
- Modify: `cmd/zv-editor/main.go`
- Modify: `cmd/zv/command_validation.go`
- Modify: `cmd/zv/usage.go`
- Test: `cmd/zv/app_workflows_e2e_test.go`

- [ ] **Step 1: Write the failing wrapper test**

In `cmd/zv/app_workflows_e2e_test.go`, find the existing `shorts render` validation test case that includes many flags. Add these args to that test case:

```go
"--music", "music/trap-130.mp3",
"--rhythm", "run/rhythm.json",
"--fps", "24",
"--compile-segments",
```

Expected stderr for that case should remain empty.

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./cmd/zv -run TestAppWorkflows -v`

Expected: FAIL with validation rejecting `--music`, `--rhythm`, `--fps`, or `--compile-segments`.

- [ ] **Step 3: Add CLI flags**

In `cmd/zv-editor/main.go`, add flag vars near `videoPreset`:

```go
		musicPath          = flag.String("music", "", "optional external music file mixed over game audio")
		rhythmPath         = flag.String("rhythm", "", "optional zv-rhythm JSON used for segment beat sync")
		outputFPS          = flag.Int("fps", 0, "final output FPS; defaults by editor preset")
		compileSegments    = flag.Bool("compile-segments", false, "render selected segments as one compilation short")
```

Pass values into `editor.Config`:

```go
		MusicPath:          *musicPath,
		RhythmPath:         *rhythmPath,
		OutputFPS:          *outputFPS,
		CompileSegments:    *compileSegments,
```

- [ ] **Step 4: Add wrapper validation flags**

In `cmd/zv/command_validation.go`, add value flags to the `"shorts render"` case:

```go
			"--music",
			"--rhythm",
			"--fps",
```

Add `--compile-segments` to the `"shorts render"` bool flags case.

In `cmd/zv/usage.go`, keep `shortsUsage` broad as `zv-editor flags`; no text change is required unless tests expect a detailed flag list.

- [ ] **Step 5: Run test to verify pass**

Run: `go test ./cmd/zv -run TestAppWorkflows -v`

Expected: PASS.

- [ ] **Step 6: Commit the work unit**

```bash
git add cmd/zv-editor/main.go cmd/zv/command_validation.go cmd/zv/usage.go cmd/zv/app_workflows_e2e_test.go
git commit -m "feat(cli): expose music rhythm fps shorts flags"
```

---

### Task 9: Verify Editor Slice And Build Binaries


**Files:**
- No source edits expected.

- [ ] **Step 1: Format changed Go files**

Run: `bash scripts/go-format-changed.sh internal/editor/types.go internal/editor/run.go internal/editor/manifest.go internal/editor/filter.go internal/editor/ffmpeg.go internal/editor/rhythm_sync.go internal/editor/rhythm_sync_test.go internal/editor/manifest_test.go cmd/zv-editor/main.go cmd/zv/command_validation.go cmd/zv/usage.go cmd/zv/app_workflows_e2e_test.go`

Expected: exits 0.

- [ ] **Step 2: Run focused Go tests**

Run: `go test ./internal/editor ./cmd/zv ./cmd/zv-editor ./internal/rhythm -v`

Expected: PASS.

- [ ] **Step 3: Run project gate without formatting unrelated dirty files**

Run: `bash scripts/go-gate.sh --no-format`

Expected: PASS. If optional tools are missing, record the exact tool message and continue only if the script exits 0.

- [ ] **Step 4: Build binaries**

Run: `powershell -ExecutionPolicy Bypass -File .\scripts\build.ps1`

Expected: PASS and binaries under `bin/`, including `bin/zv.exe`, `bin/zv-editor.exe`, `bin/zv-rhythm.exe`, `bin/zv-recorder.exe`, and `bin/zv-parser.exe`.

- [ ] **Step 5: Commit verification-ready changes**

```bash
git status --short
git add internal/editor cmd/zv cmd/zv-editor docs/superpowers/specs/2026-05-29-zack-replay-shorts-design.md docs/superpowers/plans/2026-05-29-zack-replay-shorts.md
git commit -m "feat(editor): render beat synced replay compilations"
```

---

### Task 10: Run The Authorized Zack Production Workflow

**Files:**
- Generated only under `data/runs/zack-replays-20260529/`

- [ ] **Step 1: Create run directories**

Run in PowerShell:

```powershell
$RunRoot = "data\runs\zack-replays-20260529"
New-Item -ItemType Directory -Path $RunRoot -Force | Out-Null
New-Item -ItemType Directory -Path "$RunRoot\music" -Force | Out-Null
```

Expected: directories exist.

- [ ] **Step 2: Download CC0 music**

Run in PowerShell:

```powershell
$Music = "data\runs\zack-replays-20260529\music\trap-loop-130bpm-spatelyk4.mp3"
Invoke-WebRequest -Uri "https://cdn.freesound.org/previews/490/490790_9818679-hq.mp3" -OutFile $Music
```

Expected: `$Music` exists and is non-empty.

- [ ] **Step 3: Discover demos**

Run in PowerShell:

```powershell
$Demos = Get-ChildItem -LiteralPath "C:\Users\reche\Downloads\replays" -Filter "*.dem" | Sort-Object Name
$Demos.Count
```

Expected: prints `8`.

- [ ] **Step 4: Parse each demo for Zack**

Run in PowerShell:

```powershell
$RunRoot = "data\runs\zack-replays-20260529"
$SteamID = "76561197997743909"
$Demos = Get-ChildItem -LiteralPath "C:\Users\reche\Downloads\replays" -Filter "*.dem" | Sort-Object Name
foreach ($Demo in $Demos) {
  $Slug = [IO.Path]::GetFileNameWithoutExtension($Demo.Name)
  $Out = Join-Path $RunRoot $Slug
  New-Item -ItemType Directory -Path $Out -Force | Out-Null
  .\bin\zv.exe demo parse --demo $Demo.FullName --steamid $SteamID --out (Join-Path $Out "killplan.json")
}
```

Expected: each demo directory contains `killplan.json` with at least one segment.

- [ ] **Step 5: Analyze music per kill plan**

Run in PowerShell:

```powershell
$RunRoot = "data\runs\zack-replays-20260529"
$Music = "data\runs\zack-replays-20260529\music\trap-loop-130bpm-spatelyk4.mp3"
Get-ChildItem -LiteralPath $RunRoot -Directory | Where-Object { $_.Name -ne "music" } | ForEach-Object {
  .\bin\zv.exe music analyze --input $Music --killplan (Join-Path $_.FullName "killplan.json") --out (Join-Path $_.FullName "rhythm.json") --min-bpm 120 --max-bpm 140 --kill-offset-ms 100 --max-beats 512
}
```

Expected: each demo directory contains `rhythm.json` with `segment_sync` entries.

- [ ] **Step 6: Record each demo with authorized HLAE/CS2**

Run in PowerShell:

```powershell
$RunRoot = "data\runs\zack-replays-20260529"
$DemosBySlug = @{}
Get-ChildItem -LiteralPath "C:\Users\reche\Downloads\replays" -Filter "*.dem" | ForEach-Object { $DemosBySlug[[IO.Path]::GetFileNameWithoutExtension($_.Name)] = $_.FullName }
Get-ChildItem -LiteralPath $RunRoot -Directory | Where-Object { $_.Name -ne "music" } | ForEach-Object {
  .\bin\zv.exe record --killplan (Join-Path $_.FullName "killplan.json") --demo $DemosBySlug[$_.Name] --out (Join-Path $_.FullName "recording") --hlae "C:\HLAE-2.190.1\HLAE.exe" --cs2 "C:\Games\Counter-Strike 2\game\bin\win64\cs2.exe"
}
```

Expected: each demo directory contains `recording\recording-result.json` and segment clips. If the CS2 path is not valid on this machine, stop and locate the installed `cs2.exe` before retrying this step once.

- [ ] **Step 7: Render one Short per demo**

Run in PowerShell:

```powershell
$RunRoot = "data\runs\zack-replays-20260529"
$Music = (Resolve-Path "data\runs\zack-replays-20260529\music\trap-loop-130bpm-spatelyk4.mp3").Path
Get-ChildItem -LiteralPath $RunRoot -Directory | Where-Object { $_.Name -ne "music" } | ForEach-Object {
  .\bin\zv.exe shorts render --recording-result (Join-Path $_.FullName "recording\recording-result.json") --killplan (Join-Path $_.FullName "killplan.json") --out (Join-Path $_.FullName "shorts") --publish-dir (Join-Path $_.FullName "shorts\publish") --preset short-clean --effects "effects\viral_premium.lua" --music $Music --rhythm (Join-Path $_.FullName "rhythm.json") --fps 24 --compile-segments --video-crf 16 --video-preset slow --hq-filters --audio-normalize --quality-checks --cover-sheets
}
```

Expected: each demo directory contains exactly one publishable compilation MP4 under `shorts\publish` plus manifest, caption, cover, gallery, and logs.

- [ ] **Step 8: Manual verification**

Open each `shorts\publish\index.html` or MP4. Confirm:

- The video is vertical and playable.
- The output is 24 FPS according to the publish manifest/probe metadata.
- Kills land close to beats.
- Lua overlays appear at kill moments.
- Music is audible above quiet game audio.

- [ ] **Step 9: Final non-media verification**

Run: `bash scripts/go-gate.sh --no-format`

Expected: PASS before claiming completion.

Do not commit generated files under `data/runs/`.

---

## Self-Review Notes

- Spec coverage: editor controls, 24 FPS, music, rhythm JSON validation, compilation output, CLI flags, unit tests, and authorized production run are each mapped to tasks.
- Generated media is isolated under `data/runs/zack-replays-20260529/` and excluded from commit instructions.
- Default behavior is protected by explicit default FPS and omitted-field tests.
