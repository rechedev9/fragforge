package streamcli

import (
	"context"

	"github.com/rechedev9/fragforge/internal/streamclips"
	"github.com/rechedev9/fragforge/internal/streamkillfeed"
)

func detectKillfeedCues(
	ctx context.Context,
	ffmpeg string,
	input string,
	probe streamclips.SourceProbe,
	crop streamclips.CropRect,
	clip streamclips.ClipRange,
) ([]float64, error) {
	events, err := (streamkillfeed.Analyzer{FFmpegPath: ffmpeg}).Scan(ctx, input, probe, crop, clip)
	if err != nil {
		return nil, err
	}
	return exactKillfeedCues(events), nil
}

// exactKillfeedCues exposes only events whose first visible native source
// frame is known. Unresolved bursts stay out of edit plans rather than being
// assigned a synthetic timestamp.
func exactKillfeedCues(events []streamkillfeed.Event) []float64 {
	cues := make([]float64, 0, len(events))
	for _, event := range events {
		switch event.Mode {
		case streamkillfeed.ModeAlignedFrame, streamkillfeed.ModeBurst:
			cues = append(cues, event.CueSeconds)
		case streamkillfeed.ModeUnresolved:
			continue
		}
	}
	return cues
}
