package streamkillfeed

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

func TestEncodeEventRowPNGDoesNotIncludeAdjacentRow(t *testing.T) {
	frame := image.NewRGBA(image.Rect(0, 0, 80, 40))
	paintRect(frame, image.Rect(5, 5, 35, 18), color.RGBA{R: 0xff, A: 0xff})
	paintRect(frame, image.Rect(5, 20, 35, 33), color.RGBA{B: 0xff, A: 0xff})

	encoded, err := encodeEventRowPNG(frame, streamclips.NoticeRow{
		X: 5, Y: 5, Width: 30, Height: 13,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	wantWidth := (30*streamclips.KillfeedNoticeHeight + 13/2) / 13
	if got.Bounds() != image.Rect(0, 0, wantWidth, streamclips.KillfeedNoticeHeight) {
		t.Fatalf("crop bounds = %v, want normalized exact first row", got.Bounds())
	}
	for y := got.Bounds().Min.Y; y < got.Bounds().Max.Y; y++ {
		for x := got.Bounds().Min.X; x < got.Bounds().Max.X; x++ {
			_, _, blue, _ := got.At(x, y).RGBA()
			if blue != 0 {
				t.Fatalf("first event crop contains adjacent blue row at %d,%d", x, y)
			}
		}
	}
}

func TestAnalyzerExtractEventRowsUsesScanner1080GeometryAndExactPTS(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	sourcePath := filepath.Join(t.TempDir(), "killfeed-extract.mp4")
	generateSyntheticKillfeed(t, ffmpeg, sourcePath, 0)
	probe := streamclips.SourceProbe{
		Width: 1280, Height: 720, DurationSeconds: 2,
		FrameRate: "30000/1001", VideoTimeBase: "1/30000",
	}
	analyzer := Analyzer{FFmpegPath: ffmpeg}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	events, err := analyzer.Scan(
		ctx,
		sourcePath,
		probe,
		streamclips.CropRect{X: 0.7, Y: 0, Width: 0.3, Height: 0.2},
		streamclips.ClipRange{ID: "clip-extract", StartSeconds: 0.5, EndSeconds: 1.8},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || len(events[0].Rows) != 1 {
		t.Fatalf("events = %+v, want one event with one row", events)
	}
	row := events[0].Rows[0].SampleBounds
	// The source is 720 high, but durable bounds must address the shared
	// 1080-high detector/extractor grid (the source drawbox starts at y=50).
	if row.Y < 60 || row.Width < 350 {
		t.Fatalf("SampleBounds = %+v, want 1080-high scaled geometry", row)
	}
	rows, err := analyzer.ExtractEventRowPNGs(ctx, sourcePath, probe, events[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("extracted rows = %d, want 1", len(rows))
	}
	decoded, err := png.Decode(bytes.NewReader(rows[0]))
	if err != nil {
		t.Fatal(err)
	}
	wantWidth := (row.Width*streamclips.KillfeedNoticeHeight + row.Height/2) / row.Height
	if got := decoded.Bounds().Dx(); got != wantWidth {
		t.Fatalf("PNG width = %d, want proportional normalized width %d", got, wantWidth)
	}
	if got, want := decoded.Bounds().Dy(), streamclips.KillfeedNoticeHeight; got != want {
		t.Fatalf("PNG height = %d, want compositor stack height %d", got, want)
	}
}

func paintRect(frame *image.RGBA, rect image.Rectangle, c color.RGBA) {
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			frame.SetRGBA(x, y, c)
		}
	}
}
