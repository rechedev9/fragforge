package httpapi

import (
	"image"
	"math/bits"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

const (
	killfeedFingerprintWidth       = 64
	killfeedFingerprintHeight      = 16
	killfeedFingerprintWords       = killfeedFingerprintWidth * killfeedFingerprintHeight / 64
	killfeedFingerprintMinFeatures = 16
	killfeedFingerprintMaxDistance = 48
	killfeedFingerprintMinOverlap  = 65
)

// killfeedRowFingerprint is a compact mask of bright, non-red HUD content in
// one normalized notice row. It excludes the common red highlight border so a
// continuously occupied screen slot cannot make two different notices appear
// identical merely because their geometry is the same.
type killfeedRowFingerprint struct {
	bits     [killfeedFingerprintWords]uint64
	features int
}

func fingerprintKillfeedRows(frame image.Image, rows []streamclips.NoticeRow) []killfeedRowFingerprint {
	if frame == nil || len(rows) == 0 {
		return nil
	}
	fingerprints := make([]killfeedRowFingerprint, len(rows))
	for i, row := range rows {
		fingerprints[i] = fingerprintKillfeedRow(frame, row)
	}
	return fingerprints
}

func fingerprintKillfeedRow(frame image.Image, row streamclips.NoticeRow) killfeedRowFingerprint {
	if frame == nil {
		return killfeedRowFingerprint{}
	}
	rect := image.Rect(row.X, row.Y, row.X+row.Width, row.Y+row.Height).Intersect(frame.Bounds())
	if rect.Empty() {
		return killfeedRowFingerprint{}
	}

	var fingerprint killfeedRowFingerprint
	for outY := range killfeedFingerprintHeight {
		for outX := range killfeedFingerprintWidth {
			x := rect.Min.X + (2*outX+1)*rect.Dx()/(2*killfeedFingerprintWidth)
			y := rect.Min.Y + (2*outY+1)*rect.Dy()/(2*killfeedFingerprintHeight)
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
			bit := outY*killfeedFingerprintWidth + outX
			fingerprint.bits[bit/64] |= uint64(1) << (bit % 64)
			fingerprint.features++
		}
	}
	return fingerprint
}

func matchingKillfeedFingerprint(a, b killfeedRowFingerprint) bool {
	if a.features < killfeedFingerprintMinFeatures || b.features < killfeedFingerprintMinFeatures {
		return false
	}
	distance := 0
	intersection := 0
	union := 0
	for i := range a.bits {
		distance += bits.OnesCount64(a.bits[i] ^ b.bits[i])
		if distance > killfeedFingerprintMaxDistance {
			return false
		}
		intersection += bits.OnesCount64(a.bits[i] & b.bits[i])
		union += bits.OnesCount64(a.bits[i] | b.bits[i])
	}
	return union > 0 && intersection*100 >= killfeedFingerprintMinOverlap*union
}
