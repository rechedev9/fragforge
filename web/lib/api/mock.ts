import type { ApiClient } from './client';
import type { Session, Match, Play, Song, Video, FeedItem, RenderMode, VideoStatus, DemoPlayer, Preset, EditConfig, CaptureReadiness, RosterMatch, SeriesDemo } from './types';
import { DEFAULT_EDIT_CONFIG } from './reel-store';
import {
  PUBLISH_ASSISTANT_SCHEMA_VERSION,
  PUBLISH_ASSISTANT_TIME_ZONE,
  YOUTUBE_STUDIO_URL,
  type PublishAssistant,
  type PublishRecommendation,
} from './publish-assistant';
import { playsSelectionLabel } from '@/lib/format';
import {
  fixtureUser,
  fixtureSlots,
  fixtureMatches,
  fixtureSongs,
  fixtureFeed,
  playsForMatch,
  seedVideos,
  synthUploadedMatch,
  synthRoster,
  synthRosterMatch,
  SAMPLE_REEL_URL,
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

function mockPublishAssistant(video: Video): PublishAssistant {
  const now = Date.now();
  const dayFormatter = new Intl.DateTimeFormat('en-CA', {
    timeZone: PUBLISH_ASSISTANT_TIME_ZONE,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  });
  const weekdayFormatter = new Intl.DateTimeFormat('es-ES', {
    timeZone: PUBLISH_ASSISTANT_TIME_ZONE,
    weekday: 'long',
  });
  const timeFormatter = new Intl.DateTimeFormat('es-ES', {
    timeZone: PUBLISH_ASSISTANT_TIME_ZONE,
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
  function dateKey(date: Date): string {
    const parts = dayFormatter.formatToParts(date);
    const year = parts.find((part) => part.type === 'year')?.value;
    const month = parts.find((part) => part.type === 'month')?.value;
    const day = parts.find((part) => part.type === 'day')?.value;
    return year && month && day ? `${year}-${month}-${day}` : date.toISOString().slice(0, 10);
  }
  const days = Array.from({ length: 7 }, (_, index) => {
    const best = new Date(now + (index * 24 + 2) * 60 * 60 * 1000);
    return {
      date: dateKey(best),
      weekday: weekdayFormatter.format(best),
      slots: [
        {
          publishAt: best.toISOString(),
          localTime: timeFormatter.format(best),
          source: 'baseline' as const,
          confidence: 0.62,
          score: 0.7,
          rationale: 'Franja de referencia para Shorts en horario local de Madrid.',
        },
      ],
    };
  });
  const recommendations: PublishRecommendation[] = [
    {
      title: `${video.title} | CS2 Shorts`,
      description: `${video.title} en ${video.map}, con el POV completo.\n\n#CS2 #CounterStrike2 #Shorts`,
      keywords: [video.title, video.map, 'CS2 Shorts'],
      tags: ['CS2', 'Counter-Strike 2', video.map, 'Shorts'],
      score: 91,
      rationale: 'Usa únicamente el título del reel y el mapa disponibles en el render.',
    },
    {
      title: `${video.map}: ${video.title} en CS2`,
      description: `La jugada ${video.title} en ${video.map}, editada como Short vertical.\n\n#CS2 #Shorts`,
      keywords: [video.map, video.title, 'CS2'],
      tags: ['CS2', video.map, 'Shorts'],
      score: 86,
      rationale: 'Prioriza el mapa y la jugada sin añadir resultados que no constan en el reel.',
    },
    {
      title: `POV completo en ${video.map} | ${video.title}`,
      description: `POV de CS2 en ${video.map}: ${video.title}.\n\n#CS2 #CounterStrike2`,
      keywords: ['CS2 POV', video.map, video.title],
      tags: ['CS2', 'CS2 POV', video.map],
      score: 82,
      rationale: 'Describe el formato POV y repite solo datos presentes en la ficha del vídeo.',
    },
  ];
  return {
    schemaVersion: PUBLISH_ASSISTANT_SCHEMA_VERSION,
    metadata: {
      title: video.title,
      description: `${video.title} en ${video.map}.\n\n#CS2 #CounterStrike2 #Shorts`,
      tags: ['CS2', 'Counter-Strike 2', video.map, 'Shorts'],
    },
    recommendations,
    keywords: [...recommendations[0].keywords],
    tags: [...recommendations[0].tags],
    schedule: {
      timeZone: PUBLISH_ASSISTANT_TIME_ZONE,
      generatedAt: new Date(now).toISOString(),
      days,
      sources: [
        { title: 'YouTube: subir vídeos', url: 'https://support.google.com/youtube/answer/57407?hl=es' },
      ],
      caveat: 'No existe una hora mágica: usa estas franjas como punto de partida y reajusta con tus resultados.',
    },
    trends: {
      available: false,
      terms: [],
      sources: [],
      reason: 'Firecrawl no está configurado en el modo de demostración.',
    },
    studioUrl: YOUTUBE_STUDIO_URL,
  };
}

/** Set by pairPc so the next getPcStatus reports the PC as paired. */
let pcPaired = false;

/**
 * Uploaded demos, parsed on the fly. They are not Steam matches (the demo may
 * belong to anyone) so they live apart from fixtureMatches, but getMatch /
 * findClips / createVideo resolve them too, letting an upload reuse the same
 * highlight → render pipeline.
 */
const uploadedMatches: Match[] = [];
const uploadedPlays = new Map<string, Play[]>();
let uploadSeq = 0;

/**
 * Scans awaiting a target pick: scanDemo mints a jobId and stashes the file name
 * + roster so parseDemo can resolve it. In-memory only (a scan that is never
 * parsed costs nothing); not persisted, since the picker resolves it in one go.
 */
type PendingScan = { fileName: string; seq: number; players: DemoPlayer[]; match: RosterMatch };
const pendingScans = new Map<string, PendingScan>();

/**
 * Demos recorded per bulk series id by scanDemo, in upload order, so getSeries
 * can synthesize a plausible series without a real orchestrator. In-memory only,
 * like pendingScans: a series that is never listed costs nothing.
 */
type SeriesScan = { jobId: string; fileName: string; match: RosterMatch };
const seriesScans = new Map<string, SeriesScan[]>();

/**
 * Uploaded demos persist to sessionStorage so the bookmarkable /matches/[id]
 * URL still resolves after a reload or a direct visit, matching the Steam path.
 * Guarded for SSR (no window) and tolerant of corrupt / over-quota storage.
 */
const UPLOAD_STORE_KEY = 'fragforge.uploads.v1';

function saveUploads(): void {
  if (typeof window === 'undefined') return;
  try {
    const store = { matches: uploadedMatches, plays: Object.fromEntries(uploadedPlays), seq: uploadSeq };
    window.sessionStorage.setItem(UPLOAD_STORE_KEY, JSON.stringify(store));
  } catch {
    // sessionStorage can throw (quota / privacy mode); in-memory state still works this session.
  }
}

function loadUploads(): void {
  if (typeof window === 'undefined') return;
  try {
    const raw = window.sessionStorage.getItem(UPLOAD_STORE_KEY);
    if (!raw) return;
    const store = JSON.parse(raw) as { matches: Match[]; plays: Record<string, Play[]>; seq: number };
    uploadedMatches.splice(0, uploadedMatches.length, ...store.matches);
    uploadedPlays.clear();
    for (const [matchId, plays] of Object.entries(store.plays)) {
      uploadedPlays.set(matchId, plays);
    }
    uploadSeq = store.seq;
  } catch {
    // ignore corrupt storage; uploaded demos are best-effort in the mock.
  }
}

loadUploads();

/**
 * The auth session persists to sessionStorage too, so a page reload keeps the
 * user signed in (and slots/pairing state) instead of silently logging out.
 * Same SSR/quota guards as the upload store.
 */
const SESSION_STORE_KEY = 'fragforge.session.v1';

function saveSession(): void {
  if (typeof window === 'undefined') return;
  try {
    window.sessionStorage.setItem(SESSION_STORE_KEY, JSON.stringify(session));
  } catch {
    // sessionStorage can throw (quota / privacy mode); in-memory state still works.
  }
}

function loadSession(): void {
  if (typeof window === 'undefined') return;
  try {
    const raw = window.sessionStorage.getItem(SESSION_STORE_KEY);
    if (!raw) return;
    const stored = JSON.parse(raw) as Session;
    session.user = stored.user;
    session.slots = stored.slots ?? session.slots;
    session.pcPaired = stored.pcPaired ?? false;
    session.matchHistoryLinked = stored.matchHistoryLinked ?? false;
    pcPaired = stored.pcPaired ?? false;
  } catch {
    // ignore corrupt storage; fall back to the signed-out default.
  }
}

loadSession();

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
    next.downloadUrl = SAMPLE_REEL_URL;
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
    saveSession();
    return cloneSession();
  }

  async signOut(): Promise<void> {
    await delay();
    session.user = null;
    session.matchHistoryLinked = false;
    session.pcPaired = false;
    pcPaired = false;
    saveSession();
  }

  async linkMatchHistory(_input: { authCode: string; knownCode: string }): Promise<{ ok: boolean; matchesFound: number }> {
    await delay();
    session.matchHistoryLinked = true;
    saveSession();
    return { ok: true, matchesFound: fixtureMatches.length };
  }

  async pairPc(): Promise<{ pairingCode: string }> {
    await delay();
    pcPaired = true;
    session.pcPaired = true;
    saveSession();
    const code = `CS2V-${randomCode()}`;
    return { pairingCode: code };
  }

  async getPcStatus(): Promise<{ paired: boolean }> {
    await delay();
    session.pcPaired = pcPaired;
    saveSession();
    return { paired: pcPaired };
  }

  // Offline/dev mode has no real orchestrator to probe, so capture reads ready.
  async getCaptureReadiness(): Promise<CaptureReadiness> {
    await delay();
    return { recordEnabled: true, status: 'ready', tools: [] };
  }

  async listMatches(): Promise<Match[]> {
    await delay();
    return fixtureMatches.map((m) => ({ ...m, stats: { ...m.stats } }));
  }

  async getMatch(id: string): Promise<Match | null> {
    await delay();
    const match = uploadedMatches.find((m) => m.id === id) ?? fixtureMatches.find((m) => m.id === id);
    return match ? { ...match, stats: { ...match.stats } } : null;
  }

  /** @deprecated Superseded by scanDemo + parseDemo. */
  async uploadDemo(input: { fileName: string }): Promise<Match> {
    await delay();
    uploadSeq += 1;
    const { match, plays } = synthUploadedMatch(input.fileName, uploadSeq);
    uploadedMatches.unshift(match);
    uploadedPlays.set(match.id, plays);
    saveUploads();
    return { ...match, stats: { ...match.stats } };
  }

  async scanDemo(file: File, opts?: { seriesId?: string }): Promise<{ jobId: string; players: DemoPlayer[]; match: RosterMatch }> {
    await delay();
    uploadSeq += 1;
    const jobId = `m-upload-${uploadSeq}`;
    const players = synthRoster(file.name);
    const match = synthRosterMatch(file.name);
    pendingScans.set(jobId, { fileName: file.name, seq: uploadSeq, players, match });
    if (opts?.seriesId) {
      const list = seriesScans.get(opts.seriesId) ?? [];
      list.push({ jobId, fileName: file.name, match });
      seriesScans.set(opts.seriesId, list);
    }
    return { jobId, players: players.map((p) => ({ ...p })), match: { ...match } };
  }

  async getSeries(seriesId: string): Promise<SeriesDemo[]> {
    await delay();
    const list = seriesScans.get(seriesId) ?? [];
    // A scanned demo has a roster, so the synthetic series reports it as scanned
    // and carries the match; failed/unscanned demos never enter the mock series.
    return list.map((s) => ({ jobId: s.jobId, fileName: s.fileName, status: 'scanned', match: { ...s.match } }));
  }

  async parseDemo(input: { jobId: string; steamId: string }): Promise<Match> {
    await delay();
    const pending = pendingScans.get(input.jobId);
    if (!pending) throw new Error(`no scan to parse: ${input.jobId}`);
    const player = pending.players.find((p) => p.steamId === input.steamId);
    if (!player) throw new Error(`player not in roster: ${input.steamId}`);

    const { match, plays } = synthUploadedMatch(pending.fileName, pending.seq);
    // The synthesized match uses the chosen player's real roster K/D/A so the
    // picked target's stats carry through to /matches/[id].
    const picked: Match = {
      ...match,
      id: input.jobId,
      stats: {
        ...match.stats,
        kills: player.kills,
        deaths: player.deaths,
        assists: player.assists,
        kd: player.deaths ? Number((player.kills / player.deaths).toFixed(2)) : player.kills,
      },
    };
    const pickedPlays = plays.map((p) => ({ ...p, matchId: input.jobId }));

    uploadedMatches.unshift(picked);
    uploadedPlays.set(picked.id, pickedPlays);
    pendingScans.delete(input.jobId);
    saveUploads();
    return { ...picked, stats: { ...picked.stats } };
  }

  async findClips(matchId: string): Promise<Play[]> {
    await delay();
    const uploaded = uploadedPlays.get(matchId);
    if (uploaded) return uploaded.map((p) => ({ ...p }));
    return playsForMatch(matchId);
  }

  async listSongs(): Promise<Song[]> {
    await delay();
    return fixtureSongs.map((s) => ({ ...s }));
  }

  async listPresets(): Promise<Preset[]> {
    await delay();
    return [
      { name: 'viral-60-clean', label: 'Killfeed', description: 'POV sin HUD que conserva el killfeed del juego, con punch-ins y contador de kills', hudMode: 'deathnotices', default: true },
      { name: 'clean-pov-60', label: 'POV limpio', description: 'POV cinemático en primera persona, sin HUD ni killfeed del juego', hudMode: 'clean' },
      { name: 'full-hud-60', label: 'HUD completo', description: 'Conserva el HUD completo de CS2: vida, munición y radar visibles', hudMode: 'gameplay' },
    ];
  }

  async createVideo(input: { matchId: string; playIds: string[]; mode: RenderMode; songId?: string; variant?: string; editConfig?: EditConfig }): Promise<Video> {
    await delay();
    const match = uploadedMatches.find((m) => m.id === input.matchId) ?? fixtureMatches.find((m) => m.id === input.matchId);
    const plays = uploadedPlays.get(input.matchId) ?? playsForMatch(input.matchId);
    // Preserve the caller's (plan) order rather than the plays array's order.
    const pickedPlays = input.playIds.map((pid) => plays.find((p) => p.id === pid)).filter((p): p is Play => Boolean(p));

    const modeLabel = input.mode === 'music' ? 'Edición Musical' : 'Clean POV';
    const playLabel = playsSelectionLabel(pickedPlays) ?? 'Highlight';
    const id = `v-${Date.now()}`;

    const video: Video = {
      id,
      title: `${playLabel} - ${modeLabel}`,
      map: match?.map ?? 'Unknown',
      score: match?.score ?? '',
      mode: input.mode,
      variant: input.variant,
      songId: input.songId,
      editConfig: input.editConfig ?? DEFAULT_EDIT_CONFIG,
      status: 'queued',
      createdAt: Date.now(),
      availableForSec: 14 * 3600,
      thumbnailUrl: `${THUMB_BASE}/${id}/640/360`,
    };

    videos.unshift(video);
    session.slots.used += 1;
    saveSession();
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

  async getPublishAssistant(id: string): Promise<PublishAssistant> {
    await delay();
    const video = videos.find((v) => v.id === id);
    if (!video) throw new Error(`video not found: ${id}`);
    return mockPublishAssistant(video);
  }

  async retryVideo(id: string): Promise<Video> {
    await delay();
    const video = videos.find((v) => v.id === id);
    if (!video) throw new Error(`video not found: ${id}`);
    // Restart the projected timeline so a failed mock reel re-renders to ready.
    video.status = 'queued';
    video.createdAt = Date.now();
    video.failureReason = undefined;
    return project(video);
  }

  async deleteVideo(id: string): Promise<void> {
    await delay();
    const index = videos.findIndex((v) => v.id === id);
    if (index !== -1) videos.splice(index, 1);
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
