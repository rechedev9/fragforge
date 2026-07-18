package streamkillfeed

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"math/bits"
	"sort"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

const (
	fingerprintWidth       = 64
	fingerprintHeight      = 16
	fingerprintWords       = fingerprintWidth * fingerprintHeight / 64
	fingerprintMinFeatures = 16
	fingerprintMaxDistance = 48
	fingerprintMinOverlap  = 65
)

// rowFingerprint is a normalized foreground mask. The common red highlight
// border is excluded so replacing a notice in the same screen slot is still a
// row birth.
type rowFingerprint struct {
	bits     [fingerprintWords]uint64
	features int
}

type observedRow struct {
	index       int
	bounds      streamclips.NoticeRow
	fingerprint rowFingerprint
}

func observeRows(frame image.Image, rows []streamclips.NoticeRow) []observedRow {
	if frame == nil || len(rows) == 0 {
		return nil
	}
	observed := make([]observedRow, 0, len(rows))
	for i, row := range rows {
		fingerprint := fingerprintRow(frame, row)
		if fingerprint.features < fingerprintMinFeatures {
			continue
		}
		observed = append(observed, observedRow{
			index:       i,
			bounds:      row,
			fingerprint: fingerprint,
		})
	}
	return observed
}

func fingerprintRow(frame image.Image, row streamclips.NoticeRow) rowFingerprint {
	if frame == nil {
		return rowFingerprint{}
	}
	rect := image.Rect(row.X, row.Y, row.X+row.Width, row.Y+row.Height).Intersect(frame.Bounds())
	if rect.Empty() {
		return rowFingerprint{}
	}

	var fingerprint rowFingerprint
	for outY := range fingerprintHeight {
		for outX := range fingerprintWidth {
			x := rect.Min.X + (2*outX+1)*rect.Dx()/(2*fingerprintWidth)
			y := rect.Min.Y + (2*outY+1)*rect.Dy()/(2*fingerprintHeight)
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
			bit := outY*fingerprintWidth + outX
			fingerprint.bits[bit/64] |= uint64(1) << (bit % 64)
			fingerprint.features++
		}
	}
	return fingerprint
}

func matchingFingerprint(a, b rowFingerprint) bool {
	distance, overlap, ok := fingerprintSimilarity(a, b)
	return ok && distance <= fingerprintMaxDistance && overlap >= fingerprintMinOverlap
}

func fingerprintSimilarity(a, b rowFingerprint) (distance int, overlap int, ok bool) {
	if a.features < fingerprintMinFeatures || b.features < fingerprintMinFeatures {
		return 0, 0, false
	}
	intersection := 0
	union := 0
	for i := range a.bits {
		distance += bits.OnesCount64(a.bits[i] ^ b.bits[i])
		intersection += bits.OnesCount64(a.bits[i] & b.bits[i])
		union += bits.OnesCount64(a.bits[i] | b.bits[i])
	}
	if union == 0 {
		return 0, 0, false
	}
	return distance, intersection * 100 / union, true
}

type rowMatchCandidate struct {
	left     int
	right    int
	distance int
	overlap  int
}

// matchRows finds a deterministic one-to-one match. Considering all valid
// pairs before claiming either side avoids row-order changes creating births.
func matchRows(left, right []observedRow) (leftMatched, rightMatched []bool) {
	leftMatched = make([]bool, len(left))
	rightMatched = make([]bool, len(right))
	candidates := make([]rowMatchCandidate, 0, len(left)*len(right))
	for i := range left {
		for j := range right {
			distance, overlap, ok := fingerprintSimilarity(left[i].fingerprint, right[j].fingerprint)
			if !ok || distance > fingerprintMaxDistance || overlap < fingerprintMinOverlap {
				continue
			}
			candidates = append(candidates, rowMatchCandidate{
				left: i, right: j, distance: distance, overlap: overlap,
			})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		if candidates[i].overlap != candidates[j].overlap {
			return candidates[i].overlap > candidates[j].overlap
		}
		if candidates[i].left != candidates[j].left {
			return candidates[i].left < candidates[j].left
		}
		return candidates[i].right < candidates[j].right
	})
	for _, candidate := range candidates {
		if leftMatched[candidate.left] || rightMatched[candidate.right] {
			continue
		}
		leftMatched[candidate.left] = true
		rightMatched[candidate.right] = true
	}
	return leftMatched, rightMatched
}

func bornRows(previous, current []observedRow) []observedRow {
	_, matched := matchRows(previous, current)
	born := make([]observedRow, 0, len(current))
	for i, row := range current {
		if !matched[i] {
			born = append(born, row)
		}
	}
	return born
}

func matchBornRows(want, current []observedRow) ([]int, bool) {
	indexes := make([]int, len(want))
	for i := range indexes {
		indexes[i] = -1
	}
	candidates := make([]rowMatchCandidate, 0, len(want)*len(current))
	for i := range want {
		for j := range current {
			distance, overlap, ok := fingerprintSimilarity(want[i].fingerprint, current[j].fingerprint)
			if ok && distance <= fingerprintMaxDistance && overlap >= fingerprintMinOverlap {
				candidates = append(candidates, rowMatchCandidate{i, j, distance, overlap})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance != candidates[j].distance {
			return candidates[i].distance < candidates[j].distance
		}
		if candidates[i].overlap != candidates[j].overlap {
			return candidates[i].overlap > candidates[j].overlap
		}
		if candidates[i].left != candidates[j].left {
			return candidates[i].left < candidates[j].left
		}
		return candidates[i].right < candidates[j].right
	})
	usedCurrent := make([]bool, len(current))
	for _, candidate := range candidates {
		if indexes[candidate.left] >= 0 || usedCurrent[candidate.right] {
			continue
		}
		indexes[candidate.left] = candidate.right
		usedCurrent[candidate.right] = true
	}
	for _, index := range indexes {
		if index < 0 {
			return nil, false
		}
	}
	return indexes, true
}

func fingerprintKey(fingerprint rowFingerprint) string {
	var encoded [fingerprintWords * 8]byte
	for i, word := range fingerprint.bits {
		binary.LittleEndian.PutUint64(encoded[i*8:], word)
	}
	sum := sha256.Sum256(encoded[:])
	return hex.EncodeToString(sum[:])
}
