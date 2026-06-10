package parser

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	demoinfocs "github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/common"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/events"
	"github.com/markus-wa/demoinfocs-golang/v5/pkg/demoinfocs/msg"

	"github.com/rechedev9/fragforge/internal/killplan"
	"github.com/rechedev9/fragforge/internal/rules"
)

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

	if err := parseToEnd(p); err != nil {
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
		if errors.Is(err, ErrTargetNotFound) {
			return killplan.Plan{}, ErrTargetNotFound
		}
		return killplan.Plan{}, err
	}
	return plan, nil
}
