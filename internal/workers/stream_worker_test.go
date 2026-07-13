package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
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

func TestStreamRenderWorkerOmitsKillfeedWhenDetectionFails(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, false)
	hint := streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	plan.KillfeedCrop = &hint
	plan.Clips[0].KillfeedSeconds = []float64{0.5, 1.25}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
	})
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		if argValue(args, "-frames:v") == "1" {
			if argValue(args, "-ss") == "1.600" {
				return nil, errors.New("ffmpeg probe failed")
			}
			return nil, writeStreamKillfeedPNG(out, frame)
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

	if got, want := len(runner.calls), 3; got != want {
		t.Fatalf("runner calls = %d, want %d (two cue extractions and one render)", got, want)
	}
	for i, cue := range []string{"0.850", "1.600"} {
		call := runner.calls[i]
		if call.exe != "ffmpeg" {
			t.Fatalf("cue extraction %d executable = %q, want ffmpeg", i, call.exe)
		}
		if got := strings.Join(call.args[:4], " "); got != "-y -loglevel error -ss" {
			t.Fatalf("cue extraction %d prefix = %q", i, got)
		}
		if got := argValue(call.args, "-ss"); got != cue {
			t.Fatalf("cue extraction %d -ss = %q, want %q", i, got, cue)
		}
		if got := argValue(call.args, "-frames:v"); got != "1" {
			t.Fatalf("cue extraction %d -frames:v = %q, want 1", i, got)
		}
		if got := filepath.Base(call.args[len(call.args)-1]); got != fmt.Sprintf("killfeed-cue-clip-001-%d.png", i) {
			t.Fatalf("cue extraction %d output = %q", i, got)
		}
	}
	renderFilter := argValue(runner.calls[2].args, "-filter_complex")
	for _, forbidden := range []string{"killfeedin", "scale=620", "curves="} {
		if strings.Contains(renderFilter, forbidden) {
			t.Fatalf("failed-detection render contains %q: %s", forbidden, renderFilter)
		}
	}
	stateKey, err := streamclips.RenderStateKey(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	state := string(store.files[stateKey])
	for _, warning := range []string{
		"clip clip-001 killfeed cue 0.50s: no highlighted kill notice detected; omitting killfeed overlay",
		"clip clip-001 killfeed cue 1.25s: ffmpeg probe failed; omitting killfeed overlay",
	} {
		if !strings.Contains(state, warning) {
			t.Fatalf("render state missing warning %q: %s", warning, state)
		}
	}
}

func TestStreamRenderWorkerUsesDetectedKillfeedRows(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, false)
	hint := streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	plan.KillfeedCrop = &hint
	plan.Clips[0].KillfeedSeconds = []float64{0.5}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
	})
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	drawStreamWorkerKillfeedNotice(frame, image.Rect(1621, 73, 1909, 110))
	rows := streamclips.DetectNoticeRows(frame, &hint)
	if len(rows) != 1 {
		t.Fatalf("test frame detected %d rows, want 1: %+v", len(rows), rows)
	}
	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		if argValue(args, "-frames:v") == "1" {
			return nil, writeStreamKillfeedPNG(out, frame)
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

	if got, want := len(runner.calls), 2; got != want {
		t.Fatalf("runner calls = %d, want %d (cue extraction and render)", got, want)
	}
	row := rows[0]
	wantCrop := fmt.Sprintf("crop=%d:%d:%d:%d", row.Width, row.Height, row.X, row.Y)
	renderFilter := argValue(runner.calls[1].args, "-filter_complex")
	if !strings.Contains(renderFilter, wantCrop) {
		t.Fatalf("render filter missing detected row %q: %s", wantCrop, renderFilter)
	}
	if strings.Contains(renderFilter, "curves=") {
		t.Fatalf("detected-row render darkens the killfeed: %s", renderFilter)
	}
}

func writeStreamKillfeedPNG(path string, frame image.Image) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(file, frame); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func drawStreamWorkerKillfeedNotice(frame *image.RGBA, notice image.Rectangle) {
	dim := color.RGBA{R: 130, G: 45, B: 45, A: 255}
	for y := notice.Min.Y; y < notice.Max.Y; y++ {
		for x := notice.Min.X; x < notice.Max.X; x++ {
			frame.Set(x, y, dim)
		}
	}
	red := color.RGBA{R: 200, G: 30, B: 30, A: 255}
	inner := notice.Inset(1)
	for x := inner.Min.X; x < inner.Max.X; x++ {
		for d := range 2 {
			frame.Set(x, inner.Min.Y+d, red)
			frame.Set(x, inner.Max.Y-1-d, red)
		}
	}
	for y := inner.Min.Y; y < inner.Max.Y; y++ {
		for d := range 2 {
			frame.Set(inner.Min.X+d, y, red)
			frame.Set(inner.Max.X-1-d, y, red)
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
		wantCaptioning bool
	}{
		{
			name:           "neither configured",
			cfg:            StreamRenderWorkerConfig{},
			wantWhisper:    false,
			wantXAI:        false,
			wantCaptioning: false,
		},
		{
			name:           "xai only",
			cfg:            StreamRenderWorkerConfig{XAIAPIKey: "xai_test"},
			wantWhisper:    false,
			wantXAI:        true,
			wantCaptioning: true,
		},
		{
			name:           "whisper only",
			cfg:            StreamRenderWorkerConfig{WhisperPath: "whisper-cli", WhisperModelPath: "model.bin"},
			wantWhisper:    true,
			wantXAI:        false,
			wantCaptioning: true,
		},
		{
			name:           "whisper missing model path is not configured",
			cfg:            StreamRenderWorkerConfig{WhisperPath: "whisper-cli"},
			wantWhisper:    false,
			wantXAI:        false,
			wantCaptioning: false,
		},
		{
			name:           "both configured",
			cfg:            StreamRenderWorkerConfig{XAIAPIKey: "xai_test", WhisperPath: "whisper-cli", WhisperModelPath: "model.bin"},
			wantWhisper:    true,
			wantXAI:        true,
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
			if got := tt.cfg.captionsConfigured(); got != tt.wantCaptioning {
				t.Errorf("captionsConfigured() = %v, want %v", got, tt.wantCaptioning)
			}
		})
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
