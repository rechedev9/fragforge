package streamcli

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"

	"github.com/rechedev9/fragforge/internal/streamclips"
)

const streamCoverAfterKillSeconds = 0.25

type streamCoverGenerator interface {
	Generate(ctx context.Context, ffmpeg, videoPath, coverPath string, atSeconds float64) error
}

type ffmpegStreamCoverGenerator struct{}

func (ffmpegStreamCoverGenerator) Generate(ctx context.Context, ffmpeg, videoPath, coverPath string, atSeconds float64) error {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", videoPath,
		"-ss", strconv.FormatFloat(atSeconds, 'f', 3, 64),
		"-frames:v", "1", "-q:v", "2", coverPath,
	}
	// #nosec G204 -- ffmpeg is a configured local executable and arguments are
	// passed directly without a shell.
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(out))
	if len(detail) > 4096 {
		detail = detail[:4096] + "..."
	}
	if detail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, detail)
}

// streamCoverTimestamp chooses the strongest confirmed killfeed event and
// maps its source timestamp onto the rendered clip timeline. When there is no
// confirmed event, the first third of the clip is a stable, non-black default.
func streamCoverTimestamp(plan streamclips.EditPlan, clipID string, renderedDuration float64) float64 {
	for _, clip := range plan.Clips {
		if clip.ID != clipID {
			continue
		}
		speed := clip.EffectiveSpeed()
		at := renderedDuration * 0.35
		best := -1
		for i := range clip.KillfeedSeconds {
			if killCountAt(clip, i) > 0 && (best < 0 || killCountAt(clip, i) > killCountAt(clip, best)) {
				best = i
			}
		}
		if best >= 0 {
			at = (clip.KillfeedSeconds[best] - clip.StartSeconds + streamCoverAfterKillSeconds) / speed
		}
		return clampStreamCoverTimestamp(at, renderedDuration)
	}
	return clampStreamCoverTimestamp(renderedDuration*0.35, renderedDuration)
}

func killCountAt(clip streamclips.ClipRange, index int) int {
	if index < 0 || index >= len(clip.KillfeedKills) {
		return 0
	}
	return len(clip.KillfeedKills[index])
}

func clampStreamCoverTimestamp(at, duration float64) float64 {
	if math.IsNaN(at) || math.IsInf(at, 0) || at < 0 {
		at = 0
	}
	if duration > 0 {
		lastFrame := math.Max(0, duration-0.05)
		at = math.Min(at, lastFrame)
	}
	return at
}
