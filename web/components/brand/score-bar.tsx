import { cn } from '@/lib/utils';

export type ScoreBarProps = {
  /** true = win (cyan accent + glow), false = loss (muted zinc). */
  win: boolean;
  className?: string;
};

/**
 * ScoreBar — a thin vertical accent bar for match rows. Cyan with a soft glow
 * = win, dim zinc = loss, for a fast win/loss scan down a list. Per the design
 * language, loss is muted zinc, never red/magenta.
 */
export function ScoreBar({ win, className }: ScoreBarProps) {
  return (
    <span
      aria-hidden
      className={cn(
        'block w-1 self-stretch',
        win
          ? 'bg-primary shadow-[0_0_10px_color-mix(in_oklch,var(--primary)_50%,transparent)]'
          : 'bg-muted-foreground/40',
        className,
      )}
    />
  );
}
