package parser

import (
	"fmt"
	"strconv"
	"strings"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"

	"github.com/reche/zackvideo/internal/killplan"
	"github.com/reche/zackvideo/internal/rules"
)

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
		smoke := findRecentPendingWeaponFire(pending, thrower, target, tick, int(p.TickRate()))
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
		smoke := findPendingSmoke(byEntityID, pending, e.GrenadeEntityID, e.Thrower, targetID, target, tick, 12*int(p.TickRate()))
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
		if smoke := findPendingSmoke(byEntityID, pending, e.GrenadeEntityID, e.Thrower, targetID, target, tick, 35*int(p.TickRate())); smoke != nil {
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
