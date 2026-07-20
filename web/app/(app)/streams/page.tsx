'use client';

import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import {
  AlertTriangle,
  Captions,
  CircleCheck,
  Download,
  Film,
  Link2,
  Loader2,
  MonitorPlay,
  Pause,
  Play,
  Plus,
  RefreshCw,
  ShieldCheck,
  Sparkles,
  Trash2,
  Twitch,
  UploadCloud,
} from 'lucide-react';
import {
  CAPTION_CLIP_STATUS,
  CAPTION_GENERATION_STATUS,
  KILLFEED_ANALYSIS_STATUS,
  streamsApi,
  STREAM_VARIANTS,
  SERVICE_UNAVAILABLE_CODE,
  type CaptionCandidateClip,
  type CaptionGenerationState,
  type KillfeedKill,
  type KillfeedAnalysisState,
  type NormalizedRect,
  type StreamClipEdit,
  type StreamClipRange,
  type StreamCaptionWord,
  type StreamEditPlan,
  type StreamTextOverlay,
  type StreamJob,
  type StreamRenderState,
  type StreamVariant,
} from '@/lib/api/streams';
import { api } from '@/lib/api';
import type { Song } from '@/lib/api/types';
import { navSection } from '@/lib/nav';
import { cn } from '@/lib/utils';
import { SectionEyebrow } from '@/components/brand/section-eyebrow';
import { StudioPageHeader } from '@/components/studio/page-header';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { CropPicker } from '@/components/streams/crop-picker';
import { StreamPreview } from '@/components/streams/stream-preview';
import { KillfeedKillsEditor } from '@/components/streams/killfeed-kills-editor';
import {
  STREAMER_BANNER_MAX_POSITION,
  STREAMER_BANNER_MIN_POSITION,
  advanceMontagePlayback,
  clampStreamerBannerPosition,
  resolveStreamerBannerPosition,
  representativeFrameTime,
  startMontagePlayback,
} from '@/lib/stream-preview';
import { addClipCue, applyClipKillfeedRead, fitPlanToSourceDuration, initialStreamClipEnd, normalizeKillfeedPlan, removeClipCue, setClipCueKills } from '@/lib/killfeed-plan';
import { CLIP_SPEEDS, clipEditIssue, DEFAULT_OVERLAY_FONT_SIZE, MAX_OVERLAY_FONT_SIZE, MAX_TEXT_OVERLAYS, MIN_OVERLAY_FONT_SIZE, streamRangeIssue, streamRangesIssue } from '@/lib/clip-edit';
import {
  captionDraftDiffersFromReview,
  captionInputsFingerprint,
  captionsNeedReview,
  captionWordsIssue,
  clipHasAudibleSource,
  invalidateCaptionReview,
  streamHasAudio,
} from '@/lib/caption-review';
import {
  appliedKillfeedEventReference,
  invalidateKillfeedAnalysis,
  killfeedAnalysisInputsFingerprint,
  killfeedAnalysisNeeded,
  killfeedManualCueIssue,
  killfeedStateNeedsRefreshForRead,
} from '@/lib/killfeed-analysis';
import {
  canRequestCaptionCandidates,
  streamRenderCanRetry,
  streamRenderNeedsKillfeedReanalysis,
} from '@/lib/stream-recovery';
import { loadStreamDraft, reconcileStreamDraftAfterSave, recoverableStreamJobs, saveStreamDraft, selectStreamDraftPlan, streamEditPlanFingerprint } from '@/lib/stream-draft';
import { isCurrentStreamEditorLoad, nextStreamEditorLoad, type StreamEditorLoad } from '@/lib/stream-editor-load';

const NAV = navSection('/streams');

/** Accent styling shared by every purple range slider in this editor. */
const ACCENT_SLIDER_CLASS = 'accent-[#9146ff] disabled:opacity-50';

/** Upstream code for a killfeed-read blocked by a missing xAI key. */
const XAI_KEY_MISSING_CODE = 'xai_key_missing';

type Stage = 'idle' | 'submitting' | 'acquiring' | 'editing' | 'rendering' | 'rendered' | 'failed';

const FULL_FRAME: NormalizedRect = { x: 0, y: 0, width: 1, height: 1 };
const DEFAULT_FACE_CROP: NormalizedRect = { x: 0.62, y: 0.03, width: 0.34, height: 0.3 };
const DEFAULT_KILLFEED_CROP: NormalizedRect = { x: 0.68, y: 0.04, width: 0.31, height: 0.14 };
const KILLFEED_MIN_CROP_SIZE = 0.02;
const STREAMER_NICK_RE = /^[A-Za-z0-9_]{0,25}$/;
const NO_MUSIC_VALUE = '__none__';

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** True when an API error means the local analysis service is unreachable. */
function isServiceUnavailable(err: unknown): boolean {
  return (err as { code?: string } | null)?.code === SERVICE_UNAVAILABLE_CODE;
}

/** Localized message for a failed API call, preferring the offline hint. */
function errorMessage(err: unknown, fallback: string): string {
  if (isServiceUnavailable(err)) {
    return 'El servicio de Clips de stream está offline. Arráncalo y vuelve a intentarlo.';
  }
  if (err instanceof Error) {
    return err.message;
  }
  return fallback;
}

/**
 * Extensions of URLs that are a direct link to a non-video asset (an image
 * pasted from a clipboard uploader like ShareX, a document, an archive, an
 * audio-only file). yt-dlp cannot turn these into an MP4, so we reject them
 * instantly with a localized message instead of round-tripping to a doomed
 * acquire job. The server guards the same set (vodfetch.ClassifySource) for
 * direct API callers; this is the fast, in-language UX path.
 */
const NON_VIDEO_EXT_RE =
  /\.(png|jpe?g|gif|webp|bmp|svg|ico|tiff?|heic|avif|pdf|txt|md|csv|json|xml|html?|zip|rar|7z|gz|tar|mp3|wav|flac|ogg|m4a|docx?|xlsx?)$/i;

function isStreamURLValidationError(message: string | null): message is string {
  return message?.startsWith('Pega una URL') === true || message?.startsWith('Esa URL') === true;
}

/** The extension (without the dot, lowercased) if `raw` clearly points to a
 * non-video file, else null. Unparseable input is left for the server. */
function nonVideoExtension(raw: string): string | null {
  try {
    const match = new URL(raw).pathname.match(NON_VIDEO_EXT_RE);
    return match ? match[1].toLowerCase() : null;
  } catch {
    return null;
  }
}

function streamSourceLabel(sourceUrl?: string): string | null {
  if (!sourceUrl) return null;
  try {
    const url = new URL(sourceUrl);
    if (url.hostname.endsWith('twitch.tv')) {
      const parts = url.pathname.split('/').filter(Boolean);
      const channel = parts.length === 3 && parts[1] === 'clip' ? parts[0] : null;
      return channel ? `Twitch · ${channel}` : 'Twitch';
    }
    if (url.hostname.endsWith('youtube.com') || url.hostname === 'youtu.be') return 'YouTube';
    return url.hostname;
  } catch {
    return null;
  }
}

let clipSeq = 0;
function nextClipId(): string {
  clipSeq += 1;
  return `clip-${Date.now()}-${clipSeq}`;
}

function blankPlan(
  durationSeconds = 0,
  variant: StreamVariant = 'streamer-vertical-stack-40-60',
): StreamEditPlan {
  const clipEnd = initialStreamClipEnd(durationSeconds);
  return {
    schema_version: '1.1',
    variant,
    face_crop: DEFAULT_FACE_CROP,
    face_crop_reviewed: false,
    gameplay_crop: FULL_FRAME,
    clips: [{ id: nextClipId(), start_seconds: 0, end_seconds: clipEnd, title: '' }],
    captions: { enabled: false, language: 'es' },
  };
}

/** True once every clip range in the plan is well-formed (end strictly after start). */
function clipsAreValid(clips: StreamClipRange[]): boolean {
  return clips.length > 0 && clips.every((c) => Number.isFinite(c.start_seconds) && Number.isFinite(c.end_seconds) && c.end_seconds > c.start_seconds);
}

function formatStreamTimestamp(seconds: number): string {
  const safeSeconds = Number.isFinite(seconds) ? Math.max(0, seconds) : 0;
  const minutes = Math.floor(safeSeconds / 60);
  const remainder = safeSeconds - minutes * 60;
  return `${minutes}:${remainder.toFixed(2).padStart(5, '0')}`;
}

/**
 * Canonical fingerprint of everything a render consumes from the plan, so the
 * UI can tell whether the shown Shorts still match the current edits. Fields
 * are listed explicitly (not JSON.stringify of the object) so key order and
 * volatile fields like updated_at can never cause a false mismatch.
 */
function planFingerprint(plan: StreamEditPlan): string {
  const rect = (r?: NormalizedRect) => (r ? [r.x, r.y, r.width, r.height] : null);
  const overlay = (o: StreamTextOverlay) => [o.text, o.position_y, o.start_seconds ?? null, o.end_seconds ?? null, o.font_size ?? DEFAULT_OVERLAY_FONT_SIZE];
  // Defaults collapse an absent edit and an all-defaults edit to the same key.
  const edit = (e?: StreamClipEdit) => [
    e?.speed ?? 1,
    e?.source_volume ?? 1,
    e?.fade_in_seconds ?? 0,
    e?.fade_out_seconds ?? 0,
    (e?.text_overlays ?? []).map(overlay),
  ];
  return JSON.stringify({
    variant: plan.variant,
    face: rect(plan.face_crop),
    faceReviewed: plan.face_crop_reviewed ?? false,
    killfeed: rect(plan.killfeed_crop),
    killfeedAnalysis: [
      plan.killfeed_analysis?.generation_id ?? '',
      plan.killfeed_analysis?.fingerprint ?? '',
    ],
    game: rect(plan.gameplay_crop),
    clips: plan.clips.map((c) => [
      c.id,
      c.start_seconds,
      c.end_seconds,
      c.title ?? '',
      c.killfeed_seconds ?? [],
      c.killfeed_kills ?? [],
      (c.caption_words ?? []).map((word) => [word.word, word.start_seconds, word.end_seconds]),
      c.caption_reviewed ?? false,
      edit(c.edit),
    ]),
    streamerNick: plan.streamer_banner?.nick?.trim() ?? '',
    streamerPosition: plan.streamer_banner?.position_y ?? null,
    streamerSlide: plan.streamer_banner?.slide_enabled ?? false,
    captions: [plan.captions?.enabled ?? false, 'es'],
    music: [plan.music?.key ?? '', plan.music?.volume ?? 0],
    grade: plan.effects?.grade ?? false,
  });
}

function captionGenerationIsPending(state: CaptionGenerationState | null): boolean {
  return state?.status === CAPTION_GENERATION_STATUS.queued || state?.status === CAPTION_GENERATION_STATUS.generating;
}

function killfeedAnalysisIsPending(state: KillfeedAnalysisState | null): boolean {
  return state?.status === KILLFEED_ANALYSIS_STATUS.queued || state?.status === KILLFEED_ANALYSIS_STATUS.analyzing;
}

function captionDraftsFromState(state: CaptionGenerationState): Record<string, StreamCaptionWord[]> {
  return Object.fromEntries(
    (state.clips ?? []).map((clip) => [
      clip.clip_id,
      (clip.candidate_words ?? []).map((word) => ({ ...word })),
    ]),
  );
}

function AnalysisProgress({ label, onCancel }: { label: string; onCancel: () => void }) {
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    const started = Date.now();
    const timer = setInterval(() => setElapsed(Math.floor((Date.now() - started) / 1000)), 1000);
    return () => clearInterval(timer);
  }, []);
  return (
    <div role="status" className="flex flex-wrap items-center gap-2 text-xs text-stream">
      <Loader2 className="size-4 animate-spin" />
      <span>{label} · {elapsed}s transcurridos · tiempo restante ajustándose</span>
      <Button type="button" variant="ghost" size="sm" onClick={onCancel}>CANCELAR ESPERA</Button>
    </div>
  );
}

/**
 * Stream Clips (/streams) — paste a Twitch clip/VOD URL or upload an MP4, then
 * lay out the facecam over gameplay and cut clip ranges before rendering
 * vertical Shorts. Mirrors /upload's stage machine (submit → wait → edit) but
 * against the /api/streams/* proxy, which forwards to the orchestrator's
 * stream-jobs pipeline (acquire/probe → edit plan → render).
 */
export default function StreamsPage() {
  return <LocalStreamsPage />;
}

function LocalStreamsPage() {
  const [stage, setStage] = useState<Stage>('idle');
  const [job, setJob] = useState<StreamJob | null>(null);
  const [plan, setPlan] = useState<StreamEditPlan | null>(null);
  const [renderState, setRenderState] = useState<StreamRenderState | null>(null);
  /** The exact plan the shown render used; drives URLs and staleness. */
  const [renderedPlan, setRenderedPlan] = useState<StreamEditPlan | null>(null);
  const [sourceUrl, setSourceUrl] = useState('');
  const [title, setTitle] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [failureReason, setFailureReason] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [recoverableJobs, setRecoverableJobs] = useState<StreamJob[]>([]);
  const autosaveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const autosaveGeneration = useRef(0);
  const autosaveChain = useRef<Promise<void>>(Promise.resolve());
  const serverPlanFingerprint = useRef<{ jobId: string; fingerprint: string } | null>(null);
  const draftSessionId = useRef('');
  const draftRevision = useRef(0);
  const editorLoad = useRef<StreamEditorLoad>({ generation: 0, jobId: '' });

  const pollGen = useRef(0);

  const reset = useCallback((message: string) => {
    pollGen.current += 1;
    editorLoad.current = nextStreamEditorLoad(editorLoad.current, '');
    setError(message);
    setStage('idle');
    setJob(null);
    setPlan(null);
    setRenderState(null);
    setRenderedPlan(null);
    setFailureReason(null);
    serverPlanFingerprint.current = null;
  }, []);

  const loadEditor = useCallback(async (j: StreamJob) => {
    const requestedLoad = nextStreamEditorLoad(editorLoad.current, j.id);
    editorLoad.current = requestedLoad;
    draftSessionId.current = window.crypto.randomUUID();
    draftRevision.current = 0;
    setJob(j);
    const duration = j.probe?.duration_seconds ?? 0;
    try {
      const browserDraft = typeof window === 'undefined' ? null : loadStreamDraft(window.localStorage, j.id);
      const serverPlan = j.edit_plan ?? (await streamsApi.getEditPlan(j.id));
      if (!isCurrentStreamEditorLoad(requestedLoad, editorLoad.current)) return;
      serverPlanFingerprint.current = { jobId: j.id, fingerprint: streamEditPlanFingerprint(serverPlan) };
      const loadedPlan = fitPlanToSourceDuration(
        normalizeKillfeedPlan(selectStreamDraftPlan(browserDraft, serverPlan) ?? serverPlan),
        duration,
      );
      if (j.title?.trim() && loadedPlan.clips[0] && !loadedPlan.clips[0].title?.trim()) {
        loadedPlan.clips[0] = { ...loadedPlan.clips[0], title: j.title.trim() };
      }
      setPlan(
        loadedPlan.clips.length > 0
          ? loadedPlan
          : {
              ...loadedPlan,
              clips: [{ id: nextClipId(), start_seconds: 0, end_seconds: initialStreamClipEnd(duration), title: '' }],
            },
      );
    } catch {
      if (!isCurrentStreamEditorLoad(requestedLoad, editorLoad.current)) return;
      setPlan(blankPlan(duration));
    }
    if (!isCurrentStreamEditorLoad(requestedLoad, editorLoad.current)) return;
    setStage('editing');
  }, []);

  const pollAcquiring = useCallback(
    async (jobId: string) => {
      const gen = ++pollGen.current;
      for (let attempt = 0; attempt < 200; attempt++) {
        await sleep(1200);
        if (pollGen.current !== gen) return; // superseded by a new submission/reset
        try {
          const j = await streamsApi.getJob(jobId);
          if (!j) {
            reset('Ese trabajo ya no está disponible.');
            return;
          }
          if (j.status === 'failed') {
            setJob(j);
            setFailureReason(j.failure_reason || 'no se pudo obtener el vídeo de origen');
            setStage('failed');
            return;
          }
          if (j.status !== 'acquiring') {
            void loadEditor(j);
            return;
          }
        } catch (err) {
          if (isServiceUnavailable(err)) {
            reset('El servicio de Clips de stream está offline. Arráncalo y vuelve a intentarlo.');
            return;
          }
          // transient network hiccup; keep polling
        }
      }
      reset('Se agotó el tiempo esperando a que el vídeo de origen estuviera listo.');
    },
    [loadEditor, reset],
  );

  const resumeJob = useCallback((candidate: StreamJob) => {
    setError(null);
    setJob(candidate);
    if (candidate.status === 'acquiring') {
      setStage('acquiring');
      void pollAcquiring(candidate.id);
      return;
    }
    void loadEditor(candidate);
  }, [loadEditor, pollAcquiring]);

  const submitUrl = useCallback(async () => {
    const trimmed = sourceUrl.trim();
    if (!trimmed) {
      setError('Pega una URL de clip o VOD de Twitch o YouTube. Para un archivo local, usa un MP4.');
      return;
    }
    const badExt = nonVideoExtension(trimmed);
    if (badExt) {
      setError(
        `Esa URL apunta a un archivo .${badExt}, no a un vídeo. Pega el enlace de un clip o VOD de Twitch o YouTube, o usa “Subir un MP4”.`,
      );
      return;
    }
    setError(null);
    setStage('submitting');
    try {
      const j = await streamsApi.createFromUrl({ sourceUrl: trimmed, title: title.trim() || undefined });
      if (j.status === 'acquiring') {
        setJob(j);
        setStage('acquiring');
        void pollAcquiring(j.id);
      } else {
        void loadEditor(j);
      }
    } catch (err) {
      reset(errorMessage(err, 'No se pudo iniciar ese trabajo. Revisa la URL y vuelve a intentarlo.'));
    }
  }, [sourceUrl, title, pollAcquiring, loadEditor, reset]);

  const submitFile = useCallback(
    async (file: File) => {
      setError(null);
      setStage('submitting');
      try {
        const j = await streamsApi.createFromFile(file, title.trim() || undefined);
        if (j.status === 'acquiring') {
          setJob(j);
          setStage('acquiring');
          void pollAcquiring(j.id);
        } else {
          void loadEditor(j);
        }
      } catch (err) {
        reset(errorMessage(err, 'No se pudo procesar ese archivo. Prueba con otro MP4.'));
      }
    },
    [title, pollAcquiring, loadEditor, reset],
  );

  const pollRender = useCallback(
    async (jobId: string, variant: StreamVariant, attemptedPlan: StreamEditPlan) => {
      const gen = ++pollGen.current;
      for (let attempt = 0; attempt < 300; attempt++) {
        try {
          const state = await streamsApi.getRenderState(jobId, variant);
          if (pollGen.current !== gen) return;
          setRenderState(state);
          if (state.status === 'rendered') {
            setRenderedPlan(attemptedPlan);
            setStage('rendered');
            return;
          }
          if (state.status === 'failed') {
            if (streamRenderNeedsKillfeedReanalysis(state)) {
              setStage('editing');
              setError(
                state.error ||
                  'Las capturas exactas de la killfeed ya no están disponibles. Reanaliza la killfeed y vuelve a crear los Shorts.',
              );
              return;
            }
            if (streamRenderCanRetry(state)) {
              setStage('editing');
              setError(
                state.error ||
                  (state.published
                    ? 'El nuevo render falló. La última versión publicada sigue disponible; revisa el plan y vuelve a intentarlo.'
                    : 'El plan cambió antes de publicar el render. Revísalo y vuelve a crear los Shorts.'),
              );
              return;
            }
            setStage('failed');
            setFailureReason(state.error || 'el render falló');
            return;
          }
        } catch (err) {
          if (isServiceUnavailable(err)) {
            reset('El servicio de Clips de stream está offline. Arráncalo y vuelve a intentarlo.');
            return;
          }
        }
        await sleep(1500);
        if (pollGen.current !== gen) return;
      }
      setStage('failed');
      setFailureReason('se agotó el tiempo esperando a que terminara el render');
    },
    [reset],
  );

  const clearRecoverableKillfeedRender = useCallback(() => {
    if (!streamRenderNeedsKillfeedReanalysis(renderState)) return;
    setRenderState((current) =>
      current?.published
        ? { ...current, error: undefined, error_code: undefined }
        : null,
    );
    setError(null);
  }, [renderState]);

  const createShorts = useCallback(async () => {
    if (!job || !plan) return;
    const fittedPlan = fitPlanToSourceDuration(plan, job.probe?.duration_seconds ?? 0);
    const rangeIssue = streamRangesIssue(fittedPlan.clips, job.probe?.duration_seconds ?? 0);
    if (rangeIssue !== null) {
      setError(rangeIssue);
      return;
    }
    const needsFaceCrop = STREAM_VARIANTS.find((variant) => variant.value === fittedPlan.variant)?.needsFaceCrop ?? false;
    if (needsFaceCrop && fittedPlan.face_crop_reviewed !== true) {
      setError('Confirma manualmente el recorte de facecam antes de renderizar; no asumimos que el recorte automático contenga una cara.');
      return;
    }
    if (!STREAMER_NICK_RE.test(fittedPlan.streamer_banner?.nick?.trim() ?? '')) {
      setError('El nick debe tener hasta 25 letras, números o guiones bajos.');
      return;
    }
    const editIssue = clipEditIssue(fittedPlan.clips);
    if (editIssue !== null) {
      setError(editIssue);
      return;
    }
    const sourceHasAudio = streamHasAudio(job.probe);
    if (captionsNeedReview(fittedPlan, sourceHasAudio)) {
      setError('Revisa los subtítulos de cada clip con audio antes de crear los Shorts.');
      return;
    }
    if (killfeedAnalysisNeeded(fittedPlan)) {
      setError('Espera a que termine el análisis automático de la killfeed antes de crear los Shorts.');
      return;
    }
    setError(null);
    setSaving(true);
    try {
      autosaveGeneration.current += 1;
      if (autosaveTimer.current !== null) {
        clearTimeout(autosaveTimer.current);
        autosaveTimer.current = null;
      }
      await autosaveChain.current.catch(() => undefined);
      const submittedRevision = { editorSessionId: draftSessionId.current, revision: draftRevision.current };
      const saved = await streamsApi.putEditPlan(job.id, fittedPlan);
      if (typeof window !== 'undefined') reconcileStreamDraftAfterSave(window.localStorage, job.id, fittedPlan, saved, submittedRevision);
      serverPlanFingerprint.current = { jobId: job.id, fingerprint: streamEditPlanFingerprint(saved) };
      setPlan(saved);
      setStage('rendering');
      setRenderState((previous) =>
        previous && (previous.published || previous.status === 'rendered')
          ? { ...previous, published: true, status: 'queued' }
          : { status: 'queued', videos: [] },
      );
      await streamsApi.startRender(job.id, saved.variant);
      void pollRender(job.id, saved.variant, saved);
    } catch (err) {
      setStage('editing');
      setError(errorMessage(err, 'No se pudo iniciar el render.'));
    } finally {
      setSaving(false);
    }
  }, [job, plan, pollRender]);

  useEffect(() => {
    let active = true;
    void streamsApi.listJobs()
      .then((jobs) => {
        if (active) setRecoverableJobs(recoverableStreamJobs(jobs).slice(0, 5));
      })
      .catch(() => {
        // Source creation remains available if the recent-job read fails.
      });
    return () => { active = false; };
  }, []);

  useEffect(() => {
    if (!job || !plan || (stage !== 'editing' && stage !== 'rendered')) return;
    const revision = ++draftRevision.current;
    const submittedRevision = { editorSessionId: draftSessionId.current, revision };
    const requestedLoad = editorLoad.current;
    if (typeof window !== 'undefined') {
      saveStreamDraft(
        window.localStorage,
        job.id,
        plan,
        undefined,
        serverPlanFingerprint.current?.jobId === job.id
          ? serverPlanFingerprint.current.fingerprint
          : streamEditPlanFingerprint(plan),
        submittedRevision,
      );
    }
    const generation = ++autosaveGeneration.current;
    if (autosaveTimer.current !== null) clearTimeout(autosaveTimer.current);
    autosaveTimer.current = setTimeout(() => {
      autosaveTimer.current = null;
      autosaveChain.current = autosaveChain.current
        .catch(() => undefined)
        .then(async () => {
          if (autosaveGeneration.current !== generation) return;
          const saved = await streamsApi.putEditPlan(job.id, plan);
          if (typeof window !== 'undefined') reconcileStreamDraftAfterSave(window.localStorage, job.id, plan, saved, submittedRevision);
          if (isCurrentStreamEditorLoad(requestedLoad, editorLoad.current)) {
            serverPlanFingerprint.current = { jobId: job.id, fingerprint: streamEditPlanFingerprint(saved) };
          }
        });
      void autosaveChain.current.catch(() => {
        // The synchronous local draft still protects navigation/restart recovery.
      });
    }, 500);
    return () => {
      if (autosaveTimer.current !== null) {
        clearTimeout(autosaveTimer.current);
        autosaveTimer.current = null;
      }
    };
  }, [job, plan, stage]);

  useEffect(() => {
    return () => {
      pollGen.current += 1; // stop any in-flight poll loop on unmount
    };
  }, []);

  let stageContent: ReactNode;
  if (stage === 'idle' || stage === 'submitting') {
    stageContent = (
      <SourceCard
        sourceUrl={sourceUrl}
        title={title}
        submitting={stage === 'submitting'}
        error={error}
        recoverableJobs={recoverableJobs}
        onSourceUrlChange={(value) => {
          setSourceUrl(value);
          if (isStreamURLValidationError(error)) setError(null);
        }}
        onTitleChange={setTitle}
        onSubmitUrl={() => void submitUrl()}
        onSubmitFile={(f) => void submitFile(f)}
        onResume={resumeJob}
      />
    );
  } else if (stage === 'acquiring') {
    stageContent = (
      <div role="status" aria-live="polite" className="studio-panel flex max-w-4xl flex-col items-center justify-center gap-4 p-6 py-14 text-center sm:p-8">
        <Loader2 className="size-8 animate-spin text-stream" />
        <div className="flex flex-col gap-1">
          <p className="font-[family-name:var(--font-display)] text-lg font-semibold tracking-tight text-foreground">
            Descargando {job?.title || 'el clip'}…
          </p>
          <p className="text-sm text-muted-foreground">Descargando y analizando el vídeo de origen.</p>
        </div>
      </div>
    );
  } else if (stage === 'failed') {
    stageContent = (
      <div role="alert" className="studio-panel flex max-w-4xl flex-col items-center justify-center gap-4 border-destructive/40 p-6 py-14 text-center sm:p-8">
        <AlertTriangle className="size-8 text-destructive" />
        <div className="flex flex-col gap-1">
          <p className="font-[family-name:var(--font-display)] text-lg font-semibold tracking-tight text-foreground">
            Ese trabajo falló
          </p>
          <p className="max-w-md text-sm text-muted-foreground">{failureReason ?? 'Algo salió mal.'}</p>
        </div>
        <button
          type="button"
          onClick={() => reset('')}
          className="rounded-md bg-primary px-5 py-2.5 font-[family-name:var(--font-display)] text-sm font-bold tracking-[0.06em] text-primary-foreground transition-colors hover:bg-primary/90"
        >
          EMPEZAR DE NUEVO
        </button>
      </div>
    );
  } else if (job && plan) {
    stageContent = (
      <StreamEditor
        job={job}
        plan={plan}
        onPlanChange={setPlan}
        stage={stage as 'editing' | 'rendering' | 'rendered'}
        renderState={renderState}
        renderedPlan={renderedPlan}
        error={error}
        saving={saving}
        onCreate={() => void createShorts()}
        onKillfeedAnalysisRecovered={clearRecoverableKillfeedRender}
        onStartOver={() => reset('')}
      />
    );
  } else {
    stageContent = null;
  }

  return (
    <div className="flex flex-col gap-8">
      <StudioPageHeader
        number={Number(NAV.number)}
        label={NAV.label.toUpperCase()}
        accent="magenta"
        title="DE STREAM A SHORT"
        description={
          <p>
          Pega un clip de Twitch o YouTube, o sube un MP4. Córtalo en vertical con tu facecam y conserva la killfeed original cuando la necesites.
          </p>
        }
      />

      {stageContent}
    </div>
  );
}

function SourceCard({
  sourceUrl,
  title,
  submitting,
  error,
  recoverableJobs,
  onSourceUrlChange,
  onTitleChange,
  onSubmitUrl,
  onSubmitFile,
  onResume,
}: {
  sourceUrl: string;
  title: string;
  submitting: boolean;
  error: string | null;
  recoverableJobs: StreamJob[];
  onSourceUrlChange: (v: string) => void;
  onTitleChange: (v: string) => void;
  onSubmitUrl: () => void;
  onSubmitFile: (file: File) => void;
  onResume: (job: StreamJob) => void;
}) {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const urlError = isStreamURLValidationError(error) ? error : null;

  return (
    <div className="studio-panel studio-panel-raised relative max-w-5xl p-5 sm:p-7">
      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_280px] lg:items-stretch">
        <div>
          <SectionEyebrow label="FUENTE" accent="magenta" />
          <p className="mt-3 max-w-xl text-sm leading-6 text-muted-foreground">
            Importa el vídeo completo. En el siguiente paso eliges encuadre, rangos, música y efectos.
          </p>

          <div className="mt-6 flex flex-col gap-5">
            <div className="flex flex-col gap-2">
              <Label htmlFor="stream-title" className="text-[13px]">Título (opcional)</Label>
              <Input
                id="stream-title"
                placeholder="Clutch 1v5 en pistola"
                value={title}
                disabled={submitting}
                onChange={(e) => onTitleChange(e.target.value)}
              />
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="stream-url" className="text-[13px]">URL de clip o VOD de Twitch o YouTube</Label>
              <div className="flex flex-col gap-3 sm:flex-row">
                <div className="relative flex-1">
                  <Link2 className="pointer-events-none absolute top-1/2 left-3.5 size-4 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    id="stream-url"
                    placeholder="https://clips.twitch.tv/…"
                    value={sourceUrl}
                    disabled={submitting}
                    aria-invalid={urlError !== null || undefined}
                    aria-describedby={urlError ? 'stream-url-error' : undefined}
                    onChange={(e) => onSourceUrlChange(e.target.value)}
                    className="pl-10"
                  />
                </div>
                <Button
                  type="button"
                  onClick={onSubmitUrl}
                  disabled={submitting}
                  className="bg-stream text-stream-foreground shadow-stream/15 hover:bg-stream/90"
                >
                  {submitting ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />}
                  TRAER CLIP
                </Button>
              </div>
              {urlError ? <p id="stream-url-error" role="alert" className="text-sm leading-6 text-destructive">{urlError}</p> : null}
            </div>

            <div className="flex items-center gap-3.5 font-[family-name:var(--font-mono)] text-[11px] tracking-[0.18em] text-muted-foreground">
              <div className="h-px flex-1 bg-border" />
              O USA UN ARCHIVO LOCAL
              <div className="h-px flex-1 bg-border" />
            </div>

            <Button
              type="button"
              variant="outline"
              disabled={submitting}
              onClick={() => fileInputRef.current?.click()}
              className="w-full border-dashed border-stream/35 hover:border-stream/60 hover:bg-stream/8"
            >
              <UploadCloud className="size-4" />
              SUBIR UN MP4
            </Button>
            <input
              ref={fileInputRef}
              type="file"
              accept="video/mp4,.mp4"
              className="hidden"
              onChange={(e) => {
                const file = e.target.files?.[0];
                e.target.value = '';
                if (file) onSubmitFile(file);
              }}
            />

            {error && !urlError ? <p role="alert" className="text-sm leading-6 text-destructive">{error}</p> : null}

            {recoverableJobs.length > 0 ? (
              <section className="flex flex-col gap-2 border-t border-border pt-5" aria-labelledby="stream-drafts-title">
                <h3 id="stream-drafts-title" className="text-sm font-medium text-foreground">Borradores recientes</h3>
                {recoverableJobs.map((candidate) => (
                  <button
                    key={candidate.id}
                    type="button"
                    disabled={submitting}
                    onClick={() => onResume(candidate)}
                    className="flex items-center justify-between gap-3 border border-border px-3 py-2 text-left hover:border-stream/60 hover:bg-stream/5 disabled:opacity-50"
                  >
                    <span className="min-w-0 truncate text-sm">{candidate.title?.trim() || 'Clip sin título'}</span>
                    <span className="shrink-0 font-[family-name:var(--font-mono)] text-[10px] text-stream">CONTINUAR BORRADOR</span>
                  </button>
                ))}
              </section>
            ) : null}
          </div>
        </div>

        <aside className="flex min-h-64 flex-col justify-between border border-stream/20 bg-background/35 p-5">
          <div>
            <div className="flex items-center justify-between gap-3 font-[family-name:var(--font-mono)] text-[11px] uppercase tracking-[0.16em] text-muted-foreground">
              <span>Salida</span>
              <span className="text-stream">9:16 · 1080p</span>
            </div>
            <div className="mx-auto mt-5 grid aspect-[9/16] h-36 place-items-center border border-stream/30 bg-[linear-gradient(180deg,color-mix(in_oklch,var(--stream)_10%,transparent),transparent)]">
              <MonitorPlay className="size-7 text-stream" aria-hidden />
            </div>
          </div>
          <div className="mt-6 space-y-3 text-sm leading-5 text-muted-foreground">
            <p className="flex gap-2.5"><Twitch className="mt-0.5 size-4 shrink-0 text-stream" aria-hidden /> Twitch y YouTube compatibles</p>
            <p className="flex gap-2.5"><ShieldCheck className="mt-0.5 size-4 shrink-0 text-success" aria-hidden /> Procesado en este PC</p>
          </div>
        </aside>
      </div>
    </div>
  );
}

function StreamEditor({
  job,
  plan,
  onPlanChange,
  stage,
  renderState,
  renderedPlan,
  error,
  saving,
  onCreate,
  onKillfeedAnalysisRecovered,
  onStartOver,
}: {
  job: StreamJob;
  plan: StreamEditPlan;
  onPlanChange: (plan: StreamEditPlan) => void;
  stage: 'editing' | 'rendering' | 'rendered';
  renderState: StreamRenderState | null;
  renderedPlan: StreamEditPlan | null;
  error: string | null;
  saving: boolean;
  onCreate: () => void;
  onKillfeedAnalysisRecovered: () => void;
  onStartOver: () => void;
}) {
  const videoSrc = streamsApi.sourceUrl(job.id);
  const sourceLabel = streamSourceLabel(job.source_url);
  const variantMeta = STREAM_VARIANTS.find((v) => v.value === plan.variant) ?? STREAM_VARIANTS[0];
  const stale = useMemo(
    () => renderedPlan !== null && plan !== null && planFingerprint(renderedPlan) !== planFingerprint(plan),
    [renderedPlan, plan],
  );
  const [captionState, setCaptionState] = useState<CaptionGenerationState | null>(null);
  const [captionDrafts, setCaptionDrafts] = useState<Record<string, StreamCaptionWord[]>>({});
  const [captionLoading, setCaptionLoading] = useState(false);
  const [captionRequestBusy, setCaptionRequestBusy] = useState(false);
  const [reviewingClipId, setReviewingClipId] = useState<string | null>(null);
  const [captionError, setCaptionError] = useState<string | null>(null);
  const captionPollGen = useRef(0);
  const [killfeedState, setKillfeedState] = useState<KillfeedAnalysisState | null>(null);
  const [killfeedRequestBusy, setKillfeedRequestBusy] = useState(false);
  const [killfeedError, setKillfeedError] = useState<string | null>(null);
  const killfeedPollGen = useRef(0);
  const killfeedDebounce = useRef<ReturnType<typeof setTimeout> | null>(null);
  const killfeedRunActive = useRef(false);
  const latestPlan = useRef(plan);
  latestPlan.current = plan;
  const [readingCueKey, setReadingCueKey] = useState<string | null>(null);
  const sourceHasAudio = streamHasAudio(job.probe);
  const captionReviewBlocked = captionsNeedReview(plan, sourceHasAudio);
  const captionGenerating = captionGenerationIsPending(captionState);
  const killfeedAnalyzing = killfeedAnalysisIsPending(killfeedState);
  const canGenerateCaptions = canRequestCaptionCandidates(captionReviewBlocked, captionState);
  const busy =
    stage === 'rendering' ||
    saving ||
    captionRequestBusy ||
    captionGenerating ||
    killfeedRequestBusy ||
    killfeedAnalyzing ||
    reviewingClipId !== null ||
    readingCueKey !== null;
  const probedDuration = job.probe?.duration_seconds ?? 0;
  const sourceDuration =
    Number.isFinite(probedDuration) && probedDuration > 0
      ? probedDuration
      : 0;
  const [previewSeconds, setPreviewSeconds] = useState(() =>
    representativeFrameTime(sourceDuration),
  );
  const previewSecondsRef = useRef(previewSeconds);
  previewSecondsRef.current = previewSeconds;
  const [previewPlaying, setPreviewPlaying] = useState(false);
  const previewAudioRef = useRef<HTMLAudioElement>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [previewReload, setPreviewReload] = useState(0);
  const [weapons, setWeapons] = useState<string[]>([]);
  const [readErrors, setReadErrors] = useState<Record<string, string>>({});
  const [killfeedReadNotice, setKillfeedReadNotice] = useState<string | null>(null);
  const killfeedEnabled = plan.killfeed_crop !== undefined;
  const killfeedRenderNeedsReanalysis = streamRenderNeedsKillfeedReanalysis(renderState);
  const killfeedManualCueError = killfeedManualCueIssue(plan, killfeedState);
  const killfeedAnalysisBlocked =
    killfeedEnabled &&
    (killfeedAnalysisNeeded(plan) ||
      killfeedState?.status !== KILLFEED_ANALYSIS_STATUS.applied ||
      killfeedRenderNeedsReanalysis ||
      killfeedManualCueError !== undefined);
  const detectedKillfeedEvents = (killfeedState?.clips ?? []).reduce(
    (total, clip) => total + clip.events.length,
    0,
  );

  useEffect(() => {
    if (!previewPlaying || sourceDuration <= 0) return;
    const audio = previewAudioRef.current;
    let cursor = startMontagePlayback(plan.clips, previewSecondsRef.current);
    if (!cursor) {
      setPreviewPlaying(false);
      return;
    }
    const playCursor = (next: typeof cursor): void => {
      if (!next) return;
      cursor = next;
      setPreviewSeconds(next.sourceSeconds);
      if (audio) {
        audio.currentTime = next.sourceSeconds;
        audio.playbackRate = next.playbackRate;
        audio.volume = Math.min(1, Math.max(0, plan.clips[next.clipIndex]?.edit?.source_volume ?? 1));
        void audio.play().catch(() => {
          setPreviewPlaying(false);
          setPreviewError('El navegador no pudo iniciar el audio de la preview. Pulsa reintentar y vuelve a reproducir.');
        });
      }
    };
    playCursor(cursor);
    const timer = setInterval(() => {
      if (!cursor) return;
      const sourceSeconds = audio && !audio.paused
        ? audio.currentTime
        : previewSecondsRef.current + 0.1 * cursor.playbackRate;
      const next = advanceMontagePlayback(plan.clips, cursor.clipIndex, sourceSeconds);
      if (!next) {
        const restart = startMontagePlayback(plan.clips, Number.NaN);
        if (restart) setPreviewSeconds(restart.sourceSeconds);
        setPreviewPlaying(false);
        return;
      }
      if (next.clipIndex !== cursor.clipIndex) {
        playCursor(next);
        return;
      }
      cursor = next;
      setPreviewSeconds(next.sourceSeconds);
    }, 100);
    return () => {
      clearInterval(timer);
      audio?.pause();
    };
  }, [plan.clips, previewPlaying, sourceDuration]);

  const pollCaptionState = useCallback(async (initial?: CaptionGenerationState): Promise<CaptionGenerationState> => {
    const gen = ++captionPollGen.current;
    let next = initial ?? (await streamsApi.getCaptionGenerationState(job.id));
    if (captionPollGen.current !== gen) return next;
    setCaptionState(next);
    for (let attempt = 0; attempt < 240 && captionGenerationIsPending(next); attempt++) {
      await sleep(1250);
      if (captionPollGen.current !== gen) return next;
      next = await streamsApi.getCaptionGenerationState(job.id);
      if (captionPollGen.current !== gen) return next;
      setCaptionState(next);
    }
    if (captionPollGen.current === gen && captionGenerationIsPending(next)) {
      setCaptionState(null);
      throw new Error('El análisis de subtítulos sigue pendiente. Actualiza el estado o inténtalo de nuevo.');
    }
    if (captionPollGen.current === gen && !captionGenerationIsPending(next)) {
      setCaptionDrafts(captionDraftsFromState(next));
    }
    return next;
  }, [job.id]);

  const pollKillfeedState = useCallback(async (initial?: KillfeedAnalysisState): Promise<KillfeedAnalysisState> => {
    const gen = ++killfeedPollGen.current;
    let next = initial ?? (await streamsApi.getKillfeedAnalysisState(job.id));
    if (killfeedPollGen.current !== gen) return next;
    setKillfeedState(next);
    for (let attempt = 0; attempt < 240 && killfeedAnalysisIsPending(next); attempt++) {
      await sleep(1250);
      if (killfeedPollGen.current !== gen) return next;
      next = await streamsApi.getKillfeedAnalysisState(job.id);
      if (killfeedPollGen.current !== gen) return next;
      setKillfeedState(next);
    }
    if (killfeedPollGen.current !== gen) return next;
    if (killfeedAnalysisIsPending(next)) {
      setKillfeedState(null);
      throw new Error('El análisis de killfeed sigue pendiente. Puedes reintentarlo sin perder el plan.');
    }
    if (next.status === KILLFEED_ANALYSIS_STATUS.failed) {
      throw new Error(next.error || 'El análisis automático de killfeed falló.');
    }
    if (next.status === KILLFEED_ANALYSIS_STATUS.reviewRequired) {
      setKillfeedError('Hay avisos cuyo fotograma de origen no pudo resolverse. Corrígelos o vuelve a analizar.');
      return next;
    }
    if (next.status === KILLFEED_ANALYSIS_STATUS.ready) {
      const appliedPlan = await streamsApi.applyKillfeedAnalysis(job.id, next.generation_id);
      if (killfeedPollGen.current !== gen) return next;
      latestPlan.current = appliedPlan;
      onPlanChange(appliedPlan);
      onKillfeedAnalysisRecovered();
      next = { ...next, status: KILLFEED_ANALYSIS_STATUS.applied };
      setKillfeedState(next);
    }
    return next;
  }, [job.id, onKillfeedAnalysisRecovered, onPlanChange]);

  const runKillfeedAnalysis = useCallback(async (candidatePlan: StreamEditPlan): Promise<void> => {
    if (killfeedRunActive.current || !candidatePlan.killfeed_crop || !clipsAreValid(candidatePlan.clips)) return;
    killfeedRunActive.current = true;
    setKillfeedRequestBusy(true);
    setKillfeedError(null);
    try {
      const saved = await streamsApi.putEditPlan(job.id, candidatePlan);
      latestPlan.current = saved;
      onPlanChange(saved);
      const started = await streamsApi.startKillfeedAnalysis(job.id);
      await pollKillfeedState(started);
    } catch (err) {
      setKillfeedError(errorMessage(err, 'No se pudo analizar automáticamente la killfeed.'));
    } finally {
      killfeedRunActive.current = false;
      setKillfeedRequestBusy(false);
    }
  }, [job.id, onPlanChange, pollKillfeedState]);

  const scheduleKillfeedAnalysis = useCallback((candidatePlan: StreamEditPlan, delay = 750): void => {
    if (killfeedDebounce.current !== null) clearTimeout(killfeedDebounce.current);
    const expectedInputs = killfeedAnalysisInputsFingerprint(candidatePlan);
    latestPlan.current = candidatePlan;
    killfeedDebounce.current = setTimeout(() => {
      killfeedDebounce.current = null;
      const current = latestPlan.current;
      if (
        !current.killfeed_crop ||
        !killfeedAnalysisNeeded(current) ||
        killfeedAnalysisInputsFingerprint(current) !== expectedInputs
      ) return;
      void runKillfeedAnalysis(current);
    }, delay);
  }, [runKillfeedAnalysis]);

  useEffect(() => {
    if (!plan.captions?.enabled) {
      captionPollGen.current += 1;
      setCaptionState(null);
      setCaptionDrafts({});
      setCaptionLoading(false);
      setCaptionError(null);
      return;
    }
    let active = true;
    setCaptionLoading(true);
    void pollCaptionState()
      .catch((err: unknown) => {
        if (active) {
          setCaptionState(null);
          setCaptionError(errorMessage(err, 'No se pudo consultar el análisis de subtítulos.'));
        }
      })
      .finally(() => {
        if (active) setCaptionLoading(false);
      });
    return () => {
      active = false;
      captionPollGen.current += 1;
    };
  }, [job.id, plan.captions?.enabled, pollCaptionState]);

  useEffect(() => {
    setCaptionDrafts((current) => {
      const next = { ...current };
      for (const clip of plan.clips) {
        if (clip.caption_reviewed && next[clip.id] === undefined) {
          next[clip.id] = (clip.caption_words ?? []).map((word) => ({ ...word }));
        }
      }
      return next;
    });
  }, [plan.clips]);

  useEffect(() => {
    if (!killfeedEnabled) {
      killfeedPollGen.current += 1;
      if (killfeedDebounce.current !== null) {
        clearTimeout(killfeedDebounce.current);
        killfeedDebounce.current = null;
      }
      setKillfeedState(null);
      setKillfeedError(null);
      return;
    }
    let active = true;
    void streamsApi.getKillfeedAnalysisState(job.id)
      .then(async (state) => {
        if (!active) return;
        setKillfeedState(state);
        if (killfeedAnalysisIsPending(state) || state.status === KILLFEED_ANALYSIS_STATUS.ready) {
          await pollKillfeedState(state);
          return;
        }
        if (
          (state.status === KILLFEED_ANALYSIS_STATUS.none ||
            state.status === KILLFEED_ANALYSIS_STATUS.applied) &&
          killfeedAnalysisNeeded(latestPlan.current)
        ) {
          scheduleKillfeedAnalysis(latestPlan.current);
        }
      })
      .catch((err: unknown) => {
        if (active) setKillfeedError(errorMessage(err, 'No se pudo consultar el análisis de killfeed.'));
      });
    return () => {
      active = false;
      killfeedPollGen.current += 1;
      if (killfeedDebounce.current !== null) {
        clearTimeout(killfeedDebounce.current);
        killfeedDebounce.current = null;
      }
    };
  }, [job.id, killfeedEnabled, pollKillfeedState, scheduleKillfeedAnalysis]);

  useEffect(() => {
    if (!killfeedEnabled || weapons.length > 0) return;
    let active = true;
    streamsApi
      .listKillfeedWeapons()
      .then((next) => {
        if (active) setWeapons(next);
      })
      .catch(() => {
        // The weapon <select> stays empty; a render still validates server-side.
      });
    return () => {
      active = false;
    };
  }, [killfeedEnabled, weapons.length]);

  const cueKey = (clipId: string, cue: number) => `${clipId}@${cue}`;

  const readCueWithAI = async (clip: StreamClipRange, cue: number): Promise<void> => {
    const key = cueKey(clip.id, cue);
    setReadingCueKey(key);
    setReadErrors((prev) => {
      const { [key]: _removed, ...rest } = prev;
      return rest;
    });
    try {
      // Persist first so the orchestrator can locate this clip/cue for the job;
      // the read endpoint reads the saved plan, not the in-memory edits.
      const saved = await streamsApi.putEditPlan(job.id, plan);
      let resolvedState = killfeedState;
      if (killfeedStateNeedsRefreshForRead(saved, resolvedState)) {
        resolvedState = await streamsApi.getKillfeedAnalysisState(job.id);
        setKillfeedState(resolvedState);
      }
      if (killfeedStateNeedsRefreshForRead(saved, resolvedState)) {
        throw new Error('La generación exacta de killfeed cambió. Recarga el análisis antes de leer esta marca.');
      }
      const eventReference = appliedKillfeedEventReference(saved, resolvedState, clip.id, cue);
      const read = await streamsApi.readKillfeed(job.id, clip.id, cue, eventReference);
      const clips = saved.clips.map((c) =>
        c.id === clip.id ? applyClipKillfeedRead(c, cue, read.events) : c,
      );
      onPlanChange({ ...saved, clips });
      const reviewNote = read.review_required
        ? ` ${read.warnings?.join(' ') || 'Revisa manualmente el resultado antes de renderizar.'}`
        : '';
      if (read.aligned && read.events.length > 0) {
        const newest = read.events[read.events.length - 1];
        setPreviewSeconds(newest.cue_seconds);
        setKillfeedReadNotice(
          `IA ajustó ${read.events.length === 1 ? 'la marca' : `${read.events.length} marcas`} al instante real de ${read.events.length === 1 ? 'la kill' : 'las kills'}.${reviewNote}`,
        );
      } else {
        setKillfeedReadNotice(`No se pudo detectar el borde temporal; se conservó la marca elegida.${reviewNote}`);
      }
    } catch (err) {
      const message =
        (err as { code?: string } | null)?.code === XAI_KEY_MISSING_CODE
          ? 'Configura tu clave de xAI en Ajustes para leer la killfeed con IA.'
          : errorMessage(err, 'No se pudieron leer las kills de esta marca.');
      setReadErrors((prev) => ({ ...prev, [key]: message }));
    } finally {
      setReadingCueKey(null);
    }
  };
  const containingClipIndex = plan.clips.findIndex(
    (clip) =>
      Number.isFinite(clip.start_seconds) &&
      Number.isFinite(clip.end_seconds) &&
      clip.start_seconds <= previewSeconds &&
      previewSeconds < clip.end_seconds,
  );
  const containingClip =
    containingClipIndex >= 0 ? plan.clips[containingClipIndex] : undefined;
  const cueAlreadyExists =
    containingClip?.killfeed_seconds?.includes(previewSeconds) ?? false;
  const canAddKillfeedCue =
    killfeedEnabled &&
    sourceDuration > 0 &&
    containingClip !== undefined &&
    !cueAlreadyExists;
  let killfeedCueStatus = `La marca se añadirá a Clip ${containingClipIndex + 1}, cuyo rango contiene este tiempo.`;
  if (sourceDuration <= 0) {
    killfeedCueStatus = 'La duración del vídeo no está disponible; todavía no se puede añadir una marca.';
  } else if (containingClip === undefined) {
    killfeedCueStatus = `El tiempo ${formatStreamTimestamp(previewSeconds)} queda fuera de todos los rangos de clip. Mueve el cursor o ajusta los rangos.`;
  } else if (cueAlreadyExists) {
    killfeedCueStatus = `Ese tiempo ya está marcado en Clip ${containingClipIndex + 1}.`;
  }

  const setVariant = (variant: StreamVariant) => onPlanChange({ ...plan, variant });
  const setFaceCrop = (rect: NormalizedRect) => onPlanChange({ ...plan, face_crop: rect, face_crop_reviewed: false });
  const confirmFaceCrop = () => onPlanChange({ ...plan, face_crop_reviewed: true });
  const setKillfeedEnabled = (enabled: boolean) => {
    if (enabled) {
      const next = invalidateKillfeedAnalysis({
        ...plan,
        killfeed_crop: plan.killfeed_crop ?? DEFAULT_KILLFEED_CROP,
      });
      latestPlan.current = next;
      onPlanChange(next);
      scheduleKillfeedAnalysis(next);
      return;
    }
    killfeedPollGen.current += 1;
    if (killfeedDebounce.current !== null) {
      clearTimeout(killfeedDebounce.current);
      killfeedDebounce.current = null;
    }
    const withoutKillfeed = invalidateKillfeedAnalysis(plan);
    delete withoutKillfeed.killfeed_crop;
    latestPlan.current = withoutKillfeed;
    onPlanChange(withoutKillfeed);
  };
  const setKillfeedCrop = (rect: NormalizedRect) => {
    const next = invalidateKillfeedAnalysis({ ...plan, killfeed_crop: rect });
    latestPlan.current = next;
    onPlanChange(next);
    scheduleKillfeedAnalysis(next);
  };
  const addKillfeedCue = () => {
    if (!canAddKillfeedCue) return;
    const clips = plan.clips.map((clip, index) =>
      index === containingClipIndex ? addClipCue(clip, previewSeconds) : clip,
    );
    onPlanChange({ ...plan, clips });
  };
  const removeKillfeedCue = (clipId: string, cue: number) => {
    const clips = plan.clips.map((clip) =>
      clip.id === clipId ? removeClipCue(clip, cue) : clip,
    );
    onPlanChange({ ...plan, clips });
  };
  const setCueKills = (clipId: string, cue: number, kills: KillfeedKill[]) => {
    const clips = plan.clips.map((clip) =>
      clip.id === clipId ? setClipCueKills(clip, cue, kills) : clip,
    );
    onPlanChange({ ...plan, clips });
  };
  const bannerPosition = resolveStreamerBannerPosition(plan.variant, plan.streamer_banner?.position_y);
  const setStreamerNick = (nick: string) =>
    onPlanChange({ ...plan, streamer_banner: { ...plan.streamer_banner, nick } });
  const setStreamerPosition = (position: number) =>
    onPlanChange({
      ...plan,
      streamer_banner: { ...plan.streamer_banner, position_y: clampStreamerBannerPosition(position) },
    });
  const resetStreamerPosition = () => {
    const { position_y: _position, ...banner } = plan.streamer_banner ?? {};
    onPlanChange({ ...plan, streamer_banner: banner });
  };
  const setStreamerSlide = (slideEnabled: boolean) =>
    onPlanChange({ ...plan, streamer_banner: { ...plan.streamer_banner, slide_enabled: slideEnabled } });
  const setCaptionsEnabled = (enabled: boolean) =>
    onPlanChange({ ...plan, captions: { enabled, language: 'es' } });
  const setClips = (clips: StreamClipRange[]) => {
    const beforeFingerprint = captionInputsFingerprint(plan.clips);
    const beforeKillfeedFingerprint = killfeedAnalysisInputsFingerprint(plan);
    const clipsWithValidReviews = clips.map((clip) => {
      const previous = plan.clips.find((item) => item.id === clip.id);
      if (!previous || captionInputsFingerprint([previous]) === captionInputsFingerprint([clip])) return clip;
      return invalidateCaptionReview(clip);
    });
    let nextPlan = normalizeKillfeedPlan({ ...plan, clips: clipsWithValidReviews });
    if (beforeFingerprint !== captionInputsFingerprint(nextPlan.clips)) {
      captionPollGen.current += 1;
      setCaptionState(null);
      setCaptionDrafts({});
      if (captionsNeedReview(nextPlan, sourceHasAudio)) {
        setCaptionError('El rango o el audio del clip cambió. Genera candidatos nuevos antes de renderizar.');
      }
    }
    if (killfeedEnabled && beforeKillfeedFingerprint !== killfeedAnalysisInputsFingerprint(nextPlan)) {
      nextPlan = invalidateKillfeedAnalysis(nextPlan);
      scheduleKillfeedAnalysis(nextPlan);
    }
    latestPlan.current = nextPlan;
    onPlanChange(nextPlan);
  };
  const generateCaptionCandidates = async (): Promise<void> => {
    if (!clipsAreValid(plan.clips)) {
      setCaptionError('Corrige los rangos de clip antes de generar subtítulos.');
      return;
    }
    const editIssue = clipEditIssue(plan.clips);
    if (editIssue !== null) {
      setCaptionError(editIssue);
      return;
    }
    setCaptionRequestBusy(true);
    setCaptionError(null);
    setCaptionDrafts({});
    try {
      const saved = await streamsApi.putEditPlan(job.id, plan);
      onPlanChange(saved);
      const started = await streamsApi.startCaptionGeneration(job.id);
      await pollCaptionState(started);
    } catch (err) {
      setCaptionState(null);
      setCaptionError(errorMessage(err, 'No se pudieron generar los candidatos de subtítulos.'));
    } finally {
      setCaptionRequestBusy(false);
    }
  };
  const updateCaptionDraft = (clipId: string, words: StreamCaptionWord[]) => {
    setCaptionDrafts((current) => ({ ...current, [clipId]: words }));
    const clip = plan.clips.find((item) => item.id === clipId);
    if (clip && captionDraftDiffersFromReview(clip, words)) {
      onPlanChange({
        ...plan,
        clips: plan.clips.map((item) => item.id === clipId ? invalidateCaptionReview(item) : item),
      });
    }
  };
  const reviewCaptionClip = async (clip: StreamClipRange, noSpeech: boolean): Promise<void> => {
    if (!captionState || !captionState.generation_id) {
      setCaptionError('Genera candidatos actuales antes de guardar la revisión.');
      return;
    }
    const words = noSpeech
      ? []
      : (captionDrafts[clip.id] ?? []).map((word) => ({ ...word, word: word.word.trim() }));
    if (!noSpeech) {
      const issue = captionWordsIssue(words, clip.end_seconds - clip.start_seconds);
      if (issue !== null) {
        setCaptionError(issue);
        return;
      }
    }
    setReviewingClipId(clip.id);
    setCaptionError(null);
    try {
      const saved = await streamsApi.reviewCaptionCandidates(job.id, captionState.generation_id, [{
        clip_id: clip.id,
        words,
        no_speech: noSpeech || undefined,
      }]);
      onPlanChange(saved);
      setCaptionDrafts((current) => ({ ...current, [clip.id]: words }));
      try {
        setCaptionState(await streamsApi.getCaptionGenerationState(job.id));
      } catch {
        // The reviewed plan is already durable; a later page load can refresh its candidate state.
      }
    } catch (err) {
      setCaptionError(errorMessage(err, 'No se pudo guardar la revisión de este clip.'));
    } finally {
      setReviewingClipId(null);
    }
  };
  const setMusicKey = (key: string) =>
    onPlanChange({ ...plan, music: key ? { key, volume: plan.music?.volume } : {} });
  const setMusicVolume = (volume: number) =>
    onPlanChange({ ...plan, music: { key: plan.music?.key, volume } });
  const setGrade = (grade: boolean) => onPlanChange({ ...plan, effects: { grade } });

  let captionGenerationNotice: ReactNode;
  if (!sourceHasAudio) {
    captionGenerationNotice = (
      <p className="flex items-center gap-2 text-xs text-success">
        <CircleCheck className="size-4" />
        El archivo no tiene pista de audio; no necesita revisión de subtítulos.
      </p>
    );
  } else if (captionGenerating || captionRequestBusy) {
    captionGenerationNotice = (
      <AnalysisProgress
        label="Analizando audio por clip"
        onCancel={() => {
          captionPollGen.current += 1;
          setCaptionRequestBusy(false);
          setCaptionState(null);
          setCaptionError('Espera cancelada. El análisis puede continuar en segundo plano; vuelve a consultar cuando quieras.');
        }}
      />
    );
  } else if (captionLoading) {
    captionGenerationNotice = (
      <p role="status" className="flex items-center gap-2 text-xs text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        Consultando candidatos guardados…
      </p>
    );
  } else if (!captionReviewBlocked) {
    captionGenerationNotice = (
      <p role="status" className="flex items-center gap-2 text-xs text-success">
        <CircleCheck className="size-4" />
        Todos los clips con audio están revisados.
      </p>
    );
  } else if (captionState?.status === CAPTION_GENERATION_STATUS.failed) {
    captionGenerationNotice = (
      <p role="alert" className="flex items-center gap-2 text-xs text-destructive">
        <AlertTriangle className="size-4" />
        {captionState.error || 'Falló la generación de uno o más clips. Puedes corregirlos a mano o generar los pendientes.'}
      </p>
    );
  } else if (
    captionState?.status === CAPTION_GENERATION_STATUS.reviewRequired ||
    (captionState?.status === CAPTION_GENERATION_STATUS.ready && captionReviewBlocked)
  ) {
    captionGenerationNotice = (
      <p role="status" className="text-xs text-amber-500">
        Los candidatos todavía no son subtítulos finales. Aprueba cada clip para desbloquear el render.
      </p>
    );
  } else {
    captionGenerationNotice = (
      <p className="text-xs text-muted-foreground">
        Guarda los rangos actuales y pulsa Generar candidatos para empezar la revisión.
      </p>
    );
  }

  return (
    <div className="grid gap-6 lg:grid-cols-[1fr_280px]">
      <div className="flex flex-col gap-[18px]">
        <div className="studio-panel flex flex-wrap items-center justify-between gap-3 p-4">
          <div className="min-w-0">
            <p className="truncate text-sm font-medium text-foreground">{job.title?.trim() || 'Clip de stream'}</p>
            <p className="text-xs text-muted-foreground">El título se ha copiado al primer rango y puedes editarlo allí.</p>
          </div>
          {job.source_url && sourceLabel ? (
            <a href={job.source_url} target="_blank" rel="noreferrer" className="text-xs text-stream underline-offset-4 hover:underline">
              {sourceLabel} · ver origen
            </a>
          ) : null}
        </div>
        <div className="studio-panel p-5 sm:p-6">
          <div className="flex flex-col gap-4">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <SectionEyebrow label="LAYOUT" />
              <span className="font-[family-name:var(--font-mono)] text-[10px] tracking-[0.14em] text-muted-foreground">
                SALIDA: 1080×1920
              </span>
            </div>

            <div className="grid gap-3.5 sm:grid-cols-3">
              {STREAM_VARIANTS.map((v) => {
                const selected = v.value === plan.variant;
                return (
                  <button
                    key={v.value}
                    type="button"
                    disabled={busy}
                    onClick={() => setVariant(v.value)}
                    aria-pressed={selected}
                    className={cn(
                      'flex items-center gap-3 border p-3 text-left transition-colors disabled:pointer-events-none disabled:opacity-50',
                      selected ? 'border-[1.5px] border-stream bg-stream/[0.07]' : 'border-white/14 hover:border-white/25',
                    )}
                  >
                    <LayoutGlyph variant={v.value} selected={selected} />
                    <span className="flex flex-col gap-0.5">
                      <span
                        className={cn(
                          'font-[family-name:var(--font-display)] text-[12.5px] font-bold uppercase',
                          selected ? 'text-[#ffeaf2]' : 'text-foreground',
                        )}
                      >
                        {v.label}
                      </span>
                      <span
                        className={cn(
                          'font-[family-name:var(--font-mono)] text-[9.5px] uppercase tracking-[0.1em]',
                          selected ? 'text-[#b88fa3]' : 'text-muted-foreground',
                        )}
                      >
                        {v.subtitle}
                      </span>
                    </span>
                  </button>
                );
              })}
            </div>

            {variantMeta.needsFaceCrop ? (
              <div className="flex flex-col gap-2">
                <Label>
                  Recorte de facecam: arrastra para mover o usa las flechas; ajusta la esquina para redimensionar
                </Label>
                <CropPicker
                  videoSrc={videoSrc}
                  rect={plan.face_crop ?? DEFAULT_FACE_CROP}
                  onChange={setFaceCrop}
                  kind="facecam"
                  frameSeconds={previewSeconds}
                  disabled={busy}
                />
                <div className="flex flex-wrap items-center gap-3">
                  <Button
                    type="button"
                    size="sm"
                    variant={plan.face_crop_reviewed ? 'outline' : 'default'}
                    disabled={busy}
                    onClick={confirmFaceCrop}
                  >
                    <CircleCheck className="size-4" />
                    {plan.face_crop_reviewed ? 'RECORTE CONFIRMADO' : 'CONFIRMAR RECORTE DE FACECAM'}
                  </Button>
                  {!plan.face_crop_reviewed ? (
                    <p role="alert" className="text-xs text-amber-500">
                      Verifica que el marco contiene una cara. El recorte inicial es solo una guía y podría coincidir con el radar.
                    </p>
                  ) : null}
                </div>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                No hace falta recorte de facecam: este layout renderiza el gameplay a pantalla completa.
              </p>
            )}

            <section
              aria-labelledby="killfeed-clean-title"
              className="flex flex-col gap-4 border-t border-border pt-4"
            >
              <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="flex max-w-2xl flex-col gap-1">
                  <h3 id="killfeed-clean-title" className="text-sm font-medium text-foreground">
                    Killfeed limpia (opcional)
                  </h3>
                  <p id="killfeed-clean-description" className="text-xs leading-relaxed text-muted-foreground">
                    FragForge recorre todos los clips, localiza cada nacimiento por PTS del vídeo y coordina
                    automáticamente su captura. La edición manual queda disponible como corrección.
                  </p>
                </div>
                <Button
                  id="killfeed-clean-toggle"
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  aria-pressed={killfeedEnabled}
                  aria-expanded={killfeedEnabled}
                  aria-controls="killfeed-clean-controls"
                  aria-describedby="killfeed-clean-description"
                  onClick={() => setKillfeedEnabled(!killfeedEnabled)}
                  className={cn(
                    'shrink-0 focus-visible:ring-stream',
                    killfeedEnabled
                      ? 'border-stream bg-stream text-stream-foreground hover:bg-stream/90'
                      : 'border-stream/50 text-stream hover:border-stream hover:bg-stream/10',
                  )}
                >
                  {killfeedEnabled ? 'Killfeed: activada' : 'Activar killfeed limpia'}
                </Button>
              </div>

              {killfeedEnabled ? (
                <div id="killfeed-clean-controls" className="flex flex-col gap-4">
                  <p className="text-xs leading-relaxed text-muted-foreground">
                    Ajusta el recorte para que cubra holgadamente el área de la killfeed. Tras dejar de moverlo,
                    FragForge vuelve a analizar los rangos automáticamente. El cue se guarda con el primer
                    fotograma fuente verificable; un fotograma posterior se usa solo para leer o congelar el aviso.
                  </p>
                  <div className="flex flex-wrap items-center gap-3">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={busy || !clipsAreValid(plan.clips)}
                      onClick={() => void runKillfeedAnalysis(plan)}
                      className="border-stream/60 text-stream hover:border-stream hover:bg-stream/10 focus-visible:ring-stream"
                    >
                      {killfeedRequestBusy || killfeedAnalyzing ? (
                        <Loader2 className="size-4 animate-spin" />
                      ) : (
                        <RefreshCw className="size-4" />
                      )}
                      {plan.killfeed_analysis ? 'REANALIZAR KILLFEED' : 'ANALIZAR KILLFEED'}
                    </Button>
                    {killfeedAnalyzing || killfeedRequestBusy ? (
                      <AnalysisProgress
                        label="Analizando clips por fotograma"
                        onCancel={() => {
                          killfeedPollGen.current += 1;
                          setKillfeedRequestBusy(false);
                          setKillfeedState(null);
                          setKillfeedError('Espera cancelada. El análisis puede continuar en segundo plano; puedes retomarlo después.');
                        }}
                      />
                    ) : null}
                    {!killfeedAnalyzing &&
                    !killfeedRequestBusy &&
                    plan.killfeed_analysis &&
                    killfeedState?.status === KILLFEED_ANALYSIS_STATUS.applied ? (
                      <span role="status" className="text-xs text-success">
                        {detectedKillfeedEvents} {detectedKillfeedEvents === 1 ? 'evento alineado' : 'eventos alineados'}.
                      </span>
                    ) : null}
                  </div>
                  {killfeedError ? (
                    <p role="alert" className="flex items-center gap-2 text-xs text-destructive">
                      <AlertTriangle className="size-4" />
                      {killfeedError}
                    </p>
                  ) : null}
                  {killfeedRenderNeedsReanalysis ? (
                    <p role="alert" className="flex items-center gap-2 text-xs text-destructive">
                      <AlertTriangle className="size-4" />
                      Las capturas exactas ya no están disponibles. Pulsa REANALIZAR KILLFEED antes de crear los Shorts otra vez.
                    </p>
                  ) : null}
                  {killfeedState?.warnings?.map((warning) => (
                    <p key={warning} className="flex items-center gap-2 text-xs text-amber-500">
                      <AlertTriangle className="size-4" />
                      {warning}
                    </p>
                  ))}
                  {killfeedReadNotice ? (
                    <p role="status" className="text-xs text-stream">
                      {killfeedReadNotice}
                    </p>
                  ) : null}
                  <CropPicker
                    videoSrc={videoSrc}
                    rect={plan.killfeed_crop ?? DEFAULT_KILLFEED_CROP}
                    onChange={setKillfeedCrop}
                    kind="killfeed"
                    frameSeconds={previewSeconds}
                    durationSeconds={sourceDuration}
                    onFrameSecondsChange={setPreviewSeconds}
                    showScrubber
                    minWidth={KILLFEED_MIN_CROP_SIZE}
                    minHeight={KILLFEED_MIN_CROP_SIZE}
                    disabled={busy}
                  />

                  <div className="flex flex-col gap-2">
                    <div className="flex flex-wrap items-center gap-3">
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        disabled={busy || !canAddKillfeedCue}
                        aria-describedby="killfeed-cue-status"
                        onClick={addKillfeedCue}
                        className="border-stream/60 text-stream hover:border-stream hover:bg-stream/10 focus-visible:ring-stream"
                      >
                        <Plus className="size-4" aria-hidden />
                        Añadir corrección en {formatStreamTimestamp(previewSeconds)}
                      </Button>
                      <p id="killfeed-cue-status" className="text-xs text-muted-foreground">
                        {killfeedCueStatus}
                      </p>
                    </div>
                  </div>

                  <div className="flex flex-col divide-y divide-border border-y border-border">
                    {plan.clips.map((clip, index) => {
                      const cues = clip.killfeed_seconds ?? [];
                      const clipTitle = clip.title?.trim();
                      const clipLabel = clipTitle
                        ? `Clip ${index + 1}: ${clipTitle}`
                        : `Clip ${index + 1}`;
                      const headingId = `killfeed-cues-${clip.id}`;
                      return (
                        <section
                          key={clip.id}
                          aria-labelledby={headingId}
                          className="flex flex-col gap-2 py-3"
                        >
                          <div className="flex flex-wrap items-baseline justify-between gap-2">
                            <h4 id={headingId} className="text-xs font-semibold text-foreground">
                              {clipLabel}
                            </h4>
                            <span className="font-[family-name:var(--font-mono)] text-xs tabular-nums text-muted-foreground">
                              {formatStreamTimestamp(clip.start_seconds)} - {formatStreamTimestamp(clip.end_seconds)}
                            </span>
                          </div>
                          {cues.length > 0 ? (
                            <ul className="flex flex-col gap-3" aria-label={`Marcas de ${clipLabel}`}>
                              {cues.map((cue, cueIndex) => {
                                const key = cueKey(clip.id, cue);
                                return (
                                  <li
                                    key={`${clip.id}-${cue}`}
                                    className="flex flex-col gap-3 border border-stream/30 bg-stream/[0.04] p-3"
                                  >
                                    <div className="flex flex-wrap items-center justify-between gap-2">
                                      <button
                                        type="button"
                                        disabled={busy}
                                        aria-label={`Mostrar la marca ${formatStreamTimestamp(cue)} de ${clipLabel}`}
                                        onClick={() => setPreviewSeconds(cue)}
                                        className="inline-flex items-center rounded-full border border-stream/45 bg-stream/10 px-2.5 py-1 font-[family-name:var(--font-mono)] text-xs tabular-nums text-stream outline-none hover:bg-stream/15 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-stream disabled:opacity-50"
                                      >
                                        {formatStreamTimestamp(cue)}
                                      </button>
                                      <Button
                                        type="button"
                                        variant="ghost"
                                        size="icon-sm"
                                        disabled={busy}
                                        aria-label={`Eliminar la marca ${formatStreamTimestamp(cue)} de ${clipLabel}`}
                                        onClick={() => removeKillfeedCue(clip.id, cue)}
                                      >
                                        <Trash2 className="size-3.5" aria-hidden />
                                      </Button>
                                    </div>
                                    <KillfeedKillsEditor
                                      kills={clip.killfeed_kills?.[cueIndex] ?? []}
                                      weapons={weapons}
                                      reading={readingCueKey === key}
                                      readError={readErrors[key] ?? null}
                                      disabled={busy}
                                      onChange={(kills) => setCueKills(clip.id, cue, kills)}
                                      onReadWithAI={() => void readCueWithAI(clip, cue)}
                                    />
                                  </li>
                                );
                              })}
                            </ul>
                          ) : (
                            <p className="text-xs text-muted-foreground">
                              Aún no se detectaron eventos de killfeed en este clip.
                            </p>
                          )}
                        </section>
                      );
                    })}
                  </div>
                  <p className="text-xs text-muted-foreground">
                    Los eventos automáticos se ordenan por PTS. Cambiar crop o rangos invalida el análisis anterior y lanza uno nuevo.
                  </p>
                </div>
              ) : (
                <p className="text-xs text-muted-foreground">
                  Desactivada: el render conserva exactamente el flujo actual, sin recorte ni avisos superpuestos.
                </p>
              )}
            </section>

            <div className="flex flex-col gap-2 border-t border-border pt-4">
              <div className="flex flex-col gap-1">
                <Label htmlFor="streamer-nick">Banner del streamer (opcional)</Label>
                <p className="text-xs text-muted-foreground">
                  Añade una franja morada con el nick sobre la unión entre facecam y gameplay.
                </p>
              </div>
              <Input
                id="streamer-nick"
                value={plan.streamer_banner?.nick ?? ''}
                disabled={busy}
                maxLength={25}
                pattern="[A-Za-z0-9_]{1,25}"
                aria-invalid={!STREAMER_NICK_RE.test(plan.streamer_banner?.nick?.trim() ?? '')}
                onChange={(e) => setStreamerNick(e.target.value)}
                placeholder="zacketizorcs2"
                className="max-w-sm"
              />
              {!STREAMER_NICK_RE.test(plan.streamer_banner?.nick?.trim() ?? '') ? (
                <p className="text-xs text-destructive">Usa solo letras, números o guiones bajos (máximo 25).</p>
              ) : null}
              <div className="mt-2 flex max-w-xl flex-col gap-3 border-l-2 border-stream/35 pl-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <Label htmlFor="streamer-banner-position">Posición vertical del banner</Label>
                  <span className="font-[family-name:var(--font-mono)] text-[11px] text-stream">
                    {Math.round(bannerPosition * 100)}%
                  </span>
                </div>
                <input
                  id="streamer-banner-position"
                  type="range"
                  min={STREAMER_BANNER_MIN_POSITION}
                  max={STREAMER_BANNER_MAX_POSITION}
                  step="0.001"
                  value={bannerPosition}
                  disabled={busy}
                  aria-label="Posición vertical del banner"
                  aria-valuetext={`${Math.round(bannerPosition * 100)}% desde arriba`}
                  onChange={(event) => setStreamerPosition(Number(event.target.value))}
                  className={cn('w-full', ACCENT_SLIDER_CLASS)}
                />
                <div className="flex flex-wrap items-center gap-2">
                  <Button
                    type="button"
                    variant={plan.streamer_banner?.slide_enabled ? 'default' : 'outline'}
                    size="sm"
                    disabled={busy}
                    aria-pressed={plan.streamer_banner?.slide_enabled ?? false}
                    onClick={() => setStreamerSlide(!(plan.streamer_banner?.slide_enabled ?? false))}                  >
                    {plan.streamer_banner?.slide_enabled
                      ? 'Deslizamiento: activado'
                      : 'Deslizamiento: desactivado'}
                  </Button>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    disabled={busy || plan.streamer_banner?.position_y === undefined}
                    onClick={resetStreamerPosition}                  >
                    Restablecer posición
                  </Button>
                </div>
                {plan.streamer_banner?.slide_enabled ? (
                  <p className="text-xs text-muted-foreground">
                    La vista previa repite una entrada desde la izquierda, una pausa y la salida hacia la izquierda.
                  </p>
                ) : null}
              </div>
            </div>
          </div>
        </div>

        <div className="studio-panel p-5 sm:p-6">
          <ClipEditor
            clips={plan.clips}
            sourceDuration={sourceDuration}
            onChange={setClips}
            disabled={busy}
          />
        </div>

        <div className="studio-panel p-5 sm:p-6">
          <div className="flex flex-col gap-4">
            <SectionEyebrow label="SUBTÍTULOS" />
            <div className="flex flex-wrap items-center justify-between gap-3">
              <Button
                type="button"
                variant={plan.captions?.enabled ? 'default' : 'outline'}
                size="sm"
                disabled={busy}
                onClick={() => setCaptionsEnabled(!plan.captions?.enabled)}
                className="gap-1.5"
              >
                <Captions className="size-4" />
                {plan.captions?.enabled ? 'Subtítulos incrustados: activados' : 'Subtítulos incrustados: desactivados'}
              </Button>
              {plan.captions?.enabled ? (
                <div className="flex flex-wrap items-center gap-2">
                  <span className="studio-chip">Salida: español</span>
                  {canGenerateCaptions ? (
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      disabled={busy || captionLoading}
                      onClick={() => void generateCaptionCandidates()}
                    >
                      {captionRequestBusy || captionGenerating ? (
                        <Loader2 className="size-4 animate-spin" />
                      ) : (
                        <RefreshCw className="size-4" />
                      )}
                      {captionState?.status === CAPTION_GENERATION_STATUS.none || captionState === null
                        ? 'GENERAR CANDIDATOS'
                        : 'GENERAR PENDIENTES'}
                    </Button>
                  ) : null}
                </div>
              ) : null}
            </div>
            {plan.captions?.enabled ? (
              <div className="flex flex-col gap-4">
                <p className="text-xs leading-relaxed text-muted-foreground">
                  La IA genera candidatos separados del render. Revisa el texto y los tiempos de cada clip;
                  FragForge no los incrusta hasta que los apruebes o confirmes que no hay voz.
                </p>

                {captionGenerationNotice}

                {captionState?.warnings?.map((warning) => (
                  <p key={warning} className="flex items-center gap-2 text-xs text-amber-500">
                    <AlertTriangle className="size-4" />
                    {warning}
                  </p>
                ))}

                {captionState && captionState.status !== CAPTION_GENERATION_STATUS.none && !captionGenerating ? (
                  <div className="flex flex-col gap-3">
                    {plan.clips.map((clip, index) => (
                      <CaptionReviewCard
                        key={clip.id}
                        videoSrc={videoSrc}
                        clip={clip}
                        clipNumber={index + 1}
                        candidate={(captionState.clips ?? []).find((item) => item.clip_id === clip.id)}
                        words={captionDrafts[clip.id] ?? clip.caption_words ?? []}
                        disabled={busy}
                        reviewing={reviewingClipId === clip.id}
                        onWordsChange={(words) => updateCaptionDraft(clip.id, words)}
                        onApprove={() => void reviewCaptionClip(clip, false)}
                        onNoSpeech={() => void reviewCaptionClip(clip, true)}
                      />
                    ))}
                  </div>
                ) : null}

                {captionError ? (
                  <p role="alert" className="flex items-center gap-2 text-xs text-destructive">
                    <AlertTriangle className="size-4" />
                    {captionError}
                  </p>
                ) : null}
              </div>
            ) : null}
          </div>
        </div>

        <MusicAndEffectsCard
          plan={plan}
          busy={busy}
          onMusicKey={setMusicKey}
          onMusicVolume={setMusicVolume}
          onGrade={setGrade}
        />

        {error ? (
          <p className="flex items-center gap-2 text-sm text-destructive">
            <AlertTriangle className="size-4" />
            {error}
          </p>
        ) : null}

        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={onCreate}
            disabled={busy || captionReviewBlocked || killfeedAnalysisBlocked}
            className="neon-glow rounded-md inline-flex items-center gap-1.5 bg-primary px-5 py-2.5 font-[family-name:var(--font-display)] text-[13px] font-bold tracking-[0.06em] text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
          >
            {busy ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />}
            {stage === 'rendering' ? 'RENDERIZANDO…' : 'CREAR SHORTS'}
          </button>
          {captionReviewBlocked ? (
            <span className="text-xs text-amber-500">Revisa los subtítulos pendientes para continuar.</span>
          ) : null}
          {killfeedAnalysisBlocked ? (
            <span className="text-xs text-amber-500">
              {killfeedManualCueError ?? 'Espera al análisis automático de la killfeed para continuar.'}
            </span>
          ) : null}
          <Button variant="ghost" onClick={onStartOver} disabled={busy}>
            Empezar de nuevo
          </Button>
        </div>

        {renderedPlan && (stage === 'rendered' || renderState?.published) ? (
          <RenderResults renderState={renderState} job={job} renderedPlan={renderedPlan} stale={stale} />
        ) : null}
      </div>

      <div className="flex flex-col gap-3">
        <span className="font-[family-name:var(--font-mono)] text-[10.5px] tracking-[0.28em] text-muted-foreground">
          PREVIEW · 9:16
        </span>
        <StreamPreview
          key={previewReload}
          videoSrc={videoSrc}
          variant={plan.variant}
          faceCrop={plan.face_crop}
          gameplayCrop={plan.gameplay_crop}
          killfeedCrop={plan.killfeed_crop}
          clips={plan.clips}
          frameSeconds={previewSeconds}
          streamerNick={plan.streamer_banner?.nick?.trim()}
          streamerPositionY={plan.streamer_banner?.position_y}
          streamerSlideEnabled={plan.streamer_banner?.slide_enabled}
          onStreamerPositionChange={setStreamerPosition}
          disabled={busy}
          onMediaError={() => {
            setPreviewPlaying(false);
            setPreviewError('No se pudo decodificar o leer el MP4 de origen. Comprueba que el archivo siga disponible y reintenta la vista previa.');
          }}
        />
        <audio key={previewReload} ref={previewAudioRef} src={videoSrc} preload="metadata" onError={() => {
          setPreviewPlaying(false);
          setPreviewError('No se pudo decodificar la pista de audio de la preview. Revisa el MP4 y reintenta.');
        }} />
        <div className="flex flex-wrap items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={sourceDuration <= 0 || startMontagePlayback(plan.clips, previewSeconds) === null}
            onClick={() => {
              setPreviewError(null);
              setPreviewPlaying((current) => !current);
            }}
          >
            {previewPlaying ? <Pause className="size-4" /> : <Play className="size-4" />}
            {previewPlaying ? 'PAUSAR PREVIEW' : 'REPRODUCIR MONTAJE'}
          </Button>
          <span className="font-[family-name:var(--font-mono)] text-[10px] text-muted-foreground">
            {formatStreamTimestamp(previewSeconds)} / {formatStreamTimestamp(sourceDuration)}
          </span>
        </div>
        {previewError ? (
          <div role="alert" className="flex flex-col gap-2 text-xs text-destructive">
            <span>{previewError}</span>
            <Button type="button" variant="outline" size="sm" onClick={() => {
              setPreviewError(null);
              setPreviewReload((current) => current + 1);
            }}>
              REINTENTAR VISTA PREVIA
            </Button>
          </div>
        ) : null}
        <p className="text-[11.5px] leading-relaxed text-muted-foreground/80">
          La preview replica el encuadre vertical. En cada marca: con kills confirmadas superpone la killfeed sintética nítida; sin kills congela el recorte del MP4.
        </p>
      </div>
    </div>
  );
}

/** A tiny two-region bar visualizing a layout variant (facecam/gameplay
 * proportion, a stacked triptych, or a solid full-frame block), matching the
 * mockup's mini icon next to each layout option. Purely decorative. */
function LayoutGlyph({ variant, selected }: { variant: StreamVariant; selected: boolean }) {
  const tone = selected ? 'bg-stream' : 'bg-white/25';
  const dim = 'bg-white/12';

  let regions: ReactNode;
  if (variant === 'streamer-vertical-stack-40-60') {
    regions = (
      <>
        <span className={cn('h-[40%]', tone)} />
        <span className={cn('flex-1', dim)} />
      </>
    );
  } else if (variant === 'streamer-vertical-stack') {
    regions = (
      <>
        <span className={cn('h-[26%]', tone)} />
        <span className={cn('flex-1', dim)} />
        <span className={cn('h-[26%]', tone)} />
      </>
    );
  } else {
    regions = <span className={cn('flex-1', dim)} />;
  }

  return (
    <span className="flex h-[42px] w-6 shrink-0 flex-col overflow-hidden border border-white/25">
      {regions}
    </span>
  );
}

/**
 * Drops default-valued fields so an untouched edit keeps the plan (and the
 * render fingerprint) identical to a plan without an `edit` object at all.
 */
function pruneClipEdit(edit: StreamClipEdit): StreamClipEdit | undefined {
  const next: StreamClipEdit = {};
  if (edit.speed !== undefined && edit.speed !== 1) next.speed = edit.speed;
  if (edit.source_volume !== undefined && edit.source_volume !== 1) next.source_volume = edit.source_volume;
  if (edit.fade_in_seconds) next.fade_in_seconds = edit.fade_in_seconds;
  if (edit.fade_out_seconds) next.fade_out_seconds = edit.fade_out_seconds;
  if (edit.text_overlays && edit.text_overlays.length > 0) next.text_overlays = edit.text_overlays;
  return Object.keys(next).length > 0 ? next : undefined;
}

function CaptionReviewCard({
  videoSrc,
  clip,
  clipNumber,
  candidate,
  words,
  disabled,
  reviewing,
  onWordsChange,
  onApprove,
  onNoSpeech,
}: {
  videoSrc: string;
  clip: StreamClipRange;
  clipNumber: number;
  candidate?: CaptionCandidateClip;
  words: StreamCaptionWord[];
  disabled: boolean;
  reviewing: boolean;
  onWordsChange: (words: StreamCaptionWord[]) => void;
  onApprove: () => void;
  onNoSpeech: () => void;
}) {
  const audioRef = useRef<HTMLAudioElement>(null);
  const duration = Math.max(0, clip.end_seconds - clip.start_seconds);
  const audible = clipHasAudibleSource(clip);
  const reviewed = clip.caption_reviewed === true;
  const reviewedNoSpeech = reviewed && (clip.caption_words?.length ?? 0) === 0;
  const issue = captionWordsIssue(words, duration);
  const updateWord = (index: number, patch: Partial<StreamCaptionWord>) =>
    onWordsChange(words.map((word, wordIndex) => (wordIndex === index ? { ...word, ...patch } : word)));
  const removeWord = (index: number) => onWordsChange(words.filter((_word, wordIndex) => wordIndex !== index));
  const addWord = () => {
    const start = words[words.length - 1]?.end_seconds ?? 0;
    const end = Math.min(duration, start + 0.5);
    if (end <= start) return;
    onWordsChange([...words, { word: '', start_seconds: start, end_seconds: end }]);
  };

  let status: string;
  let statusClass = 'text-amber-500';
  if (!audible) {
    status = 'Audio silenciado: este clip no necesita subtítulos.';
    statusClass = 'text-muted-foreground';
  } else if (reviewedNoSpeech) {
    status = 'Revisado: confirmado sin voz.';
    statusClass = 'text-success';
  } else if (reviewed) {
    status = `Revisado: ${clip.caption_words?.length ?? 0} palabras aprobadas.`;
    statusClass = 'text-success';
  } else if (candidate?.status === CAPTION_CLIP_STATUS.failed) {
    status = candidate.error || 'El proveedor de transcripción falló. Escucha el tramo y vuelve a generar o corrígelo a mano.';
    statusClass = 'text-destructive';
  } else if (candidate?.status === CAPTION_CLIP_STATUS.noSpeech) {
    status = 'El proveedor analizó el audio pero no detectó voz. Escucha el tramo; si sí hay voz, vuelve a generar.';
  } else if (candidate) {
    status = `${words.length} palabras candidatas pendientes de revisión.`;
  } else {
    status = 'Este clip todavía no tiene candidatos actuales.';
  }

  return (
    <section className="flex flex-col gap-3 border border-border bg-background/35 p-4" aria-labelledby={`${clip.id}-caption-title`}>
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <h3 id={`${clip.id}-caption-title`} className="text-sm font-medium text-foreground">
            Clip {clipNumber}{clip.title?.trim() ? ` · ${clip.title.trim()}` : ''}
          </h3>
          <p className={cn('mt-1 text-xs', statusClass)}>{status}</p>
        </div>
        {candidate?.provider ? (
          <span className="font-[family-name:var(--font-mono)] text-[10px] text-muted-foreground">
            {candidate.provider}{candidate.stt_model ? ` · ${candidate.stt_model}` : ''}
          </span>
        ) : null}
      </div>

      {audible && candidate ? (
        <>
          <div className="flex flex-col gap-1">
            <Label htmlFor={`${clip.id}-audio-review`} className="text-[10px] text-muted-foreground">
              Escuchar tramo original ({formatStreamTimestamp(clip.start_seconds)}–{formatStreamTimestamp(clip.end_seconds)})
            </Label>
            <audio
              id={`${clip.id}-audio-review`}
              ref={audioRef}
              src={videoSrc}
              controls
              preload="metadata"
              onPlay={(event) => {
                if (event.currentTarget.currentTime < clip.start_seconds || event.currentTarget.currentTime >= clip.end_seconds) {
                  event.currentTarget.currentTime = clip.start_seconds;
                }
              }}
              onTimeUpdate={(event) => {
                if (event.currentTarget.currentTime >= clip.end_seconds) event.currentTarget.pause();
              }}
              className="h-10 w-full"
            />
          </div>
          <div className="max-h-96 overflow-auto border border-border/60">
            {words.length === 0 ? (
              <p className="p-3 text-xs text-muted-foreground">
                No hay palabras. Puedes añadirlas manualmente o confirmar que el clip no contiene voz.
              </p>
            ) : (
              <div className="flex flex-col divide-y divide-border/60">
                {words.map((word, index) => (
                  <div key={index} className="grid grid-cols-[minmax(8rem,1fr)_5.5rem_5.5rem_2.25rem] items-end gap-2 p-2">
                    <div className="flex flex-col gap-1">
                      <Label htmlFor={`${clip.id}-caption-${index}-word`} className="text-[10px] text-muted-foreground">
                        Palabra {index + 1}
                      </Label>
                      <Input
                        id={`${clip.id}-caption-${index}-word`}
                        value={word.word}
                        maxLength={80}
                        disabled={disabled}
                        onChange={(event) => updateWord(index, { word: event.target.value })}
                        className="h-8"
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor={`${clip.id}-caption-${index}-start`} className="text-[10px] text-muted-foreground">
                        Inicio
                      </Label>
                      <Input
                        id={`${clip.id}-caption-${index}-start`}
                        type="number"
                        min={0}
                        max={duration}
                        step="0.05"
                        value={word.start_seconds}
                        disabled={disabled}
                        onChange={(event) => updateWord(index, { start_seconds: Number(event.target.value) })}
                        className="h-8"
                      />
                    </div>
                    <div className="flex flex-col gap-1">
                      <Label htmlFor={`${clip.id}-caption-${index}-end`} className="text-[10px] text-muted-foreground">
                        Fin
                      </Label>
                      <Input
                        id={`${clip.id}-caption-${index}-end`}
                        type="number"
                        min={0}
                        max={duration}
                        step="0.05"
                        value={word.end_seconds}
                        disabled={disabled}
                        onChange={(event) => updateWord(index, { end_seconds: Number(event.target.value) })}
                        className="h-8"
                      />
                    </div>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      disabled={disabled}
                      onClick={() => removeWord(index)}
                      aria-label={`Eliminar palabra ${index + 1}`}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" variant="outline" size="sm" disabled={disabled} onClick={addWord}>
              <Plus className="size-4" />
              AÑADIR PALABRA
            </Button>
            <Button type="button" size="sm" disabled={disabled || issue !== null} onClick={onApprove}>
              {reviewing ? <Loader2 className="size-4 animate-spin" /> : <CircleCheck className="size-4" />}
              {reviewed ? 'GUARDAR REVISIÓN' : 'APROBAR TEXTO'}
            </Button>
            <Button type="button" variant="ghost" size="sm" disabled={disabled} onClick={onNoSpeech}>
              CONFIRMAR SIN VOZ
            </Button>
          </div>
          {issue && words.length > 0 ? <p className="text-xs text-destructive">{issue}</p> : null}
        </>
      ) : null}
    </section>
  );
}

function ClipEditor({
  clips,
  sourceDuration,
  onChange,
  disabled,
}: {
  clips: StreamClipRange[];
  sourceDuration: number;
  onChange: (clips: StreamClipRange[]) => void;
  disabled: boolean;
}) {
  const updateClip = (id: string, patch: Partial<StreamClipRange>) =>
    onChange(clips.map((c) => (c.id === id ? { ...c, ...patch } : c)));
  const removeClip = (id: string) => onChange(clips.filter((c) => c.id !== id));
  const addClip = () => onChange([...clips, { id: nextClipId(), start_seconds: 0, end_seconds: initialStreamClipEnd(sourceDuration), title: '' }]);
  const updateEdit = (id: string, patch: Partial<StreamClipEdit>) =>
    onChange(clips.map((c) => (c.id === id ? { ...c, edit: pruneClipEdit({ ...c.edit, ...patch }) } : c)));

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <SectionEyebrow label="RANGOS DE CLIP" count={clips.length} />
        <button
          type="button"
          onClick={addClip}
          disabled={disabled}
          className="inline-flex min-h-10 items-center gap-1 font-[family-name:var(--font-mono)] text-[11px] tracking-[0.14em] text-stream transition-opacity hover:opacity-80 disabled:pointer-events-none disabled:opacity-40"
        >
          <Plus className="size-3.5" />
          AÑADIR
        </button>
      </div>

      <div className="flex flex-col gap-3">
        {clips.map((clip, i) => {
          const rangeIssue = streamRangeIssue(clip, sourceDuration, i);
          const invalid = rangeIssue !== null;
          return (
            <div key={clip.id} className="flex flex-col gap-2 border border-border bg-background/30 p-4">
              <div className="flex flex-wrap items-end gap-2">
                <div className="flex flex-col gap-1">
                  <Label htmlFor={`${clip.id}-start`} className="text-xs text-muted-foreground">
                    Inicio (s)
                  </Label>
                  <Input
                    id={`${clip.id}-start`}
                    type="number"
                    min={0}
                    step="0.1"
                    value={clip.start_seconds}
                    disabled={disabled}
                    aria-invalid={invalid}
                    onChange={(e) => updateClip(clip.id, { start_seconds: Number(e.target.value) })}
                    className="w-24"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor={`${clip.id}-end`} className="text-xs text-muted-foreground">
                    Fin (s)
                  </Label>
                  <Input
                    id={`${clip.id}-end`}
                    type="number"
                    min={0}
                    step="0.1"
                    value={clip.end_seconds}
                    disabled={disabled}
                    aria-invalid={invalid}
                    onChange={(e) => updateClip(clip.id, { end_seconds: Number(e.target.value) })}
                    className="w-24"
                  />
                </div>
                <div className="flex min-w-40 flex-1 flex-col gap-1">
                  <Label htmlFor={`${clip.id}-title`} className="text-xs text-muted-foreground">
                    Título (opcional)
                  </Label>
                  <Input
                    id={`${clip.id}-title`}
                    value={clip.title ?? ''}
                    disabled={disabled}
                    onChange={(e) => updateClip(clip.id, { title: e.target.value })}
                    placeholder={`Clip ${i + 1}`}                  />
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  disabled={disabled || clips.length <= 1}
                  onClick={() => removeClip(clip.id)}
                  aria-label="Eliminar clip"
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
              {rangeIssue ? <p role="alert" className="text-xs text-destructive">{rangeIssue}</p> : null}
              <ClipEditControls
                clip={clip}
                disabled={disabled}
                onEditChange={(patch) => updateEdit(clip.id, patch)}
              />
            </div>
          );
        })}
      </div>
    </div>
  );
}

/**
 * Per-clip edit options (speed, original-audio volume, fades, text overlays),
 * mirroring streamclips.ClipEdit. All controls emit through `onEditChange`,
 * which prunes defaults so untouched clips keep their original plan shape.
 */
function ClipEditControls({
  clip,
  disabled,
  onEditChange,
}: {
  clip: StreamClipRange;
  disabled: boolean;
  onEditChange: (patch: Partial<StreamClipEdit>) => void;
}) {
  const edit = clip.edit ?? {};
  const speed = edit.speed ?? 1;
  const sourceVolume = edit.source_volume ?? 1;
  const overlays = edit.text_overlays ?? [];
  const clipDuration = Math.max(0, clip.end_seconds - clip.start_seconds);

  const updateOverlay = (index: number, patch: Partial<StreamTextOverlay>) =>
    onEditChange({ text_overlays: overlays.map((o, i) => (i === index ? { ...o, ...patch } : o)) });
  const removeOverlay = (index: number) =>
    onEditChange({ text_overlays: overlays.filter((_, i) => i !== index) });
  const addOverlay = () =>
    onEditChange({ text_overlays: [...overlays, { text: '', position_y: 0.5 }] });

  /** Empty input clears an optional numeric field; anything else sets it. */
  const optionalNumber = (value: string): number | undefined => (value === '' ? undefined : Number(value));
  /** Go's font_size is an int, so typed decimals are rounded before saving. */
  const optionalInteger = (value: string): number | undefined => (value === '' ? undefined : Math.round(Number(value)));

  return (
    <div className="mt-1 flex flex-col gap-3 border-t border-border/60 pt-3">
      <span className="font-[family-name:var(--font-mono)] text-[10px] tracking-[0.22em] text-muted-foreground">
        EDICIÓN
      </span>
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1">
          <Label htmlFor={`${clip.id}-speed`} className="text-xs text-muted-foreground">
            Velocidad
          </Label>
          <Select
            value={String(speed)}
            disabled={disabled}
            onValueChange={(value) => onEditChange({ speed: Number(value) })}
          >
            <SelectTrigger id={`${clip.id}-speed`} aria-label="Velocidad de reproducción" className="w-24">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {CLIP_SPEEDS.map((value) => (
                <SelectItem key={value} value={String(value)}>
                  {value}x
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor={`${clip.id}-fade-in`} className="text-xs text-muted-foreground">
            Fundido entrada (s)
          </Label>
          <Input
            id={`${clip.id}-fade-in`}
            type="number"
            min={0}
            max={5}
            step="0.1"
            value={edit.fade_in_seconds ?? 0}
            disabled={disabled}
            onChange={(e) => onEditChange({ fade_in_seconds: Number(e.target.value) })}
            className="w-24"
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor={`${clip.id}-fade-out`} className="text-xs text-muted-foreground">
            Fundido salida (s)
          </Label>
          <Input
            id={`${clip.id}-fade-out`}
            type="number"
            min={0}
            max={5}
            step="0.1"
            value={edit.fade_out_seconds ?? 0}
            disabled={disabled}
            onChange={(e) => onEditChange({ fade_out_seconds: Number(e.target.value) })}
            className="w-24"
          />
        </div>
        <div className="flex min-w-44 flex-1 flex-col gap-1">
          <div className="flex items-center justify-between">
            <Label htmlFor={`${clip.id}-source-volume`} className="text-xs text-muted-foreground">
              Volumen original
            </Label>
            <span className="font-[family-name:var(--font-mono)] text-[11px] text-stream">
              {sourceVolume === 0 ? 'Silencio' : `${Math.round(sourceVolume * 100)}%`}
            </span>
          </div>
          <input
            id={`${clip.id}-source-volume`}
            type="range"
            min={0}
            max={2}
            step="0.05"
            value={sourceVolume}
            disabled={disabled}
            aria-label="Volumen del audio original"
            aria-valuetext={sourceVolume === 0 ? 'Silencio' : `${Math.round(sourceVolume * 100)}%`}
            onChange={(e) => onEditChange({ source_volume: Number(e.target.value) })}
            className={cn('min-h-10 w-full', ACCENT_SLIDER_CLASS)}
          />
        </div>
      </div>

      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">Textos en pantalla</span>
        <button
          type="button"
          onClick={addOverlay}
          disabled={disabled || overlays.length >= MAX_TEXT_OVERLAYS}
          className="inline-flex min-h-10 items-center gap-1 font-[family-name:var(--font-mono)] text-[11px] tracking-[0.14em] text-stream transition-opacity hover:opacity-80 disabled:pointer-events-none disabled:opacity-40"
        >
          <Plus className="size-3.5" />
          AÑADIR TEXTO
        </button>
      </div>
      {overlays.map((overlay, index) => (
        <div key={index} className="flex flex-col gap-2 border border-border/60 bg-background/40 p-3">
          <div className="flex flex-wrap items-end gap-2">
            <div className="flex min-w-40 flex-1 flex-col gap-1">
              <Label htmlFor={`${clip.id}-text-${index}`} className="text-xs text-muted-foreground">
                Texto
              </Label>
              <Input
                id={`${clip.id}-text-${index}`}
                value={overlay.text}
                maxLength={120}
                disabled={disabled}
                aria-invalid={overlay.text.trim() === ''}
                onChange={(e) => updateOverlay(index, { text: e.target.value })}
                placeholder="NICE SHOT"
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor={`${clip.id}-text-${index}-start`} className="text-xs text-muted-foreground">
                Desde (s)
              </Label>
              <Input
                id={`${clip.id}-text-${index}-start`}
                type="number"
                min={0}
                max={clipDuration}
                step="0.1"
                value={overlay.start_seconds ?? ''}
                disabled={disabled}
                onChange={(e) => updateOverlay(index, { start_seconds: optionalNumber(e.target.value) })}
                placeholder="0"
                className="w-20"
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor={`${clip.id}-text-${index}-end`} className="text-xs text-muted-foreground">
                Hasta (s)
              </Label>
              <Input
                id={`${clip.id}-text-${index}-end`}
                type="number"
                min={0}
                max={clipDuration}
                step="0.1"
                value={overlay.end_seconds ?? ''}
                disabled={disabled}
                onChange={(e) => updateOverlay(index, { end_seconds: optionalNumber(e.target.value) })}
                placeholder={clipDuration.toFixed(1)}
                className="w-20"
              />
            </div>
            <div className="flex flex-col gap-1">
              <Label htmlFor={`${clip.id}-text-${index}-size`} className="text-xs text-muted-foreground">
                Tamaño
              </Label>
              <Input
                id={`${clip.id}-text-${index}-size`}
                type="number"
                min={MIN_OVERLAY_FONT_SIZE}
                max={MAX_OVERLAY_FONT_SIZE}
                step="1"
                value={overlay.font_size ?? ''}
                disabled={disabled}
                onChange={(e) => updateOverlay(index, { font_size: optionalInteger(e.target.value) })}
                placeholder={String(DEFAULT_OVERLAY_FONT_SIZE)}
                className="w-20"
              />
            </div>
            <Button
              type="button"
              variant="ghost"
              size="icon-sm"
              disabled={disabled}
              onClick={() => removeOverlay(index)}
              aria-label="Eliminar texto"
            >
              <Trash2 className="size-4" />
            </Button>
          </div>
          <div className="flex items-center gap-3">
            <Label htmlFor={`${clip.id}-text-${index}-position`} className="shrink-0 text-xs text-muted-foreground">
              Posición vertical
            </Label>
            <input
              id={`${clip.id}-text-${index}-position`}
              type="range"
              min={STREAMER_BANNER_MIN_POSITION}
              max={STREAMER_BANNER_MAX_POSITION}
              step="0.005"
              value={overlay.position_y}
              disabled={disabled}
              aria-valuetext={`${Math.round(overlay.position_y * 100)}% desde arriba`}
              onChange={(e) => updateOverlay(index, { position_y: Number(e.target.value) })}
              className={cn('min-h-10 w-full', ACCENT_SLIDER_CLASS)}
            />
            <span className="w-10 shrink-0 text-right font-[family-name:var(--font-mono)] text-[11px] text-stream">
              {Math.round(overlay.position_y * 100)}%
            </span>
          </div>
        </div>
      ))}
    </div>
  );
}

function RenderResults({
  renderState,
  job,
  renderedPlan,
  stale,
}: {
  renderState: StreamRenderState | null;
  job: StreamJob;
  /** The plan the shown render actually used; URLs must come from it, never the live edits. */
  renderedPlan: StreamEditPlan;
  stale: boolean;
}) {
  if (!renderState) return null;

  return (
    <div className="studio-panel p-5 sm:p-6">
      <div className="flex flex-col gap-4">
        <SectionEyebrow label="SHORTS RENDERIZADOS" count={renderState.videos.length} />

        {stale ? (
          <p className="flex items-center gap-2 border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
            <AlertTriangle className="size-3.5 shrink-0" />
            Estos Shorts se renderizaron antes de tus últimos cambios. Pulsa Crear Shorts para
            aplicarlos — la descarga está bloqueada hasta entonces para que nunca te quedes con un
            archivo desactualizado.
          </p>
        ) : null}

        {renderState.warnings && renderState.warnings.length > 0 ? (
          <ul className="flex flex-col gap-1">
            {renderState.warnings.map((w, i) => (
              <li key={i} className="flex items-center gap-2 text-xs text-amber-500">
                <AlertTriangle className="size-3.5" />
                {w}
              </li>
            ))}
          </ul>
        ) : null}

        {renderState.videos.length === 0 ? (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Film className="size-4" />
            No se generó ningún Short.
          </div>
        ) : (
          <div className="grid gap-5 sm:grid-cols-2">
            {renderState.videos.map((v) => {
              const url = streamsApi.videoUrl(job.id, renderedPlan.variant, v.clip_id);
              return (
                <div key={v.clip_id} className="flex flex-col gap-2">
                  {/* eslint-disable-next-line jsx-a11y/media-has-caption */}
                  <video src={url} controls className="aspect-[9/16] w-full bg-black object-contain" />
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate text-sm text-foreground">{v.title || v.clip_id}</span>
                    {stale ? (
                      <Button variant="outline" size="icon-sm" disabled aria-label={`Descargar ${v.title || v.clip_id} (desactualizado)`}>
                        <Download className="size-4" />
                      </Button>
                    ) : (
                      <Button asChild variant="outline" size="icon-sm">
                        <a href={url} download aria-label={`Descargar ${v.title || v.clip_id}`}>
                          <Download className="size-4" />
                        </a>
                      </Button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
        {renderState.delivery && renderState.delivery.length > 0 ? (
          <section className="flex flex-col gap-2 border-t border-border pt-4" aria-labelledby="delivery-pack-title">
            <h3 id="delivery-pack-title" className="text-sm font-medium text-foreground">Paquete shortslistosparasubir</h3>
            <p className="text-xs text-muted-foreground">Incluye MP4, portada, plan, manifest, subtítulos revisados y metadata.</p>
            <div className="flex flex-wrap gap-2">
              {renderState.delivery.map((artifact) => (
                stale ? (
                  <Button key={artifact.name} variant="outline" size="sm" disabled aria-label={`${artifact.name} (desactualizado)`}>
                    <Download className="size-4" />{artifact.name}
                  </Button>
                ) : (
                  <Button key={artifact.name} asChild variant="outline" size="sm">
                    <a href={streamsApi.deliveryUrl(job.id, renderedPlan.variant, artifact.name)} download={artifact.name}>
                      <Download className="size-4" />{artifact.name}
                    </a>
                  </Button>
                )
              ))}
            </div>
          </section>
        ) : null}
      </div>
    </div>
  );
}

/** Preset music gains: quiet bed, balanced, or music-forward. */
const MUSIC_VOLUMES = [
  { value: 0.15, label: 'Bajo' },
  { value: 0.25, label: 'Medio' },
  { value: 0.4, label: 'Alto' },
];

function MusicAndEffectsCard({
  plan,
  busy,
  onMusicKey,
  onMusicVolume,
  onGrade,
}: {
  plan: StreamEditPlan;
  busy: boolean;
  onMusicKey: (key: string) => void;
  onMusicVolume: (volume: number) => void;
  onGrade: (grade: boolean) => void;
}) {
  const [songs, setSongs] = useState<Song[] | null>(null);
  const [previewPlaying, setPreviewPlaying] = useState(false);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const audioRef = useRef<HTMLAudioElement>(null);
  const previewRequest = useRef(0);

  useEffect(() => {
    let active = true;
    api
      .listSongs()
      .then((next) => {
        if (active) setSongs(next);
      })
      .catch(() => {
        if (active) setSongs([]);
      });
    return () => {
      active = false;
    };
  }, []);

  const musicKey = plan.music?.key ?? '';
  const volume = plan.music?.volume ?? 0.25;
  const grade = plan.effects?.grade ?? false;
  const selectedSong = songs?.find((song) => song.id === musicKey);

  const stopAndResetPreview = useCallback(() => {
    previewRequest.current += 1;
    const audio = audioRef.current;
    if (audio) {
      audio.pause();
      audio.currentTime = 0;
    }
    setPreviewPlaying(false);
  }, []);

  useEffect(() => {
    stopAndResetPreview();
    setPreviewError(null);
  }, [musicKey, busy, stopAndResetPreview]);

  useEffect(() => {
    const audio = audioRef.current;
    return () => {
      previewRequest.current += 1;
      if (audio) {
        audio.pause();
        audio.currentTime = 0;
      }
    };
  }, []);

  const togglePreview = async (): Promise<void> => {
    const audio = audioRef.current;
    if (!audio || !selectedSong?.previewUrl || busy || songs === null) return;

    if (previewPlaying) {
      previewRequest.current += 1;
      audio.pause();
      setPreviewPlaying(false);
      return;
    }

    const request = ++previewRequest.current;
    setPreviewError(null);
    try {
      await audio.play();
      if (previewRequest.current === request) setPreviewPlaying(true);
    } catch {
      if (previewRequest.current !== request) return;
      audio.pause();
      audio.currentTime = 0;
      setPreviewPlaying(false);
      setPreviewError('No se pudo reproducir la vista previa de esta canción.');
    }
  };

  let selectedMusicLabel = 'Ninguna';
  if (songs === null) {
    selectedMusicLabel = musicKey ? 'Cargando pista…' : 'Cargando pistas…';
  } else if (selectedSong) {
    selectedMusicLabel = `${selectedSong.title}${selectedSong.genre ? ` · ${selectedSong.genre}` : ''}`;
  }

  return (
    <div className="studio-panel p-5 sm:p-6">
      <div className="flex flex-col gap-4">
        <SectionEyebrow label="MÚSICA Y EFECTOS" />

        <div className="flex flex-wrap items-center gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="stream-music" className="text-xs text-muted-foreground">
              Música de fondo
            </Label>
            <div className="flex items-center gap-2">
              <Select
                value={musicKey || NO_MUSIC_VALUE}
                disabled={busy || songs === null}
                onValueChange={(value) => onMusicKey(value === NO_MUSIC_VALUE ? '' : value)}
              >
                <SelectTrigger id="stream-music" className="w-72 max-w-[calc(80vw-2.5rem)]">
                  <SelectValue>{selectedMusicLabel}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NO_MUSIC_VALUE}>Ninguna</SelectItem>
                  {(songs ?? []).map((song) => (
                    <SelectItem key={song.id} value={song.id}>
                      {song.title}
                      {song.genre ? ` · ${song.genre}` : ''}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                type="button"
                variant="outline"
                size="icon"
                disabled={busy || songs === null || !selectedSong?.previewUrl}
                onClick={() => void togglePreview()}
                aria-label={`${previewPlaying ? 'Pausar' : 'Escuchar'} ${selectedSong?.title ?? 'música seleccionada'}`}
                className="shrink-0"
              >
                {previewPlaying ? <Pause className="size-4" /> : <Play className="size-4" />}
              </Button>
            </div>
            <audio
              ref={audioRef}
              src={selectedSong?.previewUrl}
              preload="none"
              data-music-preview
              className="hidden"
              onPlay={() => setPreviewPlaying(true)}
              onPause={() => setPreviewPlaying(false)}
              onEnded={stopAndResetPreview}
              onError={() => {
                stopAndResetPreview();
                setPreviewError('No se pudo reproducir la vista previa de esta canción.');
              }}
            />
            {previewError ? <p role="alert" className="text-xs text-destructive">{previewError}</p> : null}
          </div>

          {musicKey ? (
            <div className="flex flex-col gap-1">
              <Label className="text-xs text-muted-foreground">Volumen de música</Label>
              <ToggleGroup
                type="single"
                variant="outline"
                value={String(volume)}
                onValueChange={(v) => v && onMusicVolume(Number(v))}
                disabled={busy}
              >
                {MUSIC_VOLUMES.map((v) => (
                  <ToggleGroupItem key={v.value} value={String(v.value)} className="text-xs">
                    {v.label}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </div>
          ) : null}
        </div>

        <div className="flex flex-wrap items-center gap-3">
          <Button
            type="button"
            variant={grade ? 'default' : 'outline'}
            size="sm"
            disabled={busy}
            onClick={() => onGrade(!grade)}
            className="gap-1.5"
          >
            <Sparkles className="size-4" />
            {grade ? 'Gradación viral: activada' : 'Gradación viral: desactivada'}
          </Button>
          <p className="text-xs text-muted-foreground">
            Ligero realce de contraste y saturación. La música se mezcla bajo el audio del streamer.
          </p>
        </div>
      </div>
    </div>
  );
}
