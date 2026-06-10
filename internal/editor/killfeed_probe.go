package editor

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
)

const (
	// killfeedSampleDelaySeconds samples the frame slightly after the kill so
	// the death notice is fully drawn before measuring.
	killfeedSampleDelaySeconds = 0.35
	// killfeedHighlightMargin pads the detected highlight box. The two-pass
	// detection already includes the anti-aliased border ring, so anything
	// wider drags mismatched source background into the overlay.
	killfeedHighlightMargin = 1
	// killfeedMinHighlightPixels filters scene noise: a real highlight border
	// contributes a few hundred saturated red pixels.
	killfeedMinHighlightPixels = 60
	// killfeedBorderSearchRadius bounds the second, looser pass that picks up
	// the border's anti-aliased edge around the saturated-red box without
	// letting distant dim-red scene pixels stretch the crop.
	killfeedBorderSearchRadius = 6
	// killfeedOverlayScale keeps the on-screen overlay size consistent with
	// the historical 360px-crop / 430px-overlay default.
	killfeedOverlayScale = 430.0 / 360.0
)

// refineKillfeedEffects replaces the static killfeed crop defaults with a
// per-kill measurement: it samples the source frame just after each kill,
// finds the red highlight box CS2 draws around the recording player's own
// kill notice, and crops exactly that entry. Detection failures keep the
// static crop and are reported as warnings.
func refineKillfeedEffects(short *ShortEdit, probe func(input string, atSeconds float64) (image.Image, error)) []string {
	if probe == nil {
		return nil
	}
	var warnings []string
	for i := range short.Effects {
		effect := &short.Effects[i]
		if effect.Type != EffectKillfeed {
			continue
		}
		input, sampleAt := killfeedSampleSource(short, *effect)
		if input == "" {
			warnings = append(warnings, fmt.Sprintf("killfeed crop at %.2fs: no source input to probe; keeping default crop", effect.StartSeconds))
			continue
		}
		frame, err := probe(input, sampleAt)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("killfeed crop at %.2fs: %v; keeping default crop", effect.StartSeconds, err))
			continue
		}
		rect, ok := detectKillfeedHighlight(frame)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("killfeed crop at %.2fs: no highlighted kill notice detected in %s; keeping default crop", effect.StartSeconds, input))
			continue
		}
		effect.CropX = rect.Min.X
		effect.CropY = rect.Min.Y
		effect.CropWidth = rect.Dx()
		effect.CropHeight = rect.Dy()
		effect.Width = int(float64(rect.Dx())*killfeedOverlayScale + 0.5)
	}
	return warnings
}

// killfeedSampleSource resolves which source file and timestamp to probe for
// a killfeed effect: the owning part on the compiled timeline, or the short's
// own input for single-clip renders.
func killfeedSampleSource(short *ShortEdit, effect Effect) (string, float64) {
	partIndex, sample := killfeedSamplePart(short, effect)
	if partIndex < 0 {
		return short.Input, sample
	}
	return short.Parts[partIndex].Input, sample
}

// killfeedSamplePart resolves the part index (-1 for single-clip shorts) and
// the in-part timestamp of the frame that represents a killfeed effect. The
// probe measures this exact frame and the render freezes it, so both must
// resolve identically.
func killfeedSamplePart(short *ShortEdit, effect Effect) (int, float64) {
	at := effect.AtSeconds
	if at == 0 {
		at = effect.StartSeconds
	}
	if len(short.Parts) == 0 {
		return -1, at + killfeedSampleDelaySeconds
	}
	partIndex := compilationPartIndexAt(short.Parts, effect)
	part := short.Parts[partIndex]
	sample := at - part.TimelineStartSeconds + killfeedSampleDelaySeconds
	if part.DurationSeconds > 0 && sample > part.DurationSeconds {
		sample = part.DurationSeconds
	}
	if sample < 0 {
		sample = 0
	}
	return partIndex, sample
}

// detectKillfeedHighlight finds the red border CS2 draws around the local
// player's kill notices in two passes: saturated red locates the notice in
// the top-right region, then a looser threshold within a few pixels of that
// box picks up the border's anti-aliased edge. Returns the padded bounding
// box.
func detectKillfeedHighlight(frame image.Image) (image.Rectangle, bool) {
	bounds := frame.Bounds()
	scanRegion := image.Rect(
		bounds.Min.X+bounds.Dx()*3/5,
		bounds.Min.Y,
		bounds.Max.X,
		bounds.Min.Y+bounds.Dy()*3/10,
	)
	core, count := redPixelBounds(frame, scanRegion, 150, 55)
	if count < killfeedMinHighlightPixels {
		return image.Rectangle{}, false
	}
	edgeRegion := core.Inset(-killfeedBorderSearchRadius).Intersect(bounds)
	edge, edgeCount := redPixelBounds(frame, edgeRegion, 120, 70)
	if edgeCount > 0 {
		core = core.Union(edge)
	}
	return core.Inset(-killfeedHighlightMargin).Intersect(bounds), true
}

// redPixelBounds returns the bounding box and count of pixels within region
// whose 8-bit color exceeds minRed and stays below maxGreenBlue on the other
// channels.
func redPixelBounds(frame image.Image, region image.Rectangle, minRed, maxGreenBlue uint32) (image.Rectangle, int) {
	found := image.Rectangle{}
	count := 0
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			if r>>8 > minRed && g>>8 < maxGreenBlue && b>>8 < maxGreenBlue {
				pixel := image.Rect(x, y, x+1, y+1)
				if count == 0 {
					found = pixel
				} else {
					found = found.Union(pixel)
				}
				count++
			}
		}
	}
	return found, count
}

// ffmpegFrameProbe extracts a single source frame as PNG via FFmpeg for
// killfeed crop measurement.
func ffmpegFrameProbe(ffmpegPath string) func(input string, atSeconds float64) (image.Image, error) {
	if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}
	return func(input string, atSeconds float64) (image.Image, error) {
		tmp, err := os.CreateTemp("", "zv-killfeed-*.png")
		if err != nil {
			return nil, fmt.Errorf("create killfeed frame file: %w", err)
		}
		tmpPath := tmp.Name()
		if err := tmp.Close(); err != nil {
			return nil, fmt.Errorf("close killfeed frame file: %w", err)
		}
		defer func() { _ = os.Remove(tmpPath) }()
		// #nosec G204 -- ffmpegPath and input are local pipeline configuration, not untrusted input.
		out, err := exec.Command(ffmpegPath,
			"-y", "-v", "error",
			"-ss", fmt.Sprintf("%.3f", atSeconds),
			"-i", input,
			"-frames:v", "1",
			tmpPath,
		).CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("extract frame from %s at %.3fs: %v: %s", input, atSeconds, err, out)
		}
		// #nosec G304 -- tmpPath was created by os.CreateTemp above.
		f, err := os.Open(tmpPath)
		if err != nil {
			return nil, fmt.Errorf("open killfeed frame: %w", err)
		}
		defer func() { _ = f.Close() }()
		frame, err := png.Decode(f)
		if err != nil {
			return nil, fmt.Errorf("decode killfeed frame: %w", err)
		}
		return frame, nil
	}
}
