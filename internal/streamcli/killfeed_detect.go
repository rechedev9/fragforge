package streamcli

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"math/bits"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

const (
	streamKillfeedTimelineFPS          = 8
	streamKillfeedLookbackSeconds      = 8
	streamKillfeedStabilityFrames      = 2
	streamKillfeedIdentitySeconds      = 1.0
	streamKillfeedLowerCountSeconds    = 1.0
	streamKillfeedIdentityGraceSeconds = 2.8
	streamKillfeedCueMergeWindow       = 0.2

	streamFingerprintWidth          = 64
	streamFingerprintHeight         = 16
	streamFingerprintWords          = streamFingerprintWidth * streamFingerprintHeight / 64
	streamFingerprintMinFeatures    = 16
	streamFingerprintMaxDistance    = 48
	streamFingerprintMinimumOverlap = 65
	streamPNGMaxChunkBytes          = 256 << 20
)

type streamNoticeFingerprint struct {
	bits     [streamFingerprintWords]uint64
	features int
}

// streamNoticeDetector tracks both stable row counts and normalized notice
// content. Count rises handle ordinary multikills. A same-count replacement is
// emitted only after its new identity remains stable for a full second and the
// prior birth's entrance/fade grace has elapsed. The scanner seeds this state
// from an eight-second pre-roll, so notices already visible at the clip boundary
// do not become fabricated clip-start kills.
type streamNoticeDetector struct {
	initialized bool
	peakRows    int
	baseline    []streamNoticeFingerprint
	lastCue     float64

	riseRows   int
	riseFirst  float64
	riseFrames int

	lowerRows  int
	lowerSince float64
	lowerSet   bool

	identityCandidate []streamNoticeFingerprint
	identityFirst     float64
}

func (d *streamNoticeDetector) Observe(seconds, reportFrom float64, fingerprints []streamNoticeFingerprint) (float64, bool) {
	rows := len(fingerprints)
	if !d.initialized {
		d.initialized = true
		d.peakRows = rows
		d.baseline = copyStreamFingerprints(fingerprints)
		d.lastCue = math.Inf(-1)
		return 0, false
	}

	if rows > d.peakRows {
		if d.riseRows == 0 {
			d.riseFirst = seconds
			d.riseFrames = 1
		} else {
			d.riseFrames++
		}
		d.riseRows = max(d.riseRows, rows)
		d.lowerSet = false
		d.clearIdentityCandidate()
		if d.riseFrames < streamKillfeedStabilityFrames {
			return 0, false
		}
		d.peakRows = d.riseRows
		d.baseline = copyStreamFingerprints(fingerprints)
		cue := max(0, d.riseFirst-1.0/streamKillfeedTimelineFPS)
		d.lastCue = cue
		d.clearRise()
		if cue < reportFrom {
			return 0, false
		}
		return cue, true
	}

	d.clearRise()
	if rows < d.peakRows {
		d.clearIdentityCandidate()
		if !d.lowerSet || rows != d.lowerRows {
			d.lowerRows = rows
			d.lowerSince = seconds
			d.lowerSet = true
			return 0, false
		}
		if seconds-d.lowerSince >= streamKillfeedLowerCountSeconds {
			d.peakRows = rows
			d.baseline = copyStreamFingerprints(fingerprints)
			d.lowerSet = false
		}
		return 0, false
	}

	d.lowerSet = false
	if seconds-d.lastCue < streamKillfeedIdentityGraceSeconds {
		// Keep learning the same notice through its entrance and early reflow.
		d.baseline = copyStreamFingerprints(fingerprints)
		d.clearIdentityCandidate()
		return 0, false
	}
	if sameStreamNoticeSet(d.baseline, fingerprints) {
		d.clearIdentityCandidate()
		return 0, false
	}
	if len(d.identityCandidate) == 0 || !sameStreamNoticeSet(d.identityCandidate, fingerprints) {
		d.identityCandidate = copyStreamFingerprints(fingerprints)
		d.identityFirst = seconds
		return 0, false
	}
	if seconds-d.identityFirst < streamKillfeedIdentitySeconds {
		return 0, false
	}
	cue := max(0, d.identityFirst-1.0/streamKillfeedTimelineFPS)
	d.baseline = copyStreamFingerprints(fingerprints)
	d.lastCue = cue
	d.clearIdentityCandidate()
	if cue < reportFrom {
		return 0, false
	}
	return cue, true
}

func (d *streamNoticeDetector) clearRise() {
	d.riseRows = 0
	d.riseFirst = 0
	d.riseFrames = 0
}

func (d *streamNoticeDetector) clearIdentityCandidate() {
	d.identityCandidate = nil
	d.identityFirst = 0
}

func detectKillfeedCues(ctx context.Context, ffmpeg, input string, crop streamclips.CropRect, start, end float64) ([]float64, error) {
	if end <= start {
		return nil, fmt.Errorf("clip end must be greater than clip start")
	}
	if err := crop.Validate("killfeed_crop"); err != nil {
		return nil, err
	}
	scanStart := max(0, start-streamKillfeedLookbackSeconds)
	filter := fmt.Sprintf("fps=%d,scale=-2:1080", streamKillfeedTimelineFPS)
	args := []string{
		"-loglevel", "error",
		"-ss", strconv.FormatFloat(scanStart, 'f', 3, 64),
		"-t", strconv.FormatFloat(end-scanStart, 'f', 3, 64),
		"-i", input,
		"-vf", filter,
		"-an", "-sn", "-dn",
		"-f", "image2pipe",
		"-vcodec", "png",
		"pipe:1",
	}
	// #nosec G204 -- ffmpeg is operator-configured and every argument is passed without a shell.
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var detector streamNoticeDetector
	var cues []float64
	frameIndex := 0
	for {
		frame, decodeErr := decodeStreamPNG(stdout)
		if errors.Is(decodeErr, io.EOF) {
			break
		}
		if decodeErr != nil {
			// The decoder owns no child resources; stop ffmpeg before waiting.
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, fmt.Errorf("decode killfeed timeline frame: %w", decodeErr)
		}
		seconds := scanStart + float64(frameIndex)/streamKillfeedTimelineFPS
		frameIndex++
		rows := streamclips.DetectNoticeRows(frame, &crop)
		fingerprints := fingerprintStreamNoticeRows(frame, rows)
		if cue, ok := detector.Observe(seconds, start, fingerprints); ok {
			cues = append(cues, math.Round(cue*1000)/1000)
		}
	}
	if waitErr := cmd.Wait(); waitErr != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return nil, fmt.Errorf("ffmpeg extract killfeed timeline: %w: %s", waitErr, message)
		}
		return nil, fmt.Errorf("ffmpeg extract killfeed timeline: %w", waitErr)
	}
	if frameIndex == 0 {
		return nil, fmt.Errorf("ffmpeg extracted no killfeed timeline frames")
	}
	return mergeStreamKillfeedCues(cues), nil
}

func decodeStreamPNG(r io.Reader) (image.Image, error) {
	var signature [8]byte
	if _, err := io.ReadFull(r, signature[:]); err != nil {
		return nil, err
	}
	if !bytes.Equal(signature[:], []byte("\x89PNG\r\n\x1a\n")) {
		return nil, fmt.Errorf("invalid PNG signature")
	}
	var encoded bytes.Buffer
	encoded.Write(signature[:])
	for {
		var header [8]byte
		if _, err := io.ReadFull(r, header[:]); err != nil {
			return nil, err
		}
		length := binary.BigEndian.Uint32(header[:4])
		if length > streamPNGMaxChunkBytes {
			return nil, fmt.Errorf("PNG chunk is too large: %d bytes", length)
		}
		encoded.Write(header[:])
		body := make([]byte, int(length)+4) // chunk data plus CRC
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, err
		}
		encoded.Write(body)
		if string(header[4:]) == "IEND" {
			break
		}
	}
	return png.Decode(bytes.NewReader(encoded.Bytes()))
}

func fingerprintStreamNoticeRows(frame image.Image, rows []streamclips.NoticeRow) []streamNoticeFingerprint {
	fingerprints := make([]streamNoticeFingerprint, len(rows))
	for i, row := range rows {
		fingerprints[i] = fingerprintStreamNotice(frame, row)
	}
	return fingerprints
}

func fingerprintStreamNotice(frame image.Image, row streamclips.NoticeRow) streamNoticeFingerprint {
	if frame == nil {
		return streamNoticeFingerprint{}
	}
	rect := image.Rect(row.X, row.Y, row.X+row.Width, row.Y+row.Height).Intersect(frame.Bounds())
	if rect.Empty() {
		return streamNoticeFingerprint{}
	}
	var fingerprint streamNoticeFingerprint
	for outY := range streamFingerprintHeight {
		for outX := range streamFingerprintWidth {
			x := rect.Min.X + (2*outX+1)*rect.Dx()/(2*streamFingerprintWidth)
			y := rect.Min.Y + (2*outY+1)*rect.Dy()/(2*streamFingerprintHeight)
			r16, g16, b16, _ := frame.At(x, y).RGBA()
			r, g, b := uint8(r16>>8), uint8(g16>>8), uint8(b16>>8)
			maxRGB := max(r, max(g, b))
			minRGB := min(r, min(g, b))
			redBorder := r > 120 && g < 90 && b < 90
			foreground := !redBorder && maxRGB >= 135 &&
				(int(maxRGB)-int(minRGB) >= 40 || int(r)+int(g)+int(b) >= 465)
			if !foreground {
				continue
			}
			bit := outY*streamFingerprintWidth + outX
			fingerprint.bits[bit/64] |= uint64(1) << (bit % 64)
			fingerprint.features++
		}
	}
	return fingerprint
}

func sameStreamNoticeSet(left, right []streamNoticeFingerprint) bool {
	if len(left) != len(right) {
		return false
	}
	matched := make([]bool, len(right))
	for _, want := range left {
		found := false
		for i, got := range right {
			if matched[i] || !matchingStreamNoticeFingerprint(want, got) {
				continue
			}
			matched[i] = true
			found = true
			break
		}
		if !found {
			return false
		}
	}
	return true
}

func matchingStreamNoticeFingerprint(a, b streamNoticeFingerprint) bool {
	if a.features < streamFingerprintMinFeatures || b.features < streamFingerprintMinFeatures {
		return false
	}
	distance := 0
	intersection := 0
	union := 0
	for i := range a.bits {
		distance += bits.OnesCount64(a.bits[i] ^ b.bits[i])
		if distance > streamFingerprintMaxDistance {
			return false
		}
		intersection += bits.OnesCount64(a.bits[i] & b.bits[i])
		union += bits.OnesCount64(a.bits[i] | b.bits[i])
	}
	return union > 0 && intersection*100 >= streamFingerprintMinimumOverlap*union
}

func copyStreamFingerprints(in []streamNoticeFingerprint) []streamNoticeFingerprint {
	return append([]streamNoticeFingerprint(nil), in...)
}

func mergeStreamKillfeedCues(cues []float64) []float64 {
	if len(cues) == 0 {
		return []float64{}
	}
	sort.Float64s(cues)
	merged := cues[:1]
	for _, cue := range cues[1:] {
		if cue-merged[len(merged)-1] <= streamKillfeedCueMergeWindow {
			continue
		}
		merged = append(merged, cue)
	}
	return merged
}
