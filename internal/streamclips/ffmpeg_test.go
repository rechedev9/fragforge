package streamclips

import (
	"encoding/json"
	"math"
	"path/filepath"
	"slices"
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

func TestEditPlanValidationRequiresKillfeedCrop(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      12,
		KillfeedSeconds: []float64{11},
	}}

	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "killfeed_crop is not configured") {
		t.Fatalf("Validate error = %v, want missing killfeed_crop error", err)
	}
}

func TestEditPlanValidationRejectsInvalidKillfeedCues(t *testing.T) {
	tests := []struct {
		name    string
		cue     float64
		wantErr string
	}{
		{name: "start boundary accepted", cue: 10},
		{name: "finite cue inside range accepted", cue: 11.5},
		{name: "nan rejected", cue: math.NaN(), wantErr: "only finite values"},
		{name: "positive infinity rejected", cue: math.Inf(1), wantErr: "only finite values"},
		{name: "negative infinity rejected", cue: math.Inf(-1), wantErr: "only finite values"},
		{name: "before start rejected", cue: 9.999, wantErr: "start_seconds <= cue < end_seconds"},
		{name: "end boundary rejected", cue: 12, wantErr: "start_seconds <= cue < end_seconds"},
		{name: "after end rejected", cue: 12.001, wantErr: "start_seconds <= cue < end_seconds"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			killfeedCrop := CropRect{X: 0.75, Y: 0.05, Width: 0.2, Height: 0.1}
			plan := DefaultEditPlan()
			plan.KillfeedCrop = &killfeedCrop
			plan.Clips = []ClipRange{{
				ID:              "clip-001",
				StartSeconds:    10,
				EndSeconds:      12,
				KillfeedSeconds: []float64{tt.cue},
			}}

			err := plan.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestEditPlanValidationRequiresFiniteKillfeedCropCoordinates(t *testing.T) {
	coordinates := []struct {
		name string
		set  func(*CropRect, float64)
	}{
		{name: "x", set: func(crop *CropRect, value float64) { crop.X = value }},
		{name: "y", set: func(crop *CropRect, value float64) { crop.Y = value }},
		{name: "width", set: func(crop *CropRect, value float64) { crop.Width = value }},
		{name: "height", set: func(crop *CropRect, value float64) { crop.Height = value }},
	}
	nonFiniteValues := []struct {
		name  string
		value float64
	}{
		{name: "nan", value: math.NaN()},
		{name: "positive infinity", value: math.Inf(1)},
		{name: "negative infinity", value: math.Inf(-1)},
	}

	for _, coordinate := range coordinates {
		for _, nonFinite := range nonFiniteValues {
			t.Run(coordinate.name+"/"+nonFinite.name, func(t *testing.T) {
				killfeedCrop := CropRect{X: 0.75, Y: 0.05, Width: 0.2, Height: 0.1}
				coordinate.set(&killfeedCrop, nonFinite.value)
				plan := DefaultEditPlan()
				plan.KillfeedCrop = &killfeedCrop
				plan.Clips = []ClipRange{{
					ID:              "clip-001",
					StartSeconds:    10,
					EndSeconds:      12,
					KillfeedSeconds: []float64{11},
				}}

				if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "killfeed_crop must use finite normalized coordinates") {
					t.Fatalf("Validate error = %v, want finite killfeed_crop coordinates error", err)
				}
			})
		}
	}

	t.Run("finite coordinates accepted", func(t *testing.T) {
		killfeedCrop := CropRect{X: 0.75, Y: 0.05, Width: 0.2, Height: 0.1}
		plan := DefaultEditPlan()
		plan.KillfeedCrop = &killfeedCrop
		plan.Clips = []ClipRange{{
			ID:              "clip-001",
			StartSeconds:    10,
			EndSeconds:      12,
			KillfeedSeconds: []float64{11},
		}}

		if err := plan.Validate(); err != nil {
			t.Fatalf("Validate error = %v, want nil", err)
		}
	})
}

func TestNormalizeEditPlanSortsAndDeduplicatesKillfeedCuesWithoutMutatingCaller(t *testing.T) {
	distinctNearDuplicate := math.Nextafter(2, 3)
	callerCues := []float64{3, 1, 2, 1, distinctNearDuplicate, 2}
	originalCues := slices.Clone(callerCues)
	callerClips := []ClipRange{{
		ID:              " clip-001 ",
		StartSeconds:    1,
		EndSeconds:      4,
		KillfeedSeconds: callerCues,
	}}

	normalized := NormalizeEditPlan(EditPlan{Clips: callerClips})
	wantCues := []float64{1, 2, distinctNearDuplicate, 3}
	if normalized.Clips[0].ID != "clip-001" {
		t.Fatalf("normalized clip id = %q, want %q", normalized.Clips[0].ID, "clip-001")
	}
	if !slices.Equal(normalized.Clips[0].KillfeedSeconds, wantCues) {
		t.Fatalf("normalized killfeed cues = %v, want %v", normalized.Clips[0].KillfeedSeconds, wantCues)
	}
	if callerClips[0].ID != " clip-001 " {
		t.Fatalf("caller clip id = %q after normalization, want unchanged", callerClips[0].ID)
	}
	if !slices.Equal(callerCues, originalCues) {
		t.Fatalf("caller cue backing array = %v after normalization, want %v", callerCues, originalCues)
	}

	normalized.Clips[0].ID = "changed"
	normalized.Clips[0].KillfeedSeconds[0] = 99
	if callerClips[0].ID != " clip-001 " {
		t.Fatalf("caller clip backing array changed through normalized result: %+v", callerClips)
	}
	if !slices.Equal(callerCues, originalCues) {
		t.Fatalf("caller cue backing array changed through normalized result: %v", callerCues)
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

func TestBuildFFmpegArgsBuildsKillfeedCueGraphs(t *testing.T) {
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	clip := ClipRange{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      15,
		KillfeedSeconds: []float64{12, 11},
	}
	killfeedRows := [][]NoticeRow{
		{
			{X: 1621, Y: 73, Width: 288, Height: 37},
			{X: 1469, Y: 103, Width: 440, Height: 40},
		},
		nil,
	}
	tests := []struct {
		name       string
		plan       EditPlan
		wantFilter string
	}{
		{
			name: "stacked graph",
			plan: DefaultEditPlan(),
			wantFilter: strings.Join([]string{
				`[0:v]split=4[facein][gamein][killfeedin0_0][killfeedin0_1]`,
				`[facein]crop=w=iw*0.250000:h=ih*0.300000:x=iw*0.000000:y=ih*0.000000,scale=1080:768:force_original_aspect_ratio=increase,crop=1080:768[face]`,
				`[gamein]crop=w=iw*1.000000:h=ih*1.000000:x=iw*0.000000:y=ih*0.000000,scale=1080:1152:force_original_aspect_ratio=increase,crop=1080:1152[game]`,
				`[face][game]vstack=inputs=2[layout]`,
				`[killfeedin0_0]trim=start=1.000000,select='eq(n\,0)',setpts=PTS-STARTPTS,crop=288:37:1621:73,scale=288:-2:flags=lanczos,tpad=stop_mode=clone:stop_duration=5.000000[killfeed0_0]`,
				`[killfeedin0_1]trim=start=1.000000,select='eq(n\,0)',setpts=PTS-STARTPTS,crop=440:40:1469:103,scale=440:-2:flags=lanczos,tpad=stop_mode=clone:stop_duration=5.000000[killfeed0_1]`,
				`[layout][killfeed0_0]overlay=x=W-w-24:y=840:enable='between(t\,0.650000\,3.800000)':eof_action=pass:shortest=0[killfeeded0_0]`,
				`[killfeeded0_0][killfeed0_1]overlay=x=W-w-24:y=870:enable='between(t\,0.650000\,3.800000)':eof_action=pass:shortest=0[content]`,
				`[content]fps=60,format=yuv420p[v]`,
			}, ";"),
		},
		{
			name: "fullframe graph",
			plan: EditPlan{
				Variant:      VariantStreamerFullframeNoCam,
				GameplayCrop: CropRect{X: 0, Y: 0, Width: 1, Height: 1},
			},
			wantFilter: strings.Join([]string{
				`[0:v]split=3[layoutin][killfeedin0_0][killfeedin0_1]`,
				`[layoutin]crop=w=iw*1.000000:h=ih*1.000000:x=iw*0.000000:y=ih*0.000000,scale=1080:1920:force_original_aspect_ratio=increase,crop=1080:1920[layout]`,
				`[killfeedin0_0]trim=start=1.000000,select='eq(n\,0)',setpts=PTS-STARTPTS,crop=288:37:1621:73,scale=288:-2:flags=lanczos,tpad=stop_mode=clone:stop_duration=5.000000[killfeed0_0]`,
				`[killfeedin0_1]trim=start=1.000000,select='eq(n\,0)',setpts=PTS-STARTPTS,crop=440:40:1469:103,scale=440:-2:flags=lanczos,tpad=stop_mode=clone:stop_duration=5.000000[killfeed0_1]`,
				`[layout][killfeed0_0]overlay=x=W-w-24:y=64:enable='between(t\,0.650000\,3.800000)':eof_action=pass:shortest=0[killfeeded0_0]`,
				`[killfeeded0_0][killfeed0_1]overlay=x=W-w-24:y=94:enable='between(t\,0.650000\,3.800000)':eof_action=pass:shortest=0[content]`,
				`[content]fps=60,format=yuv420p[v]`,
			}, ";"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := tt.plan
			plan.KillfeedCrop = &killfeedCrop
			args, err := BuildFFmpegArgs(
				FFmpegInputs{
					SourcePath:   "source.mp4",
					OutputPath:   "out.mp4",
					KillfeedRows: killfeedRows,
				},
				plan,
				clip,
			)
			if err != nil {
				t.Fatalf("BuildFFmpegArgs error = %v", err)
			}

			got := filterComplexArg(t, args)
			if got != tt.wantFilter {
				t.Fatalf("filter_complex mismatch\n got: %s\nwant: %s", got, tt.wantFilter)
			}
			if strings.Contains(got, "curves=") {
				t.Fatalf("killfeed graph darkens cue branches: %s", got)
			}
		})
	}
}

func TestBuildFFmpegArgsOmitsCuesWithoutDetectedRows(t *testing.T) {
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &killfeedCrop
	clip := ClipRange{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      15,
		KillfeedSeconds: []float64{11, 12},
	}
	inputs := FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}

	args, err := BuildFFmpegArgs(inputs, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs with undetected cues error = %v", err)
	}
	clip.KillfeedSeconds = nil
	standardArgs, err := BuildFFmpegArgs(inputs, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs standard graph error = %v", err)
	}
	if !slices.Equal(args, standardArgs) {
		t.Fatalf("undetected cues changed the standard graph\n got: %q\nwant: %q", args, standardArgs)
	}
	filter := filterComplexArg(t, args)
	for _, forbidden := range []string{"killfeedin", "scale=620", "curves="} {
		if strings.Contains(filter, forbidden) {
			t.Fatalf("undetected cue graph contains %q: %s", forbidden, filter)
		}
	}
}

func TestBuildFFmpegArgsWithoutKillfeedCuesIsByteEquivalentToLegacyPath(t *testing.T) {
	clip := ClipRange{ID: "clip-001", StartSeconds: 1.5, EndSeconds: 4.25}
	legacyPlan := DefaultEditPlan()
	inputs := FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}

	legacyArgs, err := BuildFFmpegArgs(inputs, legacyPlan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs legacy error = %v", err)
	}

	planWithUnusedKillfeedCrop := legacyPlan
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	planWithUnusedKillfeedCrop.KillfeedCrop = &killfeedCrop
	argsWithUnusedKillfeedCrop, err := BuildFFmpegArgs(inputs, planWithUnusedKillfeedCrop, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs with unused killfeed crop error = %v", err)
	}

	if !slices.Equal(argsWithUnusedKillfeedCrop, legacyArgs) {
		t.Fatalf("no-cue args changed\n got: %q\nwant: %q", argsWithUnusedKillfeedCrop, legacyArgs)
	}
}

func filterComplexArg(t *testing.T, args []string) string {
	t.Helper()
	for i, arg := range args {
		if arg != "-filter_complex" {
			continue
		}
		if i+1 >= len(args) {
			t.Fatal("-filter_complex is missing its value")
		}
		return args[i+1]
	}
	t.Fatal("args are missing -filter_complex")
	return ""
}

func TestBuildFFmpegArgsOldStreamerBannerPlanUsesCurrentLayoutDefault(t *testing.T) {
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
		"overlay=x='0':y=670:eval=frame:eof_action=pass:shortest=0",
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

func TestBuildFFmpegArgsDefaultsLegacyBannerToStackSeam(t *testing.T) {
	layout, ok := VariantByName(VariantStreamerVerticalStack)
	if !ok {
		t.Fatal("legacy layout is not registered")
	}
	plan := DefaultEditPlan()
	plan.Variant = layout.Name
	plan.FaceCrop = layout.DefaultFaceCrop
	plan.GameplayCrop = layout.DefaultGameplayCrop
	plan.StreamerBanner = StreamerBannerPlan{Nick: "zacketizorcs2"}
	plan.Clips = []ClipRange{{ID: "one", StartSeconds: 0, EndSeconds: 5}}
	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:     "source.mp4",
		OutputPath:     "out.mp4",
		BannerFontPath: "font.ttf",
	}, plan, plan.Clips[0])
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "overlay=x='0':y=472:eval=frame") {
		t.Fatalf("args missing legacy banner centered at the 520px seam: %s", joined)
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
