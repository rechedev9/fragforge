import { cn } from '@/lib/utils';

export type RecDotProps = {
  /** Optional caption next to the dot; defaults to "LIVE ON YOUR RIG". */
  label?: string;
  /** Render the dot only, no label. */
  hideLabel?: boolean;
  className?: string;
};

/**
 * RecDot — a small pulsing magenta dot with an optional "LIVE ON YOUR RIG"
 * label, shown on videos currently capturing on the player's machine. Magenta
 * is reserved for this REC indicator and other stream-specific signals. Honors
 * reduced motion.
 */
export function RecDot({ label = 'LIVE ON YOUR RIG', hideLabel = false, className }: RecDotProps) {
  return (
    <span className={cn('inline-flex items-center gap-2', className)}>
      <span className="relative grid size-2.5 place-items-center">
        <span className="absolute inline-flex size-2.5 rounded-full bg-stream/40 neon-pulse" />
        <span className="relative inline-flex size-1.5 rounded-full bg-stream shadow-[0_0_10px_color-mix(in_oklch,var(--stream)_80%,transparent)]" />
      </span>
      {!hideLabel ? (
        <span className="font-[family-name:var(--font-mono)] text-[0.7rem] uppercase tracking-widest text-stream">
          {label}
        </span>
      ) : null}
    </span>
  );
}
