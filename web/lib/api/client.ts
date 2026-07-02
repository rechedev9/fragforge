import type { Session, Match, Play, Song, Video, FeedItem, RenderMode, DemoPlayer, Preset, EditConfig, CaptureReadiness, RosterMatch } from './types';

export interface ApiClient {
  getSession(): Promise<Session>;
  signInWithSteam(): Promise<Session>;
  signOut(): Promise<void>;
  linkMatchHistory(input: { authCode: string; knownCode: string }): Promise<{ ok: boolean; matchesFound: number }>;
  pairPc(): Promise<{ pairingCode: string }>;
  getPcStatus(): Promise<{ paired: boolean }>;
  /** Whether gameplay capture (HLAE + CS2) is configured on the local machine. */
  getCaptureReadiness(): Promise<CaptureReadiness>;
  listMatches(): Promise<Match[]>;
  getMatch(id: string): Promise<Match | null>;
  /** @deprecated Superseded by scanDemo + parseDemo (roster scan → target pick). */
  uploadDemo(input: { fileName: string }): Promise<Match>;
  /** Roster scan: returns the demo's players (K/D/A) so the user can pick a target. */
  scanDemo(file: File): Promise<{ jobId: string; players: DemoPlayer[]; match?: RosterMatch }>;
  /** Parse the scanned demo for the chosen player and return its Match. */
  parseDemo(input: { jobId: string; steamId: string }): Promise<Match>;
  findClips(matchId: string): Promise<Play[]>;
  listSongs(): Promise<Song[]>;
  /** The user-selectable reel presets (preset name == render variant). */
  listPresets(): Promise<Preset[]>;
  /** playIds is 2+ ids for a concatenated reel; pass them in plan order, not click order. */
  createVideo(input: { matchId: string; playIds: string[]; mode: RenderMode; songId?: string; variant?: string; editConfig?: EditConfig }): Promise<Video>;
  listVideos(): Promise<Video[]>;
  getVideo(id: string): Promise<Video | null>;
  publishVideo(id: string): Promise<Video>;
  /** Re-drive a failed reel from where it failed (re-record or re-render). */
  retryVideo(id: string): Promise<Video>;
  listFeed(): Promise<FeedItem[]>;
}
