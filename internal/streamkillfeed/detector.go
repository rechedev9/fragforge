package streamkillfeed

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

type frameObservation struct {
	pts      int64
	timeBase TimeBase
	seconds  float64
	rows     []observedRow
}

func detectFrameEvents(frames []frameObservation, clip streamclips.ClipRange) ([]Event, error) {
	if len(frames) == 0 {
		return []Event{}, nil
	}
	for i := range frames {
		if err := frames[i].timeBase.Validate(); err != nil {
			return nil, fmt.Errorf("frame %d: %w", i, err)
		}
		if i == 0 {
			continue
		}
		if !equivalentTimeBase(frames[0].timeBase, frames[i].timeBase) {
			return nil, fmt.Errorf(
				"frame %d time base changed from %s to %s",
				i, frames[0].timeBase, frames[i].timeBase,
			)
		}
		if frames[i].pts <= frames[i-1].pts {
			return nil, fmt.Errorf(
				"frame PTS must increase strictly: frame %d has %d after %d",
				i, frames[i].pts, frames[i-1].pts,
			)
		}
	}

	events := make([]Event, 0)
	for i := range frames {
		frame := frames[i]
		if frame.seconds < clip.StartSeconds || frame.seconds >= clip.EndSeconds {
			continue
		}

		var previousRows []observedRow
		onsetStartPTS := frame.pts
		hasPrevious := i > 0
		// A refinement window may start in the middle of a clip while old
		// notices are already visible. Its first native frame is baseline, not
		// evidence of a birth. Only the actual clip/source boundary can be
		// unresolved for lack of a preceding frame.
		if !hasPrevious && frame.seconds > clip.StartSeconds+1e-9 {
			continue
		}
		if hasPrevious {
			previousRows = frames[i-1].rows
			onsetStartPTS = frames[i-1].pts
		}
		born := bornRows(previousRows, frame.rows)
		if len(born) == 0 {
			continue
		}

		mode := ModeAlignedFrame
		switch {
		case !hasPrevious:
			mode = ModeUnresolved
		case len(born) > 1:
			mode = ModeBurst
		}
		sampleFrame, sampleIndexes := chooseSampleFrame(frames, i, born, clip.EndSeconds)
		rows := make([]RowEvidence, len(born))
		for rowIndex := range born {
			sampleRow := sampleFrame.rows[sampleIndexes[rowIndex]]
			rows[rowIndex] = RowEvidence{
				OnsetRowIndex:  born[rowIndex].index,
				SampleRowIndex: sampleRow.index,
				Fingerprint:    fingerprintKey(born[rowIndex].fingerprint),
				OnsetBounds:    born[rowIndex].bounds,
				SampleBounds:   sampleRow.bounds,
			}
		}
		event := Event{
			EventID:       stableEventID(clip.ID, frame.pts, frame.timeBase, born),
			SourcePTS:     frame.pts,
			TimeBase:      frame.timeBase,
			CueSeconds:    frame.seconds,
			OnsetStartPTS: onsetStartPTS,
			OnsetEndPTS:   frame.pts,
			SamplePTS:     sampleFrame.pts,
			SampleSeconds: sampleFrame.seconds,
			Mode:          mode,
			Rows:          rows,
		}
		if err := event.Validate(); err != nil {
			return nil, fmt.Errorf("event at PTS %d: %w", frame.pts, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func chooseSampleFrame(
	frames []frameObservation,
	onsetIndex int,
	born []observedRow,
	sampleBefore float64,
) (frameObservation, []int) {
	onset := frames[onsetIndex]
	bestFrame := onset
	bestIndexes, _ := matchBornRows(born, onset.rows)
	target := onset.seconds + SampleDelaySeconds
	for i := onsetIndex; i < len(frames); i++ {
		if frames[i].seconds >= sampleBefore {
			break
		}
		indexes, ok := matchBornRows(born, frames[i].rows)
		if !ok {
			continue
		}
		bestFrame = frames[i]
		bestIndexes = indexes
		if frames[i].seconds >= target {
			return bestFrame, bestIndexes
		}
	}
	return bestFrame, bestIndexes
}

func stableEventID(clipID string, pts int64, timeBase TimeBase, rows []observedRow) string {
	keys := make([]string, len(rows))
	for i := range rows {
		keys[i] = fingerprintKey(rows[i].fingerprint)
	}
	return stableEventIDFromKeys(clipID, pts, timeBase, keys)
}

func stableEventIDFromKeys(clipID string, pts int64, timeBase TimeBase, keys []string) string {
	keys = append([]string(nil), keys...)
	sort.Strings(keys)
	var identity strings.Builder
	identity.WriteString(clipID)
	identity.WriteByte(0)
	identity.WriteString(strconv.FormatInt(pts, 10))
	identity.WriteByte(0)
	identity.WriteString(timeBase.String())
	for _, key := range keys {
		identity.WriteByte(0)
		identity.WriteString(key)
	}
	sum := sha256.Sum256([]byte(identity.String()))
	return "kf_" + hex.EncodeToString(sum[:16])
}

func equivalentTimeBase(left, right TimeBase) bool {
	leftDivisor := greatestCommonDivisor(left.Num, left.Den)
	rightDivisor := greatestCommonDivisor(right.Num, right.Den)
	return left.Num/leftDivisor == right.Num/rightDivisor &&
		left.Den/leftDivisor == right.Den/rightDivisor
}

func greatestCommonDivisor(left, right int64) int64 {
	for right != 0 {
		left, right = right, left%right
	}
	if left < 0 {
		return -left
	}
	return left
}
