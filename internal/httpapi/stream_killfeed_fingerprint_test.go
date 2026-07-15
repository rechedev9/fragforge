package httpapi

import (
	"image"
	"image/color"
	"testing"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

func TestKillfeedRowFingerprintDistinguishesNoticeContent(t *testing.T) {
	row := streamclips.NoticeRow{X: 0, Y: 0, Width: 320, Height: 40}
	left := image.NewRGBA(image.Rect(0, 0, row.Width, row.Height))
	right := image.NewRGBA(left.Bounds())
	white := color.RGBA{R: 230, G: 230, B: 230, A: 255}
	for y := 5; y < 35; y++ {
		for x := 20; x < 100; x++ {
			left.SetRGBA(x, y, white)
		}
		for x := 220; x < 300; x++ {
			right.SetRGBA(x, y, white)
		}
	}

	leftFingerprint := fingerprintKillfeedRow(left, row)
	if !matchingKillfeedFingerprint(leftFingerprint, fingerprintKillfeedRow(left, row)) {
		t.Fatal("matching fingerprint rejected identical notice content")
	}
	if matchingKillfeedFingerprint(leftFingerprint, fingerprintKillfeedRow(right, row)) {
		t.Fatal("matching fingerprint accepted different notice content in the same screen slot")
	}
	if matchingKillfeedFingerprint(leftFingerprint, killfeedRowFingerprint{}) {
		t.Fatal("matching fingerprint accepted a row without enough visual features")
	}
	sparseLeft := killfeedRowFingerprint{features: killfeedFingerprintMinFeatures}
	sparseRight := killfeedRowFingerprint{features: killfeedFingerprintMinFeatures}
	sparseLeft.bits[0] = 0xffff
	sparseRight.bits[1] = 0xffff
	if matchingKillfeedFingerprint(sparseLeft, sparseRight) {
		t.Fatal("matching fingerprint accepted disjoint sparse notice content")
	}
}
