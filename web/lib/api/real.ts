import type { ApiClient } from './client';
import type { Session, Match, Play, Song, Video, FeedItem, RenderMode, DemoPlayer, Preset, SteamUser, EditConfig, CaptureReadiness, CaptureTool, CaptureStatus, RosterMatch, CaptureProgress } from './types';
import { SERVICE_UNAVAILABLE_CODE } from './types';
import { MockApiClient } from './mock';
import { planToMatch, planToPlays, type KillPlan } from './map';
import { deriveReelView, type ReelAction, type ReelView, type RenderStatus } from './reel-reconcile';
import { loadReelIntents, saveReelIntents, DEFAULT_VARIANT, DEFAULT_EDIT_CONFIG, type ReelIntent } from './reel-store';
import { dataPlane, type DataPlane, type Loopback, type PcStatus } from './dataplane';
import { isLocalMode } from '@/lib/mode';
import { playsSelectionLabel } from '@/lib/format';

/** Segment ids joined into the stable local id for a reel (not an artifact path). */
function reelName(segmentIds: string[]): string {
  return segmentIds.join('_');
}

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
  'viral-60-clean': 'Killfeed',
  'clean-pov-60': 'POV limpio',
  'full-hud-60': 'HUD completo',
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
 * RealApiClient drives the whole upload→parse→record→render pipeline against one
 * orchestrator, reached over a mode-selected data plane (see lib/api/dataplane):
 * local mode proxies through the same-origin /api/demos/* routes; cloud mode
 * talks straight to the paired agent's loopback with a Bearer token read from
 * /api/pc/status. The orchestrator is the source of truth: the client persists
 * only lightweight reel INTENTS (reel-store) and derives each reel's live status
 * by reconciling against it on every poll (reel-reconcile), driving record→render
 * idempotently. A hard reload re-reads server state and resumes exactly where it
 * left off. Everything outside the upload→reel path (steam, library seeds, feed)
 * delegates to a MockApiClient. Selected by index.ts when NEXT_PUBLIC_API_BASE is
 * set or in local mode.
 */
export class RealApiClient implements ApiClient {
  private readonly fallback = new MockApiClient();
  /** Live, derived view of each tracked reel (status/downloadUrl/failureReason). */
  private readonly reels = new Map<string, Video>();
  /** Durable facts the user asked for, mirrored to localStorage via reel-store. */
  private readonly intents = new Map<string, ReelIntent>();
  /** Reels with a record/render POST in flight, so a tick never double-drives. */
  private readonly driving = new Set<string>();
  /** Cloud-mode loopback endpoint (port + token), resolved once from /api/pc/status. */
  private loopback: Loopback | null = null;
  /** Memoized data plane for the resolved loopback (or the constant local proxy). */
  private dpCache: DataPlane | null = null;
  /** Cloud-mode DOM object URLs for each ready reel's mp4/cover (Bearer-fetched). */
  private readonly media = new Map<string, { video?: string; cover?: string }>();
  /** Server-reported artifact names for each reel (the file names the editor wrote). */
  private readonly artifactNames = new Map<string, { video: string; cover: string }>();
  /** Reels with a loadMedia fetch in flight, so a tick never double-fetches media. */
  private readonly mediaLoading = new Set<string>();

  constructor() {
    // Rehydrate the reels the user asked for so the Library survives a hard reload
    // or a direct visit; their live status is filled on the first reconcile tick.
    for (const intent of loadReelIntents()) {
      this.intents.set(intent.videoId, intent);
      this.reels.set(intent.videoId, videoFromIntent(intent));
    }
  }

  /**
   * The active data plane: same-origin proxy (local) or cloud loopback. Memoized
   * so a poll tick's dozen requests reuse one built plane instead of rebuilding
   * its closures each call; invalidateLoopback drops the cache when the loopback
   * changes (401, offline). Local mode's plane is constant, so it caches once.
   */
  private async dp(): Promise<DataPlane> {
    if (this.dpCache) return this.dpCache;
    const lb = isLocalMode() ? null : await this.ensureLoopback();
    this.dpCache = dataPlane(lb);
    return this.dpCache;
  }

  /** Drops the cached loopback and its data plane so the next call re-resolves. */
  private invalidateLoopback(): void {
    this.loopback = null;
    this.dpCache = null;
  }

  /**
   * Resolves and caches the cloud loopback endpoint from /api/pc/status. A user
   * with no paired agent (or one that has not reported a loopback yet) has
   * nothing on their PC to reach, so this reports PC_OFFLINE and lets the upload
   * flow show the actionable "open FragForge Agent" state.
   */
  private async ensureLoopback(): Promise<Loopback> {
    if (this.loopback) return this.loopback;
    const status = await this.fetchPcStatus();
    if (!status.loopback) throw new Error('PC_OFFLINE');
    this.loopback = status.loopback;
    return this.loopback;
  }

  private async fetchPcStatus(): Promise<PcStatus> {
    const res = await fetch('/api/pc/status', { cache: 'no-store' });
    if (!res.ok) return { paired: false, online: false, loopback: null };
    return (await res.json()) as PcStatus;
  }

  /**
   * Issues one data-plane request. `build` receives the resolved DataPlane and
   * returns a URL plus optional init; send() merges the transport's auth headers
   * and, on a cloud 401 (a rotated/stale token), drops the cached loopback so the
   * next call re-resolves it from /api/pc/status. In cloud mode a fetch rejection
   * means the loopback died mid-flight (PC slept, agent quit): translate it to
   * PC_OFFLINE (and drop the cached loopback) so callers offer Retry instead of a
   * raw network error. Local mode keeps propagating so the 503 service_unavailable
   * contract is untouched.
   */
  private async send(build: (dp: DataPlane) => { url: string; init?: RequestInit }): Promise<Response> {
    const dp = await this.dp();
    const { url, init } = build(dp);
    const headers = { ...dp.headers, ...((init?.headers as Record<string, string> | undefined) ?? {}) };
    let res: Response;
    try {
      res = await fetch(url, { ...init, headers });
    } catch (err) {
      if (!isLocalMode()) {
        this.invalidateLoopback();
        throw new Error('PC_OFFLINE');
      }
      throw err;
    }
    if (res.status === 401 && !isLocalMode()) this.invalidateLoopback();
    return res;
  }

  /** Loopback liveness for cloud mode; local mode has no separate probe. */
  private async probeHealthz(dp: DataPlane): Promise<boolean> {
    if (!dp.healthzUrl) return true;
    try {
      const res = await fetch(dp.healthzUrl, { headers: dp.headers, cache: 'no-store' });
      return res.ok;
    } catch {
      return false;
    }
  }

  async scanDemo(file: File): Promise<{ jobId: string; players: DemoPlayer[]; match?: RosterMatch }> {
    const dp = await this.dp();
    // Cloud mode is a public-HTTPS -> loopback hop: probe the agent's /healthz
    // before uploading so an offline PC surfaces as PC_OFFLINE (an actionable
    // "open FragForge Agent" state) instead of a failed upload mid-stream. Local
    // mode has no probe and always passes.
    if (!(await this.probeHealthz(dp))) {
      this.invalidateLoopback();
      throw new Error('PC_OFFLINE');
    }

    const form = new FormData();
    form.append(dp.scanField, file);
    const scanned = await readJson<unknown>(
      await this.send((d) => ({ url: d.scanUrl, init: { method: 'POST', body: form } })),
    );
    const jobId = dp.scanJobId(scanned);

    await this.waitForStatus(jobId, 'scanned');

    const roster = await readJson<RosterResponse>(await this.send((d) => ({ url: d.rosterUrl(jobId) })));
    return { jobId, players: roster.players.map(toDemoPlayer), match: toRosterMatch(roster.match) };
  }

  async parseDemo(input: { jobId: string; steamId: string }): Promise<Match> {
    await readJson<unknown>(
      await this.send((dp) => ({
        url: dp.parseUrl(input.jobId),
        init: {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(dp.parseBody(input.steamId)),
        },
      })),
    );

    await this.waitForStatus(input.jobId, 'parsed');

    const [plan, roster] = await Promise.all([
      readJson<KillPlan>(await this.send((dp) => ({ url: dp.planUrl(input.jobId) }))),
      readJson<RosterResponse>(await this.send((dp) => ({ url: dp.rosterUrl(input.jobId) }))),
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

    const plan = await readJson<KillPlan>(await this.send((dp) => ({ url: dp.planUrl(id) })));
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
      const { players } = await readJson<RosterResponse>(await this.send((dp) => ({ url: dp.rosterUrl(jobId) })));
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

    const plan = await readJson<KillPlan>(await this.send((dp) => ({ url: dp.planUrl(matchId) })));
    return planToPlays(matchId, plan);
  }

  /**
   * Polls /status until it reaches `want`; throws on `failed` or timeout. In
   * cloud mode a loopback that drops mid-poll (PC slept, agent quit) throws from
   * the fetch; that is translated to PC_OFFLINE so the flow offers Retry instead
   * of a raw network error.
   */
  private async waitForStatus(jobId: string, want: string, maxAttempts = 240): Promise<void> {
    for (let attempt = 0; attempt < maxAttempts; attempt++) {
      let status: string | null;
      try {
        status = await this.fetchStatus(jobId);
      } catch (err) {
        if (!isLocalMode()) {
          this.invalidateLoopback();
          throw new Error('PC_OFFLINE');
        }
        throw err;
      }
      if (status === want) return;
      if (status === 'failed') throw new Error(`job ${jobId} failed`);
      await sleep(800);
    }
    throw new Error(`timed out waiting for ${want}`);
  }

  /**
   * For an uploaded job (matchId = job UUID, playIds = the segment ids picked,
   * in plan order), registers a durable reel intent and returns immediately
   * with a queued Video. 2+ ids render as one concatenated reel. The reconcile
   * loop (driven by listVideos polling) advances it record→render; this is safe
   * across reloads because every step is derived from the orchestrator's state.
   * Mock matches delegate to the fallback.
   */
  async createVideo(input: { matchId: string; playIds: string[]; mode: RenderMode; songId?: string; variant?: string; editConfig?: EditConfig }): Promise<Video> {
    if (!isJobId(input.matchId)) return this.fallback.createVideo(input);

    const videoId = `${input.matchId}__${reelName(input.playIds)}`;
    const existing = this.reels.get(videoId);
    if (existing && existing.status !== 'failed') return { ...existing };

    const [plays, match] = await Promise.all([this.findClips(input.matchId), this.getMatch(input.matchId)]);
    // Preserve the caller's (plan) order rather than the plays array's order.
    const pickedPlays = input.playIds.map((pid) => plays.find((p) => p.id === pid)).filter((p): p is Play => Boolean(p));
    const variant = input.variant ?? REEL_VARIANT;
    const suffix = input.songId ? `${variantLabel(variant)} + Music` : variantLabel(variant);
    const intent: ReelIntent = {
      videoId,
      jobId: input.matchId,
      segmentIds: pickedPlays.map((p) => p.id),
      mode: input.mode,
      variant,
      editConfig: input.editConfig ?? DEFAULT_EDIT_CONFIG,
      songId: input.songId,
      title: `${playsSelectionLabel(pickedPlays) ?? 'Highlight'} - ${suffix}`,
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
   * Removes a reel from the library. The orchestrator delete (video + cover +
   * caption artifacts, freeing disk) is best-effort: the local intent is
   * dropped regardless so the reel disappears even when the orchestrator is
   * unreachable, matching the user's intent to clear it.
   */
  async deleteVideo(id: string): Promise<void> {
    const intent = this.intents.get(id);
    if (!intent) return this.fallback.deleteVideo(id);

    try {
      const variant = variantOf(intent);
      const name = await this.resolveArtifactName(intent, variant);
      // No name means nothing was ever rendered for this reel, so there is no
      // artifact to delete - just drop it locally below.
      if (name) {
        await this.send((dp) => ({ url: dp.videoUrl(intent.jobId, variant, name), init: { method: 'DELETE' } }));
      }
    } catch {
      // Orchestrator offline: the artifacts stay on disk, but the reel still
      // leaves the library. A future render of the same job overwrites them.
    }
    this.revokeMedia(id);
    this.artifactNames.delete(id);
    this.intents.delete(id);
    this.reels.delete(id);
    saveReelIntents(Array.from(this.intents.values()));
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
    // Job and render status have no happy-path data dependency; fetch in parallel.
    const [job, render] = await Promise.all([
      this.fetchStatusFull(intent.jobId),
      this.fetchRenderStatus(intent.jobId, variantOf(intent)),
    ]);
    // Capture the server's real artifact names so applyView/loadMedia address
    // the reel by the editor's file names instead of guessing from segment ids.
    if (render.videoName && render.coverName) {
      this.artifactNames.set(intent.videoId, { video: render.videoName, cover: render.coverName });
    }
    if (job === null) {
      // Memory-mode orchestrator restart drops jobs; surface it instead of spinning.
      this.applyView(intent, {
        status: 'failed',
        action: 'none',
        failureReason: 'job no longer available (the local orchestrator may have restarted)',
      });
      return;
    }
    const view = deriveReelView({
      jobStatus: job.status,
      jobFailureReason: job.failureReason,
      renderStatus: render.status,
      renderFailureReason: render.failureReason,
      captureProgress: job.captureProgress,
    });
    this.applyView(intent, view);
    if (view.action !== 'none') void this.drive(intent, view.action);
  }

  /** Writes a reel's derived view onto its live Video, wiring URLs once ready. */
  private applyView(intent: ReelIntent, view: ReelView): void {
    const base = this.reels.get(intent.videoId) ?? videoFromIntent(intent);
    // captureProgress is present only while recording (view carries it through);
    // any other status clears it so a stale percent never lingers on the card.
    const next: Video = { ...base, status: view.status, failureReason: view.failureReason, captureProgress: view.captureProgress };
    // The server-reported artifact names are present once the render is ready
    // (fetchRenderStatus fills them). If a tick sees ready before the names are
    // known, leave the URLs unset so the card keeps its placeholder until the
    // next tick resolves them - the same not-yet-ready handling as before.
    const names = this.artifactNames.get(intent.videoId);
    if (view.status === 'ready' && names) {
      const variant = variantOf(intent);
      if (isLocalMode()) {
        // Same-origin proxy URLs the browser can hand straight to <video>/<img>.
        const dp = dataPlane(null);
        next.downloadUrl = dp.videoUrl(intent.jobId, variant, names.video);
        next.thumbnailUrl = dp.coverUrl(intent.jobId, variant, names.cover);
      } else {
        // Cloud: the bytes sit behind the Bearer-gated loopback, which a bare
        // <video>/<img> src cannot authenticate to. Serve DOM object URLs
        // fetched with the token; loadMedia fills them on a later tick, and the
        // card shows its placeholder cover until then. Surface whatever is cached
        // (possibly a partial entry when one of the two fetches failed) and kick
        // loadMedia to fill the rest until both pieces are present.
        const cached = this.media.get(intent.videoId);
        next.downloadUrl = cached?.video;
        next.thumbnailUrl = cached?.cover;
        if (!cached || !cached.video || !cached.cover) void this.loadMedia(intent);
      }
    }
    this.reels.set(intent.videoId, next);
  }

  /**
   * Cloud mode only: fetches a ready reel's mp4 and cover through the
   * authenticated loopback and caches DOM object URLs, then surfaces them on the
   * live reel. Self-healing: when only one of the two fetches succeeded, the
   * partial entry is cached and a later tick re-fetches only the missing piece and
   * merges it, so a transient cover (or video) failure does not strand the reel
   * without media until reload. An in-flight guard keeps concurrent ticks from
   * double-fetching the same piece and orphaning object URLs.
   */
  private async loadMedia(intent: ReelIntent): Promise<void> {
    const cached = this.media.get(intent.videoId);
    if (cached?.video && cached?.cover) return; // already fully loaded
    if (this.mediaLoading.has(intent.videoId)) return;
    const names = this.artifactNames.get(intent.videoId);
    if (!names) return; // names not resolved yet; a later tick retries once ready
    this.mediaLoading.add(intent.videoId);
    try {
      const variant = variantOf(intent);
      // Reuse the already-fetched piece verbatim (no re-fetch, so no URL to
      // orphan) and fetch only what is still missing.
      const [video, cover] = await Promise.all([
        cached?.video ? Promise.resolve(cached.video) : this.objectUrl((dp) => dp.videoUrl(intent.jobId, variant, names.video)),
        cached?.cover ? Promise.resolve(cached.cover) : this.objectUrl((dp) => dp.coverUrl(intent.jobId, variant, names.cover)),
      ]);
      if (!video && !cover) return; // nothing fetched (offline / not ready yet): retry next tick
      this.media.set(intent.videoId, { video, cover });
      const reel = this.reels.get(intent.videoId);
      if (reel && reel.status === 'ready') {
        this.reels.set(intent.videoId, { ...reel, downloadUrl: video, thumbnailUrl: cover });
      }
    } finally {
      this.mediaLoading.delete(intent.videoId);
    }
  }

  /** Fetches one loopback artifact and wraps it in a DOM object URL, or undefined. */
  private async objectUrl(build: (dp: DataPlane) => string): Promise<string | undefined> {
    try {
      const res = await this.send((dp) => ({ url: build(dp) }));
      if (!res.ok) return undefined;
      return URL.createObjectURL(await res.blob());
    } catch {
      return undefined;
    }
  }

  /** Releases any object URLs held for a reel (cloud mode). */
  private revokeMedia(videoId: string): void {
    const held = this.media.get(videoId);
    if (!held) return;
    if (held.video) URL.revokeObjectURL(held.video);
    if (held.cover) URL.revokeObjectURL(held.cover);
    this.media.delete(videoId);
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
            // segment_ids scopes the capture to exactly the selected clips (in plan
            // order) instead of recording the whole demo. 2+ ids render as one
            // concatenated reel.
            await this.send((dp) => ({
              url: dp.recordUrl(intent.jobId),
              init: {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ preset: variant, segment_ids: intent.segmentIds }),
              },
            }))
          : await this.send((dp) => ({
              url: dp.renderUrl(intent.jobId, variant),
              init: {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                  music: intent.mode === 'music' ? intent.songId : undefined,
                  edit: buildEditRequest(intent.editConfig),
                }),
              },
            }));
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

  /**
   * The reel's real artifact name for delete: the cached server name when the
   * reel has been reconciled this session, else fetched fresh (a delete right
   * after a hard reload has no cached name yet). Undefined when nothing was
   * rendered, so there is no artifact to remove.
   */
  private async resolveArtifactName(intent: ReelIntent, variant: string): Promise<string | undefined> {
    const cached = this.artifactNames.get(intent.videoId);
    if (cached) return cached.video;
    const render = await this.fetchRenderStatus(intent.jobId, variant);
    return render.videoName;
  }

  /**
   * Reads job status + failure reason (+ live capture progress while recording);
   * null when the job is unknown (404). Cloud and local share this path: both hit
   * GET /api/jobs/{id} (loopback or same-origin proxy), so the progress the
   * orchestrator reports flows to the card the same way in either mode.
   */
  private async fetchStatusFull(
    jobId: string,
  ): Promise<{ status: string; failureReason?: string; captureProgress?: CaptureProgress } | null> {
    const res = await this.send((dp) => ({ url: dp.jobStatusUrl(jobId) }));
    if (res.status === 404) return null;
    const data = await readJson<{
      status: string;
      failure_reason?: string;
      progress?: { stage?: string; done?: number; total?: number };
    }>(res);
    const full: { status: string; failureReason?: string; captureProgress?: CaptureProgress } = {
      status: data.status,
      failureReason: data.failure_reason,
    };
    const p = data.progress;
    if (p && typeof p.done === 'number' && typeof p.total === 'number' && p.total > 0) {
      full.captureProgress = { done: p.done, total: p.total };
    }
    return full;
  }

  /** Reads the job status string; null when the job is unknown (404). */
  private async fetchStatus(jobId: string): Promise<string | null> {
    const full = await this.fetchStatusFull(jobId);
    return full ? full.status : null;
  }

  /**
   * Reads the reel render-variant state; 'none' when the render has not started.
   * The orchestrator reports the reel's real artifact file names (videos[0]/
   * covers[0]) so the client addresses the mp4/cover by the name the editor
   * actually wrote (e.g. "demo-compilation") instead of guessing it from the
   * segment ids. They are absent until the render is ready.
   */
  private async fetchRenderStatus(
    jobId: string,
    variant: string,
  ): Promise<{ status: RenderStatus; failureReason?: string; videoName?: string; coverName?: string }> {
    const res = await this.send((dp) => ({ url: dp.renderUrl(jobId, variant) }));
    if (!res.ok) return { status: 'none' }; // 404 = render not started yet
    const data = (await res.json()) as { status?: string; failure_reason?: string; videos?: string[]; covers?: string[] };
    const known = new Set<RenderStatus>(['queued', 'rendering', 'ready', 'failed']);
    const status: RenderStatus = data.status && known.has(data.status as RenderStatus) ? (data.status as RenderStatus) : 'none';
    return { status, failureReason: data.failure_reason, videoName: data.videos?.[0], coverName: data.covers?.[0] };
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
    return { paired: (await this.fetchPcStatus()).paired };
  }

  /**
   * Reads capture readiness from the local orchestrator (/api/capabilities): is
   * the record worker enabled and are HLAE/CS2/recorder reachable. A 503 maps to
   * 'offline' (orchestrator down) rather than 'unconfigured', so the UI can tell
   * "start your orchestrator" apart from "set your tool paths".
   */
  async getCaptureReadiness(): Promise<CaptureReadiness> {
    try {
      const res = await this.send((dp) => ({ url: dp.capabilitiesUrl, init: { cache: 'no-store' } }));
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
      let status: CaptureStatus;
      if (!enabled) {
        status = 'unconfigured';
      } else if (anyMissing) {
        status = 'warning';
      } else {
        status = 'ready';
      }
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
