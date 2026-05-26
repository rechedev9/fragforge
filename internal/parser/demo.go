package parser

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/golang/geo/r3"
	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
)

// ErrTargetNotFound is returned when the requested target SteamID was never
// observed in the demo (neither as killer nor as victim). The CLI maps this
// to exit code 5.
var ErrTargetNotFound = errors.New("target steamid not found in demo")

type SegmentMode string

const (
	SegmentModeKills   SegmentMode = "kills"
	SegmentModeSmokes  SegmentMode = "smokes"
	SegmentModeUtility SegmentMode = "utility"
)

type RunOptions struct {
	SegmentMode SegmentMode
}

// gameInfo carries the per-event game state needed to convert a demoinfocs
// Kill into a RawKill. It exists so BuildRawKill can be tested without a live
// parser.
type gameInfo struct {
	Tick  int
	Round int
}

// BuildRawKill converts a demoinfocs Kill event into a RawKill, applying the
// target-match and team-kill checks. The second return value reports whether
// the event should be recorded; weapon/headshot/round filters are applied
// later by the Collector.
func BuildRawKill(e events.Kill, gi gameInfo, targetID uint64, excludeTeamKills bool) (RawKill, bool) {
	if e.Killer == nil || e.Killer.SteamID64 != targetID {
		return RawKill{}, false
	}
	if e.Victim == nil {
		return RawKill{}, false
	}
	if excludeTeamKills && e.Killer.Team == e.Victim.Team {
		return RawKill{}, false
	}
	return RawKill{
		Tick:     gi.Tick,
		Round:    gi.Round,
		Weapon:   weaponName(e.Weapon),
		Headshot: e.IsHeadshot,
		Wallbang: e.PenetratedObjects > 0,
		Killer: killplan.Player{
			SteamID64:  strconv.FormatUint(e.Killer.SteamID64, 10),
			NameInDemo: e.Killer.Name,
			TeamAtKill: teamLabel(e.Killer.Team),
		},
		Victim: killplan.Player{
			SteamID64:  strconv.FormatUint(e.Victim.SteamID64, 10),
			NameInDemo: e.Victim.Name,
			TeamAtKill: teamLabel(e.Victim.Team),
		},
		KillerPos: playerPosition(e.Killer),
		VictimPos: playerPosition(e.Victim),
	}, true
}

func weaponName(w *common.Equipment) string {
	if w == nil {
		return ""
	}
	// CS:GO demos populate Equipment.OriginalString with the in-game entity
	// name (e.g. "weapon_m4a1_silencer"); strip the prefix and use it.
	if w.OriginalString != "" {
		return strings.TrimPrefix(w.OriginalString, "weapon_")
	}
	// CS2 demos leave OriginalString empty. Fall back to a hand-curated
	// EquipmentType → canonical entity-name map. The strings match the
	// keys of demoinfocs' internal eqNameToWeapon table so the rest of
	// the pipeline (rules JSON, filters) uses a single vocabulary.
	if n, ok := equipmentNames[w.Type]; ok {
		return n
	}
	return strings.ToLower(w.Type.String())
}

// equipmentNames maps EquipmentType to the canonical lowercase entity name
// expected by zackvideo rules (matching the game's "weapon_X" entity names
// used in CS:GO demos and the demoinfocs reverse lookup).
var equipmentNames = map[common.EquipmentType]string{
	// Pistols
	common.EqP2000:        "hkp2000",
	common.EqGlock:        "glock",
	common.EqP250:         "p250",
	common.EqDeagle:       "deagle",
	common.EqFiveSeven:    "fiveseven",
	common.EqDualBerettas: "elite",
	common.EqTec9:         "tec9",
	common.EqCZ:           "cz75a",
	common.EqUSP:          "usp_silencer",
	common.EqRevolver:     "revolver",
	// SMGs
	common.EqMP7:   "mp7",
	common.EqMP9:   "mp9",
	common.EqBizon: "bizon",
	common.EqMac10: "mac10",
	common.EqUMP:   "ump45",
	common.EqP90:   "p90",
	common.EqMP5:   "mp5sd",
	// Heavy
	common.EqSawedOff: "sawedoff",
	common.EqNova:     "nova",
	common.EqSwag7:    "mag7",
	common.EqXM1014:   "xm1014",
	common.EqM249:     "m249",
	common.EqNegev:    "negev",
	// Rifles
	common.EqGalil:  "galilar",
	common.EqFamas:  "famas",
	common.EqAK47:   "ak47",
	common.EqM4A4:   "m4a1", // weapon_m4a1 = M4A4
	common.EqM4A1:   "m4a1_silencer",
	common.EqScout:  "ssg08",
	common.EqSG556:  "sg556",
	common.EqAUG:    "aug",
	common.EqAWP:    "awp",
	common.EqScar20: "scar20",
	common.EqG3SG1:  "g3sg1",
	// Grenades
	common.EqDecoy:      "decoy",
	common.EqMolotov:    "molotov",
	common.EqIncendiary: "incgrenade",
	common.EqFlash:      "flashbang",
	common.EqSmoke:      "smokegrenade",
	common.EqHE:         "hegrenade",
	// Equipment
	common.EqZeus:  "taser",
	common.EqKnife: "knife",
	common.EqBomb:  "c4",
	common.EqWorld: "world",
}

func teamLabel(t common.Team) string {
	switch t {
	case common.TeamCounterTerrorists:
		return "CT"
	case common.TeamTerrorists:
		return "T"
	case common.TeamSpectators:
		return "SPEC"
	default:
		return ""
	}
}

func playerPosition(p *common.Player) [3]float64 {
	if p == nil {
		return [3]float64{}
	}
	v := p.Position()
	return [3]float64{v.X, v.Y, v.Z}
}

// Run wires kill event handlers on p, drives the parser to completion, and
// returns the assembled kill plan. The passed PlanMeta supplies demo path
// and SHA256; map name, tickrate, and duration are filled in from the
// parser unless already provided.
func Run(p demoinfocs.Parser, target string, r rules.Rules, m PlanMeta) (killplan.Plan, error) {
	return RunWithOptions(p, target, r, m, RunOptions{SegmentMode: SegmentModeKills})
}

func RunWithOptions(p demoinfocs.Parser, target string, r rules.Rules, m PlanMeta, opts RunOptions) (killplan.Plan, error) {
	switch opts.SegmentMode {
	case "", SegmentModeKills:
		return runKills(p, target, r, m)
	case SegmentModeSmokes:
		return runSmokes(p, target, r, m)
	case SegmentModeUtility:
		return runUtility(p, target, r, m)
	default:
		return killplan.Plan{}, fmt.Errorf("unknown segment mode %q", opts.SegmentMode)
	}
}

func runKills(p demoinfocs.Parser, target string, r rules.Rules, m PlanMeta) (killplan.Plan, error) {
	targetID, err := strconv.ParseUint(target, 10, 64)
	if err != nil {
		return killplan.Plan{}, fmt.Errorf("invalid target steamid %q: %w", target, err)
	}

	c := NewCollector(target, r)
	var mapName string
	var maxTick int

	p.RegisterNetMessageHandler(func(info *msg.CSVCMsg_ServerInfo) {
		if name := info.GetMapName(); name != "" {
			mapName = name
		}
	})

	p.RegisterEventHandler(func(e events.Kill) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		gi := gameInfo{Tick: tick, Round: gs.TotalRoundsPlayed() + 1}

		if e.Killer != nil && e.Killer.SteamID64 == targetID {
			c.RecordTargetIdentity(e.Killer.Name, teamLabel(e.Killer.Team))
		} else if e.Victim != nil && e.Victim.SteamID64 == targetID {
			c.RecordTargetIdentity(e.Victim.Name, teamLabel(e.Victim.Team))
			return
		}

		rk, ok := BuildRawKill(e, gi, targetID, r.ExcludeTeamKills)
		if !ok {
			return
		}
		c.RecordKill(rk)
	})

	p.RegisterEventHandler(func(events.RoundEnd) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		c.RecordRoundEnd(RoundEnd{Round: gs.TotalRoundsPlayed() + 1, Tick: tick})
	})

	if err := p.ParseToEnd(); err != nil {
		return killplan.Plan{}, fmt.Errorf("parsing demo: %w", err)
	}

	if m.Tickrate <= 0 {
		m.Tickrate = int(p.TickRate())
	}
	if m.Map == "" {
		m.Map = mapName
	}
	if m.DurationTicks <= 0 {
		m.DurationTicks = maxTick
	}

	plan, err := c.Build(m)
	if err != nil {
		// Translate the collector's "target not seen" error into the sentinel
		// so the CLI can map it to its exit code.
		if strings.Contains(err.Error(), "not found in demo") {
			return killplan.Plan{}, ErrTargetNotFound
		}
		return killplan.Plan{}, err
	}
	return plan, nil
}

func runSmokes(p demoinfocs.Parser, target string, r rules.Rules, m PlanMeta) (killplan.Plan, error) {
	targetID, err := strconv.ParseUint(target, 10, 64)
	if err != nil {
		return killplan.Plan{}, fmt.Errorf("invalid target steamid %q: %w", target, err)
	}

	c := NewSmokeCollector(target, r)
	var mapName string
	var maxTick int
	var pending []*RawUtilityThrow
	byEntityID := map[int]*RawUtilityThrow{}
	byUniqueID := map[int64]*RawUtilityThrow{}

	p.RegisterNetMessageHandler(func(info *msg.CSVCMsg_ServerInfo) {
		if name := info.GetMapName(); name != "" {
			mapName = name
		}
	})

	p.RegisterEventHandler(func(e events.WeaponFire) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		if e.Shooter == nil || e.Shooter.SteamID64 != targetID || !isSmokeEquipment(e.Weapon) {
			return
		}
		c.RecordTargetIdentity(e.Shooter.Name, teamLabel(e.Shooter.Team))
		smoke := &RawUtilityThrow{
			Type:       SmokeGrenadeType,
			Round:      gs.TotalRoundsPlayed() + 1,
			ThrowTick:  tick,
			Thrower:    playerIdentity(e.Shooter),
			ThrowPos:   playerPosition(e.Shooter),
			ThrowPlace: safeLastPlaceName(e.Shooter),
		}
		applyThrowState(smoke, e.Shooter, tick, "weapon_fire")
		pending = append(pending, smoke)
	})

	p.RegisterEventHandler(func(e events.GrenadeProjectileThrow) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		projectile := e.Projectile
		if !isSmokeProjectile(projectile) || projectile.Thrower == nil || projectile.Thrower.SteamID64 != targetID {
			return
		}
		thrower := projectile.Thrower
		c.RecordTargetIdentity(thrower.Name, teamLabel(thrower.Team))
		smoke := findRecentPendingWeaponFire(pending, thrower, tick, int(p.TickRate()))
		if smoke == nil {
			smoke = &RawUtilityThrow{
				Type:       SmokeGrenadeType,
				Round:      gs.TotalRoundsPlayed() + 1,
				ThrowTick:  tick,
				Thrower:    playerIdentity(thrower),
				ThrowPos:   playerPosition(thrower),
				ThrowPlace: safeLastPlaceName(thrower),
			}
			applyThrowState(smoke, thrower, tick, "projectile_throw")
			pending = append(pending, smoke)
		}
		applyThrowState(smoke, thrower, tick, "projectile_throw")
		smoke.LandingPos = projectilePosition(projectile)
		smoke.LandingSource = "projectile_spawn"
		byUniqueID[projectile.UniqueID()] = smoke
		if entityID := projectileEntityID(projectile); entityID != 0 {
			byEntityID[entityID] = smoke
		}
	})

	p.RegisterEventHandler(func(e events.SmokeStart) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		if e.GrenadeType != common.EqSmoke {
			return
		}
		smoke := findPendingSmoke(byEntityID, pending, e.GrenadeEntityID, e.Thrower, targetID, tick, 12*int(p.TickRate()))
		if smoke == nil {
			if e.Thrower == nil || e.Thrower.SteamID64 != targetID {
				return
			}
			c.RecordTargetIdentity(e.Thrower.Name, teamLabel(e.Thrower.Team))
			smoke = &RawUtilityThrow{
				Type:       SmokeGrenadeType,
				Round:      gs.TotalRoundsPlayed() + 1,
				ThrowTick:  tick,
				Thrower:    playerIdentity(e.Thrower),
				ThrowPos:   playerPosition(e.Thrower),
				ThrowPlace: safeLastPlaceName(e.Thrower),
			}
			applyThrowState(smoke, e.Thrower, tick, "smoke_start")
			pending = append(pending, smoke)
			if e.GrenadeEntityID != 0 {
				byEntityID[e.GrenadeEntityID] = smoke
			}
		}
		smoke.PopTick = tick
		smoke.LandingPos = vectorPosition(e.Position)
		smoke.LandingSource = "smoke_start"
	})

	p.RegisterEventHandler(func(e events.SmokeExpired) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		if e.GrenadeType != common.EqSmoke {
			return
		}
		if smoke := findPendingSmoke(byEntityID, pending, e.GrenadeEntityID, e.Thrower, targetID, tick, 35*int(p.TickRate())); smoke != nil {
			smoke.ExpireTick = tick
		}
	})

	p.RegisterEventHandler(func(e events.GrenadeProjectileDestroy) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		projectile := e.Projectile
		if !isSmokeProjectile(projectile) {
			return
		}
		smoke := byUniqueID[projectile.UniqueID()]
		if smoke == nil {
			smoke = byEntityID[projectileEntityID(projectile)]
		}
		if smoke == nil || smoke.Thrower.SteamID64 != target {
			return
		}
		if smoke.LandingSource != "" && smoke.LandingSource != "projectile_spawn" {
			return
		}
		if last, ok := lastTrajectoryPosition(projectile); ok {
			smoke.LandingPos = last
			smoke.LandingSource = "projectile_destroy_trajectory"
		} else {
			smoke.LandingPos = projectilePosition(projectile)
			smoke.LandingSource = "projectile_destroy_position"
		}
		if smoke.PopTick == 0 {
			smoke.PopTick = tick
		}
	})

	p.RegisterEventHandler(func(events.RoundEnd) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		c.RecordRoundEnd(RoundEnd{Round: gs.TotalRoundsPlayed() + 1, Tick: tick})
	})

	if err := p.ParseToEnd(); err != nil {
		return killplan.Plan{}, fmt.Errorf("parsing demo: %w", err)
	}

	for _, smoke := range pending {
		c.RecordSmoke(*smoke)
	}

	if m.Tickrate <= 0 {
		m.Tickrate = int(p.TickRate())
	}
	if m.Map == "" {
		m.Map = mapName
	}
	if m.DurationTicks <= 0 {
		m.DurationTicks = maxTick
	}

	plan, err := c.Build(m)
	if err != nil {
		if strings.Contains(err.Error(), "not found in demo") {
			return killplan.Plan{}, ErrTargetNotFound
		}
		return killplan.Plan{}, err
	}
	return plan, nil
}

func runUtility(p demoinfocs.Parser, target string, r rules.Rules, m PlanMeta) (killplan.Plan, error) {
	targetID, err := strconv.ParseUint(target, 10, 64)
	if err != nil {
		return killplan.Plan{}, fmt.Errorf("invalid target steamid %q: %w", target, err)
	}

	c := NewUtilityCollector(target, r)
	var mapName string
	var maxTick int
	var pending []*RawUtilityThrow
	byEntityID := map[int]*RawUtilityThrow{}
	byUniqueID := map[int64]*RawUtilityThrow{}

	p.RegisterNetMessageHandler(func(info *msg.CSVCMsg_ServerInfo) {
		if name := info.GetMapName(); name != "" {
			mapName = name
		}
	})

	p.RegisterEventHandler(func(e events.WeaponFire) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		if e.Shooter == nil || e.Shooter.SteamID64 != targetID || !isTrackedUtilityEquipment(e.Weapon) {
			return
		}
		c.RecordTargetIdentity(e.Shooter.Name, teamLabel(e.Shooter.Team))
		u := &RawUtilityThrow{
			Type:       utilityTypeFromEquipment(e.Weapon),
			Round:      gs.TotalRoundsPlayed() + 1,
			ThrowTick:  tick,
			Thrower:    playerIdentity(e.Shooter),
			ThrowPos:   playerPosition(e.Shooter),
			ThrowPlace: safeLastPlaceName(e.Shooter),
		}
		applyThrowState(u, e.Shooter, tick, "weapon_fire")
		pending = append(pending, u)
	})

	p.RegisterEventHandler(func(e events.GrenadeProjectileThrow) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		projectile := e.Projectile
		if !isTrackedUtilityProjectile(projectile) || projectile.Thrower == nil || projectile.Thrower.SteamID64 != targetID {
			return
		}
		thrower := projectile.Thrower
		c.RecordTargetIdentity(thrower.Name, teamLabel(thrower.Team))
		typ := utilityTypeFromEquipment(projectile.WeaponInstance)
		u := findRecentPendingUtility(pending, thrower, typ, tick, int(p.TickRate()))
		if u == nil {
			u = &RawUtilityThrow{
				Type:       typ,
				Round:      gs.TotalRoundsPlayed() + 1,
				ThrowTick:  tick,
				Thrower:    playerIdentity(thrower),
				ThrowPos:   playerPosition(thrower),
				ThrowPlace: safeLastPlaceName(thrower),
			}
			applyThrowState(u, thrower, tick, "projectile_throw")
			pending = append(pending, u)
		}
		applyThrowState(u, thrower, tick, "projectile_throw")
		u.LandingPos = projectilePosition(projectile)
		u.LandingSource = "projectile_spawn"
		byUniqueID[projectile.UniqueID()] = u
		if entityID := projectileEntityID(projectile); entityID != 0 {
			byEntityID[entityID] = u
		}
	})

	p.RegisterEventHandler(func(e events.FlashExplode) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		if e.GrenadeType != common.EqFlash {
			return
		}
		u := findPendingUtility(byEntityID, pending, e.GrenadeEntityID, e.Thrower, targetID, FlashbangType, tick, 4*int(p.TickRate()))
		if u == nil {
			if e.Thrower == nil || e.Thrower.SteamID64 != targetID {
				return
			}
			c.RecordTargetIdentity(e.Thrower.Name, teamLabel(e.Thrower.Team))
			u = &RawUtilityThrow{
				Type:       FlashbangType,
				Round:      gs.TotalRoundsPlayed() + 1,
				ThrowTick:  tick,
				Thrower:    playerIdentity(e.Thrower),
				ThrowPos:   playerPosition(e.Thrower),
				ThrowPlace: safeLastPlaceName(e.Thrower),
			}
			applyThrowState(u, e.Thrower, tick, "flash_explode")
			pending = append(pending, u)
		}
		applyThrowState(u, e.Thrower, tick, "flash_explode")
		u.PopTick = tick
		u.LandingPos = vectorPosition(e.Position)
		u.LandingSource = "flash_explode"
	})

	p.RegisterEventHandler(func(e events.SmokeStart) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		if e.GrenadeType != common.EqSmoke {
			return
		}
		u := findPendingUtility(byEntityID, pending, e.GrenadeEntityID, e.Thrower, targetID, SmokeGrenadeType, tick, 12*int(p.TickRate()))
		if u == nil {
			if e.Thrower == nil || e.Thrower.SteamID64 != targetID {
				return
			}
			c.RecordTargetIdentity(e.Thrower.Name, teamLabel(e.Thrower.Team))
			u = &RawUtilityThrow{
				Type:       SmokeGrenadeType,
				Round:      gs.TotalRoundsPlayed() + 1,
				ThrowTick:  tick,
				Thrower:    playerIdentity(e.Thrower),
				ThrowPos:   playerPosition(e.Thrower),
				ThrowPlace: safeLastPlaceName(e.Thrower),
			}
			applyThrowState(u, e.Thrower, tick, "smoke_start")
			pending = append(pending, u)
			if e.GrenadeEntityID != 0 {
				byEntityID[e.GrenadeEntityID] = u
			}
		}
		applyThrowState(u, e.Thrower, tick, "smoke_start")
		u.PopTick = tick
		u.LandingPos = vectorPosition(e.Position)
		u.LandingSource = "smoke_start"
	})

	p.RegisterEventHandler(func(e events.SmokeExpired) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		if e.GrenadeType != common.EqSmoke {
			return
		}
		if u := findPendingUtility(byEntityID, pending, e.GrenadeEntityID, e.Thrower, targetID, SmokeGrenadeType, tick, 35*int(p.TickRate())); u != nil {
			u.ExpireTick = tick
		}
	})

	p.RegisterEventHandler(func(e events.InfernoStart) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		if e.Inferno == nil || e.Inferno.Thrower() == nil || e.Inferno.Thrower().SteamID64 != targetID {
			return
		}
		thrower := e.Inferno.Thrower()
		c.RecordTargetIdentity(thrower.Name, teamLabel(thrower.Team))
		u := findRecentPendingAnyUtility(pending, thrower, []string{MolotovType, IncendiaryGrenadeType}, tick, int(p.TickRate()))
		if u == nil {
			u = &RawUtilityThrow{
				Type:       MolotovType,
				Round:      gs.TotalRoundsPlayed() + 1,
				ThrowTick:  tick,
				Thrower:    playerIdentity(thrower),
				ThrowPos:   playerPosition(thrower),
				ThrowPlace: safeLastPlaceName(thrower),
			}
			applyThrowState(u, thrower, tick, "inferno_start")
			pending = append(pending, u)
		}
		applyThrowState(u, thrower, tick, "inferno_start")
		u.PopTick = tick
		if pos, ok := infernoCenter(e.Inferno); ok {
			u.LandingPos = pos
			u.LandingSource = "inferno_center"
		}
	})

	p.RegisterEventHandler(func(e events.InfernoExpired) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		if e.Inferno == nil || e.Inferno.Thrower() == nil || e.Inferno.Thrower().SteamID64 != targetID {
			return
		}
		if u := findRecentPendingAnyUtility(pending, e.Inferno.Thrower(), []string{MolotovType, IncendiaryGrenadeType}, tick, 12*int(p.TickRate())); u != nil {
			u.ExpireTick = tick
		}
	})

	p.RegisterEventHandler(func(e events.GrenadeProjectileDestroy) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		projectile := e.Projectile
		if !isTrackedUtilityProjectile(projectile) {
			return
		}
		u := byUniqueID[projectile.UniqueID()]
		if u == nil {
			u = byEntityID[projectileEntityID(projectile)]
		}
		if u == nil || u.Thrower.SteamID64 != target {
			return
		}
		if u.LandingSource != "" && u.LandingSource != "projectile_spawn" {
			return
		}
		if last, ok := lastTrajectoryPosition(projectile); ok {
			u.LandingPos = last
			u.LandingSource = "projectile_destroy_trajectory"
		} else {
			u.LandingPos = projectilePosition(projectile)
			u.LandingSource = "projectile_destroy_position"
		}
		if u.PopTick == 0 {
			u.PopTick = tick
		}
	})

	p.RegisterEventHandler(func(events.RoundEnd) {
		gs := p.GameState()
		tick := gs.IngameTick()
		if tick > maxTick {
			maxTick = tick
		}
		c.RecordRoundEnd(RoundEnd{Round: gs.TotalRoundsPlayed() + 1, Tick: tick})
	})

	if err := p.ParseToEnd(); err != nil {
		return killplan.Plan{}, fmt.Errorf("parsing demo: %w", err)
	}

	if m.Tickrate <= 0 {
		m.Tickrate = int(p.TickRate())
	}
	if m.Map == "" {
		m.Map = mapName
	}
	if m.DurationTicks <= 0 {
		m.DurationTicks = maxTick
	}

	for _, u := range pending {
		annotateUtilityDestination(u, m.Map)
		c.RecordUtility(*u)
	}

	plan, err := c.Build(m)
	if err != nil {
		if strings.Contains(err.Error(), "not found in demo") {
			return killplan.Plan{}, ErrTargetNotFound
		}
		return killplan.Plan{}, err
	}
	return plan, nil
}

func isSmokeProjectile(projectile *common.GrenadeProjectile) bool {
	return projectile != nil && isSmokeEquipment(projectile.WeaponInstance)
}

func isSmokeEquipment(eq *common.Equipment) bool {
	return eq != nil && (eq.Type == common.EqSmoke || weaponName(eq) == SmokeGrenadeType)
}

func isTrackedUtilityProjectile(projectile *common.GrenadeProjectile) bool {
	return projectile != nil && isTrackedUtilityEquipment(projectile.WeaponInstance)
}

func isTrackedUtilityEquipment(eq *common.Equipment) bool {
	if eq == nil {
		return false
	}
	return isTrackedUtilityType(utilityTypeFromEquipment(eq))
}

func utilityTypeFromEquipment(eq *common.Equipment) string {
	if eq == nil {
		return ""
	}
	switch eq.Type {
	case common.EqSmoke:
		return SmokeGrenadeType
	case common.EqFlash:
		return FlashbangType
	case common.EqMolotov:
		return MolotovType
	case common.EqIncendiary:
		return IncendiaryGrenadeType
	default:
		return weaponName(eq)
	}
}

func projectileEntityID(projectile *common.GrenadeProjectile) int {
	if projectile == nil || projectile.Entity == nil {
		return 0
	}
	return projectile.Entity.ID()
}

func projectilePosition(projectile *common.GrenadeProjectile) [3]float64 {
	if projectile == nil || projectile.Entity == nil {
		return [3]float64{}
	}
	return vectorPosition(projectile.Position())
}

func findPendingSmoke(byEntityID map[int]*RawUtilityThrow, pending []*RawUtilityThrow, entityID int, thrower *common.Player, targetID uint64, tick, maxGapTicks int) *RawUtilityThrow {
	if entityID != 0 {
		if smoke := byEntityID[entityID]; smoke != nil {
			if smokeEventGapOK(smoke, tick, maxGapTicks) {
				return smoke
			}
		}
	}
	if thrower == nil || thrower.SteamID64 != targetID {
		return nil
	}
	target := strconv.FormatUint(targetID, 10)
	for i := len(pending) - 1; i >= 0; i-- {
		smoke := pending[i]
		if smoke.Thrower.SteamID64 == target && smoke.PopTick == 0 {
			if smokeEventGapOK(smoke, tick, maxGapTicks) {
				return smoke
			}
		}
	}
	return nil
}

func smokeEventGapOK(smoke *RawUtilityThrow, tick, maxGapTicks int) bool {
	if smoke == nil || maxGapTicks <= 0 {
		return true
	}
	gap := tick - smoke.ThrowTick
	return gap >= 0 && gap <= maxGapTicks
}

func findRecentPendingWeaponFire(pending []*RawUtilityThrow, thrower *common.Player, tick, tickrate int) *RawUtilityThrow {
	if thrower == nil {
		return nil
	}
	if tickrate <= 0 {
		tickrate = 64
	}
	maxGap := tickrate * 2
	target := strconv.FormatUint(thrower.SteamID64, 10)
	for i := len(pending) - 1; i >= 0; i-- {
		smoke := pending[i]
		if smoke.Thrower.SteamID64 != target || smoke.PopTick != 0 || smoke.LandingPos != [3]float64{} {
			continue
		}
		gap := tick - smoke.ThrowTick
		if gap >= 0 && gap <= maxGap {
			return smoke
		}
	}
	return nil
}

func findPendingUtility(byEntityID map[int]*RawUtilityThrow, pending []*RawUtilityThrow, entityID int, thrower *common.Player, targetID uint64, typ string, tick, maxGapTicks int) *RawUtilityThrow {
	if entityID != 0 {
		if u := byEntityID[entityID]; u != nil && u.Type == typ {
			if smokeEventGapOK(u, tick, maxGapTicks) {
				return u
			}
		}
	}
	if thrower == nil || thrower.SteamID64 != targetID {
		return nil
	}
	target := strconv.FormatUint(targetID, 10)
	for i := len(pending) - 1; i >= 0; i-- {
		u := pending[i]
		if u.Thrower.SteamID64 == target && u.Type == typ && u.PopTick == 0 {
			if smokeEventGapOK(u, tick, maxGapTicks) {
				return u
			}
		}
	}
	return nil
}

func findRecentPendingUtility(pending []*RawUtilityThrow, thrower *common.Player, typ string, tick, tickrate int) *RawUtilityThrow {
	return findRecentPendingAnyUtility(pending, thrower, []string{typ}, tick, tickrate)
}

func findRecentPendingAnyUtility(pending []*RawUtilityThrow, thrower *common.Player, types []string, tick, tickrate int) *RawUtilityThrow {
	if thrower == nil {
		return nil
	}
	if tickrate <= 0 {
		tickrate = 64
	}
	maxGap := tickrate * 12
	target := strconv.FormatUint(thrower.SteamID64, 10)
	for i := len(pending) - 1; i >= 0; i-- {
		u := pending[i]
		if u.Thrower.SteamID64 != target || !containsUtilityType(types, u.Type) {
			continue
		}
		gap := tick - u.ThrowTick
		if gap >= 0 && gap <= maxGap {
			return u
		}
	}
	return nil
}

func containsUtilityType(types []string, typ string) bool {
	for _, candidate := range types {
		if candidate == typ {
			return true
		}
	}
	return false
}

func playerIdentity(p *common.Player) killplan.Player {
	if p == nil {
		return killplan.Player{}
	}
	return killplan.Player{
		SteamID64:  strconv.FormatUint(p.SteamID64, 10),
		NameInDemo: p.Name,
		TeamAtKill: teamLabel(p.Team),
	}
}

func vectorPosition(v r3.Vector) [3]float64 {
	return [3]float64{v.X, v.Y, v.Z}
}

func infernoCenter(inferno *common.Inferno) ([3]float64, bool) {
	if inferno == nil {
		return [3]float64{}, false
	}
	fires := inferno.Fires().List()
	if len(fires) == 0 {
		return [3]float64{}, false
	}
	var sum [3]float64
	for _, fire := range fires {
		sum[0] += fire.X
		sum[1] += fire.Y
		sum[2] += fire.Z
	}
	n := float64(len(fires))
	return [3]float64{sum[0] / n, sum[1] / n, sum[2] / n}, true
}

func annotateUtilityDestination(u *RawUtilityThrow, mapName string) {
	if u == nil || u.LineupMatch != nil {
		return
	}
	destination := inferUtilityDestination(mapName, u.Type, u.LandingPos)
	if destination == "" {
		return
	}
	u.LineupMatch = &killplan.LineupMatch{
		ID:          "auto-" + utilityIDPrefix(u.Type) + "-" + sanitizeLineupID(destination),
		Destination: destination,
		FromArea:    u.ThrowPlace,
		Confidence:  0.55,
	}
}

func inferUtilityDestination(mapName, typ string, pos [3]float64) string {
	if strings.TrimSpace(mapName) != "de_inferno" {
		return ""
	}
	x, y := pos[0], pos[1]
	switch {
	case x > 500 && y > 1800:
		return "B site"
	case x < 350 && y > 1200:
		return "Banana"
	case x < 350 && y > 750:
		return "T ramp"
	case x > 850 && y > 300 && y <= 1100:
		return "CT spawn / ruins"
	case x >= 1700 && y < 350:
		return "A long / arch"
	case x >= 900 && y < 350:
		return "A site / short"
	case x < -500 && y < -300:
		return "Apartments"
	default:
		if typ == FlashbangType {
			return "contested area"
		}
		return "utility landing"
	}
}

func sanitizeLineupID(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var sb strings.Builder
	lastDash := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			sb.WriteRune(r)
			lastDash = false
		case !lastDash:
			sb.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(sb.String(), "-")
}

func lastTrajectoryPosition(projectile *common.GrenadeProjectile) ([3]float64, bool) {
	if projectile == nil || len(projectile.Trajectory) == 0 {
		return [3]float64{}, false
	}
	last := projectile.Trajectory[len(projectile.Trajectory)-1]
	return vectorPosition(last.Position), true
}

func safeLastPlaceName(p *common.Player) (out string) {
	if p == nil {
		return ""
	}
	defer func() {
		if recover() != nil {
			out = ""
		}
	}()
	return p.LastPlaceName()
}

func applyThrowState(u *RawUtilityThrow, p *common.Player, tick int, source string) {
	if u == nil || p == nil {
		return
	}
	stance := safePlayerStance(p)
	onGround := safePlayerOnGround(p)
	walking := safePlayerWalking(p)
	ducking := stance == "crouching" || stance == "crouching_in_progress"
	speed := safePlayerSpeed2D(p)
	movement := movementLabel(speed, walking)
	action := throwActionLabel(stance, movement, onGround)

	// Prefer the projectile-created tick over weapon_fire when it proves the
	// throw was airborne; later pop/landing events must not rewrite action.
	if u.ThrowAction != "" && (action != "jumpthrow" || source != "projectile_throw") {
		return
	}
	u.ThrowStateTick = tick
	u.ThrowStateSource = source
	u.Stance = stance
	u.OnGround = onGround
	u.Walking = walking
	u.Ducking = ducking
	u.Speed2D = math.Round(speed*10) / 10
	u.Movement = movement
	u.ThrowAction = action
}

func safePlayerStance(p *common.Player) (out string) {
	out = "standing"
	if p == nil {
		return out
	}
	defer func() {
		if recover() != nil {
			out = "standing"
		}
	}()
	switch {
	case p.IsDucking():
		return "crouching"
	case p.IsDuckingInProgress():
		return "crouching_in_progress"
	case p.IsUnDuckingInProgress():
		return "uncrouching"
	default:
		return "standing"
	}
}

func safePlayerOnGround(p *common.Player) (out bool) {
	if p == nil {
		return true
	}
	defer func() {
		if recover() != nil {
			out = true
		}
	}()
	return p.Flags().OnGround()
}

func safePlayerWalking(p *common.Player) (out bool) {
	if p == nil {
		return false
	}
	defer func() {
		if recover() != nil {
			out = false
		}
	}()
	return p.IsWalking()
}

func safePlayerSpeed2D(p *common.Player) (out float64) {
	if p == nil || p.PlayerPawnEntity() == nil {
		return 0
	}
	defer func() {
		if recover() != nil {
			out = 0
		}
	}()
	velocity := p.PlayerPawnEntity().PropertyValueMust("m_vecVelocity").R3Vec()
	return math.Hypot(velocity.X, velocity.Y)
}

func movementLabel(speed float64, walking bool) string {
	switch {
	case speed < 8:
		return "stopped"
	case walking || speed < 140:
		return "walking"
	default:
		return "running"
	}
}

func throwActionLabel(stance, movement string, onGround bool) string {
	if !onGround {
		return "jumpthrow"
	}
	if stance == "crouching" || stance == "crouching_in_progress" {
		return "crouch_throw"
	}
	if movement == "running" {
		return "run_throw"
	}
	if movement == "walking" {
		return "walk_throw"
	}
	return "standing_throw"
}
