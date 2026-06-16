import type { ApiClient } from './client';
import type { Session, Match, Play, Song, Video, FeedItem, RenderMode, DemoPlayer } from './types';
import { MockApiClient } from './mock';
import { planToMatch, planToPlays, type KillPlan } from './map';

/** Server roster row as returned by /api/demos/{jobId}/roster (steamid64). */
type RosterPlayer = {
  steamid64: string;
  name: string;
  team: 'CT' | 'T' | '';
  kills: number;
  deaths: number;
  assists: number;
};

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** The only registered vertical-reel preset/variant today (1080x1920 viral short). */
const REEL_VARIANT = 'viral-60-clean';

/**
 * Job statuses at or past which the kill plan exists and stays available. Once a
 * job is parsed it keeps its plan through recording/render, so the match detail
 * must not 404 just because the user moved the job forward by creating a reel.
 */
const PLAN_READY = new Set(['parsed', 'recording', 'recorded', 'composing', 'composed', 'done']);

function isJobId(id: string): boolean {
  return UUID_RE.test(id);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function readJson<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    const message = body && typeof body.error === 'string' ? body.error : `request failed (${res.status})`;
    throw new Error(message);
  }
  return (await res.json()) as T;
}

/**
 * RealApiClient talks to the same-origin Next route handlers under
 * /api/demos/*, which proxy the local orchestrator. Only the upload-real path
 * (scan → pick → parse) is implemented here; everything else (steam, library,
 * feed, render) delegates to a MockApiClient so the rest of the app keeps
 * working in this slice. Selected by index.ts when NEXT_PUBLIC_API_BASE is set.
 */
export class RealApiClient implements ApiClient {
  private readonly fallback = new MockApiClient();
  /**
   * Reels (vertical shorts) rendered for uploaded jobs, keyed by
   * `${jobId}__${segmentId}`. createVideo registers one and drives the
   * record→render pipeline in the background, mutating the entry's status as it
   * advances so listVideos/getVideo report live progress (queued→recording→
   * composing→ready). In-memory for the session, like the mock's videos.
   */
  private readonly reels = new Map<string, Video>();

  async scanDemo(file: File): Promise<{ jobId: string; players: DemoPlayer[] }> {
    const form = new FormData();
    form.append('file', file);
    const { jobId } = await readJson<{ jobId: string }>(
      await fetch('/api/demos/scan', { method: 'POST', body: form }),
    );

    await this.waitForStatus(jobId, 'scanned');

    const { players } = await readJson<{ players: RosterPlayer[] }>(
      await fetch(`/api/demos/${jobId}/roster`),
    );
    return { jobId, players: players.map(toDemoPlayer) };
  }

  async parseDemo(input: { jobId: string; steamId: string }): Promise<Match> {
    await readJson<unknown>(
      await fetch(`/api/demos/${input.jobId}/parse`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ steamId: input.steamId }),
      }),
    );

    await this.waitForStatus(input.jobId, 'parsed');

    const [plan, roster] = await Promise.all([
      readJson<KillPlan>(await fetch(`/api/demos/${input.jobId}/plan`)),
      readJson<{ players: RosterPlayer[] }>(await fetch(`/api/demos/${input.jobId}/roster`)),
    ]);

    const picked = roster.players.find((p) => p.steamid64 === input.steamId);
    if (!picked) throw new Error('chosen player not found in roster');
    return planToMatch(input.jobId, plan, toDemoPlayer(picked));
  }

  async getMatch(id: string): Promise<Match | null> {
    if (!isJobId(id)) return this.fallback.getMatch(id);

    const status = await this.fetchStatus(id);
    if (status === null) return null;
    // The plan exists once parsing finishes and stays through record/render.
    if (!PLAN_READY.has(status)) return null;

    const plan = await readJson<KillPlan>(await fetch(`/api/demos/${id}/plan`));
    return planToMatch(id, plan, await this.summaryPlayer(id, plan));
  }

  /**
   * Picks the player whose stats head the match summary. Prefers the roster row
   * for the plan's target (else the top fragger); if the job has no roster
   * (e.g. a job parsed via a directly-supplied target), falls back to the plan's
   * own target with kills from stats. Roster fetch is best-effort, never fatal.
   */
  private async summaryPlayer(jobId: string, plan: KillPlan): Promise<DemoPlayer> {
    try {
      const { players } = await readJson<{ players: RosterPlayer[] }>(await fetch(`/api/demos/${jobId}/roster`));
      const row = players.find((p) => p.steamid64 === plan.target?.steamid64) ?? players[0];
      if (row) return toDemoPlayer(row);
    } catch {
      // No roster for this job; fall back to the plan's target below.
    }
    return {
      steamId: plan.target?.steamid64 ?? '',
      name: plan.target?.name_in_demo ?? '',
      team: normalizeTeam(plan.target?.team_at_start ?? ''),
      kills: plan.stats?.total_kills_target ?? 0,
      deaths: 0,
      assists: 0,
    };
  }

  async findClips(matchId: string): Promise<Play[]> {
    if (!isJobId(matchId)) return this.fallback.findClips(matchId);

    const status = await this.fetchStatus(matchId);
    // No plan until parsing finishes; it persists through record/render.
    if (status === null || !PLAN_READY.has(status)) return [];

    const plan = await readJson<KillPlan>(await fetch(`/api/demos/${matchId}/plan`));
    return planToPlays(matchId, plan);
  }

  /** Polls /status until it reaches `want`; throws on `failed` or timeout. */
  private async waitForStatus(jobId: string, want: string, maxAttempts = 240): Promise<void> {
    for (let attempt = 0; attempt < maxAttempts; attempt++) {
      const status = await this.fetchStatus(jobId);
      if (status === want) return;
      if (status === 'failed') throw new Error(`job ${jobId} failed`);
      await sleep(800);
    }
    throw new Error(`timed out waiting for ${want}`);
  }

  /** Polls a render variant's state until 'ready'; throws on 'failed' or timeout. */
  private async waitForRender(jobId: string, variant: string, maxAttempts = 600): Promise<void> {
    for (let attempt = 0; attempt < maxAttempts; attempt++) {
      const res = await fetch(`/api/demos/${jobId}/renders/${variant}`);
      const { status } = await readJson<{ status: string }>(res);
      if (status === 'ready') return;
      if (status === 'failed') throw new Error(`render ${variant} for ${jobId} failed`);
      await sleep(1000);
    }
    throw new Error(`timed out waiting for render ${variant}`);
  }

  /**
   * Drives record→render for one chosen play in the background and updates the
   * reel entry's status as it advances. Fire-and-forget from createVideo.
   */
  private async orchestrateReel(videoId: string, jobId: string, segmentId: string, mode: RenderMode, songId?: string): Promise<void> {
    const patch = (next: Partial<Video>) => {
      const cur = this.reels.get(videoId);
      if (cur) this.reels.set(videoId, { ...cur, ...next });
    };
    try {
      // 1. Record the segments on the local rig (HLAE/CS2). Recording a full
      //    match can take several minutes, so allow a generous window.
      patch({ status: 'recording' });
      await readJson<unknown>(await fetch(`/api/demos/${jobId}/record`, { method: 'POST' }));
      await this.waitForStatus(jobId, 'recorded', 900);

      // 2. Render the vertical reel (zv-editor viral short). Music Edit mixes the
      //    chosen track in; Clean POV renders without music.
      patch({ status: 'composing' });
      const renderInit: RequestInit = { method: 'POST' };
      if (mode === 'music' && songId) {
        renderInit.headers = { 'Content-Type': 'application/json' };
        renderInit.body = JSON.stringify({ music: songId });
      }
      await readJson<unknown>(await fetch(`/api/demos/${jobId}/renders/${REEL_VARIANT}`, renderInit));
      await this.waitForRender(jobId, REEL_VARIANT);

      // 3. Ready: point playback/download at the proxied reel mp4 + cover.
      patch({
        status: 'ready',
        downloadUrl: `/api/demos/${jobId}/renders/${REEL_VARIANT}/videos/${segmentId}`,
        thumbnailUrl: `/api/demos/${jobId}/renders/${REEL_VARIANT}/covers/${segmentId}`,
      });
    } catch {
      patch({ status: 'failed' });
    }
  }

  /** Reads the job status; null when the job is unknown (404). */
  private async fetchStatus(jobId: string): Promise<string | null> {
    const res = await fetch(`/api/demos/${jobId}/status`);
    if (res.status === 404) return null;
    const { status } = await readJson<{ status: string }>(res);
    return status;
  }

  // --- everything below delegates to the mock fallback (out of scope here) ---

  getSession(): Promise<Session> {
    return this.fallback.getSession();
  }
  signInWithSteam(): Promise<Session> {
    return this.fallback.signInWithSteam();
  }
  signOut(): Promise<void> {
    return this.fallback.signOut();
  }
  linkMatchHistory(input: { authCode: string; knownCode: string }): Promise<{ ok: boolean; matchesFound: number }> {
    return this.fallback.linkMatchHistory(input);
  }
  pairPc(): Promise<{ pairingCode: string }> {
    return this.fallback.pairPc();
  }
  getPcStatus(): Promise<{ paired: boolean }> {
    return this.fallback.getPcStatus();
  }
  listMatches(): Promise<Match[]> {
    return this.fallback.listMatches();
  }
  /** @deprecated Superseded by scanDemo + parseDemo. */
  uploadDemo(input: { fileName: string }): Promise<Match> {
    return this.fallback.uploadDemo(input);
  }
  listSongs(): Promise<Song[]> {
    return this.fallback.listSongs();
  }
  /**
   * For an uploaded job (matchId = job UUID, playId = segment id), registers a
   * reel and drives record→render in the background; returns immediately with a
   * queued Video. Mock matches delegate to the fallback.
   */
  async createVideo(input: { matchId: string; playId: string; mode: RenderMode; songId?: string }): Promise<Video> {
    if (!isJobId(input.matchId)) return this.fallback.createVideo(input);

    const videoId = `${input.matchId}__${input.playId}`;
    const existing = this.reels.get(videoId);
    if (existing && existing.status !== 'failed') return { ...existing };

    const [plays, match] = await Promise.all([this.findClips(input.matchId), this.getMatch(input.matchId)]);
    const play = plays.find((p) => p.id === input.playId);
    const modeLabel = input.mode === 'music' ? 'Music Edit' : 'Clean POV';
    const video: Video = {
      id: videoId,
      title: `${play?.label ?? 'Highlight'} - ${modeLabel}`,
      map: match?.map ?? 'Unknown',
      score: match?.score ?? '',
      mode: input.mode,
      songId: input.songId,
      status: 'queued',
      createdAt: Date.now(),
      availableForSec: 14 * 3600,
      published: false,
    };
    this.reels.set(videoId, video);
    void this.orchestrateReel(videoId, input.matchId, input.playId, input.mode, input.songId);
    return { ...video };
  }

  async listVideos(): Promise<Video[]> {
    const reels = Array.from(this.reels.values())
      .sort((a, b) => b.createdAt - a.createdAt)
      .map((v) => ({ ...v }));
    const seeds = await this.fallback.listVideos();
    return [...reels, ...seeds];
  }

  async getVideo(id: string): Promise<Video | null> {
    const reel = this.reels.get(id);
    if (reel) return { ...reel };
    return this.fallback.getVideo(id);
  }

  async publishVideo(id: string): Promise<Video> {
    const reel = this.reels.get(id);
    if (!reel) return this.fallback.publishVideo(id);
    const updated: Video = { ...reel, published: true };
    this.reels.set(id, updated);
    return { ...updated };
  }
  listFeed(): Promise<FeedItem[]> {
    return this.fallback.listFeed();
  }
}

/** Server roster row (steamid64) → the UI's DemoPlayer (steamId). */
function toDemoPlayer(p: RosterPlayer): DemoPlayer {
  return {
    steamId: p.steamid64,
    name: p.name,
    team: normalizeTeam(p.team),
    kills: p.kills,
    deaths: p.deaths,
    assists: p.assists,
  };
}

/** Keeps only the known sides; anything else collapses to '' (spectator/unknown). */
function normalizeTeam(team: string): DemoPlayer['team'] {
  return team === 'CT' || team === 'T' ? team : '';
}
