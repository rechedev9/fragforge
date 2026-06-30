package recording

import (
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/killplan"
)

func TestStreamSetupCommandsQuotesRecordName(t *testing.T) {
	plan := testPlan()
	// A double quote in the output path must be escaped, not left to terminate
	// the quoted console argument and inject further MIRV commands.
	plan.OutputDir = `C:\out\evil" ; quit ;`

	cmds := streamSetupCommands(plan)
	nameCmd := ""
	for _, c := range cmds {
		if strings.HasPrefix(c, "mirv_streams record name ") {
			nameCmd = c
			break
		}
	}
	if nameCmd == "" {
		t.Fatal("no 'mirv_streams record name' command generated")
	}
	if !strings.Contains(nameCmd, `\"`) {
		t.Fatalf("record name command did not escape embedded quote: %q", nameCmd)
	}
}

func testPlan() RecordingPlan {
	return RecordingPlan{
		DemoPath:         `C:\demos\x.dem`,
		OutputDir:        `C:\out`,
		TargetSteamID64:  "76561198148986856",
		TargetNameInDemo: "maaryy",
		TargetAccountID:  188721128,
		Tickrate:         64,
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
		`cl_demo_predict 0`,
		`cl_trueview_show_status 0`,
		`mirv_panorama panelstyle panelId=trueview_row opacity=0`,
		`spec_player \"maaryy\"`,
		`spec_autodirector 0; spec_mode 2; spec_player \"maaryy\"`,
		`camera-warmup-seg-001`,
		`camera-lead-3s-seg-001`,
		`camera-lead-2s-seg-001`,
		`camera-lead-1s-seg-001`,
		`camera-lock-seg-001`,
		`camera-relock-seg-001`,
		// Seeks are driven by the runtime (re-issued until they land), declared as
		// targets in the seeks array rather than one-shot demo_gototick commands.
		`const seeks = `,
		"\"target\": 21766",
		"\"target\": 31426",
		"mirv.exec(`demo_gototick ${s.target}`)",
		`if (tick <= 0) return;`,
		`demoui`,
		`mirv_streams record fps 60`,
		`mirv_streams record screen enabled 1`,
		`mirv_streams settings add ffmpeg zvFfmpegYuv420pCrf18`,
		`-crf 18`,
		`mirv_streams record screen settings zvFfmpegYuv420pCrf18`,
		`disconnect; quit`,
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

func TestGenerateHLAEJavaScriptUsesConfiguredCRF(t *testing.T) {
	p := testPlan()
	p.Stream.CRF = 16
	js, err := GenerateHLAEJavaScript(p)
	if err != nil {
		t.Fatalf("GenerateHLAEJavaScript error = %v", err)
	}
	for _, want := range []string{
		`mirv_streams settings add ffmpeg zvFfmpegYuv420pCrf16`,
		`-crf 16`,
		`mirv_streams record screen settings zvFfmpegYuv420pCrf16`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("generated JS missing %q\n%s", want, js)
		}
	}
	if strings.Contains(js, `afxFfmpegYuv420p`) {
		t.Fatalf("generated JS should use the CRF-specific preset:\n%s", js)
	}
}

func TestEffectiveRecordStartTickAllowsCameraToSettleBeforeFirstKill(t *testing.T) {
	segment := RecordingSegment{
		ID:        "seg-001",
		TickStart: 14029,
		TickEnd:   14770,
		Kills: []killplan.Kill{
			{Tick: 14221},
			{Tick: 14450},
		},
	}
	if got, want := effectiveRecordStartTick(segment, 64), 14157; got != want {
		t.Fatalf("effectiveRecordStartTick() = %d, want %d", got, want)
	}

	segment.Kills = nil
	if got, want := effectiveRecordStartTick(segment, 64), segment.TickStart; got != want {
		t.Fatalf("effectiveRecordStartTick() without kills = %d, want %d", got, want)
	}
}

func TestCameraCommandFallsBackToAccountID(t *testing.T) {
	if got := cameraCommand("", 188721128); !strings.Contains(got, `spec_player_by_accountid 188721128`) {
		t.Fatalf("cameraCommand() = %q, want account-id fallback", got)
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

func TestGenerateHLAEJavaScriptDeathnoticesHUDMode(t *testing.T) {
	p := testPlan()
	p.Stream.HUDMode = HUDModeDeathnotices
	js, err := GenerateHLAEJavaScript(p)
	if err != nil {
		t.Fatalf("GenerateHLAEJavaScript error = %v", err)
	}
	for _, want := range []string{
		`cl_drawhud 1`,
		`cl_draw_only_deathnotices 1`,
		`cl_show_observer_crosshair 2`,
		`crosshair 1`,
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("generated JS missing %q\n%s", want, js)
		}
	}
	if strings.Contains(js, `cl_drawhud 0`) || strings.Contains(js, `cl_draw_only_deathnotices 0`) {
		t.Fatalf("deathnotices mode should keep death notices visible:\n%s", js)
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
