package streamclips

import (
	"strings"
	"testing"
)

func TestEditPlanValidationRejectsOutOfBoundsCrop(t *testing.T) {
	plan := DefaultEditPlan()
	plan.FaceCrop = CropRect{X: 0.8, Y: 0, Width: 0.3, Height: 0.2}

	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "within the source frame") {
		t.Fatalf("Validate error = %v, want source frame bounds error", err)
	}
}

func TestEditPlanValidationRejectsInvalidClipRange(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 12, EndSeconds: 10}}

	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "greater than start_seconds") {
		t.Fatalf("Validate error = %v, want range error", err)
	}
}

func TestBuildFFmpegArgsCreatesVerticalStackCommand(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 1.5, EndSeconds: 4.25}}

	args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}, plan, plan.Clips[0])
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-ss 1.500",
		"-t 2.750",
		"scale=1080:768",
		"scale=1080:1152",
		"vstack=inputs=2",
		"fps=60",
		"-map 0:a?",
		"-crf 18",
		"-movflags +faststart",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
	if got := args[len(args)-1]; got != "out.mp4" {
		t.Fatalf("last arg = %q, want output path", got)
	}
}

func TestBuildFFmpegArgsLegacyVariantUnchanged(t *testing.T) {
	plan := EditPlan{
		Variant:      VariantStreamerVerticalStack,
		FaceCrop:     CropRect{X: 0, Y: 0, Width: 1, Height: 0.35},
		GameplayCrop: CropRect{X: 0, Y: 0.35, Width: 1, Height: 0.65},
	}
	clip := ClipRange{ID: "clip-001", StartSeconds: 1.5, EndSeconds: 4.25}

	args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-ss 1.500",
		"-t 2.750",
		"split=2[facein][gamein]",
		"scale=1080:520:force_original_aspect_ratio=increase,crop=1080:520[face]",
		"scale=1080:1400:force_original_aspect_ratio=increase,crop=1080:1400[game]",
		"vstack=inputs=2",
		"fps=60",
		"-map 0:a?",
		"-crf 18",
		"-movflags +faststart",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
}

func TestBuildFFmpegArgsFullframeNoCamCommand(t *testing.T) {
	plan := EditPlan{
		Variant:      VariantStreamerFullframeNoCam,
		GameplayCrop: CropRect{X: 0, Y: 0, Width: 1, Height: 1},
	}
	clip := ClipRange{ID: "clip-001", StartSeconds: 1.5, EndSeconds: 4.25}

	args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "split=2") || strings.Contains(joined, "vstack") {
		t.Fatalf("fullframe-nocam args must not split or stack: %s", joined)
	}
	for _, want := range []string{
		"scale=1080:1920:force_original_aspect_ratio=increase,crop=1080:1920",
		"fps=60",
		"-map 0:a?",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
}

func TestBuildFFmpegArgsMixesMusicUnderOriginalAudio(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Music = MusicPlan{Key: "concrete-teeth", Volume: 0.3}
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 5}}

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:     "source.mp4",
		OutputPath:     "out.mp4",
		MusicPath:      "music/concrete-teeth.mp3",
		SourceHasAudio: true,
	}, plan, plan.Clips[0])
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"-stream_loop -1 -i music/concrete-teeth.mp3",
		"[1:a]volume=0.300000[bgm]",
		"[0:a][bgm]amix=inputs=2:duration=first:dropout_transition=0:normalize=0[a]",
		"-map [a]",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "-map 0:a?") {
		t.Fatalf("music mix must replace the passthrough audio map: %s", joined)
	}
	if strings.Contains(joined, "-shortest") {
		t.Fatalf("amix duration=first already bounds the mix; -shortest is for silent sources only: %s", joined)
	}
}

func TestBuildFFmpegArgsMusicOnSilentSourceUsesMusicAlone(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Music = MusicPlan{Key: "concrete-teeth"}
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 5}}

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:     "source.mp4",
		OutputPath:     "out.mp4",
		MusicPath:      "music/concrete-teeth.mp3",
		SourceHasAudio: false,
	}, plan, plan.Clips[0])
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	// Default volume applies when the plan does not set one.
	for _, want := range []string{"[1:a]volume=0.250000[a]", "-map [a]", "-shortest"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "amix") {
		t.Fatalf("silent source must not amix a missing stream: %s", joined)
	}
}

func TestBuildFFmpegArgsGradeInsertsEqFilter(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Effects = EffectsPlan{Grade: true}
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 5}}

	args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}, plan, plan.Clips[0])
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "eq=contrast=1.05:saturation=1.15,fps=60") {
		t.Fatalf("args missing grade filter before fps: %s", joined)
	}
}

func TestEditPlanValidationRejectsBadMusic(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Music = MusicPlan{Key: "../escape"}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "invalid music key") {
		t.Fatalf("Validate error = %v, want invalid music key", err)
	}

	plan.Music = MusicPlan{Key: "concrete-teeth", Volume: 1.5}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "music volume") {
		t.Fatalf("Validate error = %v, want music volume error", err)
	}
}

func TestBuildFFmpegArgsRejectsUnknownVariant(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Variant = "other"
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 1.5, EndSeconds: 4.25}}

	_, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}, plan, plan.Clips[0])
	if err == nil || !strings.Contains(err.Error(), "unsupported stream render variant") {
		t.Fatalf("BuildFFmpegArgs error = %v, want unsupported variant error", err)
	}
}
