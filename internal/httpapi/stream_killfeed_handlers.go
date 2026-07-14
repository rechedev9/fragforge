package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"

	"github.com/rechedev9/fragforge/internal/killfeedvision"
	"github.com/rechedev9/fragforge/internal/streamclips"
)

// xaiKeyMissingCode is the machine-readable error code the killfeed-read
// endpoint returns when no xAI API key is configured, so the web client can
// tell a missing credential apart from other 409s.
const xaiKeyMissingCode = "xai_key_missing"

// xaiRequestFailedCode marks a killfeed read that reached xAI but failed
// upstream (auth, quota, network), so the web editor can suggest retrying or
// checking the key instead of reporting an orchestrator bug.
const xaiRequestFailedCode = "xai_request_failed"

const (
	// killfeedCropTargetWidth is the width a killfeed crop is enlarged toward
	// before it is read, so player names are tall enough to transcribe.
	killfeedCropTargetWidth = 1600
	// killfeedCropMaxUpscale bounds that enlargement.
	killfeedCropMaxUpscale = 3
)

// WithFFmpegPath configures the ffmpeg binary used to extract a cue frame for
// the killfeed-read endpoint. An empty path leaves the endpoint returning 409.
func WithFFmpegPath(path string) Option {
	return func(h *Handlers) {
		h.ffmpegPath = path
	}
}

// WithXAIKey configures the xAI API key the killfeed vision reader uses. An
// empty key leaves the killfeed-read endpoint returning a 409 with code
// xai_key_missing. The key is never echoed back to clients.
func WithXAIKey(key string) Option {
	return func(h *Handlers) {
		h.xaiAPIKey = key
	}
}

// readKillfeedRequest is the JSON body for POST
// /api/stream-jobs/{id}/killfeed-read.
type readKillfeedRequest struct {
	ClipID     string  `json:"clip_id"`
	CueSeconds float64 `json:"cue_seconds"`
}

// ReadStreamKillfeed extracts the cue frame from the stream source, crops it to
// the plan's killfeed region, and reads the visible kill notices with the xAI
// vision client. It requires ffmpeg and an xAI key to be configured.
func (h *Handlers) ReadStreamKillfeed(w http.ResponseWriter, r *http.Request) {
	j, ok := h.loadStreamJob(w, r)
	if !ok {
		return
	}
	if !h.requireKillfeedFFmpeg(w) {
		return
	}
	if !h.requireKillfeedVision(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	var req readKillfeedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid killfeed read JSON")
		return
	}
	plan, err := h.currentStreamEditPlan(j)
	if err != nil {
		internalError(w, "load stream edit plan", err)
		return
	}
	if plan.KillfeedCrop == nil {
		writeError(w, http.StatusBadRequest, "edit plan has no killfeed_crop configured")
		return
	}
	clip, ok := findClip(plan.Clips, req.ClipID)
	if !ok {
		writeError(w, http.StatusNotFound, "clip not found in edit plan")
		return
	}
	if math.IsNaN(req.CueSeconds) || math.IsInf(req.CueSeconds, 0) ||
		req.CueSeconds < clip.StartSeconds || req.CueSeconds >= clip.EndSeconds {
		writeError(w, http.StatusBadRequest, "cue_seconds must satisfy start_seconds <= cue < end_seconds")
		return
	}

	frame, err := h.killfeedFrame(r.Context(), j.SourcePath, req.CueSeconds+streamclips.KillfeedSampleDelaySeconds)
	if err != nil {
		internalError(w, "extract killfeed frame", err)
		return
	}
	cropPNG, err := encodeKillfeedCropPNG(frame, *plan.KillfeedCrop)
	if err != nil {
		internalError(w, "encode killfeed crop", err)
		return
	}
	client := killfeedvision.Client{APIKey: h.xaiAPIKey, BaseURL: h.killfeedVisionBaseURL}
	kills, err := client.ReadKillfeed(r.Context(), cropPNG)
	if err != nil {
		// An upstream xAI failure is not this server's fault: surface it as a
		// 502 with a stable code so the web editor can message it apart from
		// an orchestrator bug. The client error text is already bounded and
		// never contains the API key.
		writeCodedError(w, http.StatusBadGateway, xaiRequestFailedCode, err.Error())
		return
	}
	if kills == nil {
		kills = []streamclips.KillfeedKill{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"kills": kills})
}

// PreviewStreamKillfeedNotice renders a single kill notice supplied in the body
// and returns it as a PNG, so the web editor can preview a notice while the user
// edits it. It never touches a job or the network.
func (h *Handlers) PreviewStreamKillfeedNotice(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	var kill streamclips.KillfeedKill
	if err := json.NewDecoder(r.Body).Decode(&kill); err != nil {
		writeError(w, http.StatusBadRequest, "invalid kill notice JSON")
		return
	}
	kill.Weapon = strings.ToLower(strings.TrimSpace(kill.Weapon))
	img, err := streamclips.RenderNotice(kill)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		internalError(w, "encode notice png", err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

// ListStreamKillfeedWeapons returns the weapon icon keys a kill notice may use,
// so the web editor can offer the same catalog the renderer validates against.
func (h *Handlers) ListStreamKillfeedWeapons(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"weapons": streamclips.WeaponKeys()})
}

func (h *Handlers) requireKillfeedFFmpeg(w http.ResponseWriter) bool {
	if h.ffmpegPath != "" {
		return true
	}
	writeError(w, http.StatusConflict, "reading the killfeed is not configured on this machine; install ffmpeg (or set ZV_FFMPEG_PATH) and restart the orchestrator")
	return false
}

func (h *Handlers) requireKillfeedVision(w http.ResponseWriter) bool {
	if strings.TrimSpace(h.xaiAPIKey) != "" {
		return true
	}
	writeCodedError(w, http.StatusConflict, xaiKeyMissingCode, "reading the killfeed needs an xAI API key; configure one in FragForge Studio Settings (or set XAI_API_KEY) and restart the orchestrator")
	return false
}

func findClip(clips []streamclips.ClipRange, id string) (streamclips.ClipRange, bool) {
	id = strings.TrimSpace(id)
	for _, c := range clips {
		if c.ID == id {
			return c, true
		}
	}
	return streamclips.ClipRange{}, false
}

// sourcePathResolver is the optional storage capability of exposing an
// artifact's local filesystem path, so ffmpeg can read a multi-gigabyte stream
// source in place instead of copying it through Open on every killfeed read.
type sourcePathResolver interface {
	ResolvePath(key string) (string, error)
}

// extractKillfeedFrame asks ffmpeg for a single decoded frame of the stream
// source at atSeconds. It reads the source in place when the storage can
// resolve a local path and falls back to materializing a temporary copy
// otherwise. It is the production implementation behind Handlers.killfeedFrame.
func (h *Handlers) extractKillfeedFrame(ctx context.Context, sourceKey string, atSeconds float64) (image.Image, error) {
	if resolver, ok := h.storage.(sourcePathResolver); ok {
		srcName, err := resolver.ResolvePath(sourceKey)
		if err != nil {
			return nil, fmt.Errorf("resolve stream source path: %w", err)
		}
		return h.ffmpegFramePNG(ctx, srcName, atSeconds)
	}
	srcName, cleanup, err := h.materializeSource(sourceKey)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return h.ffmpegFramePNG(ctx, srcName, atSeconds)
}

// materializeSource copies the stream source to a temporary file for storages
// that cannot expose a local path. The caller must invoke cleanup.
func (h *Handlers) materializeSource(sourceKey string) (string, func(), error) {
	rc, err := h.storage.Open(sourceKey)
	if err != nil {
		return "", nil, fmt.Errorf("open stream source: %w", err)
	}
	srcFile, err := os.CreateTemp("", "zv-killfeed-src-*")
	if err != nil {
		_ = rc.Close()
		return "", nil, err
	}
	srcName := srcFile.Name()
	cleanup := func() { _ = os.Remove(srcName) }
	_, copyErr := io.Copy(srcFile, rc)
	closeSrcErr := srcFile.Close()
	closeRCErr := rc.Close()
	if err := errors.Join(copyErr, closeSrcErr, closeRCErr); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("materialize stream source: %w", err)
	}
	return srcName, cleanup, nil
}

// ffmpegFramePNG extracts one decoded frame at atSeconds from the media file at
// srcName into a temporary PNG and decodes it.
func (h *Handlers) ffmpegFramePNG(ctx context.Context, srcName string, atSeconds float64) (image.Image, error) {
	frameFile, err := os.CreateTemp("", "zv-killfeed-frame-*.png")
	if err != nil {
		return nil, err
	}
	frameName := frameFile.Name()
	_ = frameFile.Close()
	defer os.Remove(frameName)

	args := []string{
		"-y",
		"-loglevel", "error",
		"-ss", strconv.FormatFloat(atSeconds, 'f', 3, 64),
		"-i", srcName,
		"-frames:v", "1",
		frameName,
	}
	// #nosec G204 -- ffmpegPath is an operator-configured binary and args are a slice, not a shell string.
	cmd := exec.CommandContext(ctx, h.ffmpegPath, args...)
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		if text := strings.TrimSpace(string(out)); text != "" {
			return nil, fmt.Errorf("ffmpeg extract frame: %w: %s", runErr, text)
		}
		return nil, fmt.Errorf("ffmpeg extract frame: %w", runErr)
	}
	return decodePNGFile(frameName)
}

func decodePNGFile(path string) (image.Image, error) {
	// #nosec G304 -- path is a temp file this process just created.
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	frame, decodeErr := png.Decode(f)
	closeErr := f.Close()
	if decodeErr != nil {
		return nil, fmt.Errorf("decode killfeed frame: %w", decodeErr)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return frame, nil
}

// encodeKillfeedCropPNG crops frame to the normalized killfeed region and
// encodes it as PNG for the vision reader.
func encodeKillfeedCropPNG(frame image.Image, crop streamclips.CropRect) ([]byte, error) {
	b := frame.Bounds()
	fw, fh := b.Dx(), b.Dy()
	x0 := b.Min.X + int(crop.X*float64(fw))
	y0 := b.Min.Y + int(crop.Y*float64(fh))
	cw := int(crop.Width * float64(fw))
	ch := int(crop.Height * float64(fh))
	if cw < 1 || ch < 1 {
		return nil, fmt.Errorf("killfeed crop is empty for a %dx%d frame", fw, fh)
	}
	dst := image.NewRGBA(image.Rect(0, 0, cw, ch))
	draw.Draw(dst, dst.Bounds(), frame, image.Pt(x0, y0), draw.Src)
	var buf bytes.Buffer
	if err := png.Encode(&buf, upscaleKillfeedCrop(dst)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// upscaleKillfeedCrop enlarges a killfeed crop before it is read. A 1080p
// source crops to roughly 595x151, where player names are only a few pixels
// tall and the vision model misreads them (verified: "bek667" read as "bk657",
// an awp icon read as an ak47). Nearest-neighbour keeps the notice's hard edges
// and flat colours crisp, which matters because a name's colour decides its
// side. The crop is only enlarged, never shrunk, and the factor is capped so an
// already-large crop is not blown up past the model's image budget.
func upscaleKillfeedCrop(src image.Image) image.Image {
	b := src.Bounds()
	factor := min(killfeedCropTargetWidth/max(b.Dx(), 1), killfeedCropMaxUpscale)
	if factor < 2 {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*factor, b.Dy()*factor))
	xdraw.NearestNeighbor.Scale(dst, dst.Bounds(), src, b, xdraw.Src, nil)
	return dst
}

func writeCodedError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"code": code, "error": msg})
}
