import type { Session, Match, Play, Song, Video, FeedItem, RenderMode } from './types';

export interface ApiClient {
  getSession(): Promise<Session>;
  signInWithSteam(): Promise<Session>;
  signOut(): Promise<void>;
  linkMatchHistory(input: { authCode: string; knownCode: string }): Promise<{ ok: boolean; matchesFound: number }>;
  pairPc(): Promise<{ pairingCode: string }>;
  getPcStatus(): Promise<{ paired: boolean }>;
  listMatches(): Promise<Match[]>;
  getMatch(id: string): Promise<Match | null>;
  findClips(matchId: string): Promise<Play[]>;
  listSongs(): Promise<Song[]>;
  createVideo(input: { matchId: string; playId: string; mode: RenderMode; songId?: string }): Promise<Video>;
  listVideos(): Promise<Video[]>;
  getVideo(id: string): Promise<Video | null>;
  publishVideo(id: string): Promise<Video>;
  listFeed(): Promise<FeedItem[]>;
}
