package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

func TestStreamRenderWorkerRendersSyntheticKillfeedNotices(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, false)
	hint := streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	plan.KillfeedCrop = &hint
	plan.Clips[0].KillfeedSeconds = []float64{0.5}
	weapon := streamclips.WeaponKeys()[0]
	plan.Clips[0].KillfeedKills = [][]streamclips.KillfeedKill{{
		{AttackerSide: "CT", AttackerName: "hero", VictimSide: "T", VictimName: "villain", Weapon: weapon, Headshot: true},
	}}
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
		WorkDir:    t.TempDir(),
		FFmpegPath: "ffmpeg",
	})
	w.runner = runner

	task, err := tasks.NewRenderStreamClipTask(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderStreamClip error = %v", err)
	}

	// The synthetic-notice flow renders notice PNGs in Go; it does not extract
	// cue frames, so only the render pass runs.
	if got, want := len(runner.calls), 1; got != want {
		t.Fatalf("runner calls = %d, want %d (render only)", got, want)
	}
	renderArgs := runner.calls[0].args
	var noticePath string
	for _, a := range renderArgs {
		if strings.HasSuffix(filepath.ToSlash(a), "killfeed/clip-001/cue0_0.png") {
			noticePath = a
		}
	}
	if noticePath == "" {
		t.Fatalf("render args missing notice PNG input: %v", renderArgs)
	}
	renderFilter := argValue(renderArgs, "-filter_complex")
	if !strings.Contains(renderFilter, "notice0_0") || !strings.Contains(renderFilter, "overlay") {
		t.Fatalf("render filter missing synthetic notice overlay: %s", renderFilter)
	}
	for _, forbidden := range []string{"killfeedin", "scale=620:-2"} {
		if strings.Contains(renderFilter, forbidden) {
			t.Fatalf("synthetic-notice render fell back to a frozen crop (%q): %s", forbidden, renderFilter)
		}
	}
	data, err := os.ReadFile(noticePath)
	if err != nil {
		t.Fatalf("read rendered notice PNG: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("notice file is not a PNG: % x", data[:min(8, len(data))])
	}
}

func TestStreamRenderWorkerFreezesKillfeedCropWithoutKills(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, false)
	hint := streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	plan.KillfeedCrop = &hint
	plan.Clips[0].KillfeedSeconds = []float64{0.5}
	// No KillfeedKills: the render must fall back to a frozen crop of the
	// killfeed region rather than extract or detect anything.
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
		WorkDir:    t.TempDir(),
		FFmpegPath: "ffmpeg",
	})
	w.runner = runner

	task, err := tasks.NewRenderStreamClipTask(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderStreamClip error = %v", err)
	}

	if got, want := len(runner.calls), 1; got != want {
		t.Fatalf("runner calls = %d, want %d (render only)", got, want)
	}
	renderFilter := argValue(runner.calls[0].args, "-filter_complex")
	if !strings.Contains(renderFilter, "killfeedin0") || !strings.Contains(renderFilter, "scale=620:-2") {
		t.Fatalf("frozen-crop fallback filter missing: %s", renderFilter)
	}
	for _, a := range runner.calls[0].args {
		if strings.Contains(filepath.ToSlash(a), "/killfeed/clip-001/cue") {
			t.Fatalf("frozen-crop fallback unexpectedly rendered a notice PNG: %q", a)
		}
	}
}

func TestStreamRenderWorkerBurnsCaptionsAndPublishesCaptionedClip(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	plan.Clips[0].StartSeconds = 1.25
	plan.Clips[0].EndSeconds = 3.25
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
		Probe: streamclips.SourceProbe{AudioCodec: "aac"},
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
		WorkDir:    t.TempDir(),
		FFmpegPath: "ffmpeg",
		XAIAPIKey:  "xai_test",
	})
	w.runner = runner
	var transcriptionPath string
	w.transcribe = func(_ context.Context, mediaPath, _, _ string) ([]captions.WordCue, error) {
		transcriptionPath = mediaPath
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

	if len(runner.calls) != 3 {
		t.Fatalf("runner calls = %d, want 3 (render + source audio extraction + caption burn)", len(runner.calls))
	}
	extractArgs := runner.calls[1].args
	if got, want := argValue(extractArgs, "-ss"), "1.250"; got != want {
		t.Fatalf("caption audio -ss = %q, want %q", got, want)
	}
	if got, want := argValue(extractArgs, "-t"), "2.000"; got != want {
		t.Fatalf("caption audio -t = %q, want %q", got, want)
	}
	if got, want := argValue(extractArgs, "-map"), "0:a:0"; got != want {
		t.Fatalf("caption audio -map = %q, want %q", got, want)
	}
	if got, want := argValue(extractArgs, "-c:a"), "pcm_s16le"; got != want {
		t.Fatalf("caption audio codec = %q, want %q", got, want)
	}
	if got, want := argValue(extractArgs, "-ac"), "1"; got != want {
		t.Fatalf("caption audio channels = %q, want %q", got, want)
	}
	if got, want := argValue(extractArgs, "-ar"), "16000"; got != want {
		t.Fatalf("caption audio sample rate = %q, want %q", got, want)
	}
	if !strings.HasSuffix(transcriptionPath, "clip-001.wav") {
		t.Fatalf("transcription path = %q, want original-range WAV", transcriptionPath)
	}
	burnArgs := runner.calls[2].args
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

func TestStreamRenderWorkerPublishesUncaptionedWhenXAISourceHasNoAudio(t *testing.T) {
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
		WorkDir:    t.TempDir(),
		FFmpegPath: "ffmpeg",
		XAIAPIKey:  "xai_test",
	})
	w.runner = runner
	transcribeCalled := false
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		transcribeCalled = true
		return nil, errors.New("unexpected transcription")
	}

	task, err := tasks.NewRenderStreamClipTask(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderStreamClip error = %v", err)
	}
	if transcribeCalled {
		t.Fatal("xai transcription ran for a source with no audio")
	}
	if got, want := len(runner.calls), 1; got != want {
		t.Fatalf("runner calls = %d, want %d render call", got, want)
	}
	stateKey, err := streamclips.RenderStateKey(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(store.files[stateKey]), "source has no audio") {
		t.Fatalf("render state missing no-audio warning: %s", store.files[stateKey])
	}
	wantKey, err := streamclips.RenderVideoKey(id, streamclips.VariantStreamer4060, "clip-001")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.files[wantKey]; !ok {
		t.Fatalf("storage missing uncaptioned video at %s", wantKey)
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
		wantXAI        bool
		wantGroq       bool
		wantCaptioning bool
	}{
		{
			name:           "none configured",
			cfg:            StreamRenderWorkerConfig{},
			wantWhisper:    false,
			wantXAI:        false,
			wantGroq:       false,
			wantCaptioning: false,
		},
		{
			name:           "xai only",
			cfg:            StreamRenderWorkerConfig{XAIAPIKey: "xai_test"},
			wantWhisper:    false,
			wantXAI:        true,
			wantGroq:       false,
			wantCaptioning: true,
		},
		{
			name:           "groq only",
			cfg:            StreamRenderWorkerConfig{GroqAPIKey: "groq_test"},
			wantWhisper:    false,
			wantXAI:        false,
			wantGroq:       true,
			wantCaptioning: true,
		},
		{
			name:           "whisper only",
			cfg:            StreamRenderWorkerConfig{WhisperPath: "whisper-cli", WhisperModelPath: "model.bin"},
			wantWhisper:    true,
			wantXAI:        false,
			wantGroq:       false,
			wantCaptioning: true,
		},
		{
			name:           "whisper missing model path is not configured",
			cfg:            StreamRenderWorkerConfig{WhisperPath: "whisper-cli"},
			wantWhisper:    false,
			wantXAI:        false,
			wantGroq:       false,
			wantCaptioning: false,
		},
		{
			name:           "all configured",
			cfg:            StreamRenderWorkerConfig{XAIAPIKey: "xai_test", GroqAPIKey: "groq_test", WhisperPath: "whisper-cli", WhisperModelPath: "model.bin"},
			wantWhisper:    true,
			wantXAI:        true,
			wantGroq:       true,
			wantCaptioning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.whisperConfigured(); got != tt.wantWhisper {
				t.Errorf("whisperConfigured() = %v, want %v", got, tt.wantWhisper)
			}
			if got := tt.cfg.xaiConfigured(); got != tt.wantXAI {
				t.Errorf("xaiConfigured() = %v, want %v", got, tt.wantXAI)
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

func TestStreamRenderWorkerConfigCaptionBackendOrder(t *testing.T) {
	tests := []struct {
		name string
		cfg  StreamRenderWorkerConfig
		want []string
	}{
		{
			name: "none configured",
			cfg:  StreamRenderWorkerConfig{},
			want: nil,
		},
		{
			name: "xai only",
			cfg:  StreamRenderWorkerConfig{XAIAPIKey: "xai_test"},
			want: []string{"xai"},
		},
		{
			name: "groq only",
			cfg:  StreamRenderWorkerConfig{GroqAPIKey: "groq_test"},
			want: []string{"groq"},
		},
		{
			name: "xai preferred, then groq, then whisper",
			cfg: StreamRenderWorkerConfig{
				XAIAPIKey:        "xai_test",
				GroqAPIKey:       "groq_test",
				WhisperPath:      "whisper-cli",
				WhisperModelPath: "model.bin",
			},
			want: []string{"xai", "groq", "whisper"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, b := range tt.cfg.captionBackends("auto") {
				got = append(got, b.name)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("captionBackends() order = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTranscribeCaptionsWithFallbackFirstSuccessSkipsRest(t *testing.T) {
	want := []captions.WordCue{{Word: "nice", StartSeconds: 0.5, EndSeconds: 1}}
	var calls []string
	backends := []captionBackend{
		{name: "xai", transcribe: func(context.Context, string, string) ([]captions.WordCue, error) {
			calls = append(calls, "xai")
			return want, nil
		}},
		{name: "groq", transcribe: func(context.Context, string, string) ([]captions.WordCue, error) {
			calls = append(calls, "groq")
			return nil, errors.New("must not be called")
		}},
	}
	got, err := transcribeCaptionsWithFallback(context.Background(), "clip.wav", t.TempDir(), backends)
	if err != nil {
		t.Fatalf("transcribeCaptionsWithFallback error = %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("got cues %v, want %v", got, want)
	}
	if !slices.Equal(calls, []string{"xai"}) {
		t.Errorf("backend calls = %v, want only xai", calls)
	}
}

func TestTranscribeCaptionsWithFallbackUsesNextBackendOnNoWords(t *testing.T) {
	// Regression for a real clip with sparse Spanish speech ("Hola Martínez" in
	// 15s of gameplay audio): xAI returned an empty transcript on every attempt
	// while Groq transcribed it. The chain must fall back to the next
	// configured backend instead of publishing the clip uncaptioned.
	want := []captions.WordCue{{Word: "Hola", StartSeconds: 1, EndSeconds: 1.4}, {Word: "Martínez", StartSeconds: 1.4, EndSeconds: 2}}
	var calls []string
	backends := []captionBackend{
		{name: "xai", transcribe: func(context.Context, string, string) ([]captions.WordCue, error) {
			calls = append(calls, "xai")
			return nil, fmt.Errorf("captions: xai transcript contains no words")
		}},
		{name: "groq", transcribe: func(context.Context, string, string) ([]captions.WordCue, error) {
			calls = append(calls, "groq")
			return want, nil
		}},
	}
	got, err := transcribeCaptionsWithFallback(context.Background(), "clip.wav", t.TempDir(), backends)
	if err != nil {
		t.Fatalf("transcribeCaptionsWithFallback error = %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("got cues %v, want %v", got, want)
	}
	if !slices.Equal(calls, []string{"xai", "groq"}) {
		t.Errorf("backend calls = %v, want xai then groq", calls)
	}
}

func TestTranscribeCaptionsWithFallbackAllNoWordsPublishesUncaptioned(t *testing.T) {
	backends := []captionBackend{
		{name: "xai", transcribe: func(context.Context, string, string) ([]captions.WordCue, error) {
			return nil, fmt.Errorf("captions: xai transcript contains no words")
		}},
		{name: "whisper", transcribe: func(context.Context, string, string) ([]captions.WordCue, error) {
			return nil, fmt.Errorf("captions: whisper transcript contains no words")
		}},
	}
	_, err := transcribeCaptionsWithFallback(context.Background(), "clip.wav", t.TempDir(), backends)
	if err == nil {
		t.Fatal("transcribeCaptionsWithFallback returned nil error, want a no-words error")
	}
	// burnClipCaptions keys on this substring to publish the clip uncaptioned
	// instead of failing the render.
	if !strings.Contains(err.Error(), "no words") {
		t.Errorf("got error %q, want it to contain \"no words\"", err.Error())
	}
}

func TestTranscribeCaptionsWithFallbackHardErrorFailsRender(t *testing.T) {
	// One backend finding no words must not mask another backend's hard
	// failure as an "empty transcript" warning: captions were explicitly
	// requested, so the render fails and the user sees the real error.
	backends := []captionBackend{
		{name: "xai", transcribe: func(context.Context, string, string) ([]captions.WordCue, error) {
			return nil, fmt.Errorf("captions: xai transcript contains no words")
		}},
		{name: "groq", transcribe: func(context.Context, string, string) ([]captions.WordCue, error) {
			return nil, fmt.Errorf("captions: groq transcription request failed (status 500)")
		}},
	}
	_, err := transcribeCaptionsWithFallback(context.Background(), "clip.wav", t.TempDir(), backends)
	if err == nil {
		t.Fatal("transcribeCaptionsWithFallback returned nil error, want an error")
	}
	if strings.Contains(err.Error(), "no words") {
		t.Errorf("got error %q, want the hard failure not to be downgraded to a no-words warning", err.Error())
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("got error %q, want it to surface the groq failure", err.Error())
	}
}

func TestTranscribeCaptionsWithFallbackStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls []string
	backends := []captionBackend{
		{name: "xai", transcribe: func(context.Context, string, string) ([]captions.WordCue, error) {
			calls = append(calls, "xai")
			cancel()
			return nil, fmt.Errorf("captions: xai transcription interrupted: %w", context.Canceled)
		}},
		{name: "groq", transcribe: func(context.Context, string, string) ([]captions.WordCue, error) {
			calls = append(calls, "groq")
			return nil, errors.New("must not be called after cancellation")
		}},
	}
	_, err := transcribeCaptionsWithFallback(ctx, "clip.wav", t.TempDir(), backends)
	if err == nil {
		t.Fatal("transcribeCaptionsWithFallback returned nil error, want a context error")
	}
	if !slices.Equal(calls, []string{"xai"}) {
		t.Errorf("backend calls = %v, want the chain to stop at xai after cancellation", calls)
	}
}

func TestNewStreamRenderWorker_PrefersXAIWhenWhisperAlsoConfigured(t *testing.T) {
	// The transcribe seam is built once in NewStreamRenderWorker from cfg; when
	// both backends are configured it must choose xAI rather than local whisper.
	// A cancelled context stops xAI before network access, while selecting local
	// Whisper would fail on the intentionally missing binary.
	repo := newFakeStreamRepo()
	store := newFakeStorage()
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		XAIAPIKey:        "xai_test",
		WhisperPath:      filepath.Join(t.TempDir(), "does-not-exist-whisper-cli.exe"),
		WhisperModelPath: filepath.Join(t.TempDir(), "does-not-exist-model.bin"),
	})

	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := w.transcribe(ctx, mediaPath, dir, "en")
	if err == nil {
		t.Fatal("transcribe returned nil error, want a context error")
	}
	if strings.Contains(err.Error(), "whisper binary not found") {
		t.Fatalf("got error %q, selection used local whisper instead of xai", err.Error())
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

	// Neither XAIAPIKey nor Whisper*Path is set: the worker must fail fast
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
