package streamclips

import (
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/mediafont"
)

func TestFindBannerFontPrefersEmbeddedMontserrat(t *testing.T) {
	got := FindBannerFont()
	if filepath.Base(got) != mediafont.FileName {
		t.Fatalf("FindBannerFont = %q, want embedded %s", got, mediafont.FileName)
	}
}

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
	if strings.Contains(joined, "drawtext=") {
		t.Fatalf("empty streamer nick must not add a banner: %s", joined)
	}
}

func TestBuildFFmpegArgsOldStreamerBannerPlanStaysStaticAtStackSeam(t *testing.T) {
	var banner StreamerBannerPlan
	if err := json.Unmarshal([]byte(`{"nick":"zacketizorcs2"}`), &banner); err != nil {
		t.Fatalf("Unmarshal old streamer banner plan: %v", err)
	}
	plan := DefaultEditPlan()
	plan.StreamerBanner = banner
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 5}}

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:     "source.mp4",
		OutputPath:     "out.mp4",
		BannerFontPath: `C:\Windows\Fonts\arialbd.ttf`,
	}, plan, plan.Clips[0])
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"vstack=inputs=2[content]",
		"color=c=0x9146ff:s=1080x96:r=60:d=5.000",
		"drawbox=x=0:y=0:w=116:h=96:color=0x5b1ba9:t=fill",
		`fontfile='C\:/Windows/Fonts/arialbd.ttf'`,
		"text='zacketizorcs2'",
		"fontsize=52",
		"overlay=x='0':y=720:eval=frame:eof_action=pass:shortest=0",
		"fps=60",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
}

func TestBuildFFmpegArgsPositionsStaticStreamerBanner(t *testing.T) {
	positionY := 0.5
	plan := DefaultEditPlan()
	plan.StreamerBanner = StreamerBannerPlan{Nick: "zacketizorcs2", PositionY: &positionY}
	clip := ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 5}

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:     "source.mp4",
		OutputPath:     "out.mp4",
		BannerFontPath: "font.ttf",
	}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "overlay=x='0':y=912:eval=frame") {
		t.Fatalf("args missing static banner at top pixel 912: %s", joined)
	}
}

func TestBuildFFmpegArgsDefaultsFullFrameBannerToTwentyPercent(t *testing.T) {
	plan := EditPlan{
		Variant:        VariantStreamerFullframeNoCam,
		GameplayCrop:   CropRect{X: 0, Y: 0, Width: 1, Height: 1},
		StreamerBanner: StreamerBannerPlan{Nick: "zacketizorcs2"},
	}
	clip := ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 5}

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:     "source.mp4",
		OutputPath:     "out.mp4",
		BannerFontPath: "font.ttf",
	}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "overlay=x='0':y=336:eval=frame") {
		t.Fatalf("args missing full-frame banner centered at 20%%: %s", joined)
	}
}

func TestBuildFFmpegArgsAnimatesStreamerBanner(t *testing.T) {
	tests := []struct {
		name     string
		duration float64
		wantX    string
	}{
		{
			name:     "normal clip uses fixed phase",
			duration: 5,
			wantX:    `overlay=x='if(lt(t\,0.350000)\,-w*(1-t/0.350000)\,if(lt(t\,4.650000)\,0\,-w*(t-4.650000)/0.350000))'`,
		},
		{
			name:     "short clip uses half duration",
			duration: 0.6,
			wantX:    `overlay=x='if(lt(t\,0.300000)\,-w*(1-t/0.300000)\,if(lt(t\,0.300000)\,0\,-w*(t-0.300000)/0.300000))'`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := DefaultEditPlan()
			plan.StreamerBanner = StreamerBannerPlan{Nick: "zacketizorcs2", SlideEnabled: true}
			clip := ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: tt.duration}

			args, err := BuildFFmpegArgs(FFmpegInputs{
				SourcePath:     "source.mp4",
				OutputPath:     "out.mp4",
				BannerFontPath: "font.ttf",
			}, plan, clip)
			if err != nil {
				t.Fatalf("BuildFFmpegArgs error = %v", err)
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, tt.wantX) {
				t.Fatalf("args missing animation expression %q: %s", tt.wantX, joined)
			}
		})
	}
}

func TestEditPlanValidatesStreamerBannerPosition(t *testing.T) {
	tests := []struct {
		name    string
		value   float64
		wantErr bool
	}{
		{name: "lower boundary", value: 0.025},
		{name: "upper boundary", value: 0.975},
		{name: "below boundary", value: 0.024999, wantErr: true},
		{name: "above boundary", value: 0.975001, wantErr: true},
		{name: "nan", value: math.NaN(), wantErr: true},
		{name: "positive infinity", value: math.Inf(1), wantErr: true},
		{name: "negative infinity", value: math.Inf(-1), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := DefaultEditPlan()
			plan.StreamerBanner.PositionY = &tt.value
			err := plan.Validate()
			if tt.wantErr && (err == nil || !strings.Contains(err.Error(), "position_y")) {
				t.Fatalf("Validate error = %v, want position_y error", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate error = %v, want nil", err)
			}
		})
	}
}

func TestBuildFFmpegArgsEmptyNickIgnoresBannerRenderingFields(t *testing.T) {
	positionY := 0.75
	plan := DefaultEditPlan()
	plan.StreamerBanner = StreamerBannerPlan{PositionY: &positionY, SlideEnabled: true}
	clip := ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 5}

	args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, unwanted := range []string{"color=c=0x9146ff", "drawtext=", "overlay="} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("empty streamer nick must not add %q: %s", unwanted, joined)
		}
	}
}

func TestBuildFFmpegArgsRejectsBannerWithoutFont(t *testing.T) {
	plan := DefaultEditPlan()
	plan.StreamerBanner = StreamerBannerPlan{Nick: "zacketizorcs2"}
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 5}}

	_, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}, plan, plan.Clips[0])
	if err == nil || !strings.Contains(err.Error(), "font path is required") {
		t.Fatalf("BuildFFmpegArgs error = %v, want banner font error", err)
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

func TestEditPlanNormalizesAndValidatesStreamerBannerNick(t *testing.T) {
	plan := DefaultEditPlan()
	plan.StreamerBanner = StreamerBannerPlan{Nick: "  zacketizorcs2  "}
	plan = NormalizeEditPlan(plan)
	if plan.StreamerBanner.Nick != "zacketizorcs2" {
		t.Fatalf("normalized nick = %q, want %q", plan.StreamerBanner.Nick, "zacketizorcs2")
	}
	if err := plan.Validate(); err != nil {
		t.Fatalf("Validate error = %v", err)
	}

	for _, nick := range []string{"nick with spaces", "@streamer", strings.Repeat("a", 26)} {
		plan.StreamerBanner.Nick = nick
		if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "streamer banner nick") {
			t.Fatalf("Validate nick %q error = %v, want streamer banner nick error", nick, err)
		}
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
