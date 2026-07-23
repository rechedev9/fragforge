'use client';

import type { ReactNode } from 'react';
import type { Video } from '@/lib/api/types';
import { cn } from '@/lib/utils';
import { PipelineSteps } from '@/components/brand/pipeline-steps';
import { RecDot } from '@/components/brand/rec-dot';
import { ReelCover } from '@/components/brand/reel-cover';

const FORMAT_LABEL: Record<string, string> = { 'short-9x16': '9:16', 'landscape-16x9': '16:9' };

/**
 * A render still in flight (queued / recording / composing) — the mockup's
 * EN COLA, CAPTURANDO and EDITANDO cards, each with its own accent: dim and
 * idle while queued, magenta with a pulsing REC dot while capturing, and a
 * cyan-to-violet gradient sweep while composing. While capturing, the
 * orchestrator reports real segment progress (done/total), so the card shows
 * "CAPTURANDO 2/4 · 50%" with the bar driven by the true percent; until the
 * first segment lands (or for the editing stage, which has no such signal) an
 * indeterminate bar stands in instead of a fabricated number.
 */
export function RenderingCard({ video }: { video: Video }) {
  const isQueued = video.status === 'queued';
  const isCapturing = video.status === 'recording';
  const isComposing = video.status === 'composing';
  const formatBadge = video.editConfig ? FORMAT_LABEL[video.editConfig.format] : undefined;

  // Real capture progress, present only while capturing and once at least one
  // segment clip exists. A single derived value carries done, total, and the
  // percent together so the JSX guards on one thing.
  const capture =
    isCapturing && video.captureProgress && video.captureProgress.total > 0
      ? {
          done: video.captureProgress.done,
          total: video.captureProgress.total,
          pct: Math.min(
            100,
            Math.max(0, Math.round((video.captureProgress.done / video.captureProgress.total) * 100)),
          ),
        }
      : undefined;

  let accentClass: string;
  if (isCapturing) {
    accentClass = 'neon-brackets border-stream/45 [--neon-bracket-color:var(--stream)]';
  } else if (isComposing) {
    accentClass = 'border-primary/40';
  } else {
    accentClass = 'border-border';
  }

  let stageLabel: ReactNode;
  if (isCapturing) {
    stageLabel = (
      <span className="inline-flex min-h-8 items-center border border-stream/35 bg-background/85 px-2.5">
        <RecDot label="CAPTURANDO · EN TU RIG" />
      </span>
    );
  } else if (isComposing) {
    stageLabel = (
      <span className="inline-flex min-h-8 items-center border border-primary/35 bg-background/85 px-2.5 font-[family-name:var(--font-mono)] text-[10px] tracking-[0.18em] text-primary">
        EDITANDO
      </span>
    );
  } else {
    stageLabel = (
      <span className="inline-flex min-h-8 items-center border border-border bg-background/85 px-2.5 font-[family-name:var(--font-mono)] text-[10px] tracking-[0.18em] text-muted-foreground">
        EN COLA
      </span>
    );
  }

  let progressBar: ReactNode;
  if (isCapturing) {
    progressBar =
      capture !== undefined ? (
        <span
          className="block h-1 bg-stream shadow-[0_0_8px_color-mix(in_oklch,var(--stream)_60%,transparent)] transition-[width] duration-500"
          style={{ width: `${capture.pct}%` }}
        />
      ) : (
        <span className="neon-pulse block h-1 w-2/3 bg-stream shadow-[0_0_8px_color-mix(in_oklch,var(--stream)_60%,transparent)]" />
      );
  } else if (isComposing) {
    progressBar = <span className="neon-pulse block h-1 w-1/2 bg-gradient-to-r from-primary to-chart-3" />;
  } else {
    progressBar = null;
  }

  const meta = video.score ? `${video.map} · ${video.score}` : video.map;
  let progressLabel = 'ESPERANDO CAPTURA';
  let progressAriaLabel: string | undefined;
  if (isCapturing) {
    progressLabel = capture ? `SEGMENTOS ${capture.done}/${capture.total}` : 'PREPARANDO CAPTURA';
    progressAriaLabel = 'Progreso de captura';
  } else if (isComposing) {
    progressLabel = 'CORTES + RITMO';
    progressAriaLabel = 'Progreso de edición';
  }

  return (
    <div
      className={cn(
        'studio-panel studio-panel-interactive studio-defer-render flex h-full flex-col',
        accentClass,
      )}
    >
      <div className="relative aspect-video overflow-hidden border-b border-border bg-muted">
        {video.thumbnailUrl ? (
          // eslint-disable-next-line @next/next/no-img-element -- dynamic local reel cover
          <img src={video.thumbnailUrl} alt="" className="size-full object-cover opacity-45" />
        ) : (
          <ReelCover seed={video.id} label={video.map} className="opacity-65" />
        )}
        <span
          aria-hidden
          className={cn(
            'absolute inset-0',
            isCapturing && 'bg-gradient-to-br from-stream/20 via-background/25 to-background/70',
            isComposing && 'bg-gradient-to-br from-primary/15 via-background/25 to-background/70',
            isQueued && 'bg-background/55',
          )}
        />

        {formatBadge ? (
          <span className="absolute top-2.5 right-2.5 border border-border-strong bg-background/90 px-2 py-1 font-[family-name:var(--font-mono)] text-[10px] tracking-[0.12em] text-muted-foreground">
            {formatBadge}
          </span>
        ) : null}

        <span className="absolute bottom-2.5 left-2.5">{stageLabel}</span>
      </div>

      <div className="flex flex-1 flex-col gap-4 p-4">
        <p
          className={cn(
            'truncate font-[family-name:var(--font-display)] text-base font-bold',
            isQueued ? 'text-muted-foreground' : 'text-foreground',
          )}
        >
          {video.title}
        </p>
        <p className="-mt-2 truncate font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.1em] text-muted-foreground">
          {meta}
        </p>

        <div className="border-y border-border/70 py-3">
          <PipelineSteps status={video.status} className="gap-x-2 text-[10px]" />
        </div>

        <div className="mt-auto">
          <div className="mb-2 flex items-center justify-between gap-3 font-[family-name:var(--font-mono)] text-[10px] uppercase tracking-[0.12em]">
            <span className={isCapturing ? 'text-stream' : 'text-muted-foreground'}>
              {progressLabel}
            </span>
            {capture ? <span className="text-stream">{capture.pct}%</span> : null}
          </div>
          <div
            className="h-1 overflow-hidden bg-white/8"
            role={isCapturing || isComposing ? 'progressbar' : undefined}
            aria-label={progressAriaLabel}
            aria-valuemin={capture ? 0 : undefined}
            aria-valuemax={capture ? 100 : undefined}
            aria-valuenow={capture?.pct}
          >
            {progressBar}
          </div>
        </div>
      </div>
    </div>
  );
}
