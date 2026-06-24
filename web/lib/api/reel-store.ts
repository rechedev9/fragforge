import type { EditConfig, RenderMode } from './types';

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
  /** Render variant / preset name (Kill Feed / Clean POV / Full HUD). */
  variant?: string;
  editConfig: EditConfig;
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

/**
 * Default render variant. Also the migration target for intents persisted before
 * preset selection existed: those reels were recorded with the orchestrator's
 * default HUD, which is exactly this preset's HUD, so defaulting them to it (not
 * leaving variant undefined) keeps a later retry's re-record visually identical.
 */
export const DEFAULT_VARIANT = 'viral-60-clean';

export const DEFAULT_EDIT_CONFIG: EditConfig = {
  format: 'short-9x16',
  killEffect: 'punch-in',
  transition: 'flash',
  intro: false,
  outro: false,
};

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
        variant: typeof r.variant === 'string' ? r.variant : DEFAULT_VARIANT,
        editConfig: coerceEditConfig(r.editConfig),
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

export function coerceEditConfig(value: unknown): EditConfig {
  if (!value || typeof value !== 'object') return DEFAULT_EDIT_CONFIG;
  const raw = value as Partial<EditConfig>;
  return {
    format: raw.format === 'landscape-16x9' ? 'landscape-16x9' : DEFAULT_EDIT_CONFIG.format,
    killEffect: isKillEffect(raw.killEffect) ? raw.killEffect : DEFAULT_EDIT_CONFIG.killEffect,
    transition: isTransition(raw.transition) ? raw.transition : DEFAULT_EDIT_CONFIG.transition,
    intro: raw.intro === true,
    outro: raw.outro === true,
  };
}

function isKillEffect(value: unknown): value is EditConfig['killEffect'] {
  return value === 'clean' || value === 'punch-in' || value === 'velocity' || value === 'freeze-flash';
}

function isTransition(value: unknown): value is EditConfig['transition'] {
  return value === 'cut' || value === 'flash' || value === 'whip' || value === 'dip';
}
