package recording

import (
	"strings"
	"testing"
)

func testPlan() RecordingPlan {
	return RecordingPlan{
		DemoPath:        `C:\demos\x.dem`,
		OutputDir:       `C:\out`,
		TargetSteamID64: "76561198148986856",
		TargetAccountID: 188721128,
		Tickrate:        64,
		Segments: []RecordingSegment{
			{ID: "seg-001", TickStart: 22086, TickEnd: 22406},
			{ID: "seg-002", TickStart: 31746, TickEnd: 32258},
		},
		Stream:  DefaultStreamConfig(),
		Runtime: RuntimeConfig{QuitTickPad: 200},
	}
}

func TestGenerateHLAEJavaScriptUsesOneShotTickSchedule(t *testing.T) {
	js, err := GenerateHLAEJavaScript(testPlan())
	if err != nil {
		t.Fatalf("GenerateHLAEJavaScript error = %v", err)
	}
	for _, want := range []string{
		`mirv.events.clientFrameStageNotify.on`,
		`mirv.getDemoTick()`,
		`tick >= item.tick`,
		`fired[item.key] = true`,
		`spec_player_by_accountid 188721128`,
		`spec_player_by_accountid 188721128; spec_mode 4`,
		`camera-lock-seg-001`,
		`camera-relock-seg-001`,
		`demo_gototick 21958`,
		`demo_gototick 31618`,
		`demoui`,
		`mirv_streams record fps 60`,
		`mirv_streams record screen enabled 1`,
		`mirv_streams record screen settings afxFfmpegYuv420p`,
		`spec_show_xray 0`,
		`cl_spec_show_bindings 0`,
		`cl_drawhud 1`,
		`cl_draw_only_deathnotices 0`,
		`cl_show_observer_crosshair 2`,
		`crosshair 1`,
		`mirv_streams record start`,
		`mirv_streams record end`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("generated JS missing %q\n%s", want, js)
		}
	}
}

func TestGenerateHLAEJavaScriptGameplayHUDIsDefault(t *testing.T) {
	p := testPlan()
	p.Stream.HUDMode = ""
	js, err := GenerateHLAEJavaScript(p)
	if err != nil {
		t.Fatalf("GenerateHLAEJavaScript error = %v", err)
	}
	if !strings.Contains(js, `cl_drawhud 1`) {
		t.Fatalf("generated JS missing gameplay HUD:\n%s", js)
	}
	if strings.Contains(js, `cl_drawhud 0`) {
		t.Fatalf("generated JS hides HUD in default mode:\n%s", js)
	}
}

func TestGenerateHLAEJavaScriptCleanHUDMode(t *testing.T) {
	p := testPlan()
	p.Stream.HUDMode = HUDModeClean
	js, err := GenerateHLAEJavaScript(p)
	if err != nil {
		t.Fatalf("GenerateHLAEJavaScript error = %v", err)
	}
	if !strings.Contains(js, `spec_show_xray 0; cl_drawhud 0`) {
		t.Fatalf("generated JS missing clean HUD command:\n%s", js)
	}
	if strings.Contains(js, `cl_draw_only_deathnotices 0`) {
		t.Fatalf("clean mode should not enable gameplay HUD commands:\n%s", js)
	}
}

func TestGenerateHLAEJavaScriptEscapesCommandsViaJSON(t *testing.T) {
	p := testPlan()
	p.OutputDir = `C:\Users\name with spaces\out`
	js, err := GenerateHLAEJavaScript(p)
	if err != nil {
		t.Fatalf("GenerateHLAEJavaScript error = %v", err)
	}
	if !strings.Contains(js, `C:/Users/name with spaces/out`) {
		t.Errorf("generated JS should use slash-normalized output path:\n%s", js)
	}
	if strings.Contains(js, `\Users`) {
		t.Errorf("generated JS contains unescaped Windows backslashes:\n%s", js)
	}
}

func TestGenerateHLAEJavaScriptTimescale(t *testing.T) {
	p := testPlan()
	p.Runtime.HostTimescale = 2
	js, err := GenerateHLAEJavaScript(p)
	if err != nil {
		t.Fatalf("GenerateHLAEJavaScript error = %v", err)
	}
	if !strings.Contains(js, `host_timescale 2`) || !strings.Contains(js, `host_timescale 1`) {
		t.Errorf("generated JS missing host_timescale wrapper:\n%s", js)
	}
}
