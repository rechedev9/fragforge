package streamcli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/rechedev9/fragforge/internal/pathguard"
)

type streamInputPath struct {
	flag string
	path string
}

func rejectStreamOutputAliases(output string, inputs ...streamInputPath) error {
	guardInputs := make([]pathguard.Input, len(inputs))
	for i, input := range inputs {
		guardInputs[i] = pathguard.Input{Flag: input.flag, Path: input.path}
	}
	return pathguard.RejectOutputAliases(output, guardInputs...)
}

func rejectStreamInputsWithinDirectory(directory string, inputs ...streamInputPath) error {
	guardInputs := make([]pathguard.Input, len(inputs))
	for i, input := range inputs {
		guardInputs[i] = pathguard.Input{Flag: input.flag, Path: input.path}
	}
	return pathguard.RejectInputsWithinDirectory(directory, guardInputs...)
}

func (localStreamService) ValidateFFmpeg(ctx context.Context, ffmpeg string, requireWhisper bool) error {
	if strings.TrimSpace(ffmpeg) == "" {
		return fmt.Errorf("ffmpeg is not configured")
	}
	resolved, err := exec.LookPath(ffmpeg)
	if err != nil {
		return fmt.Errorf("ffmpeg %q is not accessible: %w", ffmpeg, err)
	}
	if !requireWhisper {
		return nil
	}
	// #nosec G204 -- resolved is the user-selected local FFmpeg executable and
	// the fixed argument list is passed directly without a shell.
	cmd := exec.CommandContext(ctx, resolved, "-hide_banner", "-filters")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect ffmpeg filters: %w", err)
	}
	if !ffmpegHasFilter(out, "whisper") {
		return fmt.Errorf("ffmpeg %q does not include the required whisper filter", resolved)
	}
	return nil
}

func ffmpegHasFilter(output []byte, name string) bool {
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			return true
		}
	}
	return false
}
