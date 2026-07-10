package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/captions"
	"github.com/rechedev9/fragforge/internal/mediafont"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
	"github.com/rechedev9/fragforge/internal/vodfetch"
)

// fakeStreamRepo implements StreamRenderRepository and StreamAcquireRepository
// for tests.
type fakeStreamRepo struct {
	jobs map[uuid.UUID]streamclips.Job
}

func newFakeStreamRepo(jobs ...streamclips.Job) *fakeStreamRepo {
	f := &fakeStreamRepo{jobs: map[uuid.UUID]streamclips.Job{}}
	for _, j := range jobs {
		f.jobs[j.ID] = j
	}
	return f
}

func (f *fakeStreamRepo) Get(_ context.Context, id uuid.UUID) (streamclips.Job, error) {
	j, ok := f.jobs[id]
	if !ok {
		return streamclips.Job{}, streamclips.ErrNotFound
	}
	return j, nil
}

func (f *fakeStreamRepo) UpdateStatus(_ context.Context, id uuid.UUID, s streamclips.Status, reason string) error {
	j, ok := f.jobs[id]
	if !ok {
		return streamclips.ErrNotFound
	}
	j.Status = s
	j.FailureReason = reason
	f.jobs[id] = j
	return nil
}

func (f *fakeStreamRepo) SetAcquired(_ context.Context, id uuid.UUID, probe streamclips.SourceProbe, sha256 string) error {
	j, ok := f.jobs[id]
	if !ok {
		return streamclips.ErrNotFound
	}
	j.Probe = probe
	j.SourceSHA256 = sha256
	j.Status = streamclips.StatusReady
	j.FailureReason = ""
	f.jobs[id] = j
	return nil
}

// fakeVodfetchRunner implements vodfetch.CommandRunner: it "downloads" by
// writing fixed content to the -o destination, so tests never shell out to a
// real yt-dlp binary.
type fakeVodfetchRunner struct {
	content string
	stderr  string
	err     error
}

func (f *fakeVodfetchRunner) Run(_ context.Context, _, _ string, args ...string) (string, string, error) {
	if f.err != nil {
		return "", f.stderr, f.err
	}
	dest := argValue(args, "-o")
	if dest == "" {
		return "", "", fmt.Errorf("fake yt-dlp: missing -o arg")
	}
	if err := os.WriteFile(dest, []byte(f.content), 0o600); err != nil {
		return "", "", err
	}
	return "", "", nil
}

type fakeProber struct {
	probe streamclips.SourceProbe
	err   error
}

func (f fakeProber) Probe(context.Context, string) (streamclips.SourceProbe, error) {
	return f.probe, f.err
}

func streamAcquireTask(t *testing.T, id uuid.UUID) *asynq.Task {
	t.Helper()
	task, err := tasks.NewStreamAcquireTask(id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestAcquireWorkerDownloadsProbesAndMarksReady(t *testing.T) {
	id := uuid.New()
	repo := newFakeStreamRepo(streamclips.Job{ID: id, Status: streamclips.StatusAcquiring, SourceURL: "https://clips.twitch.tv/SomeSlug"})
	store := newFakeStorage()

	w := NewAcquireWorker(repo, store, AcquireWorkerConfig{WorkDir: t.TempDir()})
	w.fetcher = vodfetch.Fetcher{Runner: &fakeVodfetchRunner{content: "fake-mp4-bytes"}}
	w.prober = fakeProber{probe: streamclips.SourceProbe{Width: 1920, Height: 1080, DurationSeconds: 12.5}}

	if err := w.HandleStreamAcquire(context.Background(), streamAcquireTask(t, id)); err != nil {
		t.Fatalf("HandleStreamAcquire error = %v", err)
	}

	got := repo.jobs[id]
	if got.Status != streamclips.StatusReady {
		t.Fatalf("status = %s, want ready", got.Status)
	}
	if got.SourceSHA256 == "" {
		t.Fatal("source sha256 not set")
	}
	if got.Probe.Width != 1920 || got.Probe.Height != 1080 {
		t.Fatalf("probe = %#v, want 1920x1080", got.Probe)
	}
	if _, ok := store.files[streamclips.SourceKey(id)]; !ok {
		t.Fatal("storage missing uploaded source")
	}
	if _, ok := store.files[streamclips.EditPlanKey(id)]; !ok {
		t.Fatal("storage missing default edit plan artifact")
	}
}

func TestAcquireWorkerFailureRecordsReasonAndObs(t *testing.T) {
	id := uuid.New()
	repo := newFakeStreamRepo(streamclips.Job{ID: id, Status: streamclips.StatusAcquiring, SourceURL: "https://clips.twitch.tv/SomeSlug"})
	store := newFakeStorage()

	w := NewAcquireWorker(repo, store, AcquireWorkerConfig{WorkDir: t.TempDir()})
	w.fetcher = vodfetch.Fetcher{Runner: &fakeVodfetchRunner{stderr: "HTTP Error 404: Not Found", err: fmt.Errorf("exit status 1")}}

	err := w.HandleStreamAcquire(context.Background(), streamAcquireTask(t, id))
	if err == nil {
		t.Fatal("HandleStreamAcquire error = nil, want error")
	}

	got := repo.jobs[id]
	if got.Status != streamclips.StatusFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	if got.FailureReason == "" {
		t.Fatal("failure reason not set")
	}
	// The stored reason must be the clean, user-facing "not found" message, not
	// the raw yt-dlp stderr the user reported seeing dumped into the failed card.
	if !strings.Contains(got.FailureReason, "No encontramos un vídeo") {
		t.Fatalf("failure reason = %q, want the friendly not-found message", got.FailureReason)
	}
	if strings.Contains(got.FailureReason, "HTTP Error 404") {
		t.Fatalf("failure reason = %q, leaked the raw yt-dlp stderr", got.FailureReason)
	}
}

func TestAcquireWorkerIsIdempotentWhenSourceAlreadyExists(t *testing.T) {
	id := uuid.New()
	repo := newFakeStreamRepo(streamclips.Job{ID: id, Status: streamclips.StatusAcquiring, SourceURL: "https://clips.twitch.tv/SomeSlug"})
	store := newFakeStorage()
	_ = store.Put(streamclips.SourceKey(id), strings.NewReader("already-downloaded"))

	runner := &fakeVodfetchRunner{content: "should-not-be-used"}
	w := NewAcquireWorker(repo, store, AcquireWorkerConfig{WorkDir: t.TempDir()})
	w.fetcher = vodfetch.Fetcher{Runner: runner}
	w.prober = fakeProber{probe: streamclips.SourceProbe{Width: 1280, Height: 720}}

	if err := w.HandleStreamAcquire(context.Background(), streamAcquireTask(t, id)); err != nil {
		t.Fatalf("HandleStreamAcquire error = %v", err)
	}

	if string(store.files[streamclips.SourceKey(id)]) != "already-downloaded" {
		t.Fatalf("source artifact overwritten: %q", store.files[streamclips.SourceKey(id)])
	}
	if repo.jobs[id].Status != streamclips.StatusReady {
		t.Fatalf("status = %s, want ready", repo.jobs[id].Status)
	}
}

// --- StreamRenderWorker captions pass -------------------------------------

func newReadyStreamJobWithCaptions(t *testing.T, store *fakeStorage, enabled bool) (uuid.UUID, streamclips.EditPlan) {
	t.Helper()
	id := uuid.New()
	_ = store.Put(streamclips.SourceKey(id), strings.NewReader("source-bytes"))
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 2, Title: "one"}}
	plan.Captions = streamclips.CaptionsPlan{Enabled: enabled, Language: "en"}
	return id, plan
}

func TestStreamRenderWorkerBurnsCaptionsAndPublishesCaptionedClip(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
	})

	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		// Both the render pass and the caption burn pass write to the last arg.
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(out, []byte("video-bytes"), 0o644); err != nil {
			return nil, err
		}
		return nil, nil
	}}

	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir:          t.TempDir(),
		FFmpegPath:       "ffmpeg",
		WhisperPath:      "whisper-cli",
		WhisperModelPath: "model.bin",
	})
	w.runner = runner
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		return []captions.WordCue{
			{Word: "gg", StartSeconds: 0.75, EndSeconds: 1},
			{Word: "nice", StartSeconds: 0, EndSeconds: 0.5},
		}, nil
	}

	task, err := tasks.NewRenderStreamClipTask(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderStreamClip error = %v", err)
	}

	if len(runner.calls) != 2 {
		t.Fatalf("runner calls = %d, want 2 (render + caption burn)", len(runner.calls))
	}
	burnArgs := runner.calls[1].args
	if got := argValue(burnArgs, "-vf"); !strings.HasPrefix(got, "ass=") || !strings.Contains(got, ":fontsdir='") {
		t.Fatalf("caption burn -vf = %q, want an ass= filter with fontsdir", got)
	}
	if out := burnArgs[len(burnArgs)-1]; !strings.HasSuffix(out, "clip-001_captioned.mp4") {
		t.Fatalf("caption burn output = %q, want a clip-001_captioned.mp4 path", out)
	}

	wantKey, err := streamclips.RenderVideoKey(id, streamclips.VariantStreamer4060, "clip-001_captioned")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.files[wantKey]; !ok {
		t.Fatalf("storage missing captioned video at %s", wantKey)
	}
	captionKey, err := streamclips.RenderCaptionKey(id, streamclips.VariantStreamer4060, "clip-001")
	if err != nil {
		t.Fatal(err)
	}
	ass, ok := store.files[captionKey]
	if !ok {
		t.Fatal("storage missing .ass caption artifact")
	}
	if !strings.Contains(string(ass), `Dialogue: 0,0:00:00.00,0:00:01.00,Karaoke,,0,0,0,,{\k50}nice {\k25}gg`) {
		t.Fatalf("caption artifact does not order cues chronologically: %s", ass)
	}
	if !strings.Contains(string(ass), "Style: Karaoke,"+mediafont.FamilyName+",") {
		t.Fatalf("caption artifact does not use %s: %s", mediafont.FamilyName, ass)
	}

	resultKey, err := streamclips.RenderResultKey(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(store.files[resultKey]), `clip-001_captioned.mp4`) {
		t.Fatalf("render result does not reference captioned clip: %s", store.files[resultKey])
	}
}

func TestStreamRenderWorkerPublishesUncaptionedOnZeroCueWarning(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
	})

	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("video-bytes"), 0o644)
	}}

	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir:          t.TempDir(),
		FFmpegPath:       "ffmpeg",
		WhisperPath:      "whisper-cli",
		WhisperModelPath: "model.bin",
	})
	w.runner = runner
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		return nil, fmt.Errorf("captions: whisper transcript contains no words")
	}

	task, err := tasks.NewRenderStreamClipTask(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderStreamClip error = %v", err)
	}

	// Only the render pass ran; no caption burn pass since there were no cues.
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1 (render only)", len(runner.calls))
	}
	wantKey, err := streamclips.RenderVideoKey(id, streamclips.VariantStreamer4060, "clip-001")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.files[wantKey]; !ok {
		t.Fatalf("storage missing uncaptioned video at %s", wantKey)
	}

	stateKey, err := streamclips.RenderStateKey(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(store.files[stateKey]), "no words") {
		t.Fatalf("render state missing zero-cue warning: %s", store.files[stateKey])
	}
}

// --- captions backend selection --------------------------------------------

func TestStreamRenderWorkerConfig_CaptionsBackendSelection(t *testing.T) {
	tests := []struct {
		name           string
		cfg            StreamRenderWorkerConfig
		wantWhisper    bool
		wantGroq       bool
		wantCaptioning bool
	}{
		{
			name:           "neither configured",
			cfg:            StreamRenderWorkerConfig{},
			wantWhisper:    false,
			wantGroq:       false,
			wantCaptioning: false,
		},
		{
			name:           "whisper only",
			cfg:            StreamRenderWorkerConfig{WhisperPath: "whisper-cli", WhisperModelPath: "model.bin"},
			wantWhisper:    true,
			wantGroq:       false,
			wantCaptioning: true,
		},
		{
			name:           "whisper missing model path is not configured",
			cfg:            StreamRenderWorkerConfig{WhisperPath: "whisper-cli"},
			wantWhisper:    false,
			wantGroq:       false,
			wantCaptioning: false,
		},
		{
			name:           "groq only",
			cfg:            StreamRenderWorkerConfig{GroqAPIKey: "gsk_test"},
			wantWhisper:    false,
			wantGroq:       true,
			wantCaptioning: true,
		},
		{
			name:           "correction model without api key does not enable groq",
			cfg:            StreamRenderWorkerConfig{GroqCorrectionModel: "llama-test"},
			wantWhisper:    false,
			wantGroq:       false,
			wantCaptioning: false,
		},
		{
			name:           "both configured",
			cfg:            StreamRenderWorkerConfig{GroqAPIKey: "gsk_test", WhisperPath: "whisper-cli", WhisperModelPath: "model.bin"},
			wantWhisper:    true,
			wantGroq:       true,
			wantCaptioning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.whisperConfigured(); got != tt.wantWhisper {
				t.Errorf("whisperConfigured() = %v, want %v", got, tt.wantWhisper)
			}
			if got := tt.cfg.groqConfigured(); got != tt.wantGroq {
				t.Errorf("groqConfigured() = %v, want %v", got, tt.wantGroq)
			}
			if got := tt.cfg.captionsConfigured(); got != tt.wantCaptioning {
				t.Errorf("captionsConfigured() = %v, want %v", got, tt.wantCaptioning)
			}
		})
	}
}

func TestNewStreamRenderWorker_PrefersGroqWhenBothConfigured(t *testing.T) {
	// The transcribe seam is built once in NewStreamRenderWorker from cfg; when
	// both backends are configured it must choose Groq (no local model
	// download, runs on Groq's GPU) rather than local whisper. A GroqTranscriber
	// call fails distinctly (missing/unreachable API) from a Transcriber call
	// (missing binary), so the error text tells them apart without a real
	// network call or whisper binary on disk.
	repo := newFakeStreamRepo()
	store := newFakeStorage()
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		GroqAPIKey:       "gsk_test",
		WhisperPath:      filepath.Join(t.TempDir(), "does-not-exist-whisper-cli.exe"),
		WhisperModelPath: filepath.Join(t.TempDir(), "does-not-exist-model.bin"),
	})

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := w.transcribe(context.Background(), mediaPath, dir, "en")
	if err == nil {
		t.Fatal("transcribe returned nil error, want an error (no real ffmpeg/network in this test)")
	}
	// A Transcriber (local whisper) call on a missing binary fails with
	// "whisper binary not found"; a GroqTranscriber call instead fails trying
	// to extract audio with ffmpeg (or reach the network), never that message.
	if strings.Contains(err.Error(), "whisper binary not found") {
		t.Fatalf("got error %q, selection used local whisper instead of groq", err.Error())
	}
}

func TestStreamRenderWorkerRejectsCaptionsWithNoBackendConfigured(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
	})

	// Neither GroqAPIKey nor Whisper*Path is set: the worker must fail fast
	// (defensively, even though the HTTP layer already gates this) rather than
	// launch ffmpeg and then fail deep into the render.
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{FFmpegPath: "ffmpeg"})

	task, err := tasks.NewRenderStreamClipTask(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	err = w.HandleRenderStreamClip(context.Background(), task)
	if err == nil {
		t.Fatal("HandleRenderStreamClip returned nil error, want an error")
	}
	if !strings.Contains(err.Error(), "no transcription backend is configured") {
		t.Fatalf("got error %q, want it to mention no transcription backend is configured", err.Error())
	}
}
