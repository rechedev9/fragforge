import type { ApiClient } from './client';
import type { Session, Match, Play, Song, Video, FeedItem, RenderMode, VideoStatus } from './types';
import {
  fixtureUser,
  fixtureSlots,
  fixtureMatches,
  fixtureSongs,
  fixtureFeed,
  playsForMatch,
  seedVideos,
} from './fixtures';

/**
 * Mutable in-memory state at module scope so a single browser session keeps its
 * progress across navigations (the module is a singleton via lib/api/index).
 */
const session: Session = {
  user: null,
  slots: { ...fixtureSlots },
  pcPaired: false,
  matchHistoryLinked: false,
};

const videos: Video[] = seedVideos();

/** Set by pairPc so the next getPcStatus reports the PC as paired. */
let pcPaired = false;

function delay(): Promise<void> {
  const ms = 150 + Math.floor(Math.random() * 250); // 150-400ms
  return new Promise((resolve) => setTimeout(resolve, ms));
}

const THUMB_BASE = 'https://picsum.photos/seed';

/**
 * Recomputes a video's status from how long ago it was created, so the UI can
 * poll and watch progress without any timers running in the mock:
 *   <2s queued, <6s recording, <10s composing, else ready.
 * Pre-ready videos keep their stored status (already-ready seeds stay ready).
 */
function project(video: Video): Video {
  if (video.status === 'failed') return video;

  const elapsed = (Date.now() - video.createdAt) / 1000;
  let status: VideoStatus;
  if (elapsed < 2) status = 'queued';
  else if (elapsed < 6) status = 'recording';
  else if (elapsed < 10) status = 'composing';
  else status = 'ready';

  const next: Video = { ...video, status };
  if (status === 'ready' && !next.downloadUrl) {
    next.downloadUrl = `https://example.com/mock/${video.id}.mp4`;
  }
  return next;
}

export class MockApiClient implements ApiClient {
  async getSession(): Promise<Session> {
    await delay();
    return cloneSession();
  }

  async signInWithSteam(): Promise<Session> {
    await delay();
    session.user = { ...fixtureUser };
    return cloneSession();
  }

  async signOut(): Promise<void> {
    await delay();
    session.user = null;
    session.matchHistoryLinked = false;
    session.pcPaired = false;
    pcPaired = false;
  }

  async linkMatchHistory(_input: { authCode: string; knownCode: string }): Promise<{ ok: boolean; matchesFound: number }> {
    await delay();
    session.matchHistoryLinked = true;
    return { ok: true, matchesFound: fixtureMatches.length };
  }

  async pairPc(): Promise<{ pairingCode: string }> {
    await delay();
    pcPaired = true;
    const code = `CS2V-${randomCode()}`;
    return { pairingCode: code };
  }

  async getPcStatus(): Promise<{ paired: boolean }> {
    await delay();
    session.pcPaired = pcPaired;
    return { paired: pcPaired };
  }

  async listMatches(): Promise<Match[]> {
    await delay();
    return fixtureMatches.map((m) => ({ ...m, stats: { ...m.stats } }));
  }

  async getMatch(id: string): Promise<Match | null> {
    await delay();
    const match = fixtureMatches.find((m) => m.id === id);
    return match ? { ...match, stats: { ...match.stats } } : null;
  }

  async findClips(matchId: string): Promise<Play[]> {
    await delay();
    return playsForMatch(matchId);
  }

  async listSongs(): Promise<Song[]> {
    await delay();
    return fixtureSongs.map((s) => ({ ...s }));
  }

  async createVideo(input: { matchId: string; playId: string; mode: RenderMode; songId?: string }): Promise<Video> {
    await delay();
    const match = fixtureMatches.find((m) => m.id === input.matchId);
    const play = playsForMatch(input.matchId).find((p) => p.id === input.playId);

    const modeLabel = input.mode === 'music' ? 'Music Edit' : 'Clean POV';
    const playLabel = play?.label ?? 'Highlight';
    const id = `v-${Date.now()}`;

    const video: Video = {
      id,
      title: `${playLabel} - ${modeLabel}`,
      map: match?.map ?? 'Unknown',
      score: match?.score ?? '',
      mode: input.mode,
      songId: input.songId,
      status: 'queued',
      createdAt: Date.now(),
      availableForSec: 14 * 3600,
      thumbnailUrl: `${THUMB_BASE}/${id}/640/360`,
      published: false,
    };

    videos.unshift(video);
    session.slots.used += 1;
    return project(video);
  }

  async listVideos(): Promise<Video[]> {
    await delay();
    return videos.map(project);
  }

  async getVideo(id: string): Promise<Video | null> {
    await delay();
    const video = videos.find((v) => v.id === id);
    return video ? project(video) : null;
  }

  async publishVideo(id: string): Promise<Video> {
    await delay();
    const video = videos.find((v) => v.id === id);
    if (!video) throw new Error(`video not found: ${id}`);
    video.published = true;
    return project(video);
  }

  async listFeed(): Promise<FeedItem[]> {
    await delay();
    return fixtureFeed.map((f) => ({ ...f }));
  }
}

function cloneSession(): Session {
  return {
    user: session.user ? { ...session.user } : null,
    slots: { ...session.slots },
    pcPaired: session.pcPaired,
    matchHistoryLinked: session.matchHistoryLinked,
  };
}

function randomCode(): string {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';
  let out = '';
  for (let i = 0; i < 4; i++) {
    out += chars[Math.floor(Math.random() * chars.length)];
  }
  return out;
}
