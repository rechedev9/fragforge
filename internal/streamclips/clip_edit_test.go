package streamclips

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func floatPtr(v float64) *float64 { return &v }

func planWithClip(clip ClipRange) EditPlan {
	plan := DefaultEditPlan()
	plan.Clips = []ClipRange{clip}
	return plan
}

func TestClipEditValidation(t *testing.T) {
	tests := []struct {
		name    string
		edit    ClipEdit
		wantErr string
	}{
		{name: "empty edit accepted"},
		{name: "speed one accepted", edit: ClipEdit{Speed: 1}},
		{name: "speed min accepted", edit: ClipEdit{Speed: 0.25}},
		{name: "speed max accepted", edit: ClipEdit{Speed: 3}},
		{name: "speed too slow rejected", edit: ClipEdit{Speed: 0.2}, wantErr: "speed must be between 0.25 and 3"},
		{name: "speed too fast rejected", edit: ClipEdit{Speed: 3.5}, wantErr: "speed must be between 0.25 and 3"},
		{name: "speed nan rejected", edit: ClipEdit{Speed: math.NaN()}, wantErr: "speed must be between 0.25 and 3"},
		{name: "source volume zero mutes", edit: ClipEdit{SourceVolume: floatPtr(0)}},
		{name: "source volume max accepted", edit: ClipEdit{SourceVolume: floatPtr(2)}},
		{name: "source volume negative rejected", edit: ClipEdit{SourceVolume: floatPtr(-0.1)}, wantErr: "source_volume must be between 0 and 2"},
		{name: "source volume too loud rejected", edit: ClipEdit{SourceVolume: floatPtr(2.5)}, wantErr: "source_volume must be between 0 and 2"},
		{name: "source volume nan rejected", edit: ClipEdit{SourceVolume: floatPtr(math.NaN())}, wantErr: "source_volume must be between 0 and 2"},
		{name: "fades accepted", edit: ClipEdit{FadeInSeconds: 0.5, FadeOutSeconds: 1}},
		{name: "fade in negative rejected", edit: ClipEdit{FadeInSeconds: -1}, wantErr: "fade_in_seconds must be between 0 and 5"},
		{name: "fade out too long rejected", edit: ClipEdit{FadeOutSeconds: 5.5}, wantErr: "fade_out_seconds must be between 0 and 5"},
		{name: "fade in nan rejected", edit: ClipEdit{FadeInSeconds: math.NaN()}, wantErr: "fade_in_seconds must be between 0 and 5"},
		{
			// The clip is 10s of source but plays back in 4s at 2.5x; the
			// combined fades must fit the output duration, not the source.
			name:    "fades exceeding sped-up duration rejected",
			edit:    ClipEdit{Speed: 2.5, FadeInSeconds: 2.5, FadeOutSeconds: 2},
			wantErr: "fades must fit within the clip",
		},
		{name: "text overlay accepted", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "Nice shot!", PositionY: 0.3}}}},
		{name: "text overlay with window accepted", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "hola", PositionY: 0.5, StartSeconds: floatPtr(1), EndSeconds: floatPtr(3)}}}},
		{name: "blank overlay text rejected", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "   ", PositionY: 0.5}}}, wantErr: "text is required"},
		{name: "overlay text too long rejected", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: strings.Repeat("a", 121), PositionY: 0.5}}}, wantErr: "at most 120 characters"},
		{name: "overlay text newline rejected", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "two\nlines", PositionY: 0.5}}}, wantErr: "control characters"},
		{name: "overlay text with punctuation accepted", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: `it's 100% \ ¡ÉPICO! {ok}`, PositionY: 0.5}}}},
		{name: "overlay position too high rejected", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.01}}}, wantErr: "position_y must be finite and between 0.025 and 0.975"},
		{name: "overlay position too low rejected", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.99}}}, wantErr: "position_y must be finite and between 0.025 and 0.975"},
		{name: "overlay font size accepted", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.5, FontSize: 24}}}},
		{name: "overlay font size too small rejected", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.5, FontSize: 12}}}, wantErr: "font_size must be between 24 and 120"},
		{name: "overlay font size too large rejected", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.5, FontSize: 200}}}, wantErr: "font_size must be between 24 and 120"},
		{name: "overlay start after end rejected", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.5, StartSeconds: floatPtr(3), EndSeconds: floatPtr(1)}}}, wantErr: "end_seconds must be greater than start_seconds"},
		{name: "overlay start beyond clip rejected", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.5, StartSeconds: floatPtr(11)}}}, wantErr: "start_seconds must be inside the clip"},
		{name: "overlay end beyond clip rejected", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.5, EndSeconds: floatPtr(10.5)}}}, wantErr: "end_seconds must be inside the clip"},
		{name: "overlay negative start rejected", edit: ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.5, StartSeconds: floatPtr(-1)}}}, wantErr: "start_seconds must be inside the clip"},
		{
			name: "too many overlays rejected",
			edit: ClipEdit{TextOverlays: []TextOverlay{
				{Text: "1", PositionY: 0.1}, {Text: "2", PositionY: 0.2}, {Text: "3", PositionY: 0.3},
				{Text: "4", PositionY: 0.4}, {Text: "5", PositionY: 0.5},
			}},
			wantErr: "at most 4 text overlays",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edit := tt.edit
			// A 10-second clip so overlay windows and fades have room.
			plan := planWithClip(ClipRange{ID: "clip-001", StartSeconds: 5, EndSeconds: 15, Edit: &edit})

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

func TestNormalizeEditPlanTrimsOverlayTextWithoutMutatingCaller(t *testing.T) {
	original := planWithClip(ClipRange{
		ID:           "clip-001",
		StartSeconds: 0,
		EndSeconds:   5,
		Edit:         &ClipEdit{TextOverlays: []TextOverlay{{Text: "  Nice shot!  ", PositionY: 0.3}}},
	})

	normalized := NormalizeEditPlan(original)

	if got := normalized.Clips[0].Edit.TextOverlays[0].Text; got != "Nice shot!" {
		t.Fatalf("normalized overlay text = %q, want trimmed", got)
	}
	if got := original.Clips[0].Edit.TextOverlays[0].Text; got != "  Nice shot!  " {
		t.Fatalf("caller overlay text = %q, want untouched", got)
	}
}

func TestNormalizeEditPlanDropsEmptyClipEdit(t *testing.T) {
	plan := planWithClip(ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 5, Edit: &ClipEdit{}})

	normalized := NormalizeEditPlan(plan)

	if normalized.Clips[0].Edit != nil {
		t.Fatalf("empty clip edit should normalize to nil, got %+v", normalized.Clips[0].Edit)
	}
}

func TestNewVideoEntryUsesSpeedAdjustedDuration(t *testing.T) {
	clip := ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 10, Edit: &ClipEdit{Speed: 2}}

	entry := NewVideoEntry(clip, "key")

	if entry.DurationSeconds != 5 {
		t.Fatalf("DurationSeconds = %g, want 5 (10s source at 2x)", entry.DurationSeconds)
	}
}

func TestBuildFFmpegArgsSpeedAppliesSetptsAndAtempo(t *testing.T) {
	plan := planWithClip(ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 10, Edit: &ClipEdit{Speed: 2}})

	args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4", SourceHasAudio: true}, plan, plan.Clips[0])
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"setpts=PTS/2.000000,fps=60",
		"[0:a]atempo=2.000000[a]",
		"-map [a]",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "-map 0:a?") {
		t.Fatalf("tempo-adjusted audio must replace the passthrough map: %s", joined)
	}
}

func TestBuildFFmpegArgsSpeedChainsAtempoOutsideSingleFilterRange(t *testing.T) {
	tests := []struct {
		name  string
		speed float64
		want  string
	}{
		{name: "triple speed", speed: 3, want: "[0:a]atempo=2.000000,atempo=1.500000[a]"},
		{name: "quarter speed", speed: 0.25, want: "[0:a]atempo=0.500000,atempo=0.500000[a]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := planWithClip(ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 10, Edit: &ClipEdit{Speed: tt.speed}})

			args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4", SourceHasAudio: true}, plan, plan.Clips[0])
			if err != nil {
				t.Fatalf("BuildFFmpegArgs error = %v", err)
			}
			if joined := strings.Join(args, " "); !strings.Contains(joined, tt.want) {
				t.Fatalf("args missing %q: %s", tt.want, joined)
			}
		})
	}
}

func TestBuildFFmpegArgsSourceVolumeFiltersOriginalAudio(t *testing.T) {
	tests := []struct {
		name   string
		volume float64
		want   string
	}{
		{name: "half volume", volume: 0.5, want: "[0:a]volume=0.500000[a]"},
		{name: "mute", volume: 0, want: "[0:a]volume=0.000000[a]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			volume := tt.volume
			plan := planWithClip(ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 5, Edit: &ClipEdit{SourceVolume: &volume}})

			args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4", SourceHasAudio: true}, plan, plan.Clips[0])
			if err != nil {
				t.Fatalf("BuildFFmpegArgs error = %v", err)
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, tt.want) {
				t.Fatalf("args missing %q: %s", tt.want, joined)
			}
			if !strings.Contains(joined, "-map [a]") {
				t.Fatalf("filtered audio must be mapped: %s", joined)
			}
		})
	}
}

func TestBuildFFmpegArgsFadesUseOutputTimeline(t *testing.T) {
	// 10s of source at 2x plays back in 5s: the fade-out must start at
	// 5s - 1s = 4s of output time, not at 9s of source time.
	plan := planWithClip(ClipRange{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 10,
		Edit: &ClipEdit{Speed: 2, FadeInSeconds: 0.5, FadeOutSeconds: 1},
	})

	args, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4", SourceHasAudio: true}, plan, plan.Clips[0])
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"setpts=PTS/2.000000,fade=t=in:st=0:d=0.500000,fade=t=out:st=4.000000:d=1.000000,fps=60",
		"[0:a]atempo=2.000000,afade=t=in:st=0:d=0.500000,afade=t=out:st=4.000000:d=1.000000[a]",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
}

func TestBuildFFmpegArgsSpeedWithMusicMixesTempoAdjustedSource(t *testing.T) {
	plan := planWithClip(ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 10, Edit: &ClipEdit{Speed: 2}})
	plan.Music = MusicPlan{Key: "concrete-teeth", Volume: 0.3}

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
		"[0:a]atempo=2.000000[srca]",
		"[1:a]volume=0.300000[bgm]",
		"[srca][bgm]amix=inputs=2:duration=first:dropout_transition=0:normalize=0[a]",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
}

func TestBuildFFmpegArgsFadeAppliesToMusicMix(t *testing.T) {
	plan := planWithClip(ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 5, Edit: &ClipEdit{FadeOutSeconds: 1}})
	plan.Music = MusicPlan{Key: "concrete-teeth", Volume: 0.3}

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
	want := "[0:a][bgm]amix=inputs=2:duration=first:dropout_transition=0:normalize=0,afade=t=out:st=4.000000:d=1.000000[a]"
	if !strings.Contains(joined, want) {
		t.Fatalf("args missing %q: %s", want, joined)
	}
}

func TestBuildFFmpegArgsTextOverlayDrawsTextWithEnableWindow(t *testing.T) {
	plan := planWithClip(ClipRange{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 10,
		Edit: &ClipEdit{TextOverlays: []TextOverlay{{
			Text: "Nice shot!", PositionY: 0.3, StartSeconds: floatPtr(1), EndSeconds: floatPtr(3),
		}}},
	})

	args, err := BuildFFmpegArgs(FFmpegInputs{
		SourcePath:       "source.mp4",
		OutputPath:       "out.mp4",
		BannerFontPath:   "font.ttf",
		TextOverlayPaths: []string{"text0.txt"},
	}, plan, plan.Clips[0])
	if err != nil {
		t.Fatalf("BuildFFmpegArgs error = %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		// The text lives in a materialized file read with expansion=none, so
		// arbitrary user text never needs filtergraph escaping.
		"drawtext=fontfile='font.ttf':textfile='text0.txt':expansion=none",
		"fontsize=64",
		"x=(w-text_w)/2:y=h*0.300000-text_h/2",
		`enable='between(t\,1.000000\,3.000000)'`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
}

func TestBuildFFmpegArgsTextOverlayRequiresMatchingTextPaths(t *testing.T) {
	plan := planWithClip(ClipRange{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 10,
		Edit: &ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.5}}},
	})

	_, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4", BannerFontPath: "font.ttf"}, plan, plan.Clips[0])
	if err == nil || !strings.Contains(err.Error(), "text overlay paths") {
		t.Fatalf("BuildFFmpegArgs error = %v, want mismatched text overlay paths error", err)
	}
}

func TestBuildFFmpegArgsTextOverlayRequiresFont(t *testing.T) {
	plan := planWithClip(ClipRange{
		ID: "clip-001", StartSeconds: 0, EndSeconds: 10,
		Edit: &ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.5}}},
	})

	_, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4", TextOverlayPaths: []string{"text0.txt"}}, plan, plan.Clips[0])
	if err == nil || !strings.Contains(err.Error(), "font path is required") {
		t.Fatalf("BuildFFmpegArgs error = %v, want missing font error", err)
	}
}

func TestBuildFFmpegArgsEmptyClipEditKeepsLegacyArgs(t *testing.T) {
	clip := ClipRange{ID: "clip-001", StartSeconds: 1.5, EndSeconds: 4.25}
	legacy, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4", SourceHasAudio: true}, planWithClip(clip), clip)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs legacy error = %v", err)
	}

	edited := clip
	edited.Edit = &ClipEdit{}
	got, err := BuildFFmpegArgs(FFmpegInputs{SourcePath: "source.mp4", OutputPath: "out.mp4", SourceHasAudio: true}, planWithClip(edited), edited)
	if err != nil {
		t.Fatalf("BuildFFmpegArgs edited error = %v", err)
	}

	if !reflect.DeepEqual(got, legacy) {
		t.Fatalf("empty clip edit changed args\n got: %v\nwant: %v", got, legacy)
	}
}

func TestEditPlanHasTextOverlays(t *testing.T) {
	plan := planWithClip(ClipRange{ID: "clip-001", StartSeconds: 0, EndSeconds: 5})
	if plan.HasTextOverlays() {
		t.Fatalf("plan without overlays reported HasTextOverlays = true")
	}
	plan.Clips[0].Edit = &ClipEdit{TextOverlays: []TextOverlay{{Text: "hi", PositionY: 0.5}}}
	if !plan.HasTextOverlays() {
		t.Fatalf("plan with overlays reported HasTextOverlays = false")
	}
}
