'use client';

import type { ReactNode } from 'react';
import type { Video } from '@/lib/api/types';
import { cn } from '@/lib/utils';
import { RecDot } from '@/components/brand/rec-dot';

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
          pct: Math.round((video.captureProgress.done / video.captureProgress.total) * 100),
        }
      : undefined;

  let accentClass: string;
  if (isCapturing) {
    accentClass = 'border-destructive/40';
  } else if (isComposing) {
    accentClass = 'border-primary/14';
  } else {
    accentClass = 'border-white/10 bg-card/60';
  }

  let stageLabel: ReactNode;
  if (isCapturing) {
    stageLabel = <RecDot label="CAPTURANDO · EN TU RIG" />;
  } else if (isComposing) {
    stageLabel = (
      <span className="font-[family-name:var(--font-mono)] text-[10px] tracking-[0.24em] text-muted-foreground">
        EDICIÓN…
      </span>
    );
  } else {
    stageLabel = (
      <span className="font-[family-name:var(--font-mono)] text-[10px] tracking-[0.24em] text-muted-foreground/70">
        EN COLA
      </span>
    );
  }

  let progressBar: ReactNode;
  if (isCapturing) {
    progressBar =
      capture !== undefined ? (
        <span
          className="block h-[3px] bg-destructive shadow-[0_0_8px_rgba(255,45,120,0.6)] transition-[width] duration-500"
          style={{ width: `${capture.pct}%` }}
        />
      ) : (
        <span className="neon-pulse block h-[3px] w-2/3 bg-destructive shadow-[0_0_8px_rgba(255,45,120,0.6)]" />
      );
  } else if (isComposing) {
    progressBar = <span className="neon-pulse block h-[3px] w-1/2 bg-gradient-to-r from-primary to-chart-3" />;
  } else {
    progressBar = null;
  }

  return (
    <div
      className={cn(
        'relative border bg-card/80',
        accentClass,
      )}
    >
      <div
        className={cn(
          'relative flex h-[150px] items-center justify-center overflow-hidden',
          isCapturing && 'bg-gradient-to-br from-destructive/16 to-card/40',
          isComposing && 'bg-gradient-to-br from-chart-3/16 to-card/40',
        )}
      >
        {formatBadge ? (
          <span className="absolute top-2 right-2 bg-background/80 px-1.5 py-0.5 font-[family-name:var(--font-mono)] text-[9px] tracking-[0.12em] text-muted-foreground">
            {formatBadge}
          </span>
        ) : null}

        {stageLabel}
      </div>

      <div className="flex flex-col gap-1 p-4">
        <p
          className={cn(
            'truncate font-[family-name:var(--font-display)] text-[14.5px] font-bold',
            isQueued ? 'text-muted-foreground' : 'text-foreground',
          )}
        >
          {video.title}
        </p>

        {isCapturing ? (
          <div className="flex items-center justify-between font-[family-name:var(--font-mono)] text-[9.5px] uppercase tracking-[0.16em]">
            <span className="text-destructive">
              {capture ? `CAPTURANDO ${capture.done}/${capture.total} · ${capture.pct}%` : 'CAPTURANDO'}
            </span>
          </div>
        ) : (
          <p className="font-[family-name:var(--font-mono)] text-[9.5px] uppercase tracking-[0.16em] text-muted-foreground">
            {isComposing ? 'EDITANDO — CORTES + RITMO' : 'ESPERANDO CAPTURA'}
          </p>
        )}

        <div className="mt-3 h-[3px] bg-white/8">{progressBar}</div>
      </div>
    </div>
  );
}
