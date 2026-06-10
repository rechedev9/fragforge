package streamclips

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type Prober interface {
	Probe(ctx context.Context, path string) (SourceProbe, error)
}

type FFprobeProber struct {
	Path string
}

func (p FFprobeProber) Probe(ctx context.Context, filePath string) (SourceProbe, error) {
	if p.Path == "" {
		return SourceProbe{Warnings: []string{"ffprobe not configured"}}, nil
	}
	// #nosec G204 -- ffprobe path is local config and args are not shell-expanded.
	cmd := exec.CommandContext(ctx, p.Path,
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,width,height,r_frame_rate,duration:format=duration",
		"-of", "json",
		filePath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return SourceProbe{}, fmt.Errorf("ffprobe failed: %w: %s", err, msg)
		}
		return SourceProbe{}, fmt.Errorf("ffprobe failed: %w", err)
	}
	var raw struct {
		Streams []struct {
			CodecType  string `json:"codec_type"`
			CodecName  string `json:"codec_name"`
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			RFrameRate string `json:"r_frame_rate"`
			Duration   string `json:"duration"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return SourceProbe{}, fmt.Errorf("decode ffprobe: %w", err)
	}
	probe := SourceProbe{DurationSeconds: parseFloat(raw.Format.Duration)}
	for _, stream := range raw.Streams {
		switch stream.CodecType {
		case "video":
			if probe.VideoCodec == "" {
				probe.VideoCodec = stream.CodecName
				probe.Width = stream.Width
				probe.Height = stream.Height
				probe.FrameRate = stream.RFrameRate
				if probe.DurationSeconds == 0 {
					probe.DurationSeconds = parseFloat(stream.Duration)
				}
			}
		case "audio":
			if probe.AudioCodec == "" {
				probe.AudioCodec = stream.CodecName
			}
		}
	}
	return probe, nil
}

func parseFloat(value string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return f
}
