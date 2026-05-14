package parser

import (
	"testing"

	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
)

const (
	killerID uint64 = 76561198000000000
	victimID uint64 = 76561198000000001
)

func mkPlayer(id uint64, name string, team common.Team) *common.Player {
	return &common.Player{
		SteamID64: id,
		Name:      name,
		Team:      team,
	}
}

func mkEquipment(t common.EquipmentType, original string) *common.Equipment {
	return &common.Equipment{Type: t, OriginalString: original}
}

func TestBuildRawKillRecordsTargetKill(t *testing.T) {
	killer := mkPlayer(killerID, "MARTINEZSA", common.TeamCounterTerrorists)
	victim := mkPlayer(victimID, "Player2", common.TeamTerrorists)
	e := events.Kill{
		Killer:            killer,
		Victim:            victim,
		Weapon:            mkEquipment(common.EqAWP, "weapon_awp"),
		IsHeadshot:        true,
		PenetratedObjects: 1,
	}

	rk, ok := BuildRawKill(e, gameInfo{Tick: 12345, Round: 5}, killerID, true)
	if !ok {
		t.Fatal("BuildRawKill ok = false, want true")
	}
	if rk.Tick != 12345 {
		t.Errorf("Tick = %d, want 12345", rk.Tick)
	}
	if rk.Round != 5 {
		t.Errorf("Round = %d, want 5", rk.Round)
	}
	if rk.Weapon != "awp" {
		t.Errorf("Weapon = %q, want %q", rk.Weapon, "awp")
	}
	if !rk.Headshot {
		t.Errorf("Headshot = false, want true")
	}
	if !rk.Wallbang {
		t.Errorf("Wallbang = false, want true (PenetratedObjects=1)")
	}
	if rk.Victim.SteamID64 != "76561198000000001" {
		t.Errorf("Victim.SteamID64 = %q, want %q", rk.Victim.SteamID64, "76561198000000001")
	}
	if rk.Victim.TeamAtKill != "T" {
		t.Errorf("Victim.TeamAtKill = %q, want %q", rk.Victim.TeamAtKill, "T")
	}
	if rk.Killer.NameInDemo != "MARTINEZSA" {
		t.Errorf("Killer.NameInDemo = %q, want %q", rk.Killer.NameInDemo, "MARTINEZSA")
	}
}

func TestBuildRawKillSkipsWrongKiller(t *testing.T) {
	wrong := mkPlayer(99999, "Other", common.TeamTerrorists)
	victim := mkPlayer(victimID, "Player2", common.TeamCounterTerrorists)
	e := events.Kill{Killer: wrong, Victim: victim, Weapon: mkEquipment(common.EqAWP, "weapon_awp")}

	_, ok := BuildRawKill(e, gameInfo{Tick: 1, Round: 1}, killerID, true)
	if ok {
		t.Error("BuildRawKill ok = true, want false (killer is not target)")
	}
}

func TestBuildRawKillSkipsNilKiller(t *testing.T) {
	victim := mkPlayer(victimID, "Player2", common.TeamCounterTerrorists)
	e := events.Kill{Killer: nil, Victim: victim, Weapon: mkEquipment(common.EqAWP, "weapon_awp")}

	_, ok := BuildRawKill(e, gameInfo{Tick: 1, Round: 1}, killerID, true)
	if ok {
		t.Error("BuildRawKill ok = true, want false (nil killer)")
	}
}

func TestBuildRawKillSkipsNilVictim(t *testing.T) {
	killer := mkPlayer(killerID, "MARTINEZSA", common.TeamCounterTerrorists)
	e := events.Kill{Killer: killer, Victim: nil, Weapon: mkEquipment(common.EqAWP, "weapon_awp")}

	_, ok := BuildRawKill(e, gameInfo{Tick: 1, Round: 1}, killerID, true)
	if ok {
		t.Error("BuildRawKill ok = true, want false (nil victim)")
	}
}

func TestBuildRawKillExcludesTeamKillsWhenRequested(t *testing.T) {
	killer := mkPlayer(killerID, "MARTINEZSA", common.TeamCounterTerrorists)
	teammate := mkPlayer(victimID, "Friend", common.TeamCounterTerrorists)
	e := events.Kill{Killer: killer, Victim: teammate, Weapon: mkEquipment(common.EqAWP, "weapon_awp")}

	_, ok := BuildRawKill(e, gameInfo{Tick: 1, Round: 1}, killerID, true)
	if ok {
		t.Error("BuildRawKill ok = true, want false (team kill excluded)")
	}

	_, ok2 := BuildRawKill(e, gameInfo{Tick: 1, Round: 1}, killerID, false)
	if !ok2 {
		t.Error("BuildRawKill ok = false with excludeTeamKills=false, want true")
	}
}

func TestWeaponNameFromEquipmentUsesOriginalString(t *testing.T) {
	// OriginalString takes precedence: "weapon_m4a1_silencer" → "m4a1_silencer"
	w := mkEquipment(common.EqM4A1, "weapon_m4a1_silencer")
	if got := weaponName(w); got != "m4a1_silencer" {
		t.Errorf("weaponName(weapon_m4a1_silencer) = %q, want %q", got, "m4a1_silencer")
	}
}

func TestWeaponNameFromEquipmentFallsBackWhenOriginalMissing(t *testing.T) {
	// CS2 demos leave OriginalString empty. The fallback must produce
	// the canonical entity name (matching the game's "weapon_X" entities,
	// i.e. the keys of the library's eqNameToWeapon map).
	tests := map[common.EquipmentType]string{
		common.EqAK47:         "ak47",
		common.EqAWP:          "awp",
		common.EqM4A4:         "m4a1",          // weapon_m4a1 in-game = M4A4
		common.EqM4A1:         "m4a1_silencer", // weapon_m4a1_silencer = M4A1-S
		common.EqDeagle:       "deagle",
		common.EqGlock:        "glock",
		common.EqP2000:        "hkp2000",
		common.EqUSP:          "usp_silencer",
		common.EqDualBerettas: "elite",
		common.EqMP9:          "mp9",
		common.EqMP7:          "mp7",
		common.EqHE:           "hegrenade",
		common.EqFlash:        "flashbang",
		common.EqScout:        "ssg08",
		common.EqSG556:        "sg556",
		common.EqWorld:        "world",
	}
	for typ, want := range tests {
		w := mkEquipment(typ, "")
		got := weaponName(w)
		if got != want {
			t.Errorf("weaponName(%v, no original) = %q, want %q", typ, got, want)
		}
	}
}

func TestWeaponNameNilEquipmentReturnsEmpty(t *testing.T) {
	if got := weaponName(nil); got != "" {
		t.Errorf("weaponName(nil) = %q, want empty", got)
	}
}

func TestTeamLabelMap(t *testing.T) {
	tests := map[common.Team]string{
		common.TeamCounterTerrorists: "CT",
		common.TeamTerrorists:        "T",
		common.TeamSpectators:        "SPEC",
		common.TeamUnassigned:        "",
	}
	for input, want := range tests {
		if got := teamLabel(input); got != want {
			t.Errorf("teamLabel(%v) = %q, want %q", input, got, want)
		}
	}
}
