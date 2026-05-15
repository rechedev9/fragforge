package recording

import (
	"testing"

	"github.com/reche/zackvideo/internal/killplan"
)

func TestAccountIDFromSteamID64(t *testing.T) {
	got, err := AccountIDFromSteamID64("76561198148986856")
	if err != nil {
		t.Fatalf("AccountIDFromSteamID64 error = %v", err)
	}
	if got != 188721128 {
		t.Errorf("account id = %d, want 188721128", got)
	}
}

func TestNewPlanFromKillPlan(t *testing.T) {
	kp := killplan.NewPlan()
	kp.Demo.Tickrate = 64
	kp.Target.SteamID64 = "76561198148986856"
	kp.Segments = []killplan.Segment{
		{ID: "seg-001", TickStart: 22086, TickEnd: 22406},
	}

	plan, err := NewPlanFromKillPlan(kp, `C:\demos\x.dem`, `C:\out`, StreamConfig{})
	if err != nil {
		t.Fatalf("NewPlanFromKillPlan error = %v", err)
	}
	if plan.TargetAccountID != 188721128 {
		t.Errorf("TargetAccountID = %d, want 188721128", plan.TargetAccountID)
	}
	if plan.Stream.Mode != StreamModeFFmpegDirect {
		t.Errorf("Stream.Mode = %q, want %q", plan.Stream.Mode, StreamModeFFmpegDirect)
	}
	if plan.Stream.HUDMode != HUDModeGameplay {
		t.Errorf("Stream.HUDMode = %q, want %q", plan.Stream.HUDMode, HUDModeGameplay)
	}
}

func TestValidateRejectsBadSegment(t *testing.T) {
	p := RecordingPlan{
		DemoPath:        "x.dem",
		OutputDir:       "out",
		TargetAccountID: 1,
		Tickrate:        64,
		Stream:          DefaultStreamConfig(),
		Segments: []RecordingSegment{
			{ID: "seg-001", TickStart: 100, TickEnd: 100},
		},
	}
	if err := p.Validate(); err == nil {
		t.Fatal("Validate error = nil, want error")
	}
}

func TestValidateRejectsUnknownHUDMode(t *testing.T) {
	p := testPlan()
	p.Stream.HUDMode = "weird"
	if err := p.Validate(); err == nil {
		t.Fatal("Validate error = nil, want error")
	}
}
