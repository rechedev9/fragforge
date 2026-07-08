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
	// killfeedMaxHighlightHeightDiv caps a notice ring's height at frame
	// height / 12: a kill notice bar is ~37px tall at 1080p, so /12 (90px)
	// leaves headroom for higher resolutions while rejecting tall scene
	// geometry like a red wall or container.
	killfeedMaxHighlightHeightDiv = 12
	// killfeedMinHighlightAspect is the minimum width/height ratio of a notice
	// ring: kill notices are wide and short, so a qualifying component must be
	// at least twice as wide as it is tall.
	killfeedMinHighlightAspect = 2
	// killfeedMaxHighlightFill caps the fraction of a component's bounding box
	// its pixels may fill: a 2px border ring fills ~13% of its bbox, while
	// solid scene red fills ~100%, so anything over half is rejected as a blob.
	killfeedMaxHighlightFill = 0.5
)

// refineKillfeedEffects replaces the static killfeed crop defaults with a
// per-kill measurement: it samples the source frame just after each kill,
// finds the red highlight box CS2 draws around the recording player's own
// kill notice, and crops exactly that entry. Probe errors keep the static
// crop and are reported as warnings. When the probe runs but explicitly finds
// no highlight, generated ("edit-request") overlays are dropped instead —
// overlaying the static region would paint arbitrary scene pixels — while
// script-authored effects keep the crop the author asked for.
func refineKillfeedEffects(short *ShortEdit, probe func(input string, atSeconds float64) (image.Image, error)) []string {
	if probe == nil {
		return nil
	}
	var warnings []string
	kept := short.Effects[:0]
	for i := range short.Effects {
		effect := &short.Effects[i]
		if effect.Type != EffectKillfeed {
			kept = append(kept, *effect)
			continue
		}
		input, sampleAt := killfeedSampleSource(short, *effect)
		if input == "" {
			warnings = append(warnings, fmt.Sprintf("killfeed crop at %.2fs: no source input to probe; keeping default crop", effect.StartSeconds))
			kept = append(kept, *effect)
			continue
		}
		frame, err := probe(input, sampleAt)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("killfeed crop at %.2fs: %v; keeping default crop", effect.StartSeconds, err))
			kept = append(kept, *effect)
			continue
		}
		rect, ok := detectKillfeedHighlight(frame)
		if !ok {
			if effect.Source == "edit-request" {
				warnings = append(warnings, fmt.Sprintf("killfeed crop at %.2fs: no highlighted kill notice detected in %s; dropping overlay", effect.StartSeconds, input))
				continue
			}
			warnings = append(warnings, fmt.Sprintf("killfeed crop at %.2fs: no highlighted kill notice detected in %s; keeping default crop", effect.StartSeconds, input))
			kept = append(kept, *effect)
			continue
		}
		effect.CropX = rect.Min.X
		effect.CropY = rect.Min.Y
		effect.CropWidth = rect.Dx()
		effect.CropHeight = rect.Dy()
		effect.Width = int(float64(rect.Dx())*killfeedOverlayScale + 0.5)
		kept = append(kept, *effect)
	}
	short.Effects = kept
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
// player's kill notices in the top-right region. The first pass is
// shape-aware: it groups strict saturated-red pixels into connected
// components and keeps only those shaped like a notice highlight ring - wide,
// short, and mostly hollow. This rejects red scene geometry (a wall or
// container) that passes the same color threshold but forms a tall, solid
// blob, which previously got unioned into the crop. A second, looser pass
// within a few pixels of the qualifying rings picks up the border's
// anti-aliased edge. Returns the padded bounding box, or ok=false when no
// component looks like a notice.
func detectKillfeedHighlight(frame image.Image) (image.Rectangle, bool) {
	bounds := frame.Bounds()
	scanRegion := image.Rect(
		bounds.Min.X+bounds.Dx()*3/5,
		bounds.Min.Y,
		bounds.Max.X,
		bounds.Min.Y+bounds.Dy()*3/10,
	)
	maxHeight := bounds.Dy() / killfeedMaxHighlightHeightDiv
	core := image.Rectangle{}
	qualified := 0
	for _, comp := range redComponents(frame, scanRegion, 150, 55) {
		if isNoticeRing(comp, maxHeight) {
			if qualified == 0 {
				core = comp.bounds
			} else {
				core = core.Union(comp.bounds)
			}
			qualified++
		}
	}
	if qualified == 0 {
		return image.Rectangle{}, false
	}
	edgeRegion := core.Inset(-killfeedBorderSearchRadius).Intersect(bounds)
	edge, edgeCount := redPixelBounds(frame, edgeRegion, 120, 70)
	if edgeCount > 0 {
		core = core.Union(edge)
	}
	return core.Inset(-killfeedHighlightMargin).Intersect(bounds), true
}

// redComponent is a connected group of strict-red pixels: its bounding box and
// pixel count, used to tell a thin notice ring from a solid scene blob.
type redComponent struct {
	bounds image.Rectangle
	count  int
}

// isNoticeRing reports whether a red component is shaped like a CS2 kill-notice
// highlight border rather than solid scene geometry: enough pixels to clear
// noise, short (a notice bar), wide (notices are wide and short), and mostly
// hollow (a 2px ring barely fills its bounding box, a solid red wall fills it
// completely).
func isNoticeRing(comp redComponent, maxHeight int) bool {
	if comp.count < killfeedMinHighlightPixels {
		return false
	}
	w, h := comp.bounds.Dx(), comp.bounds.Dy()
	if h == 0 || w == 0 || h > maxHeight {
		return false
	}
	if w < killfeedMinHighlightAspect*h {
		return false
	}
	fill := float64(comp.count) / float64(w*h)
	return fill <= killfeedMaxHighlightFill
}

// redComponents groups the strict-red pixels of region (same threshold as
// redPixelBounds) into 8-connected components via BFS over a bool grid indexed
// relative to region. Region is small (~768x324 at 1080p), so a dense grid is
// cheap and simple.
func redComponents(frame image.Image, region image.Rectangle, minRed, maxGreenBlue uint32) []redComponent {
	w, h := region.Dx(), region.Dy()
	if w <= 0 || h <= 0 {
		return nil
	}
	red := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := frame.At(region.Min.X+x, region.Min.Y+y).RGBA()
			if r>>8 > minRed && g>>8 < maxGreenBlue && b>>8 < maxGreenBlue {
				red[y*w+x] = true
			}
		}
	}
	visited := make([]bool, w*h)
	var comps []redComponent
	queue := make([]int, 0, 64)
	for start := 0; start < w*h; start++ {
		if !red[start] || visited[start] {
			continue
		}
		visited[start] = true
		queue = queue[:0]
		queue = append(queue, start)
		sx, sy := start%w, start/w
		minX, minY, maxX, maxY := sx, sy, sx, sy
		count := 0
		for len(queue) > 0 {
			idx := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			count++
			cx, cy := idx%w, idx/w
			if cx < minX {
				minX = cx
			}
			if cx > maxX {
				maxX = cx
			}
			if cy < minY {
				minY = cy
			}
			if cy > maxY {
				maxY = cy
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					nx, ny := cx+dx, cy+dy
					if nx < 0 || nx >= w || ny < 0 || ny >= h {
						continue
					}
					nidx := ny*w + nx
					if red[nidx] && !visited[nidx] {
						visited[nidx] = true
						queue = append(queue, nidx)
					}
				}
			}
		}
		comps = append(comps, redComponent{
			bounds: image.Rect(
				region.Min.X+minX, region.Min.Y+minY,
				region.Min.X+maxX+1, region.Min.Y+maxY+1,
			),
			count: count,
		})
	}
	return comps
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
