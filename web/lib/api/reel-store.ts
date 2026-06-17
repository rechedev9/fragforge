import type { RenderMode } from './types';

/**
 * A reel the user asked for — the durable fact, persisted to localStorage so the
 * Library survives a hard reload / direct visit. Status, downloadUrl, and failure
 * reason are NOT stored: they are derived live from the orchestrator on each poll
 * (see reel-reconcile), which is the single source of truth.
 */
export type ReelIntent = {
  videoId: string; // `${jobId}__${segmentId}`
  jobId: string;
  segmentId: string;
  mode: RenderMode;
  songId?: string;
  title: string;
  map: string;
  score: string;
  createdAt: number;
  published: boolean;
};

const STORE_KEY = 'fragforge.reels.v1';
/** Keep localStorage bounded; newest intents win. */
const MAX_INTENTS = 50;

export function loadReelIntents(): ReelIntent[] {
  if (typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(STORE_KEY);
    return raw ? coerceIntents(JSON.parse(raw)) : [];
  } catch {
    return []; // corrupt / unavailable storage: reels are best-effort.
  }
}

export function saveReelIntents(list: ReelIntent[]): void {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(STORE_KEY, JSON.stringify(list.slice(-MAX_INTENTS)));
  } catch {
    // quota / privacy mode: in-memory reels still work this session.
  }
}

/**
 * Validates parsed JSON into well-formed intents, dropping anything malformed and
 * defaulting soft fields. Pure (no window) and unit-tested in reel-store.test.mjs.
 */
export function coerceIntents(parsed: unknown): ReelIntent[] {
  if (!Array.isArray(parsed)) return [];
  const out: ReelIntent[] = [];
  for (const v of parsed) {
    if (
      v &&
      typeof v === 'object' &&
      typeof (v as ReelIntent).videoId === 'string' &&
      typeof (v as ReelIntent).jobId === 'string' &&
      typeof (v as ReelIntent).segmentId === 'string'
    ) {
      const r = v as Partial<ReelIntent> & { videoId: string; jobId: string; segmentId: string };
      out.push({
        videoId: r.videoId,
        jobId: r.jobId,
        segmentId: r.segmentId,
        mode: r.mode === 'music' ? 'music' : 'clean',
        songId: typeof r.songId === 'string' ? r.songId : undefined,
        title: typeof r.title === 'string' ? r.title : 'Highlight',
        map: typeof r.map === 'string' ? r.map : 'Unknown',
        score: typeof r.score === 'string' ? r.score : '',
        createdAt: typeof r.createdAt === 'number' ? r.createdAt : 0,
        published: r.published === true,
      });
    }
  }
  return out;
}
