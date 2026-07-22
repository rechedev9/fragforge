package streamclips

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

var benchmarkNoticeRows []NoticeRow

func TestDetectNoticeRowsSeparatesStaggeredOverlappingNotices(t *testing.T) {
	top := image.Rect(1621, 73, 1909, 110)
	bottom := image.Rect(1469, 103, 1909, 143)
	tests := []struct {
		name  string
		touch bool
	}{
		{name: "overlapping rings"},
		{name: "touching rings", touch: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
			fillStreamKillfeedNotice(frame, top)
			fillStreamKillfeedNotice(frame, bottom)
			strokeStreamKillfeedNotice(frame, top)
			strokeStreamKillfeedNotice(frame, bottom)
			if tt.touch {
				red := color.RGBA{R: 200, G: 30, B: 30, A: 255}
				for x := top.Min.X + 1; x < top.Max.X-1; x++ {
					frame.Set(x, bottom.Min.Y+3, red)
				}
			}

			rows := DetectNoticeRows(frame, nil)
			if len(rows) != 2 {
				t.Fatalf("DetectNoticeRows returned %d rows, want 2: %+v", len(rows), rows)
			}
			assertNoticeRowNear(t, rows[0], top)
			assertNoticeRowNear(t, rows[1], bottom)
		})
	}
}

func TestDetectNoticeRowsSeparatesEqualWidthOverlappingNotices(t *testing.T) {
	top := image.Rect(1600, 73, 1909, 110)
	bottom := image.Rect(1600, 103, 1909, 143)
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	fillStreamKillfeedNotice(frame, top)
	fillStreamKillfeedNotice(frame, bottom)
	strokeStreamKillfeedNotice(frame, top)
	strokeStreamKillfeedNotice(frame, bottom)

	rows := DetectNoticeRows(frame, nil)
	if len(rows) != 2 {
		t.Fatalf("DetectNoticeRows returned %d equal-width rows, want 2: %+v", len(rows), rows)
	}
	assertNoticeRowNear(t, rows[0], top)
	assertNoticeRowNear(t, rows[1], bottom)
}

func TestDetectNoticeRowsDoesNotBridgeEqualWidthSeparatedNotices(t *testing.T) {
	top := image.Rect(1600, 60, 1909, 97)
	bottom := image.Rect(1600, 145, 1909, 185)
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	drawStreamKillfeedNotice(frame, top)
	drawStreamKillfeedNotice(frame, bottom)

	rows := DetectNoticeRows(frame, nil)
	if len(rows) != 2 {
		t.Fatalf("DetectNoticeRows returned %d separated rows, want 2 without a bridge: %+v", len(rows), rows)
	}
	assertNoticeRowNear(t, rows[0], top)
	assertNoticeRowNear(t, rows[1], bottom)
}

func TestDetectNoticeRowsBridgesCompressedLooseRedStrokes(t *testing.T) {
	top := image.Rect(1621, 73, 1909, 110)
	bottom := image.Rect(1469, 103, 1909, 143)
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	drawCompressedStreamKillfeedNotice(frame, top)
	drawCompressedStreamKillfeedNotice(frame, bottom)

	rows := DetectNoticeRows(frame, nil)
	if len(rows) != 2 {
		t.Fatalf("DetectNoticeRows returned %d compressed rows, want 2: %+v", len(rows), rows)
	}
	assertNoticeRowNear(t, rows[0], top)
	assertNoticeRowNear(t, rows[1], bottom)
}

func TestDetectNoticeRowsCombinesLooseAndStrictStyles(t *testing.T) {
	compressed := image.Rect(1621, 73, 1909, 110)
	saturated := image.Rect(1469, 140, 1909, 180)
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	drawCompressedStreamKillfeedNotice(frame, compressed)
	drawStreamKillfeedNotice(frame, saturated)

	rows := DetectNoticeRows(frame, nil)
	if len(rows) != 2 {
		t.Fatalf("DetectNoticeRows returned %d mixed-style rows, want 2: %+v", len(rows), rows)
	}
	assertNoticeRowNear(t, rows[0], compressed)
	assertNoticeRowNear(t, rows[1], saturated)
}

func TestDetectNoticeRowsRejectsSceneRedAndNoise(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	fillStreamSolidRed(frame, image.Rect(1250, 10, 1500, 300))
	dim := color.RGBA{R: 130, G: 45, B: 45, A: 255}
	for y := 160; y < 200; y++ {
		for x := 1600; x < 1700; x++ {
			frame.Set(x, y, dim)
		}
	}

	if rows := DetectNoticeRows(frame, nil); rows != nil {
		t.Fatalf("DetectNoticeRows returned scene-only rows %+v, want nil", rows)
	}
}

func TestDetectNoticeRowsEmptyFrame(t *testing.T) {
	if rows := DetectNoticeRows(image.NewRGBA(image.Rect(0, 0, 1920, 1080)), nil); rows != nil {
		t.Fatalf("DetectNoticeRows returned %+v on an empty frame, want nil", rows)
	}
}

func TestDetectNoticeRowsMatchesConcreteAndGenericImages(t *testing.T) {
	t.Parallel()

	source := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	drawStreamKillfeedNotice(source, image.Rect(1510, 70, 1810, 106))
	want := DetectNoticeRows(source, nil)
	if len(want) != 1 {
		t.Fatalf("RGBA fixture returned %d rows, want 1: %+v", len(want), want)
	}

	nrgba := image.NewNRGBA(source.Bounds())
	draw.Draw(nrgba, nrgba.Bounds(), source, source.Bounds().Min, draw.Src)
	images := []struct {
		name  string
		frame image.Image
	}{
		{name: "nrgba", frame: nrgba},
		{name: "generic", frame: struct{ image.Image }{source}},
	}
	for _, tt := range images {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectNoticeRows(tt.frame, nil)
			if len(got) != len(want) {
				t.Fatalf("DetectNoticeRows returned %d rows, want %d: %+v", len(got), len(want), got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("row %d = %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	}
}

func TestRedMasksPreserveNRGBAAlphaSemantics(t *testing.T) {
	t.Parallel()

	frame := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	for x, alpha := range []uint8{120, 121, 150, 151} {
		frame.SetNRGBA(x, 0, color.NRGBA{R: 255, A: alpha})
	}
	loose, strict := redMasks(frame, frame.Bounds())
	wantLoose := []bool{false, true, true, true}
	wantStrict := []bool{false, false, false, true}
	for i := range wantLoose {
		if loose[i] != wantLoose[i] || strict[i] != wantStrict[i] {
			t.Fatalf(
				"pixel %d masks = loose:%t strict:%t, want loose:%t strict:%t",
				i, loose[i], strict[i], wantLoose[i], wantStrict[i],
			)
		}
	}

	genericLoose, genericStrict := redMasks(struct{ image.Image }{frame}, frame.Bounds())
	for i := range wantLoose {
		if genericLoose[i] != loose[i] || genericStrict[i] != strict[i] {
			t.Fatalf("generic pixel %d masks differ from NRGBA fast path", i)
		}
	}
}

func TestDetectNoticeRowsBoundsSearchToHintPadding(t *testing.T) {
	hint := CropRect{X: 0.68, Y: 0.05, Width: 0.26, Height: 0.14}
	tests := []struct {
		name         string
		notice       image.Rectangle
		wantRows     int
		wantNotice   image.Rectangle
		wantInRegion image.Rectangle
	}{
		{
			name:       "notice inside padded hint",
			notice:     image.Rect(1320, 73, 1809, 110),
			wantRows:   1,
			wantNotice: image.Rect(1320, 73, 1809, 110),
		},
		{
			name:   "notice right of padded hint",
			notice: image.Rect(1830, 73, 1910, 110),
		},
		{
			name:   "hud band above padded hint",
			notice: image.Rect(1500, 10, 1800, 45),
		},
		{
			name:         "highlight touches padded hint edge",
			notice:       image.Rect(1320, 73, 1814, 110),
			wantRows:     1,
			wantNotice:   image.Rect(1320, 73, 1814, 110),
			wantInRegion: image.Rect(1297, 46, 1813, 214),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
			drawStreamKillfeedNotice(frame, tt.notice)

			rows := DetectNoticeRows(frame, &hint)
			if len(rows) != tt.wantRows {
				t.Fatalf("DetectNoticeRows returned %d rows, want %d: %+v", len(rows), tt.wantRows, rows)
			}
			if tt.wantRows == 1 {
				assertNoticeRowNear(t, rows[0], tt.wantNotice)
			}
			if !tt.wantInRegion.Empty() {
				row := image.Rect(rows[0].X, rows[0].Y, rows[0].X+rows[0].Width, rows[0].Y+rows[0].Height)
				if tt.wantInRegion.Intersect(row) != row {
					t.Fatalf("notice row bounds = %v, want fully inside padded hint region %v", row, tt.wantInRegion)
				}
			}
		})
	}
}

func TestDetectNoticeRowsRejectsHUDShapes(t *testing.T) {
	tests := []struct {
		name string
		draw func(*image.RGBA)
	}{
		{
			name: "near-square avatar",
			draw: func(frame *image.RGBA) {
				drawStreamKillfeedNotice(frame, image.Rect(1500, 60, 1545, 95))
			},
		},
		{
			name: "dense score bar",
			draw: func(frame *image.RGBA) {
				drawDenseStreamHUDRow(frame, image.Rect(1500, 60, 1620, 90))
			},
		},
		{
			name: "row taller than frame limit",
			draw: func(frame *image.RGBA) {
				drawStreamKillfeedNotice(frame, image.Rect(150, 30, 240, 56))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bounds := image.Rect(0, 0, 1920, 1080)
			if tt.name == "row taller than frame limit" {
				bounds = image.Rect(0, 0, 320, 240)
			}
			frame := image.NewRGBA(bounds)
			tt.draw(frame)

			if rows := DetectNoticeRows(frame, nil); len(rows) != 0 {
				t.Fatalf("DetectNoticeRows returned HUD rows %+v, want none", rows)
			}
		})
	}
}

func TestDetectNoticeRowsReturnsOnlyTightNoticeBounds(t *testing.T) {
	top := image.Rect(1510, 70, 1810, 106)
	bottom := image.Rect(1450, 120, 1810, 158)
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	drawStreamKillfeedNotice(frame, top)
	drawStreamKillfeedNotice(frame, bottom)
	drawDenseStreamHUDRow(frame, image.Rect(1500, 180, 1620, 210))
	loose := color.RGBA{R: 130, G: 45, B: 45, A: 255}
	for y := top.Min.Y; y < top.Max.Y; y++ {
		frame.Set(top.Min.X-1, y, loose)
	}

	rows := DetectNoticeRows(frame, nil)
	if len(rows) != 2 {
		t.Fatalf("DetectNoticeRows returned %d rows, want 2 notices only: %+v", len(rows), rows)
	}
	assertNoticeRowWithinOnePixel(t, rows[0], top)
	assertNoticeRowWithinOnePixel(t, rows[1], bottom)
}

func BenchmarkDetectNoticeRows1080pEmpty(b *testing.B) {
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkNoticeRows = DetectNoticeRows(frame, nil)
	}
}

func BenchmarkDetectNoticeRows1080pTwoRows(b *testing.B) {
	frame := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	drawStreamKillfeedNotice(frame, image.Rect(1510, 70, 1810, 106))
	drawCompressedStreamKillfeedNotice(frame, image.Rect(1450, 120, 1810, 158))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkNoticeRows = DetectNoticeRows(frame, nil)
	}
}

func assertNoticeRowNear(t *testing.T, got NoticeRow, want image.Rectangle) {
	t.Helper()
	if absInt(got.X-want.Min.X) > 3 || absInt(got.Y-want.Min.Y) > 3 ||
		absInt(got.Width-want.Dx()) > 3 || absInt(got.Height-want.Dy()) > 3 {
		t.Fatalf("notice row = %+v, want within 3px of %v", got, want)
	}
}

func assertNoticeRowWithinOnePixel(t *testing.T, got NoticeRow, want image.Rectangle) {
	t.Helper()
	gotRect := image.Rect(got.X, got.Y, got.X+got.Width, got.Y+got.Height)
	if absInt(gotRect.Min.X-want.Min.X) > 1 || absInt(gotRect.Min.Y-want.Min.Y) > 1 ||
		absInt(gotRect.Max.X-want.Max.X) > 1 || absInt(gotRect.Max.Y-want.Max.Y) > 1 {
		t.Fatalf("notice row bounds = %v, want every edge within 1px of %v", gotRect, want)
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// drawStreamKillfeedNotice paints the same CS2-style highlighted notice used
// by the editor detector tests: a 2px saturated-red border ring around a dimmer
// anti-aliased fill.
func drawStreamKillfeedNotice(frame *image.RGBA, notice image.Rectangle) {
	fillStreamKillfeedNotice(frame, notice)
	strokeStreamKillfeedNotice(frame, notice)
}

func fillStreamKillfeedNotice(frame *image.RGBA, notice image.Rectangle) {
	dim := color.RGBA{R: 130, G: 45, B: 45, A: 255}
	for y := notice.Min.Y; y < notice.Max.Y; y++ {
		for x := notice.Min.X; x < notice.Max.X; x++ {
			frame.Set(x, y, dim)
		}
	}
}

func strokeStreamKillfeedNotice(frame *image.RGBA, notice image.Rectangle) {
	red := color.RGBA{R: 200, G: 30, B: 30, A: 255}
	inner := notice.Inset(1)
	for x := inner.Min.X; x < inner.Max.X; x++ {
		for d := range 2 {
			frame.Set(x, inner.Min.Y+d, red)
			frame.Set(x, inner.Max.Y-1-d, red)
		}
	}
	for y := inner.Min.Y; y < inner.Max.Y; y++ {
		for d := range 2 {
			frame.Set(inner.Min.X+d, y, red)
			frame.Set(inner.Max.X-1-d, y, red)
		}
	}
}

func drawCompressedStreamKillfeedNotice(frame *image.RGBA, notice image.Rectangle) {
	strong := color.RGBA{R: 200, G: 30, B: 30, A: 255}
	loose := color.RGBA{R: 135, G: 60, B: 60, A: 255}
	inner := notice.Inset(1)
	for x := inner.Min.X; x < inner.Max.X; x++ {
		offset := x - inner.Min.X
		if offset%16 >= 12 {
			continue
		}
		pixel := loose
		if offset%3 == 1 {
			pixel = strong
		}
		for d := range 2 {
			frame.Set(x, inner.Min.Y+d, pixel)
			frame.Set(x, inner.Max.Y-1-d, pixel)
		}
	}
	for y := inner.Min.Y; y < inner.Max.Y; y++ {
		for d := range 2 {
			frame.Set(inner.Min.X+d, y, strong)
			frame.Set(inner.Max.X-1-d, y, strong)
		}
	}
}

func fillStreamSolidRed(frame *image.RGBA, block image.Rectangle) {
	wall := color.RGBA{R: 200, G: 30, B: 30, A: 255}
	for y := block.Min.Y; y < block.Max.Y; y++ {
		for x := block.Min.X; x < block.Max.X; x++ {
			frame.Set(x, y, wall)
		}
	}
}

func drawDenseStreamHUDRow(frame *image.RGBA, row image.Rectangle) {
	drawStreamKillfeedNotice(frame, row)
	red := color.RGBA{R: 200, G: 30, B: 30, A: 255}
	for y := row.Min.Y + 4; y < row.Max.Y-4; y++ {
		for x := row.Min.X + 4; x < row.Min.X+34; x++ {
			frame.Set(x, y, red)
		}
		for x := row.Min.X + 44; x < row.Min.X+74; x++ {
			frame.Set(x, y, red)
		}
	}
}
