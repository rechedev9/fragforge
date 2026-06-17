import type { ApiClient } from './client';
import type { Session, Match, Play, Song, Video, FeedItem, RenderMode, DemoPlayer } from './types';
import { MockApiClient } from './mock';
import { planToMatch, planToPlays, type KillPlan } from './map';
import { deriveReelView, type ReelAction, type ReelView, type RenderStatus } from './reel-reconcile';
import { loadReelIntents, saveReelIntents, type ReelIntent } from './reel-store';

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

/** A queued placeholder Video for an intent; its live status is filled by reconcile. */
function videoFromIntent(intent: ReelIntent): Video {
  return {
    id: intent.videoId,
    title: intent.title,
    map: intent.map,
    score: intent.score,
    mode: intent.mode,
    songId: intent.songId,
    status: 'queued',
    createdAt: intent.createdAt,
    availableForSec: 14 * 3600,
    published: intent.published,
  };
}

/**
 * RealApiClient talks to the same-origin Next route handlers under /api/demos/*,
 * which proxy the local orchestrator. The orchestrator is the source of truth: the
 * client persists only lightweight reel INTENTS (reel-store) and derives each reel's
 * live status by reconciling against the orchestrator on every poll (reel-reconcile),
 * driving record→render idempotently. A hard reload re-reads server state and
 * resumes exactly where it left off. Everything outside the upload→reel path
 * (steam, library seeds, feed) delegates to a MockApiClient. Selected by index.ts
 * when NEXT_PUBLIC_API_BASE is set.
 */
export class RealApiClient implements ApiClient {
  private readonly fallback = new MockApiClient();
  /** Live, derived view of each tracked reel (status/downloadUrl/failureReason). */
  private readonly reels = new Map<string, Video>();
  /** Durable facts the user asked for, mirrored to localStorage via reel-store. */
  private readonly intents = new Map<string, ReelIntent>();
  /** Reels with a record/render POST in flight, so a tick never double-drives. */
  private readonly driving = new Set<string>();

  constructor() {
    // Rehydrate the reels the user asked for so the Library survives a hard reload
    // or a direct visit; their live status is filled on the first reconcile tick.
    for (const intent of loadReelIntents()) {
      this.intents.set(intent.videoId, intent);
      this.reels.set(intent.videoId, videoFromIntent(intent));
    }
  }

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

  /**
   * For an uploaded job (matchId = job UUID, playId = segment id), registers a
   * durable reel intent and returns immediately with a queued Video. The reconcile
   * loop (driven by listVideos polling) advances it record→render; this is safe
   * across reloads because every step is derived from the orchestrator's state.
   * Mock matches delegate to the fallback.
   */
  async createVideo(input: { matchId: string; playId: string; mode: RenderMode; songId?: string }): Promise<Video> {
    if (!isJobId(input.matchId)) return this.fallback.createVideo(input);

    const videoId = `${input.matchId}__${input.playId}`;
    const existing = this.reels.get(videoId);
    if (existing && existing.status !== 'failed') return { ...existing };

    const [plays, match] = await Promise.all([this.findClips(input.matchId), this.getMatch(input.matchId)]);
    const play = plays.find((p) => p.id === input.playId);
    const modeLabel = input.mode === 'music' ? 'Music Edit' : 'Clean POV';
    const intent: ReelIntent = {
      videoId,
      jobId: input.matchId,
      segmentId: input.playId,
      mode: input.mode,
      songId: input.songId,
      title: `${play?.label ?? 'Highlight'} - ${modeLabel}`,
      map: match?.map ?? 'Unknown',
      score: match?.score ?? '',
      createdAt: Date.now(),
      published: false,
    };
    this.intents.set(videoId, intent);
    saveReelIntents(Array.from(this.intents.values()));
    this.reels.set(videoId, videoFromIntent(intent));
    void this.reconcile(); // kick now (idempotent); /videos polling continues it.
    return { ...videoFromIntent(intent) };
  }

  async listVideos(): Promise<Video[]> {
    await this.reconcile();
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
    const intent = this.intents.get(id);
    if (intent) {
      this.intents.set(id, { ...intent, published: true });
      saveReelIntents(Array.from(this.intents.values()));
    }
    return { ...updated };
  }

  /**
   * Re-drives a failed reel from where it failed. A failed job re-records (the
   * orchestrator allows record from failed once a kill plan exists); a recorded
   * job whose render failed re-renders. Clearing the local 'failed' status lets
   * the reconcile loop pick the reel back up and carry it to ready.
   */
  async retryVideo(id: string): Promise<Video> {
    const intent = this.intents.get(id);
    if (!intent) return this.fallback.retryVideo(id);

    this.applyView(intent, { status: 'queued', action: 'none' });
    const [job, render] = await Promise.all([
      this.fetchStatusFull(intent.jobId),
      this.fetchRenderStatus(intent.jobId),
    ]);
    if (job && job.status === 'failed') {
      await this.drive(intent, 'record');
    } else if (render.status === 'failed') {
      await this.drive(intent, 'render');
    }
    await this.reconcile();
    return { ...(this.reels.get(id) ?? videoFromIntent(intent)) };
  }

  /**
   * Reconciles every non-terminal tracked reel against the orchestrator and drives
   * its next step. Idempotent and resumable: it reads server truth each tick, so a
   * reload simply reattaches. One reel's failure never breaks the batch.
   */
  private async reconcile(): Promise<void> {
    const active = Array.from(this.intents.values()).filter((intent) => {
      const v = this.reels.get(intent.videoId);
      return !v || (v.status !== 'ready' && v.status !== 'failed');
    });
    await Promise.all(active.map((intent) => this.reconcileOne(intent).catch(() => {})));
  }

  private async reconcileOne(intent: ReelIntent): Promise<void> {
    const job = await this.fetchStatusFull(intent.jobId);
    if (job === null) {
      // Memory-mode orchestrator restart drops jobs; surface it instead of spinning.
      this.applyView(intent, {
        status: 'failed',
        action: 'none',
        failureReason: 'job no longer available (the local orchestrator may have restarted)',
      });
      return;
    }
    const render = await this.fetchRenderStatus(intent.jobId);
    const view = deriveReelView({
      jobStatus: job.status,
      jobFailureReason: job.failureReason,
      renderStatus: render.status,
      renderFailureReason: render.failureReason,
    });
    this.applyView(intent, view);
    if (view.action !== 'none') void this.drive(intent, view.action);
  }

  /** Writes a reel's derived view onto its live Video, wiring URLs once ready. */
  private applyView(intent: ReelIntent, view: ReelView): void {
    const base = this.reels.get(intent.videoId) ?? videoFromIntent(intent);
    const next: Video = { ...base, status: view.status, failureReason: view.failureReason };
    if (view.status === 'ready') {
      next.downloadUrl = `/api/demos/${intent.jobId}/renders/${REEL_VARIANT}/videos/${intent.segmentId}`;
      next.thumbnailUrl = `/api/demos/${intent.jobId}/renders/${REEL_VARIANT}/covers/${intent.segmentId}`;
    }
    this.reels.set(intent.videoId, next);
  }

  /** Issues the single pipeline POST for `action`, guarded so it fires at most once. */
  private async drive(intent: ReelIntent, action: ReelAction): Promise<void> {
    if (this.driving.has(intent.videoId)) return;
    this.driving.add(intent.videoId);
    try {
      if (action === 'record') {
        await fetch(`/api/demos/${intent.jobId}/record`, { method: 'POST' });
      } else if (action === 'render') {
        const init: RequestInit = { method: 'POST' };
        if (intent.mode === 'music' && intent.songId) {
          init.headers = { 'Content-Type': 'application/json' };
          init.body = JSON.stringify({ music: intent.songId });
        }
        await fetch(`/api/demos/${intent.jobId}/renders/${REEL_VARIANT}`, init);
      }
    } catch {
      // network blip; the next reconcile tick re-evaluates from server truth.
    } finally {
      this.driving.delete(intent.videoId);
    }
  }

  /** Reads job status + failure reason; null when the job is unknown (404). */
  private async fetchStatusFull(jobId: string): Promise<{ status: string; failureReason?: string } | null> {
    const res = await fetch(`/api/demos/${jobId}/status`);
    if (res.status === 404) return null;
    const data = await readJson<{ status: string; failure_reason?: string }>(res);
    return { status: data.status, failureReason: data.failure_reason };
  }

  /** Reads the job status string; null when the job is unknown (404). */
  private async fetchStatus(jobId: string): Promise<string | null> {
    const full = await this.fetchStatusFull(jobId);
    return full ? full.status : null;
  }

  /** Reads the reel render-variant state; 'none' when the render has not started. */
  private async fetchRenderStatus(jobId: string): Promise<{ status: RenderStatus; failureReason?: string }> {
    const res = await fetch(`/api/demos/${jobId}/renders/${REEL_VARIANT}`);
    if (!res.ok) return { status: 'none' }; // 404 = render not started yet
    const data = (await res.json()) as { status?: string; failure_reason?: string };
    const known = new Set<RenderStatus>(['queued', 'rendering', 'ready', 'failed']);
    const status: RenderStatus = data.status && known.has(data.status as RenderStatus) ? (data.status as RenderStatus) : 'none';
    return { status, failureReason: data.failure_reason };
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
