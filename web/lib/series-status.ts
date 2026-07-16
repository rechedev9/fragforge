// Presentation helpers for a bulk demo series (bo3/bo5): mapping the raw job
// status strings the series proxy surfaces onto the Spanish pills, tones, and
// polling/linking decisions the series view needs. Pure and unit-tested so the
// UI never sprinkles status string literals across components.

/**
 * Raw orchestrator job statuses grouped into the Spanish label shown on each
 * demo's status pill. `scanned` is a settled state in a series, not progress:
 * after the pick, a map whose roster lacked the chosen player parks there
 * forever, so it reads "sin jugador elegido" rather than implying work that
 * will never happen. Unknown/older statuses fall back to "analizando" via
 * {@link seriesStatusLabel}.
 */
export const SERIES_STATUS_LABELS = {
  queued: 'analizando',
  scanning: 'analizando',
  scanned: 'sin jugador elegido',
  parsing: 'analizando',
  parsed: 'lista para forjar',
  recording: 'grabando',
  recorded: 'grabando',
  composing: 'renderizando',
  composed: 'renderizando',
  done: 'completada',
  failed: 'fallida',
} as const;

/** The visual tone of a status pill, mapped to concrete classes in the view. */
export type SeriesStatusTone = 'pending' | 'ready' | 'progress' | 'done' | 'failed';

/**
 * Statuses that are still moving toward a kill plan or a rendered reel, so the
 * series page keeps polling while any demo sits in one. Deliberately excludes
 * `scanned` (a demo whose player was skipped stays there and never advances on
 * its own) and the transient `recorded`/`composed` handoffs.
 */
export const SERIES_PENDING_STATUSES: ReadonlySet<string> = new Set([
  'queued',
  'scanning',
  'parsing',
  'recording',
  'composing',
]);

/**
 * Statuses at or past which the kill plan exists, so `/matches/{jobId}` resolves
 * and the demo can link into the highlight picker. Mirrors the client's
 * PLAN_READY set.
 */
export const SERIES_FORGEABLE_STATUSES: ReadonlySet<string> = new Set([
  'parsed',
  'recording',
  'recorded',
  'composing',
  'composed',
  'done',
]);

/** The Spanish pill label for a raw status; unknown statuses read as "analizando". */
export function seriesStatusLabel(status: string): string {
  return SERIES_STATUS_LABELS[status as keyof typeof SERIES_STATUS_LABELS] ?? 'analizando';
}

/** The pill tone for a raw status; drives the pill colour in the series view. */
export function seriesStatusTone(status: string): SeriesStatusTone {
  if (status === 'failed') return 'failed';
  if (status === 'done') return 'done';
  if (status === 'parsed') return 'ready';
  if (status === 'recording' || status === 'recorded' || status === 'composing' || status === 'composed') {
    return 'progress';
  }
  return 'pending';
}

/** True while a demo is still working toward a plan/reel (keep polling). */
export function seriesStatusIsPending(status: string): boolean {
  return SERIES_PENDING_STATUSES.has(status);
}

/** True once a demo has a kill plan, so it links into `/matches/{jobId}`. */
export function seriesStatusIsForgeable(status: string): boolean {
  return SERIES_FORGEABLE_STATUSES.has(status);
}

/**
 * The series header buckets: how many maps are ready to forge, genuinely still
 * processing, failed, or settled without the chosen player (`scanned` after the
 * pick). The buckets are disjoint; only `pending` may honestly be described as
 * "still processing". An unknown/older status lands in `pending` because its
 * pill also reads "analizando", so the header and the pills never contradict.
 */
export type SeriesStatusSummary = { ready: number; pending: number; failed: number; skipped: number };

/** Counts each status into its header bucket; drives the series page description. */
export function summarizeSeriesStatuses(statuses: readonly string[]): SeriesStatusSummary {
  const summary: SeriesStatusSummary = { ready: 0, pending: 0, failed: 0, skipped: 0 };
  for (const status of statuses) {
    if (seriesStatusIsForgeable(status)) summary.ready += 1;
    else if (status === 'failed') summary.failed += 1;
    else if (status === 'scanned') summary.skipped += 1;
    else summary.pending += 1;
  }
  return summary;
}
