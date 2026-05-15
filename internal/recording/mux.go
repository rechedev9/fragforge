package recording

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// MuxSegmentClips combines each segment take's video.mp4 and audio.wav into a
// consumable MP4 under <output>/segments/<segment-id>.mp4.
func MuxSegmentClips(ctx context.Context, plan RecordingPlan, artifacts []RecordingArtifact, ffmpegPath, ffprobePath string) []RecordingArtifact {
	if ffmpegPath == "" {
		return nil
	}
	pairs := segmentMediaPairs(artifacts)
	if len(pairs) == 0 {
		return nil
	}

	outDir := filepath.Join(plan.OutputDir, "segments")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return []RecordingArtifact{{
			Role:       "segment",
			Path:       outDir,
			ProbeError: fmt.Sprintf("create segment output dir: %v", err),
		}}
	}

	segmentOrder := make(map[string]int, len(plan.Segments))
	for i, s := range plan.Segments {
		segmentOrder[s.ID] = i
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return segmentOrder[pairs[i].segmentID] < segmentOrder[pairs[j].segmentID]
	})

	out := make([]RecordingArtifact, 0, len(pairs))
	for _, pair := range pairs {
		path := filepath.Join(outDir, pair.segmentID+".mp4")
		artifact := RecordingArtifact{
			SegmentID: pair.segmentID,
			TakeID:    pair.takeID,
			Type:      "video",
			Role:      "segment",
			Path:      path,
		}
		cmd := exec.CommandContext(ctx, ffmpegPath,
			"-y",
			"-v", "error",
			"-i", pair.video.Path,
			"-i", pair.audio.Path,
			"-map", "0:v:0",
			"-map", "1:a:0",
			"-c:v", "copy",
			"-c:a", "aac",
			"-b:a", "192k",
			"-shortest",
			path,
		)
		if err := cmd.Run(); err != nil {
			artifact.ProbeError = fmt.Sprintf("ffmpeg mux: %v", err)
			out = append(out, artifact)
			continue
		}
		if info, err := os.Stat(path); err == nil {
			artifact.SizeBytes = info.Size()
		}
		if ffprobePath != "" {
			probeArtifact(ctx, ffprobePath, &artifact)
		}
		out = append(out, artifact)
	}
	return out
}

type segmentMediaPair struct {
	segmentID string
	takeID    string
	video     RecordingArtifact
	audio     RecordingArtifact
}

func segmentMediaPairs(artifacts []RecordingArtifact) []segmentMediaPair {
	type partial struct {
		video RecordingArtifact
		audio RecordingArtifact
	}
	grouped := map[string]partial{}
	for _, a := range artifacts {
		if a.SegmentID == "" || a.TakeID == "" {
			continue
		}
		key := a.SegmentID + "\x00" + a.TakeID
		p := grouped[key]
		switch a.Type {
		case "video":
			if p.video.Path == "" || filepath.Base(a.Path) == "video.mp4" {
				p.video = a
			}
		case "audio":
			if p.audio.Path == "" || filepath.Base(a.Path) == "audio.wav" {
				p.audio = a
			}
		}
		grouped[key] = p
	}

	pairs := make([]segmentMediaPair, 0, len(grouped))
	for key, p := range grouped {
		if p.video.Path == "" || p.audio.Path == "" {
			continue
		}
		segmentID, takeID := splitPairKey(key)
		pairs = append(pairs, segmentMediaPair{
			segmentID: segmentID,
			takeID:    takeID,
			video:     p.video,
			audio:     p.audio,
		})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if pairs[i].segmentID == pairs[j].segmentID {
			return takeLess(pairs[i].takeID, pairs[j].takeID)
		}
		return pairs[i].segmentID < pairs[j].segmentID
	})
	return pairs
}

func splitPairKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == 0 {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
