import { SERVICE_UNAVAILABLE_CODE } from './types';

/**
 * Stream Clips: turn a Twitch clip/VOD (or an uploaded MP4) into vertical
 * Shorts with the streamer's facecam stacked over gameplay, with optional
 * burned captions. This client mirrors the shape of RealApiClient in this
 * directory but is kept separate from the demo->reel ApiClient since it talks
 * to an unrelated orchestrator surface (/api/stream-jobs), not /api/jobs.
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

/** RealStreamsApiClient talks to the same-origin /api/streams/* proxy routes. */
export class RealStreamsApiClient implements StreamsApiClient {
  async createFromUrl(input: { sourceUrl: string; title?: string }): Promise<StreamJob> {
    return readJson<StreamJob>(
      await fetch('/api/streams', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ source_url: input.sourceUrl, title: input.title }),
      }),
    );
  }

  async createFromFile(file: File, title?: string): Promise<StreamJob> {
    const form = new FormData();
    form.append('file', file);
    if (title) form.append('title', title);
    return readJson<StreamJob>(await fetch('/api/streams', { method: 'POST', body: form }));
  }

  async listJobs(): Promise<StreamJob[]> {
    const data = await readJson<{ jobs?: StreamJob[] } | StreamJob[]>(await fetch('/api/streams', { cache: 'no-store' }));
    return Array.isArray(data) ? data : (data.jobs ?? []);
  }

  async getJob(id: string): Promise<StreamJob | null> {
    const res = await fetch(`/api/streams/${id}`, { cache: 'no-store' });
    if (res.status === 404) return null;
    return readJson<StreamJob>(res);
  }

  sourceUrl(id: string): string {
    return `/api/streams/${id}/source`;
  }

  async getEditPlan(id: string): Promise<StreamEditPlan> {
    return readJson<StreamEditPlan>(await fetch(`/api/streams/${id}/edit-plan`, { cache: 'no-store' }));
  }

  async putEditPlan(id: string, plan: StreamEditPlan): Promise<StreamEditPlan> {
    return readJson<StreamEditPlan>(
      await fetch(`/api/streams/${id}/edit-plan`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(plan),
      }),
    );
  }

  async startRender(id: string, variant: StreamVariant): Promise<StreamRenderState> {
    return readJson<StreamRenderState>(await fetch(`/api/streams/${id}/renders/${variant}`, { method: 'POST' }));
  }

  async getRenderState(id: string, variant: StreamVariant): Promise<StreamRenderState> {
    const res = await fetch(`/api/streams/${id}/renders/${variant}`, { cache: 'no-store' });
    if (res.status === 404) return { status: 'none', videos: [] };
    return readJson<StreamRenderState>(res);
  }

  videoUrl(id: string, variant: StreamVariant, clipId: string): string {
    return `/api/streams/${id}/renders/${variant}/videos/${clipId}`;
  }
}

export const streamsApi: StreamsApiClient = new RealStreamsApiClient();

export { SERVICE_UNAVAILABLE_CODE };
