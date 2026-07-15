package streamclips

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"path"
	"sync"
	"testing"
)

var benchmarkNotice image.Image

func baseKill() KillfeedKill {
	return KillfeedKill{
		AttackerSide: "T",
		AttackerName: "player1",
		VictimSide:   "CT",
		VictimName:   "player2",
		Weapon:       "ak47",
	}
}

func TestRenderNoticeUnknownWeaponErrors(t *testing.T) {
	kill := baseKill()
	kill.Weapon = "definitely_not_a_weapon"
	if _, err := RenderNotice(kill); err == nil {
		t.Fatalf("expected error for unknown weapon, got nil")
	}
}

func TestRenderNoticeEmptyNamesError(t *testing.T) {
	t.Run("attacker", func(t *testing.T) {
		kill := baseKill()
		kill.AttackerName = ""
		if _, err := RenderNotice(kill); err == nil {
			t.Fatalf("expected error for empty attacker name, got nil")
		}
	})
	t.Run("victim", func(t *testing.T) {
		kill := baseKill()
		kill.VictimName = ""
		if _, err := RenderNotice(kill); err == nil {
			t.Fatalf("expected error for empty victim name, got nil")
		}
	})
}

func TestRenderNoticeHeightConstant(t *testing.T) {
	img, err := RenderNotice(baseKill())
	if err != nil {
		t.Fatalf("RenderNotice: %v", err)
	}
	if got := img.Bounds().Dy(); got != KillfeedNoticeHeight {
		t.Fatalf("notice height: got %d, want %d", got, KillfeedNoticeHeight)
	}
}

func TestRenderNoticeWidthGrowsWithAttackerName(t *testing.T) {
	short := baseKill()
	short.AttackerName = "ab"
	long := baseKill()
	long.AttackerName = "abcdefghijklmnop"

	shortImg, err := RenderNotice(short)
	if err != nil {
		t.Fatalf("RenderNotice short: %v", err)
	}
	longImg, err := RenderNotice(long)
	if err != nil {
		t.Fatalf("RenderNotice long: %v", err)
	}
	if got, prev := longImg.Bounds().Dx(), shortImg.Bounds().Dx(); got <= prev {
		t.Fatalf("width did not grow with longer attacker name: long=%d short=%d", got, prev)
	}
}

func TestRenderNoticeWidthGrowsWithAssister(t *testing.T) {
	without := baseKill()
	with := baseKill()
	with.AssisterSide = "T"
	with.AssisterName = "helper"

	withoutImg, err := RenderNotice(without)
	if err != nil {
		t.Fatalf("RenderNotice without assister: %v", err)
	}
	withImg, err := RenderNotice(with)
	if err != nil {
		t.Fatalf("RenderNotice with assister: %v", err)
	}
	if got, prev := withImg.Bounds().Dx(), withoutImg.Bounds().Dx(); got <= prev {
		t.Fatalf("width did not grow with assister: with=%d without=%d", got, prev)
	}
}

func TestRenderNoticeBorderPixel(t *testing.T) {
	img, err := RenderNotice(baseKill())
	if err != nil {
		t.Fatalf("RenderNotice: %v", err)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	// #B50000 opaque, compared in 8-bit space.
	if r>>8 != 0xB5 || g>>8 != 0x00 || b>>8 != 0x00 || a>>8 != 0xFF {
		t.Fatalf("border pixel at (0,0): got rgba(%d,%d,%d,%d), want #B50000 opaque", r>>8, g>>8, b>>8, a>>8)
	}
}

func TestRenderNoticeInteriorPlatePixel(t *testing.T) {
	img, err := RenderNotice(baseKill())
	if err != nil {
		t.Fatalf("RenderNotice: %v", err)
	}
	// A near-corner interior pixel is inside the 2px border but clear of the
	// vertically centered, 10px-padded content: it should be the plate fill.
	r, g, b, a := img.At(4, 4).RGBA()
	if r>>8 != 0 || g>>8 != 0 || b>>8 != 0 {
		t.Fatalf("plate pixel (4,4): got rgb(%d,%d,%d), want black", r>>8, g>>8, b>>8)
	}
	if a8 := a >> 8; a8 == 0 || a8 == 0xFF {
		t.Fatalf("plate pixel (4,4) alpha: got %d, want translucent (0<a<255)", a8)
	}
}

func TestRenderNoticeAllWeaponsRender(t *testing.T) {
	for _, key := range WeaponKeys() {
		kill := baseKill()
		kill.Weapon = key
		if _, err := RenderNotice(kill); err != nil {
			t.Errorf("weapon %q failed to render: %v", key, err)
		}
	}
}

func TestWeaponKeysExcludesFlashbangAssist(t *testing.T) {
	for _, key := range WeaponKeys() {
		if key == "flashbang_assist" {
			t.Fatalf("WeaponKeys must exclude flashbang_assist")
		}
	}
	if ValidWeaponKey("flashbang_assist") {
		t.Fatalf("ValidWeaponKey must reject flashbang_assist")
	}
	if !ValidWeaponKey("ak47") {
		t.Fatalf("ValidWeaponKey must accept ak47")
	}
}

func TestEncodeNoticePNG(t *testing.T) {
	var buf bytes.Buffer
	if err := EncodeNoticePNG(baseKill(), &buf); err != nil {
		t.Fatalf("EncodeNoticePNG: %v", err)
	}
	img, err := png.Decode(&buf)
	if err != nil {
		t.Fatalf("decode PNG: %v", err)
	}
	if got := img.Bounds().Dy(); got != KillfeedNoticeHeight {
		t.Fatalf("decoded height: got %d, want %d", got, KillfeedNoticeHeight)
	}
	if img.Bounds().Dx() <= 0 {
		t.Fatalf("decoded width must be positive, got %d", img.Bounds().Dx())
	}
}

func TestLoadIconConcurrentCallersShareCachedImage(t *testing.T) {
	const callers = 32
	start := make(chan struct{})
	results := make(chan image.Image, callers)
	errors := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			img, err := loadIcon(path.Join(weaponsDir, "ak47.png"), 31)
			results <- img
			errors <- err
		}()
	}
	close(start)

	var first image.Image
	for range callers {
		if err := <-errors; err != nil {
			t.Fatalf("loadIcon: %v", err)
		}
		img := <-results
		if first == nil {
			first = img
			continue
		}
		if img != first {
			t.Fatal("concurrent callers received different cached images")
		}
	}
}

func TestRenderNoticeConcurrentCallersUseIndependentFontFaces(t *testing.T) {
	const (
		callers    = 32
		iterations = 20
	)
	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for caller := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			kill := baseKill()
			kill.AttackerName = fmt.Sprintf("player%d", caller)
			for range iterations {
				if _, err := RenderNotice(kill); err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("RenderNotice: %v", err)
	}
}

func BenchmarkRenderNotice(b *testing.B) {
	kill := benchmarkKillfeedKill()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		img, err := RenderNotice(kill)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkNotice = img
	}
}

func BenchmarkEncodeNoticePNG(b *testing.B) {
	kill := benchmarkKillfeedKill()
	var buf bytes.Buffer
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		if err := EncodeNoticePNG(kill, &buf); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkKillfeedKill() KillfeedKill {
	return KillfeedKill{
		AttackerSide: "T",
		AttackerName: "MARTINEZSA",
		AssisterSide: "CT",
		AssisterName: "teammate",
		VictimSide:   "CT",
		VictimName:   "opponent",
		Weapon:       "ak47",
		Blind:        true,
		FlashAssist:  true,
		Noscope:      true,
		Smoke:        true,
		Wallbang:     true,
		InAir:        true,
		Headshot:     true,
	}
}
