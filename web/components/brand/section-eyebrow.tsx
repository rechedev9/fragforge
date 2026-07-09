import { cn } from '@/lib/utils';

export type SectionEyebrowProps = {
  /** Uppercase section label, e.g. "PARTIDAS", "BIBLIOTECA". */
  label: string;
  /** Optional section number, rendered as `// 0N — LABEL`. */
  number?: number;
  /** Optional count shown as a mono pill after the label. */
  count?: number;
  /** Signal color: cyan (default) everywhere, magenta on the stream route. */
  accent?: 'cyan' | 'magenta';
  className?: string;
};

/**
 * SectionEyebrow — the NEON HUD section head: `// 0N — LABEL` in Share Tech
 * Mono, signal cyan, very wide tracking. Small and quiet so it frames a
 * section without competing with the screen H1. Without `number` it renders
 * the bare label (the mockup's panel-head style, e.g. "LAYOUT"). `accent`
 * switches to magenta for the Clips de stream route, per the skin's color
 * rule (magenta = REC/stream/likes/música/destructivo).
 */
export function SectionEyebrow({ label, number, count, accent = 'cyan', className }: SectionEyebrowProps) {
  return (
    <div className={cn('flex items-center gap-2', className)}>
      <span
        className={cn(
          'font-[family-name:var(--font-mono)] text-xs uppercase tracking-[0.3em]',
          accent === 'magenta' ? 'text-stream' : 'text-primary',
        )}
      >
        {number !== undefined ? `// ${String(number).padStart(2, '0')} — ` : null}
        {label}
      </span>
      {count !== undefined ? (
        <span className="font-[family-name:var(--font-mono)] text-xs tabular-nums text-muted-foreground/70">
          {count}
        </span>
      ) : null}
    </div>
  );
}
