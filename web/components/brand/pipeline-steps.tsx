import { cn } from '@/lib/utils';
import type { VideoStatus } from '@/lib/api/types';

export type PipelineStepsProps = {
  status: VideoStatus;
  className?: string;
};

/** The four product-facing pipeline stages, in order. */
const STEPS = ['Queued', 'Capturing', 'Editing', 'Ready'] as const;

/**
 * Index of the stage a status sits at:
 * queued→Queued, recording→Capturing, composing→Editing, ready→Ready.
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
 * PipelineSteps — the hero of the product story. A horizontal stepper rendering
 * Queued → Capturing → Editing → Ready. Done steps are lime-filled, the active
 * step pulses lime, future steps are muted. `failed` paints the active step
 * destructive. Honors prefers-reduced-motion (pulse disabled in CSS).
 */
export function PipelineSteps({ status, className }: PipelineStepsProps) {
  const active = activeIndex(status);
  const failed = status === 'failed';
  const done = status === 'ready';

  return (
    <ol className={cn('flex items-center gap-1.5', className)}>
      {STEPS.map((label, i) => {
        const isDone = i < active || (done && i === active);
        const isActive = i === active && !done;
        const isFailed = failed && isActive;

        return (
          <li key={label} className="flex flex-1 flex-col gap-1.5">
            <span
              aria-hidden
              className={cn(
                'h-1 rounded-full transition-colors',
                isFailed && 'bg-destructive',
                !isFailed && isActive && 'bg-primary fragforge-pulse',
                !isFailed && isDone && 'bg-primary',
                !isActive && !isDone && 'bg-muted',
              )}
            />
            <span
              className={cn(
                'text-[0.65rem] font-medium uppercase tracking-wide',
                isFailed && 'text-destructive',
                !isFailed && (isActive || isDone) ? 'text-foreground' : 'text-muted-foreground',
              )}
            >
              {label}
            </span>
          </li>
        );
      })}
    </ol>
  );
}
