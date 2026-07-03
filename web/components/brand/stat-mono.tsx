import { cn } from '@/lib/utils';

export type StatMonoProps = {
  /** Short uppercase caption, e.g. "K", "D", "K/D". */
  label: string;
  /** The number/score; rendered mono with tabular figures. */
  value: string | number;
  /** Stack label above value (default) or place it inline before the value. */
  layout?: 'stacked' | 'inline';
  /** Tint the value with the cyan signal color (e.g. a standout stat). */
  accent?: boolean;
  className?: string;
};

/**
 * StatMono — a labeled mono number, NEON HUD style. Every stat in FragForge
 * (K / D / A / MVP / K/D / scores / ticks / durations) is rendered through
 * this: Share Tech Mono tabular value over a dim mono label with wide
 * tracking, so the scoreboard/demo-tick feel is consistent.
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
        <span className="font-[family-name:var(--font-mono)] text-[9.5px] uppercase tracking-[0.2em] text-muted-foreground/70">
          {label}
        </span>
        <span
          className={cn(
            'font-[family-name:var(--font-mono)] text-sm tabular-nums',
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
      <span className="font-[family-name:var(--font-mono)] text-[9.5px] uppercase tracking-[0.2em] text-muted-foreground/70">
        {label}
      </span>
      <span
        className={cn(
          'font-[family-name:var(--font-mono)] text-lg leading-none tabular-nums',
          accent ? 'text-primary' : 'text-foreground',
        )}
      >
        {value}
      </span>
    </div>
  );
}
