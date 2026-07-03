'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import {
  AlertTriangle,
  Captions,
  Download,
  Film,
  Loader2,
  Plus,
  Sparkles,
  Trash2,
  Twitch,
  UploadCloud,
} from 'lucide-react';
import {
  streamsApi,
  STREAM_VARIANTS,
  SERVICE_UNAVAILABLE_CODE,
  type NormalizedRect,
  type StreamClipRange,
  type StreamEditPlan,
  type StreamJob,
  type StreamRenderState,
  type StreamVariant,
} from '@/lib/api/streams';
import { api } from '@/lib/api';
import type { Song } from '@/lib/api/types';
import { cn } from '@/lib/utils';
import { SectionEyebrow } from '@/components/brand';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { FacecamPicker } from '@/components/streams/facecam-picker';
import { StreamPreview } from '@/components/streams/stream-preview';

type Stage = 'idle' | 'submitting' | 'acquiring' | 'editing' | 'rendering' | 'rendered' | 'failed';

const FULL_FRAME: NormalizedRect = { x: 0, y: 0, width: 1, height: 1 };
const DEFAULT_FACE_CROP: NormalizedRect = { x: 0.62, y: 0.03, width: 0.34, height: 0.3 };

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/** True when an API error means the local analysis service is unreachable. */
function isServiceUnavailable(err: unknown): boolean {
  return (err as { code?: string } | null)?.code === SERVICE_UNAVAILABLE_CODE;
}

let clipSeq = 0;
function nextClipId(): string {
  clipSeq += 1;
  return `clip-${Date.now()}-${clipSeq}`;
}

function blankPlan(variant: StreamVariant = 'streamer-vertical-stack-40-60'): StreamEditPlan {
  return {
    schema_version: 1,
    variant,
    face_crop: DEFAULT_FACE_CROP,
    gameplay_crop: FULL_FRAME,
    clips: [{ id: nextClipId(), start_seconds: 0, end_seconds: 20, title: '' }],
    captions: { enabled: false, language: 'auto' },
  };
}

/** True once every clip range in the plan is well-formed (end strictly after start). */
function clipsAreValid(clips: StreamClipRange[]): boolean {
  return clips.length > 0 && clips.every((c) => Number.isFinite(c.start_seconds) && Number.isFinite(c.end_seconds) && c.end_seconds > c.start_seconds);
}

/**
 * Canonical fingerprint of everything a render consumes from the plan, so the
 * UI can tell whether the shown Shorts still match the current edits. Fields
 * are listed explicitly (not JSON.stringify of the object) so key order and
 * volatile fields like updated_at can never cause a false mismatch.
 */
function planFingerprint(plan: StreamEditPlan): string {
  const rect = (r?: NormalizedRect) => (r ? [r.x, r.y, r.width, r.height] : null);
  return JSON.stringify({
    variant: plan.variant,
    face: rect(plan.face_crop),
    game: rect(plan.gameplay_crop),
    clips: plan.clips.map((c) => [c.id, c.start_seconds, c.end_seconds, c.title ?? '']),
    captions: [plan.captions?.enabled ?? false, plan.captions?.language ?? 'auto'],
    music: [plan.music?.key ?? '', plan.music?.volume ?? 0],
    grade: plan.effects?.grade ?? false,
  });
}

/**
 * Stream Clips (/streams) — paste a Twitch clip/VOD URL or upload an MP4, then
 * lay out the facecam over gameplay and cut clip ranges before rendering
 * vertical Shorts. Mirrors /upload's stage machine (submit → wait → edit) but
 * against the /api/streams/* proxy, which forwards to the orchestrator's
 * stream-jobs pipeline (acquire/probe → edit plan → render).
 */
export default function StreamsPage() {
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

  const pollGen = useRef(0);

  const reset = useCallback((message: string) => {
    pollGen.current += 1;
    setError(message);
    setStage('idle');
    setJob(null);
    setPlan(null);
    setRenderState(null);
    setRenderedPlan(null);
    setFailureReason(null);
  }, []);

  const loadEditor = useCallback(async (j: StreamJob) => {
    setJob(j);
    try {
      const p = j.edit_plan ?? (await streamsApi.getEditPlan(j.id));
      setPlan(p.clips.length > 0 ? p : { ...p, clips: [{ id: nextClipId(), start_seconds: 0, end_seconds: 20, title: '' }] });
    } catch {
      setPlan(blankPlan());
    }
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

  const submitUrl = useCallback(async () => {
    if (!sourceUrl.trim()) return;
    setError(null);
    setStage('submitting');
    try {
      const j = await streamsApi.createFromUrl({ sourceUrl: sourceUrl.trim(), title: title.trim() || undefined });
      if (j.status === 'acquiring') {
        setJob(j);
        setStage('acquiring');
        void pollAcquiring(j.id);
      } else {
        void loadEditor(j);
      }
    } catch (err) {
      reset(
        isServiceUnavailable(err)
          ? 'El servicio de Clips de stream está offline. Arráncalo y vuelve a intentarlo.'
          : err instanceof Error
            ? err.message
            : 'No se pudo iniciar ese trabajo. Revisa la URL y vuelve a intentarlo.',
      );
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
        reset(
          isServiceUnavailable(err)
            ? 'El servicio de Clips de stream está offline. Arráncalo y vuelve a intentarlo.'
            : err instanceof Error
              ? err.message
              : 'No se pudo procesar ese archivo. Prueba con otro MP4.',
        );
      }
    },
    [title, pollAcquiring, loadEditor, reset],
  );

  const pollRender = useCallback(
    async (jobId: string, variant: StreamVariant) => {
      const gen = ++pollGen.current;
      for (let attempt = 0; attempt < 300; attempt++) {
        try {
          const state = await streamsApi.getRenderState(jobId, variant);
          if (pollGen.current !== gen) return;
          setRenderState(state);
          if (state.status === 'rendered') {
            setStage('rendered');
            return;
          }
          if (state.status === 'failed') {
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

  const createShorts = useCallback(async () => {
    if (!job || !plan) return;
    if (!clipsAreValid(plan.clips)) {
      setError('Cada clip necesita un fin posterior a su inicio.');
      return;
    }
    setError(null);
    setSaving(true);
    try {
      const saved = await streamsApi.putEditPlan(job.id, plan);
      setPlan(saved);
      setRenderedPlan(saved);
      setStage('rendering');
      setRenderState({ status: 'queued', videos: [] });
      await streamsApi.startRender(job.id, saved.variant);
      void pollRender(job.id, saved.variant);
    } catch (err) {
      setStage('editing');
      setError(
        isServiceUnavailable(err)
          ? 'El servicio de Clips de stream está offline. Arráncalo y vuelve a intentarlo.'
          : err instanceof Error
            ? err.message
            : 'No se pudo iniciar el render.',
      );
    } finally {
      setSaving(false);
    }
  }, [job, plan, pollRender]);

  useEffect(() => {
    return () => {
      pollGen.current += 1; // stop any in-flight poll loop on unmount
    };
  }, []);

  return (
    <div className="flex flex-col gap-7">
      <header className="flex flex-col gap-2.5">
        <SectionEyebrow number={3} label="CLIPS DE STREAM" accent="magenta" />
        <h1 className="font-[family-name:var(--font-display)] text-3xl font-bold leading-none tracking-tight text-foreground sm:text-[34px]">
          DE STREAM A SHORT
        </h1>
        <p className="max-w-xl text-sm text-muted-foreground">
          Pega un clip de Twitch o YouTube — o sube un MP4 — y córtalo en vertical con tu facecam.
        </p>
      </header>

      {stage === 'idle' || stage === 'submitting' ? (
        <SourceCard
          sourceUrl={sourceUrl}
          title={title}
          submitting={stage === 'submitting'}
          error={error}
          onSourceUrlChange={setSourceUrl}
          onTitleChange={setTitle}
          onSubmitUrl={() => void submitUrl()}
          onSubmitFile={(f) => void submitFile(f)}
        />
      ) : stage === 'acquiring' ? (
        <div className="flex flex-col items-center justify-center gap-4 border border-destructive/30 bg-card/80 p-6 py-14 text-center sm:p-8">
          <Loader2 className="size-8 animate-spin text-destructive" />
          <div className="flex flex-col gap-1">
            <p className="font-[family-name:var(--font-display)] text-lg font-semibold tracking-tight text-foreground">
              Descargando {job?.title || 'el clip'}…
            </p>
            <p className="text-sm text-muted-foreground">Descargando y analizando el vídeo de origen.</p>
          </div>
        </div>
      ) : stage === 'failed' ? (
        <div className="flex flex-col items-center justify-center gap-4 border border-destructive/30 bg-card/80 p-6 py-14 text-center sm:p-8">
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
            className="neon-notch bg-primary px-5 py-2.5 font-[family-name:var(--font-display)] text-sm font-bold tracking-[0.06em] text-primary-foreground transition-colors hover:bg-primary/90"
          >
            EMPEZAR DE NUEVO
          </button>
        </div>
      ) : job && plan ? (
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
          onStartOver={() => reset('')}
        />
      ) : null}
    </div>
  );
}

function SourceCard({
  sourceUrl,
  title,
  submitting,
  error,
  onSourceUrlChange,
  onTitleChange,
  onSubmitUrl,
  onSubmitFile,
}: {
  sourceUrl: string;
  title: string;
  submitting: boolean;
  error: string | null;
  onSourceUrlChange: (v: string) => void;
  onTitleChange: (v: string) => void;
  onSubmitUrl: () => void;
  onSubmitFile: (file: File) => void;
}) {
  const fileInputRef = useRef<HTMLInputElement>(null);

  return (
    <div className="neon-brackets [--neon-bracket-color:var(--destructive)] relative max-w-2xl border border-destructive/30 bg-[color-mix(in_oklch,var(--destructive)_6%,var(--card))] p-5 sm:p-6">
      <SectionEyebrow label="FUENTE" accent="magenta" />

      <div className="mt-4 flex flex-col gap-5">
        <div className="flex flex-col gap-2">
          <Label htmlFor="stream-title">Título (opcional)</Label>
          <Input
            id="stream-title"
            placeholder="Clutch 1v5 en pistola"
            value={title}
            disabled={submitting}
            onChange={(e) => onTitleChange(e.target.value)}
            className="rounded-none"
          />
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="stream-url">URL de clip o VOD de Twitch</Label>
          <div className="flex flex-col gap-2 sm:flex-row">
            <div className="relative flex-1">
              <Twitch className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                id="stream-url"
                placeholder="https://clips.twitch.tv/…"
                value={sourceUrl}
                disabled={submitting}
                onChange={(e) => onSourceUrlChange(e.target.value)}
                className="rounded-none pl-9"
              />
            </div>
            <button
              type="button"
              onClick={onSubmitUrl}
              disabled={submitting}
              className="neon-notch inline-flex items-center justify-center gap-1.5 bg-destructive px-5 font-[family-name:var(--font-display)] text-[13px] font-bold tracking-[0.06em] text-[#1a0410] transition-colors hover:bg-destructive/90 disabled:pointer-events-none disabled:opacity-50"
            >
              {submitting ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />}
              TRAER CLIP
            </button>
          </div>
        </div>

        <div className="flex items-center gap-3.5 font-[family-name:var(--font-mono)] text-[10px] tracking-[0.2em] text-muted-foreground">
          <div className="h-px flex-1 bg-white/10" />
          O
          <div className="h-px flex-1 bg-white/10" />
        </div>

        <div className="flex flex-col gap-2">
          <button
            type="button"
            disabled={submitting}
            onClick={() => fileInputRef.current?.click()}
            className="flex items-center justify-center gap-2 border border-dashed border-white/20 py-2.5 font-[family-name:var(--font-display)] text-[13px] font-semibold text-muted-foreground transition-colors hover:border-destructive/40 hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
          >
            <UploadCloud className="size-4" />
            SUBIR UN MP4
          </button>
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
        </div>

        {error ? <p className="text-sm text-destructive">{error}</p> : null}
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
  onStartOver: () => void;
}) {
  const videoSrc = streamsApi.sourceUrl(job.id);
  const variantMeta = STREAM_VARIANTS.find((v) => v.value === plan.variant) ?? STREAM_VARIANTS[0];
  const stale = renderedPlan !== null && planFingerprint(renderedPlan) !== planFingerprint(plan);
  const busy = stage === 'rendering' || saving;

  const setVariant = (variant: StreamVariant) => onPlanChange({ ...plan, variant });
  const setFaceCrop = (rect: NormalizedRect) => onPlanChange({ ...plan, face_crop: rect });
  const setClips = (clips: StreamClipRange[]) => onPlanChange({ ...plan, clips });
  const setCaptionsEnabled = (enabled: boolean) =>
    onPlanChange({ ...plan, captions: { enabled, language: plan.captions?.language ?? 'auto' } });
  const setLanguage = (language: string) =>
    onPlanChange({ ...plan, captions: { enabled: plan.captions?.enabled ?? false, language } });
  const setMusicKey = (key: string) =>
    onPlanChange({ ...plan, music: key ? { key, volume: plan.music?.volume } : {} });
  const setMusicVolume = (volume: number) =>
    onPlanChange({ ...plan, music: { key: plan.music?.key, volume } });
  const setGrade = (grade: boolean) => onPlanChange({ ...plan, effects: { grade } });

  return (
    <div className="grid gap-6 lg:grid-cols-[1fr_280px]">
      <div className="flex flex-col gap-[18px]">
        <div className="border border-primary/14 bg-card/75 p-5 sm:p-[22px]">
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
                      selected ? 'border-[1.5px] border-destructive bg-destructive/[0.07]' : 'border-white/14 hover:border-white/25',
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
                <Label>Recorte de facecam — arrastra para mover, arrastra la esquina para redimensionar</Label>
                <FacecamPicker
                  videoSrc={videoSrc}
                  rect={plan.face_crop ?? DEFAULT_FACE_CROP}
                  onChange={setFaceCrop}
                  disabled={busy}
                />
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                No hace falta recorte de facecam — este layout renderiza el gameplay a pantalla completa.
              </p>
            )}
          </div>
        </div>

        <div className="border border-primary/14 bg-card/75 p-5 sm:p-[22px]">
          <ClipEditor clips={plan.clips} onChange={setClips} disabled={busy} />
        </div>

        <div className="border border-primary/14 bg-card/75 p-5 sm:p-[22px]">
          <div className="flex flex-col gap-4">
            <SectionEyebrow label="SUBTÍTULOS" />
            <div className="flex flex-wrap items-center gap-3">
              <Button
                type="button"
                variant={plan.captions?.enabled ? 'default' : 'outline'}
                size="sm"
                disabled={busy}
                onClick={() => setCaptionsEnabled(!plan.captions?.enabled)}
                className="gap-1.5 rounded-none"
              >
                <Captions className="size-4" />
                {plan.captions?.enabled ? 'Subtítulos incrustados: activados' : 'Subtítulos incrustados: desactivados'}
              </Button>
              {plan.captions?.enabled ? (
                <select
                  value={plan.captions?.language ?? 'auto'}
                  disabled={busy}
                  onChange={(e) => setLanguage(e.target.value)}
                  className="h-9 rounded-none border border-input bg-transparent px-3 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40 disabled:pointer-events-none disabled:opacity-50"
                >
                  <option value="auto">Detección automática</option>
                  <option value="es">Español</option>
                  <option value="en">Inglés</option>
                </select>
              ) : null}
            </div>
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
            disabled={busy}
            className="neon-notch neon-glow inline-flex items-center gap-1.5 bg-primary px-5 py-2.5 font-[family-name:var(--font-display)] text-[13px] font-bold tracking-[0.06em] text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
          >
            {busy ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />}
            {stage === 'rendering' ? 'RENDERIZANDO…' : 'CREAR SHORTS'}
          </button>
          <Button variant="ghost" onClick={onStartOver} disabled={stage === 'rendering'}>
            Empezar de nuevo
          </Button>
        </div>

        {stage === 'rendered' && renderedPlan ? (
          <RenderResults renderState={renderState} job={job} renderedPlan={renderedPlan} stale={stale} />
        ) : null}
      </div>

      <div className="flex flex-col gap-3">
        <span className="font-[family-name:var(--font-mono)] text-[10.5px] tracking-[0.28em] text-muted-foreground">
          PREVIEW · 9:16
        </span>
        <StreamPreview videoSrc={videoSrc} variant={plan.variant} faceCrop={plan.face_crop} gameplayCrop={plan.gameplay_crop} />
        <p className="text-[11.5px] leading-relaxed text-muted-foreground/80">
          Layout aproximado. El render real conserva el HUD del juego completo.
        </p>
      </div>
    </div>
  );
}

/** A tiny two-region bar visualizing a layout variant (facecam/gameplay
 * proportion, a stacked triptych, or a solid full-frame block), matching the
 * mockup's mini icon next to each layout option. Purely decorative. */
function LayoutGlyph({ variant, selected }: { variant: StreamVariant; selected: boolean }) {
  const tone = selected ? 'bg-destructive' : 'bg-white/25';
  const dim = 'bg-white/12';

  return (
    <span className="flex h-[42px] w-6 shrink-0 flex-col overflow-hidden border border-white/25">
      {variant === 'streamer-vertical-stack-40-60' ? (
        <>
          <span className={cn('h-[40%]', tone)} />
          <span className={cn('flex-1', dim)} />
        </>
      ) : variant === 'streamer-vertical-stack' ? (
        <>
          <span className={cn('h-[26%]', tone)} />
          <span className={cn('flex-1', dim)} />
          <span className={cn('h-[26%]', tone)} />
        </>
      ) : (
        <span className={cn('flex-1', dim)} />
      )}
    </span>
  );
}

function ClipEditor({
  clips,
  onChange,
  disabled,
}: {
  clips: StreamClipRange[];
  onChange: (clips: StreamClipRange[]) => void;
  disabled: boolean;
}) {
  const updateClip = (id: string, patch: Partial<StreamClipRange>) =>
    onChange(clips.map((c) => (c.id === id ? { ...c, ...patch } : c)));
  const removeClip = (id: string) => onChange(clips.filter((c) => c.id !== id));
  const addClip = () => onChange([...clips, { id: nextClipId(), start_seconds: 0, end_seconds: 20, title: '' }]);

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <SectionEyebrow label="RANGOS DE CLIP" count={clips.length} />
        <button
          type="button"
          onClick={addClip}
          disabled={disabled}
          className="inline-flex items-center gap-1 font-[family-name:var(--font-mono)] text-[11px] tracking-[0.14em] text-destructive transition-opacity hover:opacity-80 disabled:pointer-events-none disabled:opacity-40"
        >
          <Plus className="size-3.5" />
          AÑADIR
        </button>
      </div>

      <div className="flex flex-col gap-3">
        {clips.map((clip, i) => {
          const invalid = !(clip.end_seconds > clip.start_seconds);
          return (
            <div key={clip.id} className="flex flex-col gap-2 border border-white/10 bg-card/40 p-3">
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
                    onChange={(e) => updateClip(clip.id, { start_seconds: Number(e.target.value) })}
                    className="w-24 rounded-none"
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
                    className="w-24 rounded-none"
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
                    placeholder={`Clip ${i + 1}`}
                    className="rounded-none"
                  />
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
              {invalid ? <p className="text-xs text-destructive">El fin debe ser posterior al inicio.</p> : null}
            </div>
          );
        })}
      </div>
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
    <div className="border border-primary/14 bg-card/75 p-5 sm:p-[22px]">
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

  return (
    <div className="border border-primary/14 bg-card/75 p-5 sm:p-[22px]">
      <div className="flex flex-col gap-4">
        <SectionEyebrow label="MÚSICA Y EFECTOS" />

        <div className="flex flex-wrap items-center gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="stream-music" className="text-xs text-muted-foreground">
              Música de fondo
            </Label>
            <select
              id="stream-music"
              value={musicKey}
              disabled={busy || songs === null}
              onChange={(e) => onMusicKey(e.target.value)}
              className="h-9 min-w-52 rounded-none border border-input bg-transparent px-3 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40 disabled:pointer-events-none disabled:opacity-50"
            >
              <option value="">{songs === null ? 'Cargando pistas…' : 'Ninguna'}</option>
              {(songs ?? []).map((s) => (
                <option key={s.id} value={s.id}>
                  {s.title}
                  {s.genre ? ` · ${s.genre}` : ''}
                </option>
              ))}
            </select>
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
                  <ToggleGroupItem key={v.value} value={String(v.value)} className="rounded-none text-xs">
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
            className="gap-1.5 rounded-none"
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
