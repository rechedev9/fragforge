package streamkillfeed

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

const (
	maxPNGChunkBytes = 256 << 20
	maxStderrLine    = 1 << 20
	maxStderrTail    = 64 << 10
	showinfoLabel    = "showinfo@zv_killfeed"
)

var (
	showinfoTimeBasePattern = regexp.MustCompile(`config in time_base:\s*(-?\d+)/(\d+)`)
	showinfoFramePattern    = regexp.MustCompile(`\bn:\s*(\d+)\s+pts:\s*(-?\d+|NOPTS)\b`)
)

type decodeRequest struct {
	sourcePath       string
	startSeconds     float64
	endSeconds       float64
	coarse           bool
	probeStart       float64
	expectedTimeBase *TimeBase
}

type decodedFrame struct {
	rows []observedRow
}

type frameTimestamp struct {
	ordinal int
	pts     int64
}

type stdoutResult struct {
	frames []decodedFrame
	err    error
}

type stderrResult struct {
	timestamps []frameTimestamp
	timeBase   TimeBase
	tail       string
	err        error
}

func (a Analyzer) decodeRange(
	ctx context.Context,
	request decodeRequest,
	crop streamclips.CropRect,
) ([]frameObservation, error) {
	if request.endSeconds <= request.startSeconds {
		return nil, fmt.Errorf("decode range end must be greater than start")
	}
	if a.FFmpegPath == "" {
		return nil, fmt.Errorf("ffmpeg path is required")
	}
	if strings.TrimSpace(request.sourcePath) == "" {
		return nil, fmt.Errorf("source path is required")
	}
	if err := crop.Validate("killfeed_crop"); err != nil {
		return nil, err
	}

	// Bound the interval inside the filtergraph so showinfo observes exactly
	// the frames sent to the PNG encoder. An output-level -t can discard a
	// boundary frame after showinfo, breaking the required 1:1 pairing.
	filter := "trim=end=" + formatFFmpegSeconds(request.probeStart+request.endSeconds)
	if request.coarse {
		filter += ",fps=" + strconv.Itoa(CoarseFPS)
	}
	// NoticeRow bounds are a durable contract shared with the exact-row
	// extractor. Keep both the coarse locator and native refinement on the same
	// 1080-high pixel grid so a SampleBounds rectangle always addresses the
	// exact pixels that produced its fingerprint.
	filter += ",scale=-2:1080," + showinfoLabel + "=checksum=0"
	args := []string{
		"-hide_banner",
		"-nostdin",
		"-nostats",
		"-loglevel", "info",
		"-copyts",
		"-ss", formatFFmpegSeconds(request.startSeconds),
		"-i", request.sourcePath,
		"-map", "0:v:0",
		"-vf", filter,
		"-an", "-sn", "-dn",
		"-fps_mode:v", "passthrough",
		"-c:v", "png",
		"-f", "image2pipe",
		"pipe:1",
	}

	decodeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// #nosec G204 -- ffmpeg is operator-configured and arguments are never shell-expanded.
	cmd := exec.CommandContext(decodeCtx, a.FFmpegPath, args...)
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

	stdoutDone := make(chan stdoutResult, 1)
	stderrDone := make(chan stderrResult, 1)
	go func() {
		result := readPNGFrames(stdout, crop)
		if result.err != nil {
			cancel()
		}
		stdoutDone <- result
	}()
	go func() {
		result := readShowinfo(stderr)
		if result.err != nil {
			cancel()
		}
		stderrDone <- result
	}()

	stdoutRead := <-stdoutDone
	stderrRead := <-stderrDone
	waitErr := cmd.Wait()
	if stdoutRead.err != nil {
		return nil, fmt.Errorf("decode ffmpeg PNG stream: %w", stdoutRead.err)
	}
	if stderrRead.err != nil {
		return nil, fmt.Errorf("parse ffmpeg showinfo: %w", stderrRead.err)
	}
	if waitErr != nil {
		if stderrRead.tail != "" {
			return nil, fmt.Errorf(
				"ffmpeg decode range: %w: %s",
				waitErr, strings.TrimSpace(stderrRead.tail),
			)
		}
		return nil, fmt.Errorf("ffmpeg decode range: %w", waitErr)
	}
	if len(stdoutRead.frames) != len(stderrRead.timestamps) {
		return nil, fmt.Errorf(
			"ffmpeg frame metadata cardinality mismatch: %d PNG frames, %d showinfo timestamps",
			len(stdoutRead.frames), len(stderrRead.timestamps),
		)
	}
	if len(stdoutRead.frames) == 0 {
		return []frameObservation{}, nil
	}
	if err := stderrRead.timeBase.Validate(); err != nil {
		return nil, fmt.Errorf("ffmpeg showinfo time base: %w", err)
	}
	if request.expectedTimeBase != nil &&
		!equivalentTimeBase(*request.expectedTimeBase, stderrRead.timeBase) {
		return nil, fmt.Errorf(
			"native frame time base %s differs from probe %s",
			stderrRead.timeBase, *request.expectedTimeBase,
		)
	}

	frames := make([]frameObservation, len(stdoutRead.frames))
	for i := range stdoutRead.frames {
		timestamp := stderrRead.timestamps[i]
		if timestamp.ordinal != i {
			return nil, fmt.Errorf(
				"ffmpeg frame ordinal mismatch: PNG %d has showinfo n=%d",
				i, timestamp.ordinal,
			)
		}
		if i > 0 && timestamp.pts <= stderrRead.timestamps[i-1].pts {
			return nil, fmt.Errorf(
				"ffmpeg PTS must increase strictly: frame %d has %d after %d",
				i, timestamp.pts, stderrRead.timestamps[i-1].pts,
			)
		}
		frames[i] = frameObservation{
			pts:      timestamp.pts,
			timeBase: stderrRead.timeBase,
			seconds:  relativeVideoSeconds(timestamp.pts, stderrRead.timeBase, request.probeStart),
			rows:     stdoutRead.frames[i].rows,
		}
	}
	return frames, nil
}

func readPNGFrames(reader io.Reader, crop streamclips.CropRect) stdoutResult {
	result := stdoutResult{}
	for {
		frame, err := decodePNG(reader)
		if errors.Is(err, io.EOF) {
			return result
		}
		if err != nil {
			result.err = err
			return result
		}
		rows := streamclips.DetectNoticeRows(frame, &crop)
		result.frames = append(result.frames, decodedFrame{rows: observeRows(frame, rows)})
	}
}

func readShowinfo(reader io.Reader) stderrResult {
	result := stderrResult{}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxStderrLine)
	expectedOrdinal := 0
	for scanner.Scan() {
		line := scanner.Text()
		appendStderrTail(&result.tail, line)
		if !strings.Contains(line, showinfoLabel) {
			continue
		}
		if match := showinfoTimeBasePattern.FindStringSubmatch(line); match != nil {
			timeBase, err := ParseTimeBase(match[1] + "/" + match[2])
			if err != nil {
				result.err = err
				continue
			}
			if result.timeBase.Den != 0 && !equivalentTimeBase(result.timeBase, timeBase) {
				result.err = fmt.Errorf(
					"time base changed from %s to %s",
					result.timeBase, timeBase,
				)
				continue
			}
			result.timeBase = timeBase
			continue
		}
		match := showinfoFramePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		ordinal, err := strconv.Atoi(match[1])
		if err != nil {
			result.err = fmt.Errorf("parse showinfo ordinal %q: %w", match[1], err)
			continue
		}
		if ordinal != expectedOrdinal {
			result.err = fmt.Errorf(
				"showinfo n must be sequential: got %d, want %d",
				ordinal, expectedOrdinal,
			)
			continue
		}
		expectedOrdinal++
		if match[2] == "NOPTS" {
			result.err = fmt.Errorf("showinfo frame %d has no PTS", ordinal)
			continue
		}
		pts, err := strconv.ParseInt(match[2], 10, 64)
		if err != nil {
			result.err = fmt.Errorf("parse showinfo PTS %q: %w", match[2], err)
			continue
		}
		result.timestamps = append(result.timestamps, frameTimestamp{ordinal: ordinal, pts: pts})
	}
	if err := scanner.Err(); err != nil {
		result.err = fmt.Errorf("read ffmpeg stderr: %w", err)
	}
	if result.timeBase.Den == 0 && result.err == nil {
		result.err = fmt.Errorf("showinfo did not report a time base")
	}
	return result
}

func appendStderrTail(tail *string, line string) {
	*tail += line + "\n"
	if len(*tail) > maxStderrTail {
		*tail = (*tail)[len(*tail)-maxStderrTail:]
	}
}

func decodePNG(reader io.Reader) (image.Image, error) {
	var signature [8]byte
	if _, err := io.ReadFull(reader, signature[:]); err != nil {
		return nil, err
	}
	if !bytes.Equal(signature[:], []byte("\x89PNG\r\n\x1a\n")) {
		return nil, fmt.Errorf("invalid PNG signature")
	}
	var encoded bytes.Buffer
	encoded.Write(signature[:])
	for {
		var header [8]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			return nil, err
		}
		length := binary.BigEndian.Uint32(header[:4])
		if length > maxPNGChunkBytes {
			return nil, fmt.Errorf("PNG chunk is too large: %d bytes", length)
		}
		encoded.Write(header[:])
		body := make([]byte, int(length)+4)
		if _, err := io.ReadFull(reader, body); err != nil {
			return nil, err
		}
		encoded.Write(body)
		if string(header[4:]) == "IEND" {
			break
		}
	}
	return png.Decode(bytes.NewReader(encoded.Bytes()))
}

func formatFFmpegSeconds(seconds float64) string {
	return strconv.FormatFloat(seconds, 'f', 9, 64)
}

func relativeVideoSeconds(pts int64, timeBase TimeBase, sourceStart float64) float64 {
	return timeBase.Seconds(pts) - sourceStart
}
