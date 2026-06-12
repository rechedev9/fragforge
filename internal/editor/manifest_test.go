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

func TestBuildManifestPremiumPlayerIncludesHeadlineAndImage(t *testing.T) {
	t.Skip("short-premium-player preset was removed; viral-60-clean is the only registered preset")
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.Preset = PresetShortPremiumPlayer
	opts.PlayerImagePath = filepath.Join(dir, "martinez.png")

	manifest := BuildManifest(result, opts)
	first := manifest.Shorts[0]
	if manifest.Preset != PresetShortPremiumPlayer {
		t.Fatalf("preset = %q", manifest.Preset)
	}
	if first.PlayerImage != opts.PlayerImagePath {
		t.Fatalf("player image = %q, want %q", first.PlayerImage, opts.PlayerImagePath)
	}
	if first.Headline != "2K con AK-47 en de_ancient" {
		t.Fatalf("headline = %q", first.Headline)
	}
	if got := argAfter(first.FFmpegCommand, "-filter_complex"); !strings.Contains(got, "overlay=x=(W-w)/2:y=H-h+36") {
		t.Fatalf("premium filter missing player overlay:\n%s", got)
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

func TestViralSquareFilterFadesOverlays(t *testing.T) {
	t.Skip("viral-square preset was removed; viral-60-clean is the only registered preset")
	filter := ViralSquareFilter(ShortEdit{
		DurationSeconds: 4,
		Effects: []Effect{
			{
				Type:           EffectKillfeed,
				StartSeconds:   1,
				EndSeconds:     2,
				FadeInSeconds:  0.10,
				FadeOutSeconds: 0.25,
				Width:          430,
				CropX:          1558,
				CropY:          64,
				CropWidth:      360,
				CropHeight:     110,
			},
			{
				Type:           EffectImage,
				Path:           "assets/title.png",
				StartSeconds:   0.5,
				EndSeconds:     3,
				FadeInSeconds:  0.20,
				FadeOutSeconds: 0.30,
				Width:          720,
			},
		},
	})

	for _, want := range []string{
		"fade=t=in:st=1.000:d=0.100:alpha=1",
		"fade=t=out:st=1.750:d=0.250:alpha=1",
		"[1:v]format=rgba,scale=w=720:h=-1:flags=lanczos,loop=loop=-1:size=1:start=0,setpts=N/60/TB,trim=duration=4.000",
		"fade=t=in:st=0.500:d=0.200:alpha=1",
		"fade=t=out:st=2.700:d=0.300:alpha=1",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("viral-square filter missing %q:\n%s", want, filter)
		}
	}
}

func TestPremiumPlayerFilterSupportsChromakey(t *testing.T) {
	t.Skip("short-premium-player preset was removed; viral-60-clean is the only registered preset")
	filter := PremiumPlayerFilter(ShortEdit{
		Label:          "MartinezSa | de_ancient | 2K",
		Headline:       "MartinezSa 2K M4A1-S",
		PrimaryWeapon:  "M4A1-S",
		Player:         "MartinezSa",
		PlayerImage:    "martinez.jpg",
		PlayerKeyColor: "#000000",
	})
	for _, want := range []string{
		"chromakey=0x000000:0.09:0.03",
		"overlay=x=(W-w)/2:y=H-h+36",
		"MartinezSa 2K M4A1-S",
		"MartinezSa",
		"format=yuv420p[v]",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("premium filter missing %q:\n%s", want, filter)
		}
	}
}

func TestBuildManifestViralSquareUsesBlurredLayoutAndExternalEffects(t *testing.T) {
	t.Skip("viral-square preset was removed; viral-60-clean is the only registered preset")
	dir := t.TempDir()
	result := testRecordingResult(dir)
	effectsPath := filepath.Join(dir, "raizerinho.lua")
	if err := os.WriteFile(effectsPath, []byte(`
on_segment(function(s)
  grade({
    saturation = 1.25
  })
  image({
    path = "assets/graffiti-top.png",
    start = 0,
    duration = s.duration,
    x = "(W-w)/2",
    y = 128,
    width = 720
  })
end)
on_kill(function(k)
  killfeed({
    at = k.time,
    pre = 0.2,
    post = 1.2,
    width = 430,
    crop_x = 1558,
    crop_y = 64,
    crop_width = 360,
    crop_height = 110
  })
end)
`), 0o644); err != nil {
		t.Fatal(err)
	}
	opts := testManifestOptions(dir, nil)
	opts.Preset = PresetShortViralSquare
	opts.EffectsPath = effectsPath

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.Preset != PresetShortViralSquare || manifest.EffectsPreset != EffectsPresetExternal {
		t.Fatalf("preset metadata = %#v", manifest)
	}
	if manifest.VideoCRF != NaturalHQVideoCRF || manifest.VideoPreset != NaturalHQVideoPreset {
		t.Fatalf("video encoding = crf %d preset %q", manifest.VideoCRF, manifest.VideoPreset)
	}
	if !manifest.HQFilters || !manifest.AudioNormalize || !manifest.QualityChecks || !manifest.CoverSheets {
		t.Fatalf("viral-square feature flags missing: %#v", manifest)
	}
	short := manifest.Shorts[0]
	filter := argAfter(short.FFmpegCommand, "-filter_complex")
	for _, want := range []string{
		"scale=1080:1920:force_original_aspect_ratio=increase:flags=lanczos",
		"boxblur=24:1",
		"crop=1080:1080:(iw-ow)/2:(ih-oh)/2",
		"overlay=x=0:y=420:format=auto",
		"eq=contrast=1.000:saturation=1.250:gamma=1.000",
		"crop=360:110:1558:64",
		"scale=w=430:h=-1:flags=lanczos",
		"overlay=x=W-w-18:y=438:format=auto",
		"[1:v]format=rgba,scale=w=720:h=-1:flags=lanczos[img0]",
		"overlay=x=(W-w)/2:y=128:format=auto",
		"format=yuv420p[v]",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("viral-square filter missing %q:\n%s", want, filter)
		}
	}
	if got := argAfter(short.FFmpegCommand, "-af"); got != "loudnorm=I=-16:TP=-1.5:LRA=11" {
		t.Fatalf("-af arg = %q, want loudnorm", got)
	}
	if !containsArg(short.FFmpegCommand, "assets/graffiti-top.png") {
		t.Fatalf("ffmpeg command missing image input: %#v", short.FFmpegCommand)
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

func TestBuildManifestNaturalHQDefaultsToNoEffectsAndHighQuality(t *testing.T) {
	t.Skip("natural-hq preset was removed; viral-60-clean is the only registered preset")
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.Preset = PresetShortNaturalHQ

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.Preset != PresetShortNaturalHQ {
		t.Fatalf("preset = %q", manifest.Preset)
	}
	if manifest.EffectsPreset != EffectsPresetNone {
		t.Fatalf("effects preset = %q, want none", manifest.EffectsPreset)
	}
	if manifest.VideoCRF != NaturalHQVideoCRF || manifest.VideoPreset != NaturalHQVideoPreset {
		t.Fatalf("video encoding = crf %d preset %q", manifest.VideoCRF, manifest.VideoPreset)
	}
	first := manifest.Shorts[0]
	if len(first.Effects) != 0 {
		t.Fatalf("natural-hq effects = %#v, want none", first.Effects)
	}
	if got := argAfter(first.FFmpegCommand, "-preset"); got != NaturalHQVideoPreset {
		t.Fatalf("-preset arg = %q, want %q", got, NaturalHQVideoPreset)
	}
	if got := argAfter(first.FFmpegCommand, "-crf"); got != "16" {
		t.Fatalf("-crf arg = %q, want 16", got)
	}
}

func TestBuildManifestNaturalHQ2EnablesQualityFeatures(t *testing.T) {
	t.Skip("natural-hq2 preset was removed; viral-60-clean is the only registered preset")
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.Preset = PresetShortNaturalHQ2

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.Preset != PresetShortNaturalHQ2 || manifest.EffectsPreset != EffectsPresetNone {
		t.Fatalf("preset metadata = %#v", manifest)
	}
	if !manifest.HQFilters || !manifest.AudioNormalize || !manifest.QualityChecks || !manifest.CoverSheets {
		t.Fatalf("hq2 feature flags missing: %#v", manifest)
	}
	first := manifest.Shorts[0]
	if len(first.Effects) != 0 {
		t.Fatalf("natural-hq2 effects = %#v, want none", first.Effects)
	}
	if !first.HQFilters || !first.AudioNormalize {
		t.Fatalf("short hq2 flags missing: %#v", first)
	}
	if first.CoverSheetPath == "" || len(first.CoverSheetCommand) == 0 {
		t.Fatalf("cover sheet missing: %#v", first)
	}
	if first.QualityLogPath == "" || len(first.QualityCommand) == 0 {
		t.Fatalf("quality check missing: %#v", first)
	}
	filter := argAfter(first.FFmpegCommand, "-vf")
	if !strings.Contains(filter, "flags=lanczos") || !strings.Contains(filter, "setsar=1") {
		t.Fatalf("hq2 filter missing lanczos/setsar:\n%s", filter)
	}
	if got := argAfter(first.FFmpegCommand, "-af"); got != "loudnorm=I=-16:TP=-1.5:LRA=11" {
		t.Fatalf("-af arg = %q, want loudnorm", got)
	}
}

func TestBuildManifestNaturalHQ2FullKeepsFullFrameWithVibrance(t *testing.T) {
	t.Skip("natural-hq2-full preset was removed; viral-60-clean is the only registered preset")
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.Preset = PresetShortNaturalHQ2Full

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.Preset != PresetShortNaturalHQ2Full || manifest.EffectsPreset != EffectsPresetNone {
		t.Fatalf("preset metadata = %#v", manifest)
	}
	if !manifest.HQFilters || !manifest.AudioNormalize || !manifest.QualityChecks || !manifest.CoverSheets {
		t.Fatalf("hq2-full feature flags missing: %#v", manifest)
	}
	first := manifest.Shorts[0]
	if len(first.Effects) != 0 {
		t.Fatalf("natural-hq2-full effects = %#v, want none", first.Effects)
	}
	filter := argAfter(first.FFmpegCommand, "-vf")
	for _, want := range []string{
		"scale=w=1080:h=1920:force_original_aspect_ratio=increase:eval=frame:flags=lanczos",
		"crop=1080:1920:(iw-ow)/2:(ih-oh)/2",
		"eq=saturation=1.120",
		"setsar=1",
		"fps=60",
		"format=yuv420p",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("hq2-full filter missing %q:\n%s", want, filter)
		}
	}
	for _, forbidden := range []string{"split=2", "overlay=", "pad=1080:1920", "boxblur"} {
		if strings.Contains(filter, forbidden) {
			t.Fatalf("hq2-full filter should be one continuous gameplay crop without %q:\n%s", forbidden, filter)
		}
	}
	qualityFilter := argAfter(first.QualityCommand, "-vf")
	if strings.Contains(qualityFilter, "cropdetect") {
		t.Fatalf("hq2-full quality check should not flag the full-frame foreground layout:\n%s", qualityFilter)
	}
}

func TestBuildManifestNaturalHQ2FullPlusAppliesEnhancedMastering(t *testing.T) {
	t.Skip("natural-hq2-full-plus preset was removed; viral-60-clean is the only registered preset")
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.Preset = PresetShortNaturalHQ2FullPlus

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.Preset != PresetShortNaturalHQ2FullPlus || manifest.EffectsPreset != EffectsPresetNone {
		t.Fatalf("preset metadata = %#v", manifest)
	}
	if manifest.VideoCRF != NaturalHQ2FullPlusVideoCRF || manifest.VideoPreset != NaturalHQ2FullPlusVideoPreset {
		t.Fatalf("video encoding = crf %d preset %q", manifest.VideoCRF, manifest.VideoPreset)
	}
	first := manifest.Shorts[0]
	if len(first.Effects) != 0 {
		t.Fatalf("natural-hq2-full-plus effects = %#v, want none", first.Effects)
	}
	filter := argAfter(first.FFmpegCommand, "-vf")
	for _, want := range []string{
		"scale=w=1080:h=1920:force_original_aspect_ratio=increase:eval=frame:flags=lanczos+accurate_rnd",
		"crop=1080:1920:(iw-ow)/2:(ih-oh)/2",
		"eq=contrast=1.020:saturation=1.160:gamma=1.000",
		"unsharp=5:5:0.35:3:3:0.15",
		"format=yuv420p",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("hq2-full-plus filter missing %q:\n%s", want, filter)
		}
	}
	for _, forbidden := range []string{"split=2", "overlay=", "pad=1080:1920", "boxblur"} {
		if strings.Contains(filter, forbidden) {
			t.Fatalf("hq2-full-plus filter should be one continuous gameplay crop without %q:\n%s", forbidden, filter)
		}
	}
	if got := argAfter(first.FFmpegCommand, "-preset"); got != NaturalHQ2FullPlusVideoPreset {
		t.Fatalf("-preset arg = %q, want %q", got, NaturalHQ2FullPlusVideoPreset)
	}
	if got := argAfter(first.FFmpegCommand, "-crf"); got != "15" {
		t.Fatalf("-crf arg = %q, want 15", got)
	}
	for _, key := range []string{"-color_primaries", "-color_trc", "-colorspace"} {
		if got := argAfter(first.FFmpegCommand, key); got != "bt709" {
			t.Fatalf("%s arg = %q, want bt709", key, got)
		}
	}
	if got := argAfter(first.FFmpegCommand, "-x264-params"); got != "colorprim=bt709:transfer=bt709:colormatrix=bt709" {
		t.Fatalf("-x264-params arg = %q, want bt709 params", got)
	}
	qualityFilter := argAfter(first.QualityCommand, "-vf")
	if strings.Contains(qualityFilter, "cropdetect") {
		t.Fatalf("hq2-full-plus quality check should not flag expected letterbox bars:\n%s", qualityFilter)
	}
}

func TestBuildManifestSmokeLineupsMatchesCatalogAndAddsOverlay(t *testing.T) {
	t.Skip("smoke-lineups preset was removed; viral-60-clean is the only registered preset")
	dir := t.TempDir()
	result := testSmokeRecordingResult(dir)
	catalogDir := filepath.Join(dir, "lineups")
	if err := os.MkdirAll(catalogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(catalogDir, "de_ancient.smokes.json"), map[string]any{
		"map": "de_ancient",
		"smokes": []map[string]any{
			{
				"id":             "ancient_mid_donut",
				"destination":    "Donut",
				"from_area":      "T Spawn",
				"side":           "T",
				"landing":        []float64{100, 200, 0},
				"landing_radius": 64,
			},
		},
	})
	opts := testManifestOptions(dir, nil)
	opts.Preset = PresetSmokeLineups
	opts.LineupCatalogPath = catalogDir

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.Preset != PresetSmokeLineups || manifest.EffectsPreset != EffectsPresetSmokeLineups {
		t.Fatalf("preset metadata = %#v", manifest)
	}
	if !manifest.HQFilters || !manifest.AudioNormalize || !manifest.QualityChecks || !manifest.CoverSheets {
		t.Fatalf("smoke-lineups should inherit hq2 features: %#v", manifest)
	}
	if len(manifest.Shorts) != 1 {
		t.Fatalf("shorts len = %d, want 1", len(manifest.Shorts))
	}
	short := manifest.Shorts[0]
	if short.KillCount != 0 || short.SmokeCount != 1 || short.PrimarySmoke != "Donut" {
		t.Fatalf("short smoke metadata = %#v", short)
	}
	if len(short.Smokes) != 1 || !short.Smokes[0].Matched || short.Smokes[0].Destination != "Donut" {
		t.Fatalf("smoke cues = %#v", short.Smokes)
	}
	if !strings.Contains(short.Headline, "T Spawn -> Donut") {
		t.Fatalf("headline = %q", short.Headline)
	}
	if !hasEffect(short.Effects, EffectGrade) || !hasEffect(short.Effects, EffectText) {
		t.Fatalf("smoke grade/overlay effects missing: %#v", short.Effects)
	}
	filter := argAfter(short.FFmpegCommand, "-filter_complex")
	if !strings.Contains(filter, "DONUT SMOKE") || !strings.Contains(filter, "FROM T SPAWN") || !strings.Contains(filter, "STANDING JUMPTHROW") {
		t.Fatalf("smoke filter missing overlay text:\n%s", filter)
	}
	for _, want := range []string{
		"eq=contrast=1.030:saturation=1.240:gamma=1.000",
		"trim=start=1.975:end=4.075",
		"setpts=(PTS-STARTPTS)*2.500",
		"atempo=0.5,atempo=0.800",
		"concat=n=3:v=1:a=0,fps=60,format=yuv420p[v]",
		"loudnorm=I=-16:TP=-1.5:LRA=11[a]",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("smoke filter missing slow-motion part %q:\n%s", want, filter)
		}
	}
}

func TestBuildManifestNaturalHQ3EnablesRealisticMastering(t *testing.T) {
	t.Skip("natural-hq3 preset was removed; viral-60-clean is the only registered preset")
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.Preset = PresetShortNaturalHQ3

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.Preset != PresetShortNaturalHQ3 || manifest.EffectsPreset != EffectsPresetNone {
		t.Fatalf("preset metadata = %#v", manifest)
	}
	if manifest.VideoCRF != NaturalHQ3VideoCRF || manifest.VideoPreset != NaturalHQ3VideoPreset {
		t.Fatalf("video encoding = crf %d preset %q", manifest.VideoCRF, manifest.VideoPreset)
	}
	if !manifest.HQFilters || !manifest.AudioNormalize || !manifest.QualityChecks || !manifest.CoverSheets {
		t.Fatalf("hq3 feature flags missing: %#v", manifest)
	}
	first := manifest.Shorts[0]
	if len(first.Effects) != 0 {
		t.Fatalf("natural-hq3 effects = %#v, want none", first.Effects)
	}
	if first.SourceArtifact.Width != 1920 || first.SourceArtifact.Height != 1080 || first.SourceArtifact.FrameRate != "60/1" {
		t.Fatalf("source artifact missing: %#v", first.SourceArtifact)
	}
	if got := argAfter(first.FFmpegCommand, "-preset"); got != NaturalHQ3VideoPreset {
		t.Fatalf("-preset arg = %q, want %q", got, NaturalHQ3VideoPreset)
	}
	if got := argAfter(first.FFmpegCommand, "-crf"); got != "15" {
		t.Fatalf("-crf arg = %q, want 15", got)
	}
	if got := argAfter(first.FFmpegCommand, "-profile:v"); got != "high" {
		t.Fatalf("-profile:v arg = %q, want high", got)
	}
	for _, key := range []string{"-color_primaries", "-color_trc", "-colorspace"} {
		if got := argAfter(first.FFmpegCommand, key); got != "bt709" {
			t.Fatalf("%s arg = %q, want bt709", key, got)
		}
	}
	if got := argAfter(first.FFmpegCommand, "-x264-params"); got != "colorprim=bt709:transfer=bt709:colormatrix=bt709" {
		t.Fatalf("-x264-params arg = %q, want bt709 params", got)
	}
	filter := argAfter(first.FFmpegCommand, "-vf")
	if !strings.Contains(filter, "flags=lanczos+accurate_rnd") || !strings.Contains(filter, "setsar=1") {
		t.Fatalf("hq3 filter missing accurate scaling/setsar:\n%s", filter)
	}
}

func TestBuildManifestNaturalHQ3SmoothAddsTemporalSmoothing(t *testing.T) {
	t.Skip("natural-hq3-smooth preset was removed; viral-60-clean is the only registered preset")
	dir := t.TempDir()
	result := testRecordingResult(dir)
	opts := testManifestOptions(dir, nil)
	opts.Preset = PresetShortNaturalHQ3Smooth

	manifest := BuildManifest(result, opts)
	if len(manifest.Warnings) != 0 {
		t.Fatalf("warnings = %v", manifest.Warnings)
	}
	if manifest.Preset != PresetShortNaturalHQ3Smooth || !manifest.TemporalSmoothing {
		t.Fatalf("smooth preset metadata = %#v", manifest)
	}
	if manifest.VideoCRF != NaturalHQ3VideoCRF || manifest.VideoPreset != NaturalHQ3VideoPreset {
		t.Fatalf("video encoding = crf %d preset %q", manifest.VideoCRF, manifest.VideoPreset)
	}
	first := manifest.Shorts[0]
	if len(first.Effects) != 0 {
		t.Fatalf("natural-hq3-smooth effects = %#v, want none", first.Effects)
	}
	if !first.TemporalSmoothing {
		t.Fatalf("short temporal smoothing missing: %#v", first)
	}
	filter := argAfter(first.FFmpegCommand, "-vf")
	if !strings.Contains(filter, "flags=lanczos+accurate_rnd") || !strings.Contains(filter, "tmix=frames=2:weights='1 2'") {
		t.Fatalf("smooth filter missing tmix:\n%s", filter)
	}
	if got := argAfter(first.FFmpegCommand, "-x264-params"); got != "colorprim=bt709:transfer=bt709:colormatrix=bt709" {
		t.Fatalf("-x264-params arg = %q, want bt709 params", got)
	}
	if strings.Contains(filter, "drawtext=") {
		t.Fatalf("smooth preset should not add text effects:\n%s", filter)
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
		PlayerImage:   filepath.Join("assets", "players", "martinez.png"),
		Kills: []KillCue{
			{Weapon: "AK-47", Victim: "opponent-one", Headshot: true},
			{Weapon: "AWP", Victim: "opponent-two"},
		},
	})
	for _, want := range []string{"MartinezSa", "de_ancient", "2K", "AK-47", "AWP", "opponent-one", "1 headshot", "Gameplay frame", "Player cutout/reference", "martinez.png", "2K con AK-47 en de_ancient"} {
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
