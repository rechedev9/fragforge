package editor

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
)

// killfeedTestFrame draws a CS2-style highlighted kill notice in the
// top-right quadrant of a 1920x1080 frame: a saturated red border with a 1px
// dimmer anti-aliased ring just outside it, the way the game renders it.
func killfeedTestFrame(t *testing.T, notice image.Rectangle) image.Image {
	t.Helper()
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	dim := color.RGBA{R: 130, G: 45, B: 45, A: 255}
	for y := notice.Min.Y; y < notice.Max.Y; y++ {
		for x := notice.Min.X; x < notice.Max.X; x++ {
			frame.Set(x, y, dim)
		}
	}
	red := color.RGBA{R: 200, G: 30, B: 30, A: 255}
	inner := notice.Inset(1)
	for x := inner.Min.X; x < inner.Max.X; x++ {
		for d := 0; d < 2; d++ {
			frame.Set(x, inner.Min.Y+d, red)
			frame.Set(x, inner.Max.Y-1-d, red)
		}
	}
	for y := inner.Min.Y; y < inner.Max.Y; y++ {
		for d := 0; d < 2; d++ {
			frame.Set(inner.Min.X+d, y, red)
			frame.Set(inner.Max.X-1-d, y, red)
		}
	}
	return frame
}

func TestDetectKillfeedHighlight(t *testing.T) {
	notice := image.Rect(1700, 115, 1910, 152)
	rect, ok := detectKillfeedHighlight(killfeedTestFrame(t, notice))
	if !ok {
		t.Fatal("detectKillfeedHighlight ok = false, want true")
	}
	if rect.Min.X > notice.Min.X || rect.Min.Y > notice.Min.Y || rect.Max.X < notice.Max.X || rect.Max.Y < notice.Max.Y {
		t.Fatalf("rect = %v, want it to cover the full anti-aliased notice %v", rect, notice)
	}
	if rect.Min.X < notice.Min.X-2 || rect.Min.Y < notice.Min.Y-2 || rect.Max.X > notice.Max.X+2 || rect.Max.Y > notice.Max.Y+2 {
		t.Fatalf("rect = %v, want at most %dpx beyond notice %v", rect, killfeedHighlightMargin, notice)
	}

	if _, ok := detectKillfeedHighlight(image.NewRGBA(image.Rect(0, 0, 1920, 1080))); ok {
		t.Fatal("detectKillfeedHighlight on empty frame ok = true, want false")
	}
}

func TestDetectKillfeedHighlightIgnoresDistantDimRed(t *testing.T) {
	notice := image.Rect(1700, 70, 1910, 106)
	frame := killfeedTestFrame(t, notice).(*image.RGBA)
	// dim red scene noise (an explosion glow) far below the notice must not
	// stretch the detected box; only the local anti-aliased ring counts
	dim := color.RGBA{R: 130, G: 45, B: 45, A: 255}
	for y := 160; y < 200; y++ {
		for x := 1600; x < 1700; x++ {
			frame.Set(x, y, dim)
		}
	}
	rect, ok := detectKillfeedHighlight(frame)
	if !ok {
		t.Fatal("detectKillfeedHighlight ok = false, want true")
	}
	if rect.Max.Y > notice.Max.Y+2 {
		t.Fatalf("rect = %v, want it to ignore dim red noise below notice %v", rect, notice)
	}
}

func TestRefineKillfeedEffectsMeasuresCropPerKill(t *testing.T) {
	notice := image.Rect(1690, 196, 1910, 232)
	var gotInput string
	var gotAt float64
	probe := func(input string, atSeconds float64) (image.Image, error) {
		gotInput = input
		gotAt = atSeconds
		return killfeedTestFrame(t, notice), nil
	}

	short := ShortEdit{
		DurationSeconds: 12,
		Effects: []Effect{
			{
				Type:         EffectKillfeed,
				StartSeconds: 9.5,
				EndSeconds:   12,
				AtSeconds:    9.55,
				Width:        430,
				CropX:        1558,
				CropY:        64,
				CropWidth:    360,
				CropHeight:   110,
			},
		},
		Parts: []ShortPart{
			{SegmentID: "seg-001", Input: "seg-001.mp4", DurationSeconds: 6, TimelineStartSeconds: 0},
			{SegmentID: "seg-002", Input: "seg-002.mp4", DurationSeconds: 6, TimelineStartSeconds: 6},
		},
	}

	warnings := refineKillfeedEffects(&short, probe)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if gotInput != "seg-002.mp4" {
		t.Fatalf("probe input = %q, want seg-002.mp4", gotInput)
	}
	if want := 9.55 - 6 + killfeedSampleDelaySeconds; math.Abs(gotAt-want) > 1e-9 {
		t.Fatalf("probe at = %.3f, want %.3f", gotAt, want)
	}
	effect := short.Effects[0]
	crop := image.Rect(effect.CropX, effect.CropY, effect.CropX+effect.CropWidth, effect.CropY+effect.CropHeight)
	if crop.Min.X > notice.Min.X || crop.Min.Y > notice.Min.Y || crop.Max.X < notice.Max.X || crop.Max.Y < notice.Max.Y {
		t.Fatalf("crop = %v, want it to cover notice %v", crop, notice)
	}
	if effect.CropHeight > notice.Dy()+16 {
		t.Fatalf("crop height = %d, want tight fit around %d", effect.CropHeight, notice.Dy())
	}
	wantWidth := int(float64(effect.CropWidth)*killfeedOverlayScale + 0.5)
	if effect.Width != wantWidth {
		t.Fatalf("overlay width = %d, want %d (crop width scaled)", effect.Width, wantWidth)
	}
}

func TestRefineKillfeedEffectsKeepsDefaultsOnFailure(t *testing.T) {
	tests := []struct {
		name  string
		probe func(input string, atSeconds float64) (image.Image, error)
		want  string
	}{
		{
			name: "probe error",
			probe: func(string, float64) (image.Image, error) {
				return nil, fmt.Errorf("boom")
			},
			want: "boom",
		},
		{
			name: "no highlight detected",
			probe: func(string, float64) (image.Image, error) {
				return image.NewRGBA(image.Rect(0, 0, 1920, 1080)), nil
			},
			want: "no highlighted kill notice",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			short := ShortEdit{
				DurationSeconds: 12,
				Effects: []Effect{
					{
						Type:         EffectKillfeed,
						StartSeconds: 1,
						EndSeconds:   4,
						AtSeconds:    1.05,
						Width:        430,
						CropX:        1558,
						CropY:        64,
						CropWidth:    360,
						CropHeight:   110,
					},
				},
				Parts: []ShortPart{
					{SegmentID: "seg-001", Input: "seg-001.mp4", DurationSeconds: 6, TimelineStartSeconds: 0},
				},
			}

			warnings := refineKillfeedEffects(&short, tt.probe)
			if len(warnings) != 1 || !strings.Contains(warnings[0], tt.want) {
				t.Fatalf("warnings = %v, want one containing %q", warnings, tt.want)
			}
			effect := short.Effects[0]
			if effect.CropX != 1558 || effect.CropY != 64 || effect.CropWidth != 360 || effect.CropHeight != 110 || effect.Width != 430 {
				t.Fatalf("crop changed on failure: %#v", effect)
			}
		})
	}
}

func TestRefineKillfeedEffectsUsesShortInputWithoutParts(t *testing.T) {
	notice := image.Rect(1700, 70, 1910, 106)
	var gotInput string
	var gotAt float64
	probe := func(input string, atSeconds float64) (image.Image, error) {
		gotInput = input
		gotAt = atSeconds
		return killfeedTestFrame(t, notice), nil
	}

	short := ShortEdit{
		Input:           "seg-001.mp4",
		DurationSeconds: 6,
		Effects: []Effect{
			{
				Type:         EffectKillfeed,
				StartSeconds: 2,
				EndSeconds:   5,
				AtSeconds:    2.05,
				Width:        430,
				CropWidth:    360,
				CropHeight:   110,
				CropX:        1558,
				CropY:        64,
			},
		},
	}

	if warnings := refineKillfeedEffects(&short, probe); len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}
	if gotInput != "seg-001.mp4" {
		t.Fatalf("probe input = %q, want seg-001.mp4", gotInput)
	}
	if want := 2.05 + killfeedSampleDelaySeconds; gotAt != want {
		t.Fatalf("probe at = %.3f, want %.3f", gotAt, want)
	}
}
