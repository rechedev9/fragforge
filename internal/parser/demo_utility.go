package parser

import (
	"errors"
	"fmt"
	"strconv"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
)

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
		u := findRecentPendingUtility(pending, thrower, target, typ, tick, int(p.TickRate()))
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
		u := findPendingUtility(byEntityID, pending, e.GrenadeEntityID, e.Thrower, targetID, target, FlashbangType, tick, 4*int(p.TickRate()))
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
		u := findPendingUtility(byEntityID, pending, e.GrenadeEntityID, e.Thrower, targetID, target, SmokeGrenadeType, tick, 12*int(p.TickRate()))
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
		if u := findPendingUtility(byEntityID, pending, e.GrenadeEntityID, e.Thrower, targetID, target, SmokeGrenadeType, tick, 35*int(p.TickRate())); u != nil {
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
		u := findRecentPendingFireUtility(pending, thrower, target, tick, int(p.TickRate()))
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
		if u := findRecentPendingFireUtility(pending, e.Inferno.Thrower(), target, tick, 12*int(p.TickRate())); u != nil {
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
		if errors.Is(err, ErrTargetNotFound) {
			return killplan.Plan{}, ErrTargetNotFound
		}
		return killplan.Plan{}, err
	}
	return plan, nil
}
