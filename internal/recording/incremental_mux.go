package recording

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

// IncrementalMuxer publishes segments/<id>.mp4 clips while HLAE is still
// recording, so observers (the orchestrator's segment watcher, and through it
// the library card's capture progress) see each segment as soon as it is done
// instead of only after the whole run.
//
// HLAE records the plan's segments sequentially, one takeNNNN directory per
// segment in plan order, and creates the next take directory only after ending
// the previous segment's recording. A take is therefore finished exactly when a
// strictly later take directory exists; the newest take may still be streaming
// and is never touched here — the end-of-run MuxSegmentClips pass owns it (CS2
// exiting is its completion signal). Outputs are published atomically (temp
// name + rename), so a segments/<id>.mp4 from this path never exists
// half-written.
type IncrementalMuxer struct {
	plan       RecordingPlan
	ffmpegPath string
	muxed      map[string]bool
}

// NewIncrementalMuxer returns a muxer for the plan. With an empty ffmpegPath
// MuxFinished is a no-op, mirroring MuxSegmentClips.
func NewIncrementalMuxer(plan RecordingPlan, ffmpegPath string) *IncrementalMuxer {
	return &IncrementalMuxer{plan: plan, ffmpegPath: ffmpegPath, muxed: map[string]bool{}}
}

// MuxFinished muxes every finished take that has not been published yet and
// returns the newly published segment ids. Best-effort: a failed mux is left
// unmarked so the next call retries it, and it never returns an error — the
// end-of-run pass re-muxes anything still missing.
func (m *IncrementalMuxer) MuxFinished(ctx context.Context) []string {
	if m.ffmpegPath == "" {
		return nil
	}
	var published []string
	for _, take := range finishedTakePairs(m.plan) {
		if m.muxed[take.segmentID] {
			continue
		}
		out := filepath.Join(m.plan.OutputDir, "segments", take.segmentID+".mp4")
		if err := muxPair(ctx, m.ffmpegPath, take.videoPath, take.audioPath, out); err != nil {
			continue
		}
		m.muxed[take.segmentID] = true
		published = append(published, take.segmentID)
	}
	return published
}

// finishedTake is one completed take mapped to its plan segment.
type finishedTake struct {
	segmentID string
	videoPath string
	audioPath string
}

// finishedTakePairs maps completed take directories to plan segments. Takes are
// direct children of the plan's output dir, sorted by take number; the i-th
// take records the i-th plan segment (the same positional mapping as
// mapTakesToSegments). The newest take is excluded because HLAE may still be
// writing it, and a take without both video.mp4 and audio.wav is skipped.
func finishedTakePairs(plan RecordingPlan) []finishedTake {
	takes := takeDirNames(plan.OutputDir)
	if len(takes) < 2 {
		return nil
	}
	finished := len(takes) - 1 // the newest take may still be streaming
	if finished > len(plan.Segments) {
		finished = len(plan.Segments)
	}
	out := make([]finishedTake, 0, finished)
	for i := 0; i < finished; i++ {
		video := filepath.Join(plan.OutputDir, takes[i], "video.mp4")
		audio := filepath.Join(plan.OutputDir, takes[i], "audio.wav")
		if !fileExists(video) || !fileExists(audio) {
			continue
		}
		out = append(out, finishedTake{
			segmentID: plan.Segments[i].ID,
			videoPath: video,
			audioPath: audio,
		})
	}
	return out
}

// takeDirNames lists the takeNNNN directories directly under root, sorted by
// take number. A missing root yields no names.
func takeDirNames(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && isTakeID(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.SliceStable(names, func(i, j int) bool { return takeLess(names[i], names[j]) })
	return names
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// muxPair combines one take's video and audio into out, publishing atomically:
// ffmpeg writes to a temp name and the finished file is renamed into place, so
// out never exists half-written.
func muxPair(ctx context.Context, ffmpegPath, video, audio, out string) error {
	if err := os.MkdirAll(filepath.Dir(out), 0o750); err != nil {
		return err
	}
	tmp := out + ".part"
	// #nosec G204 -- ffmpegPath is configured locally and media paths are passed as arguments.
	cmd := exec.CommandContext(ctx, ffmpegPath,
		"-y",
		"-v", "error",
		"-i", video,
		"-i", audio,
		"-map", "0:v:0",
		"-map", "1:a:0",
		"-c:v", "copy",
		"-c:a", "aac",
		"-b:a", "192k",
		"-shortest",
		"-f", "mp4", // the .part temp name hides the container from ffmpeg
		tmp,
	)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, out)
}
