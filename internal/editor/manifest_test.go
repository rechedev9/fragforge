package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/recording"
)

func TestBuildManifestUsesSegmentOrderAndKillTimes(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	manifest := BuildManifest(result, testManifestOptions(dir, nil))

	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if len(manifest.Shorts) != 2 {
		t.Fatalf("shorts len = %d, want 2", len(manifest.Shorts))
	}
	if manifest.Shorts[0].SegmentID != "seg-001" || manifest.Shorts[1].SegmentID != "seg-002" {
		t.Fatalf("short order = %s, %s", manifest.Shorts[0].SegmentID, manifest.Shorts[1].SegmentID)
	}
	first := manifest.Shorts[0]
	if first.Label != "MartinezSa | de_ancient | 2K" {
		t.Fatalf("label = %q", first.Label)
	}
	if first.PrimaryWeapon != "AK-47" {
		t.Fatalf("primary weapon = %q, want AK-47", first.PrimaryWeapon)
	}
	if len(first.Kills) != 2 {
		t.Fatalf("kills len = %d, want 2", len(first.Kills))
	}
	if first.Kills[0].TimeSeconds != 1 {
		t.Fatalf("first kill time = %.3f, want 1.000", first.Kills[0].TimeSeconds)
	}
	if filepath.Base(first.Output) != "short-001-seg-001.mp4" {
		t.Fatalf("output = %s", first.Output)
	}
	if filepath.Base(first.PromptPath) != "short-001-seg-001-cover.md" {
		t.Fatalf("prompt path = %s", first.PromptPath)
	}
	if filepath.Base(first.PublishPath) != "01_seg-001_martinezsa_de_ancient_2k_ak47.mp4" {
		t.Fatalf("publish path = %s", first.PublishPath)
	}
	if filepath.Base(first.CaptionPath) != "01_seg-001_martinezsa_de_ancient_2k_ak47.caption.txt" {
		t.Fatalf("caption path = %s", first.CaptionPath)
	}
	if filepath.Base(first.CoverPath) != "01_seg-001_martinezsa_de_ancient_2k_ak47.cover.jpg" {
		t.Fatalf("cover path = %s", first.CoverPath)
	}
	if first.CoverTimeSeconds != 0.88 {
		t.Fatalf("cover time = %.3f, want 0.880", first.CoverTimeSeconds)
	}
	if len(first.CoverCommand) == 0 {
		t.Fatal("cover command is empty")
	}
	if manifest.Preset != DefaultPreset().Name {
		t.Fatalf("preset = %q, want default %q", manifest.Preset, DefaultPreset().Name)
	}
	if manifest.VideoCRF != DefaultPreset().VideoCRF || manifest.VideoPreset != DefaultPreset().VideoPreset {
		t.Fatalf("video encoding = crf %d preset %q", manifest.VideoCRF, manifest.VideoPreset)
	}
	if first.VideoCRF != DefaultPreset().VideoCRF || first.VideoPreset != DefaultPreset().VideoPreset {
		t.Fatalf("short video encoding = crf %d preset %q", first.VideoCRF, first.VideoPreset)
	}
	if !strings.Contains(first.Caption, "MartinezSa turns this round on Ancient into a clean 2K with the AK-47.") {
		t.Fatalf("caption = %q", first.Caption)
	}
}

func TestBuildManifestSanitizesSegmentIDInOutputPaths(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	// A pathological segment ID must not let output paths escape the output
	// directory. The "short-NNN-" prefix absorbs one "..", so use several.
	const evil = "../../../../evil"
	result.Plan.Segments[0].ID = evil
	result.Artifacts[1].SegmentID = evil // the seg-001 clip

	opts := testManifestOptions(dir, nil)
	manifest := BuildManifest(result, opts)

	idx := -1
	for i, s := range manifest.Shorts {
		if s.SegmentID == evil {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("no short produced for segment %q", evil)
	}
	short := manifest.Shorts[idx]
	for _, p := range []string{short.Output, short.PromptPath} {
		rel, err := filepath.Rel(opts.OutputDir, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("path %q escapes output dir %q (rel=%q, err=%v)", p, opts.OutputDir, rel, err)
		}
	}
	for _, p := range []string{short.PublishPath, short.CaptionPath, short.CoverPath} {
		rel, err := filepath.Rel(opts.PublishDir, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			t.Fatalf("publish path %q escapes publish dir %q (rel=%q, err=%v)", p, opts.PublishDir, rel, err)
		}
	}
	if got := filepath.Base(short.Output); got != "short-001-evil.mp4" {
		t.Fatalf("output base = %q, want short-001-evil.mp4 (sanitized)", got)
	}
}

func TestBuildManifestSupportsSegmentFilterAndLimit(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.SegmentIDs = []string{"seg-002", "seg-missing", "seg-002"}
	opts.Limit = 1
	opts.SkipExisting = true

	manifest := BuildManifest(result, opts)
	if got := manifest.SegmentFilter; len(got) != 2 || got[0] != "seg-002" || got[1] != "seg-missing" {
		t.Fatalf("segment filter = %#v", got)
	}
	if manifest.Limit != 1 {
		t.Fatalf("limit = %d, want 1", manifest.Limit)
	}
	if !manifest.SkipExisting || manifest.GalleryPath == "" || manifest.SummaryPath == "" {
		t.Fatalf("manifest reuse/gallery metadata missing: %#v", manifest)
	}
	if len(manifest.Shorts) != 1 || manifest.Shorts[0].SegmentID != "seg-002" {
		t.Fatalf("shorts = %#v", manifest.Shorts)
	}
	if manifest.Shorts[0].Index != 2 {
		t.Fatalf("index = %d, want original segment index 2", manifest.Shorts[0].Index)
	}
	if joined := strings.Join(manifest.Warnings, "\n"); !strings.Contains(joined, `requested segment "seg-missing" was not found`) {
		t.Fatalf("warnings missing segment warning:\n%s", joined)
	}
}

func TestBuildManifestUsesKillPlanMapWhenRecordingMapMissing(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	result.Plan.DemoMap = ""
	result.Plan.DemoPath = filepath.Join(dir, "match.dem")
	kp := killplan.NewPlan()
	kp.Demo.Map = "de_ancient"

	manifest := BuildManifest(result, testManifestOptions(dir, &kp))
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.KillPlan == "" {
		t.Fatal("KillPlan path is empty")
	}
	if got := manifest.Shorts[0].Map; got != "de_ancient" {
		t.Fatalf("map = %q, want de_ancient", got)
	}
	if got := manifest.Shorts[0].Label; got != "MartinezSa | de_ancient | 2K" {
		t.Fatalf("label = %q", got)
	}
}

func TestVideoFilterEscapesDrawtextAndBuildsPunchIns(t *testing.T) {
	short := ShortEdit{
		Effects: []Effect{
			{Type: EffectZoom, StartSeconds: 0.72, EndSeconds: 1.72, Scale: 1.040625},
			{
				Type:         EffectText,
				Value:        "Martinez:Sa's | de_ancient | 100%",
				StartSeconds: 0,
				EndSeconds:   2.6,
				X:            "48",
				Y:            "72",
				Size:         36,
				FontColor:    "white@0.92",
				BoxColor:     "black@0.36",
				BoxBorder:    16,
			},
			{
				Type:         EffectText,
				Value:        "AK-47 HS",
				StartSeconds: 0.85,
				EndSeconds:   2.1,
				X:            "48",
				Y:            "132",
				Size:         30,
				FontColor:    "white@0.90",
				BoxColor:     "black@0.30",
				BoxBorder:    14,
			},
		},
	}
	filter := VideoFilter(short)
	for _, want := range []string{
		"scale=w=-2:h='if(between(t\\,0.720\\,1.220)",
		"(1920.000+(1998.000-1920.000)",
		"if(between(t\\,1.220\\,1.720)",
		"(1998.000+(1920.000-1998.000)",
		"crop=1080:1920:(iw-ow)/2:(ih-oh)/2",
		"setsar=1",
		"fps=60",
		"Martinez\\:Sa\\'s | de_ancient | 100\\%",
		"AK-47 HS",
		"format=yuv420p",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("filter missing %q:\n%s", want, filter)
		}
	}
}

func TestVideoFilterFadesLuaText(t *testing.T) {
	filter := VideoFilter(ShortEdit{
		Effects: []Effect{
			{
				Type:           EffectText,
				Value:          "HEADSHOT",
				StartSeconds:   1,
				EndSeconds:     2,
				FadeInSeconds:  0.12,
				FadeOutSeconds: 0.20,
			},
		},
	})

	for _, want := range []string{
		"drawtext=text='HEADSHOT'",
		"alpha='min(min(1\\,((t-1.000)/0.120))\\,((2.000-t)/0.200))'",
		"enable='between(t\\,1.000\\,2.000)'",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("filter missing %q:\n%s", want, filter)
		}
	}
}

func TestVideoFilterTextShadowAndBoxNone(t *testing.T) {
	filter := VideoFilter(ShortEdit{
		Effects: []Effect{
			{
				Type:         EffectText,
				Value:        "HEADSHOT",
				StartSeconds: 1,
				EndSeconds:   2,
				BoxColor:     "none",
				ShadowColor:  "black@0.55",
				ShadowX:      2,
				ShadowY:      3,
			},
		},
	})

	for _, want := range []string{
		"drawtext=text='HEADSHOT'",
		"box=0",
		"shadowcolor=black@0.55:shadowx=2:shadowy=3",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("filter missing %q:\n%s", want, filter)
		}
	}
	for _, reject := range []string{"box=1", "boxcolor="} {
		if strings.Contains(filter, reject) {
			t.Fatalf("filter contains %q, want it omitted:\n%s", reject, filter)
		}
	}
}

func TestCompilationFilterOverlaysKillfeedFromSourcePart(t *testing.T) {
	short := ShortEdit{
		Output:          "compiled.mp4",
		Preset:          PresetViral60Clean,
		DurationSeconds: 12,
		Effects: []Effect{
			{
				Type:           EffectKillfeed,
				StartSeconds:   9.5,
				EndSeconds:     12,
				AtSeconds:      9.55,
				X:              "W-w-24",
				Y:              "416",
				Width:          430,
				CropX:          1558,
				CropY:          64,
				CropWidth:      360,
				CropHeight:     110,
				FadeInSeconds:  0.08,
				FadeOutSeconds: 0.25,
			},
		},
		Parts: []ShortPart{
			{SegmentID: "seg-001", Input: "seg-001.mp4", DurationSeconds: 6, TimelineStartSeconds: 0},
			{SegmentID: "seg-002", Input: "seg-002.mp4", DurationSeconds: 6, TimelineStartSeconds: 6},
		},
	}

	filter := CompilationFilter(short)
	for _, want := range []string{
		"[1:v]",
		"crop=360:110:1558:64",
		"scale=w=430:h=-1:flags=lanczos",
		// the notice is frozen at the probed frame so its translucent
		// background does not carry moving source footage into the overlay
		"trim=start=3.900:duration=0.050",
		"loop=loop=-1:size=1:start=0",
		"setpts=N/60/TB",
		// shadow crush flattens the world baked into the translucent notice
		// background; text and icons are bright and stay untouched
		"curves=all='0/0 0.35/0.08 1/1'",
		"overlay=x=W-w-24:y=416:format=auto:enable='between(t\\,9.500\\,12.000)'",
		"format=yuv420p[v]",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("compilation filter missing %q:\n%s", want, filter)
		}
	}
	if strings.Contains(filter, "setpts=PTS-STARTPTS+6.000/TB") {
		t.Fatalf("compilation filter still time-shifts live footage instead of freezing the notice:\n%s", filter)
	}
}

func TestBuildFFmpegCommandKeepsPathsAsArgs(t *testing.T) {
	short := ShortEdit{
		Input:  `C:\clips\clip's input.mp4`,
		Output: `C:\shorts\short 001.mp4`,
		Label:  "MartinezSa | de_ancient | 1K",
	}
	command := BuildFFmpegCommand(`C:\ffmpeg\ffmpeg.exe`, short)
	if command[0] != `C:\ffmpeg\ffmpeg.exe` {
		t.Fatalf("ffmpeg path arg = %q", command[0])
	}
	if got := argAfter(command, "-i"); got != short.Input {
		t.Fatalf("-i arg = %q, want %q", got, short.Input)
	}
	if got := command[len(command)-1]; got != short.Output {
		t.Fatalf("output arg = %q, want %q", got, short.Output)
	}
	if got := argAfter(command, "-map"); got != "0:v:0" {
		t.Fatalf("first -map arg = %q, want 0:v:0", got)
	}
	if got := argAfter(command, "-preset"); got != DefaultVideoPreset {
		t.Fatalf("-preset arg = %q, want %q", got, DefaultVideoPreset)
	}
	if got := argAfter(command, "-crf"); got != "18" {
		t.Fatalf("-crf arg = %q, want 18", got)
	}
}

func TestBuildManifestUsesVideoEncodingOptions(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.VideoCRF = 16
	opts.VideoPreset = "slow"

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.VideoCRF != 16 || manifest.VideoPreset != "slow" {
		t.Fatalf("manifest video encoding = crf %d preset %q", manifest.VideoCRF, manifest.VideoPreset)
	}
	first := manifest.Shorts[0]
	if first.VideoCRF != 16 || first.VideoPreset != "slow" {
		t.Fatalf("short video encoding = crf %d preset %q", first.VideoCRF, first.VideoPreset)
	}
	if got := argAfter(first.FFmpegCommand, "-preset"); got != "slow" {
		t.Fatalf("-preset arg = %q, want slow", got)
	}
	if got := argAfter(first.FFmpegCommand, "-crf"); got != "16" {
		t.Fatalf("-crf arg = %q, want 16", got)
	}
}

func TestBuildManifestCarriesMusicRhythmAndOutputFPS(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.MusicPath = filepath.Join(dir, "music", "brightmelodicskippyedm.wav")
	opts.OutputFPS = 24

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.MusicPath != opts.MusicPath {
		t.Fatalf("manifest music path = %q, want %q", manifest.MusicPath, opts.MusicPath)
	}
	if manifest.OutputFPS != 24 {
		t.Fatalf("manifest output fps = %d, want 24", manifest.OutputFPS)
	}
	first := manifest.Shorts[0]
	if first.MusicPath != opts.MusicPath || first.OutputFPS != 24 {
		t.Fatalf("short render controls = %#v", first)
	}
	filter := argAfter(first.FFmpegCommand, "-filter_complex")
	for _, want := range []string{"fps=24", "[1:a]volume=1.00[music]", "amix=inputs=2:duration=first"} {
		if !strings.Contains(filter, want) {
			t.Fatalf("music filter missing %q:\n%s", want, filter)
		}
	}
}

func TestBuildFFmpegCommandUsesConfiguredOutputFPS(t *testing.T) {
	command := BuildFFmpegCommand("ffmpeg", ShortEdit{
		Input:     "clip.mp4",
		Output:    "short.mp4",
		OutputFPS: 24,
	})
	filter := argAfter(command, "-vf")
	if !strings.Contains(filter, "fps=24") {
		t.Fatalf("filter missing configured fps:\n%s", filter)
	}
	if strings.Contains(filter, "fps=60") {
		t.Fatalf("filter should not contain default fps when configured:\n%s", filter)
	}
}

func TestBuildManifestUsesLandscapeOutputFormat(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.OutputFormat = OutputFormatLandscape16x9

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.OutputFormat != OutputFormatLandscape16x9 {
		t.Fatalf("manifest output format = %q, want %q", manifest.OutputFormat, OutputFormatLandscape16x9)
	}
	first := manifest.Shorts[0]
	filter := argAfter(first.FFmpegCommand, "-vf")
	for _, want := range []string{
		"scale=w=1920:h=1080:force_original_aspect_ratio=increase",
		"crop=1920:1080:(iw-ow)/2:(ih-oh)/2",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("landscape filter missing %q:\n%s", want, filter)
		}
	}
	if got := argAfter(first.CoverCommand, "-vf"); !strings.Contains(got, "scale=1920:1080") || !strings.Contains(got, "crop=1920:1080") {
		t.Fatalf("landscape cover filter = %s", got)
	}
}

func TestBuildManifestAppliesExplicitEditOptions(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.KillEffect = KillEffectFreezeFlash
	opts.Transition = TransitionDip
	opts.Intro = true
	opts.Outro = true

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	first := manifest.Shorts[0]
	if first.KillEffect != KillEffectFreezeFlash || first.Transition != TransitionDip || !first.Intro || !first.Outro {
		t.Fatalf("short edit options = %#v", first)
	}
	var hasZoom, hasWhiteFlash, hasBlackFlash, hasText bool
	for _, effect := range first.Effects {
		switch {
		case effect.Type == EffectZoom && effect.Source == "edit-request":
			hasZoom = true
		case effect.Type == EffectFlash && effect.Color == "white" && effect.Source == "edit-request":
			hasWhiteFlash = true
		case effect.Type == EffectFlash && effect.Color == "black" && effect.Source == "edit-request":
			hasBlackFlash = true
		case effect.Type == EffectText && effect.Source == "edit-request":
			hasText = true
		}
	}
	if !hasZoom || !hasWhiteFlash || !hasBlackFlash || !hasText {
		t.Fatalf("generated effects missing: zoom=%v white=%v black=%v text=%v effects=%#v", hasZoom, hasWhiteFlash, hasBlackFlash, hasText, first.Effects)
	}
	filter := argAfter(first.FFmpegCommand, "-vf")
	for _, want := range []string{"drawbox", "color=0xffffff", "color=0x000000", "drawtext=text="} {
		if !strings.Contains(filter, want) {
			t.Fatalf("edit filter missing %q:\n%s", want, filter)
		}
	}
}

func TestBuildManifestCompileSegmentsCreatesOneShort(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.CompileSegments = true
	opts.MusicPath = filepath.Join(dir, "music", "brightmelodicskippyedm.wav")
	opts.OutputFPS = 24

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if !manifest.CompileSegments {
		t.Fatal("manifest compile_segments = false, want true")
	}
	if len(manifest.Shorts) != 1 {
		t.Fatalf("shorts len = %d, want 1", len(manifest.Shorts))
	}
	short := manifest.Shorts[0]
	if short.SegmentID != "demo-compilation" {
		t.Fatalf("compiled segment id = %q", short.SegmentID)
	}
	if len(short.Parts) != 2 {
		t.Fatalf("parts len = %d, want 2", len(short.Parts))
	}
	if short.Parts[0].SegmentID != "seg-001" || short.Parts[1].SegmentID != "seg-002" {
		t.Fatalf("part order = %#v", short.Parts)
	}
	if len(short.Kills) != 3 {
		t.Fatalf("compiled kills len = %d, want 3", len(short.Kills))
	}
	if short.OutputFPS != 24 || short.MusicPath != opts.MusicPath {
		t.Fatalf("compiled short controls = %#v", short)
	}
}

func TestBuildManifestAppliesRhythmSyncToCompiledParts(t *testing.T) {
	dir := t.TempDir()
	rhythmPath := filepath.Join(dir, "rhythm.json")
	if err := os.WriteFile(rhythmPath, []byte(`{
		"schema_version":"1.0",
		"segment_sync":[
			{"segment_id":"seg-001","timeline_start_seconds":0.500,"gap_before_seconds":0.500},
			{"segment_id":"seg-002","timeline_start_seconds":9.000,"gap_before_seconds":1.000}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.CompileSegments = true
	opts.MusicPath = filepath.Join(dir, "music", "brightmelodicskippyedm.wav")
	opts.RhythmPath = rhythmPath
	opts.OutputFPS = 24

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	short := manifest.Shorts[0]
	if got := short.Parts[0].GapBeforeSeconds; got != 0.5 {
		t.Fatalf("part[0] gap = %.3f, want 0.500", got)
	}
	if got := short.Parts[1].TimelineStartSeconds; got != 9.0 {
		t.Fatalf("part[1] timeline start = %.3f, want 9.000", got)
	}
	if got := short.Kills[2].TimeSeconds; got <= 9.0 {
		t.Fatalf("third kill time = %.3f, want shifted onto compiled timeline", got)
	}
}

func TestBuildFFmpegCommandForCompilationShort(t *testing.T) {
	short := ShortEdit{
		Output:          "compiled.mp4",
		MusicPath:       "music/brightmelodicskippyedm.wav",
		OutputFPS:       24,
		AudioNormalize:  true,
		DurationSeconds: 8,
		Effects: []Effect{
			{
				Type:           EffectImage,
				Path:           "assets/donk.png",
				StartSeconds:   0,
				EndSeconds:     1.5,
				X:              "W-w-34",
				Y:              "1010",
				Width:          430,
				FadeInSeconds:  0.10,
				FadeOutSeconds: 0.30,
			},
		},
		Parts: []ShortPart{
			{SegmentID: "seg-001", Input: "seg-001.mp4", DurationSeconds: 4.0, GapBeforeSeconds: 0.5},
			{SegmentID: "seg-002", Input: "seg-002.mp4", DurationSeconds: 3.0},
		},
	}
	command := BuildFFmpegCommand("ffmpeg", short)
	filter := argAfter(command, "-filter_complex")
	for _, want := range []string{
		"[0:v]",
		"[1:v]",
		"color=c=black:s=1080x1920:r=24:d=0.500",
		"anullsrc=channel_layout=stereo:sample_rate=48000:d=0.500",
		"concat=n=3:v=1:a=1",
		"fps=24",
		"[2:a]volume=1.00[music]",
		"[3:v]format=rgba,scale=w=430:h=-1:flags=lanczos,loop=loop=-1:size=1:start=0,setpts=N/24/TB,trim=duration=8.000",
		"overlay=x=W-w-34:y=1010:format=auto:enable='between(t\\,0.000\\,1.500)'[vimages]",
		"[vimages]format=yuv420p[v]",
		"amix=inputs=2:duration=first:dropout_transition=0",
		"loudnorm=I=-16:TP=-1.5:LRA=11[a]",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("compilation filter missing %q:\n%s", want, filter)
		}
	}
	for _, want := range []string{"seg-001.mp4", "seg-002.mp4", "music/brightmelodicskippyedm.wav", "assets/donk.png"} {
		if !containsArg(command, want) {
			t.Fatalf("command missing %q: %#v", want, command)
		}
	}
}

func TestBuildManifestWarnsOnUnexpectedSourceFormat(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	result.Artifacts[1].Width = 1280
	result.Artifacts[1].Height = 720
	result.Artifacts[1].FrameRate = "30/1"

	manifest := BuildManifest(result, testManifestOptions(dir, nil))
	joined := strings.Join(manifest.Warnings, "\n")
	for _, want := range []string{
		"source seg-001 resolution = 1280x720, want 1920x1080",
		`source seg-001 frame_rate = "30/1", want 60fps`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings missing %q:\n%s", want, joined)
		}
	}
}

func TestGenerateCoverPromptUsesMetadata(t *testing.T) {
	prompt := GenerateCoverPrompt(ShortEdit{
		Player:        "MartinezSa",
		Map:           "de_ancient",
		KillCount:     2,
		PrimaryWeapon: "AK-47",
		Output:        filepath.Join("shorts", "short-001.mp4"),
		CoverPath:     filepath.Join("publish", "cover.jpg"),
		Kills: []KillCue{
			{Weapon: "AK-47", Victim: "opponent-one", Headshot: true},
			{Weapon: "AWP", Victim: "opponent-two"},
		},
	})
	for _, want := range []string{"MartinezSa", "de_ancient", "2K", "AK-47", "AWP", "opponent-one", "1 headshot", "Gameplay frame", "Do not invent a face", "2K con AK-47 en de_ancient"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestPublishFileBaseIsStableAndWindowsSafe(t *testing.T) {
	got := publishFileBase(1, "seg-001", "MartinezSa", "de_ancient", 2, "M4A1-S")
	want := "01_seg-001_martinezsa_de_ancient_2k_m4a1s"
	if got != want {
		t.Fatalf("publishFileBase = %q, want %q", got, want)
	}
}

func TestCoverTimeSecondsUsesFirstKillAndFallback(t *testing.T) {
	if got := coverTimeSeconds([]KillCue{{TimeSeconds: 1}}, 8); got != 0.88 {
		t.Fatalf("coverTimeSeconds = %.3f, want 0.880", got)
	}
	if got := coverTimeSeconds([]KillCue{{TimeSeconds: 0.05}}, 8); got != 0 {
		t.Fatalf("coverTimeSeconds clamped = %.3f, want 0", got)
	}
	if got := coverTimeSeconds(nil, 10); got != 3.5 {
		t.Fatalf("coverTimeSeconds fallback = %.3f, want 3.500", got)
	}
}

func testManifestOptions(dir string, kp *killplan.Plan) ManifestOptions {
	return ManifestOptions{
		RecordingResultPath: filepath.Join(dir, "recording", "recording-result.json"),
		KillPlanPath:        filepath.Join(dir, "plan.json"),
		OutputDir:           filepath.Join(dir, "shorts"),
		PublishDir:          filepath.Join(dir, "shorts", "publish"),
		FFmpegPath:          "ffmpeg",
		CoversEnabled:       true,
		KillPlan:            kp,
	}
}

func testRecordingResult(dir string) recording.RecordingResult {
	return recording.RecordingResult{
		Plan: recording.RecordingPlan{
			DemoPath:         filepath.Join(dir, "match-de_ancient.dem"),
			DemoMap:          "de_ancient",
			OutputDir:        filepath.Join(dir, "recording"),
			TargetSteamID64:  "76561198148986856",
			TargetNameInDemo: "MartinezSa",
			TargetAccountID:  188721128,
			Tickrate:         64,
			Stream:           recording.DefaultStreamConfig(),
			Segments: []recording.RecordingSegment{
				{
					ID:        "seg-001",
					TickStart: 14029,
					TickEnd:   14770,
					Kills: []killplan.Kill{
						{Tick: 14221, Weapon: "ak47", Headshot: true, Victim: killplan.Player{NameInDemo: "opponent-one"}},
						{Tick: 14450, Weapon: "ak47", Victim: killplan.Player{NameInDemo: "opponent-two"}},
					},
				},
				{
					ID:        "seg-002",
					TickStart: 22086,
					TickEnd:   22406,
					Kills: []killplan.Kill{
						{Tick: 22250, Weapon: "awp", Victim: killplan.Player{NameInDemo: "opponent-three"}},
					},
				},
			},
		},
		Artifacts: []recording.RecordingArtifact{
			{SegmentID: "seg-002", Role: "segment", Type: "video", Path: filepath.Join(dir, "recording", "segments", "seg-002.mp4"), SizeBytes: 9_500_000, DurationSeconds: 5, Codec: "h264", Width: 1920, Height: 1080, FrameRate: "60/1"},
			{SegmentID: "seg-001", Role: "segment", Type: "video", Path: filepath.Join(dir, "recording", "segments", "seg-001.mp4"), SizeBytes: 14_500_000, DurationSeconds: 8, Codec: "h264", Width: 1920, Height: 1080, FrameRate: "60/1"},
		},
	}
}

func testSmokeRecordingResult(dir string) recording.RecordingResult {
	return recording.RecordingResult{
		Plan: recording.RecordingPlan{
			DemoPath:         filepath.Join(dir, "match-de_ancient.dem"),
			DemoMap:          "de_ancient",
			OutputDir:        filepath.Join(dir, "recording"),
			TargetSteamID64:  "76561198148986856",
			TargetNameInDemo: "MartinezSa",
			TargetAccountID:  188721128,
			Tickrate:         64,
			Stream:           recording.DefaultStreamConfig(),
			Segments: []recording.RecordingSegment{
				{
					ID:        "seg-001",
					Round:     3,
					TickStart: 9000,
					TickEnd:   9800,
					Utility: []killplan.UtilityThrow{
						{
							ID:          "smoke-001",
							Type:        "smokegrenade",
							Round:       3,
							ThrowTick:   9200,
							PopTick:     9500,
							ThrowPlace:  "T Spawn",
							ThrowAction: "jumpthrow",
							Stance:      "standing",
							ThrowPos:    [3]float64{10, 20, 0},
							LandingPos:  [3]float64{110, 205, 0},
						},
					},
				},
			},
		},
		Artifacts: []recording.RecordingArtifact{
			{SegmentID: "seg-001", Role: "segment", Type: "video", Path: filepath.Join(dir, "recording", "segments", "seg-001.mp4"), SizeBytes: 12_000_000, DurationSeconds: 10, Codec: "h264", Width: 1920, Height: 1080, FrameRate: "60/1"},
		},
	}
}

func argAfter(args []string, key string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key {
			return args[i+1]
		}
	}
	return ""
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestTailTrimmedDuration(t *testing.T) {
	kills := []KillCue{{TimeSeconds: 1.0}, {TimeSeconds: 4.578}}
	tests := []struct {
		name     string
		kills    []KillCue
		duration float64
		tail     float64
		want     float64
	}{
		{name: "no kills keeps duration", kills: nil, duration: 8, tail: 1.5, want: 8},
		{name: "zero tail disables trim", kills: kills, duration: 8, tail: 0, want: 8},
		{name: "trims dead air after final kill", kills: kills, duration: 8, tail: 1.5, want: 6.078},
		{name: "tail beyond clip end keeps duration", kills: kills, duration: 5, tail: 1.5, want: 5},
		{name: "unknown duration stays unknown", kills: kills, duration: 0, tail: 1.5, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tailTrimmedDuration(tt.kills, tt.duration, tt.tail); got != tt.want {
				t.Fatalf("tailTrimmedDuration = %.3f, want %.3f", got, tt.want)
			}
		})
	}
}

func TestBuildManifestAppliesRetentionOverlaysAndTailTrim(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.HookText = true
	opts.KillCounter = true
	opts.KillfeedOverlay = true
	opts.TailTrimSeconds = 1.5

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	first := manifest.Shorts[0]
	lastKill := first.Kills[len(first.Kills)-1].TimeSeconds
	if want := roundMillis(lastKill + 1.5); first.DurationSeconds != want {
		t.Fatalf("duration = %.3f, want tail-trimmed %.3f", first.DurationSeconds, want)
	}

	var hooks, counters, killfeeds []Effect
	for _, effect := range first.Effects {
		switch {
		case effect.Type == EffectText && effect.Y == "150":
			hooks = append(hooks, effect)
		case effect.Type == EffectText && effect.Y == "h*0.62":
			counters = append(counters, effect)
		case effect.Type == EffectKillfeed:
			killfeeds = append(killfeeds, effect)
		}
	}
	if len(hooks) != 1 || hooks[0].Value != first.Headline {
		t.Fatalf("hook effects = %#v, want one drawing headline %q", hooks, first.Headline)
	}
	if len(counters) != 2 || counters[0].Value != "1" || counters[1].Value != "2K" {
		t.Fatalf("counter effects = %#v, want values 1 and 2K", counters)
	}
	if len(killfeeds) != len(first.Kills) {
		t.Fatalf("killfeed effects = %d, want one per kill (%d)", len(killfeeds), len(first.Kills))
	}
	command := strings.Join(first.FFmpegCommand, " ")
	if !strings.Contains(command, "-filter_complex") || !strings.Contains(command, "split=3[main][kfsrc0][kfsrc1]") {
		t.Fatalf("command = %q, want killfeed filter_complex split", command)
	}
	if !strings.Contains(command, "-t 6.078") {
		t.Fatalf("command = %q, want tail trim -t 6.078", command)
	}
}

func TestBuildManifestKillfeedOverlayRequiresKillfeedSourcePreset(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.Preset = PresetCleanPOV60
	opts.HookText = true
	opts.KillfeedOverlay = true

	manifest := BuildManifest(result, opts)
	first := manifest.Shorts[0]
	if first.KillfeedOverlay {
		t.Fatal("KillfeedOverlay = true, want disabled for clean-pov capture")
	}
	for _, effect := range first.Effects {
		if effect.Type == EffectKillfeed {
			t.Fatalf("unexpected killfeed effect %#v for clean-pov preset", effect)
		}
	}
	var hooks int
	for _, effect := range first.Effects {
		if effect.Type == EffectText && effect.Y == "150" {
			hooks++
		}
	}
	if hooks != 1 {
		t.Fatalf("hook effects = %d, want 1 (hook is preset-independent)", hooks)
	}
}

func TestBuildManifestCompiledTailTrimsParts(t *testing.T) {
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.CompileSegments = true
	opts.TailTrimSeconds = 1.5

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	short := manifest.Shorts[0]
	for i, part := range short.Parts {
		lastKill := part.Kills[len(part.Kills)-1].TimeSeconds
		want := roundMillis(lastKill + 1.5)
		if part.DurationSeconds != want {
			t.Fatalf("part[%d] duration = %.3f, want tail-trimmed %.3f", i, part.DurationSeconds, want)
		}
	}
	command := short.FFmpegCommand
	for i, arg := range command {
		if arg == "-t" && i+2 < len(command) && command[i+1] == "6.078" && command[i+2] == "-i" {
			return
		}
	}
	t.Fatalf("command = %v, want input-level -t 6.078 before the first part input", command)
}

func TestBuildManifestCompiledRhythmSkipsTailTrim(t *testing.T) {
	dir := t.TempDir()
	rhythmPath := filepath.Join(dir, "rhythm.json")
	if err := os.WriteFile(rhythmPath, []byte(`{
		"schema_version":"1.0",
		"segment_sync":[
			{"segment_id":"seg-001","timeline_start_seconds":0.500,"gap_before_seconds":0.500},
			{"segment_id":"seg-002","timeline_start_seconds":9.000,"gap_before_seconds":1.000}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.CompileSegments = true
	opts.MusicPath = filepath.Join(dir, "music", "beat.wav")
	opts.RhythmPath = rhythmPath
	opts.TailTrimSeconds = 1.5

	manifest := BuildManifest(result, opts)
	found := false
	for _, warning := range manifest.Warnings {
		if strings.Contains(warning, "tail trim skipped") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want tail trim skipped notice", manifest.Warnings)
	}
	short := manifest.Shorts[0]
	if got := short.Parts[0].DurationSeconds; got != 8 {
		t.Fatalf("part[0] duration = %.3f, want untrimmed 8.000", got)
	}
	for _, arg := range short.FFmpegCommand {
		if arg == "-t" {
			t.Fatalf("command = %v, want no -t under rhythm sync", short.FFmpegCommand)
		}
	}
}
