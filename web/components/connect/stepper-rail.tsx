'use client';

import { Check } from 'lucide-react';
import { cn } from '@/lib/utils';

export type StepperStep = {
  /** Short title shown beside the number. */
  title: string;
  /** One-line description under the title. */
  hint: string;
};

export type StepperRailProps = {
  steps: StepperStep[];
  /** Zero-based index of the active step. Steps before it are done. */
  current: number;
  className?: string;
};

/**
 * A vertical numbered stepper rail. Done steps fill lime with a check, the
 * active step gets a lime ring, future steps stay muted. Numbers are mono so
 * they sit in the same scoreboard family as the rest of the app.
 */
export function StepperRail({ steps, current, className }: StepperRailProps) {
  return (
    <ol className={cn('flex flex-col', className)}>
      {steps.map((step, index) => {
        const done = index < current;
        const active = index === current;
        const isLast = index === steps.length - 1;

        return (
          <li key={step.title} className="flex gap-4">
            <div className="flex flex-col items-center">
              <span
                className={cn(
                  'grid size-9 shrink-0 place-items-center rounded-full border font-[family-name:var(--font-mono)] text-sm tabular-nums transition-colors',
                  done && 'border-primary bg-primary text-primary-foreground',
                  active && 'border-primary text-primary',
                  !done && !active && 'border-border text-muted-foreground',
                )}
              >
                {done ? <Check className="size-4" aria-hidden /> : index + 1}
              </span>
              {!isLast ? (
                <span
                  className={cn(
                    'my-1 w-px flex-1',
                    index < current ? 'bg-primary/60' : 'bg-border',
                  )}
                />
              ) : null}
            </div>

            <div className={cn('pb-8', isLast && 'pb-0')}>
              <p
                className={cn(
                  'text-sm font-medium leading-none',
                  active ? 'text-foreground' : 'text-muted-foreground',
                )}
              >
                {step.title}
              </p>
              <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground/70">
                {step.hint}
              </p>
            </div>
          </li>
        );
      })}
    </ol>
  );
}
