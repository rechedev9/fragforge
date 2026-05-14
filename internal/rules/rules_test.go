package rules

import (
	"strings"
	"testing"
)

func TestDefaultRules(t *testing.T) {
	r := Default()

	if r.WindowSeconds != 8 {
		t.Errorf("WindowSeconds default = %d, want 8", r.WindowSeconds)
	}
	if r.PreRollSeconds != 3 {
		t.Errorf("PreRollSeconds default = %d, want 3", r.PreRollSeconds)
	}
	if r.PostRollSeconds != 5 {
		t.Errorf("PostRollSeconds default = %d, want 5", r.PostRollSeconds)
	}
	if r.MinKillsInWindow != 1 {
		t.Errorf("MinKillsInWindow default = %d, want 1", r.MinKillsInWindow)
	}
	if !r.ExcludeTeamKills {
		t.Errorf("ExcludeTeamKills default = false, want true")
	}
	if r.IncludeHeadshotOnly {
		t.Errorf("IncludeHeadshotOnly default = true, want false")
	}
	if r.MinRound != 1 {
		t.Errorf("MinRound default = %d, want 1", r.MinRound)
	}
	if r.MaxRound != 0 {
		t.Errorf("MaxRound default = %d, want 0 (no max)", r.MaxRound)
	}
	wantWeapons := []string{"awp", "deagle", "ak47", "m4a1", "m4a1_silencer", "usp_silencer", "glock", "hkp2000"}
	if len(r.Weapons) != len(wantWeapons) {
		t.Fatalf("Weapons default length = %d, want %d", len(r.Weapons), len(wantWeapons))
	}
	for i, w := range wantWeapons {
		if r.Weapons[i] != w {
			t.Errorf("Weapons[%d] = %q, want %q", i, r.Weapons[i], w)
		}
	}
}

func TestLoadEmptyJSON(t *testing.T) {
	r, err := Load(strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("Load({}) error = %v, want nil", err)
	}
	if r.WindowSeconds != 8 {
		t.Errorf("WindowSeconds = %d, want 8 (default)", r.WindowSeconds)
	}
	if len(r.Weapons) == 0 {
		t.Errorf("Weapons empty, expected defaults to be applied")
	}
}

func TestLoadPartialJSONMergesWithDefaults(t *testing.T) {
	r, err := Load(strings.NewReader(`{"window_seconds": 15, "pre_roll_seconds": 2}`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if r.WindowSeconds != 15 {
		t.Errorf("WindowSeconds = %d, want 15 (overridden)", r.WindowSeconds)
	}
	if r.PreRollSeconds != 2 {
		t.Errorf("PreRollSeconds = %d, want 2 (overridden)", r.PreRollSeconds)
	}
	if r.PostRollSeconds != 5 {
		t.Errorf("PostRollSeconds = %d, want 5 (default kept)", r.PostRollSeconds)
	}
	if len(r.Weapons) == 0 {
		t.Errorf("Weapons empty, expected defaults to be kept")
	}
}

func TestLoadEmptyWeaponsRejected(t *testing.T) {
	_, err := Load(strings.NewReader(`{"weapons": []}`))
	if err == nil {
		t.Fatal("Load({weapons:[]}) error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "weapons") {
		t.Errorf("error message %q should mention 'weapons'", err.Error())
	}
}

func TestLoadNegativeWindowRejected(t *testing.T) {
	_, err := Load(strings.NewReader(`{"window_seconds": -1}`))
	if err == nil {
		t.Fatal("Load({window_seconds:-1}) error = nil, want validation error")
	}
}

func TestLoadInvalidJSONRejected(t *testing.T) {
	_, err := Load(strings.NewReader(`{not-json}`))
	if err == nil {
		t.Fatal("Load(not-json) error = nil, want parse error")
	}
}

func TestLoadCustomWeaponsOverridesDefaults(t *testing.T) {
	r, err := Load(strings.NewReader(`{"weapons": ["awp", "scout"]}`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(r.Weapons) != 2 {
		t.Fatalf("Weapons length = %d, want 2", len(r.Weapons))
	}
	if r.Weapons[0] != "awp" || r.Weapons[1] != "scout" {
		t.Errorf("Weapons = %v, want [awp scout]", r.Weapons)
	}
}

func TestLoadMaxRoundSetsValue(t *testing.T) {
	r, err := Load(strings.NewReader(`{"max_round": 12}`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if r.MaxRound != 12 {
		t.Errorf("MaxRound = %d, want 12", r.MaxRound)
	}
}

func TestAllowsWeapon(t *testing.T) {
	r := Default()
	if !r.AllowsWeapon("awp") {
		t.Errorf("AllowsWeapon(awp) = false, want true (in defaults)")
	}
	if r.AllowsWeapon("knife") {
		t.Errorf("AllowsWeapon(knife) = true, want false (not in defaults)")
	}
}

func TestAllowsRound(t *testing.T) {
	r := Rules{MinRound: 3, MaxRound: 10}
	if r.AllowsRound(2) {
		t.Errorf("AllowsRound(2) = true with MinRound=3, want false")
	}
	if !r.AllowsRound(3) {
		t.Errorf("AllowsRound(3) = false with MinRound=3, want true")
	}
	if !r.AllowsRound(10) {
		t.Errorf("AllowsRound(10) = false with MaxRound=10, want true")
	}
	if r.AllowsRound(11) {
		t.Errorf("AllowsRound(11) = true with MaxRound=10, want false")
	}

	rNoMax := Rules{MinRound: 1, MaxRound: 0}
	if !rNoMax.AllowsRound(999) {
		t.Errorf("AllowsRound(999) = false with MaxRound=0, want true (no cap)")
	}
}
