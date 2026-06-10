package parser

import (
	"math"
	"strconv"
	"strings"

	"github.com/golang/geo/r3"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"

	"github.com/rechedev9/fragforge/internal/killplan"
)

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

func findPendingSmoke(byEntityID map[int]*RawUtilityThrow, pending []*RawUtilityThrow, entityID int, thrower *common.Player, targetID uint64, targetSteamID string, tick, maxGapTicks int) *RawUtilityThrow {
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
	for i := len(pending) - 1; i >= 0; i-- {
		smoke := pending[i]
		if smoke.Thrower.SteamID64 == targetSteamID && smoke.PopTick == 0 {
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

func findRecentPendingWeaponFire(pending []*RawUtilityThrow, thrower *common.Player, throwerSteamID string, tick, tickrate int) *RawUtilityThrow {
	if thrower == nil {
		return nil
	}
	if tickrate <= 0 {
		tickrate = 64
	}
	maxGap := tickrate * 2
	for i := len(pending) - 1; i >= 0; i-- {
		smoke := pending[i]
		if smoke.Thrower.SteamID64 != throwerSteamID || smoke.PopTick != 0 || smoke.LandingPos != [3]float64{} {
			continue
		}
		gap := tick - smoke.ThrowTick
		if gap >= 0 && gap <= maxGap {
			return smoke
		}
	}
	return nil
}

func findPendingUtility(byEntityID map[int]*RawUtilityThrow, pending []*RawUtilityThrow, entityID int, thrower *common.Player, targetID uint64, targetSteamID string, typ string, tick, maxGapTicks int) *RawUtilityThrow {
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
	for i := len(pending) - 1; i >= 0; i-- {
		u := pending[i]
		if u.Thrower.SteamID64 == targetSteamID && u.Type == typ && u.PopTick == 0 {
			if smokeEventGapOK(u, tick, maxGapTicks) {
				return u
			}
		}
	}
	return nil
}

func findRecentPendingUtility(pending []*RawUtilityThrow, thrower *common.Player, throwerSteamID string, typ string, tick, tickrate int) *RawUtilityThrow {
	if thrower == nil {
		return nil
	}
	if tickrate <= 0 {
		tickrate = 64
	}
	maxGap := tickrate * 12
	for i := len(pending) - 1; i >= 0; i-- {
		u := pending[i]
		if u.Thrower.SteamID64 != throwerSteamID || u.Type != typ {
			continue
		}
		gap := tick - u.ThrowTick
		if gap >= 0 && gap <= maxGap {
			return u
		}
	}
	return nil
}

func findRecentPendingFireUtility(pending []*RawUtilityThrow, thrower *common.Player, throwerSteamID string, tick, tickrate int) *RawUtilityThrow {
	if thrower == nil {
		return nil
	}
	if tickrate <= 0 {
		tickrate = 64
	}
	maxGap := tickrate * 12
	for i := len(pending) - 1; i >= 0; i-- {
		u := pending[i]
		if u.Thrower.SteamID64 != throwerSteamID || (u.Type != MolotovType && u.Type != IncendiaryGrenadeType) {
			continue
		}
		gap := tick - u.ThrowTick
		if gap >= 0 && gap <= maxGap {
			return u
		}
	}
	return nil
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
