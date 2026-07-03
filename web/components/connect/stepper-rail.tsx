'use client';

import { Check } from 'lucide-react';
import { cn } from '@/lib/utils';

export type StepperStep = {
  /** Uppercase mono label shown beside the step marker, e.g. "VINCULA STEAM". */
  label: string;
};

export type StepperRailProps = {
  steps: StepperStep[];
  /** Zero-based index of the active step. Steps before it are done. */
  current: number;
  className?: string;
};

/**
 * A horizontal numbered stepper, NEON HUD style (mockup 3c): done steps fill
 * cyan with a check, the active step gets a cyan outline with a glow and its
 * own two-digit index, future steps stay dim. Numbers and labels are mono so
 * they read as HUD telemetry rather than a generic wizard.
 */
export function StepperRail({ steps, current, className }: StepperRailProps) {
  return (
    <ol
      className={cn(
        'flex flex-wrap items-center justify-center gap-x-[18px] gap-y-3 font-[family-name:var(--font-mono)] text-[11px] tracking-[0.18em]',
        className,
      )}
    >
      {steps.map((step, index) => {
        const done = index < current;
        const active = index === current;
        const isLast = index === steps.length - 1;

        return (
          <li key={step.label} className="flex items-center gap-[18px]">
            <span
              className={cn(
                'flex items-center gap-2',
                done && 'text-primary',
                active && 'text-foreground',
                !done && !active && 'text-muted-foreground',
              )}
            >
              <span
                className={cn(
                  'grid size-[22px] shrink-0 place-items-center rounded-full text-[10px] font-bold tabular-nums',
                  done && 'bg-primary text-primary-foreground',
                  active &&
                    'border-[1.5px] border-primary text-primary shadow-[0_0_12px_color-mix(in_oklch,var(--primary)_40%,transparent)]',
                  !done && !active && 'border border-white/25 text-muted-foreground',
                )}
              >
                {done ? <Check className="size-3" aria-hidden /> : String(index + 1).padStart(2, '0')}
              </span>
              {step.label}
            </span>
            {!isLast ? (
              <span aria-hidden className={cn('h-px w-11', done ? 'bg-primary' : 'bg-white/15')} />
            ) : null}
          </li>
        );
      })}
    </ol>
  );
}
