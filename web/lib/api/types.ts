export type SteamUser = { id: string; personaName: string; avatarUrl: string };
export type MatchStats = { kills: number; deaths: number; assists: number; mvps: number; kd: number };
export type Match = { id: string; map: string; score: string; playedAt: string; stats: MatchStats; decentPlays: number; thumbnailUrl?: string; source?: 'steam' | 'upload' };
export type PlayKind = 'clean' | 'highlight';
export type Play = { id: string; matchId: string; label: string; kind: PlayKind; round: number; kills: number; weapon?: string; thumbnailUrl?: string };
export type RenderMode = 'clean' | 'music';
export type RenderFormat = 'short-9x16' | 'landscape-16x9';
export type KillEffect = 'clean' | 'punch-in' | 'velocity' | 'freeze-flash';
export type TransitionStyle = 'cut' | 'flash' | 'whip' | 'dip';
export type EditConfig = {
  format: RenderFormat;
  killEffect: KillEffect;
  transition: TransitionStyle;
  intro: boolean;
  outro: boolean;
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
/** One player from a roster scan of an uploaded demo; the user picks who to clip. */
export type DemoPlayer = { steamId: string; name: string; team: 'CT' | 'T' | ''; kills: number; deaths: number; assists: number };

/**
 * Stable error code returned by the /api/demos/* proxy routes when the local
 * analysis service (the orchestrator) is unreachable, and the code the client
 * branches on to tell "backend offline" apart from "bad demo". Shared so server
 * and client agree on one string instead of sniffing messages.
 */
export const SERVICE_UNAVAILABLE_CODE = 'service_unavailable';
