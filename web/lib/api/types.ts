export type SteamUser = { id: string; personaName: string; avatarUrl: string };
export type MatchStats = {
  kills: number;
  deaths: number;
  assists: number;
  mvps: number;
  kd: number;
  /** Scoreboard extras from the enriched scan; absent on mock/seed matches. */
  rating?: number;
  adr?: number;
  kast?: number;
  hsPct?: number;
};
export type Match = { id: string; map: string; score: string; playedAt: string; stats: MatchStats; decentPlays: number; thumbnailUrl?: string; source?: 'steam' | 'upload' };
export type PlayKind = 'clean' | 'highlight';
export type Play = { id: string; matchId: string; label: string; kind: PlayKind; round: number; kills: number; weapon?: string; thumbnailUrl?: string };
export type RenderMode = 'clean' | 'music';
export type RenderFormat = 'short-9x16' | 'landscape-16x9';
export type KillEffect = 'clean' | 'punch-in' | 'velocity' | 'freeze-flash';
export type TransitionStyle = 'cut' | 'flash' | 'whip' | 'dip';
/** Max length (trimmed) for the intro/outro bookend text, enforced client-side via `maxLength`. */
export const BOOKEND_TEXT_MAX_LENGTH = 80;
export type EditConfig = {
  format: RenderFormat;
  killEffect: KillEffect;
  transition: TransitionStyle;
  intro: boolean;
  outro: boolean;
  /** Optional intro headline override, shown only while `intro` is on; empty = generated headline. */
  introText?: string;
  /** Optional outro text override, shown only while `outro` is on; empty = "FragForge". */
  outroText?: string;
};
export type Song = { id: string; title: string; artist: string; genre: string; previewUrl: string; durationSec: number; license?: string };
/**
 * A user-selectable reel preset. `name` is the render variant; picking it sets
 * both the recording HUD and the render style. `hudMode` is shown for context.
 */
export type Preset = { name: string; label: string; description: string; hudMode?: string; default?: boolean };
export type VideoStatus = 'queued' | 'recording' | 'composing' | 'ready' | 'failed';
export type Video = { id: string; title: string; map: string; score: string; mode: RenderMode; variant?: string; songId?: string; editConfig?: EditConfig; status: VideoStatus; createdAt: number; availableForSec?: number; thumbnailUrl?: string; published: boolean; downloadUrl?: string; failureReason?: string };
export type Slots = { used: number; total: number };
export type FeedItem = { id: string; author: string; authorAvatarUrl: string; title: string; map: string; thumbnailUrl: string; likes: number; createdAt: number; videoUrl: string };
export type Session = { user: SteamUser | null; slots: Slots; pcPaired: boolean; matchHistoryLinked: boolean };
/**
 * One player from a roster scan of an uploaded demo; the user picks who to clip.
 * The scoreboard fields (headshots..rating) come from the enriched parser scan;
 * they default to 0 on the fallback paths that predate the richer scan.
 */
export type DemoPlayer = {
  steamId: string;
  name: string;
  team: 'CT' | 'T' | '';
  kills: number;
  deaths: number;
  assists: number;
  headshots: number;
  mvps: number;
  rounds: number;
  adr: number;
  hsPct: number;
  kast: number;
  rating: number;
  /**
   * Multi-kill round counts from the enriched scan. Optional and absent on
   * artifacts recorded before this field existed, so callers must tolerate
   * `undefined` (treat as 0) rather than assume every player carries them.
   */
  rounds2k?: number;
  rounds3k?: number;
  rounds4k?: number;
  rounds5k?: number;
};

/**
 * Match-level context for the roster scoreboard header (map, final score,
 * rounds played). Optional on the scan response; absent on artifacts that
 * predate this field or when the parser could not determine it.
 */
export type RosterMatch = {
  map: string;
  scoreCt: number;
  scoreT: number;
  rounds: number;
};

/**
 * Stable error code returned by the /api/demos/* proxy routes when the local
 * analysis service (the orchestrator) is unreachable, and the code the client
 * branches on to tell "backend offline" apart from "bad demo". Shared so server
 * and client agree on one string instead of sniffing messages.
 */
export const SERVICE_UNAVAILABLE_CODE = 'service_unavailable';

/**
 * One external capture tool (recorder/HLAE/CS2) and its readiness on this PC.
 * `source` is how the path was resolved: 'detected' (auto-found on the machine),
 * 'env' (set explicitly), or 'none' (not found - the user must install/set it).
 */
export type CaptureTool = {
  name: string;
  path?: string;
  source?: 'env' | 'detected' | 'none';
  configured: boolean;
  accessible: boolean;
};

/**
 * Whether gameplay capture (HLAE + CS2 recording) is set up on the local machine.
 * - ready: the record worker is enabled and every tool path exists.
 * - warning: enabled but a configured path is missing (e.g. the wrong HLAE install).
 * - unconfigured: the record worker is off (no tool paths set).
 * - offline: the local orchestrator could not be reached.
 */
export type CaptureStatus = 'ready' | 'warning' | 'unconfigured' | 'offline';
export type CaptureReadiness = {
  recordEnabled: boolean;
  status: CaptureStatus;
  tools: CaptureTool[];
  reason?: string;
};
