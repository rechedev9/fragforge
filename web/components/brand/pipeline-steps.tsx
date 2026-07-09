import { Fragment } from 'react';
import { cn } from '@/lib/utils';
import type { VideoStatus } from '@/lib/api/types';

export type PipelineStepsProps = {
  status: VideoStatus;
  className?: string;
};

/** The four product-facing pipeline stages, in order (visible labels). */
const STEPS = ['COLA', 'CAPTURA', 'EDICIÓN', 'LISTO'] as const;

/**
 * Index of the stage a status sits at:
 * queued→COLA, recording→CAPTURA, composing→EDICIÓN, ready→LISTO.
 * `failed` keeps the bar but marks the active stage as errored.
 */
function activeIndex(status: VideoStatus): number {
  switch (status) {
    case 'queued':
      return 0;
    case 'recording':
      return 1;
    case 'composing':
      return 2;
    case 'ready':
      return 3;
    case 'failed':
      return 2;
  }
}

/**
 * PipelineSteps — the hero of the product story, NEON HUD style: a mono
 * COLA ▸ CAPTURA ▸ EDICIÓN ▸ LISTO line. The active step glows (magenta while
 * capturing — the REC color — cyan otherwise, per the skin's color rules);
 * done steps are dim, future steps dimmer. `failed` paints the active step
 * magenta. Honors prefers-reduced-motion (pulse disabled in CSS).
 */
export function PipelineSteps({ status, className }: PipelineStepsProps) {
  const active = activeIndex(status);
  const failed = status === 'failed';
  const done = status === 'ready';

  return (
    <ol
      className={cn(
        'flex flex-wrap items-center gap-x-2.5 gap-y-1 font-[family-name:var(--font-mono)] text-[0.7rem] uppercase tracking-[0.14em]',
        className,
      )}
    >
      {STEPS.map((label, i) => {
        const isDone = i < active || (done && i === active);
        const isActive = i === active && !done;
        const isMagenta = failed || i === 1; // failed step or CAPTURA (the REC stage)

        return (
          <Fragment key={label}>
            {i > 0 ? (
              <li aria-hidden className="text-muted-foreground/40">
                ▸
              </li>
            ) : null}
            <li
              className={cn(
                isActive &&
                  (isMagenta
                    ? 'text-stream [text-shadow:0_0_10px_color-mix(in_oklch,var(--stream)_60%,transparent)]'
                    : 'text-primary [text-shadow:0_0_10px_color-mix(in_oklch,var(--primary)_60%,transparent)]'),
                isActive && !failed && 'neon-pulse',
                !isActive && isDone && (done && i === active ? 'text-primary' : 'text-muted-foreground'),
                !isActive && !isDone && 'text-muted-foreground/60',
              )}
            >
              {label}
            </li>
          </Fragment>
        );
      })}
    </ol>
  );
}
