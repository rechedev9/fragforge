package editor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/recording"
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
	if !strings.Contains(first.Caption, "MartinezSa turns this round on de_ancient into a clean 2K with the AK-47.") {
		t.Fatalf("caption = %q", first.Caption)
	}
}

func TestBuildManifestPremiumPlayerIncludesHeadlineAndImage(t *testing.T) {
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
	if first.Headline != "MartinezSa 2K AK-47" {
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
		"scale=w=-2:h='if(between(t\\,0.720\\,1.720)\\,1998\\,1920)':eval=frame",
		"crop=1080:1920:(iw-ow)/2:(ih-oh)/2",
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

func TestPremiumPlayerFilterSupportsChromakey(t *testing.T) {
	filter := PremiumPlayerFilter(ShortEdit{
		Label:          "MartinezSa | de_ancient | 2K",
		Headline:       "MartinezSa 2K M4A1-S",
		PrimaryWeapon:  "M4A1-S",
		PlayerImage:    "martinez.jpg",
		PlayerKeyColor: "#000000",
	})
	for _, want := range []string{
		"chromakey=0x000000:0.09:0.03",
		"overlay=x=(W-w)/2:y=H-h+36",
		"MartinezSa 2K M4A1-S",
		"M4A1-S",
		"format=yuv420p[v]",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("premium filter missing %q:\n%s", want, filter)
		}
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
	for _, want := range []string{"MartinezSa", "de_ancient", "2K", "AK-47", "AWP", "opponent-one", "1 headshot", "Gameplay frame", "Player cutout/reference", "martinez.png"} {
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
			{SegmentID: "seg-002", Role: "segment", Type: "video", Path: filepath.Join(dir, "recording", "segments", "seg-002.mp4"), DurationSeconds: 5},
			{SegmentID: "seg-001", Role: "segment", Type: "video", Path: filepath.Join(dir, "recording", "segments", "seg-001.mp4"), DurationSeconds: 8},
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
