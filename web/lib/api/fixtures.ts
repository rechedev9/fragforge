import type { Match, Play, Song, Video, FeedItem, SteamUser, Slots, DemoPlayer, RosterMatch } from './types';

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

/**
 * A locally-served sample clip (same-origin, from web/public) so mock "ready"
 * reels and feed items are actually playable/downloadable in the demo with no
 * external dependency. scripts/run-local.sh generates it; reels produced by the
 * real pipeline (uploaded demos) stream from the orchestrator instead.
 */
export const SAMPLE_REEL_URL = '/sample-reel.mp4';

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

/** Maps an uploaded demo can land on; the file name picks a stable one. */
const uploadMapPool = ['Inferno', 'Mirage', 'Nuke', 'Ancient', 'Dust2', 'Anubis', 'Overpass', 'Vertigo'];

/**
 * Raw (de_/cs_-prefixed) map pool for the roster scan's match header, mirroring
 * the backend's wire format so the UI's map-prettifying logic is exercised in
 * the mock too, the same way it would be against a real orchestrator response.
 */
const rosterMapPool = ['de_inferno', 'de_mirage', 'de_nuke', 'de_ancient', 'de_dust2', 'de_anubis', 'de_overpass', 'de_vertigo'];

/** Highlight templates an uploaded demo's plays are drawn from (first N used). */
const uploadPlaySeeds: PlaySeed[] = [
  { label: '4K - Site retake', kind: 'highlight', round: 12, kills: 4, weapon: 'AK-47' },
  { label: '3K - Clutch hold', kind: 'highlight', round: 7, kills: 3, weapon: 'M4A1-S' },
  { label: '5K - Eco frags', kind: 'highlight', round: 3, kills: 5, weapon: 'USP-S' },
];

/** Small stable string hash so the same file name always yields the same map. */
function hashName(name: string): number {
  let h = 0;
  for (let i = 0; i < name.length; i++) {
    h = (h * 31 + name.charCodeAt(i)) | 0;
  }
  return Math.abs(h);
}

/**
 * Synthesizes a Match (and its highlight plays) for an uploaded .dem. The demo
 * may belong to anyone, so there is no Steam user behind it — the rest of the
 * app treats it like any other match. seq keeps successive uploads distinct in
 * a single session; the file name picks a stable map.
 */
export function synthUploadedMatch(fileName: string, seq: number): { match: Match; plays: Play[] } {
  const id = `m-upload-${seq}`;
  const map = uploadMapPool[(hashName(fileName) + seq) % uploadMapPool.length];
  const losses = 6 + (seq % 7);
  const kills = 18 + (seq % 9);
  const deaths = 10 + (seq % 6);
  const playCount = 1 + (seq % 3);

  const plays: Play[] = uploadPlaySeeds.slice(0, playCount).map((seed, i) => ({
    id: `${id}-p${i + 1}`,
    matchId: id,
    thumbnailUrl: thumb(`${id}-p${i + 1}`),
    ...seed,
  }));

  const match: Match = {
    id,
    map,
    score: `13-${losses}`,
    playedAt: new Date().toISOString(),
    stats: {
      kills,
      deaths,
      assists: 3 + (seq % 4),
      mvps: 1 + (seq % 3),
      kd: Number((kills / deaths).toFixed(2)),
    },
    decentPlays: playCount,
    thumbnailUrl: thumb(id),
    source: 'upload',
  };

  return { match, plays };
}

/** Plausible CS2 handles a synthesized roster draws from (first 10 used). */
const rosterNamePool = [
  'kekO', 'RaiSeNN', 'granz', 'mcyans', 'Revol',
  's1xth', 'noctis', 'pylon', 'wraith', 'kovaq',
];

/**
 * Synthesizes a deterministic ~10-player roster for an uploaded .dem, seeded off
 * the file name so the same demo always yields the same scan. SteamIDs are stable
 * decimal-string ids; K/D/A are plausible and the list is sorted by kills desc so
 * the picker can auto-highlight the top fragger. parseDemo reuses these steamIds.
 */
export function synthRoster(fileName: string): DemoPlayer[] {
  const base = hashName(fileName);
  const rounds = 24;
  const players: DemoPlayer[] = rosterNamePool.map((name, i) => {
    const kills = 8 + ((base + i * 7) % 22);
    const deaths = 8 + ((base + i * 5) % 16);
    const assists = (base + i * 3) % 9;
    const headshots = Math.min(kills, Math.round(kills * (0.3 + ((base + i * 9) % 45) / 100)));
    const adr = 55 + ((base + i * 11) % 58);
    const kast = 55 + ((base + i * 13) % 35);
    const rating = Math.max(0.2, Math.round((0.55 + (kills - deaths) / rounds + (kills / rounds) * 0.35) * 100) / 100);
    return {
      steamId: String(76561190000000000n + BigInt(base % 1000000) + BigInt(i)),
      name,
      team: i % 2 === 0 ? 'CT' : 'T',
      kills,
      deaths,
      assists,
      headshots,
      mvps: Math.round(kills / 12),
      rounds,
      adr,
      hsPct: kills > 0 ? Math.round((1000 * headshots) / kills) / 10 : 0,
      kast,
      rating,
      // Most of the roster has no multi-kill rounds; the standout fragger
      // (index 0) gets an ace plus a couple of 3Ks so the Highlights column
      // and the Recommended badge are visible in dev without a real demo.
      rounds5k: i === 0 ? 1 : 0,
      rounds4k: 0,
      rounds3k: i === 0 ? 2 : i === 1 ? 1 : 0,
      rounds2k: i < 3 ? 1 : 0,
    };
  });
  return players.sort((a, b) => b.kills - a.kills || a.name.localeCompare(b.name));
}

/**
 * Synthesizes the roster scan's match header (map, final score, rounds) for an
 * uploaded .dem. Uses the raw de_-prefixed map name, matching the backend's
 * wire format, so the UI's map-prettifying logic runs the same as it would
 * against a real orchestrator response.
 */
export function synthRosterMatch(fileName: string): RosterMatch {
  const base = hashName(fileName);
  const rounds = 24;
  const scoreCt = 9 + (base % 7);
  return {
    map: rosterMapPool[base % rosterMapPool.length],
    scoreCt,
    scoreT: rounds - scoreCt,
    rounds,
  };
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
      downloadUrl: SAMPLE_REEL_URL,
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
    videoUrl: SAMPLE_REEL_URL,
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
    videoUrl: SAMPLE_REEL_URL,
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
    videoUrl: SAMPLE_REEL_URL,
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
    videoUrl: SAMPLE_REEL_URL,
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
    videoUrl: SAMPLE_REEL_URL,
  },
];
