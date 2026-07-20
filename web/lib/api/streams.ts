import { SERVICE_UNAVAILABLE_CODE } from './types.ts';

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
  audio_codec?: string;
};

/** A crop rectangle normalized to 0..1 of the source frame. */
export type NormalizedRect = { x: number; y: number; width: number; height: number };

export type StreamVariant = 'streamer-vertical-stack-40-60' | 'streamer-vertical-stack' | 'streamer-fullframe-nocam';

export const STREAM_VARIANTS: { value: StreamVariant; label: string; subtitle: string; needsFaceCrop: boolean }[] = [
  { value: 'streamer-vertical-stack-40-60', label: 'Facecam 40', subtitle: 'Gameplay 60', needsFaceCrop: true },
  { value: 'streamer-vertical-stack', label: 'Stack', subtitle: 'Cam / juego / chat', needsFaceCrop: true },
  { value: 'streamer-fullframe-nocam', label: 'Full-frame', subtitle: 'Sin facecam', needsFaceCrop: false },
];

/** The two CS2 team sides a kill notice can belong to. */
export const KILLFEED_SIDES = ['CT', 'T'] as const;
export type KillfeedSide = (typeof KILLFEED_SIDES)[number];

/**
 * One confirmed kill notice, mirroring streamclips.KillfeedKill (snake_case
 * JSON). It is either read from the cue frame by the xAI vision reader or
 * entered by hand in the editor, then rendered as a synthetic notice. `weapon`
 * is a catalog key served by the weapons endpoint.
 */
export type KillfeedKill = {
  attacker_side: KillfeedSide;
  attacker_name: string;
  victim_side: KillfeedSide;
  victim_name: string;
  assister_side?: KillfeedSide;
  assister_name?: string;
  weapon: string;
  headshot?: boolean;
  wallbang?: boolean;
  noscope?: boolean;
  smoke?: boolean;
  blind?: boolean;
  in_air?: boolean;
  flash_assist?: boolean;
};

export type KillfeedReadEvent = {
  cue_seconds: number;
  kills: KillfeedKill[];
};

export type KillfeedReadResult = {
  kills: KillfeedKill[];
  cue_seconds: number;
  aligned: boolean;
  events: KillfeedReadEvent[];
  warnings?: string[];
  review_required?: boolean;
};

/** Identifies one immutable source-PTS event captured by automatic analysis. */
export type KillfeedReadEventReference = {
  eventId: string;
  generationId: string;
};

export const KILLFEED_ANALYSIS_STATUS = {
  none: 'none',
  queued: 'queued',
  analyzing: 'analyzing',
  reviewRequired: 'review_required',
  ready: 'ready',
  applied: 'applied',
  failed: 'failed',
} as const;
export type KillfeedAnalysisStatus =
  (typeof KILLFEED_ANALYSIS_STATUS)[keyof typeof KILLFEED_ANALYSIS_STATUS];

export type KillfeedTimeBase = { num: number; den: number };

export type KillfeedRowEvidence = {
  onset_row_index: number;
  sample_row_index: number;
  fingerprint: string;
  onset_bounds: { x: number; y: number; width: number; height: number };
  sample_bounds: { x: number; y: number; width: number; height: number };
};

/** One source-frame-aligned killfeed event produced by durable analysis. */
export type KillfeedAnalysisEvent = {
  event_id: string;
  source_pts: number;
  time_base: KillfeedTimeBase;
  cue_seconds: number;
  onset_start_pts: number;
  onset_end_pts: number;
  sample_pts: number;
  sample_seconds: number;
  mode: 'aligned_frame' | 'burst' | 'unresolved';
  rows: KillfeedRowEvidence[];
  kills: KillfeedKill[];
  warnings?: string[];
  error?: string;
};

export type KillfeedAnalysisClip = {
  clip_id: string;
  start_seconds: number;
  end_seconds: number;
  events: KillfeedAnalysisEvent[];
  warnings?: string[];
  error?: string;
};

export type KillfeedAnalysisState = {
  job_id: string;
  generation_id: string;
  status: KillfeedAnalysisStatus;
  source_sha256?: string;
  killfeed_crop?: NormalizedRect;
  fingerprint?: string;
  clips: KillfeedAnalysisClip[] | null;
  warnings?: string[];
  error?: string;
  updated_at: string;
};

/**
 * One burned-in text line, mirroring streamclips.TextOverlay. Times are
 * relative to the clip start in source seconds; missing bounds extend to the
 * clip edges. `position_y` is the normalized vertical center (0.025..0.975).
 */
export type StreamTextOverlay = {
  text: string;
  position_y: number;
  start_seconds?: number;
  end_seconds?: number;
  /** Output pixels, 24..120; omitted means the default 64. */
  font_size?: number;
};

/**
 * Optional per-clip edit options, mirroring streamclips.ClipEdit. An absent
 * object renders the clip untouched. `speed` is the playback rate (0.25..3),
 * `source_volume` scales the original audio (0 mutes, up to 2), and the fades
 * are measured in output (post-speed) seconds.
 */
export type StreamClipEdit = {
  speed?: number;
  source_volume?: number;
  fade_in_seconds?: number;
  fade_out_seconds?: number;
  text_overlays?: StreamTextOverlay[];
};

/** One reviewed or machine-generated Spanish word cue, relative to its clip. */
export type StreamCaptionWord = {
  word: string;
  start_seconds: number;
  end_seconds: number;
};

export type StreamClipRange = {
  id: string;
  start_seconds: number;
  end_seconds: number;
  title?: string;
  killfeed_seconds?: number[];
  /**
   * Per-cue confirmed kills, index-aligned with `killfeed_seconds`. A cue with
   * an empty or missing entry keeps the frozen-crop behavior; a cue with kills
   * renders synthetic notices instead.
   */
  killfeed_kills?: KillfeedKill[][];
  /** Reviewed Spanish cues. Candidate cues live outside the render plan. */
  caption_words?: StreamCaptionWord[];
  /** True only after a person has approved the words or confirmed no speech. */
  caption_reviewed?: boolean;
  edit?: StreamClipEdit;
};

export type StreamCaptions = { enabled: boolean; language: string };

/** A music catalog track mixed under the clip audio; empty key means none. */
export type StreamMusic = { key?: string; volume?: number };

/** Light post effects; grade is the mild viral contrast/saturation lift. */
export type StreamEffects = { grade?: boolean };

/** Optional branded strip rendered over the stream output. */
export type StreamerBanner = { nick?: string; position_y?: number; slide_enabled?: boolean };

export type StreamEditPlan = {
  schema_version: string;
  variant: StreamVariant;
  face_crop?: NormalizedRect;
  /** Explicit human confirmation; default coordinates are never assumed to contain a face. */
  face_crop_reviewed?: boolean;
  killfeed_crop?: NormalizedRect;
  killfeed_analysis?: {
    generation_id: string;
    fingerprint: string;
    applied_at: string;
  };
  gameplay_crop?: NormalizedRect;
  clips: StreamClipRange[];
  streamer_banner?: StreamerBanner;
  captions?: StreamCaptions;
  music?: StreamMusic;
  effects?: StreamEffects;
  updated_at?: string;
};

export type StreamJob = {
  id: string;
  status: StreamJobStatus;
  title?: string;
  source_url?: string;
  probe?: StreamProbe;
  edit_plan?: StreamEditPlan;
  failure_reason?: string;
  created_at: string;
  updated_at?: string;
};

export type StreamRenderVideo = { clip_id: string; title?: string; key: string; duration_seconds?: number };
export type StreamRenderStatus = 'queued' | 'rendering' | 'rendered' | 'failed' | 'none';
export const STREAM_RENDER_ERROR_CODE = {
  killfeedArtifactsStale: 'killfeed_artifacts_stale',
  superseded: 'render_superseded',
} as const;
export type StreamRenderErrorCode =
  (typeof STREAM_RENDER_ERROR_CODE)[keyof typeof STREAM_RENDER_ERROR_CODE];
export type StreamRenderState = {
  status: StreamRenderStatus;
  videos: StreamRenderVideo[];
  published?: boolean;
  warnings?: string[];
  error?: string;
  error_code?: StreamRenderErrorCode | string;
  delivery?: { name: string; kind: string; key: string }[];
};

export const CAPTION_GENERATION_STATUS = {
  none: 'none',
  queued: 'queued',
  generating: 'generating',
  reviewRequired: 'review_required',
  ready: 'ready',
  failed: 'failed',
} as const;
export type CaptionGenerationStatus =
  (typeof CAPTION_GENERATION_STATUS)[keyof typeof CAPTION_GENERATION_STATUS];

export const CAPTION_CLIP_STATUS = {
  reviewRequired: 'review_required',
  noSpeech: 'no_speech',
  ready: 'ready',
  failed: 'failed',
} as const;
export type CaptionCandidateClipStatus =
  (typeof CAPTION_CLIP_STATUS)[keyof typeof CAPTION_CLIP_STATUS];

/** Durable, non-renderable caption candidates returned by the analysis job. */
export type CaptionCandidateClip = {
  clip_id: string;
  start_seconds: number;
  end_seconds: number;
  fingerprint: string;
  status: CaptionCandidateClipStatus;
  candidate_words?: StreamCaptionWord[];
  source_words?: StreamCaptionWord[];
  provider?: string;
  stt_model?: string;
  translation_model?: string;
  error?: string;
};

export type CaptionGenerationState = {
  job_id: string;
  generation_id: string;
  status: CaptionGenerationStatus;
  /** Older queued artifacts may encode the not-yet-populated slice as null. */
  clips: CaptionCandidateClip[] | null;
  warnings?: string[];
  error?: string;
  updated_at: string;
};

export type CaptionReviewDecision = {
  clip_id: string;
  words: StreamCaptionWord[];
  no_speech?: boolean;
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
  /** Starts durable speech analysis; generated words cannot render before review. */
  startCaptionGeneration(id: string): Promise<CaptionGenerationState>;
  getCaptionGenerationState(id: string): Promise<CaptionGenerationState>;
  reviewCaptionCandidates(id: string, generationId: string, clips: CaptionReviewDecision[]): Promise<StreamEditPlan>;
  startRender(id: string, variant: StreamVariant): Promise<StreamRenderState>;
  getRenderState(id: string, variant: StreamVariant): Promise<StreamRenderState>;
  /** Same-origin URL for a <video>/download link to a rendered Short. */
  videoUrl(id: string, variant: StreamVariant, clipId: string): string;
  deliveryUrl(id: string, variant: StreamVariant, name: string): string;
  /** The weapon catalog keys a kill notice may use. */
  listKillfeedWeapons(): Promise<string[]>;
  /** Renders one kill notice to the exact synthetic PNG the render uses. */
  previewKillfeedNotice(kill: KillfeedKill): Promise<Blob>;
  /** Reads one exact automatic event, or uses legacy alignment for a manual cue. */
  readKillfeed(
    id: string,
    clipId: string,
    cueSeconds: number,
    event?: KillfeedReadEventReference,
  ): Promise<KillfeedReadResult>;
  /** Starts automatic analysis of every selected clip on the source-frame timeline. */
  startKillfeedAnalysis(id: string): Promise<KillfeedAnalysisState>;
  getKillfeedAnalysisState(id: string): Promise<KillfeedAnalysisState>;
  /** Atomically copies a current, ready generation into the edit plan. */
  applyKillfeedAnalysis(id: string, generationId: string): Promise<StreamEditPlan>;
}

/** Throws an Error (carrying any upstream `code`) for a non-2xx response. */
async function throwResponseError(res: Response): Promise<never> {
  const body = (await res.json().catch(() => null)) as { error?: unknown; code?: unknown } | null;
  const message = body && typeof body.error === 'string' ? body.error : `request failed (${res.status})`;
  const err = new Error(message) as Error & { code?: string };
  if (body && typeof body.code === 'string') err.code = body.code;
  throw err;
}

async function readJson<T>(res: Response): Promise<T> {
  if (!res.ok) await throwResponseError(res);
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
    form.append('video', file, file.name);
    if (title) form.append('config', JSON.stringify({ title }));
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

  async startCaptionGeneration(id: string): Promise<CaptionGenerationState> {
    return readJson<CaptionGenerationState>(
      await fetch(`/api/streams/${id}/captions`, { method: 'POST' }),
    );
  }

  async getCaptionGenerationState(id: string): Promise<CaptionGenerationState> {
    return readJson<CaptionGenerationState>(
      await fetch(`/api/streams/${id}/captions`, { cache: 'no-store' }),
    );
  }

  async reviewCaptionCandidates(
    id: string,
    generationId: string,
    clips: CaptionReviewDecision[],
  ): Promise<StreamEditPlan> {
    return readJson<StreamEditPlan>(
      await fetch(`/api/streams/${id}/captions/review`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ generation_id: generationId, clips }),
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

  deliveryUrl(id: string, variant: StreamVariant, name: string): string {
    return `/api/streams/${id}/renders/${variant}/delivery/${encodeURIComponent(name)}`;
  }

  async listKillfeedWeapons(): Promise<string[]> {
    const data = await readJson<{ weapons?: string[] }>(
      await fetch('/api/streams/killfeed/weapons', { cache: 'no-store' }),
    );
    return data.weapons ?? [];
  }

  async previewKillfeedNotice(kill: KillfeedKill): Promise<Blob> {
    const res = await fetch('/api/streams/killfeed/notice-preview', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(kill),
    });
    if (!res.ok) await throwResponseError(res);
    return res.blob();
  }

  async readKillfeed(
    id: string,
    clipId: string,
    cueSeconds: number,
    event?: KillfeedReadEventReference,
  ): Promise<KillfeedReadResult> {
    const data = await readJson<{
      kills?: KillfeedKill[];
      cue_seconds?: number;
      aligned?: boolean;
      events?: KillfeedReadEvent[];
    }>(
      await fetch(`/api/streams/${id}/killfeed-read`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          clip_id: clipId,
          cue_seconds: cueSeconds,
          ...(event ? { event_id: event.eventId, generation_id: event.generationId } : {}),
        }),
      }),
    );
    const kills = data.kills ?? [];
    const alignedCue = data.cue_seconds ?? cueSeconds;
    return {
      kills,
      cue_seconds: alignedCue,
      aligned: data.aligned ?? false,
      events: data.events ?? [{ cue_seconds: alignedCue, kills }],
    };
  }

  async startKillfeedAnalysis(id: string): Promise<KillfeedAnalysisState> {
    return readJson<KillfeedAnalysisState>(
      await fetch(`/api/streams/${id}/killfeed`, { method: 'POST' }),
    );
  }

  async getKillfeedAnalysisState(id: string): Promise<KillfeedAnalysisState> {
    return readJson<KillfeedAnalysisState>(
      await fetch(`/api/streams/${id}/killfeed`, { cache: 'no-store' }),
    );
  }

  async applyKillfeedAnalysis(id: string, generationId: string): Promise<StreamEditPlan> {
    return readJson<StreamEditPlan>(
      await fetch(`/api/streams/${id}/killfeed/apply`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ generation_id: generationId }),
      }),
    );
  }
}

export const streamsApi: StreamsApiClient = new RealStreamsApiClient();

export { SERVICE_UNAVAILABLE_CODE };
