import type { Session, Match, Play, Song, Video, FeedItem, RenderMode, DemoPlayer } from './types';

export interface ApiClient {
  getSession(): Promise<Session>;
  signInWithSteam(): Promise<Session>;
  signOut(): Promise<void>;
  linkMatchHistory(input: { authCode: string; knownCode: string }): Promise<{ ok: boolean; matchesFound: number }>;
  pairPc(): Promise<{ pairingCode: string }>;
  getPcStatus(): Promise<{ paired: boolean }>;
  listMatches(): Promise<Match[]>;
  getMatch(id: string): Promise<Match | null>;
  /** @deprecated Superseded by scanDemo + parseDemo (roster scan → target pick). */
  uploadDemo(input: { fileName: string }): Promise<Match>;
  /** Roster scan: returns the demo's players (K/D/A) so the user can pick a target. */
  scanDemo(file: File): Promise<{ jobId: string; players: DemoPlayer[] }>;
  /** Parse the scanned demo for the chosen player and return its Match. */
  parseDemo(input: { jobId: string; steamId: string }): Promise<Match>;
  findClips(matchId: string): Promise<Play[]>;
  listSongs(): Promise<Song[]>;
  createVideo(input: { matchId: string; playId: string; mode: RenderMode; songId?: string }): Promise<Video>;
  listVideos(): Promise<Video[]>;
  getVideo(id: string): Promise<Video | null>;
  publishVideo(id: string): Promise<Video>;
  listFeed(): Promise<FeedItem[]>;
}
