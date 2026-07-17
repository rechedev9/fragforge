package streamcli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

func TestRunStreamTranscribeWritesRequiresReviewDocument(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "edit-plan.json")
	modelPath := filepath.Join(dir, "whisper.bin")
	vadPath := filepath.Join(dir, "vad.bin")
	outPath := filepath.Join(dir, "transcript-review.json")
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 2, EndSeconds: 8}}
	writeJSONFile(t, planPath, plan)
	for _, path := range []string{modelPath, vadPath} {
		if err := os.WriteFile(path, []byte("model"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	wantReview := streamTranscriptReview{
		SchemaVersion: streamTranscriptReviewSchemaVersion,
		ReviewStatus:  "requires_review",
		ClipID:        "clip-001",
		Language:      "es",
		Input:         "stream.mp4",
		StartSeconds:  2,
		EndSeconds:    8,
		Passes: []streamTranscriptPass{{
			AudioPass: "raw", Model: "whisper.bin", ModelSHA256: "abc",
			Segments: []streamTranscriptSegment{{StartSeconds: 1, EndSeconds: 1.5, Text: "Toma"}},
		}},
		Warnings:    []string{"review every word"},
		GeneratedAt: time.Unix(1, 0).UTC(),
	}
	service := &fakeStreamService{
		probe:      streamclips.SourceProbe{DurationSeconds: 10, VideoCodec: "h264", AudioCodec: "aac"},
		transcript: wantReview,
	}

	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"transcribe", "--input", "stream.mp4", "--plan", planPath,
		"--model", modelPath, "--vad-model", vadPath, "--out", outPath,
		"--format", "json",
	}, &stdout, &stderr, service)
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if service.transcribeCalls != 1 || service.transcribeRequest.Clip.ID != "clip-001" {
		t.Fatalf("transcribe calls = %d request = %#v", service.transcribeCalls, service.transcribeRequest)
	}
	if !service.probeHasDeadline || !service.ffmpegHasDeadline || !service.transcribeDeadline {
		t.Fatalf("timeout deadlines: probe=%v ffmpeg=%v transcribe=%v", service.probeHasDeadline, service.ffmpegHasDeadline, service.transcribeDeadline)
	}
	var persisted streamTranscriptReview
	if err := readStrictJSON(outPath, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.ReviewStatus != "requires_review" || len(persisted.Passes) != 1 {
		t.Fatalf("persisted review = %#v", persisted)
	}
	var result streamTranscribeResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if !result.OK || result.DryRun || !result.Executed || result.Review.ReviewStatus != "requires_review" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunStreamTranscribeDryRunDoesNotInvokeWhisper(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "edit-plan.json")
	modelPath := filepath.Join(dir, "whisper.bin")
	vadPath := filepath.Join(dir, "vad.bin")
	outPath := filepath.Join(dir, "transcript-review.json")
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 5}}
	writeJSONFile(t, planPath, plan)
	for _, path := range []string{modelPath, vadPath} {
		if err := os.WriteFile(path, []byte("model"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := &fakeStreamService{probe: streamclips.SourceProbe{DurationSeconds: 5, AudioCodec: "aac"}}

	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"transcribe", "--input", "stream.mp4", "--plan", planPath,
		"--model", modelPath, "--vad-model", vadPath, "--out", outPath,
		"--dry-run", "--format", "json",
	}, &stdout, &stderr, service)
	if code != exitSuccess || stderr.Len() != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if service.transcribeCalls != 0 {
		t.Fatalf("transcribe calls = %d, want 0", service.transcribeCalls)
	}
	if service.ffmpegChecks != 1 || !service.whisperRequired {
		t.Fatalf("ffmpeg checks = %d whisper = %v, want one Whisper-capability check", service.ffmpegChecks, service.whisperRequired)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run output stat error = %v, want not exist", err)
	}
	var result streamTranscribeResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || result.Executed || result.Review.ReviewStatus != "requires_review" {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunStreamTranscribeDryRunRejectsFFmpegWithoutWhisper(t *testing.T) {
	dir := t.TempDir()
	planPath := filepath.Join(dir, "edit-plan.json")
	modelPath := filepath.Join(dir, "whisper.bin")
	vadPath := filepath.Join(dir, "vad.bin")
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 5}}
	writeJSONFile(t, planPath, plan)
	for _, path := range []string{modelPath, vadPath} {
		if err := os.WriteFile(path, []byte("model"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := &fakeStreamService{
		probe:     streamclips.SourceProbe{DurationSeconds: 5, AudioCodec: "aac"},
		ffmpegErr: errors.New("whisper filter unavailable"),
	}
	var stdout, stderr bytes.Buffer
	code := runStreamWithService([]string{
		"transcribe", "--input", "stream.mp4", "--plan", planPath,
		"--model", modelPath, "--vad-model", vadPath, "--out", filepath.Join(dir, "review.json"),
		"--ffmpeg", "ffmpeg-without-whisper", "--dry-run", "--format", "json",
	}, &stdout, &stderr, service)
	if code != exitUnexpected || stderr.Len() != 0 || service.transcribeCalls != 0 {
		t.Fatalf("code = %d, stderr = %q, transcribe calls = %d", code, stderr.String(), service.transcribeCalls)
	}
	var result streamErrorResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Error, "whisper filter unavailable") || !service.whisperRequired {
		t.Fatalf("result = %#v whisper required = %v", result, service.whisperRequired)
	}
}

func TestFFmpegHasFilterMatchesFilterColumnOnly(t *testing.T) {
	output := []byte("Filters:\n .. whisper A->A Transcribe audio\n .. other A->A mentions whisper in description\n")
	if !ffmpegHasFilter(output, "whisper") {
		t.Fatal("ffmpegHasFilter did not find the whisper filter")
	}
	if ffmpegHasFilter(output, "mentions") {
		t.Fatal("ffmpegHasFilter matched description text instead of the filter column")
	}
}

func TestParseWhisperSRTPreservesQuotesBackslashesAndClampsDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "whisper.srt")
	body := strings.Join([]string{
		"0",
		"00:00:00.100 --> 00:00:00.500",
		`Dijo "vamos" en C:\B`,
		"",
		"1",
		"00:00:00,500 --> 00:00:00,600",
		"...",
		"",
		"2",
		"00:00:00.900 --> 00:00:01.400",
		"dos",
		"líneas",
		"",
		"3",
		"00:00:01.400 --> 00:00:01.400",
		"cero",
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := parseWhisperSRT(path, 1.2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("segments = %#v, want 2", got)
	}
	if got[0].Text != `Dijo "vamos" en C:\B` || got[1].Text != "dos líneas" || got[1].EndSeconds != 1.2 {
		t.Fatalf("segments = %#v", got)
	}
}

func TestEscapeWhisperPathEscapesBothFilterParsingLevels(t *testing.T) {
	got := escapeWhisperPathForOS(`C:\Models\o'clock.bin`, "windows")
	want := `C\\:/Models/o\\\'clock.bin`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEscapeWhisperPathPreservesLiteralUnixBackslashes(t *testing.T) {
	got := escapeWhisperPathForOS(`/tmp/model\v1.bin`, "linux")
	want := `/tmp/model\\\\v1.bin`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeTranscriptLanguageUsesWhisperBaseID(t *testing.T) {
	tests := map[string]string{
		"es":    "es",
		"EN-us": "en",
		"pt-BR": "pt",
		"auto":  "auto",
	}
	for input, want := range tests {
		if got := normalizeTranscriptLanguage(input); got != want {
			t.Fatalf("normalizeTranscriptLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCreateTranscriptRunDirIsolatesCallerWorkDirectory(t *testing.T) {
	baseDir := t.TempDir()
	sentinel := filepath.Join(baseDir, "clip-raw-16k.wav")
	if err := os.WriteFile(sentinel, []byte("user-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	runDir, err := createTranscriptRunDir(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(runDir)
	if filepath.Clean(runDir) == filepath.Clean(baseDir) || filepath.Dir(runDir) != filepath.Clean(baseDir) {
		t.Fatalf("run dir = %q, want unique child of %q", runDir, baseDir)
	}
	body, err := os.ReadFile(sentinel)
	if err != nil || string(body) != "user-data" {
		t.Fatalf("sentinel = %q, error = %v", body, err)
	}
}

func TestValidTranscriptLanguageRejectsFilterInjection(t *testing.T) {
	for _, language := range []string{"es", "en-US", "auto"} {
		if !validTranscriptLanguage(language) {
			t.Fatalf("validTranscriptLanguage(%q) = false, want true", language)
		}
	}
	for _, language := range []string{"", "e", "es:destination=other", "es/../../"} {
		if validTranscriptLanguage(language) {
			t.Fatalf("validTranscriptLanguage(%q) = true, want false", language)
		}
	}
}
