package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	_ "image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rechedev9/fragforge/internal/httpapi"
	"github.com/rechedev9/fragforge/internal/storage"
	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/tasks"
	"github.com/rechedev9/fragforge/internal/workers"
)

// TestStreamRenderE2E drives the stream-clips vertical layout pipeline the
// way an end user would: through the real HTTP API, backed by the same
// in-memory repository and inline queue construction as `zv serve
// ZV_DATABASE_URL=memory`, rendering with a real ffmpeg binary. It skips
// cleanly when ffmpeg/ffprobe are not on PATH, since it cannot fake the
// encoder without losing the point of the test.
func TestStreamRenderE2E(t *testing.T) {
	t.Parallel()
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found on PATH, skipping real stream-render e2e")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not found on PATH, skipping real stream-render e2e")
	}

	srv, sourcePath := newStreamE2EServer(t, ffmpegPath, ffprobePath)
	t.Cleanup(srv.Close)
	client := srv.Client()

	t.Run("40-60 vertical stack renders correct geometry", func(t *testing.T) {
		t.Parallel()
		id := uploadStreamSource(t, client, srv.URL, sourcePath)

		plan := streamclips.EditPlan{
			Variant:          streamclips.VariantStreamer4060,
			FaceCrop:         streamclips.CropRect{X: 0, Y: 0, Width: 0.25, Height: 0.25},
			FaceCropReviewed: true,
			GameplayCrop:     streamclips.CropRect{X: 0.25, Y: 0.25, Width: 0.75, Height: 0.75},
			Clips:            []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 0.5, EndSeconds: 3.5}},
			Captions:         streamclips.CaptionsPlan{Enabled: false},
		}
		putStreamEditPlan(t, client, srv.URL, id, plan)

		clipID := startAndAwaitStreamRender(t, client, srv.URL, id, streamclips.VariantStreamer4060)
		outPath := downloadStreamVideo(t, client, srv.URL, id, streamclips.VariantStreamer4060, clipID)

		probe := ffprobeVideo(t, ffprobePath, outPath)
		if probe.Width != 1080 || probe.Height != 1920 {
			t.Fatalf("output size = %dx%d, want 1080x1920", probe.Width, probe.Height)
		}
		if probe.FPS < 59.5 || probe.FPS > 60.5 {
			t.Fatalf("output fps = %.2f, want ~60", probe.FPS)
		}
		if probe.VideoCodec != "h264" {
			t.Fatalf("video codec = %q, want h264", probe.VideoCodec)
		}
		if probe.AudioCodec != "aac" {
			t.Fatalf("audio codec = %q, want aac", probe.AudioCodec)
		}
		const wantDuration = 3.0
		if diff := probe.Duration - wantDuration; diff < -0.3 || diff > 0.3 {
			t.Fatalf("output duration = %.3fs, want %.1fs +-0.3s", probe.Duration, wantDuration)
		}
		t.Logf("40-60 output probe: %+v", probe)

		framePath := extractFramePNG(t, ffmpegPath, outPath, 1.0)
		facePixel := readPixel(t, framePath, 540, 300)
		gamePixel := readPixel(t, framePath, 540, 1400)
		t.Logf("face band pixel (540,300) = %+v, gameplay band pixel (540,1400) = %+v", facePixel, gamePixel)

		if !isPredominantlyRed(facePixel) {
			t.Fatalf("face band pixel (540,300) = %+v, want predominantly red", facePixel)
		}
		if !isPredominantlyBlue(gamePixel) {
			t.Fatalf("gameplay band pixel (540,1400) = %+v, want predominantly blue", gamePixel)
		}
	})

	t.Run("fullframe-nocam renders successfully with center gameplay pixel", func(t *testing.T) {
		t.Parallel()
		id := uploadStreamSource(t, client, srv.URL, sourcePath)

		plan := streamclips.EditPlan{
			Variant:      streamclips.VariantStreamerFullframeNoCam,
			GameplayCrop: streamclips.CropRect{X: 0, Y: 0, Width: 1, Height: 1},
			Clips:        []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 0.5, EndSeconds: 3.5}},
			Captions:     streamclips.CaptionsPlan{Enabled: false},
		}
		putStreamEditPlan(t, client, srv.URL, id, plan)

		clipID := startAndAwaitStreamRender(t, client, srv.URL, id, streamclips.VariantStreamerFullframeNoCam)
		outPath := downloadStreamVideo(t, client, srv.URL, id, streamclips.VariantStreamerFullframeNoCam, clipID)

		probe := ffprobeVideo(t, ffprobePath, outPath)
		if probe.Width != 1080 || probe.Height != 1920 {
			t.Fatalf("output size = %dx%d, want 1080x1920", probe.Width, probe.Height)
		}
		t.Logf("fullframe-nocam output probe: %+v", probe)

		framePath := extractFramePNG(t, ffmpegPath, outPath, 1.0)
		centerPixel := readPixel(t, framePath, 540, 960)
		t.Logf("center pixel (540,960) = %+v", centerPixel)
		if !isPredominantlyBlue(centerPixel) {
			t.Fatalf("center pixel (540,960) = %+v, want predominantly blue (source width scaled+cropped past the red corner)", centerPixel)
		}
	})

	t.Run("fullframe staggered killfeed notices are absent before cue and visible per row at cue", func(t *testing.T) {
		t.Parallel()
		id := uploadStreamSource(t, client, srv.URL, sourcePath)
		plan := streamclips.EditPlan{
			Variant:      streamclips.VariantStreamerFullframeNoCam,
			GameplayCrop: streamclips.CropRect{X: 0, Y: 0, Width: 1, Height: 1},
			KillfeedCrop: &streamclips.CropRect{X: 0.74, Y: 0.04, Width: 0.25, Height: 0.15},
			Clips: []streamclips.ClipRange{{
				ID:           "clip-1",
				StartSeconds: 0.5,
				EndSeconds:   3.5,
			}},
			Captions: streamclips.CaptionsPlan{Enabled: false},
		}
		putStreamEditPlan(t, client, srv.URL, id, plan)
		analysis, _ := startAndApplyStreamKillfeed(t, client, srv.URL, id)
		if got, want := len(analysis.Clips), 1; got != want {
			t.Fatalf("analysis clips = %d, want %d", got, want)
		}
		if got, want := len(analysis.Clips[0].Events), 1; got != want {
			t.Fatalf("analysis events = %d, want one same-frame burst: %+v", got, analysis.Clips[0].Events)
		}
		event := analysis.Clips[0].Events[0]
		if event.Mode != streamclips.KillfeedEventBurst || len(event.Rows) != 2 {
			t.Fatalf("automatic event = %+v, want a two-row native-frame burst", event)
		}
		ptsSeconds := float64(event.SourcePTS) * float64(event.TimeBase.Num) / float64(event.TimeBase.Den)
		if math.Abs(event.CueSeconds-2) > 1e-9 || math.Abs(event.CueSeconds-ptsSeconds) > 1e-12 {
			t.Fatalf("automatic cue = %.12f, PTS time = %.12f, want exact source frame at 2s", event.CueSeconds, ptsSeconds)
		}

		clipID := startAndAwaitStreamRender(t, client, srv.URL, id, streamclips.VariantStreamerFullframeNoCam)
		outPath := downloadStreamVideo(t, client, srv.URL, id, streamclips.VariantStreamerFullframeNoCam, clipID)

		probe := ffprobeVideo(t, ffprobePath, outPath)
		if probe.Width != 1080 || probe.Height != 1920 {
			t.Fatalf("output size = %dx%d, want 1080x1920", probe.Width, probe.Height)
		}

		// Automatic rendering consumes the two immutable row PNGs captured from
		// the event's exact SamplePTS on the detector's 1920x1080 grid. Each row
		// is centered without re-sampling the full killfeed column: the bottom
		// yellow row sits at baseY=461 and the top lime row occupies y=381. The
		// sample at output t=1.8 is 0.3s after the source cue's relative t=1.5,
		// safely after the 0.12s slide/settle. Region means over each row interior
		// keep the assertions robust against one-pixel detector geometry drift
		// and chroma subsampling.
		const (
			noticeAX, noticeAY = 540, 490
			noticeBX, noticeBY = 540, 410
		)
		beforeFrame := extractFramePNG(t, ffmpegPath, outPath, 1.0)
		atCueFrame := extractFramePNG(t, ffmpegPath, outPath, 1.8)
		beforeA := readRegionMean(t, beforeFrame, noticeAX, noticeAY, 40, 6)
		beforeB := readRegionMean(t, beforeFrame, noticeBX, noticeBY, 40, 6)
		atCueA := readRegionMean(t, atCueFrame, noticeAX, noticeAY, 40, 6)
		atCueB := readRegionMean(t, atCueFrame, noticeBX, noticeBY, 40, 6)
		t.Logf("notice regions before cue A=%+v B=%+v, at cue A=%+v B=%+v", beforeA, beforeB, atCueA, atCueB)
		if !isPredominantlyBlue(beforeA) || !isPredominantlyBlue(beforeB) {
			t.Fatalf("notice regions before cue = A %+v B %+v, want blue gameplay background", beforeA, beforeB)
		}
		if !isPredominantlyYellow(atCueA) {
			t.Fatalf("bottom notice region at cue = %+v, want yellow sampled notice", atCueA)
		}
		if !isPredominantlyGreen(atCueB) {
			t.Fatalf("top notice region at cue = %+v, want lime sampled notice", atCueB)
		}
	})

	t.Run("confirmed kills render a synthetic notice at the cue", func(t *testing.T) {
		t.Parallel()
		id := uploadStreamSource(t, client, srv.URL, sourcePath)
		plan := streamclips.EditPlan{
			Variant:      streamclips.VariantStreamerFullframeNoCam,
			GameplayCrop: streamclips.CropRect{X: 0, Y: 0, Width: 1, Height: 1},
			KillfeedCrop: &streamclips.CropRect{X: 0.74, Y: 0.04, Width: 0.25, Height: 0.15},
			Clips: []streamclips.ClipRange{{
				ID:           "clip-1",
				StartSeconds: 0.5,
				EndSeconds:   3.5,
			}},
			Captions: streamclips.CaptionsPlan{Enabled: false},
		}
		putStreamEditPlan(t, client, srv.URL, id, plan)
		_, appliedPlan := startAndApplyStreamKillfeed(t, client, srv.URL, id)
		if len(appliedPlan.Clips) != 1 || len(appliedPlan.Clips[0].KillfeedSeconds) != 1 {
			t.Fatalf("applied automatic killfeed = %+v, want one source-frame cue", appliedPlan.Clips)
		}
		// Keep the detector event and add a separate reviewed manual correction.
		// OCR enrichment at the automatic cue must keep rendering its immutable
		// captured rows, while a cue with no detector event uses this synthetic
		// notice.
		appliedPlan.Clips[0].KillfeedSeconds = append(appliedPlan.Clips[0].KillfeedSeconds, 1.0)
		appliedPlan.Clips[0].KillfeedKills = append(appliedPlan.Clips[0].KillfeedKills, []streamclips.KillfeedKill{{
			AttackerSide: "CT",
			AttackerName: "donk",
			VictimSide:   "T",
			VictimName:   "s1mple",
			Weapon:       "ak47",
			Headshot:     true,
		}})
		putStreamEditPlan(t, client, srv.URL, id, appliedPlan)

		clipID := startAndAwaitStreamRender(t, client, srv.URL, id, streamclips.VariantStreamerFullframeNoCam)
		outPath := downloadStreamVideo(t, client, srv.URL, id, streamclips.VariantStreamerFullframeNoCam, clipID)

		// The synthetic notice overlays horizontally centered with its top at
		// y≈460-461 (killfeedBaseY = round(0.24*1920) = 461; the RGB conversion
		// bleeds the top red row so it reads at y≈460), height 72 with a 3px
		// #F40708 border, and slides in from the right for 0.12s after the cue,
		// so the "at cue" frame samples after the settle. The icon/text content
		// band is vertically centered (tallest content 39px -> rows ~477..515),
		// which leaves rows ~464..476 as plate-only across the full notice
		// width; x=540 is always inside the centered notice. The sample at
		// y=461 lands on the top border either way.
		const (
			plateX, plateY   = 540, 468
			borderX, borderY = 540, 461
		)
		// Source cue 1.0 is output t=0.5 because the clip starts at source 0.5.
		beforeFrame := extractFramePNG(t, ffmpegPath, outPath, 0.2)
		atCueFrame := extractFramePNG(t, ffmpegPath, outPath, 0.8)
		beforePlate := readRegionMean(t, beforeFrame, plateX, plateY, 3, 6)
		atCuePlate := readRegionMean(t, atCueFrame, plateX, plateY, 3, 6)
		atCueBorder := readRegionMean(t, atCueFrame, borderX, borderY, 8, 0)
		t.Logf("synthetic notice plate before=%+v at cue=%+v, top border at cue=%+v", beforePlate, atCuePlate, atCueBorder)
		if !isPredominantlyBlue(beforePlate) {
			t.Fatalf("notice plate region before cue = %+v, want blue gameplay background", beforePlate)
		}
		if atCuePlate.R > 80 || atCuePlate.G > 80 || atCuePlate.B < 60 || atCuePlate.B > 180 {
			t.Fatalf("notice plate region at cue = %+v, want blue darkened by the half-black plate", atCuePlate)
		}
		if !isPredominantlyRed(atCueBorder) {
			t.Fatalf("notice top border at cue = %+v, want red synthetic notice border", atCueBorder)
		}
	})

	t.Run("moved banner slides in and out", func(t *testing.T) {
		t.Parallel()
		if streamclips.FindBannerFont() == "" {
			t.Skip("supported bold system font not found, skipping real banner e2e")
		}
		id := uploadStreamSource(t, client, srv.URL, sourcePath)
		positionY := 0.7
		plan := streamclips.EditPlan{
			Variant:          streamclips.VariantStreamer4060,
			FaceCrop:         streamclips.CropRect{X: 0, Y: 0, Width: 0.25, Height: 0.25},
			FaceCropReviewed: true,
			GameplayCrop:     streamclips.CropRect{X: 0.25, Y: 0.25, Width: 0.75, Height: 0.75},
			Clips:            []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 0.5, EndSeconds: 3.5}},
			StreamerBanner: streamclips.StreamerBannerPlan{
				Nick:         "zacketizorcs2",
				PositionY:    &positionY,
				SlideEnabled: true,
			},
		}
		putStreamEditPlan(t, client, srv.URL, id, plan)

		clipID := startAndAwaitStreamRender(t, client, srv.URL, id, streamclips.VariantStreamer4060)
		outPath := downloadStreamVideo(t, client, srv.URL, id, streamclips.VariantStreamer4060, clipID)

		const bannerCenterY = 1344
		earlyPixel := readPixel(t, extractFramePNG(t, ffmpegPath, outPath, 0.1), 540, bannerCenterY)
		middlePixel := readPixel(t, extractFramePNG(t, ffmpegPath, outPath, 1.5), 540, bannerCenterY)
		latePixel := readPixel(t, extractFramePNG(t, ffmpegPath, outPath, 2.9), 540, bannerCenterY)
		t.Logf("animated banner pixels early=%+v middle=%+v late=%+v", earlyPixel, middlePixel, latePixel)
		if !isPredominantlyBlue(earlyPixel) {
			t.Fatalf("early banner-center pixel = %+v, want blue background during slide-in", earlyPixel)
		}
		if !isPredominantlyPurple(middlePixel) {
			t.Fatalf("middle banner-center pixel = %+v, want purple banner during hold", middlePixel)
		}
		if !isPredominantlyBlue(latePixel) {
			t.Fatalf("late banner-center pixel = %+v, want blue background during slide-out", latePixel)
		}
	})

	t.Run("unknown variant returns 400 listing valid variants", func(t *testing.T) {
		t.Parallel()
		id := uploadStreamSource(t, client, srv.URL, sourcePath)

		req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/stream-jobs/"+id.String()+"/renders/not-a-real-variant", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400, body = %s", resp.StatusCode, body)
		}
		for _, name := range streamclips.VariantNames() {
			if !strings.Contains(string(body), name) {
				t.Fatalf("400 body = %s, want it to list valid variant %q", body, name)
			}
		}
	})
}

// newStreamE2EServer assembles the orchestrator's HTTP handlers with the same
// building blocks as ZV_DATABASE_URL=memory production wiring
// (cmd/zv-orchestrator/main.go): a memory job repo, memory stream job repo,
// inline queue, and a real StreamRenderWorker pointed at the ffmpeg/ffprobe
// binaries on PATH. It also generates the synthetic 16:9 source video used by
// every subtest and returns its path.
func newStreamE2EServer(t *testing.T, ffmpegPath, ffprobePath string) (*httptest.Server, string) {
	t.Helper()

	dataDir := t.TempDir()
	store, err := storage.NewLocal(dataDir)
	if err != nil {
		t.Fatalf("storage.NewLocal: %v", err)
	}

	jobRepo := newMemoryJobRepository()
	streamRepo := newMemoryStreamJobRepository()
	streamJobLocks := streamclips.NewJobLocks()

	streamWorker := workers.NewStreamRenderWorker(streamRepo, store, workers.StreamRenderWorkerConfig{
		WorkDir:                        filepath.Join(dataDir, "work"),
		FFmpegPath:                     ffmpegPath,
		Timeout:                        "2m",
		JobLocks:                       streamJobLocks,
		RequireAppliedKillfeedAnalysis: true,
	})

	taskHandlers := map[string]taskHandler{
		tasks.TypeRenderStreamClip:       streamWorker.HandleRenderStreamClip,
		tasks.TypeGenerateStreamKillfeed: streamWorker.HandleGenerateStreamKillfeed,
	}
	queue := newInlineQueue(taskHandlers, 2)
	ctx, cancel := context.WithCancel(context.Background())
	queue.Start(ctx)
	t.Cleanup(cancel)

	handlers := httpapi.NewHandlers(jobRepo, store, queue,
		httpapi.WithStreamRepository(streamRepo),
		httpapi.WithStreamJobLocks(streamJobLocks),
		httpapi.WithStreamProber(streamclips.FFprobeProber{Path: ffprobePath}),
		httpapi.WithFFmpegPath(ffmpegPath),
	)
	srv := httptest.NewServer(httpapi.Routes(handlers))

	sourcePath := filepath.Join(dataDir, "source.mp4")
	generateSyntheticSource(t, ffmpegPath, sourcePath)

	return srv, sourcePath
}

// generateSyntheticSource builds a 1280x720, 4s, 30fps clip: a solid blue
// frame with a solid red rectangle over the exact top-left quarter
// (x=[0,320) y=[0,180)) and two staggered CS2-style highlighted kill notices
// at the top right, each an interior bar wrapped in a 2px saturated-red
// highlight ring so the notice-row detector can find it. Both notices appear
// on the exact source frame at t=2s: notice A is lime,
// ring (x=[1022,1250) y=[34,74)); notice B is wider, yellow, shifted left and
// 6px below A like a real staggered killfeed, ring (x=[982,1250) y=[80,118)).
// A sine wave audio track completes the source.
//
// cmd/zv/app_test_support_test.go keeps a deliberate ~20-line duplicate of this
// helper for the stream binary-level e2e (the packages do not share test
// helpers); keep the two in sync.
func generateSyntheticSource(t *testing.T, ffmpegPath, outPath string) {
	t.Helper()
	args := []string{
		"-y",
		"-f", "lavfi", "-i", "color=c=blue:s=1280x720:d=4:r=30",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=4",
		"-filter_complex", "[0:v]drawbox=x=0:y=0:w=320:h=180:color=red:t=fill,drawbox=x=1024:y=36:w=224:h=36:color=lime:t=fill:enable='gte(t,2)',drawbox=x=1022:y=34:w=228:h=40:color=red:t=2:enable='gte(t,2)',drawbox=x=984:y=82:w=264:h=32:color=yellow:t=fill:enable='gte(t,2)',drawbox=x=982:y=80:w=268:h=38:color=red:t=2:enable='gte(t,2)'[v]",
		"-map", "[v]",
		"-map", "1:a",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-shortest",
		outPath,
	}
	runFFmpeg(t, ffmpegPath, args...)
}

func runFFmpeg(t *testing.T, ffmpegPath string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// #nosec G204 -- ffmpegPath comes from exec.LookPath and args are test-local literals.
	cmd := exec.CommandContext(ctx, ffmpegPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg %v failed: %v\n%s", args, err, out)
	}
}

func uploadStreamSource(t *testing.T, client *http.Client, baseURL, sourcePath string) uuid.UUID {
	t.Helper()
	f, err := os.Open(sourcePath)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("video", "source.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, f); err != nil {
		t.Fatal(err)
	}
	if err := mw.WriteField("config", `{"title":"e2e synthetic source"}`); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/stream-jobs", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create stream job status = %d, body = %s", resp.StatusCode, respBody)
	}

	var created struct {
		ID     uuid.UUID               `json:"id"`
		Status streamclips.Status      `json:"status"`
		Probe  streamclips.SourceProbe `json:"probe"`
	}
	if err := json.Unmarshal(respBody, &created); err != nil {
		t.Fatalf("decode create response: %v\nbody = %s", err, respBody)
	}
	if created.Status != streamclips.StatusUploaded {
		t.Fatalf("status = %s, want uploaded", created.Status)
	}
	if created.Probe.Width != 1280 || created.Probe.Height != 720 {
		t.Fatalf("upload probe = %+v, want 1280x720", created.Probe)
	}
	return created.ID
}

func putStreamEditPlan(t *testing.T, client *http.Client, baseURL string, id uuid.UUID, plan streamclips.EditPlan) {
	t.Helper()
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, baseURL+"/api/stream-jobs/"+id.String()+"/edit-plan", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put edit plan status = %d, body = %s", resp.StatusCode, body)
	}
}

// startAndApplyStreamKillfeed exercises the same durable generation boundary
// as Studio: enqueue, poll the active generation, then atomically apply its
// source-PTS events to the edit plan.
func startAndApplyStreamKillfeed(
	t *testing.T,
	client *http.Client,
	baseURL string,
	id uuid.UUID,
) (streamclips.KillfeedAnalysisState, streamclips.EditPlan) {
	t.Helper()
	endpoint := baseURL + "/api/stream-jobs/" + id.String() + "/killfeed"
	startReq, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	startResp, err := client.Do(startReq)
	if err != nil {
		t.Fatal(err)
	}
	startBody, _ := io.ReadAll(startResp.Body)
	startResp.Body.Close()
	if startResp.StatusCode != http.StatusAccepted {
		t.Fatalf("start killfeed analysis status = %d, body = %s", startResp.StatusCode, startBody)
	}

	deadline := time.Now().Add(90 * time.Second)
	var state streamclips.KillfeedAnalysisState
	for time.Now().Before(deadline) {
		getResp, err := client.Get(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		getBody, _ := io.ReadAll(getResp.Body)
		getResp.Body.Close()
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("get killfeed analysis status = %d, body = %s", getResp.StatusCode, getBody)
		}
		if err := json.Unmarshal(getBody, &state); err != nil {
			t.Fatalf("decode killfeed analysis: %v\nbody = %s", err, getBody)
		}
		switch state.Status {
		case streamclips.KillfeedAnalysisReady:
			goto apply
		case streamclips.KillfeedAnalysisReviewRequired, streamclips.KillfeedAnalysisFailed:
			t.Fatalf("killfeed analysis ended as %s: %s (%+v)", state.Status, state.Error, state.Clips)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("killfeed analysis did not finish within deadline")

apply:
	applyBody := strings.NewReader(`{"generation_id":"` + state.GenerationID.String() + `"}`)
	applyReq, err := http.NewRequest(http.MethodPost, endpoint+"/apply", applyBody)
	if err != nil {
		t.Fatal(err)
	}
	applyReq.Header.Set("Content-Type", "application/json")
	applyResp, err := client.Do(applyReq)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(applyResp.Body)
	applyResp.Body.Close()
	if applyResp.StatusCode != http.StatusOK {
		t.Fatalf("apply killfeed analysis status = %d, body = %s", applyResp.StatusCode, body)
	}
	var plan streamclips.EditPlan
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatalf("decode applied killfeed plan: %v\nbody = %s", err, body)
	}
	return state, plan
}

// startAndAwaitStreamRender POSTs the render for variant and polls GET until
// the render reaches a terminal state, bounded by a deadline so a stuck
// render fails the test instead of hanging forever. It returns the rendered
// clip id.
func startAndAwaitStreamRender(t *testing.T, client *http.Client, baseURL string, id uuid.UUID, variant string) string {
	t.Helper()
	startReq, err := http.NewRequest(http.MethodPost, baseURL+"/api/stream-jobs/"+id.String()+"/renders/"+variant, nil)
	if err != nil {
		t.Fatal(err)
	}
	startResp, err := client.Do(startReq)
	if err != nil {
		t.Fatal(err)
	}
	startBody, _ := io.ReadAll(startResp.Body)
	startResp.Body.Close()
	if startResp.StatusCode != http.StatusAccepted {
		t.Fatalf("start render status = %d, body = %s", startResp.StatusCode, startBody)
	}

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		getResp, err := client.Get(baseURL + "/api/stream-jobs/" + id.String() + "/renders/" + variant)
		if err != nil {
			t.Fatal(err)
		}
		getBody, _ := io.ReadAll(getResp.Body)
		getResp.Body.Close()
		if getResp.StatusCode != http.StatusOK {
			t.Fatalf("get render status = %d, body = %s", getResp.StatusCode, getBody)
		}
		var state struct {
			Status streamclips.Status       `json:"status"`
			Error  string                   `json:"error"`
			Videos []streamclips.VideoEntry `json:"videos"`
		}
		if err := json.Unmarshal(getBody, &state); err != nil {
			t.Fatalf("decode render state: %v\nbody = %s", err, getBody)
		}
		switch state.Status {
		case streamclips.StatusRendered:
			if len(state.Videos) == 0 {
				t.Fatal("render state is rendered but has no videos")
			}
			return state.Videos[0].ClipID
		case streamclips.StatusFailed:
			t.Fatalf("render failed: %s", state.Error)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("render did not finish within deadline (variant=%s)", variant)
	return ""
}

func downloadStreamVideo(t *testing.T, client *http.Client, baseURL string, id uuid.UUID, variant, clipID string) string {
	t.Helper()
	resp, err := client.Get(baseURL + "/api/stream-jobs/" + id.String() + "/renders/" + variant + "/videos/" + clipID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("get render video status = %d, body = %s", resp.StatusCode, body)
	}
	outPath := filepath.Join(t.TempDir(), variant+"-"+clipID+".mp4")
	f, err := os.Create(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		t.Fatal(err)
	}
	return outPath
}

type videoProbe struct {
	Width      int
	Height     int
	FPS        float64
	VideoCodec string
	AudioCodec string
	Duration   float64
}

func ffprobeVideo(t *testing.T, ffprobePath, path string) videoProbe {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// #nosec G204 -- ffprobePath comes from exec.LookPath and path is a test-local temp file.
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,width,height,r_frame_rate:format=duration",
		"-of", "json",
		path,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe failed: %v\n%s", err, out)
	}
	var raw struct {
		Streams []struct {
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			RFrameRate string `json:"r_frame_rate"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatalf("decode ffprobe output: %v\n%s", err, out)
	}
	probe := videoProbe{}
	if d, err := strconv.ParseFloat(strings.TrimSpace(raw.Format.Duration), 64); err == nil {
		probe.Duration = d
	}
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video":
			probe.Width = s.Width
			probe.Height = s.Height
			probe.VideoCodec = s.CodecName
			probe.FPS = parseFrameRate(s.RFrameRate)
		case "audio":
			probe.AudioCodec = s.CodecName
		}
	}
	return probe
}

func parseFrameRate(raw string) float64 {
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 {
		f, _ := strconv.ParseFloat(raw, 64)
		return f
	}
	num, err1 := strconv.ParseFloat(parts[0], 64)
	den, err2 := strconv.ParseFloat(parts[1], 64)
	if err1 != nil || err2 != nil || den == 0 {
		return 0
	}
	return num / den
}

// extractFramePNG extracts a single frame at atSeconds as a PNG so the test
// can decode and assert on pixel colors in Go, without relying on ffmpeg's
// own (untested-here) color math a second time.
func extractFramePNG(t *testing.T, ffmpegPath, videoPath string, atSeconds float64) string {
	t.Helper()
	framePath := filepath.Join(t.TempDir(), "frame.png")
	runFFmpeg(t, ffmpegPath,
		"-y",
		"-ss", strconv.FormatFloat(atSeconds, 'f', 3, 64),
		"-i", videoPath,
		"-frames:v", "1",
		framePath,
	)
	return framePath
}

func readPixel(t *testing.T, pngPath string, x, y int) color.RGBA {
	t.Helper()
	f, err := os.Open(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	bounds := img.Bounds()
	if x < bounds.Min.X || x >= bounds.Max.X || y < bounds.Min.Y || y >= bounds.Max.Y {
		t.Fatalf("pixel (%d,%d) is outside frame bounds %v", x, y, bounds)
	}
	r, g, b, a := img.At(x, y).RGBA()
	return color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
}

// isPredominantlyRed/Blue allow for encoder (libx264 4:2:0 chroma subsampling
// and CRF quantization) tolerance around a saturated fill color.
func isPredominantlyRed(c color.RGBA) bool {
	return c.R > 150 && c.G < 100 && c.B < 100
}

func isPredominantlyBlue(c color.RGBA) bool {
	return c.B > 150 && c.R < 100 && c.G < 100
}

func isPredominantlyGreen(c color.RGBA) bool {
	return c.G > 150 && c.R < 100 && c.B < 100
}

func isPredominantlyYellow(c color.RGBA) bool {
	return c.R > 150 && c.G > 150 && c.B < 100
}

// readRegionMean averages the pixels of the (2*halfW+1)x(2*halfH+1) region
// centered on (cx,cy), keeping notice-interior assertions robust against
// one-pixel overlay geometry drift and 4:2:0 chroma subsampling.
func readRegionMean(t *testing.T, pngPath string, cx, cy, halfW, halfH int) color.RGBA {
	t.Helper()
	f, err := os.Open(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	region := image.Rect(cx-halfW, cy-halfH, cx+halfW+1, cy+halfH+1)
	if !region.In(img.Bounds()) {
		t.Fatalf("region %v is outside frame bounds %v", region, img.Bounds())
	}
	var rSum, gSum, bSum, aSum, n uint64
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			rSum += uint64(r >> 8)
			gSum += uint64(g >> 8)
			bSum += uint64(b >> 8)
			aSum += uint64(a >> 8)
			n++
		}
	}
	return color.RGBA{R: uint8(rSum / n), G: uint8(gSum / n), B: uint8(bSum / n), A: uint8(aSum / n)}
}

func isPredominantlyPurple(c color.RGBA) bool {
	return c.R > 100 && c.G < 120 && c.B > 150
}
