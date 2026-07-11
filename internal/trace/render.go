package trace

import (
	"fmt"

	"github.com/rechedev9/fragforge/internal/editor"
	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/recording"
)

// Render captures the render decision: the resolved preset, the output shape,
// a summary of the compiled edit, and the FFmpeg argv that would run. The argv
// comes from the pure builders in internal/editor (editor.BuildFFmpegCommand),
// never from executing FFmpeg. It is a slice of arg lists so the shape stays
// stable if a trace ever emits more than one short.
type Render struct {
	Preset     string      `json:"preset"`
	Output     OutputShape `json:"output"`
	Summary    EditSummary `json:"summary"`
	FFmpegArgv [][]string  `json:"ffmpeg_argv"`
}

// OutputShape is the geometry every FragForge short renders at, derived from
// the preset (1080x1920 / 60fps by construction of the registry).
type OutputShape struct {
	Format string `json:"format"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	FPS    int    `json:"fps"`
}

// EditSummary is the at-a-glance shape of the compiled edit the argv renders:
// how many segments compile into it, the kill count, the timeline duration,
// and the encoder settings inherited from the preset.
type EditSummary struct {
	Compiled        bool    `json:"compiled"`
	SegmentCount    int     `json:"segment_count"`
	KillCount       int     `json:"kill_count"`
	DurationSeconds float64 `json:"duration_seconds"`
	TailTrimSeconds float64 `json:"tail_trim_seconds"`
	VideoCRF        int     `json:"video_crf"`
	VideoPreset     string  `json:"video_preset"`
}

// buildRender assembles the render plan: it compiles every plan segment into a
// single vertical short (the `zv short` default), builds its FFmpeg argv with
// the pure editor builders using one synthetic input clip per segment, and
// summarizes the edit. The ShortEdit is assembled here rather than through the
// unexported, IO-bound editor.buildManifest, which needs a recording result
// and on-disk clips; the trace intentionally substitutes placeholder inputs.
func buildRender(plan killplan.Plan, preset editor.RenderPreset, tailTrimSeconds float64, deterministic bool) Render {
	render := Render{
		Preset: preset.Name,
		Output: OutputShape{
			Format: editor.OutputFormatShort9x16,
			Width:  preset.Width,
			Height: preset.Height,
			FPS:    preset.FPS,
		},
		FFmpegArgv: [][]string{},
	}

	short, ok := compiledShort(plan, preset, tailTrimSeconds)
	render.Summary = EditSummary{
		Compiled:        ok,
		SegmentCount:    len(short.Parts),
		KillCount:       short.KillCount,
		DurationSeconds: short.DurationSeconds,
		TailTrimSeconds: tailTrimSeconds,
		VideoCRF:        preset.VideoCRF,
		VideoPreset:     preset.VideoPreset,
	}
	if !ok {
		return render
	}

	ffmpeg := "ffmpeg"
	if !deterministic {
		if found := recording.FindFFmpeg(); found != "" {
			ffmpeg = found
		}
	}
	render.FFmpegArgv = [][]string{editor.BuildFFmpegCommand(ffmpeg, short)}
	return render
}

// compiledShort builds one compiled ShortEdit from the plan: a ShortPart per
// segment with a synthetic input path (segment-<id>.mp4), preset-derived
// encoder settings, and per-kill cues. Cues, durations, tail trimming, and
// timeline placement all come from the same exported editor functions
// production uses (editor.KillCues, editor.ClipDuration,
// editor.TailTrimmedDuration), so the argv matches what zv-editor would run;
// only the clip paths are placeholders by construction. ok is false when no
// segment yields a clip.
func compiledShort(plan killplan.Plan, preset editor.RenderPreset, tailTrimSeconds float64) (editor.ShortEdit, bool) {
	tickrate := plan.Demo.Tickrate

	var parts []editor.ShortPart
	var kills []editor.KillCue
	killCount := 0
	cursor := 0.0
	for _, segment := range plan.Segments {
		recSegment := recordingSegment(segment)
		partKills := editor.KillCues(recSegment, tickrate)
		duration := editor.TailTrimmedDuration(partKills, editor.ClipDuration(recSegment, tickrate, 0), tailTrimSeconds)
		if duration <= 0 {
			continue
		}
		for _, kill := range partKills {
			kill.TimeSeconds += cursor
			kills = append(kills, kill)
		}
		killCount += len(segment.Kills)
		parts = append(parts, editor.ShortPart{
			SegmentID:            segment.ID,
			Input:                fmt.Sprintf("segment-%s.mp4", segment.ID),
			DurationSeconds:      duration,
			TimelineStartSeconds: cursor,
			Kills:                partKills,
		})
		cursor += duration
	}
	if len(parts) == 0 {
		return editor.ShortEdit{}, false
	}

	short := editor.ShortEdit{
		Index:           1,
		SegmentID:       "demo-compilation",
		Preset:          preset.Name,
		Player:          plan.Target.NameInDemo,
		Map:             plan.Demo.Map,
		KillCount:       killCount,
		Input:           parts[0].Input,
		Output:          "short-001-demo-compilation.mp4",
		OutputFormat:    editor.OutputFormatShort9x16,
		TailTrimSeconds: tailTrimSeconds,
		OutputFPS:       preset.FPS,
		VideoCRF:        preset.VideoCRF,
		VideoPreset:     preset.VideoPreset,
		HQFilters:       preset.HQFilters,
		AudioNormalize:  preset.AudioNormalize,
		DurationSeconds: cursor,
		Kills:           kills,
		Parts:           parts,
	}
	return short, true
}

// recordingSegment adapts a kill plan segment to the recording segment shape
// the editor helpers consume; the two types share the same fields because the
// kill plan is the contract every later stage derives from.
func recordingSegment(segment killplan.Segment) recording.RecordingSegment {
	return recording.RecordingSegment{
		ID:        segment.ID,
		Round:     segment.Round,
		TickStart: segment.TickStart,
		TickEnd:   segment.TickEnd,
		Kills:     segment.Kills,
		Utility:   segment.Utility,
	}
}
