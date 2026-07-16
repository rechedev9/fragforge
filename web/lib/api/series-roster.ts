import type { DemoPlayer, AggregatedSeriesPlayer } from './types';

/**
 * Per-player accumulator across the maps of a series. Counting stats sum; rate
 * stats keep both a round-weighted numerator (`*W` = sum of rate*rounds) and a
 * plain sum (`*S`) so a series that reports zero rounds can still fall back to a
 * simple average. `base` is the player's first appearance, kept for name/team.
 */
type Accumulator = {
  base: DemoPlayer;
  mapsPresent: number;
  kills: number;
  deaths: number;
  assists: number;
  headshots: number;
  mvps: number;
  rounds: number;
  rounds2k: number;
  rounds3k: number;
  rounds4k: number;
  rounds5k: number;
  adrW: number;
  adrS: number;
  hsPctW: number;
  hsPctS: number;
  kastW: number;
  kastS: number;
  ratingW: number;
  ratingS: number;
};

/**
 * Aggregates each player's scoreboard across every map of a series. Players are
 * unioned by steamId. Counting stats (kills, deaths, assists, headshots, mvps,
 * rounds, and the multi-kill round counts) are summed. Rate stats (adr, hsPct,
 * kast, rating) are weighted by each map's rounds when the player played any
 * rounds at all, else averaged plainly, so a 30-round map counts more than a
 * 12-round one without dividing by zero. `mapsPresent` counts the maps a player
 * appeared in, and name/team come from their first appearance. The result is
 * sorted by total kills descending, then steamId, for a deterministic order.
 */
export function aggregateSeriesRoster(rosters: DemoPlayer[][]): AggregatedSeriesPlayer[] {
  const byId = new Map<string, Accumulator>();

  for (const roster of rosters) {
    for (const player of roster) {
      const acc = byId.get(player.steamId) ?? newAccumulator(player);
      acc.mapsPresent += 1;
      acc.kills += player.kills;
      acc.deaths += player.deaths;
      acc.assists += player.assists;
      acc.headshots += player.headshots;
      acc.mvps += player.mvps;
      acc.rounds += player.rounds;
      acc.rounds2k += player.rounds2k ?? 0;
      acc.rounds3k += player.rounds3k ?? 0;
      acc.rounds4k += player.rounds4k ?? 0;
      acc.rounds5k += player.rounds5k ?? 0;
      acc.adrW += player.adr * player.rounds;
      acc.adrS += player.adr;
      acc.hsPctW += player.hsPct * player.rounds;
      acc.hsPctS += player.hsPct;
      acc.kastW += player.kast * player.rounds;
      acc.kastS += player.kast;
      acc.ratingW += player.rating * player.rounds;
      acc.ratingS += player.rating;
      byId.set(player.steamId, acc);
    }
  }

  const players = Array.from(byId.values(), finalize);
  return players.sort((a, b) => b.kills - a.kills || a.steamId.localeCompare(b.steamId));
}

function newAccumulator(base: DemoPlayer): Accumulator {
  return {
    base,
    mapsPresent: 0,
    kills: 0,
    deaths: 0,
    assists: 0,
    headshots: 0,
    mvps: 0,
    rounds: 0,
    rounds2k: 0,
    rounds3k: 0,
    rounds4k: 0,
    rounds5k: 0,
    adrW: 0,
    adrS: 0,
    hsPctW: 0,
    hsPctS: 0,
    kastW: 0,
    kastS: 0,
    ratingW: 0,
    ratingS: 0,
  };
}

function finalize(acc: Accumulator): AggregatedSeriesPlayer {
  const rate = (weighted: number, plainSum: number): number => {
    if (acc.rounds > 0) return weighted / acc.rounds;
    return acc.mapsPresent > 0 ? plainSum / acc.mapsPresent : 0;
  };
  return {
    steamId: acc.base.steamId,
    name: acc.base.name,
    team: acc.base.team,
    kills: acc.kills,
    deaths: acc.deaths,
    assists: acc.assists,
    headshots: acc.headshots,
    mvps: acc.mvps,
    rounds: acc.rounds,
    adr: rate(acc.adrW, acc.adrS),
    hsPct: rate(acc.hsPctW, acc.hsPctS),
    kast: rate(acc.kastW, acc.kastS),
    rating: rate(acc.ratingW, acc.ratingS),
    rounds2k: acc.rounds2k,
    rounds3k: acc.rounds3k,
    rounds4k: acc.rounds4k,
    rounds5k: acc.rounds5k,
    mapsPresent: acc.mapsPresent,
  };
}
