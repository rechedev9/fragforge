import { cn } from '@/lib/utils';

export type ScoreBarProps = {
  /** true = win (lime accent), false = loss (muted zinc). */
  win: boolean;
  className?: string;
};

/**
 * ScoreBar — a thin vertical accent bar for match rows. Lime = win, zinc =
 * loss, for a fast win/loss scan down a list. Per the design language, loss is
 * muted zinc, never red.
 */
export function ScoreBar({ win, className }: ScoreBarProps) {
  return (
    <span
      aria-hidden
      className={cn(
        'block w-1 self-stretch rounded-full',
        win ? 'bg-primary' : 'bg-muted-foreground/40',
        className,
      )}
    />
  );
}
