import type { Match, Play, Song, Video, FeedItem, SteamUser, Slots } from './types';

/**
 * Static mock data backing the MockApiClient. None of these reference real
 * binaries; thumbnails/avatars use deterministic placeholder URLs so the same
 * seed always renders the same image.
 */

function avatar(seed: string): string {
  return `https://api.dicebear.com/7.x/bottts/svg?seed=${encodeURIComponent(seed)}`;
}

function thumb(seed: string): string {
  return `https://picsum.photos/seed/${encodeURIComponent(seed)}/640/360`;
}

export const fixtureUser: SteamUser = {
  id: '7656',
  personaName: 'kekO',
  avatarUrl: avatar('kekO'),
};

export const fixtureSlots: Slots = { used: 1, total: 2 };

export const fixtureMatches: Match[] = [
  {
    id: 'm-inferno',
    map: 'Inferno',
    score: '13-2',
    playedAt: '2026-06-15T21:40:00Z',
    stats: { kills: 18, deaths: 4, assists: 3, mvps: 2, kd: 2.38 },
    decentPlays: 2,
    thumbnailUrl: thumb('m-inferno'),
  },
  {
    id: 'm-mirage',
    map: 'Mirage',
    score: '16-12',
    playedAt: '2026-06-15T19:05:00Z',
    stats: { kills: 6, deaths: 12, assists: 3, mvps: 2, kd: 0.42 },
    decentPlays: 1,
    thumbnailUrl: thumb('m-mirage'),
  },
  {
    id: 'm-nuke',
    map: 'Nuke',
    score: '13-9',
    playedAt: '2026-06-14T23:18:00Z',
    stats: { kills: 21, deaths: 15, assists: 4, mvps: 1, kd: 1.4 },
    decentPlays: 2,
    thumbnailUrl: thumb('m-nuke'),
  },
  {
    id: 'm-ancient',
    map: 'Ancient',
    score: '13-7',
    playedAt: '2026-06-14T20:42:00Z',
    stats: { kills: 17, deaths: 11, assists: 2, mvps: 1, kd: 1.55 },
    decentPlays: 1,
    thumbnailUrl: thumb('m-ancient'),
  },
  {
    id: 'm-dust2',
    map: 'Dust2',
    score: '10-13',
    playedAt: '2026-06-13T22:11:00Z',
    stats: { kills: 14, deaths: 16, assists: 5, mvps: 0, kd: 0.88 },
    decentPlays: 1,
    thumbnailUrl: thumb('m-dust2'),
  },
  {
    id: 'm-anubis',
    map: 'Anubis',
    score: '13-11',
    playedAt: '2026-06-13T18:30:00Z',
    stats: { kills: 23, deaths: 14, assists: 6, mvps: 2, kd: 1.64 },
    decentPlays: 2,
    thumbnailUrl: thumb('m-anubis'),
  },
];

type PlaySeed = Omit<Play, 'id' | 'matchId'>;

/** Highlight plays keyed by match id; each becomes a "decent play" card. */
const playSeedsByMatch: Record<string, PlaySeed[]> = {
  'm-inferno': [
    { label: '5K - Pistol round', kind: 'highlight', round: 1, kills: 5, weapon: 'USP-S' },
    { label: '4K - AWP flick', kind: 'highlight', round: 14, kills: 4, weapon: 'AWP' },
  ],
  'm-mirage': [
    { label: '3K - Connector hold', kind: 'highlight', round: 9, kills: 3, weapon: 'AK-47' },
  ],
  'm-nuke': [
    { label: '4K - Ramp peek', kind: 'highlight', round: 6, kills: 4, weapon: 'M4A1-S' },
    { label: '3K - Lobby clutch', kind: 'highlight', round: 19, kills: 3, weapon: 'Deagle' },
  ],
  'm-ancient': [
    { label: '4K - Mid control', kind: 'highlight', round: 11, kills: 4, weapon: 'AK-47' },
  ],
  'm-dust2': [
    { label: '3K - Long defense', kind: 'highlight', round: 7, kills: 3, weapon: 'AWP' },
  ],
  'm-anubis': [
    { label: '5K - Site retake', kind: 'highlight', round: 16, kills: 5, weapon: 'AK-47' },
    { label: '4K - Water push', kind: 'highlight', round: 3, kills: 4, weapon: 'M4A4' },
  ],
};

/** Builds the concrete Play list (with ids) for a given match. */
export function playsForMatch(matchId: string): Play[] {
  const seeds = playSeedsByMatch[matchId] ?? [];
  return seeds.map((seed, i) => ({
    id: `${matchId}-p${i + 1}`,
    matchId,
    thumbnailUrl: thumb(`${matchId}-p${i + 1}`),
    ...seed,
  }));
}

export const fixtureSongs: Song[] = [
  { id: 'song-tikitaka-1', title: 'TikiTakaYala 1', artist: 'TikiTakaYala', genre: 'Phonk', previewUrl: '', durationSec: 30 },
  { id: 'song-tikitaka-2', title: 'TikiTakaYala 2', artist: 'TikiTakaYala', genre: 'Phonk', previewUrl: '', durationSec: 30 },
  { id: 'song-zerokull-1', title: 'ZeroKull 1', artist: 'ZeroKull', genre: 'Phonk', previewUrl: '', durationSec: 30 },
  { id: 'song-zerokull-2', title: 'ZeroKull 2', artist: 'ZeroKull', genre: 'Phonk', previewUrl: '', durationSec: 30 },
];

/**
 * Seed videos. One is already ready+published; one is mid-render (recent
 * createdAt so the elapsed-time status mapping in the mock client reports it as
 * recording/composing). Status here is a starting value; listVideos/getVideo
 * recompute it from createdAt.
 */
export function seedVideos(): Video[] {
  const now = Date.now();
  return [
    {
      id: 'v-seed-ready',
      title: '5K - Clean POV',
      map: 'Inferno',
      score: '13-2',
      mode: 'clean',
      status: 'ready',
      createdAt: now - 6 * 3600 * 1000,
      availableForSec: 14 * 3600,
      thumbnailUrl: thumb('v-seed-ready'),
      published: true,
      downloadUrl: 'https://example.com/mock/v-seed-ready.mp4',
    },
    {
      id: 'v-seed-rendering',
      title: '4K - Music Edit',
      map: 'Inferno',
      score: '13-2',
      mode: 'music',
      songId: 'song-zerokull-1',
      status: 'recording',
      createdAt: now - 4 * 1000,
      availableForSec: 14 * 3600,
      thumbnailUrl: thumb('v-seed-rendering'),
      published: false,
    },
  ];
}

export const fixtureFeed: FeedItem[] = [
  {
    id: 'feed-1',
    author: 'RaiSeNN',
    authorAvatarUrl: avatar('RaiSeNN'),
    title: '4K - Music Edit',
    map: 'Mirage',
    thumbnailUrl: thumb('feed-1'),
    likes: 482,
    createdAt: Date.now() - 2 * 3600 * 1000,
    videoUrl: 'https://example.com/mock/feed-1.mp4',
  },
  {
    id: 'feed-2',
    author: 'granz',
    authorAvatarUrl: avatar('granz'),
    title: '5K - Clean POV',
    map: 'Inferno',
    thumbnailUrl: thumb('feed-2'),
    likes: 1203,
    createdAt: Date.now() - 5 * 3600 * 1000,
    videoUrl: 'https://example.com/mock/feed-2.mp4',
  },
  {
    id: 'feed-3',
    author: 'mcyans',
    authorAvatarUrl: avatar('mcyans'),
    title: '3K - Clutch',
    map: 'Nuke',
    thumbnailUrl: thumb('feed-3'),
    likes: 87,
    createdAt: Date.now() - 26 * 3600 * 1000,
    videoUrl: 'https://example.com/mock/feed-3.mp4',
  },
  {
    id: 'feed-4',
    author: 'Revol',
    authorAvatarUrl: avatar('Revol'),
    title: '4K - AWP Highlight',
    map: 'Ancient',
    thumbnailUrl: thumb('feed-4'),
    likes: 351,
    createdAt: Date.now() - 50 * 3600 * 1000,
    videoUrl: 'https://example.com/mock/feed-4.mp4',
  },
  {
    id: 'feed-5',
    author: 'RaiSeNN',
    authorAvatarUrl: avatar('RaiSeNN'),
    title: '5K - Music Edit',
    map: 'Anubis',
    thumbnailUrl: thumb('feed-5'),
    likes: 902,
    createdAt: Date.now() - 72 * 3600 * 1000,
    videoUrl: 'https://example.com/mock/feed-5.mp4',
  },
];
