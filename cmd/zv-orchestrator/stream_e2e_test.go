package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	_ "image/png"
	"io"
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
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found on PATH, skipping real stream-render e2e")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not found on PATH, skipping real stream-render e2e")
	}

	srv, sourcePath := newStreamE2EServer(t, ffmpegPath, ffprobePath)
	defer srv.Close()
	client := srv.Client()

	t.Run("40-60 vertical stack renders correct geometry", func(t *testing.T) {
		id := uploadStreamSource(t, client, srv.URL, sourcePath)

		plan := streamclips.EditPlan{
			Variant:      streamclips.VariantStreamer4060,
			FaceCrop:     streamclips.CropRect{X: 0, Y: 0, Width: 0.25, Height: 0.25},
			GameplayCrop: streamclips.CropRect{X: 0.25, Y: 0.25, Width: 0.75, Height: 0.75},
			Clips:        []streamclips.ClipRange{{ID: "clip-1", StartSeconds: 0.5, EndSeconds: 3.5}},
			Captions:     streamclips.CaptionsPlan{Enabled: false},
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

	t.Run("unknown variant returns 400 listing valid variants", func(t *testing.T) {
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

	streamWorker := workers.NewStreamRenderWorker(streamRepo, store, workers.StreamRenderWorkerConfig{
		WorkDir:    filepath.Join(dataDir, "work"),
		FFmpegPath: ffmpegPath,
		Timeout:    "2m",
	})

	taskHandlers := map[string]taskHandler{
		tasks.TypeRenderStreamClip: streamWorker.HandleRenderStreamClip,
	}
	queue := newInlineQueue(taskHandlers, 2)
	ctx, cancel := context.WithCancel(context.Background())
	queue.Start(ctx)
	t.Cleanup(cancel)

	handlers := httpapi.NewHandlers(jobRepo, store, queue,
		httpapi.WithStreamRepository(streamRepo),
		httpapi.WithStreamProber(streamclips.FFprobeProber{Path: ffprobePath}),
	)
	srv := httptest.NewServer(httpapi.Routes(handlers))

	sourcePath := filepath.Join(dataDir, "source.mp4")
	generateSyntheticSource(t, ffmpegPath, sourcePath)

	return srv, sourcePath
}

// generateSyntheticSource builds a 1280x720, 4s, 30fps clip: a solid blue
// frame with a solid red rectangle over the exact top-left quarter
// (x=[0,320) y=[0,180)), plus a sine wave audio track. This fakes a facecam
// sitting in the top-left corner over gameplay.
func generateSyntheticSource(t *testing.T, ffmpegPath, outPath string) {
	t.Helper()
	args := []string{
		"-y",
		"-f", "lavfi", "-i", "color=c=blue:s=1280x720:d=4:r=30",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=4",
		"-filter_complex", "[0:v]drawbox=x=0:y=0:w=320:h=180:color=red:t=fill[v]",
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
