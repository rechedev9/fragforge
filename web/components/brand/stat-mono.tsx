import { cn } from '@/lib/utils';

export type StatMonoProps = {
  /** Short uppercase caption, e.g. "K", "D", "K/D". */
  label: string;
  /** The number/score; rendered mono with tabular figures. */
  value: string | number;
  /** Stack label above value (default) or place it inline before the value. */
  layout?: 'stacked' | 'inline';
  /** Tint the value with the lime signal color (e.g. a standout stat). */
  accent?: boolean;
  className?: string;
};

/**
 * StatMono — a labeled mono number. Every stat in FragForge (K / D / A / MVP /
 * K/D / scores / ticks / durations) is rendered through this so digits stay
 * tabular and the scoreboard/demo-tick feel is consistent.
 */
export function StatMono({
  label,
  value,
  layout = 'stacked',
  accent = false,
  className,
}: StatMonoProps) {
  if (layout === 'inline') {
    return (
      <span className={cn('inline-flex items-baseline gap-1.5', className)}>
        <span className="text-[0.7rem] font-medium uppercase tracking-wide text-muted-foreground">
          {label}
        </span>
        <span
          className={cn(
            'font-[family-name:var(--font-mono)] text-sm font-semibold tabular-nums',
            accent ? 'text-primary' : 'text-foreground',
          )}
        >
          {value}
        </span>
      </span>
    );
  }

  return (
    <div className={cn('flex flex-col gap-0.5', className)}>
      <span className="text-[0.65rem] font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </span>
      <span
        className={cn(
          'font-[family-name:var(--font-mono)] text-lg font-semibold leading-none tabular-nums',
          accent ? 'text-primary' : 'text-foreground',
        )}
      >
        {value}
      </span>
    </div>
  );
}
