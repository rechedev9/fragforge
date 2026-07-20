package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"github.com/rechedev9/fragforge/internal/captions"
	"github.com/rechedev9/fragforge/internal/mediafont"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
	"github.com/rechedev9/fragforge/internal/vodfetch"
)

func TestWriteStreamCoverExtractsRenderedFrame(t *testing.T) {
	runner := &fakeRunner{recordCoverCalls: true, fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		return nil, os.WriteFile(args[len(args)-1], []byte("jpeg"), 0o600)
	}}
	w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{})
	w.runner = runner
	filename := filepath.Join(t.TempDir(), "cover.jpg")
	if err := w.writeStreamCover(context.Background(), "ffmpeg", "rendered.mp4", filename); err != nil {
		t.Fatal(err)
	}
	if got := runner.calls[0].args; !slices.Contains(got, "rendered.mp4") || !slices.Contains(got, "scale=720:-2") {
		t.Fatalf("cover args = %v, want rendered video and thumbnail scale", got)
	}
	if slices.Contains(runner.calls[0].args, "-ss") {
		t.Fatalf("cover args seek past the first frame: %v", runner.calls[0].args)
	}
}

func TestPublicSourceURLRemovesPrivateURLParts(t *testing.T) {
	got := publicSourceURL("https://www.twitch.tv/videos/123?utm_source=test#chapter")
	want := "https://www.twitch.tv/videos/123"
	if got != want {
		t.Fatalf("publicSourceURL() = %q, want %q", got, want)
	}
	if got := publicSourceURL("not a URL"); got != "" {
		t.Fatalf("publicSourceURL(invalid) = %q, want empty", got)
	}
	got = publicSourceURL("https://www.youtube.com/watch?v=abc123&list=PL456&utm_source=test#chapter")
	want = "https://www.youtube.com/watch?list=PL456&v=abc123"
	if got != want {
		t.Fatalf("publicSourceURL(youtube) = %q, want %q", got, want)
	}
	got = publicSourceURL("https://m.youtube.com/watch?v=mobile123&utm_source=test#chapter")
	want = "https://m.youtube.com/watch?v=mobile123"
	if got != want {
		t.Fatalf("publicSourceURL(mobile youtube) = %q, want %q", got, want)
	}
	got = publicSourceURL("https://youtube.com.evil.example/watch?v=secret&utm_source=test")
	want = "https://youtube.com.evil.example/watch"
	if got != want {
		t.Fatalf("publicSourceURL(non-youtube suffix) = %q, want %q", got, want)
	}
}

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

func renderedStreamStateForTest(
	t *testing.T,
	store *fakeStorage,
	id uuid.UUID,
	variant string,
) streamclips.RenderState {
	t.Helper()
	key, err := streamclips.RenderStateKey(id, variant)
	if err != nil {
		t.Fatal(err)
	}
	var state streamclips.RenderState
	if err := json.Unmarshal(store.files[key], &state); err != nil {
		t.Fatalf("decode rendered stream state: %v", err)
	}
	if state.Status != streamclips.StatusRendered || len(state.Videos) == 0 {
		t.Fatalf("render state = %+v, want rendered videos", state)
	}
	return state
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

func (f *fakeStreamRepo) SetAcquired(_ context.Context, id uuid.UUID, probe streamclips.SourceProbe, sha256, discoveredTitle string) error {
	j, ok := f.jobs[id]
	if !ok {
		return streamclips.ErrNotFound
	}
	j.Probe = probe
	j.SourceSHA256 = sha256
	if j.Title == "" {
		j.Title = discoveredTitle
	}
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
	stdout  string
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
	return f.stdout, "", nil
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
	w.fetcher = vodfetch.Fetcher{Runner: &fakeVodfetchRunner{content: "fake-mp4-bytes", stdout: "vaya saco..\n"}}
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
	if got.Title != "vaya saco.." {
		t.Fatalf("title = %q, want provider title", got.Title)
	}
	if _, ok := store.files[streamclips.SourceMetadataKey(id)]; !ok {
		t.Fatal("storage missing provider metadata sidecar")
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

func TestAcquireWorkerRejectsOversizedDownload(t *testing.T) {
	id := uuid.New()
	repo := newFakeStreamRepo(streamclips.Job{ID: id, Status: streamclips.StatusAcquiring, SourceURL: "https://cdn.example.com/clip.mp4"})
	store := newFakeStorage()

	w := NewAcquireWorker(repo, store, AcquireWorkerConfig{WorkDir: t.TempDir(), MaxBytes: 4})
	w.fetcher.Runner = &fakeVodfetchRunner{content: "12345"}

	err := w.HandleStreamAcquire(context.Background(), streamAcquireTask(t, id))
	if !errors.Is(err, vodfetch.ErrTooLarge) {
		t.Fatalf("HandleStreamAcquire error = %v, want errors.Is(_, ErrTooLarge)", err)
	}
	got := repo.jobs[id]
	if got.Status != streamclips.StatusFailed {
		t.Fatalf("status = %s, want failed", got.Status)
	}
	if !strings.Contains(got.FailureReason, "límite máximo") {
		t.Fatalf("failure reason = %q, want size-limit guidance", got.FailureReason)
	}
	if _, ok := store.files[streamclips.SourceKey(id)]; ok {
		t.Fatal("oversized source was uploaded to storage")
	}
}

func TestAcquireWorkerIsIdempotentWhenSourceAlreadyExists(t *testing.T) {
	id := uuid.New()
	repo := newFakeStreamRepo(streamclips.Job{ID: id, Status: streamclips.StatusAcquiring, SourceURL: "https://clips.twitch.tv/SomeSlug"})
	store := newFakeStorage()
	_ = store.Put(streamclips.SourceKey(id), strings.NewReader("already-downloaded"))
	if err := putJSONToStorage(store, streamclips.SourceMetadataKey(id), acquiredSourceMetadata{Title: "Título recuperado"}); err != nil {
		t.Fatal(err)
	}

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
	if got := repo.jobs[id].Title; got != "Título recuperado" {
		t.Fatalf("title = %q, want recovered provider title", got)
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
	const sourceSHA = "synthetic-killfeed-source"
	applyKillfeedAnalysisForRenderTest(t, store, id, sourceSHA, &plan)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
		SourceSHA256: sourceSHA, Probe: streamclips.SourceProbe{AudioCodec: "aac"},
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
	for _, want := range []string{"nsharp0_0", "nblur0_0", "overlay"} {
		if !strings.Contains(renderFilter, want) {
			t.Fatalf("render filter missing synthetic notice overlay (%q): %s", want, renderFilter)
		}
	}
	for _, forbidden := range []string{"killfeedin", "scale=930:-2"} {
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

func TestStreamRenderWorkerRejectsMissingAutomaticRowArtifactWithoutFrozenCrop(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, false)
	hint := streamclips.CropRect{X: 0.68, Y: 0.04, Width: 0.31, Height: 0.14}
	plan.KillfeedCrop = &hint
	plan.Clips[0].KillfeedSeconds = []float64{0.5}
	// No KillfeedKills and no event artifact: metadata-backed automatic render
	// must fail instead of sampling the whole crop at cue+0.35, which can include
	// a later adjacent kill.
	const sourceSHA = "frozen-killfeed-source"
	applyKillfeedAnalysisForRenderTest(t, store, id, sourceSHA, &plan)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
		SourceSHA256: sourceSHA,
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
	err = w.HandleRenderStreamClip(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "no exact captured killfeed event") {
		t.Fatalf("HandleRenderStreamClip error = %v, want missing exact event", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want zero before any full-crop fallback", len(runner.calls))
	}
}

func TestStreamRenderWorkerBurnsReviewedCaptionsAndPublishesCaptionedClip(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	plan.Clips[0].StartSeconds = 1.25
	plan.Clips[0].EndSeconds = 3.25
	plan.Clips[0].CaptionWords = []streamclips.CaptionWord{
		{Word: "bien", StartSeconds: 0, EndSeconds: 0.5},
		{Word: "jugado", StartSeconds: 0.75, EndSeconds: 1},
	}
	plan.Clips[0].CaptionReviewed = true
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
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		t.Fatal("render must not transcribe reviewed captions")
		return nil, nil
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

	renderState := renderedStreamStateForTest(t, store, id, streamclips.VariantStreamer4060)
	wantKey := renderState.Videos[0].Key
	if _, ok := store.files[wantKey]; !ok {
		t.Fatalf("storage missing captioned video at %s", wantKey)
	}
	captionKey := renderState.ArtifactDir + "/captions/clip-001.ass"
	ass, ok := store.files[captionKey]
	if !ok {
		t.Fatal("storage missing .ass caption artifact")
	}
	// The leading override block carries the entrance pop and \pos; this test
	// only cares that cues are ordered chronologically with their \k timings.
	if !strings.Contains(string(ass), `\k50}bien {\k25}jugado`) {
		t.Fatalf("caption artifact does not contain translated spanish cues in chronological order: %s", ass)
	}
	if !strings.Contains(string(ass), "Style: Karaoke,"+mediafont.FamilyName+",") {
		t.Fatalf("caption artifact does not use %s: %s", mediafont.FamilyName, ass)
	}
	// The worker must thread LayoutStyle (not DefaultStyle) for the streamer
	// 40/60 variant: mid-center alignment (5) with the caption pinned at 35% of
	// the gameplay band via \pos(540,1171). Assert both the LayoutStyle style
	// line (Alignment=5 + zero MarginV) and the per-line \pos literally, so a
	// regression back to DefaultStyle (Alignment=2, MarginV=460, no \pos) fails.
	wantStyleLine := "Style: Karaoke,Montserrat ExtraBold,72,&H002FF4F9,&H002FF4F9,&H00000000,&H00000000,-1,-1,0,0,100,100,0,0,1,4,2,5,40,40,0,1"
	if !strings.Contains(string(ass), wantStyleLine) {
		t.Fatalf("caption artifact does not carry the LayoutStyle style line %q: %s", wantStyleLine, ass)
	}
	if !strings.Contains(string(ass), `\pos(540,1171)`) {
		t.Fatalf("caption artifact does not pin the caption at the 40/60 gameplay band via \\pos(540,1171): %s", ass)
	}

	resultKey := renderState.ResultKey
	if !strings.Contains(string(store.files[resultKey]), `clip-001_captioned.mp4`) {
		t.Fatalf("render result does not reference captioned clip: %s", store.files[resultKey])
	}
}

func TestStreamRenderWorkerPublishesUncaptionedWhenSourceHasNoAudio(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	plan.Clips[0].CaptionWords = []streamclips.CaptionWord{{Word: "inaudible", StartSeconds: 0, EndSeconds: 0.5}}
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
	renderState := renderedStreamStateForTest(t, store, id, streamclips.VariantStreamer4060)
	wantKey := renderState.Videos[0].Key
	if _, ok := store.files[wantKey]; !ok {
		t.Fatalf("storage missing uncaptioned video at %s", wantKey)
	}
}

func TestStreamRenderWorkerRejectsUnreviewedCaptionsBeforeFFmpeg(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
		Probe: streamclips.SourceProbe{AudioCodec: "aac"},
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
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		return nil, fmt.Errorf("captions: xai transcript contains no words: %w", captions.ErrUnusableTranscript)
	}

	task, err := tasks.NewRenderStreamClipTask(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	err = w.HandleRenderStreamClip(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "without reviewed captions") {
		t.Fatalf("HandleRenderStreamClip error = %v, want review gate", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %d, want 0 before caption review", len(runner.calls))
	}
}

func TestTranscribeCaptionCuesUsesSpeechRegionRecovery(t *testing.T) {
	w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{})
	var paths []string
	w.transcribe = func(_ context.Context, mediaPath, _, _ string) ([]captions.WordCue, error) {
		paths = append(paths, mediaPath)
		return nil, fmt.Errorf("captions: xai transcript contains no words: %w", captions.ErrUnusableTranscript)
	}
	recoveryCalls := 0
	cues, err := w.transcribeCaptionCues(context.Background(), "ordinary.wav", t.TempDir(), "auto", 15, func() ([]captions.WordCue, error) {
		recoveryCalls++
		return []captions.WordCue{{Word: "hola", StartSeconds: 1, EndSeconds: 1.4}}, nil
	})
	if err != nil {
		t.Fatalf("transcribeCaptionCues error = %v", err)
	}
	if recoveryCalls != 1 || len(paths) != 1 || paths[0] != "ordinary.wav" {
		t.Fatalf("recovery calls = %d, paths = %v; want one ordinary pass then recovery", recoveryCalls, paths)
	}
	if len(cues) != 1 || cues[0].Word != "hola" {
		t.Fatalf("cues = %+v, want recovered transcript", cues)
	}
}

func TestTranscribeCaptionCuesRecoversTemporallySparseValidTranscript(t *testing.T) {
	w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{})
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		return []captions.WordCue{{Word: "hello", StartSeconds: 11, EndSeconds: 11.4}}, nil
	}
	recoveryCalled := false
	recovered := []captions.WordCue{
		{Word: "hola", StartSeconds: 0.5, EndSeconds: 0.9},
		{Word: "mundo", StartSeconds: 11, EndSeconds: 11.4},
	}
	cues, err := w.transcribeCaptionCues(context.Background(), "ordinary.wav", t.TempDir(), "auto", 15, func() ([]captions.WordCue, error) {
		recoveryCalled = true
		return recovered, nil
	})
	if err != nil {
		t.Fatalf("transcribeCaptionCues error = %v", err)
	}
	if !recoveryCalled {
		t.Fatal("recovery was not called for a valid but temporally sparse transcript")
	}
	if got, want := wordsOf(cues), "hola mundo"; got != want {
		t.Fatalf("cues = %q, want broader recovered transcript %q", got, want)
	}
}

func TestTranscribeCaptionCuesKeepsValidSparseTranscriptWhenRecoveryFails(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "unusable", err: fmt.Errorf("no recovery speech: %w", captions.ErrUnusableTranscript)},
		{name: "transport", err: errors.New("temporary xai failure")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{})
			w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
				return []captions.WordCue{{Word: "hola", StartSeconds: 11, EndSeconds: 11.4}}, nil
			}
			cues, err := w.transcribeCaptionCues(context.Background(), "ordinary.wav", t.TempDir(), "auto", 15, func() ([]captions.WordCue, error) {
				return nil, tt.err
			})
			if err != nil {
				t.Fatalf("transcribeCaptionCues error = %v, want the valid first pass", err)
			}
			if got, want := wordsOf(cues), "hola"; got != want {
				t.Fatalf("cues = %q, want first pass %q", got, want)
			}
		})
	}
}

func TestTranscribeCaptionCuesSkipsRecoveryForBroadTranscript(t *testing.T) {
	w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{})
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		return []captions.WordCue{
			{Word: "hola", StartSeconds: 1, EndSeconds: 1.4},
			{Word: "mundo", StartSeconds: 10, EndSeconds: 10.4},
		}, nil
	}
	recoveryCalled := false
	_, err := w.transcribeCaptionCues(context.Background(), "ordinary.wav", t.TempDir(), "auto", 15, func() ([]captions.WordCue, error) {
		recoveryCalled = true
		return nil, nil
	})
	if err != nil {
		t.Fatalf("transcribeCaptionCues error = %v", err)
	}
	if recoveryCalled {
		t.Fatal("recovery called for a transcript spanning more than half the clip")
	}
}

func TestTranscribeCaptionCuesKeepsCancellationHard(t *testing.T) {
	t.Run("after ordinary transcript", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{})
		w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
			cancel()
			return nil, fmt.Errorf("captions: no words: %w", captions.ErrUnusableTranscript)
		}
		retryCalled := false
		_, err := w.transcribeCaptionCues(ctx, "ordinary.wav", t.TempDir(), "auto", 15, func() ([]captions.WordCue, error) {
			retryCalled = true
			return nil, nil
		})
		if !errors.Is(err, context.Canceled) || errors.Is(err, captions.ErrUnusableTranscript) {
			t.Fatalf("error = %v, want only context cancellation", err)
		}
		if retryCalled {
			t.Fatal("speech retry ran after context cancellation")
		}
	})

	t.Run("while extracting retry audio", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{})
		w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
			return nil, fmt.Errorf("captions: no words: %w", captions.ErrUnusableTranscript)
		}
		_, err := w.transcribeCaptionCues(ctx, "ordinary.wav", t.TempDir(), "auto", 15, func() ([]captions.WordCue, error) {
			cancel()
			return nil, errors.New("ffmpeg canceled")
		})
		if !errors.Is(err, context.Canceled) || errors.Is(err, captions.ErrUnusableTranscript) {
			t.Fatalf("error = %v, want only context cancellation", err)
		}
	})

	t.Run("during enhanced transcript", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{})
		w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
			return nil, fmt.Errorf("captions: no words: %w", captions.ErrUnusableTranscript)
		}
		_, err := w.transcribeCaptionCues(ctx, "ordinary.wav", t.TempDir(), "auto", 15, func() ([]captions.WordCue, error) {
			cancel()
			return nil, fmt.Errorf("captions: no words: %w", captions.ErrUnusableTranscript)
		})
		if !errors.Is(err, context.Canceled) || errors.Is(err, captions.ErrUnusableTranscript) {
			t.Fatalf("error = %v, want only context cancellation", err)
		}
	})
}

func TestCaptionRecoveryWindowsCoversClipAroundLocatorEnvelopeAndCaps(t *testing.T) {
	proposal := []captions.WordCue{
		{Word: "garbled-a", StartSeconds: 5.94, EndSeconds: 6.24},
		{Word: "garbled-b", StartSeconds: 6.26, EndSeconds: 8.51},
		{Word: "garbled-c", StartSeconds: 9.45, EndSeconds: 11.85},
	}
	windows, err := captionRecoveryWindows(proposal, 15.15)
	if err != nil {
		t.Fatalf("captionRecoveryWindows error = %v", err)
	}
	if got, want := len(windows), 4; got != want {
		t.Fatalf("windows = %+v, want %d", windows, want)
	}
	assertNear := func(label string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 0.0001 {
			t.Fatalf("%s = %.4f, want %.4f", label, got, want)
		}
	}
	assertNear("leading extract start", windows[0].ExtractStart, 0)
	assertNear("leading extract end", windows[0].ExtractEnd, 6.2)
	assertNear("speech extract start", windows[1].ExtractStart, 5.4)
	assertNear("speech extract end", windows[1].ExtractEnd, 12.1)
	assertNear("trailing extract start", windows[2].ExtractStart, 11.3)
	assertNear("trailing extract end", windows[2].ExtractEnd, 13.9)
	assertNear("tail extract start", windows[3].ExtractStart, 13.1)
	assertNear("tail extract end", windows[3].ExtractEnd, 15.15)
	for i := range windows {
		if i == 0 {
			assertNear("ownership starts at clip start", windows[i].KeepStart, 0)
		} else {
			assertNear("ownership ranges are contiguous", windows[i].KeepStart, windows[i-1].KeepEnd)
		}
		if got, want := windows[i].KeepEndInclusive, i == len(windows)-1; got != want {
			t.Fatalf("window %d KeepEndInclusive = %v, want %v", i, got, want)
		}
	}
	assertNear("ownership ends at clip end", windows[len(windows)-1].KeepEnd, 15.15)

	long, err := captionRecoveryWindows([]captions.WordCue{{Word: "bad", StartSeconds: 0, EndSeconds: 11.8}}, 15.15)
	if err != nil {
		t.Fatalf("long captionRecoveryWindows error = %v", err)
	}
	if got, want := len(long), 4; got != want {
		t.Fatalf("long windows = %+v, want %d", long, want)
	}
	for i, window := range long {
		if window.KeepEnd-window.KeepStart > captionRecoveryCoreSeconds+1e-6 {
			t.Fatalf("long window %d ownership = %.3fs, exceeds %.3fs", i, window.KeepEnd-window.KeepStart, captionRecoveryCoreSeconds)
		}
	}

	centered, err := captionRecoveryWindows([]captions.WordCue{{Word: "center", StartSeconds: 7.4, EndSeconds: 7.6}}, 15)
	if err != nil {
		t.Fatalf("centered captionRecoveryWindows error = %v", err)
	}
	if got, want := len(centered), 4; got != want {
		t.Fatalf("centered windows = %+v, want minimum %d", centered, want)
	}
	wantCentered := [][2]float64{{0, 5}, {5, 10}, {10, 13.5}, {13.5, 15}}
	for i, window := range centered {
		assertNear(fmt.Sprintf("centered window %d start", i), window.KeepStart, wantCentered[i][0])
		assertNear(fmt.Sprintf("centered window %d end", i), window.KeepEnd, wantCentered[i][1])
	}

	tooMany := []captions.WordCue{{Word: "center", StartSeconds: 14, EndSeconds: 15}}
	if _, err := captionRecoveryWindows(tooMany, 30); !errors.Is(err, captions.ErrUnusableTranscript) {
		t.Fatalf("too many windows error = %v, want ErrUnusableTranscript", err)
	}
}

func TestRecoverCaptionTranscriptUsesEnhancedTextOnlyAsLocator(t *testing.T) {
	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("audio"), 0o644)
	}}
	w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{})
	w.runner = runner
	w.transcribe = func(_ context.Context, mediaPath, _, _ string) ([]captions.WordCue, error) {
		switch {
		case strings.HasSuffix(mediaPath, "-speech.wav"):
			return []captions.WordCue{
				{Word: "bogus-a", StartSeconds: 5.94, EndSeconds: 6.24},
				{Word: "bogus-b", StartSeconds: 6.26, EndSeconds: 8.51},
				{Word: "bogus-c", StartSeconds: 9.45, EndSeconds: 11.85},
			}, nil
		case strings.HasSuffix(mediaPath, "-region-00.wav"):
			return nil, fmt.Errorf("no early speech: %w", captions.ErrUnusableTranscript)
		case strings.HasSuffix(mediaPath, "-region-01.wav"):
			return []captions.WordCue{
				{Word: "Lal.", StartSeconds: 0.54, EndSeconds: 0.84},
				{Word: "Are", StartSeconds: 0.82, EndSeconds: 1.02},
				{Word: "you", StartSeconds: 1.04, EndSeconds: 1.20},
				{Word: "happy?", StartSeconds: 1.22, EndSeconds: 2.30},
				{Word: "Yeah,", StartSeconds: 3.05, EndSeconds: 3.33},
				{Word: "I'm", StartSeconds: 3.35, EndSeconds: 3.43},
				{Word: "happy.", StartSeconds: 3.45, EndSeconds: 3.68},
				{Word: "Alright,", StartSeconds: 3.70, EndSeconds: 5.91},
				{Word: "Martinez.", StartSeconds: 5.93, EndSeconds: 6.43},
			}, nil
		case strings.HasSuffix(mediaPath, "-region-02.wav"):
			return nil, fmt.Errorf("no middle-tail speech: %w", captions.ErrUnusableTranscript)
		case strings.HasSuffix(mediaPath, "-region-03.wav"):
			return []captions.WordCue{{Word: "Haha.", StartSeconds: 0.34, EndSeconds: 1.99}}, nil
		default:
			return nil, fmt.Errorf("unexpected transcription path %s", mediaPath)
		}
	}

	dir := t.TempDir()
	ordinary := filepath.Join(dir, "clip.wav")
	clip := streamclips.ClipRange{ID: "clip", StartSeconds: 0, EndSeconds: 15.15}
	got, err := w.recoverCaptionTranscript(context.Background(), StreamRenderWorkerConfig{FFmpegPath: "ffmpeg"}, dir, "source.mp4", ordinary, clip, "auto")
	if err != nil {
		t.Fatalf("recoverCaptionTranscript error = %v", err)
	}
	if gotWords := wordsOf(got); strings.Contains(gotWords, "bogus") || gotWords != "Lal. Are you happy? Yeah, I'm happy. Alright, Martinez. Haha." {
		t.Fatalf("recovered words = %q, want only independently transcribed region words", gotWords)
	}
	if gotStart, wantStart := got[0].StartSeconds, 5.94; math.Abs(gotStart-wantStart) > 0.0001 {
		t.Fatalf("first recovered start = %.3f, want %.3f", gotStart, wantStart)
	}
	if got, want := len(runner.calls), 5; got != want {
		t.Fatalf("runner calls = %d, want %d (locator + complete four-region coverage)", got, want)
	}
	if got, want := argValue(runner.calls[1].args, "-ss"), "0.000"; got != want {
		t.Fatalf("leading region -ss = %q, want %q", got, want)
	}
	if got, want := argValue(runner.calls[2].args, "-ss"), "5.400"; got != want {
		t.Fatalf("speech region -ss = %q, want %q", got, want)
	}
	if got, want := argValue(runner.calls[3].args, "-ss"), "11.300"; got != want {
		t.Fatalf("trailing region -ss = %q, want %q", got, want)
	}
	if got, want := argValue(runner.calls[4].args, "-ss"), "13.100"; got != want {
		t.Fatalf("tail region -ss = %q, want %q", got, want)
	}
}

func wordsOf(cues []captions.WordCue) string {
	words := make([]string, len(cues))
	for i, cue := range cues {
		words[i] = cue.Word
	}
	return strings.Join(words, " ")
}

// --- xAI-only captions ------------------------------------------------------

func TestStreamRenderWorkerConfigCaptionsRequireXAI(t *testing.T) {
	if (StreamRenderWorkerConfig{}).captionsConfigured() {
		t.Fatal("captionsConfigured() = true without xAI, want false")
	}
	if !(StreamRenderWorkerConfig{XAIAPIKey: "xai_test"}).captionsConfigured() {
		t.Fatal("captionsConfigured() = false with xAI, want true")
	}
}

func TestTranscribeCaptionsWithXAIValidatesTheOnlyBackend(t *testing.T) {
	t.Run("usable transcript", func(t *testing.T) {
		want := []captions.WordCue{{Word: "nice", StartSeconds: 0.5, EndSeconds: 1}}
		transcribe := func(context.Context, string, string) ([]captions.WordCue, error) {
			return want, nil
		}
		got, err := transcribeCaptionsWithXAI(context.Background(), "clip.wav", t.TempDir(), transcribe)
		if err != nil {
			t.Fatalf("transcribeCaptionsWithXAI error = %v", err)
		}
		if len(got) != 1 || got[0] != want[0] {
			t.Fatalf("got cues %v, want %v", got, want)
		}
	})

	t.Run("garbled transcript stays soft and never falls back", func(t *testing.T) {
		transcribe := func(context.Context, string, string) ([]captions.WordCue, error) {
			return []captions.WordCue{{Word: "Martínez", StartSeconds: 3.66, EndSeconds: 11.8}}, nil
		}
		got, err := transcribeCaptionsWithXAI(context.Background(), "clip.wav", t.TempDir(), transcribe)
		if err == nil {
			t.Fatalf("got cues %v, want unusable-transcript error", got)
		}
		if !errors.Is(err, captions.ErrUnusableTranscript) {
			t.Fatalf("error = %v, want ErrUnusableTranscript", err)
		}
	})

	t.Run("transport failure is hard", func(t *testing.T) {
		transcribe := func(context.Context, string, string) ([]captions.WordCue, error) {
			return nil, errors.New("status 500")
		}
		_, err := transcribeCaptionsWithXAI(context.Background(), "clip.wav", t.TempDir(), transcribe)
		if err == nil || !strings.Contains(err.Error(), "status 500") {
			t.Fatalf("error = %v, want xAI transport failure", err)
		}
		if errors.Is(err, captions.ErrUnusableTranscript) {
			t.Fatalf("error = %v, transport failure must not publish uncaptioned", err)
		}
	})

	t.Run("cancelled context is hard", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		transcribe := func(context.Context, string, string) ([]captions.WordCue, error) {
			cancel()
			return []captions.WordCue{{Word: "Martínez", StartSeconds: 3.66, EndSeconds: 11.8}}, nil
		}
		_, err := transcribeCaptionsWithXAI(ctx, "clip.wav", t.TempDir(), transcribe)
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context cancellation", err)
		}
		if errors.Is(err, captions.ErrUnusableTranscript) {
			t.Fatalf("error = %v, cancellation must not publish uncaptioned", err)
		}
	})
}

func TestNewStreamRenderWorkerUsesXAI(t *testing.T) {
	w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{XAIAPIKey: "xai_test"})
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(mediaPath, []byte("fake media"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := w.transcribe(ctx, mediaPath, dir, "en")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("transcribe error = %v, want xAI context cancellation", err)
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
		Probe: streamclips.SourceProbe{AudioCodec: "aac"},
	})

	// XAIAPIKey is not set: the worker must fail fast
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
	if !strings.Contains(err.Error(), "without reviewed captions") {
		t.Fatalf("got error %q, want it to mention reviewed captions", err.Error())
	}
}

func TestStreamRenderWorkerBurnsReviewedCaptionsWithoutCloudKey(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	plan.Clips[0].CaptionWords = []streamclips.CaptionWord{
		{Word: "Buena", StartSeconds: 0, EndSeconds: 0.5},
		{Word: "jugada", StartSeconds: 0.5, EndSeconds: 1},
	}
	plan.Clips[0].CaptionReviewed = true
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
		Probe: streamclips.SourceProbe{AudioCodec: "aac"},
	})
	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("video-bytes"), 0o644)
	}}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{WorkDir: t.TempDir(), FFmpegPath: "ffmpeg"})
	w.runner = runner
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		t.Fatal("reviewed captions must not call a cloud transcriber")
		return nil, nil
	}
	task, err := tasks.NewRenderStreamClipTask(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderStreamClip error = %v", err)
	}
	renderState := renderedStreamStateForTest(t, store, id, streamclips.VariantStreamer4060)
	captionKey := renderState.ArtifactDir + "/captions/clip-001.ass"
	ass, ok := store.files[captionKey]
	if !ok || !strings.Contains(string(ass), "Buena") || !strings.Contains(string(ass), "jugada") {
		t.Fatalf("reviewed ASS caption = %q", ass)
	}
}

func TestStreamRenderWorkerPublishesReviewedNoSpeechWithoutCloudKey(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	plan.Clips[0].CaptionReviewed = true
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
		Probe: streamclips.SourceProbe{AudioCodec: "aac"},
	})
	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("video-bytes"), 0o644)
	}}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{WorkDir: t.TempDir(), FFmpegPath: "ffmpeg"})
	w.runner = runner
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		t.Fatal("reviewed no-speech clip must not call a caption backend")
		return nil, nil
	}
	task, err := tasks.NewRenderStreamClipTask(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderStreamClip error = %v", err)
	}
	if got, want := len(runner.calls), 1; got != want {
		t.Fatalf("runner calls = %d, want %d render call", got, want)
	}
	stateKey, err := streamclips.RenderStateKey(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(store.files[stateKey]), "reviewed as containing no speech") {
		t.Fatalf("render state missing no-speech warning: %s", store.files[stateKey])
	}
}

func TestStreamRenderWorkerMigratesAlreadyQueuedLegacyDuration(t *testing.T) {
	store := newFakeStorage()
	id := uuid.New()
	store.files[streamclips.SourceKey(id)] = []byte("source")
	plan := streamclips.DefaultEditPlan()
	plan.Clips = []streamclips.ClipRange{{ID: "legacy", StartSeconds: 0, EndSeconds: 20}}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID:         id,
		Status:     streamclips.StatusReady,
		SourcePath: streamclips.SourceKey(id),
		Probe:      streamclips.SourceProbe{DurationSeconds: 15.15},
		EditPlan:   planJSON,
	})
	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("video"), 0o644)
	}}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{WorkDir: t.TempDir(), FFmpegPath: "ffmpeg"})
	w.runner = runner
	task, err := tasks.NewRenderStreamClipTask(id, plan.Variant)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderStreamClip error = %v", err)
	}
	if got, want := argValue(runner.calls[0].args, "-t"), "15.150000000"; got != want {
		t.Fatalf("render -t = %q, want migrated duration %q", got, want)
	}
}

func TestStreamRenderWorkerScalesReviewedCaptionCuesBySpeed(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	plan.Clips[0].StartSeconds = 1.25
	plan.Clips[0].EndSeconds = 3.25
	plan.Clips[0].Edit = &streamclips.ClipEdit{Speed: 2}
	plan.Clips[0].CaptionWords = []streamclips.CaptionWord{
		{Word: "nice", StartSeconds: 0, EndSeconds: 0.5},
		{Word: "gg", StartSeconds: 0.5, EndSeconds: 1},
	}
	plan.Clips[0].CaptionReviewed = true
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
		Probe: streamclips.SourceProbe{AudioCodec: "aac"},
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
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		t.Fatal("render must not transcribe reviewed captions")
		return nil, nil
	}

	task, err := tasks.NewRenderStreamClipTask(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderStreamClip error = %v", err)
	}

	renderState := renderedStreamStateForTest(t, store, id, streamclips.VariantStreamer4060)
	captionKey := renderState.ArtifactDir + "/captions/clip-001.ass"
	ass, ok := store.files[captionKey]
	if !ok {
		t.Fatal("storage missing .ass caption artifact")
	}
	// At 2x the 1s of source speech plays back in 0.5s: cue times must be
	// divided by the clip speed so captions stay on the output timeline.
	if !strings.Contains(string(ass), `0:00:00.00,0:00:00.50,Karaoke`) || !strings.Contains(string(ass), `\k25}nice {\k25}gg`) {
		t.Fatalf("caption artifact does not scale cues to the sped-up output: %s", ass)
	}
}

func TestStreamRenderWorkerSkipsCaptionsWhenClipMutesSourceAudio(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	muted := 0.0
	plan.Clips[0].Edit = &streamclips.ClipEdit{SourceVolume: &muted}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
		Probe: streamclips.SourceProbe{AudioCodec: "aac"},
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
		t.Fatal("transcription ran for a clip whose edit mutes the source audio")
	}
	if got, want := len(runner.calls), 1; got != want {
		t.Fatalf("runner calls = %d, want %d render call", got, want)
	}
	stateKey, err := streamclips.RenderStateKey(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(store.files[stateKey]), "original audio is muted") {
		t.Fatalf("render state missing muted-audio warning: %s", store.files[stateKey])
	}
}

func TestStreamRenderWorkerAllowsMutedClipWithoutCaptionBackend(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	muted := 0.0
	plan.Clips[0].Edit = &streamclips.ClipEdit{SourceVolume: &muted}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), EditPlan: planJSON,
		Probe: streamclips.SourceProbe{AudioCodec: "aac"},
	})
	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("video-bytes"), 0o644)
	}}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{WorkDir: t.TempDir(), FFmpegPath: "ffmpeg"})
	w.runner = runner
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		t.Fatal("muted clip must not call a caption backend")
		return nil, nil
	}
	task, err := tasks.NewRenderStreamClipTask(id, streamclips.VariantStreamer4060)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleRenderStreamClip(context.Background(), task); err != nil {
		t.Fatalf("HandleRenderStreamClip error = %v", err)
	}
	if got, want := len(runner.calls), 1; got != want {
		t.Fatalf("runner calls = %d, want %d render call", got, want)
	}
}

func TestStreamCaptionWorkerPersistsReviewableCandidatesWithoutMutatingPlan(t *testing.T) {
	store := newFakeStorage()
	id, plan := newReadyStreamJobWithCaptions(t, store, true)
	planJSON, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeStreamRepo(streamclips.Job{
		ID: id, Status: streamclips.StatusReady, SourcePath: streamclips.SourceKey(id), SourceSHA256: "source-sha", EditPlan: planJSON,
		Probe: streamclips.SourceProbe{AudioCodec: "aac", DurationSeconds: 5},
	})
	generationID := uuid.New()
	queuedState := streamclips.CaptionCandidateState{
		JobID: id, GenerationID: generationID, Status: streamclips.CaptionCandidatesQueued,
		Clips: []streamclips.CaptionCandidateClip{}, UpdatedAt: time.Now().UTC(),
	}
	putJSON(t, store, streamclips.CaptionCandidatesKey(id), queuedState)
	generationKey, err := streamclips.CaptionCandidateGenerationKey(id, generationID)
	if err != nil {
		t.Fatal(err)
	}
	putJSON(t, store, generationKey, queuedState)
	runner := &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("wav"), 0o600)
	}}
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{
		WorkDir: t.TempDir(), FFmpegPath: "ffmpeg", XAIAPIKey: "xai_test",
	})
	w.runner = runner
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		return []captions.WordCue{
			{Word: "hello", StartSeconds: 0.1, EndSeconds: 0.8},
			{Word: "friend", StartSeconds: 0.9, EndSeconds: 1.7},
		}, nil
	}
	w.translateToSpanish = func(_ context.Context, cues []captions.WordCue) ([]captions.WordCue, error) {
		if got, want := wordsOf(cues), "hello friend"; got != want {
			t.Fatalf("source words = %q, want %q", got, want)
		}
		return []captions.WordCue{
			{Word: "hola", StartSeconds: 0.1, EndSeconds: 0.8},
			{Word: "amigo", StartSeconds: 0.9, EndSeconds: 1.7},
		}, nil
	}
	task, err := tasks.NewGenerateStreamCaptionsTask(id, generationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleGenerateStreamCaptions(context.Background(), task); err != nil {
		t.Fatalf("HandleGenerateStreamCaptions error = %v", err)
	}
	var state streamclips.CaptionCandidateState
	if err := json.Unmarshal(store.files[generationKey], &state); err != nil {
		t.Fatal(err)
	}
	if state.Status != streamclips.CaptionCandidatesReviewRequired || len(state.Clips) != 1 {
		t.Fatalf("caption state = %+v, want one review_required clip", state)
	}
	clip := state.Clips[0]
	if got, want := clip.SourceWords[0].Word, "hello"; got != want {
		t.Fatalf("source word = %q, want %q", got, want)
	}
	if got, want := clip.CandidateWords[0].Word, "hola"; got != want {
		t.Fatalf("candidate word = %q, want %q", got, want)
	}
	if clip.Provider != "xai" || clip.STTModel != "" || clip.STTEndpoint != "/v1/stt" || clip.TranslationModel == "" || clip.Fingerprint == "" {
		t.Fatalf("candidate provenance/fingerprint missing: %+v", clip)
	}
	var saved streamclips.EditPlan
	if err := json.Unmarshal(repo.jobs[id].EditPlan, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Clips[0].CaptionReviewed || len(saved.Clips[0].CaptionWords) != 0 {
		t.Fatalf("generation mutated unreviewed edit plan: %+v", saved.Clips[0])
	}
}

func TestStreamCaptionWorkerCannotOverwriteNewerGeneration(t *testing.T) {
	store := newFakeStorage()
	id := uuid.New()
	oldGeneration := uuid.New()
	newGeneration := uuid.New()
	repo := newFakeStreamRepo(streamclips.Job{ID: id, Status: streamclips.StatusReady})
	putJSON(t, store, streamclips.CaptionCandidatesKey(id), streamclips.CaptionCandidateState{
		JobID: id, GenerationID: newGeneration, Status: streamclips.CaptionCandidatesQueued,
		Clips: []streamclips.CaptionCandidateClip{}, UpdatedAt: time.Now().UTC(),
	})
	w := NewStreamRenderWorker(repo, store, StreamRenderWorkerConfig{})
	w.runner = &fakeRunner{fn: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("superseded generation must stop before media work")
		return nil, nil
	}}
	task, err := tasks.NewGenerateStreamCaptionsTask(id, oldGeneration)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.HandleGenerateStreamCaptions(context.Background(), task); err != nil {
		t.Fatalf("superseded HandleGenerateStreamCaptions error = %v", err)
	}
	var state streamclips.CaptionCandidateState
	if err := json.Unmarshal(store.files[streamclips.CaptionCandidatesKey(id)], &state); err != nil {
		t.Fatal(err)
	}
	if state.GenerationID != newGeneration || state.Status != streamclips.CaptionCandidatesQueued {
		t.Fatalf("state overwritten by stale generation: %+v", state)
	}
}

func TestStreamCaptionWorkerReturnsNoSpeechAsReviewableDecision(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.mp4")
	if err := os.WriteFile(sourcePath, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	w := NewStreamRenderWorker(newFakeStreamRepo(), newFakeStorage(), StreamRenderWorkerConfig{
		WorkDir: dir, FFmpegPath: "ffmpeg", XAIAPIKey: "xai_test",
	})
	w.runner = &fakeRunner{fn: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		out := args[len(args)-1]
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(out, []byte("wav"), 0o600)
	}}
	w.transcribe = func(context.Context, string, string, string) ([]captions.WordCue, error) {
		return nil, fmt.Errorf("xai transcript contains no words: %w", captions.ErrUnusableTranscript)
	}
	clip := streamclips.ClipRange{ID: "clip-1", StartSeconds: 0, EndSeconds: 2}
	got, err := w.generateClipCaptionCandidate(context.Background(), w.cfg.withDefaults(), dir, sourcePath, streamclips.Job{
		ID: uuid.New(), SourceSHA256: "source-sha", Probe: streamclips.SourceProbe{AudioCodec: "aac"},
	}, clip)
	if err != nil {
		t.Fatalf("generateClipCaptionCandidate error = %v", err)
	}
	if got.Status != streamclips.CaptionClipNoSpeech || len(got.CandidateWords) != 0 {
		t.Fatalf("candidate = %+v, want reviewable no_speech", got)
	}
}
