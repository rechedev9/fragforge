package streamclips

import (
	"encoding/json"
	"fmt"
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

func TestNormalizeEditPlanSortsKillfeedKillsWithTheirCues(t *testing.T) {
	first := validKill()
	first.VictimName = "first"
	second := validKill()
	second.VictimName = "second"
	plan := EditPlan{Clips: []ClipRange{{
		ID:              "clip-001",
		StartSeconds:    0,
		EndSeconds:      10,
		KillfeedSeconds: []float64{8, 2},
		KillfeedKills:   [][]KillfeedKill{{second}, {first}},
	}}}

	normalized := NormalizeEditPlan(plan)
	got := normalized.Clips[0]
	if !slices.Equal(got.KillfeedSeconds, []float64{2, 8}) {
		t.Fatalf("killfeed cues = %v, want [2 8]", got.KillfeedSeconds)
	}
	if got.KillfeedKills[0][0].VictimName != "first" || got.KillfeedKills[1][0].VictimName != "second" {
		t.Fatalf("killfeed kills = %+v, want first/second aligned with sorted cues", got.KillfeedKills)
	}
}

func TestNormalizeEditPlanMergesKillsAtDuplicateCues(t *testing.T) {
	first := validKill()
	first.VictimName = "first"
	second := validKill()
	second.VictimName = "second"
	plan := EditPlan{Clips: []ClipRange{{
		ID:              "clip-001",
		StartSeconds:    0,
		EndSeconds:      10,
		KillfeedSeconds: []float64{2, 2, 2},
		KillfeedKills:   [][]KillfeedKill{{first}, {second}, {first}},
	}}}

	got := NormalizeEditPlan(plan).Clips[0]
	if !slices.Equal(got.KillfeedSeconds, []float64{2}) {
		t.Fatalf("killfeed cues = %v, want [2]", got.KillfeedSeconds)
	}
	if len(got.KillfeedKills) != 1 || !slices.Equal(got.KillfeedKills[0], []KillfeedKill{first, second}) {
		t.Fatalf("killfeed kills = %+v, want both unique kills at the duplicate cue", got.KillfeedKills)
	}
}

func TestNormalizeEditPlanMigratesLegacyCumulativeKillfeedSnapshots(t *testing.T) {
	first := validKill()
	first.VictimName = "first"
	second := validKill()
	second.VictimName = "second"
	third := validKill()
	third.VictimName = "third"
	plan := EditPlan{
		SchemaVersion: "1.0",
		Clips: []ClipRange{{
			ID:              "clip-001",
			StartSeconds:    0,
			EndSeconds:      10,
			KillfeedSeconds: []float64{2, 3, 4, 5},
			KillfeedKills: [][]KillfeedKill{
				{first},
				{}, // unresolved cue must not reset the observed snapshot
				{first, second},
				{second, third},
			},
		}},
	}

	got := NormalizeEditPlan(plan)
	if got.SchemaVersion != EditPlanSchemaVersion {
		t.Fatalf("schema version = %q, want %q", got.SchemaVersion, EditPlanSchemaVersion)
	}
	want := [][]KillfeedKill{{first}, nil, {second}, {third}}
	if !slices.EqualFunc(got.Clips[0].KillfeedKills, want, slices.Equal) {
		t.Fatalf("killfeed events = %+v, want %+v", got.Clips[0].KillfeedKills, want)
	}
	if len(plan.Clips[0].KillfeedKills[2]) != 2 {
		t.Fatalf("caller snapshots mutated: %+v", plan.Clips[0].KillfeedKills)
	}
}

func TestEditPlanValidateForSourceDurationRejectsOverrun(t *testing.T) {
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{{ID: "clip-001", StartSeconds: 0, EndSeconds: 20}}

	err := plan.ValidateForSourceDuration(15.15)
	if err == nil || !strings.Contains(err.Error(), "exceeds source duration 15.150") {
		t.Fatalf("ValidateForSourceDuration error = %v, want source-duration overrun", err)
	}

	plan.Clips[0].EndSeconds = 15.15
	if err := plan.ValidateForSourceDuration(15.15); err != nil {
		t.Fatalf("ValidateForSourceDuration exact bound error = %v", err)
	}
}

func TestMigrateLegacySourceDurationOnlyFitsHistoricalTwentySecondDefault(t *testing.T) {
	kill := KillfeedKill{AttackerSide: "CT", AttackerName: "a", VictimSide: "T", VictimName: "b", Weapon: "awp"}
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &CropRect{X: 0.8, Y: 0, Width: 0.2, Height: 0.2}
	plan.Clips = []ClipRange{
		{
			ID:              "legacy",
			StartSeconds:    0,
			EndSeconds:      20,
			KillfeedSeconds: []float64{2.5, 16},
			KillfeedKills:   [][]KillfeedKill{{kill}, {kill}},
		},
		{ID: "outside", StartSeconds: 18, EndSeconds: 20},
	}

	got, changed := MigrateLegacySourceDuration(plan, 15.15)
	if !changed {
		t.Fatal("MigrateLegacySourceDuration changed = false, want true")
	}
	if len(got.Clips) != 1 || got.Clips[0].EndSeconds != 15.15 {
		t.Fatalf("migrated clips = %+v, want one clip ending at 15.15", got.Clips)
	}
	if !slices.Equal(got.Clips[0].KillfeedSeconds, []float64{2.5}) || len(got.Clips[0].KillfeedKills) != 1 {
		t.Fatalf("migrated killfeed = %v / %+v, want only in-range cue and kills", got.Clips[0].KillfeedSeconds, got.Clips[0].KillfeedKills)
	}
	if plan.Clips[0].EndSeconds != 20 || len(plan.Clips[0].KillfeedSeconds) != 2 {
		t.Fatalf("caller mutated: %+v", plan.Clips[0])
	}
	if err := got.ValidateForSourceDuration(15.15); err != nil {
		t.Fatalf("migrated plan validation error = %v", err)
	}

	custom := DefaultEditPlan()
	custom.Clips = []ClipRange{{ID: "custom", StartSeconds: 0, EndSeconds: 19}}
	unchanged, changed := MigrateLegacySourceDuration(custom, 15.15)
	if changed || unchanged.Clips[0].EndSeconds != 19 {
		t.Fatalf("custom overrun was migrated: changed=%v plan=%+v", changed, unchanged)
	}
	if err := unchanged.ValidateForSourceDuration(15.15); err == nil {
		t.Fatal("custom overrun validation succeeded, want strict rejection")
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

// stackedLayoutBranches are the split/scale/vstack lines the default 40/60
// stacked variant emits before any killfeed overlay chain.
var stackedLayoutBranches = []string{
	`[facein]crop=w=iw*0.250000:h=ih*0.300000:x=iw*0.000000:y=ih*0.000000,scale=1080:768:force_original_aspect_ratio=increase,crop=1080:768[face]`,
	`[gamein]crop=w=iw*1.000000:h=ih*1.000000:x=iw*0.000000:y=ih*0.000000,scale=1080:1152:force_original_aspect_ratio=increase,crop=1080:1152[game]`,
	`[face][game]vstack=inputs=2[layout]`,
}

func TestBuildFFmpegArgsBuildsNoticeOverlayGraph(t *testing.T) {
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	clip := ClipRange{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      15,
		KillfeedSeconds: []float64{11},
	}
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &killfeedCrop

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:          "source.mp4",
		OutputPath:          "out.mp4",
		KillfeedNoticePaths: [][]string{{"n0.png"}},
	}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}

	slide := killfeedSlideX(1)
	wantFilter := strings.Join(append(
		append([]string{`[0:v]split=2[facein][gamein]`}, stackedLayoutBranches...),
		`[1:v]format=rgba,setpts=PTS-STARTPTS,fade=t=out:st=3.450000:d=0.350000:alpha=1,split=2[nsharp0_0][nblurpre0_0]`,
		`[nblurpre0_0]gblur=sigma=24:sigmaV=0[nblur0_0]`,
		fmt.Sprintf(`[layout][nblur0_0]overlay=x='%s':y=1044:eval=frame:enable='between(t\,1.000000\,1.080000)':eof_action=pass:shortest=0[kfover0]`, slide),
		fmt.Sprintf(`[kfover0][nsharp0_0]overlay=x='%s':y=1044:eval=frame:enable='between(t\,1.080000\,3.800000)':eof_action=pass:shortest=0[content]`, slide),
		`[content]fps=60,format=yuv420p[v]`,
	), ";")
	if got := filterComplexArg(t, args); got != wantFilter {
		t.Fatalf("filter_complex mismatch\n got: %s\nwant: %s", got, wantFilter)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-loop 1 -i n0.png") {
		t.Fatalf("notice PNG must be a looped input: %s", joined)
	}
}

func TestBuildFFmpegArgsNoticeInputIndexShiftsAfterMusic(t *testing.T) {
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	clip := ClipRange{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      15,
		KillfeedSeconds: []float64{11},
	}
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &killfeedCrop
	plan.Music = MusicPlan{Key: "concrete-teeth"}

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:          "source.mp4",
		OutputPath:          "out.mp4",
		MusicPath:           "music/concrete-teeth.mp3",
		SourceHasAudio:      true,
		KillfeedNoticePaths: [][]string{{"n0.png"}},
	}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	filter := filterComplexArg(t, args)
	// Source is input 0 and music is input 1, so the notice PNG is input 2.
	if !strings.Contains(filter, `[2:v]format=rgba,setpts=PTS-STARTPTS,fade=t=out:st=3.450000:d=0.350000:alpha=1,split=2[nsharp0_0][nblurpre0_0]`) {
		t.Fatalf("notice input index did not shift past music: %s", filter)
	}
	if strings.Contains(filter, `[1:v]format=rgba`) {
		t.Fatalf("notice must not reuse the music input index: %s", filter)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-i music/concrete-teeth.mp3 -loop 1 -i n0.png") {
		t.Fatalf("notice input must follow the music input: %s", joined)
	}
}

func TestBuildFFmpegArgsMixesNoticesAndFrozenCropFallback(t *testing.T) {
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	clip := ClipRange{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      15,
		KillfeedSeconds: []float64{11, 12},
	}
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &killfeedCrop

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:          "source.mp4",
		OutputPath:          "out.mp4",
		KillfeedNoticePaths: [][]string{{"n0.png", "n1.png"}, nil},
	}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}

	slide1 := killfeedSlideX(1)
	slide2 := killfeedSlideX(2)
	wantFilter := strings.Join(append(
		append([]string{`[0:v]split=3[facein][gamein][killfeedin1]`}, stackedLayoutBranches...),
		`[1:v]format=rgba,setpts=PTS-STARTPTS,fade=t=out:st=3.450000:d=0.350000:alpha=1,split=2[nsharp0_0][nblurpre0_0]`,
		`[nblurpre0_0]gblur=sigma=24:sigmaV=0[nblur0_0]`,
		`[2:v]format=rgba,setpts=PTS-STARTPTS,fade=t=out:st=3.450000:d=0.350000:alpha=1,split=2[nsharp0_1][nblurpre0_1]`,
		`[nblurpre0_1]gblur=sigma=24:sigmaV=0[nblur0_1]`,
		`[killfeedin1]trim=start=2.350000,select='eq(n\,0)',setpts=PTS-STARTPTS,crop=w=iw*0.175000:h=ih*0.100000:x=iw*0.800000:y=ih*0.050000,scale=930:-2:flags=lanczos,tpad=stop_mode=clone:stop_duration=5.000000,fade=t=out:st=4.450000:d=0.350000:alpha=1,split=2[kfsharp1][kfblurpre1]`,
		`[kfblurpre1]gblur=sigma=24:sigmaV=0[kfblur1]`,
		fmt.Sprintf(`[layout][nblur0_0]overlay=x='%s':y=1044:eval=frame:enable='between(t\,1.000000\,1.080000)':eof_action=pass:shortest=0[kfover0]`, slide1),
		fmt.Sprintf(`[kfover0][nsharp0_0]overlay=x='%s':y=1044:eval=frame:enable='between(t\,1.080000\,3.800000)':eof_action=pass:shortest=0[kfover1]`, slide1),
		fmt.Sprintf(`[kfover1][nblur0_1]overlay=x='%s':y=1044-80*(between(t\,1.000000\,3.800000)):eval=frame:enable='between(t\,1.000000\,1.080000)':eof_action=pass:shortest=0[kfover2]`, slide1),
		fmt.Sprintf(`[kfover2][nsharp0_1]overlay=x='%s':y=1044-80*(between(t\,1.000000\,3.800000)):eval=frame:enable='between(t\,1.080000\,3.800000)':eof_action=pass:shortest=0[kfover3]`, slide1),
		fmt.Sprintf(`[kfover3][kfblur1]overlay=x='%s':y=1044:eval=frame:enable='between(t\,2.000000\,2.080000)':eof_action=pass:shortest=0[kfover4]`, slide2),
		fmt.Sprintf(`[kfover4][kfsharp1]overlay=x='%s':y=1044:eval=frame:enable='between(t\,2.080000\,4.800000)':eof_action=pass:shortest=0[content]`, slide2),
		`[content]fps=60,format=yuv420p[v]`,
	), ";")
	if got := filterComplexArg(t, args); got != wantFilter {
		t.Fatalf("filter_complex mismatch\n got: %s\nwant: %s", got, wantFilter)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-loop 1 -i n0.png -loop 1 -i n1.png") {
		t.Fatalf("both notice PNGs must be looped inputs in order: %s", joined)
	}
}

func TestBuildFFmpegArgsReflowsOverlappingNoticeEvents(t *testing.T) {
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	clip := ClipRange{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      15,
		KillfeedSeconds: []float64{11, 11.125, 14},
	}
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &killfeedCrop

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:          "source.mp4",
		OutputPath:          "out.mp4",
		KillfeedNoticePaths: [][]string{{"n0.png"}, {"n1.png"}, {"n2.png"}},
	}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}

	filter := filterComplexArg(t, args)
	// The first notice sits at baseY; the second, overlapping it, reflows one slot
	// UP (80 = KillfeedNoticeHeight+gap) so the caption band below baseY stays
	// clear; the third, after both clear, is back at baseY. Each notice
	// contributes a blurred slide op then a sharp settle op.
	wants := []string{
		fmt.Sprintf(`[layout][nblur0_0]overlay=x='%s':y=1044:eval=frame:enable='between(t\,1.000000\,1.080000)'`, killfeedSlideX(1)),
		fmt.Sprintf(`[kfover0][nsharp0_0]overlay=x='%s':y=1044:eval=frame:enable='between(t\,1.080000\,3.800000)'`, killfeedSlideX(1)),
		fmt.Sprintf(`[kfover1][nblur1_0]overlay=x='%s':y=1044-80*(between(t\,1.000000\,3.800000)):eval=frame:enable='between(t\,1.125000\,1.205000)'`, killfeedSlideX(1.125)),
		fmt.Sprintf(`[kfover2][nsharp1_0]overlay=x='%s':y=1044-80*(between(t\,1.000000\,3.800000)):eval=frame:enable='between(t\,1.205000\,3.925000)'`, killfeedSlideX(1.125)),
		fmt.Sprintf(`[kfover3][nblur2_0]overlay=x='%s':y=1044:eval=frame:enable='between(t\,4.000000\,4.080000)'`, killfeedSlideX(4)),
	}
	for _, want := range wants {
		if !strings.Contains(filter, want) {
			t.Errorf("filter_complex missing %q: %s", want, filter)
		}
	}
}

func TestBuildFFmpegArgsSuppressesDegenerateNoticeEntrance(t *testing.T) {
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	// The cue lands at duration-0.05, so its window (0.05s) is shorter than the
	// slide+settle (0.12s). Sliding there would render only blurred mid-slide
	// frames before the clip cuts, so the entrance is skipped: one sharp op holds
	// the notice at center for the whole window.
	clip := ClipRange{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      15,
		KillfeedSeconds: []float64{14.95},
	}
	plan := EditPlan{
		Variant:      VariantStreamerFullframeNoCam,
		GameplayCrop: CropRect{X: 0, Y: 0, Width: 1, Height: 1},
		KillfeedCrop: &killfeedCrop,
	}

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:          "source.mp4",
		OutputPath:          "out.mp4",
		KillfeedNoticePaths: [][]string{{"n0.png"}},
	}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}

	wantFilter := strings.Join([]string{
		`[0:v]crop=w=iw*1.000000:h=ih*1.000000:x=iw*0.000000:y=ih*0.000000,scale=1080:1920:force_original_aspect_ratio=increase,crop=1080:1920[layout]`,
		`[1:v]format=rgba,setpts=PTS-STARTPTS,fade=t=out:st=4.950000:d=0.050000:alpha=1[nsharp0_0]`,
		`[layout][nsharp0_0]overlay=x='(W-w)/2':y=461:eval=frame:enable='between(t\,4.950000\,5.000000)':eof_action=pass:shortest=0[content]`,
		`[content]fps=60,format=yuv420p[v]`,
	}, ";")
	got := filterComplexArg(t, args)
	if got != wantFilter {
		t.Fatalf("filter_complex mismatch\n got: %s\nwant: %s", got, wantFilter)
	}
	// A suppressed entrance emits no blur variant and no per-notice split.
	for _, forbidden := range []string{"nblur", "gblur", "split=2["} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("suppressed entrance must not emit %q: %s", forbidden, got)
		}
	}
	// The x is the static center, never the slide expression.
	if strings.Contains(got, "if(lt(t") {
		t.Fatalf("suppressed entrance must hold a static centered x, not slide: %s", got)
	}
}

func TestKillfeedStackYStacksUpwardAboveBaseY(t *testing.T) {
	const baseY = 1044
	// No live predecessor: the notice holds baseY.
	if got := killfeedStackY(baseY, 1, 3.8, nil); got != "1044" {
		t.Fatalf("killfeedStackY with no predecessor = %q, want %q", got, "1044")
	}
	// One live predecessor: slot 1 sits one step ABOVE baseY (baseY-80), keeping
	// the caption band below baseY clear.
	prior := []noticeLifetime{{start: 1, end: 3.8}}
	want := "1044-80*(between(t\\,1.000000\\,3.800000))"
	if got := killfeedStackY(baseY, 1.1, 3.9, prior); got != want {
		t.Fatalf("killfeedStackY slot 1 = %q, want %q (baseY-80)", got, want)
	}
}

func TestBuildFFmpegArgsFrozenCropFallbackGeometry(t *testing.T) {
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	clip := ClipRange{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      15,
		KillfeedSeconds: []float64{11},
	}
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &killfeedCrop

	// No notice paths: every configured cue falls back to a frozen crop strip.
	args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}

	slide := killfeedSlideX(1)
	wantFilter := strings.Join(append(
		append([]string{`[0:v]split=3[facein][gamein][killfeedin0]`}, stackedLayoutBranches...),
		`[killfeedin0]trim=start=1.350000,select='eq(n\,0)',setpts=PTS-STARTPTS,crop=w=iw*0.175000:h=ih*0.100000:x=iw*0.800000:y=ih*0.050000,scale=930:-2:flags=lanczos,tpad=stop_mode=clone:stop_duration=5.000000,fade=t=out:st=3.450000:d=0.350000:alpha=1,split=2[kfsharp0][kfblurpre0]`,
		`[kfblurpre0]gblur=sigma=24:sigmaV=0[kfblur0]`,
		fmt.Sprintf(`[layout][kfblur0]overlay=x='%s':y=1044:eval=frame:enable='between(t\,1.000000\,1.080000)':eof_action=pass:shortest=0[kfover0]`, slide),
		fmt.Sprintf(`[kfover0][kfsharp0]overlay=x='%s':y=1044:eval=frame:enable='between(t\,1.080000\,3.800000)':eof_action=pass:shortest=0[content]`, slide),
		`[content]fps=60,format=yuv420p[v]`,
	), ";")
	if got := filterComplexArg(t, args); got != wantFilter {
		t.Fatalf("filter_complex mismatch\n got: %s\nwant: %s", got, wantFilter)
	}
	if joined := strings.Join(args, " "); strings.Contains(joined, "-loop 1 -i") {
		t.Fatalf("frozen-crop fallback must not add looped notice inputs: %s", joined)
	}
}

// A frozen killfeed strip must be cropped from the same delayed frame the
// vision reader reads (cue + KillfeedSampleDelaySeconds), not from the cue
// frame itself: on a real three-kill AWP burst the newest notice had not been
// drawn yet at the cue, so freezing there rendered a strip missing that kill.
func TestBuildFFmpegArgsFreezesKillfeedAfterNoticeIsDrawn(t *testing.T) {
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &killfeedCrop

	tests := []struct {
		name  string
		clip  ClipRange
		want  string
		trail string
	}{
		{
			name: "cue mid clip samples the delayed frame",
			clip: ClipRange{ID: "clip-001", StartSeconds: 10, EndSeconds: 20, KillfeedSeconds: []float64{18.45}},
			want: "trim=start=8.800000",
		},
		{
			name: "cue near the clip end stays on a real frame",
			clip: ClipRange{ID: "clip-002", StartSeconds: 0, EndSeconds: 5, KillfeedSeconds: []float64{4.9}},
			want: "trim=start=4.950000",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}, plan, tt.clip)
			if err != nil {
				t.Fatalf("BuildFFmpegArgs error = %v", err)
			}
			got := filterComplexArg(t, args)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("filter_complex must freeze at %q\n got: %s", tt.want, got)
			}
		})
	}
}

func TestKillfeedFreezeOffsetNeverTrimsPastTheClip(t *testing.T) {
	tests := []struct {
		name     string
		relative float64
		duration float64
		want     float64
	}{
		{name: "mid clip adds the sample delay", relative: 8.45, duration: 15.15, want: 8.8},
		{name: "start of clip adds the sample delay", relative: 0, duration: 10, want: 0.35},
		{name: "delay would overshoot the end", relative: 9.9, duration: 10, want: 9.95},
		{name: "cue already past the guard never rewinds", relative: 9.99, duration: 10, want: 9.99},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := killfeedFreezeOffset(tt.relative, tt.duration)
			if math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("killfeedFreezeOffset(%v, %v) = %v, want %v", tt.relative, tt.duration, got, tt.want)
			}
			if got > tt.duration {
				t.Fatalf("killfeedFreezeOffset(%v, %v) = %v, which trims past the clip", tt.relative, tt.duration, got)
			}
			if got < tt.relative {
				t.Fatalf("killfeedFreezeOffset(%v, %v) = %v, which rewinds before the cue", tt.relative, tt.duration, got)
			}
		})
	}
}

func TestKillfeedSampleSecondsSeparatesReadDelayFromCueTiming(t *testing.T) {
	tests := []struct {
		name    string
		cue     float64
		clipEnd float64
		want    float64
	}{
		{name: "normal cue samples after the kill", cue: 7.58, clipEnd: 15.15, want: 7.93},
		{name: "near-end cue stays inside its clip", cue: 9.9, clipEnd: 10, want: 9.95},
		{name: "cue inside the end guard never rewinds", cue: 9.99, clipEnd: 10, want: 9.99},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := KillfeedSampleSeconds(tt.cue, tt.clipEnd); math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("KillfeedSampleSeconds(%v, %v) = %v, want %v", tt.cue, tt.clipEnd, got, tt.want)
			}
		})
	}
}

func TestBuildFFmpegArgsFullframeNoticeGraphOmitsDegenerateSplit(t *testing.T) {
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	clip := ClipRange{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      15,
		KillfeedSeconds: []float64{11},
	}
	plan := EditPlan{
		Variant:      VariantStreamerFullframeNoCam,
		GameplayCrop: CropRect{X: 0, Y: 0, Width: 1, Height: 1},
		KillfeedCrop: &killfeedCrop,
	}

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:          "source.mp4",
		OutputPath:          "out.mp4",
		KillfeedNoticePaths: [][]string{{"n0.png"}},
	}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}

	slide := killfeedSlideX(1)
	wantFilter := strings.Join([]string{
		`[0:v]crop=w=iw*1.000000:h=ih*1.000000:x=iw*0.000000:y=ih*0.000000,scale=1080:1920:force_original_aspect_ratio=increase,crop=1080:1920[layout]`,
		`[1:v]format=rgba,setpts=PTS-STARTPTS,fade=t=out:st=3.450000:d=0.350000:alpha=1,split=2[nsharp0_0][nblurpre0_0]`,
		`[nblurpre0_0]gblur=sigma=24:sigmaV=0[nblur0_0]`,
		fmt.Sprintf(`[layout][nblur0_0]overlay=x='%s':y=461:eval=frame:enable='between(t\,1.000000\,1.080000)':eof_action=pass:shortest=0[kfover0]`, slide),
		fmt.Sprintf(`[kfover0][nsharp0_0]overlay=x='%s':y=461:eval=frame:enable='between(t\,1.080000\,3.800000)':eof_action=pass:shortest=0[content]`, slide),
		`[content]fps=60,format=yuv420p[v]`,
	}, ";")
	got := filterComplexArg(t, args)
	if got != wantFilter {
		t.Fatalf("filter_complex mismatch\n got: %s\nwant: %s", got, wantFilter)
	}
	// The fullframe layout must not split the source: the only split is the
	// per-notice sharp/blur split, never a degenerate [0:v] layout split.
	if strings.Contains(got, "[0:v]split") {
		t.Fatalf("fullframe notice graph must not emit a degenerate layout split: %s", got)
	}
}

func TestKillfeedBaseYSitsAtGameplayBandFraction(t *testing.T) {
	tests := []struct {
		name    string
		variant string
		want    int
	}{
		// 24% down the gameplay band. Facecam bands offset by the facecam height.
		{name: "facecam 40/60", variant: VariantStreamer4060, want: 768 + 276},         // round(0.24*1152)
		{name: "legacy stack", variant: VariantStreamerVerticalStack, want: 520 + 336}, // round(0.24*1400)
		{name: "fullframe", variant: VariantStreamerFullframeNoCam, want: 461},         // round(0.24*1920)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout, ok := VariantByName(tt.variant)
			if !ok {
				t.Fatalf("variant %q not registered", tt.variant)
			}
			if got := killfeedBaseY(layout); got != tt.want {
				t.Fatalf("killfeedBaseY(%s) = %d, want %d", tt.variant, got, tt.want)
			}
		})
	}
}

func TestKillfeedSlideXEasesFromRightEdgeToCenterWithOvershoot(t *testing.T) {
	got := killfeedSlideX(1)
	// Pin the whole expression as a literal so a change to the ease curve (the
	// quadratic ease-out factor) or the settle term is caught here rather than
	// silently tracking killfeedSlideX itself. Independent of notice width: it
	// centers via overlay's W and w, eases out from the right edge to 12px past
	// center over 0.08s, then settles linearly back over 0.04s and holds.
	// Commas are escaped for the filtergraph.
	want := `if(lt(t\,1.080000)\,W+(((W-w)/2-12)-W)*(1-(1-(t-1.000000)/0.080000)*(1-(t-1.000000)/0.080000))\,if(lt(t\,1.120000)\,(W-w)/2-12*(1-(t-1.080000)/0.040000)\,(W-w)/2))`
	if got != want {
		t.Fatalf("killfeedSlideX(1) mismatch\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(got, ",") && !strings.Contains(got, `\,`) {
		t.Fatalf("killfeedSlideX must escape commas for the filtergraph: %s", got)
	}
}

func TestKillfeedFadeFilterShortensForShortWindows(t *testing.T) {
	// A full-length window fades over the fixed tail.
	if got := killfeedFadeFilter(1, 3.8); got != "fade=t=out:st=3.450000:d=0.350000:alpha=1" {
		t.Fatalf("full-window fade = %q", got)
	}
	// A window shorter than the fade shrinks the fade so it always fits.
	if got := killfeedFadeFilter(1, 1.2); got != "fade=t=out:st=1.000000:d=0.200000:alpha=1" {
		t.Fatalf("short-window fade = %q", got)
	}
}

func TestBuildFFmpegArgsRejectsMismatchedNoticePaths(t *testing.T) {
	killfeedCrop := CropRect{X: 0.8, Y: 0.05, Width: 0.175, Height: 0.1}
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &killfeedCrop
	clip := ClipRange{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      15,
		KillfeedSeconds: []float64{11, 12},
	}

	_, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:          "source.mp4",
		OutputPath:          "out.mp4",
		KillfeedNoticePaths: [][]string{{"n0.png"}},
	}, plan, clip)
	if err == nil || !strings.Contains(err.Error(), "killfeed notice paths") {
		t.Fatalf("BuildFFmpegArgs error = %v, want mismatched notice paths error", err)
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

func TestBuildFFmpegArgsLandscapePreservesSourceKillfeedAndUsesCompactLowerThird(t *testing.T) {
	layout, ok := VariantByName(VariantStreamerLandscape16x9)
	if !ok {
		t.Fatal("landscape layout is not registered")
	}
	plan := EditPlan{
		Variant:        layout.Name,
		GameplayCrop:   layout.DefaultGameplayCrop,
		KillfeedCrop:   &CropRect{X: 0.82, Y: 0.05, Width: 0.17, Height: 0.18},
		StreamerBanner: StreamerBannerPlan{Nick: "zacketizorcs2"},
	}
	clip := ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 5, KillfeedSeconds: []float64{2.75}}
	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:     "source.mp4",
		OutputPath:     "out.mp4",
		BannerFontPath: "font.ttf",
	}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2:color=black[content]",
		"color=c=0x111319:s=520x64:r=60:d=5.000",
		"text='@zacketizorcs2'",
		"overlay=x='32':y=983:eval=frame",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
	for _, unwanted := range []string{"color=c=0x9146ff:s=1920x96", "killfeed0", "nsharp0_0"} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("landscape args contain %q: %s", unwanted, joined)
		}
	}
}

func TestFFmpegDrawtextTextEscapesFilterMetacharacters(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "option delimiters", value: "o'clock:50%,[x]", want: `o\'clock\:50\%\,\[x\]`},
		{name: "backslash", value: `a\b`, want: `a\\b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ffmpegDrawtextText(tt.value); got != tt.want {
				t.Fatalf("ffmpegDrawtextText(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
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

func TestBuildFFmpegArgsLandscape16x9PreservesFullFrame(t *testing.T) {
	plan := EditPlan{
		Variant:      VariantStreamerLandscape16x9,
		GameplayCrop: CropRect{X: 0, Y: 0, Width: 1, Height: 1},
	}
	clip := ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 5}
	args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4"}, plan, clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "vstack") {
		t.Fatalf("landscape args must preserve the source frame without stacking: %s", joined)
	}
	if want := "scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2:color=black"; !strings.Contains(joined, want) {
		t.Fatalf("args missing %q: %s", want, joined)
	}
	if strings.Contains(joined, "force_original_aspect_ratio=increase,crop=1920:1080") {
		t.Fatalf("landscape args crop non-16:9 sources: %s", joined)
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

func killedPlanWith(kill KillfeedKill) EditPlan {
	killfeedCrop := CropRect{X: 0.75, Y: 0.05, Width: 0.2, Height: 0.1}
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &killfeedCrop
	plan.Clips = []ClipRange{{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      12,
		KillfeedSeconds: []float64{11},
		KillfeedKills:   [][]KillfeedKill{{kill}},
	}}
	return plan
}

func validKill() KillfeedKill {
	return KillfeedKill{
		AttackerSide: "T",
		AttackerName: "player1",
		VictimSide:   "CT",
		VictimName:   "player2",
		Weapon:       "ak47",
	}
}

func TestEditPlanValidationChecksKillfeedKills(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*KillfeedKill)
		wantErr string
	}{
		{name: "valid kill accepted"},
		{name: "empty attacker name", mutate: func(k *KillfeedKill) { k.AttackerName = "" }, wantErr: "attacker_name"},
		{name: "empty victim name", mutate: func(k *KillfeedKill) { k.VictimName = "" }, wantErr: "victim_name"},
		{name: "bad attacker side", mutate: func(k *KillfeedKill) { k.AttackerSide = "TT" }, wantErr: "attacker_side"},
		{name: "bad victim side", mutate: func(k *KillfeedKill) { k.VictimSide = "" }, wantErr: "victim_side"},
		{name: "unknown weapon", mutate: func(k *KillfeedKill) { k.Weapon = "not_a_weapon" }, wantErr: "weapon"},
		{name: "assister without name ignores side", mutate: func(k *KillfeedKill) { k.AssisterSide = "" }},
		{name: "assister with name needs side", mutate: func(k *KillfeedKill) { k.AssisterName = "helper"; k.AssisterSide = "" }, wantErr: "assister_side"},
		{name: "assister with valid side accepted", mutate: func(k *KillfeedKill) { k.AssisterName = "helper"; k.AssisterSide = "CT" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kill := validKill()
			if tt.mutate != nil {
				tt.mutate(&kill)
			}
			err := killedPlanWith(kill).Validate()
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

func TestEditPlanValidationRejectsKillCueLengthMismatch(t *testing.T) {
	killfeedCrop := CropRect{X: 0.75, Y: 0.05, Width: 0.2, Height: 0.1}
	plan := DefaultEditPlan()
	plan.KillfeedCrop = &killfeedCrop
	plan.Clips = []ClipRange{{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      13,
		KillfeedSeconds: []float64{11, 12},
		KillfeedKills:   [][]KillfeedKill{{validKill()}},
	}}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "killfeed_kills") {
		t.Fatalf("Validate error = %v, want killfeed_kills length error", err)
	}
}

func TestEditPlanValidationRequiresKillfeedCropForKills(t *testing.T) {
	// Kills are index-aligned with killfeed_seconds, so a clip carrying kills
	// always carries cues and must therefore configure a killfeed crop.
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      12,
		KillfeedSeconds: []float64{11},
		KillfeedKills:   [][]KillfeedKill{{validKill()}},
	}}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "killfeed_crop is not configured") {
		t.Fatalf("Validate error = %v, want missing killfeed_crop error", err)
	}
}

func TestNormalizeEditPlanNormalizesKillsWithoutMutatingCaller(t *testing.T) {
	callerKill := KillfeedKill{
		AttackerSide: " t ",
		AttackerName: "  player1  ",
		VictimSide:   "ct",
		VictimName:   " player2 ",
		AssisterSide: "t",
		AssisterName: " helper ",
		Weapon:       "AK47",
	}
	callerKills := [][]KillfeedKill{{callerKill}, nil}
	callerClips := []ClipRange{{
		ID:              "clip-001",
		StartSeconds:    10,
		EndSeconds:      14,
		KillfeedSeconds: []float64{11, 12},
		KillfeedKills:   callerKills,
	}}

	normalized := NormalizeEditPlan(EditPlan{Clips: callerClips})
	got := normalized.Clips[0].KillfeedKills
	if len(got) != 2 {
		t.Fatalf("normalized killfeed kills len = %d, want 2 (index alignment preserved)", len(got))
	}
	if got[1] != nil {
		t.Fatalf("normalized empty cue = %v, want nil to keep index alignment", got[1])
	}
	want := KillfeedKill{
		AttackerSide: "T",
		AttackerName: "player1",
		VictimSide:   "CT",
		VictimName:   "player2",
		AssisterSide: "T",
		AssisterName: "helper",
		Weapon:       "ak47",
	}
	if got[0][0] != want {
		t.Fatalf("normalized kill = %+v, want %+v", got[0][0], want)
	}
	if callerKills[0][0] != callerKill {
		t.Fatalf("caller kill mutated: %+v", callerKills[0][0])
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
