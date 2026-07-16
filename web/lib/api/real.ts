import type { ApiClient } from './client';
import type { Match, Play, Song, Video, FeedItem, RenderMode, DemoPlayer, Preset, EditConfig, CaptureReadiness, CaptureTool, CaptureStatus, RosterMatch, CaptureProgress, SeriesDemo } from './types';
import { SERVICE_UNAVAILABLE_CODE, PLAN_READY_STATUSES } from './types';
import { MockApiClient } from './mock';
import { planToMatch, planToPlays, type KillPlan } from './map';
import { canHaveRenderState, deriveReelView, type ReelAction, type ReelView, type RenderStatus } from './reel-reconcile';
import { loadReelIntents, saveReelIntents, DEFAULT_VARIANT, DEFAULT_EDIT_CONFIG, type ReelIntent } from './reel-store';
import { dataPlane, type DataPlane } from './dataplane';
import { parsePublishAssistant, type PublishAssistant } from './publish-assistant';
import {
  ROSTER_READY,
  listableJobs,
  summarizeSeries,
  jobToMatch,
  type IndexedJob,
  type SeriesSummary,
} from './jobs-index';
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
  hook_text: boolean;
  kill_counter: boolean;
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
    hook_text: edit.hookText,
    kill_counter: edit.killCounter,
  };
  const introText = edit.introText?.trim();
  if (edit.intro && introText) body.intro_text = introText;
  const outroText = edit.outroText?.trim();
  if (edit.outro && outroText) body.outro_text = outroText;
  return body;
}

/**
 * The render request's `music` field. A reel with no music sends nothing; a reel
 * at default full volume sends the bare song key (byte-identical to legacy reels
 * that predate volume); only a reduced volume upgrades to the `{ key, volume }`
 * object the orchestrator accepts (volume in (0,1]).
 */
function buildMusicRequest(intent: ReelIntent): string | { key: string; volume: number } | undefined {
  if (intent.mode !== 'music' || !intent.songId) return undefined;
  if (intent.musicVolume !== undefined && intent.musicVolume < 1) {
    return { key: intent.songId, volume: intent.musicVolume };
  }
  return intent.songId;
}

/** A queued placeholder Video for an intent; its live status is filled by reconcile. */
function videoFromIntent(intent: ReelIntent): Video {
  return {
    id: intent.videoId,
    jobId: intent.jobId,
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
  };
}

/**
 * RealApiClient drives the whole upload→parse→record→render pipeline against
 * the local orchestrator bundled with the desktop app, reached through the
 * same-origin /api/demos/* proxy routes (see lib/api/dataplane). The
 * orchestrator is the source of truth: the client persists only lightweight
 * reel INTENTS (reel-store) and derives each reel's live status by
 * reconciling against it on every poll (reel-reconcile), driving
 * record→render idempotently. A hard reload re-reads server state and
 * resumes exactly where it left off. Everything outside the upload→reel path
 * (library seeds, feed) delegates to a MockApiClient.
 */
export class RealApiClient implements ApiClient {
  private readonly fallback = new MockApiClient();
  /** Live, derived view of each tracked reel (status/downloadUrl/failureReason). */
  private readonly reels = new Map<string, Video>();
  /** Durable facts the user asked for, mirrored to localStorage via reel-store. */
  private readonly intents = new Map<string, ReelIntent>();
  /** Reels with a record/render POST in flight, so a tick never double-drives. */
  private readonly driving = new Set<string>();
  /** Server-reported artifact names for each reel (the file names the editor wrote). */
  private readonly artifactNames = new Map<string, { video: string; cover?: string }>();
  /**
   * Cached per-job series match (map/score), keyed by jobId. A match is
   * immutable once a job has one, and this client is a module singleton, so
   * getSeries reads a cached hit instead of refetching the roster every poll.
   */
  private readonly seriesMatches = new Map<string, RosterMatch>();

  constructor() {
    // Rehydrate the reels the user asked for so the Library survives a hard reload
    // or a direct visit; their live status is filled on the first reconcile tick.
    for (const intent of loadReelIntents()) {
      this.intents.set(intent.videoId, intent);
      this.reels.set(intent.videoId, videoFromIntent(intent));
    }
  }

  /** The local same-origin proxy data plane (the only transport this app has). */
  private dp(): DataPlane {
    return dataPlane();
  }

  /**
   * Issues one data-plane request. `build` receives the DataPlane and returns
   * a URL plus optional init; send() merges the transport's headers on top.
   */
  private async send(build: (dp: DataPlane) => { url: string; init?: RequestInit }): Promise<Response> {
    const dp = this.dp();
    const { url, init } = build(dp);
    const headers = { ...dp.headers, ...((init?.headers as Record<string, string> | undefined) ?? {}) };
    return fetch(url, { ...init, headers });
  }

  /** Reads a job's roster scan (players + optional match context) from the proxy. */
  private async fetchRoster(jobId: string): Promise<RosterResponse> {
    return readJson<RosterResponse>(await this.send((dp) => ({ url: dp.rosterUrl(jobId) })));
  }

  async scanDemo(file: File, opts?: { seriesId?: string }): Promise<{ jobId: string; players: DemoPlayer[]; match?: RosterMatch }> {
    const dp = this.dp();
    const form = new FormData();
    form.append(dp.scanField, file);
    // Tag the upload as one demo of a bulk series; the scan proxy streams the
    // multipart body straight through, so the orchestrator reads series_id.
    if (opts?.seriesId) form.append(dp.scanSeriesField, opts.seriesId);
    const scanned = await readJson<unknown>(
      await this.send((d) => ({ url: d.scanUrl, init: { method: 'POST', body: form } })),
    );
    const jobId = dp.scanJobId(scanned);

    await this.waitForStatus(jobId, 'scanned');

    const roster = await readJson<RosterResponse>(await this.send((d) => ({ url: d.rosterUrl(jobId) })));
    return { jobId, players: roster.players.map(toDemoPlayer), match: toRosterMatch(roster.match) };
  }

  /**
   * Lists the demos uploaded under one bulk series, in upload order, then
   * best-effort enriches each demo that has a roster with its map/score. A
   * single demo's roster failure (still scanning → 409, or transient) leaves
   * that demo's match undefined and never rejects the whole call.
   */
  async getSeries(seriesId: string): Promise<SeriesDemo[]> {
    type ProxyDemo = { jobId: string; status: string; failureReason?: string; fileName?: string };
    const body = await readJson<{ demos: ProxyDemo[] }>(await this.send((dp) => ({ url: dp.seriesUrl(seriesId) })));
    return Promise.all(
      body.demos.map(async (raw): Promise<SeriesDemo> => {
        const demo: SeriesDemo = { jobId: raw.jobId, status: raw.status };
        if (raw.fileName) demo.fileName = raw.fileName;
        if (raw.failureReason) demo.failureReason = raw.failureReason;
        const cached = this.seriesMatches.get(raw.jobId);
        if (cached) {
          demo.match = cached;
        } else if (ROSTER_READY.has(raw.status)) {
          try {
            const roster = await this.fetchRoster(raw.jobId);
            const match = toRosterMatch(roster.match);
            if (match) {
              this.seriesMatches.set(raw.jobId, match);
              demo.match = match;
            }
          } catch {
            // Roster not ready (409) or a transient failure: leave match unset.
          }
        }
        return demo;
      }),
    );
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
    if (!PLAN_READY_STATUSES.has(status)) return null;

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
    if (status === null || !PLAN_READY_STATUSES.has(status)) return [];

    const plan = await readJson<KillPlan>(await this.send((dp) => ({ url: dp.planUrl(matchId) })));
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
   * For an uploaded job (matchId = job UUID, playIds = the segment ids picked,
   * in plan order), registers a durable reel intent and returns immediately
   * with a queued Video. 2+ ids render as one concatenated reel. The reconcile
   * loop (driven by listVideos polling) advances it record→render; this is safe
   * across reloads because every step is derived from the orchestrator's state.
   * Mock matches delegate to the fallback.
   */
  async createVideo(input: { matchId: string; playIds: string[]; mode: RenderMode; songId?: string; musicVolume?: number; variant?: string; editConfig?: EditConfig }): Promise<Video> {
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
      // Volume only rides along with a chosen song; without one it is meaningless.
      musicVolume: input.songId ? input.musicVolume : undefined,
      title: `${playsSelectionLabel(pickedPlays) ?? 'Highlight'} - ${suffix}`,
      map: match?.map ?? 'Unknown',
      score: match?.score ?? '',
      createdAt: Date.now(),
    };
    this.intents.set(videoId, intent);
    saveReelIntents(Array.from(this.intents.values()));
    this.reels.set(videoId, videoFromIntent(intent));
    void this.reconcile(); // kick now (idempotent); /videos polling continues it.
    return { ...videoFromIntent(intent) };
  }

  async listVideos(): Promise<Video[]> {
    await this.reconcile();
    // The Library shows only the user's own real reels, persisted on this PC.
    return Array.from(this.reels.values())
      .sort((a, b) => b.createdAt - a.createdAt)
      .map((v) => ({ ...v }));
  }

  async getVideo(id: string): Promise<Video | null> {
    const reel = this.reels.get(id);
    if (reel) return { ...reel };
    return this.fallback.getVideo(id);
  }

  async getPublishAssistant(id: string): Promise<PublishAssistant> {
    const intent = this.intents.get(id);
    if (!intent) return this.fallback.getPublishAssistant(id);
    const reel = this.reels.get(id);
    if (!reel || reel.status !== 'ready') throw new Error('video is not ready for publication');
    const variant = variantOf(intent);
    const name = await this.resolveArtifactName(intent, variant);
    if (!name) throw new Error('rendered video artifact is not available');
    const raw = await readJson<unknown>(
      await this.send((dp) => ({
        url: dp.publishAssistantUrl(intent.jobId, variant, name),
        init: { cache: 'no-store' },
      })),
    );
    return parsePublishAssistant(raw);
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
    this.artifactNames.delete(id);
    this.intents.delete(id);
    this.reels.delete(id);
    saveReelIntents(Array.from(this.intents.values()));
  }

  /**
   * Deletes a demo job (match) and every server-side artifact behind it (the
   * orchestrator wipes rendered videos, covers, and the demo copy). A 404 means
   * it was already gone, which is still success; a 409 (the job is still
   * queued/scanning/parsing/recording/composing) or a 503 (orchestrator offline)
   * throws with the body's error/code so the UI can message it. On success the
   * local reels forged from this job are pruned so the Library never keeps a
   * reel whose demo no longer exists.
   */
  async deleteMatch(jobId: string): Promise<void> {
    const res = await this.send((dp) => ({ url: dp.jobDeleteUrl(jobId), init: { method: 'DELETE' } }));
    // 404 = already gone (success). Any other non-2xx (409 busy, 503 offline,
    // 500) throws here, carrying the backend's error message and stable code.
    if (res.status !== 404 && !res.ok) await readJson<unknown>(res);
    this.pruneJob(jobId);
  }

  /**
   * Deletes every demo of a bulk series, one at a time via the same per-job
   * delete. The series' jobIds come from the existing series listing; a member
   * that is already gone (404) is tolerated by deleteMatch, while a still-busy
   * member (409) surfaces so the UI can explain the wait. Local reels for each
   * deleted member are pruned as part of deleteMatch.
   */
  async deleteSeries(seriesId: string): Promise<void> {
    const demos = await this.getSeries(seriesId);
    for (const demo of demos) {
      await this.deleteMatch(demo.jobId);
    }
  }

  /**
   * Drops every locally tracked reel forged from a deleted job: its intents,
   * derived live views, cached artifact names, and cached series match, then
   * persists the surviving intents. Deleting from a Map while iterating its
   * entries is safe (each key is visited once), and reels/artifactNames are
   * keyed by the same videoIds the intents carry.
   */
  private pruneJob(jobId: string): void {
    for (const [videoId, intent] of this.intents) {
      if (intent.jobId !== jobId) continue;
      this.intents.delete(videoId);
      this.reels.delete(videoId);
      this.artifactNames.delete(videoId);
    }
    this.seriesMatches.delete(jobId);
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
    // The render variant only exists once a render POST has been driven (at/after
    // 'recorded'); before that the GET is a guaranteed 404 that floods the browser
    // network console the whole recording phase, so gate the call on the job status
    // and use 'none' — the same value the GET would map a 404 to — otherwise.
    const render: { status: RenderStatus; failureReason?: string; videoName?: string; coverName?: string } =
      canHaveRenderState(job.status)
        ? await this.fetchRenderStatus(intent.jobId, variantOf(intent))
        : { status: 'none' };
    // Capture the server's real artifact names so applyView addresses the reel
    // by the editor's file names instead of guessing from segment ids.
    if (render.videoName) {
      const names: { video: string; cover?: string } = { video: render.videoName };
      if (render.coverName) names.cover = render.coverName;
      this.artifactNames.set(intent.videoId, names);
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
      // Same-origin proxy URLs the browser can hand straight to <video>/<img>.
      const variant = variantOf(intent);
      const dp = dataPlane();
      next.downloadUrl = dp.videoUrl(intent.jobId, variant, names.video);
      if (names.cover) next.thumbnailUrl = dp.coverUrl(intent.jobId, variant, names.cover);
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
            // segment_ids scopes the capture to exactly the selected clips (in plan
            // order) instead of recording the whole demo. 2+ ids render as one
            // concatenated reel.
            await this.send((dp) => ({
              url: dp.recordUrl(intent.jobId),
              init: {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                  preset: variant,
                  segment_ids: intent.segmentIds,
                  edit: buildEditRequest(intent.editConfig),
                }),
              },
            }))
          : await this.send((dp) => ({
              url: dp.renderUrl(intent.jobId, variant),
              init: {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                  music: buildMusicRequest(intent),
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
   * null when the job is unknown (404).
   */
  private async fetchStatusFull(
    jobId: string,
  ): Promise<{ status: string; failureReason?: string; captureProgress?: CaptureProgress } | null> {
    const res = await this.send((dp) => ({ url: dp.jobStatusUrl(jobId) }));
    if (res.status === 404) return null;
    const data = await readJson<{
      status: string;
      failure_reason?: string;
      progress?: { done?: number; total?: number };
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

  // --- everything below is outside the upload→reel path: capture readiness is
  // real, the rest delegates to (or stubs out) the mock fallback ---

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
  /**
   * Rediscovers the demos uploaded on this PC by listing the orchestrator's
   * persisted jobs, so Partidas is populated after an app restart instead of
   * only being reachable by a kept URL. Only jobs past a roster scan list; each
   * lists as one Match per demo (a series still yields one entry per map, the
   * Partidas model), best-effort enriched from its roster in parallel with
   * per-job failures tolerated, newest first.
   */
  async listMatches(): Promise<Match[]> {
    const jobs = await this.fetchJobs();
    return Promise.all(listableJobs(jobs).map((job) => this.jobToMatchEnriched(job)));
  }

  /**
   * One summary per uploaded series, derived from the same jobs listing, so
   * Partidas can offer a way into each series even when its maps list
   * individually below. Series with maps still scanning are included so a fresh
   * bulk upload is discoverable immediately.
   */
  async listSeriesSummaries(): Promise<SeriesSummary[]> {
    return summarizeSeries(await this.fetchJobs());
  }

  /** The recent demo jobs the orchestrator persists (the Partidas index feed). */
  private async fetchJobs(): Promise<IndexedJob[]> {
    const body = await readJson<{ jobs: IndexedJob[] }>(await this.send((dp) => ({ url: dp.jobsUrl })));
    return body.jobs;
  }

  /**
   * One job → its Match, best-effort enriched from the roster: the demo's map
   * and, when the job's target is in the roster, that player's scoreboard. A
   * roster that is not ready (still scanning) or a transient failure leaves a
   * filename-titled, zeroed entry rather than rejecting the whole list.
   */
  private async jobToMatchEnriched(job: IndexedJob): Promise<Match> {
    try {
      const roster = await this.fetchRoster(job.jobId);
      const enrichment: { map?: string; player?: DemoPlayer } = {};
      if (roster.match) enrichment.map = roster.match.map;
      const row = job.targetSteamId ? roster.players.find((p) => p.steamid64 === job.targetSteamId) : undefined;
      if (row) enrichment.player = toDemoPlayer(row);
      return jobToMatch(job, enrichment);
    } catch {
      // Roster not ready (409) or a transient failure: still list the demo.
      return jobToMatch(job);
    }
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
    // The community feed was a cloud surface; the desktop app shows no feed.
    return Promise.resolve([]);
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
