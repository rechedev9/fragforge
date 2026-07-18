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

type ffprobeOutput struct {
	Streams []struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		RFrameRate string `json:"r_frame_rate"`
		TimeBase   string `json:"time_base"`
		StartTime  string `json:"start_time"`
		Duration   string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration  string `json:"duration"`
		StartTime string `json:"start_time"`
	} `json:"format"`
}

func (p FFprobeProber) Probe(ctx context.Context, filePath string) (SourceProbe, error) {
	if p.Path == "" {
		return SourceProbe{Warnings: []string{"ffprobe not configured"}}, nil
	}
	// #nosec G204 -- ffprobe path is local config and args are not shell-expanded.
	cmd := exec.CommandContext(ctx, p.Path,
		"-v", "error",
		"-show_entries", "stream=codec_type,codec_name,width,height,r_frame_rate,time_base,start_time,duration:format=duration,start_time",
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
	return parseFFprobeOutput(out)
}

func parseFFprobeOutput(out []byte) (SourceProbe, error) {
	var raw ffprobeOutput
	if err := json.Unmarshal(out, &raw); err != nil {
		return SourceProbe{}, fmt.Errorf("decode ffprobe: %w", err)
	}
	probe := SourceProbe{
		DurationSeconds:  parseFloat(raw.Format.Duration),
		StartTimeSeconds: parseFloat(raw.Format.StartTime),
	}
	for _, stream := range raw.Streams {
		switch stream.CodecType {
		case "video":
			if probe.VideoCodec == "" {
				probe.VideoCodec = stream.CodecName
				probe.Width = stream.Width
				probe.Height = stream.Height
				probe.FrameRate = stream.RFrameRate
				probe.VideoTimeBase = stream.TimeBase
				probe.VideoStartTimeSeconds = parseFloat(stream.StartTime)
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
