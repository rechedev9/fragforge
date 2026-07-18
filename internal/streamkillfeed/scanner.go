package streamkillfeed

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

type scanWindow struct {
	start float64
	end   float64
}

// Scan uses an eight-fps pass only to locate short candidate windows. Every
// returned event is then derived from all native frames in those windows.
func (a Analyzer) Scan(
	ctx context.Context,
	sourcePath string,
	probe streamclips.SourceProbe,
	crop streamclips.CropRect,
	clip streamclips.ClipRange,
) ([]Event, error) {
	if strings.TrimSpace(sourcePath) == "" {
		return nil, fmt.Errorf("source path is required")
	}
	if strings.TrimSpace(a.FFmpegPath) == "" {
		return nil, fmt.Errorf("ffmpeg path is required")
	}
	if err := crop.Validate("killfeed_crop"); err != nil {
		return nil, err
	}
	if err := clip.Validate(); err != nil {
		return nil, err
	}
	if math.IsNaN(probe.StartTimeSeconds) || math.IsInf(probe.StartTimeSeconds, 0) {
		return nil, fmt.Errorf("source start time must be finite")
	}
	if math.IsNaN(probe.DurationSeconds) || math.IsInf(probe.DurationSeconds, 0) ||
		probe.DurationSeconds < 0 {
		return nil, fmt.Errorf("source duration must be finite and >= 0")
	}

	var expectedTimeBase *TimeBase
	if strings.TrimSpace(probe.VideoTimeBase) != "" {
		parsed, err := ParseTimeBase(probe.VideoTimeBase)
		if err != nil {
			return nil, fmt.Errorf("source video time base: %w", err)
		}
		expectedTimeBase = &parsed
	}

	interval := 1.0 / CoarseFPS
	scanStart := max(0, clip.StartSeconds-LookbackSeconds)
	coarseEnd := clip.EndSeconds + interval
	nativeEnd := clip.EndSeconds + SampleDelaySeconds + interval
	if probe.DurationSeconds > 0 {
		if clip.StartSeconds >= probe.DurationSeconds {
			return nil, fmt.Errorf(
				"clip %s starts at %.6f beyond source duration %.6f",
				clip.ID, clip.StartSeconds, probe.DurationSeconds,
			)
		}
		coarseEnd = min(coarseEnd, probe.DurationSeconds)
		nativeEnd = min(nativeEnd, probe.DurationSeconds)
	}

	coarseFrames, err := a.decodeRange(ctx, decodeRequest{
		sourcePath:   sourcePath,
		startSeconds: scanStart,
		endSeconds:   coarseEnd,
		coarse:       true,
		probeStart:   probe.StartTimeSeconds,
	}, crop)
	if err != nil {
		return nil, fmt.Errorf("scan coarse killfeed timeline: %w", err)
	}
	if len(coarseFrames) == 0 {
		return nil, fmt.Errorf("scan coarse killfeed timeline: ffmpeg produced no frames")
	}
	windows := locateRefinementWindows(coarseFrames, clip, scanStart, nativeEnd)
	if len(windows) == 0 {
		return []Event{}, nil
	}

	allEvents := make([]Event, 0, len(windows))
	for _, window := range windows {
		request := decodeRequest{
			sourcePath:       sourcePath,
			startSeconds:     window.start,
			endSeconds:       window.end,
			probeStart:       probe.StartTimeSeconds,
			expectedTimeBase: expectedTimeBase,
		}
		nativeFrames, err := decodeRefinementFrames(
			ctx,
			request,
			crop,
			a.decodeRange,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"refine killfeed window %.9f-%.9f: %w",
				window.start, window.end, err,
			)
		}
		events, err := detectFrameEvents(nativeFrames, clip)
		if err != nil {
			return nil, fmt.Errorf(
				"detect killfeed births in window %.9f-%.9f: %w",
				window.start, window.end, err,
			)
		}
		allEvents = append(allEvents, events...)
	}
	return consolidateEvents(clip.ID, allEvents), nil
}

type frameRangeDecoder func(
	context.Context,
	decodeRequest,
	streamclips.CropRect,
) ([]frameObservation, error)

// decodeRefinementFrames guarantees that a mid-source refinement window has a
// real native frame before its first decoded frame whenever one exists. A
// fixed time lookback is insufficient for VFR sources: two adjacent frames can
// have an arbitrarily large presentation gap, and an accurate input seek drops
// the earlier frame when the seek target lands inside that gap.
//
// The search widens geometrically toward the source boundary. Once an earlier
// frame is found, only the nearest predecessor is prepended to the original
// window. Its integer PTS and decoded row evidence are preserved verbatim; no
// synthetic timestamp or frame-rate conversion enters native event detection.
func decodeRefinementFrames(
	ctx context.Context,
	request decodeRequest,
	crop streamclips.CropRect,
	decode frameRangeDecoder,
) ([]frameObservation, error) {
	frames, err := decode(ctx, request, crop)
	if err != nil || len(frames) == 0 || request.startSeconds <= 0 {
		return frames, err
	}

	anchor := frames[0]
	searchStart := request.startSeconds
	searchSpan := max(1.0/CoarseFPS, request.endSeconds-request.startSeconds)
	for searchStart > 0 {
		nextStart := max(0, searchStart-searchSpan)
		widenedRequest := request
		widenedRequest.startSeconds = nextStart
		widened, decodeErr := decode(ctx, widenedRequest, crop)
		if decodeErr != nil {
			return nil, fmt.Errorf(
				"decode preceding native frame from %.9f: %w",
				nextStart,
				decodeErr,
			)
		}
		if predecessor, ok := nearestPrecedingFrame(widened, anchor); ok {
			result := make([]frameObservation, 0, len(frames)+1)
			result = append(result, predecessor)
			result = append(result, frames...)
			return result, nil
		}
		if nextStart == 0 {
			return frames, nil
		}
		searchStart = nextStart
		searchSpan *= 2
	}
	return frames, nil
}

func nearestPrecedingFrame(
	frames []frameObservation,
	anchor frameObservation,
) (frameObservation, bool) {
	var predecessor frameObservation
	found := false
	for _, frame := range frames {
		if !equivalentTimeBase(frame.timeBase, anchor.timeBase) || frame.pts >= anchor.pts {
			continue
		}
		if !found || frame.pts > predecessor.pts {
			predecessor = frame
			found = true
		}
	}
	return predecessor, found
}

// Analyze is an explicit verb alias for callers that do not need the Scanner
// interface name.
func (a Analyzer) Analyze(
	ctx context.Context,
	sourcePath string,
	probe streamclips.SourceProbe,
	crop streamclips.CropRect,
	clip streamclips.ClipRange,
) ([]Event, error) {
	return a.Scan(ctx, sourcePath, probe, crop, clip)
}

func locateRefinementWindows(
	frames []frameObservation,
	clip streamclips.ClipRange,
	scanStart float64,
	scanEnd float64,
) []scanWindow {
	interval := 1.0 / CoarseFPS
	windows := make([]scanWindow, 0)
	for i := range frames {
		current := frames[i]
		if current.seconds < clip.StartSeconds-interval ||
			current.seconds > clip.EndSeconds+interval {
			continue
		}
		var previousRows []observedRow
		if i > 0 {
			previousRows = frames[i-1].rows
		}
		if len(bornRows(previousRows, current.rows)) == 0 {
			continue
		}

		start := current.seconds - 2*interval
		if i > 0 {
			start = frames[i-1].seconds - interval
		}
		start = max(scanStart, start)
		end := min(scanEnd, current.seconds+SampleDelaySeconds+interval)
		if end <= start {
			continue
		}
		windows = append(windows, scanWindow{start: start, end: end})
	}
	if len(windows) == 0 {
		return nil
	}
	sort.Slice(windows, func(i, j int) bool {
		if windows[i].start != windows[j].start {
			return windows[i].start < windows[j].start
		}
		return windows[i].end < windows[j].end
	})
	merged := windows[:1]
	for _, window := range windows[1:] {
		last := &merged[len(merged)-1]
		if window.start <= last.end+1e-9 {
			last.end = max(last.end, window.end)
			continue
		}
		merged = append(merged, window)
	}
	return merged
}

func consolidateEvents(clipID string, events []Event) []Event {
	if len(events) == 0 {
		return []Event{}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].CueSeconds != events[j].CueSeconds {
			return events[i].CueSeconds < events[j].CueSeconds
		}
		if events[i].SourcePTS != events[j].SourcePTS {
			return events[i].SourcePTS < events[j].SourcePTS
		}
		return events[i].EventID < events[j].EventID
	})

	result := make([]Event, 0, len(events))
	for _, event := range events {
		if len(result) == 0 ||
			result[len(result)-1].SourcePTS != event.SourcePTS ||
			!equivalentTimeBase(result[len(result)-1].TimeBase, event.TimeBase) {
			result = append(result, event)
			continue
		}
		current := &result[len(result)-1]
		existing := make(map[string]struct{}, len(current.Rows))
		for _, row := range current.Rows {
			existing[row.Fingerprint] = struct{}{}
		}
		for _, row := range event.Rows {
			if _, ok := existing[row.Fingerprint]; ok {
				continue
			}
			current.Rows = append(current.Rows, row)
			existing[row.Fingerprint] = struct{}{}
		}
		current.OnsetStartPTS = min(current.OnsetStartPTS, event.OnsetStartPTS)
		if event.SamplePTS > current.SamplePTS {
			current.SamplePTS = event.SamplePTS
			current.SampleSeconds = event.SampleSeconds
		}
		if len(current.Rows) > 1 {
			current.Mode = ModeBurst
		}
		keys := make([]string, len(current.Rows))
		for i := range current.Rows {
			keys[i] = current.Rows[i].Fingerprint
		}
		current.EventID = stableEventIDFromKeys(
			clipID, current.SourcePTS, current.TimeBase, keys,
		)
	}
	return result
}
