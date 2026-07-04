import { isLocalMode, isHostedMode } from '@/lib/mode';
import { agentBaseUrl, agentHeaders } from '@/lib/agent/connection';
import { SERVICE_UNAVAILABLE_CODE } from './types';

/**
 * Transport options for RealStreamsApiClient, mirroring RealApiClient. In hosted
 * mode `native` remaps the same-origin `/api/streams/*` proxy paths to the
 * agent's NATIVE `/api/stream-jobs/*` routes, `baseUrl` prefixes them with the
 * agent origin, and `headers` supplies the per-request X-FragForge-Token.
 */
export type RealStreamsTransport = {
  baseUrl?: string;
  headers?: () => Record<string, string>;
  native?: boolean;
};

/**
 * Stream Clips: turn a Twitch clip/VOD (or an uploaded MP4) into vertical
 * Shorts with the streamer's facecam stacked over gameplay, with optional
 * burned captions. This client mirrors the shape of RealApiClient/MockApiClient
 * in this directory (real fetch client selected once NEXT_PUBLIC_API_BASE is
 * set or in local-studio mode, else an in-memory mock so the UI is developable
 * offline) but is kept separate from the demo->reel ApiClient since it talks to
 * an unrelated orchestrator surface (/api/stream-jobs), not /api/jobs.
 */

export type StreamJobStatus = 'acquiring' | 'uploaded' | 'ready' | 'rendering' | 'rendered' | 'failed';

export type StreamProbe = {
  width: number;
  height: number;
  duration_seconds: number;
};

/** A crop rectangle normalized to 0..1 of the source frame. */
export type NormalizedRect = { x: number; y: number; width: number; height: number };

export type StreamVariant = 'streamer-vertical-stack-40-60' | 'streamer-vertical-stack' | 'streamer-fullframe-nocam';

export const STREAM_VARIANTS: { value: StreamVariant; label: string; subtitle: string; needsFaceCrop: boolean }[] = [
  { value: 'streamer-vertical-stack-40-60', label: 'Facecam 40', subtitle: 'Gameplay 60', needsFaceCrop: true },
  { value: 'streamer-vertical-stack', label: 'Stack', subtitle: 'Cam / juego / chat', needsFaceCrop: true },
  { value: 'streamer-fullframe-nocam', label: 'Full-frame', subtitle: 'Sin facecam', needsFaceCrop: false },
];

export type StreamClipRange = { id: string; start_seconds: number; end_seconds: number; title?: string };

export type StreamCaptions = { enabled: boolean; language: string };

/** A music catalog track mixed under the clip audio; empty key means none. */
export type StreamMusic = { key?: string; volume?: number };

/** Light post effects; grade is the mild viral contrast/saturation lift. */
export type StreamEffects = { grade?: boolean };

export type StreamEditPlan = {
  schema_version: number;
  variant: StreamVariant;
  face_crop?: NormalizedRect;
  gameplay_crop?: NormalizedRect;
  clips: StreamClipRange[];
  captions?: StreamCaptions;
  music?: StreamMusic;
  effects?: StreamEffects;
  updated_at?: string;
};

export type StreamJob = {
  id: string;
  status: StreamJobStatus;
  title?: string;
  probe?: StreamProbe;
  edit_plan?: StreamEditPlan;
  failure_reason?: string;
  created_at: string;
};

export type StreamRenderVideo = { clip_id: string; title?: string; key: string; duration_seconds?: number };
export type StreamRenderStatus = 'queued' | 'rendering' | 'rendered' | 'failed' | 'none';
export type StreamRenderState = {
  status: StreamRenderStatus;
  videos: StreamRenderVideo[];
  warnings?: string[];
  error?: string;
};

export interface StreamsApiClient {
  createFromUrl(input: { sourceUrl: string; title?: string }): Promise<StreamJob>;
  createFromFile(file: File, title?: string): Promise<StreamJob>;
  listJobs(): Promise<StreamJob[]>;
  getJob(id: string): Promise<StreamJob | null>;
  /** Same-origin URL for a <video> element to pull the job's source MP4. */
  sourceUrl(id: string): string;
  getEditPlan(id: string): Promise<StreamEditPlan>;
  putEditPlan(id: string, plan: StreamEditPlan): Promise<StreamEditPlan>;
  startRender(id: string, variant: StreamVariant): Promise<StreamRenderState>;
  getRenderState(id: string, variant: StreamVariant): Promise<StreamRenderState>;
  /** Same-origin URL for a <video>/download link to a rendered Short. */
  videoUrl(id: string, variant: StreamVariant, clipId: string): string;
}

async function readJson<T>(res: Response): Promise<T> {
  if (!res.ok) {
    const body = await res.json().catch(() => null);
    const message = body && typeof body.error === 'string' ? body.error : `request failed (${res.status})`;
    const err = new Error(message) as Error & { code?: string };
    if (body && typeof body.code === 'string') err.code = body.code;
    throw err;
  }
  return (await res.json()) as T;
}

/**
 * RealStreamsApiClient talks to the same-origin /api/streams/* proxy routes in
 * local/cloud, and DIRECTLY to the local agent's native /api/stream-jobs/* routes
 * in hosted mode (native transport: proxy paths remapped, agent origin prefixed,
 * X-FragForge-Token attached per request).
 *
 * HOSTED CAVEAT: sourceUrl()/videoUrl() return ABSOLUTE agent URLs in hosted
 * mode. A bare `<video src>`/`<a download>` cannot carry the token header, so the
 * UI must fetch those bytes WITH the token into a Blob/object URL (see
 * lib/agent/media). In local/cloud they are same-origin relative paths as before.
 */
export class RealStreamsApiClient implements StreamsApiClient {
  private readonly baseUrl: string;
  private readonly headers: () => Record<string, string>;
  private readonly native: boolean;

  constructor(transport: RealStreamsTransport = {}) {
    this.baseUrl = (transport.baseUrl ?? '').replace(/\/+$/, '');
    this.headers = transport.headers ?? (() => ({}));
    this.native = transport.native ?? false;
  }

  /** Remaps the `/api/streams/*` proxy path to the URL to actually fetch. */
  private mapPath(path: string): string {
    if (!this.native) return path;
    if (path === '/api/streams') return `${this.baseUrl}/api/stream-jobs`;
    if (path.startsWith('/api/streams/')) {
      return `${this.baseUrl}/api/stream-jobs/${path.slice('/api/streams/'.length)}`;
    }
    return `${this.baseUrl}${path}`;
  }

  /** The single fetch seam: remaps the path (native) and merges the token header. */
  private req(path: string, init?: RequestInit): Promise<Response> {
    const url = this.mapPath(path);
    if (!this.native) return fetch(url, init);
    const headers = { ...((init?.headers as Record<string, string> | undefined) ?? {}), ...this.headers() };
    return fetch(url, { ...init, headers });
  }

  async createFromUrl(input: { sourceUrl: string; title?: string }): Promise<StreamJob> {
    return readJson<StreamJob>(
      await this.req('/api/streams', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ source_url: input.sourceUrl, title: input.title }),
      }),
    );
  }

  async createFromFile(file: File, title?: string): Promise<StreamJob> {
    const form = new FormData();
    // The proxy renames the browser field `file` to the orchestrator's `video`;
    // when we hit the agent natively we must send `video` directly.
    form.append(this.native ? 'video' : 'file', file, file.name);
    if (title) form.append('title', title);
    return readJson<StreamJob>(await this.req('/api/streams', { method: 'POST', body: form }));
  }

  async listJobs(): Promise<StreamJob[]> {
    const data = await readJson<{ jobs?: StreamJob[] } | StreamJob[]>(await this.req('/api/streams', { cache: 'no-store' }));
    return Array.isArray(data) ? data : (data.jobs ?? []);
  }

  async getJob(id: string): Promise<StreamJob | null> {
    const res = await this.req(`/api/streams/${id}`, { cache: 'no-store' });
    if (res.status === 404) return null;
    return readJson<StreamJob>(res);
  }

  sourceUrl(id: string): string {
    return this.mapPath(`/api/streams/${id}/source`);
  }

  async getEditPlan(id: string): Promise<StreamEditPlan> {
    return readJson<StreamEditPlan>(await this.req(`/api/streams/${id}/edit-plan`, { cache: 'no-store' }));
  }

  async putEditPlan(id: string, plan: StreamEditPlan): Promise<StreamEditPlan> {
    return readJson<StreamEditPlan>(
      await this.req(`/api/streams/${id}/edit-plan`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(plan),
      }),
    );
  }

  async startRender(id: string, variant: StreamVariant): Promise<StreamRenderState> {
    return readJson<StreamRenderState>(await this.req(`/api/streams/${id}/renders/${variant}`, { method: 'POST' }));
  }

  async getRenderState(id: string, variant: StreamVariant): Promise<StreamRenderState> {
    const res = await this.req(`/api/streams/${id}/renders/${variant}`, { cache: 'no-store' });
    if (res.status === 404) return { status: 'none', videos: [] };
    return readJson<StreamRenderState>(res);
  }

  videoUrl(id: string, variant: StreamVariant, clipId: string): string {
    return this.mapPath(`/api/streams/${id}/renders/${variant}/videos/${clipId}`);
  }
}

/** A locally-served sample clip so the mock editor/preview has something to play. */
const MOCK_SOURCE_URL = '/sample-reel.mp4';
const MOCK_JOB_ID = '99999999-9999-4999-8999-999999999999';

function defaultEditPlan(): StreamEditPlan {
  return {
    schema_version: 1,
    variant: 'streamer-vertical-stack-40-60',
    face_crop: { x: 0.62, y: 0.03, width: 0.34, height: 0.3 },
    gameplay_crop: { x: 0, y: 0, width: 1, height: 1 },
    clips: [{ id: 'clip-1', start_seconds: 0, end_seconds: 20, title: 'Ace clutch' }],
    captions: { enabled: false, language: 'auto' },
    updated_at: new Date().toISOString(),
  };
}

/**
 * In-memory mock so /streams is developable and demoable with no orchestrator:
 * a single fake job that "acquires" for a couple of polls, then serves a static
 * sample frame/video for the facecam picker and preview, and "renders" into the
 * same sample clip per clip range.
 */
export class MockStreamsApiClient implements StreamsApiClient {
  private job: StreamJob = {
    id: MOCK_JOB_ID,
    status: 'acquiring',
    title: 'Mock stream clip',
    created_at: new Date().toISOString(),
  };
  private plan: StreamEditPlan = defaultEditPlan();
  private render: StreamRenderState = { status: 'none', videos: [] };
  private acquirePolls = 0;

  async createFromUrl(input: { sourceUrl: string; title?: string }): Promise<StreamJob> {
    this.job = {
      id: MOCK_JOB_ID,
      status: 'acquiring',
      title: input.title || input.sourceUrl,
      created_at: new Date().toISOString(),
    };
    this.plan = defaultEditPlan();
    this.render = { status: 'none', videos: [] };
    this.acquirePolls = 0;
    return { ...this.job };
  }

  async createFromFile(file: File): Promise<StreamJob> {
    this.job = {
      id: MOCK_JOB_ID,
      status: 'uploaded',
      title: file.name,
      probe: { width: 1920, height: 1080, duration_seconds: 20 },
      created_at: new Date().toISOString(),
    };
    this.plan = defaultEditPlan();
    this.render = { status: 'none', videos: [] };
    return { ...this.job };
  }

  async listJobs(): Promise<StreamJob[]> {
    return [{ ...this.job }];
  }

  async getJob(id: string): Promise<StreamJob | null> {
    if (id !== this.job.id) return null;
    // Simulate acquisition finishing after a couple of polls.
    if (this.job.status === 'acquiring') {
      this.acquirePolls++;
      if (this.acquirePolls >= 2) {
        this.job = { ...this.job, status: 'ready', probe: { width: 1920, height: 1080, duration_seconds: 20 } };
      }
    }
    return { ...this.job, edit_plan: { ...this.plan } };
  }

  sourceUrl(): string {
    return MOCK_SOURCE_URL;
  }

  async getEditPlan(): Promise<StreamEditPlan> {
    return { ...this.plan };
  }

  async putEditPlan(_id: string, plan: StreamEditPlan): Promise<StreamEditPlan> {
    this.plan = { ...plan, updated_at: new Date().toISOString() };
    return { ...this.plan };
  }

  async startRender(_id: string, variant: StreamVariant): Promise<StreamRenderState> {
    this.job = { ...this.job, status: 'rendering' };
    this.render = { status: 'rendering', videos: [] };
    // Resolve "instantly" for the mock so the UI can be exercised without a timer.
    this.render = {
      status: 'rendered',
      videos: this.plan.clips.map((c) => ({ clip_id: c.id, title: c.title, key: c.id, duration_seconds: c.end_seconds - c.start_seconds })),
      warnings: variant === 'streamer-fullframe-nocam' ? [] : ['facecam crop is a design-time approximation in mock mode'],
    };
    this.job = { ...this.job, status: 'rendered' };
    return { ...this.render };
  }

  async getRenderState(): Promise<StreamRenderState> {
    return { ...this.render };
  }

  videoUrl(): string {
    return MOCK_SOURCE_URL;
  }
}

function selectStreamsApi(): StreamsApiClient {
  if (isHostedMode()) {
    return new RealStreamsApiClient({ baseUrl: agentBaseUrl(), headers: agentHeaders, native: true });
  }
  if (process.env.NEXT_PUBLIC_API_BASE || isLocalMode()) return new RealStreamsApiClient();
  return new MockStreamsApiClient();
}

export const streamsApi: StreamsApiClient = selectStreamsApi();

export { SERVICE_UNAVAILABLE_CODE };
