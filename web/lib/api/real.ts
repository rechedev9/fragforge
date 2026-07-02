import type { ApiClient } from './client';
import type { Session, Match, Play, Song, Video, FeedItem, RenderMode, DemoPlayer, Preset, SteamUser, EditConfig, CaptureReadiness, CaptureTool, CaptureStatus, RosterMatch } from './types';
import { SERVICE_UNAVAILABLE_CODE } from './types';
import { MockApiClient } from './mock';
import { planToMatch, planToPlays, type KillPlan } from './map';
import { deriveReelView, type ReelAction, type ReelView, type RenderStatus } from './reel-reconcile';
import { loadReelIntents, saveReelIntents, DEFAULT_VARIANT, DEFAULT_EDIT_CONFIG, type ReelIntent } from './reel-store';
import { isLocalMode } from '@/lib/mode';

/** Server roster row as returned by /api/demos/{jobId}/roster (steamid64). */
type RosterPlayer = {
  steamid64: string;
  name: string;
  team: 'CT' | 'T' | '';
  kills: number;
  deaths: number;
  assists: number;
  headshots?: number;
  mvps?: number;
  rounds?: number;
  adr?: number;
  hs_pct?: number;
  kast?: number;
  rating?: number;
  rounds_2k?: number;
  rounds_3k?: number;
  rounds_4k?: number;
  rounds_5k?: number;
};

/** Server match summary as returned by /api/demos/{jobId}/roster (snake_case). */
type RosterMatchResponse = {
  map: string;
  score_ct: number;
  score_t: number;
  rounds: number;
};

/** The full roster response: players plus optional match-level context. */
type RosterResponse = {
  players: RosterPlayer[];
  match?: RosterMatchResponse;
};

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** Default vertical-reel preset/variant when an intent predates preset selection. */
const REEL_VARIANT = DEFAULT_VARIANT;

/** The render variant for a reel: the user's chosen preset, else the default. */
function variantOf(intent: Pick<ReelIntent, 'variant'>): string {
  return intent.variant ?? REEL_VARIANT;
}

/** Display-only labels for the known presets (server is the source of truth). */
const VARIANT_LABELS: Record<string, string> = {
  'viral-60-clean': 'Kill Feed',
  'clean-pov-60': 'Clean POV',
  'full-hud-60': 'Full HUD',
};

function variantLabel(variant: string): string {
  return VARIANT_LABELS[variant] ?? variant;
}

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
    // Carry the backend's stable `code` (e.g. SERVICE_UNAVAILABLE_CODE) onto the
    // thrown error so callers can branch deterministically instead of sniffing
    // the message string.
    const err = new Error(message) as Error & { code?: string };
    if (body && typeof body.code === 'string') err.code = body.code;
    throw err;
  }
  return (await res.json()) as T;
}

/**
 * The render-variant POST body shape for `edit`: the bool switches plus the
 * bookend text keys the orchestrator expects (snake_case, unlike the rest of
 * this camelCase struct - see internal/renderplan.EditRequest). Only sent when
 * its toggle is on and the trimmed text is non-empty; otherwise the backend
 * default applies (the generated headline for intro, "FragForge" for outro).
 */
type EditRequestBody = {
  format: EditConfig['format'];
  killEffect: EditConfig['killEffect'];
  transition: EditConfig['transition'];
  intro: boolean;
  outro: boolean;
  intro_text?: string;
  outro_text?: string;
};

function buildEditRequest(edit: EditConfig): EditRequestBody {
  const body: EditRequestBody = {
    format: edit.format,
    killEffect: edit.killEffect,
    transition: edit.transition,
    intro: edit.intro,
    outro: edit.outro,
  };
  const introText = edit.introText?.trim();
  if (edit.intro && introText) body.intro_text = introText;
  const outroText = edit.outroText?.trim();
  if (edit.outro && outroText) body.outro_text = outroText;
  return body;
}

/** A queued placeholder Video for an intent; its live status is filled by reconcile. */
function videoFromIntent(intent: ReelIntent): Video {
  return {
    id: intent.videoId,
    title: intent.title,
    map: intent.map,
    score: intent.score,
    mode: intent.mode,
    variant: intent.variant,
    songId: intent.songId,
    editConfig: intent.editConfig,
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

  async scanDemo(file: File): Promise<{ jobId: string; players: DemoPlayer[]; match?: RosterMatch }> {
    const form = new FormData();
    form.append('file', file);
    const { jobId } = await readJson<{ jobId: string }>(
      await fetch('/api/demos/scan', { method: 'POST', body: form }),
    );

    // Local studio scans synchronously on this machine (orchestrator status
    // reaches 'scanned'); cloud scans hand off to a paired agent and also watch
    // whether the user's PC is online (PC_OFFLINE).
    if (isLocalMode()) {
      await this.waitForStatus(jobId, 'scanned');
    } else {
      await this.waitForScan(jobId);
    }

    const roster = await readJson<RosterResponse>(await fetch(`/api/demos/${jobId}/roster`));
    return { jobId, players: roster.players.map(toDemoPlayer), match: toRosterMatch(roster.match) };
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
      readJson<RosterResponse>(await fetch(`/api/demos/${input.jobId}/roster`)),
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
      const { players } = await readJson<RosterResponse>(await fetch(`/api/demos/${jobId}/roster`));
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
      headshots: 0,
      mvps: 0,
      rounds: 0,
      adr: 0,
      hsPct: 0,
      kast: 0,
      rating: 0,
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
   * Polls /status until the cloud scan job reaches 'scanned', for the async
   * browser -> Supabase -> paired-agent scan flow. Unlike waitForStatus, this
   * also watches `online`: the status route reports whether the user's agent
   * heartbeat within the last minute, so a scan that never progresses because
   * the user's PC is off is reported distinctly (PC_OFFLINE) rather than as a
   * generic timeout, letting the UI show an actionable "open FragForge Agent"
   * message instead of a dead spinner.
   */
  private async waitForScan(jobId: string, maxAttempts = 200): Promise<void> {
    let consecutiveOffline = 0;
    for (let attempt = 0; attempt < maxAttempts; attempt++) {
      await sleep(1500);
      const data = await readJson<{ status: string; failure_reason?: string; online?: boolean }>(
        await fetch(`/api/demos/${jobId}/status`),
      );
      if (data.status === 'scanned') return;
      if (data.status === 'failed') throw new Error(data.failure_reason || 'scan failed');
      if (data.online === false) {
        consecutiveOffline++;
        if (consecutiveOffline > 4) throw new Error('PC_OFFLINE');
      } else {
        consecutiveOffline = 0;
      }
    }
    throw new Error('scan timed out');
  }

  /**
   * For an uploaded job (matchId = job UUID, playId = segment id), registers a
   * durable reel intent and returns immediately with a queued Video. The reconcile
   * loop (driven by listVideos polling) advances it record→render; this is safe
   * across reloads because every step is derived from the orchestrator's state.
   * Mock matches delegate to the fallback.
   */
  async createVideo(input: { matchId: string; playId: string; mode: RenderMode; songId?: string; variant?: string; editConfig?: EditConfig }): Promise<Video> {
    if (!isJobId(input.matchId)) return this.fallback.createVideo(input);

    const videoId = `${input.matchId}__${input.playId}`;
    const existing = this.reels.get(videoId);
    if (existing && existing.status !== 'failed') return { ...existing };

    const [plays, match] = await Promise.all([this.findClips(input.matchId), this.getMatch(input.matchId)]);
    const play = plays.find((p) => p.id === input.playId);
    const variant = input.variant ?? REEL_VARIANT;
    const suffix = input.songId ? `${variantLabel(variant)} + Music` : variantLabel(variant);
    const intent: ReelIntent = {
      videoId,
      jobId: input.matchId,
      segmentId: input.playId,
      mode: input.mode,
      variant,
      editConfig: input.editConfig ?? DEFAULT_EDIT_CONFIG,
      songId: input.songId,
      title: `${play?.label ?? 'Highlight'} - ${suffix}`,
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
    // Local studio shows only the user's own real reels (persisted on this PC).
    // Cloud/preview additionally shows demo seed videos for the design surface.
    if (isLocalMode()) return reels;
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
      this.fetchRenderStatus(intent.jobId, variantOf(intent)),
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
    const render = await this.fetchRenderStatus(intent.jobId, variantOf(intent));
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
      const variant = variantOf(intent);
      next.downloadUrl = `/api/demos/${intent.jobId}/renders/${variant}/videos/${intent.segmentId}`;
      next.thumbnailUrl = `/api/demos/${intent.jobId}/renders/${variant}/covers/${intent.segmentId}`;
    }
    this.reels.set(intent.videoId, next);
  }

  /** Issues the single pipeline POST for `action`, guarded so it fires at most once. */
  private async drive(intent: ReelIntent, action: ReelAction): Promise<void> {
    if (this.driving.has(intent.videoId)) return;
    this.driving.add(intent.videoId);
    const variant = variantOf(intent);
    try {
      const res =
        action === 'record'
          ? // The preset (Clean POV / Full HUD / Kill Feed) sets the recording HUD;
            // segment_ids scopes the capture to the one selected clip so the
            // recorder seeks to that kill instead of recording the whole demo.
            await fetch(`/api/demos/${intent.jobId}/record`, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ preset: variant, segment_ids: [intent.segmentId] }),
            })
          : await fetch(`/api/demos/${intent.jobId}/renders/${variant}`, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                music: intent.mode === 'music' ? intent.songId : undefined,
                edit: buildEditRequest(intent.editConfig),
              }),
            });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: string; code?: string };
        // A 503 means the orchestrator is momentarily unreachable; let the next
        // reconcile tick retry instead of permanently failing the reel.
        if (body.code === SERVICE_UNAVAILABLE_CODE) return;
        // Anything else is durable (e.g. the 409 "recording is not configured on
        // this machine; set ZV_RECORDER_PATH, ZV_HLAE_PATH and ZV_CS2_PATH and
        // restart the orchestrator"): surface it so the Library shows why the reel
        // stalled instead of spinning at QUEUED forever. The reconcile loop skips
        // failed reels, and Retry re-drives once capture is configured.
        this.applyView(intent, {
          status: 'failed',
          action: 'none',
          failureReason: body.error || (action === 'record' ? 'failed to start recording' : 'failed to start rendering'),
        });
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
  private async fetchRenderStatus(jobId: string, variant: string): Promise<{ status: RenderStatus; failureReason?: string }> {
    const res = await fetch(`/api/demos/${jobId}/renders/${variant}`);
    if (!res.ok) return { status: 'none' }; // 404 = render not started yet
    const data = (await res.json()) as { status?: string; failure_reason?: string };
    const known = new Set<RenderStatus>(['queued', 'rendering', 'ready', 'failed']);
    const status: RenderStatus = data.status && known.has(data.status as RenderStatus) ? (data.status as RenderStatus) : 'none';
    return { status, failureReason: data.failure_reason };
  }

  // --- everything below delegates to the mock fallback (out of scope here) ---

  /** Real Steam session from the signed cookie (/api/auth/session). */
  async getSession(): Promise<Session> {
    const signedOut: Session = { user: null, slots: { used: 0, total: 50 }, pcPaired: false, matchHistoryLinked: false };
    try {
      const res = await fetch('/api/auth/session', { cache: 'no-store' });
      const data = (await res.json()) as { user: SteamUser | null; matchHistoryLinked?: boolean };
      if (!data.user) return signedOut;
      return {
        user: data.user,
        slots: { used: 0, total: 50 },
        pcPaired: true, // BYO: the signed-in user's own machine hosts capture
        matchHistoryLinked: Boolean(data.matchHistoryLinked),
      };
    } catch {
      return signedOut;
    }
  }

  /** Real Steam OpenID: full-page redirect to Steam; the browser navigates away. */
  async signInWithSteam(): Promise<Session> {
    if (typeof window !== 'undefined') {
      window.location.assign('/api/auth/steam/login');
      await new Promise<never>(() => {}); // navigation in progress; never resolves
    }
    return this.getSession();
  }

  async signOut(): Promise<void> {
    try {
      await fetch('/api/auth/logout', { method: 'POST' });
    } catch {
      // best-effort; the cookie also expires on its own.
    }
  }

  /** Links real CS2 match history via the Steam Web API (needs STEAM_WEB_API_KEY). */
  async linkMatchHistory(input: { authCode: string; knownCode: string }): Promise<{ ok: boolean; matchesFound: number }> {
    const res = await fetch('/api/auth/match-history', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    });
    const data = (await res.json().catch(() => ({}))) as {
      ok?: boolean;
      matchesFound?: number;
      error?: string;
      code?: string;
    };
    if (!res.ok) {
      // Propagate the backend's stable `code` on the thrown error so the UI can
      // branch deterministically (no message string-sniffing). Additive only —
      // the ApiClient signature is unchanged.
      const err = new Error(data.error || 'failed to link match history') as Error & { code?: string };
      err.code = data.code;
      throw err;
    }
    return { ok: Boolean(data.ok), matchesFound: Number(data.matchesFound) || 0 };
  }

  /** Mints a one-time pairing code for the desktop agent (POST /api/pc/pair). */
  async pairPc(): Promise<{ pairingCode: string }> {
    // Cloud-only: pairing hands work to a remote agent via Supabase. Local studio
    // runs capture on this same machine, so there is nothing to pair.
    if (isLocalMode()) return { pairingCode: '' };
    return readJson<{ pairingCode: string }>(await fetch('/api/pc/pair', { method: 'POST' }));
  }

  /**
   * Reports whether the signed-in user has a redeemed agent (GET /api/pc/status).
   * Stays false right after pairPc until the desktop agent redeems the code and
   * heartbeats, since the pending pairing row is excluded server-side.
   */
  async getPcStatus(): Promise<{ paired: boolean }> {
    // Local studio is the machine itself; report paired without hitting the
    // cloud-only /api/pc/status route (which needs Supabase).
    if (isLocalMode()) return { paired: true };
    return readJson<{ paired: boolean }>(await fetch('/api/pc/status', { cache: 'no-store' }));
  }

  /**
   * Reads capture readiness from the local orchestrator (/api/capabilities): is
   * the record worker enabled and are HLAE/CS2/recorder reachable. A 503 maps to
   * 'offline' (orchestrator down) rather than 'unconfigured', so the UI can tell
   * "start your orchestrator" apart from "set your tool paths".
   */
  async getCaptureReadiness(): Promise<CaptureReadiness> {
    try {
      const res = await fetch('/api/capabilities', { cache: 'no-store' });
      if (!res.ok) {
        // Any non-ok here is a transport/backend problem (the orchestrator reports
        // "unconfigured" via a 200 with record.enabled=false), so treat it as
        // offline rather than blaming the user's tool paths.
        return { recordEnabled: false, status: 'offline', tools: [], reason: 'local analysis service offline' };
      }
      const data = (await res.json()) as { record?: { enabled?: boolean; tools?: CaptureTool[] } };
      const tools = data.record?.tools ?? [];
      const enabled = Boolean(data.record?.enabled);
      const anyMissing = tools.some((t) => t.configured && !t.accessible);
      const status: CaptureStatus = !enabled ? 'unconfigured' : anyMissing ? 'warning' : 'ready';
      return { recordEnabled: enabled, status, tools };
    } catch {
      return { recordEnabled: false, status: 'offline', tools: [] };
    }
  }
  listMatches(): Promise<Match[]> {
    // Local studio has no cloud match library; the mock seeds are a design-only
    // surface. A local match is opened by job id from the upload flow, not listed.
    if (isLocalMode()) return Promise.resolve([]);
    return this.fallback.listMatches();
  }
  /** @deprecated Superseded by scanDemo + parseDemo. */
  uploadDemo(input: { fileName: string }): Promise<Match> {
    return this.fallback.uploadDemo(input);
  }
  /** Real music catalog from the orchestrator; falls back to the mock offline. */
  async listSongs(): Promise<Song[]> {
    try {
      const res = await fetch('/api/songs', { cache: 'no-store' });
      const data = await readJson<{
        songs: Array<{ id: string; title: string; artist?: string; genre?: string; durationSec?: number; license?: string; audioUrl: string }>;
      }>(res);
      return data.songs.map((s) => ({
        id: s.id,
        title: s.title,
        artist: s.artist ?? '',
        genre: s.genre ?? '',
        previewUrl: s.audioUrl,
        durationSec: s.durationSec ?? 0,
        license: s.license,
      }));
    } catch {
      return this.fallback.listSongs();
    }
  }

  /** Real preset registry from the orchestrator; falls back to the mock offline. */
  async listPresets(): Promise<Preset[]> {
    try {
      const res = await fetch('/api/presets', { cache: 'no-store' });
      const data = await readJson<{
        default?: string;
        presets: Array<{ name: string; label?: string; description?: string; hud_mode?: string; default?: boolean }>;
      }>(res);
      return data.presets.map((p) => ({
        name: p.name,
        label: p.label ?? p.name,
        description: p.description ?? '',
        hudMode: p.hud_mode,
        default: p.default,
      }));
    } catch {
      return this.fallback.listPresets();
    }
  }

  listFeed(): Promise<FeedItem[]> {
    // The community feed is a cloud surface; local studio shows no seeded feed.
    if (isLocalMode()) return Promise.resolve([]);
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
    headshots: p.headshots ?? 0,
    mvps: p.mvps ?? 0,
    rounds: p.rounds ?? 0,
    adr: p.adr ?? 0,
    hsPct: p.hs_pct ?? 0,
    kast: p.kast ?? 0,
    rating: p.rating ?? 0,
    rounds2k: p.rounds_2k,
    rounds3k: p.rounds_3k,
    rounds4k: p.rounds_4k,
    rounds5k: p.rounds_5k,
  };
}

/** Keeps only the known sides; anything else collapses to '' (spectator/unknown). */
function normalizeTeam(team: string): DemoPlayer['team'] {
  return team === 'CT' || team === 'T' ? team : '';
}

/** Server match summary (snake_case) → the UI's RosterMatch; undefined when the scan has none. */
function toRosterMatch(m: RosterMatchResponse | undefined): RosterMatch | undefined {
  if (!m) return undefined;
  return { map: m.map, scoreCt: m.score_ct, scoreT: m.score_t, rounds: m.rounds };
}
