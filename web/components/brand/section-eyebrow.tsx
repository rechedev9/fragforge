import { cn } from '@/lib/utils';

export type SectionEyebrowProps = {
  /** Uppercase section label, e.g. "RENDERING", "READY". */
  label: string;
  /** Optional count shown as a mono pill after the label. */
  count?: number;
  className?: string;
};

/**
 * SectionEyebrow — an uppercase Space Grotesk label with an optional count,
 * used for section heads. Small, tracked, and quiet so it frames a section
 * without competing with the screen H1.
 */
export function SectionEyebrow({ label, count, className }: SectionEyebrowProps) {
  return (
    <div className={cn('flex items-center gap-2', className)}>
      <span className="font-[family-name:var(--font-display)] text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">
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
