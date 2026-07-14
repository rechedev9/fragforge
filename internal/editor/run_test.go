package editor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rechedev9/fragforge/internal/killplan"
)

func TestRunDryRunWritesManifestsPromptsAndDoesNotExecuteFFmpeg(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)
	outDir := filepath.Join(dir, "shorts")
	missingFFmpeg := filepath.Join(dir, "missing-ffmpeg")

	result, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           outDir,
		FFmpegPath:          missingFFmpeg,
		DryRun:              true,
	})
	if err != nil {
		t.Fatalf("Run dry-run error = %v", err)
	}
	if !result.DryRun {
		t.Fatal("result.DryRun = false, want true")
	}
	if len(result.Shorts) != 2 {
		t.Fatalf("shorts len = %d, want 2", len(result.Shorts))
	}
	for _, path := range []string{
		filepath.Join(outDir, "edit-manifest.json"),
		filepath.Join(outDir, "shorts-result.json"),
		filepath.Join(outDir, "prompts", "short-001-seg-001-cover.md"),
		filepath.Join(outDir, "publish", "pack-manifest.json"),
		filepath.Join(outDir, "publish", "index.html"),
		filepath.Join(outDir, "publish", "publish-summary.md"),
		filepath.Join(outDir, "publish", "01_seg-001_martinezsa_de_ancient_2k_ak47.caption.txt"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected file missing %s: %v", path, err)
		}
	}
	if _, err := os.Stat(filepath.Join(outDir, "short-001-seg-001.mp4")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create video, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "publish", "01_seg-001_martinezsa_de_ancient_2k_ak47.mp4")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not publish video, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "publish", "01_seg-001_martinezsa_de_ancient_2k_ak47.cover.jpg")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not create cover, stat err = %v", err)
	}
	if result.Shorts[0].CoverPath == "" || result.Shorts[0].CoverTimeSeconds == 0 {
		t.Fatalf("dry-run missing planned cover: %#v", result.Shorts[0])
	}
}

func TestRunDryRunFiltersSegmentsAndLimit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)
	outDir := filepath.Join(dir, "shorts")

	result, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           outDir,
		SegmentIDs:          []string{"seg-002"},
		Limit:               1,
		FFmpegPath:          filepath.Join(dir, "missing-ffmpeg"),
		DryRun:              true,
	})
	if err != nil {
		t.Fatalf("Run dry-run error = %v", err)
	}
	if result.Limit != 1 || len(result.SegmentFilter) != 1 || result.SegmentFilter[0] != "seg-002" {
		t.Fatalf("selection metadata missing: %#v", result)
	}
	if len(result.Shorts) != 1 || result.Shorts[0].SegmentID != "seg-002" {
		t.Fatalf("shorts = %#v", result.Shorts)
	}
	if _, err := os.Stat(filepath.Join(outDir, "publish", "02_seg-002_martinezsa_de_ancient_1k_awp.caption.txt")); err != nil {
		t.Fatalf("filtered caption missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "publish", "01_seg-001_martinezsa_de_ancient_2k_ak47.caption.txt")); !os.IsNotExist(err) {
		t.Fatalf("unselected caption should not exist, stat err = %v", err)
	}
}

func TestRunWithFakeFFmpegWritesShortResults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)
	outDir := filepath.Join(dir, "shorts")
	ffmpegPath := fakeFFmpeg(t, dir)

	result, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           outDir,
		FFmpegPath:          ffmpegPath,
	})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if len(result.Shorts) != 2 {
		t.Fatalf("shorts len = %d, want 2", len(result.Shorts))
	}
	first := result.Shorts[0]
	if first.OutputArtifact.SizeBytes == 0 {
		t.Fatalf("short output artifact missing size: %#v", first.OutputArtifact)
	}
	if _, err := os.Stat(filepath.Join(outDir, "short-001-seg-001.mp4")); err != nil {
		t.Fatalf("short output missing: %v", err)
	}
	if first.PublishArtifact.SizeBytes == 0 {
		t.Fatalf("publish artifact missing size: %#v", first.PublishArtifact)
	}
	if _, err := os.Stat(filepath.Join(outDir, "publish", "01_seg-001_martinezsa_de_ancient_2k_ak47.mp4")); err != nil {
		t.Fatalf("publish output missing: %v", err)
	}
	if first.CoverArtifact.SizeBytes == 0 {
		t.Fatalf("cover artifact missing size: %#v", first.CoverArtifact)
	}
	if _, err := os.Stat(filepath.Join(outDir, "publish", "01_seg-001_martinezsa_de_ancient_2k_ak47.cover.jpg")); err != nil {
		t.Fatalf("cover output missing: %v", err)
	}
	if got := argAfter(first.FFmpegCommand, "-vf"); !strings.Contains(got, "crop=1080:1920") {
		t.Fatalf("ffmpeg filter missing vertical crop:\n%s", got)
	}
	if got := argAfter(first.CoverCommand, "-ss"); got != "0.880" {
		t.Fatalf("cover -ss arg = %q, want 0.880", got)
	}
	if got := argAfter(first.FFmpegCommand, "-c:a"); got != "aac" {
		t.Fatalf("-c:a arg = %q, want aac", got)
	}

	var written Result
	b, err := os.ReadFile(filepath.Join(outDir, "shorts-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &written); err != nil {
		t.Fatal(err)
	}
	if len(written.Shorts) != 2 || written.Shorts[0].SegmentID != "seg-001" {
		t.Fatalf("written result = %#v", written.Shorts)
	}

	var pack PackManifest
	b, err = os.ReadFile(filepath.Join(outDir, "publish", "pack-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &pack); err != nil {
		t.Fatal(err)
	}
	if len(pack.Items) != 2 || pack.Items[0].Video == "" || pack.Items[0].CoverPath == "" || !strings.Contains(pack.Items[0].Caption, "#CS2") {
		t.Fatalf("pack manifest = %#v", pack.Items)
	}
	if _, err := os.Stat(filepath.Join(outDir, "publish", "index.html")); err != nil {
		t.Fatalf("publish gallery missing: %v", err)
	}
	indexHTML, err := os.ReadFile(filepath.Join(outDir, "publish", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"../prompts/short-001-seg-001-cover.md",
		"preset <span>viral-60-clean</span>",
		"video <span>crf 16 / slow</span>",
		"id=\"search\"",
		"data-copy-target=\".caption\"",
		"All weapons",
		"source: h264 | 1920x1080 | 60fps | 8.0s",
		"output:",
	} {
		if !strings.Contains(string(indexHTML), want) {
			t.Fatalf("publish gallery missing %q:\n%s", want, indexHTML)
		}
	}
	summary, err := os.ReadFile(filepath.Join(outDir, "publish", "publish-summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# FragForge Publish Summary", "Total kills: 3", "AK-47 x1", "AWP x1", "| 01 | seg-001 |"} {
		if !strings.Contains(string(summary), want) {
			t.Fatalf("publish summary missing %q:\n%s", want, summary)
		}
	}
}

func TestRunRejectsInvalidVideoEncodingOptions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)

	_, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           filepath.Join(dir, "bad-crf"),
		VideoCRF:            99,
		FFmpegPath:          filepath.Join(dir, "missing-ffmpeg"),
		DryRun:              true,
	})
	if err == nil || !strings.Contains(err.Error(), "video crf") {
		t.Fatalf("Run error = %v, want video crf validation", err)
	}

	_, err = Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           filepath.Join(dir, "bad-preset"),
		VideoPreset:         "cinema",
		FFmpegPath:          filepath.Join(dir, "missing-ffmpeg"),
		DryRun:              true,
	})
	if err == nil || !strings.Contains(err.Error(), "unknown video preset") {
		t.Fatalf("Run error = %v, want video preset validation", err)
	}
}

func TestRunSkipExistingReusesRenderedFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)
	outDir := filepath.Join(dir, "shorts")
	for _, path := range []string{
		filepath.Join(outDir, "short-001-seg-001.mp4"),
		filepath.Join(outDir, "short-002-seg-002.mp4"),
		filepath.Join(outDir, "publish", "01_seg-001_martinezsa_de_ancient_2k_ak47.cover.jpg"),
		filepath.Join(outDir, "publish", "02_seg-002_martinezsa_de_ancient_1k_awp.cover.jpg"),
		filepath.Join(outDir, "publish", "01_seg-001_martinezsa_de_ancient_2k_ak47.sheet.jpg"),
		filepath.Join(outDir, "publish", "02_seg-002_martinezsa_de_ancient_1k_awp.sheet.jpg"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           outDir,
		FFmpegPath:          filepath.Join(dir, "missing-ffmpeg"),
		SkipExisting:        true,
	})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if !result.SkipExisting {
		t.Fatal("SkipExisting = false, want true")
	}
	if !result.Shorts[0].RenderSkipped || !result.Shorts[0].CoverSkipped {
		t.Fatalf("skip flags missing: %#v", result.Shorts[0])
	}
	if result.Shorts[0].OutputArtifact.SizeBytes == 0 || result.Shorts[0].CoverArtifact.SizeBytes == 0 {
		t.Fatalf("reused artifacts missing size: %#v", result.Shorts[0])
	}
	if _, err := os.Stat(filepath.Join(outDir, "publish", "01_seg-001_martinezsa_de_ancient_2k_ak47.mp4")); err != nil {
		t.Fatalf("publish output missing after reuse: %v", err)
	}
}

func TestRunNoCoversSkipsCoverOutputs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)
	outDir := filepath.Join(dir, "shorts")
	ffmpegPath := fakeFFmpeg(t, dir)

	result, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           outDir,
		FFmpegPath:          ffmpegPath,
		DisableCovers:       true,
	})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if result.CoversEnabled {
		t.Fatal("CoversEnabled = true, want false")
	}
	if result.Shorts[0].CoverPath != "" || len(result.Shorts[0].CoverCommand) != 0 {
		t.Fatalf("cover data should be empty: %#v", result.Shorts[0])
	}
	if _, err := os.Stat(filepath.Join(outDir, "publish", "01_seg-001_martinezsa_de_ancient_2k_ak47.cover.jpg")); !os.IsNotExist(err) {
		t.Fatalf("cover should not exist, stat err = %v", err)
	}
}

func TestRunCoverFailureIsWarningOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)
	outDir := filepath.Join(dir, "shorts")
	ffmpegPath := fakeFFmpegFailingCovers(t, dir)

	result, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           outDir,
		FFmpegPath:          ffmpegPath,
	})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(outDir, "publish", "01_seg-001_martinezsa_de_ancient_2k_ak47.mp4")); err != nil {
		t.Fatalf("publish output missing: %v", err)
	}
	joined := strings.Join(result.Warnings, "\n")
	if !strings.Contains(joined, "cover seg-001") {
		t.Fatalf("warnings missing cover failure:\n%s", joined)
	}
}

func TestRunShortRenderFailureWritesLog(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	recordingResultPath := writeRecordingResultFixture(t, dir)
	outDir := filepath.Join(dir, "shorts")
	ffmpegPath := fakeFFmpegFailingShorts(t, dir)

	result, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           outDir,
		FFmpegPath:          ffmpegPath,
	})
	if err == nil {
		t.Fatal("Run error = nil, want short render failure")
	}
	if len(result.Shorts) == 0 || result.Shorts[0].RenderLogPath == "" {
		t.Fatalf("render log path missing: %#v", result.Shorts)
	}
	b, readErr := os.ReadFile(result.Shorts[0].RenderLogPath)
	if readErr != nil {
		t.Fatalf("read render log: %v", readErr)
	}
	if got := strings.TrimSpace(string(b)); got == "" {
		t.Fatal("render log is empty, want ffmpeg output or process start error")
	} else if !strings.Contains(got, "short render failed") && !strings.Contains(got, "ffmpeg short edit") {
		t.Fatalf("render log missing failure diagnostic:\n%s", b)
	}
}

func TestRunAutoDiscoversKillPlanFromPipelineResult(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	result := testRecordingResult(dir)
	result.Plan.DemoMap = ""
	result.Plan.DemoPath = filepath.Join(dir, "match.dem")
	recordingResultPath := writeRecordingResult(t, dir, result)
	planPath := writeKillPlanFixture(t, dir, "de_ancient")
	writeJSONFile(t, filepath.Join(dir, "pipeline-result.json"), map[string]string{"killplan": planPath})

	outDir := filepath.Join(dir, "shorts")
	_, err := Run(context.Background(), Config{
		RecordingResultPath: recordingResultPath,
		OutputDir:           outDir,
		FFmpegPath:          filepath.Join(dir, "missing-ffmpeg"),
		DryRun:              true,
	})
	if err != nil {
		t.Fatalf("Run dry-run error = %v", err)
	}

	var manifest Manifest
	b, err := os.ReadFile(filepath.Join(outDir, "edit-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.KillPlan != planPath {
		t.Fatalf("manifest.KillPlan = %q, want %q", manifest.KillPlan, planPath)
	}
	if got := manifest.Shorts[0].Label; got != "MartinezSa | de_ancient | 2K" {
		t.Fatalf("label = %q", got)
	}
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
}

func writeRecordingResultFixture(t *testing.T, dir string) string {
	t.Helper()
	return writeRecordingResult(t, dir, testRecordingResult(dir))
}

func writeRecordingResult(t *testing.T, dir string, result any) string {
	t.Helper()
	path := filepath.Join(dir, "recording", "recording-result.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeKillPlanFixture(t *testing.T, dir, mapName string) string {
	t.Helper()
	path := filepath.Join(dir, "plan.json")
	plan := killplan.NewPlan()
	plan.Demo.Map = mapName
	plan.Target.NameInDemo = "MartinezSa"
	writeJSONFile(t, path, plan)
	return path
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

var (
	fakeFFmpegOnce       sync.Once
	fakeFFmpegDir        string
	fakeFFmpegPath       string
	fakeFFmpegCoverPath  string
	fakeFFmpegShortPath  string
	fakeFFmpegFixtureErr error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if err := removeFakeFFmpegFixture(); err != nil {
		fmt.Fprintf(os.Stderr, "remove fake ffmpeg fixture: %v\n", err)
		code = 1
	}
	os.Exit(code)
}

func fakeFFmpeg(t *testing.T, _ string) string {
	t.Helper()
	ensureFakeFFmpegFixture(t)
	return fakeFFmpegPath
}

func fakeFFmpegFailingCovers(t *testing.T, _ string) string {
	t.Helper()
	ensureFakeFFmpegFixture(t)
	return fakeFFmpegCoverPath
}

func fakeFFmpegFailingShorts(t *testing.T, _ string) string {
	t.Helper()
	ensureFakeFFmpegFixture(t)
	return fakeFFmpegShortPath
}

func ensureFakeFFmpegFixture(t *testing.T) {
	t.Helper()
	fakeFFmpegOnce.Do(buildFakeFFmpegFixture)
	if fakeFFmpegFixtureErr != nil {
		t.Fatalf("build fake ffmpeg fixture: %v", fakeFFmpegFixtureErr)
	}
}

func buildFakeFFmpegFixture() {
	fakeFFmpegDir, fakeFFmpegFixtureErr = os.MkdirTemp("", "zv-editor-fake-ffmpeg-*")
	if fakeFFmpegFixtureErr != nil {
		return
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	fakeFFmpegPath = filepath.Join(fakeFFmpegDir, "ffmpeg"+ext)
	fakeFFmpegCoverPath = filepath.Join(fakeFFmpegDir, "ffmpeg-fail-cover"+ext)
	fakeFFmpegShortPath = filepath.Join(fakeFFmpegDir, "ffmpeg-fail-short"+ext)

	if runtime.GOOS == "windows" {
		src := filepath.Join(fakeFFmpegDir, "fake-ffmpeg.go")
		body := `package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		return
	}
	mode := strings.ToLower(filepath.Base(os.Args[0]))
	out := os.Args[len(os.Args)-1]
	if strings.Contains(mode, "fail-short") && strings.HasSuffix(out, ".mp4") {
		_, _ = fmt.Fprintln(os.Stderr, "short render failed")
		os.Exit(2)
	}
	if strings.Contains(mode, "fail-cover") && strings.HasSuffix(out, ".cover.jpg") {
		os.Exit(2)
	}
	if out == "-" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(out), 0755)
	_ = os.WriteFile(out, []byte("fake"), 0644)
}
`
		if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
			fakeFFmpegFixtureErr = err
			return
		}
		goExe, err := exec.LookPath("go")
		if err != nil {
			fakeFFmpegFixtureErr = fmt.Errorf("find go toolchain: %w", err)
			return
		}
		if out, err := exec.Command(goExe, "build", "-o", fakeFFmpegPath, src).CombinedOutput(); err != nil {
			fakeFFmpegFixtureErr = fmt.Errorf("go build: %w: %s", err, out)
			return
		}
	} else {
		body := "#!/bin/sh\nlast=\nfor arg in \"$@\"; do last=\"$arg\"; done\nmode=$(basename \"$0\")\ncase \"$mode:$last\" in *fail-short*:*.mp4) echo short render failed >&2; exit 2;; *fail-cover*:*.cover.jpg) exit 2;; *:-) exit 0;; esac\nmkdir -p \"$(dirname \"$last\")\"\nprintf fake > \"$last\"\n"
		if err := os.WriteFile(fakeFFmpegPath, []byte(body), 0o755); err != nil {
			fakeFFmpegFixtureErr = err
			return
		}
	}
	for _, path := range []string{fakeFFmpegCoverPath, fakeFFmpegShortPath} {
		if err := os.Link(fakeFFmpegPath, path); err != nil {
			fakeFFmpegFixtureErr = fmt.Errorf("link %s: %w", path, err)
			return
		}
	}
}

func removeFakeFFmpegFixture() error {
	if fakeFFmpegDir == "" {
		return nil
	}
	var err error
	for attempt := 0; attempt < 40; attempt++ {
		err = os.RemoveAll(fakeFFmpegDir)
		if err == nil {
			return nil
		}
		if runtime.GOOS != "windows" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return err
}
