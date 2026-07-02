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
import { SectionEyebrow } from '@/components/brand';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
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
            reset('That job is no longer available.');
            return;
          }
          if (j.status === 'failed') {
            setJob(j);
            setFailureReason(j.failure_reason || 'could not acquire the source video');
            setStage('failed');
            return;
          }
          if (j.status !== 'acquiring') {
            void loadEditor(j);
            return;
          }
        } catch (err) {
          if (isServiceUnavailable(err)) {
            reset('Stream Clips service is offline. Start it and try again.');
            return;
          }
          // transient network hiccup; keep polling
        }
      }
      reset('Timed out waiting for the source video to be ready.');
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
          ? 'Stream Clips service is offline. Start it and try again.'
          : err instanceof Error
            ? err.message
            : 'Could not start that job. Check the URL and try again.',
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
            ? 'Stream Clips service is offline. Start it and try again.'
            : err instanceof Error
              ? err.message
              : 'Could not process that file. Try another MP4.',
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
            setFailureReason(state.error || 'render failed');
            return;
          }
        } catch (err) {
          if (isServiceUnavailable(err)) {
            reset('Stream Clips service is offline. Start it and try again.');
            return;
          }
        }
        await sleep(1500);
        if (pollGen.current !== gen) return;
      }
      setStage('failed');
      setFailureReason('timed out waiting for the render to finish');
    },
    [reset],
  );

  const createShorts = useCallback(async () => {
    if (!job || !plan) return;
    if (!clipsAreValid(plan.clips)) {
      setError('Every clip needs an end time after its start time.');
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
          ? 'Stream Clips service is offline. Start it and try again.'
          : err instanceof Error
            ? err.message
            : 'Could not start the render.',
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
    <div className="flex flex-col gap-10">
      <header className="space-y-2">
        <h1 className="font-[family-name:var(--font-display)] text-3xl font-bold uppercase tracking-tight text-foreground sm:text-4xl">
          Stream Clips
        </h1>
        <p className="max-w-2xl text-sm text-muted-foreground">
          Paste a Twitch clip or VOD link — or upload your own MP4 — and forge it
          into vertical Shorts with your facecam stacked over the gameplay.
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
        <Card className="flex flex-col items-center justify-center gap-4 p-6 py-14 text-center sm:p-8">
          <Loader2 className="size-8 animate-spin text-primary" />
          <div className="flex flex-col gap-1">
            <p className="font-[family-name:var(--font-display)] text-lg font-semibold tracking-tight text-foreground">
              Fetching {job?.title || 'the clip'}…
            </p>
            <p className="text-sm text-muted-foreground">Downloading and probing the source video.</p>
          </div>
        </Card>
      ) : stage === 'failed' ? (
        <Card className="flex flex-col items-center justify-center gap-4 p-6 py-14 text-center sm:p-8">
          <AlertTriangle className="size-8 text-destructive" />
          <div className="flex flex-col gap-1">
            <p className="font-[family-name:var(--font-display)] text-lg font-semibold tracking-tight text-foreground">
              That job failed
            </p>
            <p className="max-w-md text-sm text-muted-foreground">{failureReason ?? 'Something went wrong.'}</p>
          </div>
          <Button onClick={() => reset('')}>Start over</Button>
        </Card>
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
    <Card className="p-6 sm:p-8">
      <div className="flex flex-col gap-6">
        <div className="flex flex-col gap-2">
          <Label htmlFor="stream-title">Title (optional)</Label>
          <Input
            id="stream-title"
            placeholder="Insane 1v5 clutch"
            value={title}
            disabled={submitting}
            onChange={(e) => onTitleChange(e.target.value)}
          />
        </div>

        <div className="flex flex-col gap-2">
          <Label htmlFor="stream-url">Twitch clip or VOD URL</Label>
          <div className="flex flex-col gap-2 sm:flex-row">
            <div className="relative flex-1">
              <Twitch className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                id="stream-url"
                placeholder="https://clips.twitch.tv/…"
                value={sourceUrl}
                disabled={submitting}
                onChange={(e) => onSourceUrlChange(e.target.value)}
                className="pl-9"
              />
            </div>
            <Button onClick={onSubmitUrl} disabled={submitting || !sourceUrl.trim()} className="gap-1.5">
              {submitting ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />}
              Fetch clip
            </Button>
          </div>
        </div>

        <div className="flex items-center gap-3 text-xs text-muted-foreground">
          <div className="h-px flex-1 bg-border" />
          or
          <div className="h-px flex-1 bg-border" />
        </div>

        <div className="flex flex-col gap-2">
          <Button
            variant="outline"
            disabled={submitting}
            onClick={() => fileInputRef.current?.click()}
            className="gap-1.5"
          >
            <UploadCloud className="size-4" />
            Upload an MP4
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
        </div>

        {error ? <p className="text-sm text-destructive">{error}</p> : null}
      </div>
    </Card>
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
      <div className="flex flex-col gap-6">
        <Card className="p-6 sm:p-8">
          <div className="flex flex-col gap-5">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <SectionEyebrow label="Layout" />
              <ToggleGroup
                type="single"
                variant="outline"
                value={plan.variant}
                onValueChange={(v) => v && setVariant(v as StreamVariant)}
                disabled={busy}
              >
                {STREAM_VARIANTS.map((v) => (
                  <ToggleGroupItem key={v.value} value={v.value} className="text-xs">
                    {v.label}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </div>

            {variantMeta.needsFaceCrop ? (
              <div className="flex flex-col gap-2">
                <Label>Facecam crop — drag to move, drag the corner handle to resize</Label>
                <FacecamPicker
                  videoSrc={videoSrc}
                  rect={plan.face_crop ?? DEFAULT_FACE_CROP}
                  onChange={setFaceCrop}
                  disabled={busy}
                />
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">
                No facecam crop needed — this layout renders the full gameplay frame.
              </p>
            )}
          </div>
        </Card>

        <Card className="p-6 sm:p-8">
          <ClipEditor clips={plan.clips} onChange={setClips} disabled={busy} />
        </Card>

        <Card className="p-6 sm:p-8">
          <div className="flex flex-col gap-4">
            <SectionEyebrow label="Captions" />
            <div className="flex flex-wrap items-center gap-3">
              <Button
                type="button"
                variant={plan.captions?.enabled ? 'default' : 'outline'}
                size="sm"
                disabled={busy}
                onClick={() => setCaptionsEnabled(!plan.captions?.enabled)}
                className="gap-1.5"
              >
                <Captions className="size-4" />
                {plan.captions?.enabled ? 'Burned captions on' : 'Burned captions off'}
              </Button>
              {plan.captions?.enabled ? (
                <select
                  value={plan.captions?.language ?? 'auto'}
                  disabled={busy}
                  onChange={(e) => setLanguage(e.target.value)}
                  className="h-9 rounded-md border border-input bg-transparent px-3 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40 disabled:pointer-events-none disabled:opacity-50"
                >
                  <option value="auto">Auto-detect</option>
                  <option value="es">Spanish</option>
                  <option value="en">English</option>
                </select>
              ) : null}
            </div>
          </div>
        </Card>

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
          <Button onClick={onCreate} disabled={busy} className="gap-1.5">
            {busy ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />}
            {stage === 'rendering' ? 'Rendering…' : 'Create Shorts'}
          </Button>
          <Button variant="ghost" onClick={onStartOver} disabled={stage === 'rendering'}>
            Start over
          </Button>
        </div>

        {stage === 'rendered' && renderedPlan ? (
          <RenderResults renderState={renderState} job={job} renderedPlan={renderedPlan} stale={stale} />
        ) : null}
      </div>

      <div className="flex flex-col gap-4">
        <SectionEyebrow label="Preview" />
        <StreamPreview videoSrc={videoSrc} variant={plan.variant} faceCrop={plan.face_crop} gameplayCrop={plan.gameplay_crop} />
        <p className="text-xs text-muted-foreground">
          Approximate 9:16 layout. The real render preserves the full in-game HUD.
        </p>
      </div>
    </div>
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
        <SectionEyebrow label="Clip ranges" count={clips.length} />
        <Button type="button" variant="outline" size="sm" onClick={addClip} disabled={disabled} className="gap-1.5">
          <Plus className="size-3.5" />
          Add clip
        </Button>
      </div>

      <div className="flex flex-col gap-3">
        {clips.map((clip, i) => {
          const invalid = !(clip.end_seconds > clip.start_seconds);
          return (
            <div key={clip.id} className="flex flex-col gap-2 rounded-lg border border-border bg-card/40 p-3">
              <div className="flex flex-wrap items-end gap-2">
                <div className="flex flex-col gap-1">
                  <Label htmlFor={`${clip.id}-start`} className="text-xs text-muted-foreground">
                    Start (s)
                  </Label>
                  <Input
                    id={`${clip.id}-start`}
                    type="number"
                    min={0}
                    step="0.1"
                    value={clip.start_seconds}
                    disabled={disabled}
                    onChange={(e) => updateClip(clip.id, { start_seconds: Number(e.target.value) })}
                    className="w-24"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor={`${clip.id}-end`} className="text-xs text-muted-foreground">
                    End (s)
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
                    Title (optional)
                  </Label>
                  <Input
                    id={`${clip.id}-title`}
                    value={clip.title ?? ''}
                    disabled={disabled}
                    onChange={(e) => updateClip(clip.id, { title: e.target.value })}
                    placeholder={`Clip ${i + 1}`}
                  />
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  disabled={disabled || clips.length <= 1}
                  onClick={() => removeClip(clip.id)}
                  aria-label="Remove clip"
                >
                  <Trash2 className="size-4" />
                </Button>
              </div>
              {invalid ? <p className="text-xs text-destructive">End must be after start.</p> : null}
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
    <Card className="p-6 sm:p-8">
      <div className="flex flex-col gap-4">
        <SectionEyebrow label="Rendered Shorts" count={renderState.videos.length} />

        {stale ? (
          <p className="flex items-center gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-xs text-amber-500">
            <AlertTriangle className="size-3.5 shrink-0" />
            These Shorts were rendered before your latest edits. Click Create Shorts to apply them —
            downloads are disabled until then so you never keep an outdated file.
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
            No Shorts were produced.
          </div>
        ) : (
          <div className="grid gap-5 sm:grid-cols-2">
            {renderState.videos.map((v) => {
              const url = streamsApi.videoUrl(job.id, renderedPlan.variant, v.clip_id);
              return (
                <div key={v.clip_id} className="flex flex-col gap-2">
                  {/* eslint-disable-next-line jsx-a11y/media-has-caption */}
                  <video src={url} controls className="aspect-[9/16] w-full rounded-lg bg-black object-contain" />
                  <div className="flex items-center justify-between gap-2">
                    <span className="truncate text-sm text-foreground">{v.title || v.clip_id}</span>
                    {stale ? (
                      <Button variant="outline" size="icon-sm" disabled aria-label={`Download ${v.title || v.clip_id} (outdated)`}>
                        <Download className="size-4" />
                      </Button>
                    ) : (
                      <Button asChild variant="outline" size="icon-sm">
                        <a href={url} download aria-label={`Download ${v.title || v.clip_id}`}>
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
    </Card>
  );
}

/** Preset music gains: quiet bed, balanced, or music-forward. */
const MUSIC_VOLUMES = [
  { value: 0.15, label: 'Low' },
  { value: 0.25, label: 'Medium' },
  { value: 0.4, label: 'High' },
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
    <Card className="p-6 sm:p-8">
      <div className="flex flex-col gap-4">
        <SectionEyebrow label="Music & effects" />

        <div className="flex flex-wrap items-center gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="stream-music" className="text-xs text-muted-foreground">
              Background music
            </Label>
            <select
              id="stream-music"
              value={musicKey}
              disabled={busy || songs === null}
              onChange={(e) => onMusicKey(e.target.value)}
              className="h-9 min-w-52 rounded-md border border-input bg-transparent px-3 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/40 disabled:pointer-events-none disabled:opacity-50"
            >
              <option value="">{songs === null ? 'Loading tracks…' : 'None'}</option>
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
              <Label className="text-xs text-muted-foreground">Music volume</Label>
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
            {grade ? 'Viral grade on' : 'Viral grade off'}
          </Button>
          <p className="text-xs text-muted-foreground">
            Light contrast and saturation lift. Music mixes under the streamer&apos;s audio.
          </p>
        </div>
      </div>
    </Card>
  );
}
