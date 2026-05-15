package recording

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// CollectArtifacts discovers HLAE take outputs and probes media metadata when
// ffprobe is available. HLAE Source 2 writes takeNNNN/video.mp4 and audio.wav.
func CollectArtifacts(ctx context.Context, plan RecordingPlan, ffprobePath string) []RecordingArtifact {
	files := discoverMediaFiles(plan.OutputDir)
	takeSegments := mapTakesToSegments(files, plan.Segments)
	for i := range files {
		files[i].SegmentID = takeSegments[files[i].TakeID]
		if ffprobePath != "" {
			probeArtifact(ctx, ffprobePath, &files[i])
		}
	}
	return files
}

func FindFFprobe() string {
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return ""
	}
	return path
}

func FindFFmpeg() string {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return ""
	}
	return path
}

func ProbeArtifact(ctx context.Context, ffprobePath string, artifact *RecordingArtifact) {
	probeArtifact(ctx, ffprobePath, artifact)
}

func discoverMediaFiles(root string) []RecordingArtifact {
	var artifacts []RecordingArtifact
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if filepath.Base(path) == "segments" {
				return filepath.SkipDir
			}
			return nil
		}
		mediaType := mediaTypeForExt(filepath.Ext(path))
		if mediaType == "" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		artifacts = append(artifacts, RecordingArtifact{
			TakeID:    nearestTakeID(root, path),
			Type:      mediaType,
			Role:      "raw",
			Path:      path,
			SizeBytes: info.Size(),
		})
		return nil
	})
	sort.SliceStable(artifacts, func(i, j int) bool {
		if artifacts[i].TakeID == artifacts[j].TakeID {
			if artifacts[i].Type == artifacts[j].Type {
				return artifacts[i].Path < artifacts[j].Path
			}
			return artifacts[i].Type > artifacts[j].Type // video before audio
		}
		return takeLess(artifacts[i].TakeID, artifacts[j].TakeID)
	})
	return artifacts
}

func mediaTypeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4", ".mkv", ".mov", ".avi", ".tga", ".exr":
		return "video"
	case ".wav", ".flac", ".aac", ".m4a":
		return "audio"
	default:
		return ""
	}
}

func nearestTakeID(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	parts := strings.Split(rel, string(filepath.Separator))
	for i := len(parts) - 1; i >= 0; i-- {
		if isTakeID(parts[i]) {
			return parts[i]
		}
	}
	return ""
}

func isTakeID(s string) bool {
	if !strings.HasPrefix(s, "take") || len(s) != len("take0000") {
		return false
	}
	_, err := strconv.Atoi(strings.TrimPrefix(s, "take"))
	return err == nil
}

func mapTakesToSegments(artifacts []RecordingArtifact, segments []RecordingSegment) map[string]string {
	takes := make([]string, 0)
	seen := map[string]bool{}
	for _, a := range artifacts {
		if a.TakeID == "" || seen[a.TakeID] {
			continue
		}
		seen[a.TakeID] = true
		takes = append(takes, a.TakeID)
	}
	sort.SliceStable(takes, func(i, j int) bool {
		return takeLess(takes[i], takes[j])
	})
	out := map[string]string{}
	for i, take := range takes {
		if i < len(segments) {
			out[take] = segments[i].ID
		}
	}
	return out
}

func takeLess(a, b string) bool {
	ai, aok := takeNumber(a)
	bi, bok := takeNumber(b)
	if aok && bok {
		return ai < bi
	}
	return a < b
}

func takeNumber(s string) (int, bool) {
	if !isTakeID(s) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, "take"))
	return n, err == nil
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		Duration     string `json:"duration"`
		NBFrames     string `json:"nb_frames"`
		SampleRate   string `json:"sample_rate"`
		Channels     int    `json:"channels"`
		AvgFrameRate string `json:"avg_frame_rate"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func probeArtifact(ctx context.Context, ffprobePath string, artifact *RecordingArtifact) {
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-show_streams",
		"-of", "json",
		artifact.Path,
	)
	out, err := cmd.Output()
	if err != nil {
		artifact.ProbeError = fmt.Sprintf("ffprobe: %v", err)
		return
	}
	if err := applyProbeOutput(artifact, out); err != nil {
		artifact.ProbeError = err.Error()
	}
}

func applyProbeOutput(artifact *RecordingArtifact, out []byte) error {
	var probe ffprobeOutput
	if err := json.Unmarshal(out, &probe); err != nil {
		return fmt.Errorf("parse ffprobe: %v", err)
	}
	if len(probe.Streams) == 0 {
		return fmt.Errorf("ffprobe: no streams")
	}
	stream := probe.Streams[0]
	artifact.Codec = stream.CodecName
	artifact.FrameRate = stream.AvgFrameRate
	if stream.CodecType != "" {
		artifact.Type = stream.CodecType
	}
	artifact.Width = stream.Width
	artifact.Height = stream.Height
	artifact.Channels = stream.Channels
	if stream.SampleRate != "" {
		if v, err := strconv.Atoi(stream.SampleRate); err == nil {
			artifact.SampleRate = v
		}
	}
	if stream.NBFrames != "" {
		if v, err := strconv.ParseInt(stream.NBFrames, 10, 64); err == nil {
			artifact.FrameCount = v
		}
	}
	duration := stream.Duration
	if duration == "" {
		duration = probe.Format.Duration
	}
	if duration != "" {
		if v, err := strconv.ParseFloat(duration, 64); err == nil {
			artifact.DurationSeconds = v
		}
	}
	return nil
}
