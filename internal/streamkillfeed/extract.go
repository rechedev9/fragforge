package streamkillfeed

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os/exec"
	"strings"

	"github.com/rechedev9/fragforge/internal/streamclips"
	xdraw "golang.org/x/image/draw"
)

const eventFrameSeekLookbackSeconds = 1

type extractedImage struct {
	frame image.Image
	err   error
}

// ExtractEventRowPNGs selects the event's exact SamplePTS source frame,
// verifies the selected media timestamp, and returns one PNG for each row born
// in that event. SampleBounds addresses the same 1080-high geometry used by
// Scan. Each exact crop is normalized proportionally to KillfeedNoticeHeight,
// which is the compositor's fixed stack-slot contract; no whole-column crop or
// cue-relative re-sampling is involved.
func (a Analyzer) ExtractEventRowPNGs(
	ctx context.Context,
	sourcePath string,
	probe streamclips.SourceProbe,
	event Event,
) ([][]byte, error) {
	if strings.TrimSpace(a.FFmpegPath) == "" {
		return nil, fmt.Errorf("ffmpeg path is required")
	}
	if strings.TrimSpace(sourcePath) == "" {
		return nil, fmt.Errorf("source path is required")
	}
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("validate event: %w", err)
	}
	if math.IsNaN(probe.StartTimeSeconds) || math.IsInf(probe.StartTimeSeconds, 0) {
		return nil, fmt.Errorf("source start time must be finite")
	}
	if strings.TrimSpace(probe.VideoTimeBase) != "" {
		probeTimeBase, err := ParseTimeBase(probe.VideoTimeBase)
		if err != nil {
			return nil, fmt.Errorf("source video time base: %w", err)
		}
		if !equivalentTimeBase(probeTimeBase, event.TimeBase) {
			return nil, fmt.Errorf(
				"event time base %s differs from probe %s",
				event.TimeBase, probeTimeBase,
			)
		}
	}
	wantSeconds := event.TimeBase.Seconds(event.SamplePTS) - probe.StartTimeSeconds
	if math.Abs(wantSeconds-event.SampleSeconds) > 1e-6 {
		return nil, fmt.Errorf(
			"event sample seconds %.9f do not match PTS-derived %.9f",
			event.SampleSeconds, wantSeconds,
		)
	}

	seekSeconds := max(0, event.SampleSeconds-eventFrameSeekLookbackSeconds)
	filter := fmt.Sprintf(
		"select='eq(pts\\,%d)',scale=-2:1080,%s=checksum=0",
		event.SamplePTS, showinfoLabel,
	)
	args := []string{
		"-hide_banner",
		"-nostdin",
		"-nostats",
		"-loglevel", "info",
		"-copyts",
		"-ss", formatFFmpegSeconds(seekSeconds),
		"-i", sourcePath,
		"-map", "0:v:0",
		"-vf", filter,
		"-frames:v", "1",
		"-an", "-sn", "-dn",
		"-fps_mode:v", "passthrough",
		"-c:v", "png",
		"-f", "image2pipe",
		"pipe:1",
	}

	extractCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// #nosec G204 -- ffmpeg is operator-configured and arguments are never shell-expanded.
	cmd := exec.CommandContext(extractCtx, a.FFmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open ffmpeg stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("open ffmpeg stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}

	imageDone := make(chan extractedImage, 1)
	stderrDone := make(chan stderrResult, 1)
	go func() {
		frame, decodeErr := decodePNG(stdout)
		if decodeErr != nil {
			cancel()
		}
		imageDone <- extractedImage{frame: frame, err: decodeErr}
	}()
	go func() {
		result := readShowinfo(stderr)
		if result.err != nil {
			cancel()
		}
		stderrDone <- result
	}()

	decoded := <-imageDone
	metadata := <-stderrDone
	waitErr := cmd.Wait()
	if decoded.err != nil {
		return nil, fmt.Errorf("decode exact killfeed frame: %w", decoded.err)
	}
	if metadata.err != nil {
		return nil, fmt.Errorf("parse exact killfeed frame metadata: %w", metadata.err)
	}
	if waitErr != nil {
		if metadata.tail != "" {
			return nil, fmt.Errorf(
				"ffmpeg extract exact killfeed frame: %w: %s",
				waitErr, strings.TrimSpace(metadata.tail),
			)
		}
		return nil, fmt.Errorf("ffmpeg extract exact killfeed frame: %w", waitErr)
	}
	if len(metadata.timestamps) != 1 {
		return nil, fmt.Errorf(
			"exact killfeed extraction produced %d timestamps, want 1",
			len(metadata.timestamps),
		)
	}
	if metadata.timestamps[0].pts != event.SamplePTS {
		return nil, fmt.Errorf(
			"exact killfeed extraction selected PTS %d, want %d",
			metadata.timestamps[0].pts, event.SamplePTS,
		)
	}
	if !equivalentTimeBase(metadata.timeBase, event.TimeBase) {
		return nil, fmt.Errorf(
			"exact killfeed extraction time base %s differs from event %s",
			metadata.timeBase, event.TimeBase,
		)
	}

	rows := make([][]byte, len(event.Rows))
	for i, evidence := range event.Rows {
		encoded, err := encodeEventRowPNG(decoded.frame, evidence.SampleBounds)
		if err != nil {
			return nil, fmt.Errorf("crop event %s row %d: %w", event.EventID, i, err)
		}
		rows[i] = encoded
	}
	return rows, nil
}

func encodeEventRowPNG(frame image.Image, row streamclips.NoticeRow) ([]byte, error) {
	if frame == nil {
		return nil, fmt.Errorf("frame is nil")
	}
	if row.Width <= 0 || row.Height <= 0 {
		return nil, fmt.Errorf("row bounds must be positive")
	}
	rect := image.Rect(row.X, row.Y, row.X+row.Width, row.Y+row.Height)
	if clipped := rect.Intersect(frame.Bounds()); clipped != rect {
		return nil, fmt.Errorf(
			"row bounds %v fall outside scaled frame %v",
			rect, frame.Bounds(),
		)
	}
	cropped := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(cropped, cropped.Bounds(), frame, rect.Min, draw.Src)
	normalized := cropped
	if rect.Dy() != streamclips.KillfeedNoticeHeight {
		targetWidth := max(1, (rect.Dx()*streamclips.KillfeedNoticeHeight+rect.Dy()/2)/rect.Dy())
		normalized = image.NewRGBA(image.Rect(
			0,
			0,
			targetWidth,
			streamclips.KillfeedNoticeHeight,
		))
		xdraw.CatmullRom.Scale(
			normalized,
			normalized.Bounds(),
			cropped,
			cropped.Bounds(),
			xdraw.Src,
			nil,
		)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, normalized); err != nil {
		return nil, fmt.Errorf("encode PNG: %w", err)
	}
	return encoded.Bytes(), nil
}
