// Grouping helpers for a bulk demo series (bo3/bo5). HLTV-style downloads split
// one map across several .dem parts ("...-m1-inferno-p1.dem",
// "...-m1-inferno-p2.dem"); the orchestrator surfaces each part as its own demo
// job, so the series view must fold the parts of one map back into a single
// logical map card. The only join key is the original upload file name, which
// the orchestrator preserves. Pure and unit-tested so the view never parses
// file names inline.

import { seriesStatusIsForgeable, seriesStatusIsPending } from './series-status.ts';

/**
 * A file name parsed into the tokens the series view groups on. `part` is the
 * `-p<n>` suffix immediately before the (optional) `.dem` extension; `base` is
 * the name with that suffix and extension stripped, and is the join key for
 * parts of the same map. `mapOrder` is the `m<n>` map-number token when present,
 * used to order whole maps within the series. Missing tokens are `null`.
 */
export type ParsedSeriesFileName = { base: string; mapOrder: number | null; part: number | null };

/**
 * The one status a multi-part map counts as in map-level summaries (the series
 * header buckets). Forgeable wins: as soon as any part has a kill plan the map
 * honestly reads "con jugadas listas" (the user can forge it), even while a
 * sibling part is still parsing. Then pending (still working), then failed,
 * then whatever settled state is left (e.g. scanned without the player).
 */
export function representativeSeriesStatus(statuses: readonly string[]): string {
  const forgeable = statuses.find(seriesStatusIsForgeable);
  if (forgeable !== undefined) return forgeable;
  const pending = statuses.find(seriesStatusIsPending);
  if (pending !== undefined) return pending;
  const failed = statuses.find((status) => status === 'failed');
  if (failed !== undefined) return failed;
  return statuses[0] ?? 'scanned';
}

/** A logical map card: one map's parts, sorted, plus its series-ordering key. */
export type SeriesGroup<T> = { key: string; mapOrder: number | null; demos: T[] };

/** Trailing `.dem` extension; optional, so a name without it is handled too. */
const DEM_EXTENSION_RE = /\.dem$/i;
/** The `-p<n>` part suffix at the very end of the extension-less name. */
const PART_SUFFIX_RE = /-p(\d+)$/i;
/** The `m<n>` map-number token, delimited by dashes or the string bounds. */
const MAP_ORDER_RE = /(?:^|-)m(\d+)(?:-|$)/i;

/**
 * Split a series file name into its base, map order and part number. The `.dem`
 * extension is optional and matched case-insensitively; so is the `-p<n>` part
 * suffix. Only the part suffix is stripped from `base`, so two parts of one map
 * share a base while different maps never collide.
 */
export function parseSeriesFileName(fileName: string): ParsedSeriesFileName {
  const nameNoExt = fileName.replace(DEM_EXTENSION_RE, '');
  const partMatch = PART_SUFFIX_RE.exec(nameNoExt);
  const part = partMatch ? Number.parseInt(partMatch[1], 10) : null;
  const base = partMatch ? nameNoExt.slice(0, partMatch.index) : nameNoExt;
  const mapMatch = MAP_ORDER_RE.exec(base);
  const mapOrder = mapMatch ? Number.parseInt(mapMatch[1], 10) : null;
  return { base, mapOrder, part };
}

/** A part-suffixed demo's part number for member ordering; 0 when absent. */
function partNumberOf(fileName: string | undefined): number {
  if (fileName === undefined) return 0;
  return parseSeriesFileName(fileName).part ?? 0;
}

/**
 * Fold a series' demos into one group per logical map. Demos whose file name
 * carries a `-p<n>` part suffix and share the same (lowercased) base collapse
 * into a single group, its members sorted by part number; every other demo (no
 * file name, or no part suffix) is its own singleton group. Two maps with the
 * same map name but different bases stay separate.
 *
 * Group order: when every group carries a map-number token, groups are sorted by
 * it ascending (stably); otherwise their first-appearance order is preserved, so
 * a series without reliable map numbers keeps the server's order.
 */
export function groupSeriesDemos<T extends { fileName?: string; jobId?: string }>(demos: readonly T[]): Array<SeriesGroup<T>> {
  const groups: Array<SeriesGroup<T>> = [];
  const partGroupByBase = new Map<string, SeriesGroup<T>>();
  const seenJobs = new Set<string>();

  demos.forEach((demo, index) => {
    if (demo.jobId !== undefined) {
      if (seenJobs.has(demo.jobId)) return;
      seenJobs.add(demo.jobId);
    }
    const parsed = demo.fileName !== undefined ? parseSeriesFileName(demo.fileName) : null;
    if (parsed !== null && parsed.part !== null) {
      const key = parsed.base.toLowerCase();
      const existing = partGroupByBase.get(key);
      if (existing) {
        existing.demos.push(demo);
      } else {
        const group: SeriesGroup<T> = { key, mapOrder: parsed.mapOrder, demos: [demo] };
        partGroupByBase.set(key, group);
        groups.push(group);
      }
    } else {
      // Singleton: a unique key keeps two extensionless/part-less demos apart.
      groups.push({ key: `#${index}`, mapOrder: parsed?.mapOrder ?? null, demos: [demo] });
    }
  });

  for (const group of groups) {
    if (group.demos.length > 1) {
      group.demos.sort((a, b) => partNumberOf(a.fileName) - partNumberOf(b.fileName));
    }
  }

  const everyHasMapOrder = groups.every((group) => group.mapOrder !== null);
  if (everyHasMapOrder) {
    // `?? 0` never fires here (every mapOrder is non-null); it only spares a cast.
    return [...groups].sort((a, b) => (a.mapOrder ?? 0) - (b.mapOrder ?? 0));
  }
  return groups;
}
