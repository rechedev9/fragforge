import type { EditConfig, JobSummary } from './types';
import type { ReelIntent } from './reel-store';

/**
 * Reel-library synthesis core - pure, framework-free, the testable heart of the
 * "server jobs show up in /videos" path. An MCP-triggered render runs entirely
 * server-side, so the desktop app has no local ReelIntent for it. These helpers
 * decide, from the job list alone, which server jobs deserve a *synthetic*
 * intent (a reflection of server state, never persisted) and which synthetics
 * have gone stale and must be evicted. RealApiClient performs the I/O (probing
 * render status, mutating its maps) on top of these decisions.
 *
 * Locally-created intents always win: a job already covered by a real intent is
 * never synthesized, and a synthetic is evicted the moment a local intent starts
 * covering its job. Synthesis is idempotent (keyed by a stable videoId).
 *
 * Unit-tested in match-sync.test.ts (node:test).
 */

/** Suffix marking a videoId as a synthetic (server-derived) reel. */
const SYNTHETIC_SUFFIX = '__server';

/** The stable synthetic videoId for a server job. */
export function syntheticVideoId(jobId: string): string {
  return `${jobId}${SYNTHETIC_SUFFIX}`;
}

/** The jobId a synthetic videoId was minted from. */
function jobIdOfSynthetic(videoId: string): string {
  return videoId.endsWith(SYNTHETIC_SUFFIX) ? videoId.slice(0, -SYNTHETIC_SUFFIX.length) : videoId;
}

/**
 * Job statuses at or past which a render could exist. Only these jobs are worth
 * probing for render activity; earlier stages (scanning..recording) have nothing
 * rendered yet, so synthesizing them would show an un-driveable card.
 */
const RENDER_POSSIBLE = new Set(['recorded', 'composing', 'composed', 'done']);

/**
 * The server jobs worth probing for render activity this tick: advanced enough
 * to have a render, not already covered by a local intent (local precedence),
 * and not already synthesized (idempotency). The caller probes each candidate's
 * render status and only synthesizes those with real render activity.
 */
export function selectSyntheticCandidates(input: {
  summaries: JobSummary[];
  localJobIds: Set<string>;
  existingSyntheticVideoIds: Set<string>;
}): JobSummary[] {
  const { summaries, localJobIds, existingSyntheticVideoIds } = input;
  return summaries.filter(
    (s) =>
      RENDER_POSSIBLE.has(s.status) &&
      !localJobIds.has(s.id) &&
      !existingSyntheticVideoIds.has(syntheticVideoId(s.id)),
  );
}

/**
 * A synthetic ReelIntent reflecting a server render. Empty segmentIds: the
 * desktop never selected anything, it is only mirroring what the server already
 * produced (the reconcile loop only ever *reflects* a synthetic's status, never
 * drives it to record/render, because a render already exists). Title is generic
 * since there is no local Play selection to label. `variant`, `editConfig`, and
 * the display `map` are injected by the caller so this core stays a pure,
 * type-only module (prettifyMap and the default config live outside it).
 */
export function syntheticIntentFrom(
  summary: JobSummary,
  opts: { variant: string; editConfig: EditConfig; map: string },
): ReelIntent {
  return {
    videoId: syntheticVideoId(summary.id),
    jobId: summary.id,
    segmentIds: [],
    mode: 'clean',
    variant: opts.variant,
    editConfig: opts.editConfig,
    title: 'Clip del servidor',
    map: opts.map,
    score: '',
    createdAt: Date.parse(summary.created_at) || Date.now(),
    published: false,
  };
}

/**
 * Synthetic videoIds to evict: a job now covered by a local intent (avoid a
 * duplicate card) or a job that has disappeared from the server (a memory-mode
 * restart dropped it). Real (non-synthetic) intents are never returned.
 */
export function staleSyntheticVideoIds(input: {
  syntheticVideoIds: Iterable<string>;
  localJobIds: Set<string>;
  liveJobIds: Set<string>;
}): string[] {
  const { syntheticVideoIds, localJobIds, liveJobIds } = input;
  const stale: string[] = [];
  for (const videoId of syntheticVideoIds) {
    const jobId = jobIdOfSynthetic(videoId);
    if (localJobIds.has(jobId) || !liveJobIds.has(jobId)) stale.push(videoId);
  }
  return stale;
}
