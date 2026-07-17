package streamcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

type fakeStreamService struct {
	probe              streamclips.SourceProbe
	probeErr           error
	detectedCues       []float64
	detectErr          error
	renderResult       streamRenderResult
	renderErr          error
	ffmpegErr          error
	transcript         streamTranscriptReview
	transcribeErr      error
	probeCalls         int
	detectCalls        int
	transcribeCalls    int
	renderCalls        int
	ffmpegChecks       int
	whisperRequired    bool
	probeHasDeadline   bool
	ffmpegHasDeadline  bool
	transcribeDeadline bool
	renderHasDeadline  bool
	transcribeRequest  streamTranscribeRequest
	renderRequest      streamRenderRequest
}

func (f *fakeStreamService) Probe(ctx context.Context, _ string, _ string) (streamclips.SourceProbe, error) {
	f.probeCalls++
	_, f.probeHasDeadline = ctx.Deadline()
	return f.probe, f.probeErr
}

func (f *fakeStreamService) ValidateFFmpeg(ctx context.Context, _ string, requireWhisper bool) error {
	f.ffmpegChecks++
	f.whisperRequired = requireWhisper
	_, f.ffmpegHasDeadline = ctx.Deadline()
	return f.ffmpegErr
}

func (f *fakeStreamService) DetectKillfeed(context.Context, string, string, streamclips.CropRect, float64, float64) ([]float64, error) {
	f.detectCalls++
	return append([]float64(nil), f.detectedCues...), f.detectErr
}

func (f *fakeStreamService) Transcribe(ctx context.Context, request streamTranscribeRequest) (streamTranscriptReview, error) {
	f.transcribeCalls++
	_, f.transcribeDeadline = ctx.Deadline()
	f.transcribeRequest = request
	return f.transcript, f.transcribeErr
}

func TestReplaceLocalPublishDirectoryDoesNotFailAfterPublicationOnCleanupError(t *testing.T) {
	dir := t.TempDir()
	staging := filepath.Join(dir, "staging")
	publish := filepath.Join(dir, "shortslistosparasubir")
	for _, path := range []string{staging, publish} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, "new.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publish, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanupErr := errors.New("file is temporarily in use")
	if err := replaceLocalPublishDirectoryWithCleanup(staging, publish, func(string) error { return cleanupErr }); err != nil {
		t.Fatalf("replaceLocalPublishDirectoryWithCleanup error = %v, want publication success", err)
	}
	if body, err := os.ReadFile(filepath.Join(publish, "new.txt")); err != nil || string(body) != "new" {
		t.Fatalf("published file = %q, error = %v", body, err)
	}
	backups, err := filepath.Glob(publish + ".previous-*")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backup dirs = %#v, error = %v, want one deferred cleanup", backups, err)
	}
}

func (f *fakeStreamService) Render(ctx context.Context, request streamRenderRequest) (streamRenderResult, error) {
	f.renderCalls++
	_, f.renderHasDeadline = ctx.Deadline()
	f.renderRequest = request
	return f.renderResult, f.renderErr
}

func TestRunStreamVariantsJSONIsMachineReadable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{"variants", "--format", "json"}, &stdout, &stderr, &fakeStreamService{})
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result struct {
		OK       bool               `json:"ok"`
		Variants []streamVariantRow `json:"variants"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, stdout.String())
	}
	if !result.OK || len(result.Variants) != len(streamclips.VariantNames()) {
		t.Fatalf("result = %#v", result)
	}
	if got, want := result.Variants[0].Name, streamclips.DefaultVariant().Name; got != want {
		t.Fatalf("first variant = %q, want %q", got, want)
	}
}

func TestRunStreamPlanDryRunBuildsValidatedCaptionPlanWithoutWriting(t *testing.T) {
	out := filepath.Join(t.TempDir(), "edit-plan.json")
	service := &fakeStreamService{probe: streamclips.SourceProbe{
		Width: 1920, Height: 1080, DurationSeconds: 12.5, VideoCodec: "h264", AudioCodec: "aac",
	}}
	args := []string{
		"plan",
		"--input", "stream.mp4",
		"--out", out,
		"--captions",
		"--killfeed-crop", "0.70,0.02,0.29,0.28",
		"--dry-run",
		"--format", "json",
	}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService(args, &stdout, &stderr, service)
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("dry-run output stat error = %v, want not exist", err)
	}
	var result streamPlanResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.DryRun || result.Executed {
		t.Fatalf("result = %#v", result)
	}
	if !result.Plan.Captions.Enabled || result.Plan.Captions.Language != "es" {
		t.Fatalf("captions = %#v", result.Plan.Captions)
	}
	if result.Plan.KillfeedCrop == nil {
		t.Fatal("killfeed crop missing")
	}
	if got, want := result.Plan.Clips[0].EndSeconds, 12.5; got != want {
		t.Fatalf("clip end = %v, want %v", got, want)
	}
}

func TestRunStreamPlanWritesEditablePlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nested", "edit-plan.json")
	service := &fakeStreamService{probe: streamclips.SourceProbe{DurationSeconds: 9}}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"plan", "--input", "stream.mp4", "--out", out,
		"--variant", streamclips.VariantStreamerFullframeNoCam,
		"--clip-start", "1", "--clip-end", "8",
	}, &stdout, &stderr, service)
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var plan streamclips.EditPlan
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Variant != streamclips.VariantStreamerFullframeNoCam || len(plan.Clips) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestRunStreamPlanRefusesToOverwriteSourceVideo(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "stream.mp4")
	if err := os.WriteFile(source, []byte("source-video"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &fakeStreamService{probe: streamclips.SourceProbe{DurationSeconds: 9}}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"plan", "--input", source, "--out", source,
	}, &stdout, &stderr, service)
	if code != exitInvalidArgs || !strings.Contains(stderr.String(), "must not overwrite --input") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if service.probeCalls != 0 {
		t.Fatalf("probe calls = %d, want 0", service.probeCalls)
	}
	body, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(body), "source-video"; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}
}

func TestRunStreamPlanDetectsKillfeedCuesIntoPlan(t *testing.T) {
	out := filepath.Join(t.TempDir(), "edit-plan.json")
	service := &fakeStreamService{
		probe:        streamclips.SourceProbe{DurationSeconds: 15},
		detectedCues: []float64{4.5, 5.75, 10.25},
	}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"plan", "--input", "stream.mp4", "--out", out,
		"--killfeed-crop", "0.82,0.05,0.17,0.18", "--detect-killfeed",
	}, &stdout, &stderr, service)
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if service.detectCalls != 1 {
		t.Fatalf("detect calls = %d, want 1", service.detectCalls)
	}
	body, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var plan streamclips.EditPlan
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatal(err)
	}
	if got, want := len(plan.Clips[0].KillfeedSeconds), 3; got != want {
		t.Fatalf("killfeed cues = %v, want %d", plan.Clips[0].KillfeedSeconds, want)
	}
}

func TestRunStreamPlanRequiresCropForKillfeedDetection(t *testing.T) {
	service := &fakeStreamService{probe: streamclips.SourceProbe{DurationSeconds: 15}}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"plan", "--input", "stream.mp4", "--out", "edit-plan.json", "--detect-killfeed",
	}, &stdout, &stderr, service)
	if code != exitInvalidArgs || !strings.Contains(stderr.String(), "requires --killfeed-crop") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if service.detectCalls != 0 {
		t.Fatalf("detect calls = %d, want 0", service.detectCalls)
	}
}

func TestRunStreamKillfeedImportsReviewedEventsWithoutWritingOnDryRun(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "edit-plan.json")
	eventsPath := filepath.Join(dir, "killfeed-events.json")
	outPath := filepath.Join(dir, "reviewed-plan.json")
	plan := streamclips.DefaultEditPlan()
	crop := streamclips.CropRect{X: 0.82, Y: 0.05, Width: 0.17, Height: 0.18}
	plan.KillfeedCrop = &crop
	plan.Clips = []streamclips.ClipRange{{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 15,
		KillfeedSeconds: []float64{2.75, 8.625},
	}}
	writeJSONFile(t, planPath, plan)
	writeJSONFile(t, eventsPath, killfeedImportDocument{
		SchemaVersion: killfeedImportSchemaVersion,
		ClipID:        "clip-001",
		Cues: []killfeedImportCue{
			{AtSeconds: 2.75, Kills: []streamclips.KillfeedKill{{AttackerSide: "CT", AttackerName: "ZaCkk", VictimSide: "T", VictimName: "ar4nit", Weapon: "awp", Headshot: true}}},
			{AtSeconds: 8.625, Kills: []streamclips.KillfeedKill{{AttackerSide: "CT", AttackerName: "ZaCkk", VictimSide: "T", VictimName: "bek657", Weapon: "awp"}}},
		},
	})

	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"killfeed", "--plan", planPath, "--events", eventsPath, "--out", outPath,
		"--dry-run", "--format", "json",
	}, &stdout, &stderr, &fakeStreamService{})
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run output stat error = %v, want not exist", err)
	}
	var result streamKillfeedResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.DryRun || result.Executed || result.CueCount != 2 || result.KillCount != 2 {
		t.Fatalf("result = %#v", result)
	}
	if got := result.Plan.Clips[0].KillfeedKills[0][0].VictimName; got != "ar4nit" {
		t.Fatalf("first victim = %q", got)
	}
}

func TestRunStreamKillfeedRejectsCueTimestampDrift(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "edit-plan.json")
	eventsPath := filepath.Join(dir, "killfeed-events.json")
	plan := streamclips.DefaultEditPlan()
	crop := streamclips.CropRect{X: 0.82, Y: 0.05, Width: 0.17, Height: 0.18}
	plan.KillfeedCrop = &crop
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 15, KillfeedSeconds: []float64{2.75}}}
	writeJSONFile(t, planPath, plan)
	writeJSONFile(t, eventsPath, killfeedImportDocument{
		SchemaVersion: killfeedImportSchemaVersion,
		ClipID:        "clip-001",
		Cues: []killfeedImportCue{{AtSeconds: 3.0, Kills: []streamclips.KillfeedKill{{
			AttackerSide: "CT", AttackerName: "ZaCkk", VictimSide: "T", VictimName: "ar4nit", Weapon: "awp",
		}}}},
	})

	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"killfeed", "--plan", planPath, "--events", eventsPath, "--out", filepath.Join(dir, "reviewed.json"),
	}, &stdout, &stderr, &fakeStreamService{})
	if code != exitInvalidArgs || !strings.Contains(stderr.String(), "does not match detected cue") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunStreamKillfeedDropsReviewedFalsePositiveCue(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "edit-plan.json")
	eventsPath := filepath.Join(dir, "killfeed-events.json")
	outPath := filepath.Join(dir, "reviewed-plan.json")
	plan := streamclips.DefaultEditPlan()
	crop := streamclips.CropRect{X: 0.82, Y: 0.05, Width: 0.17, Height: 0.18}
	plan.KillfeedCrop = &crop
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 15, KillfeedSeconds: []float64{2.75, 8.625}}}
	writeJSONFile(t, planPath, plan)
	writeJSONFile(t, eventsPath, killfeedImportDocument{
		SchemaVersion: killfeedImportSchemaVersion,
		ClipID:        "clip-001",
		Cues: []killfeedImportCue{
			{AtSeconds: 2.75, Kills: []streamclips.KillfeedKill{{AttackerSide: "CT", AttackerName: "ZaCkk", VictimSide: "T", VictimName: "ar4nit", Weapon: "awp"}}},
			{AtSeconds: 8.625, Kills: []streamclips.KillfeedKill{}},
		},
	})
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"killfeed", "--plan", planPath, "--events", eventsPath, "--out", outPath, "--format", "json",
	}, &stdout, &stderr, &fakeStreamService{})
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result streamKillfeedResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.CueCount != 2 || result.RejectedCueCount != 1 || result.KillCount != 1 {
		t.Fatalf("result counts = %#v", result)
	}
	clip := result.Plan.Clips[0]
	if !reflect.DeepEqual(clip.KillfeedSeconds, []float64{2.75}) || len(clip.KillfeedKills) != 1 {
		t.Fatalf("reviewed clip = %#v, want only the confirmed cue", clip)
	}
}

func TestRunStreamCaptionsImportsReviewedSpanishWords(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "reviewed-plan.json")
	wordsPath := filepath.Join(dir, "caption-words.json")
	outPath := filepath.Join(dir, "captioned-plan.json")
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 4, EndSeconds: 14}}
	writeJSONFile(t, planPath, plan)
	writeJSONFile(t, wordsPath, captionImportDocument{
		SchemaVersion: captionImportSchemaVersion,
		ClipID:        "clip-001",
		Language:      "es",
		Words: []streamclips.CaptionWord{
			{Word: "  Buena ", StartSeconds: 0.5, EndSeconds: 0.9},
			{Word: "jugada", StartSeconds: 1.0, EndSeconds: 1.5},
		},
	})

	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"captions", "--plan", planPath, "--words", wordsPath, "--out", outPath,
		"--dry-run", "--format", "json",
	}, &stdout, &stderr, &fakeStreamService{})
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run output stat error = %v, want not exist", err)
	}
	var result streamCaptionsResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.DryRun || result.Executed || result.WordCount != 2 || result.Language != "es" {
		t.Fatalf("result = %#v", result)
	}
	if !result.Plan.Captions.Enabled || result.Plan.Clips[0].CaptionWords[0].Word != "Buena" {
		t.Fatalf("caption plan = %#v", result.Plan)
	}
}

func TestRunStreamCaptionsRejectsOverlappingWords(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "edit-plan.json")
	wordsPath := filepath.Join(dir, "caption-words.json")
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 10}}
	writeJSONFile(t, planPath, plan)
	writeJSONFile(t, wordsPath, captionImportDocument{
		SchemaVersion: captionImportSchemaVersion,
		ClipID:        "clip-001",
		Language:      "es",
		Words: []streamclips.CaptionWord{
			{Word: "uno", StartSeconds: 0, EndSeconds: 1},
			{Word: "dos", StartSeconds: 0.8, EndSeconds: 1.2},
		},
	})

	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"captions", "--plan", planPath, "--words", wordsPath, "--out", filepath.Join(dir, "out.json"),
	}, &stdout, &stderr, &fakeStreamService{})
	if code != exitInvalidArgs || !strings.Contains(stderr.String(), "overlap or are unsorted") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunStreamCaptionsPersistsReviewedNoSpeech(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "edit-plan.json")
	wordsPath := filepath.Join(dir, "caption-words.json")
	outPath := filepath.Join(dir, "captioned-plan.json")
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 10}}
	writeJSONFile(t, planPath, plan)
	writeJSONFile(t, wordsPath, captionImportDocument{
		SchemaVersion: captionImportSchemaVersion,
		ClipID:        "clip-001",
		Language:      "es",
		NoSpeech:      true,
		Words:         []streamclips.CaptionWord{},
	})
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"captions", "--plan", planPath, "--words", wordsPath, "--out", outPath, "--format", "json",
	}, &stdout, &stderr, &fakeStreamService{})
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result streamCaptionsResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Plan.Clips[0].CaptionReviewed || result.WordCount != 0 || result.Plan.CaptionsNeedBackend() {
		t.Fatalf("result = %#v, want reviewed no-speech clip without backend", result)
	}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunStreamPlanReportsProbeFailureAsRuntimeError(t *testing.T) {
	service := &fakeStreamService{probeErr: errors.New("probe unavailable")}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"plan", "--input", "stream.mp4", "--out", "edit-plan.json", "--format", "json",
	}, &stdout, &stderr, service)
	if code != exitUnexpected || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result streamErrorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Error, "probe unavailable") {
		t.Fatalf("result = %#v", result)
	}
}

func TestStreamNoticeDetectorMergesBurstsAndRejectsTransientDrops(t *testing.T) {
	var detector streamNoticeDetector
	first := testStreamFingerprint(1)
	second := testStreamFingerprint(2)
	third := testStreamFingerprint(3)
	observations := []struct {
		seconds float64
		rows    []streamNoticeFingerprint
	}{
		{0, nil},
		{0.125, []streamNoticeFingerprint{first}},
		{0.250, []streamNoticeFingerprint{first, second}},
		{0.375, []streamNoticeFingerprint{first, second}},
		{0.500, []streamNoticeFingerprint{first}}, // one compressed frame must not lower the baseline
		{0.625, []streamNoticeFingerprint{first, second}},
		{1.000, []streamNoticeFingerprint{first, second, third}},
		{1.125, []streamNoticeFingerprint{first, second, third}},
	}
	var cues []float64
	for _, observation := range observations {
		if cue, ok := detector.Observe(observation.seconds, 0, observation.rows); ok {
			cues = append(cues, cue)
		}
	}
	cues = mergeStreamKillfeedCues(cues)
	if got, want := len(cues), 2; got != want {
		t.Fatalf("cues = %v, want %d bursts", cues, want)
	}
	if got, want := cues[0], 0.0; got != want {
		t.Fatalf("first cue = %v, want %v", got, want)
	}
	if got, want := cues[1], 0.875; got != want {
		t.Fatalf("second cue = %v, want %v", got, want)
	}
}

func TestStreamNoticeDetectorSeedsVisiblePrerollAndFindsSameCountReplacement(t *testing.T) {
	var detector streamNoticeDetector
	first := testStreamFingerprint(1)
	replacement := testStreamFingerprint(2)
	observations := []struct {
		seconds float64
		rows    []streamNoticeFingerprint
	}{
		{0, []streamNoticeFingerprint{first}},
		{1, []streamNoticeFingerprint{first}},
		{2, []streamNoticeFingerprint{first}},
		{3, []streamNoticeFingerprint{replacement}},
		{3.5, []streamNoticeFingerprint{replacement}},
		{4, []streamNoticeFingerprint{replacement}},
	}
	var cues []float64
	for _, observation := range observations {
		if cue, ok := detector.Observe(observation.seconds, 2, observation.rows); ok {
			cues = append(cues, cue)
		}
	}
	if got, want := cues, []float64{2.875}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cues = %v, want %v", got, want)
	}
}

func TestDecodeStreamPNGReadsConcatenatedFrames(t *testing.T) {
	var stream bytes.Buffer
	for _, width := range []int{2, 3} {
		if err := png.Encode(&stream, image.NewRGBA(image.Rect(0, 0, width, 1))); err != nil {
			t.Fatal(err)
		}
	}
	for _, wantWidth := range []int{2, 3} {
		frame, err := decodeStreamPNG(&stream)
		if err != nil {
			t.Fatal(err)
		}
		if got := frame.Bounds().Dx(); got != wantWidth {
			t.Fatalf("width = %d, want %d", got, wantWidth)
		}
	}
	if _, err := decodeStreamPNG(&stream); !errors.Is(err, io.EOF) {
		t.Fatalf("final error = %v, want EOF", err)
	}
}

func testStreamFingerprint(bit uint) streamNoticeFingerprint {
	var fingerprint streamNoticeFingerprint
	fingerprint.bits[0] = uint64(1) << bit
	fingerprint.features = streamFingerprintMinFeatures
	return fingerprint
}

func TestRunStreamRenderDryRunDoesNotInvokeRenderer(t *testing.T) {
	dir := t.TempDir()
	planPath := writeValidStreamPlan(t, dir, 10)
	service := &fakeStreamService{probe: streamclips.SourceProbe{DurationSeconds: 10, VideoCodec: "h264", AudioCodec: "aac"}}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"render", "--input", "stream.mp4", "--plan", planPath, "--out", filepath.Join(dir, "run"),
		"--dry-run", "--format", "json",
	}, &stdout, &stderr, service)
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if service.renderCalls != 0 {
		t.Fatalf("render calls = %d, want 0", service.renderCalls)
	}
	if service.ffmpegChecks != 1 || service.whisperRequired {
		t.Fatalf("ffmpeg checks = %d whisper = %v, want one ordinary render check", service.ffmpegChecks, service.whisperRequired)
	}
	if !service.probeHasDeadline || !service.ffmpegHasDeadline {
		t.Fatalf("render preflight deadlines: probe=%v ffmpeg=%v", service.probeHasDeadline, service.ffmpegHasDeadline)
	}
	var result streamRenderResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || !result.DryRun || result.Executed || !strings.HasSuffix(result.PublishDir, "shortslistosparasubir") {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunStreamRenderDryRunRejectsUnavailableFFmpeg(t *testing.T) {
	dir := t.TempDir()
	planPath := writeValidStreamPlan(t, dir, 10)
	service := &fakeStreamService{
		probe:     streamclips.SourceProbe{DurationSeconds: 10},
		ffmpegErr: errors.New("ffmpeg missing"),
	}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"render", "--input", "stream.mp4", "--plan", planPath, "--out", filepath.Join(dir, "run"),
		"--ffmpeg", "missing-ffmpeg", "--dry-run", "--format", "json",
	}, &stdout, &stderr, service)
	if code != exitUnexpected || stderr.Len() != 0 || service.renderCalls != 0 {
		t.Fatalf("code = %d, stderr = %q, render calls = %d", code, stderr.String(), service.renderCalls)
	}
	var result streamErrorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Error, "ffmpeg missing") {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunStreamRenderRejectsSourceInsideReplacedPublishDirectory(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "run")
	publishDir := filepath.Join(outDir, "shortslistosparasubir")
	planPath := writeValidStreamPlan(t, dir, 10)
	service := &fakeStreamService{probe: streamclips.SourceProbe{DurationSeconds: 10}}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"render", "--input", filepath.Join(publishDir, "old.mp4"), "--plan", planPath,
		"--out", outDir, "--dry-run", "--format", "json",
	}, &stdout, &stderr, service)
	if code != exitInvalidArgs || stderr.Len() != 0 || service.renderCalls != 0 || service.ffmpegChecks != 0 {
		t.Fatalf("code = %d, stderr = %q, render calls = %d, ffmpeg checks = %d", code, stderr.String(), service.renderCalls, service.ffmpegChecks)
	}
	var result streamErrorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Error, "must not be inside publish directory") {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunStreamRenderDryRunRejectsCaptionsWithoutWordsOrBackend(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	dir := t.TempDir()
	plan := streamclips.DefaultEditPlan()
	plan.Captions = streamclips.CaptionsPlan{Enabled: true, Language: "es"}
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 10}}
	planPath := filepath.Join(dir, "edit-plan.json")
	writeJSONFile(t, planPath, plan)
	service := &fakeStreamService{probe: streamclips.SourceProbe{DurationSeconds: 10, VideoCodec: "h264", AudioCodec: "aac"}}

	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"render", "--input", "stream.mp4", "--plan", planPath, "--out", filepath.Join(dir, "run"),
		"--dry-run", "--format", "json",
	}, &stdout, &stderr, service)
	if code != exitUnexpected || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q, want runtime preflight failure", code, stderr.String())
	}
	if service.renderCalls != 0 {
		t.Fatalf("render calls = %d, want 0", service.renderCalls)
	}
	var result streamErrorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || !strings.Contains(result.Error, "neither reviewed caption words") {
		t.Fatalf("result = %#v, want caption-readiness error", result)
	}
}

func TestRunStreamRenderDryRunAcceptsReviewedCaptionsWithoutBackend(t *testing.T) {
	t.Setenv("XAI_API_KEY", "")
	dir := t.TempDir()
	plan := streamclips.DefaultEditPlan()
	plan.Captions = streamclips.CaptionsPlan{Enabled: true, Language: "es"}
	plan.Clips = []streamclips.ClipRange{{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 10,
		CaptionWords: []streamclips.CaptionWord{{Word: "Buena", StartSeconds: 0.5, EndSeconds: 0.9}},
	}}
	planPath := filepath.Join(dir, "edit-plan.json")
	writeJSONFile(t, planPath, plan)
	service := &fakeStreamService{probe: streamclips.SourceProbe{DurationSeconds: 10, VideoCodec: "h264", AudioCodec: "aac"}}

	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"render", "--input", "stream.mp4", "--plan", planPath, "--out", filepath.Join(dir, "run"),
		"--dry-run", "--format", "json",
	}, &stdout, &stderr, service)
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q, want successful credential-free preflight", code, stderr.String())
	}
	if service.renderCalls != 0 {
		t.Fatalf("render calls = %d, want 0", service.renderCalls)
	}
}

func TestRunStreamRenderRejectsAdditionalJSONValues(t *testing.T) {
	dir := t.TempDir()
	planPath := writeValidStreamPlan(t, dir, 10)
	f, err := os.OpenFile(planPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("\n{}\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	service := &fakeStreamService{probe: streamclips.SourceProbe{DurationSeconds: 10}}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"render", "--input", "stream.mp4", "--plan", planPath, "--out", filepath.Join(dir, "run"),
	}, &stdout, &stderr, service)
	if code != exitInvalidArgs || !strings.Contains(stderr.String(), "multiple JSON values") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if service.probeCalls != 0 || service.renderCalls != 0 {
		t.Fatalf("probe calls = %d, render calls = %d, want 0, 0", service.probeCalls, service.renderCalls)
	}
}

func TestRunStreamRenderInvokesLocalServiceAndReturnsOneJSONDocument(t *testing.T) {
	dir := t.TempDir()
	planPath := writeValidStreamPlan(t, dir, 10)
	want := streamRenderResult{
		OK: true, Executed: true, Variant: streamclips.DefaultVariant().Name,
		PublishDir: filepath.Join(dir, "run", "shortslistosparasubir"),
		Videos:     []streamLocalVideo{{ClipID: "clip-001", Path: filepath.Join(dir, "clip-001.mp4")}},
		Warnings:   []string{},
	}
	service := &fakeStreamService{
		probe:        streamclips.SourceProbe{DurationSeconds: 10, VideoCodec: "h264", AudioCodec: "aac"},
		renderResult: want,
	}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"render", "--input", "stream.mp4", "--plan", planPath, "--out", filepath.Join(dir, "run"), "--format", "json",
	}, &stdout, &stderr, service)
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if service.renderCalls != 1 || service.renderRequest.Plan.Variant != streamclips.DefaultVariant().Name {
		t.Fatalf("render calls = %d request = %#v", service.renderCalls, service.renderRequest)
	}
	if !service.probeHasDeadline || !service.ffmpegHasDeadline || !service.renderHasDeadline {
		t.Fatalf("render deadlines: probe=%v ffmpeg=%v render=%v", service.probeHasDeadline, service.ffmpegHasDeadline, service.renderHasDeadline)
	}
	var got streamRenderResult
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout.String())
	}
	if got.PublishDir != want.PublishDir || len(got.Videos) != 1 {
		t.Fatalf("got = %#v, want %#v", got, want)
	}
}

func TestRunStreamRenderReportsRenderFailureAsRuntimeError(t *testing.T) {
	dir := t.TempDir()
	planPath := writeValidStreamPlan(t, dir, 10)
	service := &fakeStreamService{
		probe:     streamclips.SourceProbe{DurationSeconds: 10},
		renderErr: errors.New("encoder stopped"),
	}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"render", "--input", "stream.mp4", "--plan", planPath,
		"--out", filepath.Join(dir, "run"), "--format", "json",
	}, &stdout, &stderr, service)
	if code != exitUnexpected || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result streamErrorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Error, "encoder stopped") || service.renderCalls != 1 {
		t.Fatalf("result = %#v render calls = %d", result, service.renderCalls)
	}
}

func TestRunStreamRenderRejectsInvalidTimeoutBeforeIO(t *testing.T) {
	service := &fakeStreamService{}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"render", "--input", "stream.mp4", "--plan", "missing.json",
		"--out", "run", "--timeout", "eventually",
	}, &stdout, &stderr, service)
	if code != exitInvalidArgs || !strings.Contains(stderr.String(), "invalid --timeout") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if service.probeCalls != 0 || service.renderCalls != 0 {
		t.Fatalf("probe calls = %d render calls = %d, want 0, 0", service.probeCalls, service.renderCalls)
	}
}

func TestPublishLocalStreamResultReplacesStalePack(t *testing.T) {
	outDir := t.TempDir()
	store, err := storage.NewLocal(outDir)
	if err != nil {
		t.Fatal(err)
	}
	for key, body := range map[string]string{
		"worker/clip-001.mp4":                             "new-video",
		"shortslistosparasubir/old.mp4":                   "old-video",
		"shortslistosparasubir/captions/old.ass":          "old-caption",
		"shortslistosparasubir/stream-render-result.json": "old-manifest",
	} {
		if err := store.Put(key, strings.NewReader(body)); err != nil {
			t.Fatalf("put %s: %v", key, err)
		}
	}
	job := streamclips.Job{ID: uuid.New(), Title: "test pack"}
	plan := streamclips.DefaultEditPlan()
	result, err := publishLocalStreamResult(context.Background(), store, job, streamRenderRequest{
		Input: "stream.mp4", PlanPath: "edit-plan.json", OutDir: outDir, Plan: plan,
	}, streamclips.RenderResult{Clips: []streamclips.VideoEntry{{
		ClipID: "clip-001", Key: "worker/clip-001.mp4", DurationSeconds: 1,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"shortslistosparasubir/old.mp4",
		"shortslistosparasubir/captions/old.ass",
	} {
		exists, err := store.Exists(key)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("stale artifact remains: %s", key)
		}
	}
	for _, key := range []string{
		"shortslistosparasubir/clip-001.mp4",
		"shortslistosparasubir/index.html",
		"shortslistosparasubir/stream-render-result.json",
	} {
		exists, err := store.Exists(key)
		if err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Fatalf("published artifact missing: %s", key)
		}
	}
	if result.Warnings == nil {
		t.Fatal("warnings = nil, want an empty JSON array")
	}
}

func TestPublishLocalStreamResultCopiesCaptionSidecarForCaptionedVideo(t *testing.T) {
	outDir := t.TempDir()
	store, err := storage.NewLocal(outDir)
	if err != nil {
		t.Fatal(err)
	}
	job := streamclips.Job{ID: uuid.New(), Title: "captioned pack"}
	plan := streamclips.DefaultEditPlan()
	videoKey := "worker/clip-001_captioned.mp4"
	if err := store.Put(videoKey, strings.NewReader("captioned-video")); err != nil {
		t.Fatal(err)
	}
	captionKey, err := streamclips.RenderCaptionKey(job.ID, plan.Variant, "clip-001")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(captionKey, strings.NewReader("reviewed-ass")); err != nil {
		t.Fatal(err)
	}

	result, err := publishLocalStreamResult(context.Background(), store, job, streamRenderRequest{
		Input: "stream.mp4", PlanPath: "edit-plan.json", OutDir: outDir, Plan: plan,
	}, streamclips.RenderResult{Clips: []streamclips.VideoEntry{{
		ClipID: "clip-001", Key: videoKey, DurationSeconds: 1,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Videos) != 1 || !strings.HasSuffix(result.Videos[0].Path, "clip-001_captioned.mp4") {
		t.Fatalf("videos = %#v, want captioned video filename", result.Videos)
	}
	if !strings.HasSuffix(result.Videos[0].CaptionsPath, filepath.Join("captions", "clip-001.ass")) {
		t.Fatalf("captions_path = %q, want original clip ID sidecar", result.Videos[0].CaptionsPath)
	}
	caption, err := os.ReadFile(result.Videos[0].CaptionsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(caption), "reviewed-ass"; got != want {
		t.Fatalf("caption sidecar = %q, want %q", got, want)
	}
}

type fakeStreamCoverGenerator struct {
	calls int
	at    float64
}

func (f *fakeStreamCoverGenerator) Generate(_ context.Context, _, _, coverPath string, atSeconds float64) error {
	f.calls++
	f.at = atSeconds
	return os.WriteFile(coverPath, []byte("cover"), 0o600)
}

func TestPublishLocalStreamResultGeneratesCoverAtStrongestKill(t *testing.T) {
	outDir := t.TempDir()
	store, err := storage.NewLocal(outDir)
	if err != nil {
		t.Fatal(err)
	}
	videoKey := "worker/clip-001.mp4"
	if err := store.Put(videoKey, strings.NewReader("video")); err != nil {
		t.Fatal(err)
	}
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      20,
		KillfeedSeconds: []float64{12, 16},
		KillfeedKills: [][]streamclips.KillfeedKill{
			{{AttackerName: "one"}},
			{{AttackerName: "one"}, {AttackerName: "two"}},
		},
		Edit: &streamclips.ClipEdit{Speed: 2},
	}}
	generator := &fakeStreamCoverGenerator{}
	result, err := publishLocalStreamResult(context.Background(), store, streamclips.Job{ID: uuid.New()}, streamRenderRequest{
		Input: "stream.mp4", PlanPath: "edit-plan.json", OutDir: outDir,
		Plan: plan, FFmpeg: "ffmpeg", CoverGenerator: generator,
	}, streamclips.RenderResult{Clips: []streamclips.VideoEntry{{
		ClipID: "clip-001", Key: videoKey, DurationSeconds: 5,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if generator.calls != 1 {
		t.Fatalf("cover calls = %d, want 1", generator.calls)
	}
	if got, want := generator.at, 3.125; math.Abs(got-want) > 0.0001 {
		t.Fatalf("cover timestamp = %.3f, want %.3f", got, want)
	}
	if len(result.Videos) != 1 || !strings.HasSuffix(result.Videos[0].CoverPath, "clip-001.cover.jpg") {
		t.Fatalf("videos = %#v, want generated cover path", result.Videos)
	}
	cover, err := os.ReadFile(result.Videos[0].CoverPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(cover), "cover"; got != want {
		t.Fatalf("cover = %q, want %q", got, want)
	}
	gallery, err := os.ReadFile(result.Gallery)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gallery), "clip-001.cover.jpg") {
		t.Fatalf("gallery does not reference cover: %s", gallery)
	}
}

func TestStreamCoverTimestampIgnoresUnconfirmedDetectionCues(t *testing.T) {
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 10,
		KillfeedSeconds: []float64{1, 8},
	}}
	if got, want := streamCoverTimestamp(plan, "clip-001", 10), 3.5; math.Abs(got-want) > 0.0001 {
		t.Fatalf("cover timestamp = %.3f, want fallback %.3f", got, want)
	}
}

func TestRunStreamJSONErrorStaysOnStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{"render", "--format", "json"}, &stdout, &stderr, &fakeStreamService{})
	if code != exitInvalidArgs || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	var result streamErrorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Executed || !strings.Contains(result.Error, "required") {
		t.Fatalf("result = %#v", result)
	}
}

func writeValidStreamPlan(t *testing.T, dir string, duration float64) string {
	t.Helper()
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: duration}}
	body, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "edit-plan.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
