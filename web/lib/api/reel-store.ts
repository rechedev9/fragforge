import type { EditConfig, RenderMode } from './types';

// Mirrors types.BOOKEND_TEXT_MAX_LENGTH. Duplicated (not imported) so this module
// stays a type-only consumer of ./types, which keeps it runnable directly under
// Node's native TS loader (reel-store.test.mjs) without an explicit .ts import
// extension, which `tsc` (moduleResolution: bundler) rejects.
const BOOKEND_TEXT_MAX_LENGTH = 80;

/**
 * A reel the user asked for — the durable fact, persisted to localStorage so the
 * Library survives a hard reload / direct visit. Status, downloadUrl, and failure
 * reason are NOT stored: they are derived live from the orchestrator on each poll
 * (see reel-reconcile), which is the single source of truth.
 */
export type ReelIntent = {
  videoId: string; // `${jobId}__${segmentIds.join('_')}`
  jobId: string;
  /** Segment ids in plan order; 2+ ids render as one concatenated reel. */
  segmentIds: string[];
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
  introText: '',
  outroText: '',
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
    if (!v || typeof v !== 'object') continue;
    const r = v as Record<string, unknown>;
    if (typeof r.videoId !== 'string' || typeof r.jobId !== 'string') continue;
    const segmentIds = coerceSegmentIds(r);
    if (segmentIds.length === 0) continue;
    out.push({
      videoId: r.videoId,
      jobId: r.jobId,
      segmentIds,
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
  return out;
}

/**
 * Reads segment ids off a parsed intent: the current `segmentIds` array, or
 * (for reels persisted before multi-select) the legacy singular `segmentId`
 * string wrapped into a one-element array. Non-string entries are dropped
 * rather than coerced, so a corrupt array never smuggles a non-id through.
 */
function coerceSegmentIds(r: Record<string, unknown>): string[] {
  if (Array.isArray(r.segmentIds)) {
    return r.segmentIds.filter((entry): entry is string => typeof entry === 'string' && entry.length > 0);
  }
  if (typeof r.segmentId === 'string' && r.segmentId.length > 0) return [r.segmentId];
  return [];
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
    introText: coerceBookendText(raw.introText),
    outroText: coerceBookendText(raw.outroText),
  };
}

function coerceBookendText(value: unknown): string {
  return typeof value === 'string' ? value.slice(0, BOOKEND_TEXT_MAX_LENGTH) : '';
}

function isKillEffect(value: unknown): value is EditConfig['killEffect'] {
  return value === 'clean' || value === 'punch-in' || value === 'velocity' || value === 'freeze-flash';
}

function isTransition(value: unknown): value is EditConfig['transition'] {
  return value === 'cut' || value === 'flash' || value === 'whip' || value === 'dip';
}
