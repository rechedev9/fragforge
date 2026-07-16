// Pure logic for the Partidas index: turning the /api/demos/jobs listing (the
// demo jobs the local orchestrator persists) into the Matches and series
// summaries the /matches page rediscovers after a restart. Kept pure and
// unit-tested so the page and the RealApiClient never sprinkle status filtering,
// grouping, or stat-mapping decisions across the UI.

import type { DemoPlayer, Match, MatchStats } from './types.ts';
import { prettifyMap } from './map.ts';
import { groupSeriesDemos } from '../series-grouping.ts';

/**
 * One demo job as surfaced by the /api/demos/jobs proxy: the whitelisted,
 * camelCased fields of an orchestrator job. This is the Partidas index feed —
 * enough to list every uploaded demo/series and link into it, without the kill
 * plan (stripped from the list) or the roster (fetched lazily per job).
 */
export type IndexedJob = {
  jobId: string;
  status: string;
  failureReason?: string;
  fileName?: string;
  seriesId?: string;
  targetSteamId?: string;
  /** ISO-8601 upload time; the Partidas list sorts newest first by it. */
  createdAt?: string;
};

/**
 * Job statuses at or past which the roster scan result exists, so a listed demo
 * can be enriched with its map and the target player's stats. Excludes
 * queued/scanning (no roster yet) and failed (nothing to read). The one
 * canonical set, shared with getSeries instead of re-declared per module.
 */
export const ROSTER_READY: ReadonlySet<string> = new Set([
  'scanned',
  'parsing',
  'parsed',
  'recording',
  'recorded',
  'composing',
  'composed',
  'done',
]);

/** True once a demo has a roster scan, so it belongs in the Partidas list. */
export function jobHasRoster(status: string): boolean {
  return ROSTER_READY.has(status);
}

/** Epoch ms for a job's upload time; 0 (sorts last) when absent or unparseable. */
export function jobCreatedAtMs(job: IndexedJob): number {
  if (!job.createdAt) return 0;
  const ms = Date.parse(job.createdAt);
  return Number.isNaN(ms) ? 0 : ms;
}

/**
 * The demo jobs that belong in Partidas — those past a roster scan — newest
 * first. A still-scanning demo appears once it reaches `scanned`; a failed one
 * never lists (there is nothing to open).
 */
export function listableJobs(jobs: readonly IndexedJob[]): IndexedJob[] {
  return jobs
    .filter((job) => jobHasRoster(job.status))
    .sort((a, b) => jobCreatedAtMs(b) - jobCreatedAtMs(a));
}

/** One uploaded series, as the Partidas SERIES section lists it. */
export type SeriesSummary = { seriesId: string; mapCount: number; createdAt: number };

/**
 * Groups the jobs by series id into one summary per series: how many logical
 * maps it holds and when it started (its earliest demo's upload time). Only
 * jobs that carry a series id participate — a standalone single-demo upload
 * yields no summary. Every job of the series counts, including still-scanning
 * ones, so a freshly uploaded series is discoverable before its maps finish
 * scanning. Split demo parts (…-p1/-p2) fold into one map via the same
 * grouping the series view uses, so "SERIE DE N MAPAS" agrees everywhere.
 * Ordered newest series first.
 */
export function summarizeSeries(jobs: readonly IndexedJob[]): SeriesSummary[] {
  const bySeries = new Map<string, IndexedJob[]>();
  for (const job of jobs) {
    if (!job.seriesId) continue;
    const existing = bySeries.get(job.seriesId);
    if (existing) existing.push(job);
    else bySeries.set(job.seriesId, [job]);
  }
  return Array.from(bySeries, ([seriesId, seriesJobs]) => {
    // The series started with its earliest demo; jobs without a time count as 0.
    const times = seriesJobs.map(jobCreatedAtMs).filter((at) => at > 0);
    return {
      seriesId,
      mapCount: groupSeriesDemos(seriesJobs).length,
      createdAt: times.length > 0 ? Math.min(...times) : 0,
    };
  }).sort((a, b) => b.createdAt - a.createdAt);
}

/** Zeroed scoreboard for a listed upload whose roster could not be read. */
export const ZERO_STATS: MatchStats = { kills: 0, deaths: 0, assists: 0, mvps: 0, kd: 0 };

/**
 * The target player's scoreboard as a Match's stats. Derives K/D the same way
 * as planToMatch (kills when deaths is 0) and carries the enriched rate stats
 * through, so a listed upload's numbers match its detail view.
 */
export function statsFromPlayer(player: DemoPlayer): MatchStats {
  const { kills, deaths, assists } = player;
  return {
    kills,
    deaths,
    assists,
    mvps: player.mvps,
    kd: deaths ? Number((kills / deaths).toFixed(2)) : kills,
    rating: player.rating,
    adr: player.adr,
    kast: player.kast,
    hsPct: player.hsPct,
  };
}

/** A listed job's headline when its roster has no map yet: its file name, else a placeholder. */
function jobHeadline(job: IndexedJob): string {
  return job.fileName ?? 'Partida';
}

/**
 * One indexed job → the Match the /matches page lists. `enrichment` carries the
 * roster-derived map (raw, prettified here) and the target player's row when the
 * roster was read; without it the entry is a filename-titled, zeroed placeholder
 * so a demo whose roster failed still lists rather than dropping out. Score stays
 * empty and decentPlays 0, matching planToMatch, so the entry reads consistently
 * once opened at /matches/{jobId}.
 */
export function jobToMatch(job: IndexedJob, enrichment?: { map?: string; player?: DemoPlayer }): Match {
  const rawMap = enrichment?.map;
  const player = enrichment?.player;
  const match: Match = {
    id: job.jobId,
    map: rawMap ? prettifyMap(rawMap) : jobHeadline(job),
    score: '',
    playedAt: job.createdAt ?? new Date(jobCreatedAtMs(job)).toISOString(),
    stats: player ? statsFromPlayer(player) : { ...ZERO_STATS },
    decentPlays: 0,
    source: 'upload',
  };
  // Name the row after the clipped/target player when the roster resolved them;
  // leave it off (no stray separator) for an unenriched or nameless entry.
  if (player?.name) match.player = player.name;
  return match;
}
