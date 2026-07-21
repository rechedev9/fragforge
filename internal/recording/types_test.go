package recording

import (
	"strings"
	"testing"

	"github.com/rechedev9/fragforge/internal/killplan"
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
	kp.Demo.Map = "de_ancient"
	kp.Demo.Tickrate = 64
	kp.Target.SteamID64 = "76561198148986856"
	kp.Target.NameInDemo = "MartinezSa"
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
	if plan.DemoMap != "de_ancient" {
		t.Errorf("DemoMap = %q, want de_ancient", plan.DemoMap)
	}
	if plan.TargetNameInDemo != "MartinezSa" {
		t.Errorf("TargetNameInDemo = %q, want MartinezSa", plan.TargetNameInDemo)
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

func TestValidateRejectsInvalidCRF(t *testing.T) {
	p := testPlan()
	p.Stream.CRF = 52
	if err := p.Validate(); err == nil {
		t.Fatal("Validate error = nil, want error")
	}
}

func TestNewPlanPortraitSafeKillfeedDefaults(t *testing.T) {
	for _, hudMode := range []HUDMode{HUDModeDeathnotices, HUDModeGameplay} {
		t.Run(string(hudMode), func(t *testing.T) {
			kp := killplan.NewPlan()
			kp.Demo.Tickrate = 64
			kp.Target.SteamID64 = "76561198148986856"
			kp.Segments = []killplan.Segment{{ID: "seg-001", TickStart: 100, TickEnd: 200}}
			stream := DefaultStreamConfig()
			stream.HUDMode = hudMode
			stream.PortraitSafeKillfeed = true

			plan, err := NewPlanFromKillPlan(kp, "x.dem", "out", stream)
			if err != nil {
				t.Fatalf("NewPlanFromKillPlan error = %v", err)
			}
			if got, want := plan.Stream.DeathnoticeSafeZoneX, defaultDeathnoticeSafeZoneX; got != want {
				t.Fatalf("DeathnoticeSafeZoneX = %.2f, want %.2f", got, want)
			}
			if got, want := plan.Stream.DeathnoticeSafeZoneY, defaultDeathnoticeSafeZoneY; got != want {
				t.Fatalf("DeathnoticeSafeZoneY = %.2f, want %.2f", got, want)
			}
			if got, want := plan.Stream.DeathnoticeLifetime, defaultDeathnoticeLifetimeSeconds; got != want {
				t.Fatalf("DeathnoticeLifetime = %.2f, want %.2f", got, want)
			}
		})
	}
}

func TestValidateRejectsPortraitSafeKillfeedWithCleanHUD(t *testing.T) {
	p := testPlan()
	p.Stream.HUDMode = HUDModeClean
	p.Stream.PortraitSafeKillfeed = true

	err := p.Validate()
	if err == nil || !strings.Contains(err.Error(), "portrait_safe_killfeed") {
		t.Fatalf("Validate error = %v, want portrait_safe_killfeed HUD error", err)
	}
}

func TestValidateRejectsInvalidDeathnoticeLayout(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*RecordingPlan)
	}{
		{name: "safe zone", mutate: func(p *RecordingPlan) { p.Stream.DeathnoticeSafeZoneX = 1.1 }},
		{name: "safe zone y", mutate: func(p *RecordingPlan) { p.Stream.DeathnoticeSafeZoneY = 1.1 }},
		{name: "lifetime", mutate: func(p *RecordingPlan) { p.Stream.DeathnoticeLifetime = 10.1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := testPlan()
			tt.mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatal("Validate error = nil, want error")
			}
		})
	}
}
