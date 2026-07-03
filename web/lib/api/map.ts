import type { Match, Play, DemoPlayer } from './types';

/**
 * Minimal mirror of the killplan JSON (github.com/rechedev9/fragforge
 * internal/killplan) — only the fields the web client consumes. The Go schema is
 * richer; we read what we map to the UI and ignore the rest.
 */
export type KillPlan = {
  schema_version?: string;
  demo?: { map?: string };
  target?: { steamid64?: string; name_in_demo?: string; team_at_start?: string };
  stats?: { total_kills_target?: number };
  segments?: KillPlanSegment[];
};

export type KillPlanSegment = {
  id: string;
  round: number;
  kills?: { weapon?: string }[];
};

const THUMB_BASE = 'https://picsum.photos/seed';

function thumb(seed: string): string {
  return `${THUMB_BASE}/${encodeURIComponent(seed)}/640/360`;
}

/** Pretty CS2 map name: "de_inferno" → "Inferno". Falls back to the raw value. */
export function prettifyMap(raw: string): string {
  if (!raw) return raw;
  const stripped = raw.replace(/^(de|cs)_/, '');
  if (!stripped) return raw;
  return stripped
    .split('_')
    .map((part) => (part ? part[0].toUpperCase() + part.slice(1) : part))
    .join(' ');
}

/** Display weapon names; the parser emits engine ids like "ak47". */
const WEAPON_LABELS: Record<string, string> = {
  ak47: 'AK-47',
  m4a1: 'M4A4',
  m4a1_silencer: 'M4A1-S',
  m4a4: 'M4A4',
  awp: 'AWP',
  deagle: 'Desert Eagle',
  usp_silencer: 'USP-S',
  glock: 'Glock-18',
  hkp2000: 'P2000',
  ssg08: 'SSG 08',
  aug: 'AUG',
  sg556: 'SG 553',
  famas: 'FAMAS',
  galilar: 'Galil AR',
  mp9: 'MP9',
  mac10: 'MAC-10',
  mp7: 'MP7',
  ump45: 'UMP-45',
  p90: 'P90',
  bizon: 'PP-Bizon',
  nova: 'Nova',
  xm1014: 'XM1014',
  mag7: 'MAG-7',
  sawedoff: 'Sawed-Off',
  m249: 'M249',
  negev: 'Negev',
  fiveseven: 'Five-SeveN',
  tec9: 'Tec-9',
  cz75a: 'CZ75-Auto',
  p250: 'P250',
  revolver: 'R8 Revolver',
  elite: 'Dual Berettas',
  knife: 'Knife',
  hegrenade: 'HE Grenade',
};

/** Pretty weapon label for an engine id, falling back to the raw value. */
export function prettifyWeapon(raw: string): string {
  if (!raw) return raw;
  return WEAPON_LABELS[raw.toLowerCase()] ?? raw;
}

/** Most-frequent weapon in a segment, prettified; undefined when no kills. */
function topWeapon(segment: KillPlanSegment): string | undefined {
  const counts = new Map<string, number>();
  for (const kill of segment.kills ?? []) {
    if (!kill.weapon) continue;
    counts.set(kill.weapon, (counts.get(kill.weapon) ?? 0) + 1);
  }
  let best: string | undefined;
  let bestCount = 0;
  for (const [weapon, count] of counts) {
    if (count > bestCount) {
      best = weapon;
      bestCount = count;
    }
  }
  return best ? prettifyWeapon(best) : undefined;
}

/** One killplan segment → a UI Play. */
export function segmentToPlay(jobId: string, segment: KillPlanSegment): Play {
  const kills = segment.kills?.length ?? 0;
  return {
    id: segment.id,
    matchId: jobId,
    kind: 'highlight',
    round: segment.round,
    kills,
    weapon: topWeapon(segment),
    label: `${kills}K · Ronda ${segment.round}`,
    thumbnailUrl: thumb(segment.id),
  };
}

/** All of a plan's segments → Plays for the given job. */
export function planToPlays(jobId: string, plan: KillPlan): Play[] {
  return (plan.segments ?? []).map((segment) => segmentToPlay(jobId, segment));
}

/**
 * killplan + the chosen player's roster row → a Match. The parser computes no
 * round score or MVPs, so score is "" and mvps is 0 (the UI hides empty score).
 * Stats come from the roster tally for the picked player.
 */
export function planToMatch(jobId: string, plan: KillPlan, player: DemoPlayer): Match {
  const segments = plan.segments ?? [];
  const { kills, deaths, assists } = player;
  return {
    id: jobId,
    map: prettifyMap(plan.demo?.map ?? ''),
    score: '',
    playedAt: new Date().toISOString(),
    stats: {
      kills,
      deaths,
      assists,
      mvps: player.mvps,
      kd: deaths ? Number((kills / deaths).toFixed(2)) : kills,
      rating: player.rating,
      adr: player.adr,
      kast: player.kast,
      hsPct: player.hsPct,
    },
    decentPlays: segments.length,
    thumbnailUrl: thumb(jobId),
    source: 'upload',
  };
}
