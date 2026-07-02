package editor

import (
	"os"
	"strings"
	"testing"
)

func TestCommandWithFilterComplexScriptKeepsShortFilterInline(t *testing.T) {
	command := []string{"ffmpeg", "-filter_complex", "scale=1080:1920", "out.mp4"}

	got, cleanup, err := commandWithFilterComplexScript(command)
	if err != nil {
		t.Fatalf("commandWithFilterComplexScript() error = %v", err)
	}
	defer cleanup()

	if strings.Join(got, "\x00") != strings.Join(command, "\x00") {
		t.Fatalf("commandWithFilterComplexScript() = %#v, want original command", got)
	}
}

func TestCommandWithFilterComplexScriptSpillsLongFilter(t *testing.T) {
	filter := strings.Repeat("scale=1080:1920,", filterComplexScriptThreshold)
	command := []string{"ffmpeg", "-filter_complex", filter, "-map", "[v]", "out.mp4"}

	got, cleanup, err := commandWithFilterComplexScript(command)
	if err != nil {
		t.Fatalf("commandWithFilterComplexScript() error = %v", err)
	}

	if got[1] != "-filter_complex_script" {
		t.Fatalf("filter flag = %q, want -filter_complex_script", got[1])
	}
	if got[2] == filter {
		t.Fatalf("filter script path still contains inline filter")
	}
	b, err := os.ReadFile(got[2])
	if err != nil {
		t.Fatalf("read filter script: %v", err)
	}
	if string(b) != filter {
		t.Fatalf("filter script contents changed")
	}

	cleanup()
	if _, err := os.Stat(got[2]); !os.IsNotExist(err) {
		t.Fatalf("filter script still exists after cleanup: %v", err)
	}
}

func singleClipKillfeedShort() ShortEdit {
	return ShortEdit{
		Preset:          PresetViral60Clean,
		Input:           "in.mp4",
		Output:          "out.mp4",
		DurationSeconds: 6.078,
		TailTrimSeconds: 1.5,
		Kills:           []KillCue{{TimeSeconds: 4.578}},
		Effects: []Effect{{
			Type:         EffectKillfeed,
			StartSeconds: 4.228,
			EndSeconds:   6.078,
			AtSeconds:    4.578,
			X:            "W-w-18",
			Y:            "300",
			Width:        430,
			CropX:        1558,
			CropY:        64,
			CropWidth:    360,
			CropHeight:   110,
			Source:       "edit-request",
		}},
	}
}

func TestBuildFFmpegCommandKillfeedUsesFilterComplex(t *testing.T) {
	short := singleClipKillfeedShort()
	command := strings.Join(BuildFFmpegCommand("ffmpeg", short), " ")
	if strings.Contains(command, "-vf") {
		t.Fatalf("command = %q, want no -vf when killfeed overlays are present", command)
	}
	for _, want := range []string{
		"-filter_complex",
		"split=2[main][kfsrc0]",
		"overlay=x=W-w-18:y=300",
		"format=yuv420p[v]",
		"-map [v] -map 0:a?",
		"-t 6.078",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("command = %q, want it to contain %q", command, want)
		}
	}
}

func TestBuildFFmpegCommandWithoutKillfeedKeepsVfPath(t *testing.T) {
	short := singleClipKillfeedShort()
	short.Effects = nil
	short.TailTrimSeconds = 0
	command := BuildFFmpegCommand("ffmpeg", short)
	joined := strings.Join(command, " ")
	if !strings.Contains(joined, "-vf") || strings.Contains(joined, "-filter_complex") {
		t.Fatalf("command = %q, want the historical -vf path", joined)
	}
	for _, arg := range command {
		if arg == "-t" {
			t.Fatalf("command = %q, want no -t without tail trim", joined)
		}
	}
}

func TestBuildMusicFFmpegCommandKillfeedAndTailTrim(t *testing.T) {
	short := singleClipKillfeedShort()
	short.MusicPath = "music.mp3"
	command := BuildFFmpegCommand("ffmpeg", short)
	joined := strings.Join(command, " ")
	for _, want := range []string{
		"split=2[main][kfsrc0]",
		"[0:a]volume=0.20[game]",
		"[1:a]volume=1.00[music]",
		"-t 6.078",
		"-shortest",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("command = %q, want it to contain %q", joined, want)
		}
	}
	if command[len(command)-1] != "out.mp4" || command[len(command)-2] != "-shortest" {
		t.Fatalf("command tail = %v, want ... -shortest out.mp4", command[len(command)-3:])
	}
}

func TestBuildCompilationFFmpegCommandTailTrimsPartInputs(t *testing.T) {
	short := ShortEdit{
		Preset:          PresetViral60Clean,
		Output:          "out.mp4",
		DurationSeconds: 11.078,
		TailTrimSeconds: 1.5,
		Parts: []ShortPart{
			{SegmentID: "seg-001", Input: "p1.mp4", DurationSeconds: 6.078, Kills: []KillCue{{TimeSeconds: 4.578}}},
			{SegmentID: "seg-002", Input: "p2.mp4", DurationSeconds: 5},
		},
	}
	command := strings.Join(BuildCompilationFFmpegCommand("ffmpeg", short), " ")
	if !strings.Contains(command, "-t 6.078 -i p1.mp4") {
		t.Fatalf("command = %q, want the kill part trimmed at input level", command)
	}
	if strings.Contains(command, "-t 5.000") {
		t.Fatalf("command = %q, want no trim on the kill-less part", command)
	}
}
